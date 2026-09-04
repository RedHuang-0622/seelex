import { applyEvent, validateSnapshot } from "./protocol.js";
import { PROCESS_TOP_KEYS, classifySnapshot, processContextOf, processRuntimeOf } from "./snapshot-shape.js";

export function createGUIClient(options) {
  let snapshot = null;
  let snapshotRevisionFloor = 0;
  let lastEventSeq = 0;
  // processContext 是桌面保留的进程段（会话目录/工作区/绑定 + 进程运行原件）：
  // 会话粒度载荷只描述本会话事实，进程面板（账户/插件/技能/定时任务/模型）
  // 数据来自这份进程段，不随会话载荷抖动（G3 收口，见 snapshot-shape.js）。
  // 目录字段（sessions/session_workspaces）是后端 per-project 网格的联合
  // 投影（G6），前端只做渲染分组，不推断存储归属。
  let processContext = null;
  let refreshPromise = null;
  let refreshQueued = false;
  let refreshScroll = "auto";
  // 事件应用串行化：缺口补取与快照重拉都是异步的，两条事件并发落地会把
  // lastEventSeq 与 snapshot 交叉写坏。
  let eventChain = Promise.resolve();
  // 会话新鲜度诊断计数（只进不出，供角标/控制台；不参与业务状态）。
  const diag = { events: 0, incrementals: 0, refreshes: 0, gaps: 0, replays: 0, buffered: 0, lastSeq: 0 };

  function reportDiag(extra = {}) {
    Object.assign(diag, extra);
    diag.lastSeq = lastEventSeq;
    options.onDiag?.({ ...diag });
  }

  // rememberProcessContext 在快照边界记录桌面进程上下文。联合 Workbench
  // 快照与进程制品都会刷新它；会话粒度制品自身不含进程字段，不覆盖。
  // 最小夹具/低版本宿主把进程字段内联在 runtime 时按键识别后同样保留。
  function rememberProcessContext(value) {
    if (!value || typeof value !== "object") return;
    const kind = classifySnapshot(value);
    if (kind === "workbench" || kind === "process") {
      processContext = processContextOf(value);
      return;
    }
    if (Object.keys(processRuntimeOf(value.runtime || {})).length > 0) {
      processContext = processContextOf(value);
    }
  }

  // mergeProcessContext 把保留的进程段与会话粒度制品合并成渲染用联合快照：
  // 会话载荷只描述本会话事实，目录/工作区/账户/插件/技能等由进程段提供。
  // 联合快照/自带进程运行原件的载荷已完备（按 runtime 内容判定，而不是按
  // 顶层 sessions 推断——合并目录后会话制品也会带 sessions，但它的 runtime
  // 仍是 session-only，后续增量仍需进程段兜底）。
  function mergeProcessContext(value) {
    if (!processContext || !value || typeof value !== "object") return value;
    const runtime = value.runtime || {};
    const missingTopKeys = PROCESS_TOP_KEYS.filter(key => value[key] === undefined && processContext[key] !== undefined);
    const hasProcessRuntime = Object.keys(processRuntimeOf(runtime)).length > 0;
    if (hasProcessRuntime && missingTopKeys.length === 0) return value;
    const nextRuntime = hasProcessRuntime ? runtime : { ...runtime, ...processRuntimeOf(processContext.runtime || {}) };
    const merged = { ...value, runtime: nextRuntime };
    for (const key of missingTopKeys) merged[key] = processContext[key];
    return merged;
  }

  async function refresh(request = {}) {
    refreshQueued = true;
    refreshScroll = mergeScrollMode(refreshScroll, requestedScrollMode(request));
    if (refreshPromise) return refreshPromise;
    refreshPromise = runRefreshLoop();
    try { await refreshPromise; }
    finally { refreshPromise = null; }
  }

  async function runRefreshLoop() {
    while (refreshQueued) {
      refreshQueued = false;
      const scrollMode = refreshScroll;
      refreshScroll = "auto";
      try {
        acceptSnapshot(await options.loadSnapshot(), scrollMode);
      } catch (error) { options.onError(error); }
    }
  }

  function acceptSnapshot(value, scrollMode = "bottom") {
    const candidate = validateSnapshot(value);
    if (snapshot && Number(candidate.revision) < Number(snapshot.revision || 0)) return false;
    rememberProcessContext(candidate);
    snapshot = mergeProcessContext(candidate);
    snapshotRevisionFloor = Number(candidate.revision || 0);
    options.onSnapshot(snapshot, { scrollMode });
    return true;
  }

  async function handleEvent(event) {
    const next = eventChain.then(() => applyEventFlow(event));
    // 链本身必须吞掉失败：否则一次抛错会让后续 .then 永不执行，事件从此静默丢弃。
    // 调用方拿到的仍是原始 promise，可以自行观察成败。
    eventChain = next.catch(() => {});
    return next;
  }

  async function applyEventFlow(event) {
    reportDiag({ events: diag.events + 1 });
    const result = applyEvent(snapshot, event, lastEventSeq, snapshotRevisionFloor);
    if (result.error) {
      options.onError(result.error);
      reportApplied();
      return;
    }
    if (result.gap) {
      reportDiag({ gaps: diag.gaps + 1 });
      // delivery_seq 缺口：先向宿主按序号增量补取（C4），补得齐就不必整份重拉。
      if (await replayGap()) {
        reportDiag({ replays: diag.replays + 1 });
        reportApplied();
        return;
      }
      lastEventSeq = result.gapSeq;
    } else {
      lastEventSeq = result.lastSeq;
    }
    if (result.needsRefresh) {
      reportDiag({ refreshes: diag.refreshes + 1 });
      await refresh({ scroll: "auto" });
      reportApplied();
      return;
    }
    snapshot = mergeProcessContext(result.snapshot);
    if (result.changed) {
      options.onIncremental(snapshot, result.changed);
      reportDiag({ incrementals: diag.incrementals + 1 });
      if (result.changed === "message.delta" && bufferedDeltaMissing(event, snapshot)) {
        reportDiag({ buffered: diag.buffered + 1 });
      }
    }
    reportApplied();
  }

  function bufferedDeltaMissing(event, current) {
    const payload = decodePayloadText(event.payload);
    if (!payload || typeof payload.message_id !== "string") return false;
    return !(current.conversation || []).some(message => message.id === payload.message_id);
  }

  function decodePayloadText(payload) {
    if (!payload) return null;
    if (typeof payload === "object") return payload;
    try { return JSON.parse(payload); }
    catch { return null; }
  }

  // replayGap 补取 lastEventSeq 之后缺的事件。返回 false 表示宿主补不齐（窗口
  // 已淘汰、宿主不支持补取或补取途中又不一致），调用方退回权威快照重拉。
  async function replayGap() {
    if (!options.replay || !snapshot) return false;
    let outcome = null;
    try { outcome = await options.replay(lastEventSeq); }
    catch { return false; }
    if (!outcome || !outcome.covered || !Array.isArray(outcome.events)) return false;
    for (const item of outcome.events) {
      const step = applyEvent(snapshot, item, lastEventSeq, snapshotRevisionFloor);
      if (step.error || step.needsRefresh) return false;
      lastEventSeq = step.lastSeq;
      snapshot = mergeProcessContext(step.snapshot);
      if (step.changed) options.onIncremental(snapshot, step.changed);
    }
    return true;
  }

  // 回执告诉宿主"这些序号我已应用"，宿主因此不需要用轮询去猜自己是否漏了事件。
  function reportApplied() { options.onApplied?.(lastEventSeq); }

  return { refresh, handleEvent, acceptSnapshot, current: () => snapshot, appliedSeq: () => lastEventSeq };
}

function requestedScrollMode(options) {
  if (options.scroll === false) return "preserve";
  return typeof options.scroll === "string" ? options.scroll : "auto";
}

function mergeScrollMode(current, next) {
  const priority = { auto: 0, preserve: 1, anchor: 2, bottom: 3 };
  return priority[next] > priority[current] ? next : current;
}
