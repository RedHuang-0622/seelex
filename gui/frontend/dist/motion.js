// ── 动效细节（拟物）──────────────────────────────────────────
// 三类「细节动效」的规则集中一处：数字变更、增量入场、滚轮/滑动。纯函数可被
// `node --test` 直接覆盖；DOM 应用器只在真机上跑，模块被导入时不碰 document
// （因此测试可以直接 import，不需要像 app.js 那样做源码级替换）。
//
// 口径与 docs/gui/CHANGELOG.md 的动效契约一致：
//  - 一切动效都尊重 `prefers-reduced-motion`：减少动效时直接落终值，不播动画；
//  - 时长/缓动由调用方给（CSS 侧仍是 `--dur-*` / `--ease-*` token）；
//  - 动效是呈现层细节，不改任何业务状态、不改契约字段。

// prefersReducedMotion 读系统偏好（无 window/matchMedia 环境返回 false）。
export function prefersReducedMotion() {
  try {
    return Boolean(typeof window !== "undefined" && window.matchMedia?.("(prefers-reduced-motion: reduce)")?.matches);
  } catch {
    return false;
  }
}

// countParts 把一段文本拆成「前缀 + 恰好一个整数 + 后缀」：只有恰好一个整数
// 段时才可做数字滚动（`12 项`、`3 未读`、`7` 都行；`1 / 2 页 · 12 项` 有三个
// 整数段，退回直接赋值——页信息变化本来就少，不值得为它猜哪个数字在变）。
// 没有整数段返回 null。
export function countParts(text) {
  const value = String(text ?? "");
  const matches = [...value.matchAll(/\d+/g)];
  if (matches.length !== 1) return null;
  const match = matches[0];
  return {
    prefix: value.slice(0, match.index),
    value: Number(match[0]),
    suffix: value.slice(match.index + match[0].length)
  };
}

// easeOutCubic 与 CSS 的 `--ease-out` 同族（先快后慢），里程表滚起来末段不"卡"。
export function easeOutCubic(t) {
  const p = Math.min(1, Math.max(0, Number(t) || 0));
  return 1 - Math.pow(1 - p, 3);
}

// tweenValue 在 from→to 之间取一个取整中间值（progress∈[0,1]）。非有限 from
// 退化为直接到 to。
export function tweenValue(from, to, progress) {
  const start = Number(from);
  const end = Number(to);
  if (!Number.isFinite(end)) return null;
  if (!Number.isFinite(start)) return end;
  return Math.round(start + (end - start) * easeOutCubic(progress));
}

// rollTokens 记录元素上"当前有效的滚动序号"：后一次 roll 让前一次的 rAF 循环
// 自行退出（而不是两条循环互相覆盖 textContent，出现数字回跳）。
const rollTokens = new WeakMap();

// rollNumber 把元素里的整数从当前值滚到目标文本（里程表）。返回是否真的播了
// 动画。以下情况直接落终值、不动画：无整数段 / 前缀后缀不同（不是"同一个计数
// 变了"）/ 数值没变 / 系统偏好减少动效 / 无 requestAnimationFrame。
export function rollNumber(element, nextText, options = {}) {
  if (!element) return false;
  const next = countParts(nextText);
  const settle = () => { if (element) element.textContent = String(nextText); };
  if (!next) { settle(); return false; }
  const current = countParts(element.textContent);
  if (!current || current.prefix !== next.prefix || current.suffix !== next.suffix || current.value === next.value) {
    settle();
    return false;
  }
  const raf = typeof options.raf === "function"
    ? options.raf
    : (typeof requestAnimationFrame === "function" ? requestAnimationFrame : null);
  const duration = Number.isFinite(options.duration) ? Math.max(0, options.duration) : 420;
  if (!raf || duration === 0 || prefersReducedMotion()) { settle(); return false; }
  const token = (rollTokens.get(element) || 0) + 1;
  rollTokens.set(element, token);
  element.classList?.add("is-rolling");
  const clock = typeof options.now === "function"
    ? options.now
    : () => (typeof performance !== "undefined" ? performance.now() : Date.now());
  const started = clock();
  const step = () => {
    if (rollTokens.get(element) !== token) return; // 被后一次 roll 取代，退出
    const progress = Math.min(1, (clock() - started) / duration);
    element.textContent = `${next.prefix}${tweenValue(current.value, next.value, progress)}${next.suffix}`;
    if (progress < 1) raf(step);
    else {
      rollTokens.set(element, 0);
      element.classList?.remove("is-rolling");
      settle();
    }
  };
  raf(step);
  return true;
}

