// 沙箱渲染缓冲：让「内容变了」不再等于「帧重建」。
//
// 问题（`docs/2026-10-19-three-defect-fixes` 方向 B）：`conversation-view.js` 的
// reconcile 只要行的 html 字符串变了就 `elementFromHTML()` + `replaceWith()` 整行替换。
// 行内的 `<iframe sandbox srcdoc>` 必然被销毁重建，新帧加载完成前是空白；流式输出
// 期间 html 每帧都变 ⇒ 每帧重建 ⇒ 连续闪屏。
//
// 本文件把这件事拆成两层，**可测的决策与 DOM 适配分开**：
//
//   1) 纯决策（node:test 可直接覆盖，不需要浏览器）
//      - `shouldDeferMount({streaming, lastChangeAt, now, settleMs})`：现在能不能挂新帧。
//      - `planEmbedMounts(prev, next, opts) -> {adopt, mount, defer, unmount}`：
//        按 (key, srcdoc) 把「旧行里的帧」和「新行要的帧」分成四类处置。
//   2) DOM 适配 `createEmbedFrameBuffer(...)`：
//      领养（把旧帧原样搬进新行）、离屏挂载（load + 一帧 rAF 后再交换）、流式延迟
//      挂载（同高占位）、**加载态状态机**（100ms 首延迟 → 状态文字 + 活动条）、
//      **交叉淡入淡出**（旧帧淡出 150ms、新帧淡入 250ms）、token 防串、并发 pending
//      上限排队、行移除时的清理。
//
// 为什么不再有"双缓冲开关"（2026-10-29）：双缓冲的本意是"换帧不露白"，但它把**加载
// 过程本身**一起缓冲没了——用户在离屏加载期间看到的是一块不动的旧画面，帧换上去的
// 瞬间才"啪"地出现，既没有进度也没有变化感。现在改成两件事同时成立：
//   · 加载态**可见**（宪法 §2.1/§2.2：100ms 内不显示，之后显示状态文字 + 局部活动）；
//   · 交换只做**淡入淡出**（opacity，宪法 §4.5 白名单内），旧帧以 `--duration-fast`
//     退出、新帧以 `--duration-normal` 进入，全程在固定高度的落位台内，零 CLS。
// 只有一条路径，没有"开/关两种行为"的第二套兼容分支（宪法 §9 迁移纪律）。
//
// 安全口径**一个字都没动**：帧的 `sandbox="allow-scripts"`、`referrerpolicy`、CSP 与
// srcdoc 文档全部原样来自 `html-embed.js` 生成的标记。本文件只做搬运与克隆
// （`cloneNode(false)` 会连属性一起复制），不拼 srcdoc、不写 CSP、不新增任何放行。
// 唯一被改的属性是离屏副本上的 `loading`：`loading="lazy"` 在离屏容器里可能永远不
// 触发加载，那样 load 事件不来、帧就永远挂不上——离屏副本必须是 eager。
//
// 触发方（接线）只有 `conversation-view.js` 的 reconcile：它负责「换行前 adopt、
// 换行后 attach、行消失时 release」，缓冲算法一概不在 reconcile 里。

export const EMBED_SETTLE_MS = 150;
export const EMBED_MAX_PENDING = 4;
export const EMBED_LOAD_TIMEOUT_MS = 2500;
// 加载态首延迟：短于它就不显示任何指示器（宪法 §2.2 的 idle → pending → active）。
export const EMBED_LOADING_FIRST_DELAY_MS = 100;
// 旧帧淡出时长，与 `--duration-fast` 同值；CSS 侧用的是 token，这里只用于"到点撤帧"。
export const EMBED_FADE_OUT_MS = 150;

export const EMBED_KEY_ATTR = "data-embed-key";
export const EMBED_STAGE_ATTR = "data-embed-stage";
export const EMBED_LOADING_ATTR = "data-embed-loading";
export const EMBED_FADE_ATTR = "data-embed-fade";
export const EMBED_LOADING_TEXT_ATTR = "data-embed-loading-text";

