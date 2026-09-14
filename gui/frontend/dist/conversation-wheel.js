// 对话区右侧「会话内全量用户输入索引」（2026-09-11 SA-D 重做，取代原
// 「对话导航轮轴」）。
//
// 设计要点：
//   1. **刻度 = 用户输入**：一条刻度 = 会话里的一条用户输入。助手步骤 /
//      思考 / 工具 / 系统行都不再产生刻度；窗口里没有 user 轮时也不退回
//      助手步骤——没有用户输入就没有刻度（轨道隐藏）。
//   2. **全量**：刻度集合来自后端全量索引（SessionInputIndex，含尚未加载
//      到前端的早期轮次）。已加载轮次用真实几何（offsetTop 占内容高度的
//      比例）布点；未加载轮次在相邻已加载刻度（含 (0,0) / (N+1,1) 两端
//      锚点）之间按确定性比例线性插值——整条轨道因此代表整个会话，而不是
//      当前窗口里碰巧有几条输入。
//   3. **点击即定位**：目标在已加载窗口 → 滚动 + 高亮；不在窗口 → 先按页
//      回读（宿主注入的 locateInput）再滚动 + 高亮。定位路径由纯函数
//      planInputLocate 决策，locateInput 执行（两者都可脱离 DOM 单测）。
//   4. 视觉沿用既有轮轴语言：短线 + 1px 基线，当前输入用主信号色加长；未
//      加载刻度降低不透明度（内联 style，自带样式表里没有该态）。悬停显示
//      问题摘要；键盘 ↑/↓ 在输入刻度间移动、PgUp/PgDn 跨 5 条、Home/End 到
//      首/末条、Enter/Space 跳到当前条（只作用于用户输入刻度）。
//
// 纯函数（normalizeInputIndex / inWindowInput / linkInputRows / inputDashes /
// inputAtOffset / activeInputIndex / planInputLocate / scrollTopForFraction /
// wheelSignature）可脱离 DOM 单测，见 conversation-wheel.test.mjs。

const DASH = 3;
const MIN_GAP = 11;
const ACTIVE_LINE = 0.4;
// PAGE_JUMP 是键盘 PgUp/PgDn 一次跨过的输入刻度数。
const PAGE_JUMP = 5;
// MAX_READ_BACK_PAGES 是单次点击允许回读的页数上限（防御性：不无限翻页）。
const MAX_READ_BACK_PAGES = 40;
// HIGHLIGHT_MS 是定位后高亮的持续时间（与 .is-wheel-target 动画时长一致）。
const HIGHLIGHT_MS = 1400;
// KEY_PREFIX 与 components.js 的 data-conversation-key 约定同源。
const KEY_PREFIX = "message:";

// isUserWheelKind 判定 DOM 行/数据项的类别是否为用户输入（其余类别一律不成
// 刻度：多类别刻度语义已下线）。
export function isUserWheelKind(raw) {
  return String(raw ?? "").trim().toLowerCase() === "user";
}

// normalizeInputIndex 归一化后端 SessionInputIndex 载荷（也接受裸 items 数组）：
// 字段类型收敛 + 只保留合法用户输入刻度 + 按轮次序号升序。返回
// { items, window }，window 为空表示后端没给窗口信息（全部按未加载处理）。
export function normalizeInputIndex(payload) {
  const source = Array.isArray(payload) ? payload : (Array.isArray(payload?.items) ? payload.items : []);
  const window = payload && !Array.isArray(payload) ? normalizeIndexWindow(payload.window) : null;
  const items = [];
  for (const [position, raw] of source.entries()) {
    if (!raw || typeof raw !== "object") continue;
    const round = toNumber(raw.round, position + 1);
    if (!Number.isFinite(round) || round <= 0) continue;
    items.push({
      round,
      messageID: typeof raw.message_id === "string" ? raw.message_id : "",
      offset: toNumber(raw.offset, -1),
      roundID: toNumber(raw.round_id, 0),
      seq: toNumber(raw.seq, 0),
      summary: clampSummary(raw.summary),
      chars: toNumber(raw.chars, 0),
      loaded: Boolean(raw.loaded)
    });
  }
  items.sort((left, right) => left.round - right.round);
  return { items, window };
}

