import { applyEvent, validateSnapshot } from "./protocol.js";

export function createGUIClient(options) {
  let snapshot = null;
  let snapshotRevisionFloor = 0;
  let lastEventSeq = 0;
  let refreshPromise = null;
  let refreshQueued = false;
  let refreshScroll = "auto";
  // 事件应用串行化：缺口补取与快照重拉都是异步的，两条事件并发落地会把
  // lastEventSeq 与 snapshot 交叉写坏。
  let eventChain = Promise.resolve();

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
    snapshot = candidate;
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
    const result = applyEvent(snapshot, event, lastEventSeq, snapshotRevisionFloor);
    if (result.error) {
      options.onError(result.error);
      reportApplied();
      return;
    }
    if (result.gap) {
      // delivery_seq 缺口：先向宿主按序号增量补取（C4），补得齐就不必整份重拉。
      if (await replayGap()) {
        reportApplied();
        return;
      }
      lastEventSeq = result.gapSeq;
    } else {
      lastEventSeq = result.lastSeq;
    }
    if (result.needsRefresh) {
      await refresh({ scroll: "auto" });
      reportApplied();
      return;
    }
    snapshot = result.snapshot;
    if (result.changed) options.onIncremental(snapshot, result.changed);
    reportApplied();
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
      snapshot = step.snapshot;
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