// 加载态文案：宪法 §2.3 要求先说明"在做什么"，不写"加载中"。两个状态各有说法——
// 流式期间内容还在变，说"等待内容稳定"是事实；真正在装帧才说"正在渲染图形"。
const EMBED_LOADING_TEXT = Object.freeze({
  defer: "等待内容稳定",
  pending: "正在渲染图形",
  active: "正在渲染图形"
});

// shouldDeferMount 判定「这一帧现在能不能挂」。
//   - 会话还在流式：一律不挂（每帧重建才是闪屏的根因，流式期间挂载次数必须是 0）；
//   - 内容刚变过、还没过 settleMs：等它稳住再一次挂（连续变更只挂最后一次）。
// `lastChangeAt` 为 null/undefined（不是数字）表示「这一行没有已知的变更时刻」
// （历史行、刚被领养的稳定行），不算变更、不延迟。**0 是一个合法时刻**，不是"没有"。
export function shouldDeferMount({
  streaming = false,
  lastChangeAt = null,
  now = 0,
  settleMs = EMBED_SETTLE_MS
} = {}) {
  if (streaming) return true;
  if (lastChangeAt === null || lastChangeAt === undefined) return false;
  const changedAt = Number(lastChangeAt);
  if (!Number.isFinite(changedAt)) return false;
  const elapsed = Number(now) - changedAt;
  if (!Number.isFinite(elapsed)) return false;
  const window = Math.max(0, Number(settleMs) || 0);
  return elapsed < window;
}

// planEmbedMounts 是这一整套缓冲的判据本体：纯函数，输入两份「帧清单」
// （`{key, srcdoc}`，key 见 html-embed.js 的 `data-embed-key`），输出四类处置。
//   - adopt   同 key 且 srcdoc 完全一致 → 旧帧原样搬过去，**绝不重新加载**
//             （交互状态、滚动位置自然保留）；
//   - mount   同 key 但内容变了 / 新出现的 key → 新帧离屏加载，load 完再交换；
//   - defer   该挂但此刻不能挂（流式 / settle 窗口内）→ 先放同高占位；
//   - unmount 旧行有、新行没有的 key → 撤销在途挂载、摘监听、卸载 pending 帧。
export function planEmbedMounts(prev = [], next = [], options = {}) {
  const before = new Map();
  for (const item of prev || []) {
    if (!item || item.key === undefined || item.key === null) continue;
    before.set(String(item.key), item);
  }
  const deferMount = shouldDeferMount(options);
  const adopt = [];
  const mount = [];
  const defer = [];
  const unmount = [];
  const wanted = new Set();
  for (const item of next || []) {
    if (!item || item.key === undefined || item.key === null) continue;
    const key = String(item.key);
    wanted.add(key);
    const srcdoc = String(item.srcdoc ?? "");
    const old = before.get(key);
    if (old && String(old.srcdoc ?? "") === srcdoc) {
      adopt.push({ key, srcdoc });
      continue;
    }
    if (deferMount) defer.push({ key, srcdoc });
    else mount.push({ key, srcdoc });
  }
  for (const key of before.keys()) if (!wanted.has(key)) unmount.push(key);
  return { adopt, mount, defer, unmount };
}

