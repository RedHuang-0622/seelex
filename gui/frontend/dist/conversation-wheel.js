// 对话时间线轮轴（右下侧 minimap + 滚动滑柄）。
//
// 设计要点（2026-09-11 重做）：
//   1. 位置来自**真实几何**：每条线按对应 DOM 项在滚动内容里的 offsetTop/height
//      映射到轨道，而不是按「条目权重」均分——权重均分让线条与内容毫无对应关系
//      （旧实现即此），拖到某个位置看到的不是那一段内容。
//   2. 一条 DOM 项 = 一条线；线的粗细 = 该项占内容高度的比例（下限可见、上限
//      可读，上限随轨道高度自适应），颜色/宽度按类型（用户输入 / Agent 正文 /
//      思考 / 工具过程 / 系统）。
//   3. 视口滑柄（thumb）宽度固定、位置与高度反映当前可视区间；拖拽滑柄或点击
//      轨道即滚动，点击某条线即跳转到该条消息并闪烁定位。
//   4. 悬停出标签（类型 + 摘要）与高亮；键盘可上下/翻页/首尾定位（role=scrollbar）。
//   5. 线表随 DOM 重新测量：加载更早历史、增量新消息、容器缩放后自动重建，
//      不需要调用方维护任何「线条加载」状态。
//
// 纯函数（buildWheelLines/wheelThumb/scrollTopForThumbTop/scrollTopForFraction/
// lineAtOffset/normalizeWheelKind）可脱离 DOM 单测，见 conversation-wheel.test.mjs。

const MIN_LINE = 2;
const MAX_LINE = 48;
const MIN_THUMB = 14;
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

// buildWheelLines 把「已测量的条目几何」映射为轨道上的线表。
// rows: [{ key, kind, label, offsetTop, height }]（按文档顺序）
export function buildWheelLines(rows, options = {}) {
  const scrollHeight = Math.max(Number(options.scrollHeight) || 0, 0);
  const trackHeight = Math.max(Number(options.trackHeight) || 0, 0);
  const minLine = Math.max(Number(options.minLine) || MIN_LINE, 1);
  // 上限默认随轨道高度自适应：固定值在长轨道上会把大块内容压成细线，丢失
  // 「哪里有长工具输出/长回复」这层信息；轨道 1/4（12–48px）让最多约 8 个
  // 大块仍彼此可分辨，同时不让单块吃掉整条轨道。
  const adaptiveMax = Math.min(Math.max(trackHeight * 0.25, 12), MAX_LINE);
  const maxLine = Math.max(Number(options.maxLine) || adaptiveMax, minLine);
  const list = Array.isArray(rows) ? rows : [];
  if (scrollHeight <= 0 || trackHeight <= 0 || list.length === 0) {
    return { empty: true, lines: [], scrollHeight, trackHeight };
  }
  const scale = trackHeight / scrollHeight;
  const lines = [];
  for (const row of list) {
    if (!row || !row.key) continue;
    const offsetTop = Math.max(Number(row.offsetTop) || 0, 0);
    const height = Math.max(Number(row.height) || 0, 0);
    const lineHeight = Math.min(Math.max(height * scale, minLine), maxLine);
    // 顶对齐：线段起点与条目在内容里的起点同比例（首条线正好贴轨道顶部），
    // 高度按比例截断到 [minLine, maxLine]。
    const top = Math.max(0, Math.min(trackHeight - lineHeight, offsetTop * scale));
    lines.push({
      key: String(row.key),
      kind: normalizeWheelKind(row.kind),
      label: typeof row.label === "string" ? row.label : "",
      top: round(top),
      height: round(Math.min(lineHeight, trackHeight)),
      offsetTop,
      offsetBottom: offsetTop + height,
      progress: Math.min(1, Math.max(0, offsetTop / scrollHeight))
    });
  }
  return { empty: lines.length === 0, lines, scrollHeight, trackHeight };
}