function normalizeIndexWindow(raw) {
  if (!raw || typeof raw !== "object") return null;
  return {
    offset: toNumber(raw.offset, 0),
    count: toNumber(raw.count, 0),
    total: toNumber(raw.total, 0),
    hasMore: Boolean(raw.has_more),
    windowSize: toNumber(raw.window_size, 0)
  };
}

// inWindowInput 判定一条索引项是否落在「当前已加载窗口」：优先用随索引下发的
// 窗口偏移区间（比逐项 loaded 标志更不易过期），缺省退回逐项 loaded。
export function inWindowInput(input, window) {
  if (!input) return false;
  if (window && Number.isFinite(window.offset) && Number.isFinite(window.count) &&
      Number.isFinite(input.offset) && input.offset >= 0) {
    return input.offset >= window.offset && input.offset < window.offset + window.count;
  }
  return Boolean(input.loaded);
}

// normalizeDomUserRows 只保留 DOM 测量结果里的用户输入行（其余类别不成刻度）。
export function normalizeDomUserRows(rows) {
  const list = Array.isArray(rows) ? rows : [];
  return list
    .filter(row => row && row.key && isUserWheelKind(row.kind))
    .map(row => ({
      key: String(row.key),
      label: typeof row.label === "string" ? row.label : "",
      offsetTop: Math.max(toNumber(row.offsetTop, 0), 0),
      height: Math.max(toNumber(row.height, 0), 0)
    }));
}

// linkInputRows 把全量索引项与「窗口内真实存在的用户行」配对：
//   1) message_id → data-conversation-key（`message:<id>`）精确匹配：生产路径，
//      ID 稳定且与窗口滑动无关；
//   2) 剩余项按顺序补配（仅当剩余项数与剩余行数相等时——避免错位匹配）。
// 返回 round → row（未命中的刻度按插值布点，并在点击时走回读）。
export function linkInputRows(inputs, rows, window) {
  const list = Array.isArray(inputs) ? inputs : [];
  const domRows = Array.isArray(rows) ? rows : [];
  const links = new Map();
  const consumed = new Set();
  for (const input of list) {
    if (!input.messageID) continue;
    const key = `${KEY_PREFIX}${input.messageID}`;
    const row = domRows.find(item => item.key === key && !consumed.has(item.key));
    if (!row) continue;
    links.set(input.round, row);
    consumed.add(row.key);
  }
  const pending = list.filter(input => !links.has(input.round) && inWindowInput(input, window));
  const rest = domRows.filter(row => !consumed.has(row.key));
  if (pending.length > 0 && pending.length === rest.length) {
    for (let index = 0; index < pending.length; index += 1) links.set(pending[index].round, rest[index]);
  }
  return links;
}