// scrollEdges 判定滚动边缘（拟物：到边就不再显示那侧"还有内容"的阴影）。
// offset/viewport/size 是滚动位置/可视长度/内容长度（同一轴）。
export function scrollEdges(offset, viewport, size) {
  const start = Number(offset) > 1;
  const end = Number(offset) + Number(viewport) < Number(size) - 1;
  return { start, end };
}

// syncScrollEdges 按当前滚动位置给元素挂/摘边缘阴影类（只改 class，不碰内容）。
// axis="y" 用 is-scroll-up/down；axis="x" 用 is-scroll-start/end。
export function syncScrollEdges(element, axis = "y") {
  if (!element?.classList) return null;
  const vertical = axis === "y";
  const offset = Number(vertical ? element.scrollTop : element.scrollLeft) || 0;
  const viewport = Number(vertical ? element.clientHeight : element.clientWidth) || 0;
  const size = Number(vertical ? element.scrollHeight : element.scrollWidth) || 0;
  const edges = scrollEdges(offset, viewport, size);
  if (vertical) {
    element.classList.toggle("is-scroll-up", edges.start);
    element.classList.toggle("is-scroll-down", edges.end);
  } else {
    element.classList.toggle("is-scroll-start", edges.start);
    element.classList.toggle("is-scroll-end", edges.end);
  }
  return edges;
}

// wheelScrollDelta 决定一次纵向滚轮该翻译成多少横向位移。返回 null = 不接管
// （内容不足一屏，或该方向已到边——放行给外层容器继续纵向滚，滚轮不被"吃掉"）。
export function wheelScrollDelta(element, deltaY) {
  if (!element || !Number.isFinite(deltaY) || deltaY === 0) return null;
  const scrollWidth = Number(element.scrollWidth) || 0;
  const clientWidth = Number(element.clientWidth) || 0;
  const scrollLeft = Number(element.scrollLeft) || 0;
  if (scrollWidth <= clientWidth) return null;
  const atStart = scrollLeft <= 0;
  const atEnd = scrollLeft + clientWidth >= scrollWidth - 1;
  if ((deltaY < 0 && atStart) || (deltaY > 0 && atEnd)) return null;
  return deltaY;
}

// markEntering 给"新出现的元素"挂一次性入场类：只在元素**首次插入**时调用，
// 因此 keyed reconcile 后续的内容替换（replacement）不会重播动画。动画结束
// （或兜底超时 / 减少动效偏好）后摘类。
export function markEntering(element, className = "is-entering", timeoutMs = 800) {
  if (!element?.classList || prefersReducedMotion()) return false;
  element.classList.add(className);
  const clear = () => element.classList.remove(className);
  element.addEventListener?.("animationend", clear, { once: true });
  if (typeof setTimeout === "function") setTimeout(clear, timeoutMs);
  return true;
}

// bindHorizontalWheelDelegate 在根容器上委托"纵向滚轮 → 横向滚动"：任何位于
// `.scroll-edges-x` 横向条（chip 行 / sheet 页签栏 / 页签栏）之上的滚轮都被翻成
// 横向拨动。委托到根容器而不是逐元素绑定，动态生成的横条（工作表格 sheet 栏）
// 无需再挂监听。
export function bindHorizontalWheelDelegate(root) {
  if (!root?.addEventListener) return () => {};
  const onWheel = event => {
    const strip = event.target?.closest?.(".scroll-edges-x");
    if (!strip) return;
    const delta = wheelScrollDelta(strip, event.deltaY);
    if (delta === null) return;
    event.preventDefault();
    strip.scrollLeft += delta;
    syncScrollEdges(strip, "x");
  };
  root.addEventListener("wheel", onWheel, { passive: false });
  return () => root.removeEventListener("wheel", onWheel);
}