// wheelThumb 计算视口滑柄几何（top/height 以轨道像素为单位）。
export function wheelThumb(geometry = {}) {
  const scrollHeight = Math.max(Number(geometry.scrollHeight) || 0, 0);
  const clientHeight = Math.max(Number(geometry.clientHeight) || 0, 0);
  const trackHeight = Math.max(Number(geometry.trackHeight) || 0, 0);
  const maxScroll = Math.max(scrollHeight - clientHeight, 0);
  if (scrollHeight <= 0 || clientHeight <= 0 || trackHeight <= 0) {
    return { top: 0, height: trackHeight, travel: 0, maxScroll: 0, progress: 0 };
  }
  const ratio = Math.min(1, clientHeight / scrollHeight);
  const height = Math.max(Math.min(trackHeight, trackHeight * ratio), Math.min(MIN_THUMB, trackHeight));
  const travel = Math.max(trackHeight - height, 0);
  const scrollTop = Math.min(Math.max(Number(geometry.scrollTop) || 0, 0), maxScroll);
  const top = maxScroll > 0 ? (scrollTop / maxScroll) * travel : 0;
  return {
    top: round(top),
    height: round(height),
    travel: round(travel),
    maxScroll,
    progress: maxScroll > 0 ? scrollTop / maxScroll : 0
  };
}

// scrollTopForThumbTop 由滑柄目标位置反解滚动位置（拖拽用，1:1 跟手）。
export function scrollTopForThumbTop(thumbTop, geometry = {}) {
  const { travel, maxScroll } = wheelThumb(geometry);
  if (travel <= 0 || maxScroll <= 0) return 0;
  const clamped = Math.min(Math.max(Number(thumbTop) || 0, 0), travel);
  return (clamped / travel) * maxScroll;
}

// scrollTopForFraction 由轨道比例反解滚动位置（点击轨道空白处用）。
export function scrollTopForFraction(fraction, geometry = {}) {
  const maxScroll = Math.max((Number(geometry.scrollHeight) || 0) - (Number(geometry.clientHeight) || 0), 0);
  const value = Math.min(Math.max(Number(fraction) || 0, 0), 1);
  return value * maxScroll;
}

// lineAtOffset 返回轨道 y 处命中的线：优先命中的线，其次最近的一条
// （线很细时也要能悬停/点击到，手感优先）。
export function lineAtOffset(lines, y) {
  const list = Array.isArray(lines) ? lines : [];
  if (list.length === 0) return null;
  const offset = Number(y) || 0;
  let nearest = null;
  let best = Infinity;
  for (const line of list) {
    if (offset >= line.top && offset <= line.top + line.height) return line;
    const distance = Math.min(Math.abs(offset - line.top), Math.abs(offset - (line.top + line.height)));
    if (distance < best) {
      best = distance;
      nearest = line;
    }
  }
  return nearest;
}

// wheelSignature 是线表的几何指纹：内容没变就不重建 DOM（避免滚动/流式期间
// 反复 reflow）。
export function wheelSignature(lines) {
  const list = Array.isArray(lines) ? lines : [];
  return list.map(line => `${line.key}:${line.top}:${line.height}:${line.kind}`).join("|");
}

function round(value) {
  return Math.round(value * 100) / 100;
}