// inputDashes 生成刻度表（纯函数）：
//   - 已加载刻度 = 真实几何（offsetTop / scrollHeight）；
//   - 未加载刻度 = 相邻已加载刻度之间的确定性线性插值；
//   - 之后做最小间距收敛，保证密集刻度仍可点击、且不溢出轨道。
export function inputDashes(inputs, rows, geometry = {}) {
  const list = Array.isArray(inputs) ? inputs : [];
  const scrollHeight = Math.max(toNumber(geometry.scrollHeight, 0), 0);
  const trackHeight = Math.max(toNumber(geometry.trackHeight, 0), 0);
  const dash = Math.max(toNumber(geometry.dashHeight, DASH), 1);
  const minGap = Math.max(toNumber(geometry.minGap, MIN_GAP), 0);
  const empty = { empty: true, rounds: [], scrollHeight, trackHeight, dashHeight: dash, loaded: 0, total: list.length };
  if (list.length === 0 || scrollHeight <= 0 || trackHeight <= 0) return empty;

  const domRows = normalizeDomUserRows(rows);
  const window = geometry.window || null;
  const links = linkInputRows(list, domRows, window);
  const progress = progressKnots(list, links, scrollHeight);
  const available = Math.max(trackHeight - dash, 0);
  const gap = list.length > 1 ? Math.min(minGap, available / (list.length - 1)) : 0;
  const tops = [];
  let previous = Number.NEGATIVE_INFINITY;
  for (const input of list) {
    const proportional = clamp01(progress.get(input.round) ?? 0) * available;
    const top = Math.max(proportional, previous + gap);
    tops.push(top);
    previous = top;
  }
  // 顶到轨道底时整体回拉（自后向前收），保持相对顺序与间距。
  let next = Number.POSITIVE_INFINITY;
  for (let index = tops.length - 1; index >= 0; index -= 1) {
    tops[index] = Math.min(tops[index], next - gap);
    next = tops[index];
  }
  if (tops.length > 0 && tops[0] < 0) {
    const shift = -tops[0];
    for (let index = 0; index < tops.length; index += 1) tops[index] += shift;
  }

  const rounds = list.map((input, index) => {
    const row = links.get(input.round) || null;
    const value = clamp01(progress.get(input.round) ?? 0);
    return {
      round: input.round,
      loaded: Boolean(row),
      key: row ? row.key : "",
      messageID: input.messageID,
      summary: input.summary || (row && row.label) || `第 ${input.round} 条输入`,
      offset: input.offset,
      top: round2(clamp(tops[index], 0, available)),
      height: round2(dash),
      // anchorTop 是「高亮当前输入」用的内容坐标：已加载取真实 offsetTop，
      // 未加载取插值比例换算的估算位置（比 0 更接近真实阅读位置）。
      anchorTop: round2(row ? row.offsetTop : value * scrollHeight),
      progress: value
    };
  });
  return {
    empty: rounds.length === 0,
    rounds,
    scrollHeight,
    trackHeight,
    dashHeight: dash,
    window,
    loaded: rounds.filter(item => item.loaded).length,
    total: rounds.length
  };
}

// progressKnots 计算每条输入在内容里的比例位置：已加载项是真实锚点，未加载项
// 在相邻锚点（含 0 与 N+1 两端）之间线性插值（确定性：同一输入/几何必得同一
// 位置，不随滚动或渲染次数变化）。
function progressKnots(inputs, links, scrollHeight) {
  const knots = [{ round: 0, progress: 0 }];
  for (const input of inputs) {
    const row = links.get(input.round);
    if (row) knots.push({ round: input.round, progress: clamp01(row.offsetTop / scrollHeight) });
  }
  knots.push({ round: inputs[inputs.length - 1].round + 1, progress: 1 });
  for (let index = 1; index < knots.length; index += 1) {
    knots[index].progress = Math.max(knots[index].progress, knots[index - 1].progress);
  }
  const result = new Map();
  let cursor = 0;
  for (const input of inputs) {
    while (cursor + 1 < knots.length && knots[cursor + 1].round <= input.round) cursor += 1;
    const start = knots[cursor];
    const end = knots[Math.min(cursor + 1, knots.length - 1)];
    let value = start.progress;
    if (input.round > start.round && end.round > start.round) {
      const ratio = (input.round - start.round) / (end.round - start.round);
      value = start.progress + (end.progress - start.progress) * ratio;
    }
    result.set(input.round, clamp01(value));
  }
  return result;
}

// inputAtOffset 返回轨道 y 处命中的刻度：优先命中，其次最近的一条（3px 的线
// 也要点得中）。
export function inputAtOffset(rounds, y) {
  const list = Array.isArray(rounds) ? rounds : [];
  if (list.length === 0) return null;
  const offset = Number(y) || 0;
  let nearest = null;
  let best = Infinity;
  for (const round of list) {
    if (offset >= round.top && offset <= round.top + round.height) return round;
    const distance = Math.min(Math.abs(offset - round.top), Math.abs(offset - (round.top + round.height)));
    if (distance < best) {
      best = distance;
      nearest = round;
    }
  }
  return nearest;
}

