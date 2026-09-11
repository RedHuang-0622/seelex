// 对话导航轮轴（右侧问答刻度）。
//
// 设计要点（2026-09-11 二次重做，对齐 DeepSeek 网页版右侧导航）：
//   1. **一条刻度 = 一问一答**：刻度挂在每个 user 轮上，点击即跳到该问题，
//      它的回答就在下面——而不是给每条 DOM 项都画一条无意义的线。
//   2. 刻度位置来自**真实几何**：问题节点在滚动内容里的 offsetTop 占内容
//      高度的比例，而不是按「条目权重」均分；用户滚到哪，高亮就落在哪一条。
//   3. 窗口里没有 user 轮时（长会话翻到中段）退回按助手步骤分段，避免右侧
//      整条轨道空着。
//   4. 视觉是短线 + 一条 1px 基线：当前问答用主信号色加长，其余压暗；悬停
//      出问题摘要，键盘 ↑/↓/PgUp/PgDn/Home/End 可导航。
//
// 纯函数（buildWheelRounds/wheelAnchors/roundAtOffset/activeRoundIndex/
// scrollTopForFraction/normalizeWheelKind/wheelSignature）可脱离 DOM 单测，
// 见 conversation-wheel.test.mjs。

const DASH = 3;
const MIN_GAP = 11;
const ACTIVE_LINE = 0.4;
const DEFAULT_KINDS = ["user", "agent", "think", "tools", "system", "other"];

export function normalizeWheelKind(raw) {
  const value = String(raw ?? "").trim().toLowerCase();
  if (DEFAULT_KINDS.includes(value)) return value;
  switch (value) {
  case "assistant":
  case "llm":
  case "chat":
    return "agent";
  case "reasoning":
  case "thinking":
    return "think";
  case "tool":
  case "tool_result":
  case "axis":
  case "tool-call":
    return "tools";
  case "notice":
  case "error":
    return "system";
  default:
    return "other";
  }
}

// wheelAnchors 选出刻度的挂点：优先「用户提问」（一问一答导航），窗口里没有
// 用户轮（长会话翻到中段）时退回助手步骤，都没有才算空。
export function wheelAnchors(entries) {
  const list = (Array.isArray(entries) ? entries : []).filter(entry => entry && entry.key);
  const turns = list.filter(entry => normalizeWheelKind(entry.kind) === "user");
  if (turns.length > 0) return { mode: "turn", anchors: turns };
  const steps = list.filter(entry => {
    const kind = normalizeWheelKind(entry.kind);
    return kind === "agent" || kind === "think";
  });
  return { mode: steps.length > 0 ? "step" : "none", anchors: steps };
}