// createConversationWheel 绑定到滚动容器：轨道挂在容器的父元素（对话外壳）上，
// 线表从容器当前子项真实测量——加载更早历史后无需任何显式刷新调用。
export function createConversationWheel(container, options = {}) {
  if (!container) throw new Error("conversation wheel requires a scroll container");
  const parent = container.parentElement || container;
  const rail = document.createElement("section");
  rail.className = "conversation-wheel is-empty";
  rail.setAttribute("aria-label", "对话时间线轮轴：拖拽滚动，点击线条跳转");
  const track = document.createElement("div");
  track.className = "wheel-track";
  track.setAttribute("role", "scrollbar");
  track.setAttribute("aria-orientation", "vertical");
  track.setAttribute("aria-valuemin", "0");
  track.setAttribute("aria-valuemax", "100");
  track.tabIndex = 0;
  const viewport = document.createElement("div");
  viewport.className = "wheel-viewport";
  const viewportCore = document.createElement("div");
  viewportCore.className = "wheel-viewport-core";
  viewport.appendChild(viewportCore);
  const tip = document.createElement("div");
  tip.className = "wheel-tip";
  tip.setAttribute("aria-hidden", "true");
  track.append(viewport, tip);
  rail.appendChild(track);
  parent.appendChild(rail);

  const lineNodes = new Map();
  let signature = "";
  let lines = [];
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

  function renderLines(nextLines) {
    const next = wheelSignature(nextLines);
    if (next === signature) return;
    signature = next;
    const desired = new Set();
    for (const line of nextLines) {
      desired.add(line.key);
      let node = lineNodes.get(line.key);
      if (!node) {
        node = document.createElement("button");
        node.type = "button";
        node.className = "wheel-line";
        node.dataset.wheelNode = line.key;
        node.addEventListener("click", event => {
          event.preventDefault();
          jumpTo(line.key);
        });
        lineNodes.set(line.key, node);
      }
      if (node.dataset.wheelKind !== line.kind) node.dataset.wheelKind = line.kind;
      node.className = `wheel-line is-${line.kind}`;
      node.style.top = `${line.top}px`;
      node.style.height = `${line.height}px`;
      node.title = line.label || line.key;
      node.setAttribute("aria-label", line.label || line.key);
    }
    for (const [key, node] of [...lineNodes]) {
      if (desired.has(key)) continue;
      node.remove();
      lineNodes.delete(key);
    }
    for (const line of nextLines) {
      const node = lineNodes.get(line.key);
      if (node) track.appendChild(node);
    }
    // viewport / tip 始终在最后，避免被线条覆盖。
    track.append(viewport, tip);
  }

  function updateViewport() {
    const box = geometry();
    const thumb = wheelThumb(box);
    viewport.style.top = `${thumb.top}px`;
    viewport.style.height = `${thumb.height}px`;
    const percent = Math.round(thumb.progress * 100);
    track.setAttribute("aria-valuenow", String(percent));
    track.setAttribute("aria-valuetext", `已滚动 ${percent}%`);
  }

  // markVisibleLines 只在渲染/缩放时执行：滚动路径（每帧触发）不遍历全部
  // 线条，只更新滑柄几何。
  function markVisibleLines() {
    const box = geometry();
    for (const line of lines) {
      const node = lineNodes.get(line.key);
      if (!node) continue;
      const visible = line.offsetTop < box.scrollTop + box.clientHeight && line.offsetBottom > box.scrollTop;
      if (node.classList.contains("is-in-view") !== visible) node.classList.toggle("is-in-view", visible);
    }
  }

  function refresh() {
    const box = geometry();
    const result = buildWheelLines(rowsFromDOM(), box);
    lines = result.lines;
    rail.classList.toggle("is-empty", result.empty);
    renderLines(lines);
    updateViewport();
    markVisibleLines();
    return result;
  }

  function jumpTo(key) {
    if (!key) return;
    const node = container.querySelector(`[data-conversation-key="${CSS.escape(key)}"]`);
    if (!node) return;
    const reduced = typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    node.scrollIntoView({ behavior: reduced ? "auto" : "smooth", block: "center" });
    node.classList.add("is-wheel-target");
    window.setTimeout(() => node.classList.remove("is-wheel-target"), 1400);
    updateViewport();
  }

  function localY(event) {
    const rect = track.getBoundingClientRect();
    return Math.max(0, Math.min(rect.height, event.clientY - rect.top));
  }

  function showTip(line, y) {
    if (!line) {
      tip.classList.remove("is-visible");
      return;
    }
    tip.textContent = line.label || line.key;
    tip.dataset.wheelKind = line.kind;
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
    // 线条点击（无拖拽）走 click 的精确跳转；这里不起拖拽，也不先按点击
    // 比例滚动一次，避免「跳两下」。
    if (event.target instanceof Element && event.target.closest(".wheel-line")) {
      track.focus({ preventScroll: true });
      return;
    }
    const box = geometry();
    const thumb = wheelThumb(box);
    const y = localY(event);
    moved = false;
    dragging = { id: event.pointerId, y, thumbTop: thumb.top, height: thumb.height };
    if (typeof track.setPointerCapture === "function") {
      try { track.setPointerCapture(event.pointerId); } catch { /* ignore */ }
    }
    if (y < thumb.top || y > thumb.top + thumb.height) {
      // 轨道空白处：视口中心对到点击位置，随后的移动即拖拽。
      const target = scrollTopForThumbTop(y - thumb.height / 2, box);
      scrollInstantly(target);
      dragging.thumbTop = wheelThumb(geometry()).top;
    }
    event.preventDefault();
    track.focus({ preventScroll: true });
  });

  track.addEventListener("pointermove", event => {
    const y = localY(event);
    if (!dragging) {
      showTip(lineAtOffset(lines, y), y);
      return;
    }
    if (dragging && dragging.id === event.pointerId) {
      if (Math.abs(event.clientY - dragging.y) > 3) moved = true;
      const box = geometry();
      const thumb = wheelThumb(box);
      const target = scrollTopForThumbTop(dragging.thumbTop + (y - dragging.y), box);
      scrollInstantly(target);
      updateViewport();
    }
    showTip(lineAtOffset(lines, y), y);
  });

  function endDrag(event) {
    if (!dragging) return;
    if (event && dragging.id !== event.pointerId) return;
    dragging = null;
    if (!moved) {
      const line = lineAtOffset(lines, localY(event));
      if (line) jumpTo(line.key);
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