// activeInputIndex 返回当前输入刻度的下标：视口参考线（视口 40% 处）落在哪条
// 输入之后，就高亮哪一条；还没滚过第一条时给 0。
export function activeInputIndex(rounds, geometry = {}) {
  const list = Array.isArray(rounds) ? rounds : [];
  if (list.length === 0) return -1;
  const reference = (Number(geometry.scrollTop) || 0) + Math.max(Number(geometry.clientHeight) || 0, 0) * ACTIVE_LINE;
  let active = 0;
  for (let index = 0; index < list.length; index += 1) {
    if ((Number(list[index].anchorTop) || 0) <= reference) active = index;
    else break;
  }
  return active;
}

// planInputLocate 是「点击第 round 条输入端刻度」的定位决策（纯函数）：
//   - 目标不在索引里 → missing；
//   - 目标已加载（或 DOM 已有）→ scroll；
//   - 否则 → read-back（先回读 pages 页，再定位）。
// windowSize/windowOffset 来自索引载荷（一页消息数 / 窗口起始偏移），用它把
// 「目标与窗口的偏移差」换算成回读页数。
export function planInputLocate(rounds, round, options = {}) {
  const list = Array.isArray(rounds) ? rounds : [];
  const wanted = toNumber(round, 0);
  const index = list.findIndex(item => item.round === wanted);
  if (index < 0) return { action: "missing", round: wanted, index: -1, pages: 0 };
  const target = list[index];
  const base = {
    round: target.round,
    index,
    key: target.key || "",
    messageID: target.messageID || "",
    summary: target.summary || "",
    offset: target.offset
  };
  if (target.loaded) return { ...base, action: "scroll", pages: 0 };
  const maxPages = Math.max(toNumber(options.maxPages, MAX_READ_BACK_PAGES), 1);
  const windowSize = Math.max(toNumber(options.windowSize, 0), 1);
  const windowOffset = toNumber(options.windowOffset, NaN);
  const targetOffset = toNumber(target.offset, -1);
  let pages = 1;
  if (Number.isFinite(windowOffset) && targetOffset >= 0 && windowOffset > targetOffset) {
    pages = Math.min(Math.ceil((windowOffset - targetOffset) / windowSize), maxPages);
  }
  return { ...base, action: "read-back", pages, windowSize };
}

// locateInput 执行定位决策（可注入依赖，便于单测）：
//   - DOM 里已经有目标节点 → 直接滚动 + 高亮（比回读更准更快）；
//   - 否则按决策回读：每读一页就重新查一次节点，命中即定位；
//   - 回读通道未装配 / 页数用尽仍未命中 → false（调用方保持原视图不动）。
export async function locateInput(decision, deps = {}) {
  if (!decision || decision.action === "missing") return false;
  const findNode = typeof deps.findNode === "function" ? deps.findNode : () => null;
  const present = findNode(decision);
  if (present) {
    scrollToNode(present, deps);
    return true;
  }
  if (decision.action !== "read-back" || typeof deps.loadPage !== "function") return false;
  const pages = Math.max(toNumber(decision.pages, 0), 1);
  for (let page = 0; page < pages; page += 1) {
    const progressed = await deps.loadPage(page, decision);
    const node = findNode(decision);
    if (node) {
      scrollToNode(node, deps);
      return true;
    }
    if (progressed === false) break; // 没有更早的页可读：不再空翻
  }
  return false;
}

function scrollToNode(node, deps) {
  const behavior = deps.smooth === false ? "auto" : "smooth";
  if (typeof deps.scrollTo === "function") deps.scrollTo(node, behavior);
  else if (typeof node.scrollIntoView === "function") node.scrollIntoView({ behavior, block: "start" });
  node.classList?.add("is-wheel-target");
  const schedule = typeof deps.schedule === "function" ? deps.schedule : (fn, ms) => setTimeout(fn, ms);
  schedule(() => node.classList?.remove("is-wheel-target"), Math.max(toNumber(deps.highlightMs, HIGHLIGHT_MS), 0));
}

