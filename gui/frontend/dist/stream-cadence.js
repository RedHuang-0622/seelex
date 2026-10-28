// 流式输出的表现层 cadence。这里是四个节奏常量的唯一事实源；Snapshot、Event
// 和 reducer 不知道它存在。调度只决定何时把最新一帧交给渲染器，不改变最终文本、
// message key、revision 或持久化事实。
//
// 口径来自 docs/gui/FRONTEND-CONSTITUTION.md §3：
//   - 第一个 chunk 等下一帧，最多 50ms 合并窗口立即开始；
//   - 后续 chunk 每 50ms 最多提交一次，始终 latest-wins；
//   - 连续 100ms 有流才进入“接收中”视觉；
//   - 连续 150ms 无 chunk 才退出该视觉；
//   - terminal 到达时先 flush 尚未提交的尾包，再提交稳定帧。
export const STREAM_TICK_MS = 50;
export const STREAM_ENTER_HYSTERESIS_MS = 100;
export const STREAM_EXIT_HYSTERESIS_MS = 150;
export const STREAM_MAX_COALESCE_MS = 50;

export function createStreamCadence(options = {}) {
  const now = typeof options.now === "function" ? options.now : () => Date.now();
  const setTimer = typeof options.setTimeout === "function"
    ? options.setTimeout
    : (callback, delay) => setTimeout(callback, delay);
  const clearTimer = typeof options.clearTimeout === "function"
    ? options.clearTimeout
    : handle => clearTimeout(handle);
  const requestFrame = typeof options.requestAnimationFrame === "function"
    ? options.requestAnimationFrame
    : callback => setTimer(callback, 0);

  let pending = null;
  let renderTimer = null;
  let enterTimer = null;
  let exitTimer = null;
  let receiving = false;
  let hasRendered = false;
  let disposed = false;

  const clear = handle => {
    if (handle !== null && handle !== undefined) clearTimer(handle);
    return null;
  };

  function setReceiving(next, reason) {
    const value = Boolean(next);
    if (receiving === value) return;
    receiving = value;
    options.onPhaseChange?.(value ? "receiving" : "idle", { reason });
  }

  function emitPending() {
    if (!pending) return false;
    const frame = pending;
    pending = null;
    hasRendered = true;
    options.onRender?.(frame, receiving ? "receiving" : "idle");
    return true;
  }

  function scheduleRender(first) {
    if (disposed || renderTimer !== null || !pending) return;
    const callback = () => {
      renderTimer = null;
      emitPending();
    };
    renderTimer = first
      ? requestFrame(callback)
      : setTimer(callback, STREAM_TICK_MS);
  }

  function scheduleExit() {
    exitTimer = clear(exitTimer);
    exitTimer = setTimer(() => {
      exitTimer = null;
      hasRendered = false;
      setReceiving(false, "exit-hysteresis");
    }, STREAM_EXIT_HYSTERESIS_MS);
  }

  function push(frame) {
    if (disposed) return false;
    const first = !hasRendered;
    pending = frame;
    if (!receiving && enterTimer === null) {
      enterTimer = setTimer(() => {
        enterTimer = null;
        setReceiving(true, "enter-hysteresis");
      }, STREAM_ENTER_HYSTERESIS_MS);
    }
    scheduleExit();
    scheduleRender(first);
    return true;
  }

  // flush 用于可能改变同一消息结构的非 delta 事件：先把尚未提交的正文尾包交给
  // 渲染器，再让调用方提交自己的帧。它不会伪造 terminal，也不会改业务状态。
  function flush(frame) {
    if (frame !== undefined) pending = frame;
    renderTimer = clear(renderTimer);
    return emitPending();
  }

  // terminal 是显式稳定边界：尾包先上屏，再通知调用方提交 terminal 快照；
  // 两个 transition 定时器都取消，之后不会再有迟到的视觉活动状态。
  function terminal(frame) {
    if (disposed) return false;
    renderTimer = clear(renderTimer);
    enterTimer = clear(enterTimer);
    exitTimer = clear(exitTimer);
    emitPending();
    setReceiving(false, "terminal");
    options.onTerminal?.(frame, now());
    return true;
  }

  function reset() {
    renderTimer = clear(renderTimer);
    enterTimer = clear(enterTimer);
    exitTimer = clear(exitTimer);
    pending = null;
    hasRendered = false;
    setReceiving(false, "reset");
  }

  function dispose() {
    reset();
    disposed = true;
  }

  return {
    push,
    flush,
    terminal,
    reset,
    dispose,
    phase: () => (receiving ? "receiving" : "idle"),
    pending: () => pending
  };
}