// bindHorizontalDragDelegate 在根容器上委托"按住拖动 → 横向拨动"（拟物：把横条
// 当成一根可拨的滚轴）。超过阈值才算拖动；拖动结束后的那次 click 被吞掉，
// 不把"拨动"误当成"点了某个页签"。
export function bindHorizontalDragDelegate(root, options = {}) {
  if (!root?.addEventListener) return () => {};
  const threshold = Number.isFinite(options.threshold) ? options.threshold : 4;
  let drag = null;
  const onPointerDown = event => {
    if (event.button !== 0) return;
    // 页签本身是 draggable（拖拽换序）：让 HTML5 DnD 拥有这次手势，不跟它抢
    // ——否则横向拨动会与拖拽换序同时发生。
    if (event.target?.closest?.('[draggable="true"]')) return;
    const strip = event.target?.closest?.(".scroll-edges-x");
    if (!strip) return;
    if (Number(strip.scrollWidth) <= Number(strip.clientWidth)) return;
    drag = { strip, id: event.pointerId, x: event.clientX, left: Number(strip.scrollLeft) || 0, moved: false };
  };
  const onPointerMove = event => {
    if (!drag || event.pointerId !== drag.id) return;
    const dx = event.clientX - drag.x;
    if (!drag.moved && Math.abs(dx) < threshold) return;
    drag.moved = true;
    drag.strip.classList.add("is-drag-scrolling");
    drag.strip.scrollLeft = drag.left - dx;
    syncScrollEdges(drag.strip, "x");
  };
  const end = () => {
    if (!drag) return;
    const { strip, moved } = drag;
    drag = null;
    strip.classList.remove("is-drag-scrolling");
    if (!moved) return;
    const swallow = clickEvent => { clickEvent.stopPropagation(); clickEvent.preventDefault(); };
    strip.addEventListener("click", swallow, { capture: true, once: true });
    if (typeof setTimeout === "function") setTimeout(() => strip.removeEventListener("click", swallow, { capture: true }), 300);
  };
  root.addEventListener("pointerdown", onPointerDown, true);
  root.addEventListener("pointermove", onPointerMove, true);
  root.addEventListener("pointerup", end, true);
  root.addEventListener("pointercancel", end, true);
  return () => {
    root.removeEventListener("pointerdown", onPointerDown, true);
    root.removeEventListener("pointermove", onPointerMove, true);
    root.removeEventListener("pointerup", end, true);
    root.removeEventListener("pointercancel", end, true);
  };
}

// bindScrollShadows 在根容器上委托纵向滚动边缘阴影：容器自身滚动时按位置挂/摘
// `is-scroll-up/down`（CSS 画内阴影）。scroll 不冒泡，因此监听在捕获阶段。
// selector 命中才处理，避免给全站每个滚动容器都挂类。
export function bindScrollShadows(root, selector) {
  if (!root?.addEventListener || !selector) return () => {};
  const onScroll = event => {
    const target = event.target;
    if (!target?.matches?.(selector)) return;
    target.classList?.add("scroll-shadows");
    syncScrollEdges(target, "y");
  };
  root.addEventListener("scroll", onScroll, true);
  return () => root.removeEventListener("scroll", onScroll, true);
}

// syncAllScrollShadows 给当前所有匹配的容器补一次边缘阴影状态（首次渲染/整份
// 快照重绘后调用；之后增量由委托的滚动监听维护）。
export function syncAllScrollShadows(root, selector) {
  if (!root?.querySelectorAll || !selector) return 0;
  let count = 0;
  root.querySelectorAll(selector).forEach(element => {
    element.classList?.add("scroll-shadows");
    syncScrollEdges(element, "y");
    count += 1;
  });
  return count;
}