// scrollTopForFraction 由轨道比例反解滚动位置（点击轨道空白处/拖拽用）。
export function scrollTopForFraction(fraction, geometry = {}) {
  const maxScroll = Math.max((Number(geometry.scrollHeight) || 0) - (Number(geometry.clientHeight) || 0), 0);
  const value = clamp01(Number(fraction) || 0);
  return value * maxScroll;
}

// wheelSignature 是刻度表的几何指纹：内容没变就不重建 DOM（避免滚动/流式
// 期间反复 reflow）。
export function wheelSignature(rounds) {
  const list = Array.isArray(rounds) ? rounds : [];
  return list.map(round => `${round.round}:${round.top}:${round.loaded ? 1 : 0}:${round.summary}`).join("|");
}

function clampSummary(value) {
  const flat = String(value ?? "").replace(/\s+/g, " ").trim();
  return flat.length > 160 ? `${flat.slice(0, 160)}…` : flat;
}

function clamp01(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return 0;
  return Math.min(Math.max(number, 0), 1);
}

function clamp(value, min, max) {
  const number = Number(value);
  if (!Number.isFinite(number)) return min;
  return Math.min(Math.max(number, min), max);
}

function toNumber(value, fallback) {
  const number = Number(value);
  return Number.isFinite(number) ? number : fallback;
}

function round2(value) {
  return Math.round(value * 100) / 100;
}