// ── DOM 适配层 ────────────────────────────────────────────────────────────
//
// 每行（`[data-conversation-key]`）持有一份状态：最近一次内容变更时刻、每个
// `data-embed-key` 的槽位（mode/token/模板帧）、settle 定时器。行元素本身每次换行
// 都会换新，所以状态按行 key 存、行元素只当"当前落点"更新。
export function createEmbedFrameBuffer(options = {}) {
  const doc = options.document || (typeof globalThis !== "undefined" ? globalThis.document : null);
  const settleMs = Number.isFinite(options.settleMs) ? Number(options.settleMs) : EMBED_SETTLE_MS;
  const maxPending = Math.max(1, Number.isFinite(options.maxPending) ? Math.trunc(options.maxPending) : EMBED_MAX_PENDING);
  const loadingDelayMs = Number.isFinite(options.loadingDelayMs) ? Number(options.loadingDelayMs) : EMBED_LOADING_FIRST_DELAY_MS;
  const fadeOutMs = Number.isFinite(options.fadeOutMs) ? Number(options.fadeOutMs) : EMBED_FADE_OUT_MS;
  const loadTimeoutMs = Number.isFinite(options.loadTimeoutMs) ? Number(options.loadTimeoutMs) : EMBED_LOAD_TIMEOUT_MS;
  const clock = typeof options.now === "function" ? options.now : () => Date.now();
  const schedule = typeof options.schedule === "function" ? options.schedule : (fn, ms) => setTimeout(fn, ms);
  const cancelTimerHandle = typeof options.cancel === "function" ? options.cancel : handle => clearTimeout(handle);
  const nextFrame = typeof options.nextFrame === "function" ? options.nextFrame : fn => {
    if (typeof requestAnimationFrame === "function") requestAnimationFrame(() => fn());
    else schedule(fn, 16);
  };
  const onMounted = typeof options.onMounted === "function" ? options.onMounted : null;

  let mountHost = options.mountRoot || null;
  let streaming = Boolean(options.streaming);
  let sequence = 0;
  const rowStates = new Map();
  const inFlight = new Map();
  const waitQueue = [];
  const anonymousRows = new Map();
  const counters = { adopted: 0, mounted: 0, deferred: 0, unmounted: 0, discarded: 0, queued: 0 };

  function rowKeyOf(row) {
    const raw = row && typeof row.getAttribute === "function" ? row.getAttribute("data-conversation-key") : "";
    const key = String(raw ?? "").trim();
    if (key) return key;
    if (!row) return "";
    if (!anonymousRows.has(row)) anonymousRows.set(row, `anon:${anonymousRows.size + 1}`);
    return anonymousRows.get(row);
  }

  function stateFor(rowKey, row) {
    let state = rowStates.get(rowKey);
    if (!state) {
      state = { rowKey, row: row || null, lastChangeAt: null, slots: new Map(), timer: null };
      rowStates.set(rowKey, state);
    }
    if (row) state.row = row;
    return state;
  }

  function slotEntry(state, key) {
    let entry = state.slots.get(key);
    if (!entry) {
      entry = { key, mode: "absent", srcdoc: "", token: 0, template: null, loadingTimer: null, loadingActive: false };
      state.slots.set(key, entry);
    }
    return entry;
  }

  function stageSlots(row) {
    if (!row || typeof row.querySelectorAll !== "function") return [];
    const stages = row.querySelectorAll(`[${EMBED_STAGE_ATTR}]`);
    const slots = [];
    for (const stage of stages) {
      slots.push({
        key: String(stage.getAttribute(EMBED_KEY_ATTR) ?? ""),
        stage,
        frame: firstFrameIn(stage),
        figure: stage.parentNode || null
      });
    }
    return slots;
  }

  function firstFrameIn(node) {
    if (!node || typeof node.querySelectorAll !== "function") return null;
    const frames = node.querySelectorAll("iframe");
    return frames.length ? frames[0] : null;
  }

  function findSlot(row, key) {
    for (const slot of stageSlots(row)) if (slot.key === key) return slot;
    return null;
  }

  function srcdocOf(frame) {
    if (!frame || typeof frame.getAttribute !== "function") return "";
    return String(frame.getAttribute("srcdoc") ?? "");
  }

  // ── 加载态（宪法 §2：idle → pending → active → settled）─────────────────
  // 状态挂在 `.html-embed-body` 上（这块里**实际变化的**最小区域），CSS 据此决定
  // 要不要露出状态文字与活动条。`pending` 视觉上是空的（前 100ms 不显示指示器），
  // `active` 才可见；落帧即摘掉标记，CSS 把加载层淡出。
  function setLoading(slot, state) {
    const target = slot && (slot.figure || slot.stage);
    if (!target || typeof target.setAttribute !== "function") return;
    if (state) {
      target.setAttribute(EMBED_LOADING_ATTR, state);
      target.setAttribute("aria-busy", "true");
    } else if (typeof target.removeAttribute === "function") {
      target.removeAttribute(EMBED_LOADING_ATTR);
      target.removeAttribute("aria-busy");
    }
    const text = typeof target.querySelector === "function"
      ? target.querySelector(`[${EMBED_LOADING_TEXT_ATTR}]`)
      : null;
    if (text) text.textContent = state ? (EMBED_LOADING_TEXT[state] || "") : "";
  }

  // syncLoading 由"这一槽当下处在什么阶段"推出加载态，避免各处各写一套判断
  // （宪法 §7.3：同一状态只有一套判据）。
  function syncLoading(slot, entry) {
    if (!entry) return;
    if (entry.mode === "settled" || entry.mode === "absent") {
      setLoading(slot, null);
      return;
    }
    if (entry.mode === "defer") {
      // 流式期间只是"内容还没稳定"，不是"在装帧"：给静态文案，不放活动条。
      setLoading(slot, streaming ? "defer" : "pending");
      return;
    }
    setLoading(slot, entry.loadingActive ? "active" : "pending");
  }

  function stopLoadingTimer(entry) {
    if (!entry || !entry.loadingTimer) return;
    cancelTimerHandle(entry.loadingTimer);
    entry.loadingTimer = null;
  }

  // armLoadingTimer：挂载一开始只记 pending（不可见），100ms 后仍未交付才允许露出来。
  // 短于 100ms 的加载因此只有"最终内容"，不会闪一下 loader（宪法 §2.2）。
  function armLoadingTimer(state, key, entry) {
    stopLoadingTimer(entry);
    entry.loadingActive = false;
    syncLoading(findSlot(state.row, key), entry);
    entry.loadingTimer = schedule(() => {
      entry.loadingTimer = null;
      // 已交付或被丢弃（token 清零）：不再露加载层。
      if (!entry.token) return;
      entry.loadingActive = true;
      syncLoading(findSlot(state.row, key), entry);
    }, loadingDelayMs);
  }

  function ensureMountHost() {
    if (mountHost) return mountHost;
    if (!doc || typeof doc.createElement !== "function") return null;
    const host = doc.createElement("div");
    host.setAttribute("class", "html-embed-mount");
    host.setAttribute("aria-hidden", "true");
    const parent = doc.body || doc.documentElement;
    if (!parent || typeof parent.appendChild !== "function") return null;
    parent.appendChild(host);
    mountHost = host;
    return host;
  }

  function clearRowTimer(state) {
    if (!state || !state.timer) return;
    cancelTimerHandle(state.timer);
    state.timer = null;
  }

  // adopt：**换行前**调用。把旧行里内容没变的帧搬进新行（不重新加载）；内容变了的
  // 帧继续占着可视位（新帧 load 完才撤）；该挂但现在不能挂的记成 defer + 同高占位。
  function adopt(prevRow, nextRow, entryOptions = {}) {
    if (!nextRow) return { adopt: [], mount: [], defer: [], unmount: [] };
    const prevStages = stageSlots(prevRow);
    const nextStages = stageSlots(nextRow);
    // 这一行与上一行都没有沙箱帧：不建状态、不分配 key（绝大多数行都是这种）。
    if (!prevStages.length && !nextStages.length) return { adopt: [], mount: [], defer: [], unmount: [] };
    const rowKey = rowKeyOf(nextRow);
    const state = stateFor(rowKey, nextRow);
    const at = clock();
    state.lastChangeAt = at;
    clearRowTimer(state);
    const streamingNow = entryOptions.streaming === undefined ? streaming : Boolean(entryOptions.streaming);
    const prevByKey = new Map(prevStages.map(slot => [slot.key, slot]));
    const prevRecords = prevStages.map(slot => ({
      key: slot.key,
      // 旧行这一槽还没有帧（上一轮在排队/流式占位）：退回上一次记下的内容身份，
      // 否则「占位中的同一内容」会被误判成变化、白等一轮。
      srcdoc: slot.frame ? srcdocOf(slot.frame) : String(state.slots.get(slot.key)?.srcdoc ?? "")
    }));
    const nextRecords = nextStages.map(slot => ({
      key: slot.key,
      srcdoc: slot.frame ? srcdocOf(slot.frame) : ""
    }));
    const plan = planEmbedMounts(prevRecords, nextRecords, {
      streaming: streamingNow,
      lastChangeAt: at,
      now: at,
      settleMs
    });
    const adoptKeys = new Set(plan.adopt.map(item => item.key));
    const deferKeys = new Set(plan.defer.map(item => item.key));
    for (const slot of nextStages) {
      const entry = slotEntry(state, slot.key);
      const template = slot.frame;
      const content = template ? srcdocOf(template) : "";
      if (adoptKeys.has(slot.key)) {
        const previous = prevByKey.get(slot.key);
        if (previous && previous.frame) {
          // 同 key 同 srcdoc：旧帧整枚搬进新行。搬移只动 DOM 归属，不碰 srcdoc。
          slot.stage.replaceChildren(previous.frame);
          entry.mode = "settled";
          entry.token = 0;
          entry.template = null;
          entry.srcdoc = content;
          counters.adopted += 1;
          syncLoading(slot, entry);
          continue;
        }
        // 旧行这个槽还是占位（还没挂上过）：内容没变，在途的挂载继续用（token 保留），
        // 新行继续占位；没有在途帧就交回延迟队列，等 settle 之后再挂。
        if (template) {
          slot.stage.replaceChildren();
          entry.srcdoc = content;
          entry.template = template;
          if (!entry.token) entry.mode = "defer";
          syncLoading(slot, entry);
        } else {
          entry.srcdoc = content;
          syncLoading(slot, entry);
        }
        continue;
      }
      // 内容变了 / 新出现：先把解析出来的帧从树上摘掉（它一进文档就会自己开始加载，
      // 那正是"新帧还没准备好就露白"的来源），留作离屏克隆的模板。
      slot.stage.replaceChildren();
      const previous = prevByKey.get(slot.key);
      if (previous && previous.frame) {
        // 旧帧留在原位顶上：新帧 load 完成前用户看到的仍是上一次的画面，不塌陷、不露白。
        slot.stage.appendChild(previous.frame);
      }
      // 在途的那一枚帧如果已经不代表这一版内容了，立刻作废（token 清零 → 新帧重挂，
      // 老帧 load 回来时因 token 过期被丢弃，绝不上屏）。
      const pendingRecord = entry.token ? inFlight.get(entry.token) : null;
      if (pendingRecord && srcdocOf(pendingRecord.frame) !== content) {
        discard(pendingRecord);
        entry.token = 0;
      }
      entry.srcdoc = content;
      entry.template = template || null;
      if (deferKeys.has(slot.key)) {
        if (!entry.token) entry.mode = "defer";
        counters.deferred += 1;
      } else if (!entry.token) {
        entry.mode = "mount";
      }
      syncLoading(slot, entry);
    }
    for (const key of plan.unmount) {
      cancelSlot(state, key);
      counters.unmounted += 1;
    }
    return plan;
  }

  // attach：**换行后**（行进文档）调用。此刻才真正发起离屏挂载；仍在流式或还在
  // settle 窗口里就只留占位，并挂一个 settle 定时器等它稳住。
  function attach(row, entryOptions = {}) {
    if (!row) return;
    if (entryOptions.streaming !== undefined) setStreaming(Boolean(entryOptions.streaming));
    const rowKey = rowKeyOf(row);
    const state = rowStates.get(rowKey);
    if (!state) return;
    state.row = row;
    const at = clock();
    const deferNow = shouldDeferMount({ streaming, lastChangeAt: state.lastChangeAt, now: at, settleMs });
    for (const [key, entry] of [...state.slots]) {
      if (entry.mode !== "mount" && entry.mode !== "defer") continue;
      if (deferNow) {
        entry.mode = "defer";
        syncLoading(findSlot(row, key), entry);
        continue;
      }
      entry.mode = "mount";
      startMount(state, key, entry);
    }
    armDeferTimer(state);
  }

  function armDeferTimer(state) {
    clearRowTimer(state);
    if (!state || streaming) return;
    let waiting = false;
    for (const entry of state.slots.values()) if (entry.mode === "defer") waiting = true;
    if (!waiting) return;
    const elapsed = state.lastChangeAt === null ? settleMs : clock() - Number(state.lastChangeAt);
    const remaining = Math.max(0, settleMs - elapsed);
    state.timer = schedule(() => {
      state.timer = null;
      drainDeferred(state.rowKey);
    }, remaining);
  }

  function drainDeferred(rowKey) {
    const state = rowStates.get(rowKey);
    if (!state) return;
    const at = clock();
    if (shouldDeferMount({ streaming, lastChangeAt: state.lastChangeAt, now: at, settleMs })) {
      armDeferTimer(state);
      return;
    }
    for (const [key, entry] of [...state.slots]) {
      if (entry.mode === "mount") startMount(state, key, entry);
      else if (entry.mode === "defer") {
        entry.mode = "mount";
        startMount(state, key, entry);
      }
    }
  }

  function startMount(state, key, entry) {
    if (entry.token) return;
    if (inFlight.size >= maxPending) {
      entry.mode = "queued";
      waitQueue.push({ rowKey: state.rowKey, key });
      counters.queued += 1;
      // 排队期间只是"还没轮到"，先记 pending（不可见），轮到再起 100ms 计时。
      syncLoading(findSlot(state.row, key), entry);
      return;
    }
    const frame = cloneFrame(entry.template);
    if (!frame) {
      entry.mode = "defer";
      syncLoading(findSlot(state.row, key), entry);
      return;
    }
    if (typeof frame.removeAttribute === "function") frame.removeAttribute("loading");
    const token = sequence + 1;
    sequence = token;
    entry.token = token;
    entry.mode = "mount";
    const record = { token, rowKey: state.rowKey, key, frame, onLoad: null, timer: null };
    const onLoad = () => handleLoad(record);
    record.onLoad = onLoad;
    inFlight.set(token, record);
    if (typeof frame.addEventListener === "function") frame.addEventListener("load", onLoad);
    const host = ensureMountHost();
    if (!host) {
      // 没有可用的离屏容器（非浏览器环境）：不挂、不留假状态。
      entry.token = 0;
      entry.mode = "defer";
      inFlight.delete(token);
      syncLoading(findSlot(state.row, key), entry);
      return;
    }
    // 插进离屏容器这一刻才开始解析 srcdoc；load 之前它一个像素都没进可视区。
    host.appendChild(frame);
    armLoadingTimer(state, key, entry);
    record.timer = schedule(() => commit(record, true), loadTimeoutMs);
  }

  function cloneFrame(template) {
    if (template && typeof template.cloneNode === "function") {
      const copy = template.cloneNode(false);
      if (copy && copy !== template) return copy;
    }
    if (!template || !doc || typeof doc.createElement !== "function") return null;
    const frame = doc.createElement("iframe");
    if (typeof frame.setAttribute === "function") {
      frame.setAttribute("class", String(template.getAttribute("class") ?? "html-embed-frame"));
      const sandbox = template.getAttribute("sandbox");
      if (sandbox !== null) frame.setAttribute("sandbox", sandbox);
      frame.setAttribute("srcdoc", srcdocOf(template));
    }
    return frame;
  }

  function handleLoad(record) {
    if (!record || !inFlight.has(record.token)) return;
    // load 之后等一帧：让帧内排版落定，交换时不出现"半张图"。
    nextFrame(() => commit(record, false));
  }

  function commit(record, forced) {
    if (!record || !inFlight.has(record.token)) return;
    const state = rowStates.get(record.rowKey);
    const entry = state && state.slots.get(record.key);
    const slot = state ? findSlot(state.row, record.key) : null;
    // token 过期（期间又变了一次 / 行被移除 / key 消失）：丢弃这枚帧，绝不上屏。
    if (!state || !entry || entry.token !== record.token || !slot) {
      discard(record);
      return;
    }
    stopRecordTimer(record);
    if (record.onLoad && typeof record.frame.removeEventListener === "function") {
      record.frame.removeEventListener("load", record.onLoad);
    }
    inFlight.delete(record.token);
    stopLoadingTimer(entry);
    entry.loadingActive = false;
    entry.token = 0;
    entry.mode = "settled";
    entry.template = null;
    const previous = firstFrameIn(slot.stage);
    // 交叉淡入淡出（唯一一条路径）：新帧以 opacity:0 叠上去，下一帧放到 1；旧帧同时
    // 淡出，过渡结束才撤。落位台高度固定，两层叠着也不产生任何布局变化（零 CLS）。
    record.frame.setAttribute(EMBED_FADE_ATTR, "in");
    slot.stage.appendChild(record.frame);
    if (previous && previous !== record.frame) previous.setAttribute(EMBED_FADE_ATTR, "out");
    nextFrame(() => {
      // 等一帧再切到终态，浏览器才有"初始 opacity:0"可过渡（同一帧内改动不会触发动画）。
      if (typeof record.frame.setAttribute === "function") record.frame.setAttribute(EMBED_FADE_ATTR, "settled");
    });
    if (previous && previous !== record.frame) {
      schedule(() => {
        if (previous.parentNode === slot.stage && typeof previous.remove === "function") previous.remove();
      }, fadeOutMs);
    }
    // 加载层在这里退场（CSS 淡出），与帧的淡入同时进行。
    setLoading(slot, null);
    counters.mounted += 1;
    // 超时兜底（load 没来也先把帧放上去）：记一笔，方便诊断"帧挂上了但内容迟"。
    if (forced) counters.timedOut = (counters.timedOut || 0) + 1;
    drainQueue();
    if (onMounted) onMounted({ rowKey: record.rowKey, key: record.key, forced });
  }

  function discard(record) {
    if (!record) return;
    stopRecordTimer(record);
    if (record.onLoad && record.frame && typeof record.frame.removeEventListener === "function") {
      record.frame.removeEventListener("load", record.onLoad);
    }
    if (record.frame && typeof record.frame.remove === "function") record.frame.remove();
    if (inFlight.delete(record.token)) counters.discarded += 1;
    drainQueue();
  }

  function stopRecordTimer(record) {
    if (!record || !record.timer) return;
    cancelTimerHandle(record.timer);
    record.timer = null;
  }

  function drainQueue() {
    while (waitQueue.length && inFlight.size < maxPending) {
      const next = waitQueue.shift();
      const state = rowStates.get(next.rowKey);
      const entry = state && state.slots.get(next.key);
      if (!state || !entry || entry.mode !== "queued") continue;
      entry.mode = "mount";
      startMount(state, next.key, entry);
    }
  }

  function cancelSlot(state, key) {
    const entry = state && state.slots.get(key);
    if (!entry) return;
    const token = entry.token;
    stopLoadingTimer(entry);
    entry.token = 0;
    entry.mode = "absent";
    state.slots.delete(key);
    if (token) {
      const record = inFlight.get(token);
      if (record) discard(record);
    }
    if (entry.template && typeof entry.template.remove === "function") entry.template.remove();
    entry.template = null;
  }

  // release：行被移出对话流（换会话 / 历史窗口回退）时收尾：撤在途挂载、停定时器、
  // 摘监听。行元素已经被新元素接管的（换行场景）不在这里动，交给 adopt。
  function release(row) {
    if (!row) return;
    const rowKey = rowKeyOf(row);
    const state = rowStates.get(rowKey);
    if (!state) return;
    if (state.row && state.row !== row) return;
    clearRowTimer(state);
    for (const key of [...state.slots.keys()]) cancelSlot(state, key);
    rowStates.delete(rowKey);
  }

  function setStreaming(next) {
    const value = Boolean(next);
    if (value === streaming) return;
    streaming = value;
    if (!streaming) for (const rowKey of [...rowStates.keys()]) drainDeferred(rowKey);
  }

  return {
    // 接线用的三个动作
    adopt,
    attach,
    release,
    // 宿主/测试用
    setStreaming,
    streaming: () => streaming,
    stats: () => ({
      ...counters,
      inFlight: inFlight.size,
      queued: waitQueue.length,
      rows: rowStates.size
    }),
    // 便于诊断：当前所有在途/排队的帧 key
    pendingKeys: () => [...inFlight.values()].map(record => `${record.rowKey}::${record.key}`),
    mountRoot: () => mountHost
  };
}