// buildWheelRounds 把「已测量的条目几何」映射为轨道上的刻度表。
// entries: [{ key, kind, label, offsetTop, height }]（按文档顺序）
//
// 位置 = 该问答起始位置占内容高度的比例 × 可用轨道；同时保证最小间距，
// 否则短问答会叠在一起。刻度等长，回答区间只用于高亮与命中。
export function buildWheelRounds(entries, options = {}) {
  const scrollHeight = Math.max(Number(options.scrollHeight) || 0, 0);
  const trackHeight = Math.max(Number(options.trackHeight) || 0, 0);
  const dash = Math.max(Number(options.dashHeight) || DASH, 1);
  const minGap = Math.max(Number(options.minGap) || MIN_GAP, 0);
  const list = Array.isArray(entries) ? entries : [];
  const empty = { empty: true, mode: "none", rounds: [], scrollHeight, trackHeight, dashHeight: dash };
  if (scrollHeight <= 0 || trackHeight <= 0 || list.length === 0) return empty;

  const { mode, anchors } = wheelAnchors(list);
  if (anchors.length === 0) return empty;

  const available = Math.max(trackHeight - dash, 0);
  const gap = anchors.length > 1 ? Math.min(minGap, available / (anchors.length - 1)) : 0;
  const tops = [];
  let previous = Number.NEGATIVE_INFINITY;
  for (const anchor of anchors) {
    const offsetTop = Math.max(Number(anchor.offsetTop) || 0, 0);
    const proportional = Math.min(offsetTop / scrollHeight, 1) * available;
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

  const rounds = anchors.map((anchor, index) => {
    const offsetTop = Math.max(Number(anchor.offsetTop) || 0, 0);
    const height = Math.max(Number(anchor.height) || 0, 0);
    const nextTop = index + 1 < anchors.length ? Math.max(Number(anchors[index + 1].offsetTop) || 0, offsetTop) : scrollHeight;
    return {
      index,
      key: String(anchor.key),
      kind: normalizeWheelKind(anchor.kind),
      label: typeof anchor.label === "string" ? anchor.label : "",
      top: round(Math.min(Math.max(tops[index], 0), available)),
      height: round(dash),
      offsetTop,
      offsetBottom: Math.max(nextTop, offsetTop + height),
      progress: Math.min(1, Math.max(0, offsetTop / scrollHeight))
    };
  });
  return { empty: rounds.length === 0, mode, rounds, scrollHeight, trackHeight, dashHeight: dash };
}

// roundAtOffset 返回轨道 y 处命中的刻度：优先命中，其次最近的一条
// （刻度很细时也要能悬停/点击到，手感优先）。
export function roundAtOffset(rounds, y) {
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

// activeRoundIndex 返回当前问答的序号：视口参考线（40% 处）落在哪个问答的
// 区间里，就高亮哪一条；还没滚过第一条时给 0。
export function activeRoundIndex(rounds, geometry = {}) {
  const list = Array.isArray(rounds) ? rounds : [];
  if (list.length === 0) return -1;
  const reference = (Number(geometry.scrollTop) || 0) + Math.max(Number(geometry.clientHeight) || 0, 0) * ACTIVE_LINE;
  let active = 0;
  for (const round of list) {
    if (round.offsetTop <= reference) active = round.index;
    else break;
  }
  return active;
}

// scrollTopForFraction 由轨道比例反解滚动位置（点击轨道空白处用）。
export function scrollTopForFraction(fraction, geometry = {}) {
  const maxScroll = Math.max((Number(geometry.scrollHeight) || 0) - (Number(geometry.clientHeight) || 0), 0);
  const value = Math.min(Math.max(Number(fraction) || 0, 0), 1);
  return value * maxScroll;
}

// wheelSignature 是刻度表的几何指纹：内容没变就不重建 DOM（避免滚动/流式
// 期间反复 reflow）。
export function wheelSignature(rounds) {
  const list = Array.isArray(rounds) ? rounds : [];
  return list.map(round => `${round.key}:${round.top}:${round.kind}`).join("|");
}

function round(value) {
  return Math.round(value * 100) / 100;
}

// createConversationWheel 绑定到滚动容器：轨道挂在容器的父元素（对话外壳）
// 上，刻度从容器当前子项真实测量——加载更早历史后无需任何显式刷新调用。
export function createConversationWheel(container, options = {}) {
  if (!container) throw new Error("conversation wheel requires a scroll container");
  const parent = container.parentElement || container;
  const rail = document.createElement("section");
  rail.className = "conversation-wheel is-empty";
  rail.setAttribute("aria-label", "对话导航轮轴：点击刻度跳到对应问答");
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

  function renderRounds(nextRounds) {
    const next = wheelSignature(nextRounds);
    if (next === signature) return;
    signature = next;
    const desired = new Set();
    for (const round of nextRounds) {
      desired.add(round.key);
      let node = dashNodes.get(round.key);
      if (!node) {
        node = document.createElement("button");
        node.type = "button";
        node.className = "wheel-dash";
        node.dataset.wheelNode = round.key;
        node.addEventListener("click", event => {
          event.preventDefault();
          jumpTo(round.key);
        });
        dashNodes.set(round.key, node);
      }
      node.className = `wheel-dash is-${round.kind}`;
      node.style.top = `${round.top}px`;
      node.title = round.label || round.key;
      node.setAttribute("aria-label", round.label || round.key);
    }
    for (const [key, node] of [...dashNodes]) {
      if (desired.has(key)) continue;
      node.remove();
      dashNodes.delete(key);
    }
    for (const round of nextRounds) {
      const node = dashNodes.get(round.key);
      if (node) track.appendChild(node);
    }
    // tip 始终在最后，避免被刻度覆盖。
    track.appendChild(tip);
  }

  // updateViewport 只更新「当前问答」高亮：滚动路径（每帧触发）不重建刻度。
  function updateViewport() {
    const box = geometry();
    const next = activeRoundIndex(rounds, box);
    if (next !== active) {
      const previousNode = active >= 0 ? dashNodes.get(rounds[active]?.key) : null;
      if (previousNode) previousNode.classList.remove("is-active");
      active = next;
      const node = active >= 0 ? dashNodes.get(rounds[active]?.key) : null;
      if (node) node.classList.add("is-active");
    }
    const percent = box.scrollHeight > box.clientHeight
      ? Math.round((box.scrollTop / (box.scrollHeight - box.clientHeight)) * 100)
      : 100;
    track.setAttribute("aria-valuenow", String(Math.min(Math.max(percent, 0), 100)));
    track.setAttribute("aria-valuetext", rounds.length > 0
      ? `第 ${Math.max(active, 0) + 1} 段，共 ${rounds.length} 段问答`
      : "暂无问答");
  }

  function refresh() {
    const box = geometry();
    const result = buildWheelRounds(rowsFromDOM(), box);
    rounds = result.rounds;
    active = -1;
    rail.classList.toggle("is-empty", result.empty);
    renderRounds(rounds);
    updateViewport();
    return result;
  }

  function jumpTo(key) {
    if (!key) return;
    const node = container.querySelector(`[data-conversation-key="${CSS.escape(key)}"]`);
    if (!node) return;
    const reduced = typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    // 问题对齐视口上沿：它的回答正好在下面铺开。
    node.scrollIntoView({ behavior: reduced ? "auto" : "smooth", block: "start" });
    node.classList.add("is-wheel-target");
    window.setTimeout(() => node.classList.remove("is-wheel-target"), 1400);
    updateViewport();
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
    tip.textContent = round.label || round.key;
    tip.dataset.wheelKind = round.kind;
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
      showTip(roundAtOffset(rounds, y), y);
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
    showTip(roundAtOffset(rounds, y), y);
  });

  function endDrag(event) {
    if (!dragging) return;
    if (event && dragging.id !== event.pointerId) return;
    dragging = null;
    if (!moved && event) {
      const round = roundAtOffset(rounds, localY(event));
      if (round) jumpTo(round.key);
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
    const box = geometry();
    const page = Math.max(container.clientHeight - 48, 120);
    const stepBy = { ArrowUp: -48, ArrowDown: 48, PageUp: -page, PageDown: page };
    if (event.key in stepBy) container.scrollTop += stepBy[event.key];
    else if (event.key === "Home") container.scrollTop = 0;
    else if (event.key === "End") container.scrollTop = box.scrollHeight;
    else if (event.key === "Enter" || event.key === " ") {
      const round = rounds[Math.max(active, 0)];
      if (round) jumpTo(round.key);
      event.preventDefault();
      return;
    }
    else return;
    container.dispatchEvent(new Event("scroll"));
    updateViewport();
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
    scrollToKey: jumpTo
  };
}