function escapeKey(key) {
  const text = String(key);
  if (typeof CSS !== "undefined" && typeof CSS.escape === "function") return CSS.escape(text);
  return text.replace(/["\\]/g, "\\$&");
}

// createConversationWheel 绑定到滚动容器：轨道挂在容器的父元素（对话外壳）上，
// 刻度位置来自容器当前子项的真实测量——加载更早历史后无需显式刷新调用。
//
// options.locateInput(item, decision) 是宿主的「回读那一页」通道（返回是否已
// 把目标拉进窗口）；未装配时点击未加载刻度只做提示，不静默空转。
export function createConversationWheel(container, options = {}) {
  if (!container) throw new Error("conversation wheel requires a scroll container");
  const parent = container.parentElement || container;
  const rail = document.createElement("section");
  rail.className = "conversation-wheel is-empty";
  rail.setAttribute("aria-label", "会话内用户输入索引：点击刻度跳到对应输入");
  const track = document.createElement("div");
  track.className = "wheel-track";
  track.setAttribute("role", "scrollbar");
  track.setAttribute("aria-orientation", "vertical");
  track.setAttribute("aria-valuemin", "0");
  track.setAttribute("aria-valuemax", "100");
  track.tabIndex = 0;
  const tip = document.createElement("div");
  tip.className = "wheel-tip";
  tip.setAttribute("aria-hidden", "true");
  track.appendChild(tip);
  rail.appendChild(track);
  parent.appendChild(rail);

  const dashNodes = new Map();
  let signature = "";
  let rounds = [];
  let active = -1;
  let dragging = null;
  let moved = false;
  let locating = false;
  // indexReady = 已拿到后端全量索引（即使为空，也以后端为准）；未就绪时用
  // 窗口内的用户行临时成表（保证"只索引用户输入"在接线前也成立）。
  let indexReady = false;
  let wheelIndex = { items: [], window: null };

  function geometry() {
    return {
      scrollHeight: container.scrollHeight,
      clientHeight: container.clientHeight,
      scrollTop: container.scrollTop,
      trackHeight: track.getBoundingClientRect().height
    };
  }

  // rowsFromDOM 从容器直接子项测量几何：样式/内容/换行任何变化都以实际
  // offsetTop/height 为准，而不是靠模型里的估算值。
  function rowsFromDOM() {
    const containerTop = container.getBoundingClientRect().top - container.scrollTop;
    const rows = [];
    for (const node of container.children) {
      if (!node.dataset || !node.dataset.conversationKey) continue;
      const rect = node.getBoundingClientRect();
      if (rect.height <= 0 && rect.width <= 0) continue;
      rows.push({
        key: node.dataset.conversationKey,
        kind: node.dataset.wheelKind || "",
        label: node.dataset.wheelLabel || "",
        offsetTop: rect.top - containerTop,
        height: rect.height
      });
    }
    return rows;
  }

  // domFallbackInputs 在还没拿到后端索引时，把窗口内的用户行临时当作刻度
  // （round = 窗口内序号）：只索引用户输入，不会退回助手步骤。
  function domFallbackInputs() {
    return normalizeDomUserRows(rowsFromDOM()).map((row, index) => ({
      round: index + 1,
      messageID: row.key.startsWith(KEY_PREFIX) ? row.key.slice(KEY_PREFIX.length) : "",
      offset: -1,
      roundID: 0,
      seq: 0,
      summary: clampSummary(row.label),
      chars: 0,
      loaded: true
    }));
  }

  function windowSize() {
    const size = toNumber(wheelIndex.window?.windowSize, 0);
    if (size > 0) return size;
    const count = toNumber(wheelIndex.window?.count, 0);
    return count > 0 ? count : 1;
  }

  function windowOffset() {
    const window = wheelIndex.window;
    if (!window) return NaN;
    if (Number.isFinite(window.offset)) return window.offset;
    return Math.max(toNumber(window.total, 0) - toNumber(window.count, 0), 0);
  }

  function renderRounds(nextRounds) {
    const next = wheelSignature(nextRounds);
    if (next === signature) return;
    signature = next;
    const desired = new Set();
    for (const round of nextRounds) {
      const id = `input:${round.round}`;
      desired.add(id);
      let node = dashNodes.get(id);
      if (!node) {
        node = document.createElement("button");
        node.type = "button";
        node.className = "wheel-dash";
        node.dataset.wheelNode = id;
        node.addEventListener("click", event => {
          event.preventDefault();
          void jumpToRound(round.round);
        });
        dashNodes.set(id, node);
      }
      node.className = round.loaded ? "wheel-dash is-user" : "wheel-dash is-user is-pending";
      node.style.top = `${round.top}px`;
      // 未加载刻度压暗：样式表没有该态（不改样式表），用内联不透明度表达。
      node.style.opacity = round.loaded ? "" : "0.5";
      node.title = round.summary;
      node.setAttribute("aria-label", round.loaded
        ? `第 ${round.round} 条输入：${round.summary}`
        : `第 ${round.round} 条输入（未加载，点击回读）：${round.summary}`);
    }
    for (const [id, node] of [...dashNodes]) {
      if (desired.has(id)) continue;
      node.remove();
      dashNodes.delete(id);
    }
    for (const round of nextRounds) {
      const node = dashNodes.get(`input:${round.round}`);
      if (node) track.appendChild(node);
    }
    // tip 始终在最后，避免被刻度覆盖。
    track.appendChild(tip);
  }

  // updateViewport 只更新「当前输入」高亮：滚动路径（每帧触发）不重建刻度。
  function updateViewport() {
    const box = geometry();
    const next = activeInputIndex(rounds, box);
    if (next !== active) {
      const previousNode = active >= 0 ? dashNodes.get(`input:${rounds[active]?.round}`) : null;
      if (previousNode) previousNode.classList.remove("is-active");
      active = next;
      const node = active >= 0 ? dashNodes.get(`input:${rounds[active]?.round}`) : null;
      if (node) node.classList.add("is-active");
    }
    const percent = box.scrollHeight > box.clientHeight
      ? Math.round((box.scrollTop / (box.scrollHeight - box.clientHeight)) * 100)
      : 100;
    track.setAttribute("aria-valuenow", String(Math.min(Math.max(percent, 0), 100)));
    track.setAttribute("aria-valuetext", rounds.length > 0
      ? `第 ${Math.max(active, 0) + 1} 条输入，共 ${rounds.length} 条`
      : "暂无用户输入");
  }

  function refresh() {
    const box = geometry();
    const inputs = indexReady ? wheelIndex.items : domFallbackInputs();
    const result = inputDashes(inputs, rowsFromDOM(), {
      ...box,
      window: indexReady ? wheelIndex.window : null
    });
    rounds = result.rounds;
    active = -1;
    rail.classList.toggle("is-empty", result.empty);
    renderRounds(rounds);
    updateViewport();
    return result;
  }

  function nodeForDecision(decision) {
    if (!decision) return null;
    const keys = [];
    if (decision.key) keys.push(decision.key);
    if (decision.messageID) keys.push(`${KEY_PREFIX}${decision.messageID}`);
    for (const key of keys) {
      const node = container.querySelector(`[data-conversation-key="${escapeKey(key)}"]`);
      if (node) return node;
    }
    return null;
  }

  function locateDecision(round) {
    return planInputLocate(rounds, round, {
      windowSize: windowSize(),
      windowOffset: windowOffset(),
      maxPages: MAX_READ_BACK_PAGES
    });
  }

  // jumpToRound 是「点击/键盘选中某条输入刻度」的唯一入口：DOM 已有 → 滚动；
  // 否则先按决策回读那一页，再滚动定位（宿主只提供回读通道，不关心定位细节）。
  async function jumpToRound(round, behavior) {
    if (locating) return false;
    let decision = locateDecision(round);
    if (decision.action === "missing") return false;
    locating = true;
    try {
      if (decision.action !== "scroll" && !nodeForDecision(decision)) {
        // 几何/索引可能落后于 DOM：先重建一次再决策，避免无谓回读。
        refresh();
        decision = locateDecision(round);
        if (decision.action === "missing") return false;
      }
      const ok = await locateInput(decision, {
        findNode: nodeForDecision,
        loadPage: typeof options.locateInput === "function"
          ? async () => {
            const progressed = await options.locateInput({
              round: decision.round,
              offset: decision.offset,
              message_id: decision.messageID || "",
              summary: decision.summary
            }, decision);
            refresh(); // 回读后窗口变了：几何来自 DOM，重建刻度表
            return progressed !== false;
          }
          : null,
        smooth: behavior !== "instant",
        schedule: typeof window !== "undefined" ? window.setTimeout.bind(window) : undefined,
        highlightMs: HIGHLIGHT_MS
      });
      return ok;
    } finally {
      locating = false;
      updateViewport();
    }
  }

  function moveActive(delta) {
    if (rounds.length === 0) return;
    const current = Math.max(active, 0);
    const next = Math.min(Math.max(current + delta, 0), rounds.length - 1);
    active = next;
    void jumpToRound(rounds[next].round);
  }

  function localY(event) {
    const rect = track.getBoundingClientRect();
    return Math.max(0, Math.min(rect.height, event.clientY - rect.top));
  }

  function showTip(round, y) {
    if (!round) {
      tip.classList.remove("is-visible");
      return;
    }
    tip.textContent = `#${round.round} ${round.summary}`;
    tip.dataset.wheelKind = "user";
    tip.style.top = `${Math.max(0, Math.min(track.getBoundingClientRect().height - 18, y))}px`;
    tip.classList.add("is-visible");
  }

  function scrollInstantly(top) {
    // 拖拽/锚定必须是立即定位：.conversation 上的 scroll-behavior 在这里被
    // 临时屏蔽，否则每次 pointermove 都变成一段动画（表现为「拖不动、跟不上」）。
    const previous = container.style.scrollBehavior;
    container.style.scrollBehavior = "auto";
    container.scrollTop = top;
    container.style.scrollBehavior = previous;
  }

  track.addEventListener("pointerdown", event => {
    if (event.button !== 0) return;
    // 刻度点击（无拖拽）走 click 的精确跳转；这里不起拖拽，也不先按点击
    // 比例滚动一次，避免「跳两下」。
    if (event.target instanceof Element && event.target.closest(".wheel-dash")) {
      track.focus({ preventScroll: true });
      return;
    }
    const box = geometry();
    const y = localY(event);
    moved = false;
    dragging = { id: event.pointerId, y, scrollTop: box.scrollTop };
    if (typeof track.setPointerCapture === "function") {
      try { track.setPointerCapture(event.pointerId); } catch { /* ignore */ }
    }
    // 轨道空白处：视口对到点击位置（按内容比例），随后的移动即拖拽。
    const fraction = box.trackHeight > 0 ? y / box.trackHeight : 0;
    scrollInstantly(scrollTopForFraction(fraction, box));
    dragging.scrollTop = container.scrollTop;
    updateViewport();
    event.preventDefault();
    track.focus({ preventScroll: true });
  });

  track.addEventListener("pointermove", event => {
    const y = localY(event);
    if (!dragging) {
      showTip(inputAtOffset(rounds, y), y);
      return;
    }
    if (dragging && dragging.id === event.pointerId) {
      if (Math.abs(event.clientY - dragging.y) > 3) moved = true;
      const box = geometry();
      // 1:1 跟手：指针走过的轨道比例 = 内容比例。
      const ratio = box.trackHeight > 0 ? (y - dragging.y) / box.trackHeight : 0;
      const maxScroll = Math.max(box.scrollHeight - box.clientHeight, 0);
      scrollInstantly(dragging.scrollTop + ratio * maxScroll);
      updateViewport();
    }
    showTip(inputAtOffset(rounds, y), y);
  });

  function endDrag(event) {
    if (!dragging) return;
    if (event && dragging.id !== event.pointerId) return;
    dragging = null;
    if (!moved && event) {
      const round = inputAtOffset(rounds, localY(event));
      if (round) void jumpToRound(round.round);
    }
  }
  track.addEventListener("pointerup", endDrag);
  track.addEventListener("pointercancel", event => {
    dragging = null;
    if (event) showTip(null, 0);
  });
  track.addEventListener("pointerleave", () => showTip(null, 0));
  track.addEventListener("wheel", event => {
    // 轨道上的滚轮直接滚动对话（不改变缩放），手感与普通滚动条一致。
    container.scrollTop += event.deltaY;
    container.dispatchEvent(new Event("scroll"));
    updateViewport();
    event.preventDefault();
  }, { passive: false });
  track.addEventListener("keydown", event => {
    // 键盘导航只作用于用户输入刻度：↑/↓ 相邻一条、PgUp/PgDn 跨 5 条、
    // Home/End 到首/末条、Enter/Space 跳到当前条。
    if (event.key === "ArrowUp") moveActive(-1);
    else if (event.key === "ArrowDown") moveActive(1);
    else if (event.key === "PageUp") moveActive(-PAGE_JUMP);
    else if (event.key === "PageDown") moveActive(PAGE_JUMP);
    else if (event.key === "Home") moveActive(-rounds.length);
    else if (event.key === "End") moveActive(rounds.length);
    else if (event.key === "Enter" || event.key === " ") {
      const round = rounds[Math.max(active, 0)];
      if (round) void jumpToRound(round.round);
      event.preventDefault();
      return;
    }
    else return;
    event.preventDefault();
  });

  if (typeof ResizeObserver === "function") {
    const observer = new ResizeObserver(() => refresh());
    observer.observe(container);
  }

  return {
    // update 兼容旧调用点（模型变化）：几何来自 DOM，模型参数不再参与布局。
    update() { return refresh(); },
    refresh,
    updateViewport,
    scrollToKey(key) {
      const text = String(key ?? "");
      const round = rounds.find(item => item.key === text || (item.messageID && `${KEY_PREFIX}${item.messageID}` === text));
      if (!round) return false;
      return jumpToRound(round.round);
    },
    // setIndex 写入后端全量用户输入索引（payload = SessionInputIndex 的 JSON；
    // 传 null/undefined 退回「窗口内用户行」临时模式）。
    setIndex(payload) {
      if (payload === null || payload === undefined) {
        indexReady = false;
        wheelIndex = { items: [], window: null };
      } else {
        indexReady = true;
        wheelIndex = normalizeInputIndex(payload);
      }
      signature = "";
      return refresh();
    },
    // rounds 返回当前刻度表（宿主做定位编排/诊断）。
    rounds() { return rounds; },
    // locator 暴露定位入口（宿主在外部触发"跳到第 N 条输入"时使用）。
    locate(round) { return jumpToRound(round); }
  };
}
