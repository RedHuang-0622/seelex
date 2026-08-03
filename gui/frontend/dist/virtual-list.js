// 基建-B：窗口化列表核心（docs/2026-08-04-context-memory-lifecycle/plan.md §2.4）
// conversation-view 滑动窗口渲染的接缝：只渲染可见区 DOM（虚拟列表），
// 上下哨兵触发加载更早/更新的消息。
//
// 用法：
//   const list = createVirtualList({ rowHeight: 24, viewportHeight: 600, overscan: 8 });
//   list.onScroll(scrollTop);                    // 滚动 → { start, end, offsetY, totalHeight }
//   list.setCount(total);                        // 数据总数变化
//   list.onReachTop(cb) / list.onReachBottom(cb) // 哨兵回调（加载历史/新消息）
//
// 非 happy path 覆盖见 virtual-list.test.mjs。

export function createVirtualList({ rowHeight, viewportHeight, overscan = 8 }) {
  const state = {
    rowHeight: Math.max(1, rowHeight || 24),
    viewport: Math.max(1, viewportHeight || 0),
    overscan: Math.max(0, overscan),
    scrollTop: 0,
    count: 0,
    reachTop: null,
    reachBottom: null,
    topFired: false,
    bottomFired: false
  };

  // visibleRange 计算可见区（含 overscan）：滚动位置 → 首/末行索引。
  // 边界：空列表返回空区间；scrollTop 越界钳制（scrollTop 不可能为负；
  // 超过底部时按总高度钳制）。
  function visibleRange() {
    if (state.count === 0) return { start: 0, end: 0, offsetY: 0, totalHeight: 0 };
    const totalHeight = state.count * state.rowHeight;
    const maxScroll = Math.max(0, totalHeight - state.viewport);
    const top = Math.min(state.scrollTop, maxScroll);
    const start = Math.max(0, Math.floor(top / state.rowHeight) - state.overscan);
    const end = Math.min(state.count, Math.ceil((top + state.viewport) / state.rowHeight) + state.overscan);
    return { start, end, offsetY: start * state.rowHeight, totalHeight };
  }

  // onScroll 滚动入口：更新 scrollTop 并触发哨兵（只触发一次直到反向越过阈值）。
  function onScroll(scrollTop) {
    state.scrollTop = Math.max(0, scrollTop || 0);
    if (state.count === 0) return visibleRange();
    // 上哨兵：接近顶部（首行可见）→ 回调（加载更早历史）。
    const nearTop = state.scrollTop <= state.viewport * 0.5;
    if (nearTop && !state.topFired) {
      state.topFired = true;
      state.bottomFired = false;
      if (state.reachTop) state.reachTop();
    } else if (!nearTop && state.topFired && state.scrollTop > state.viewport) {
      state.topFired = false;
    }
    // 下哨兵：接近底部 → 回调（加载新消息/自动滚动）。
    const totalHeight = state.count * state.rowHeight;
    const nearBottom = totalHeight - (state.scrollTop + state.viewport) <= state.viewport * 0.5;
    if (nearBottom && !state.bottomFired) {
      state.bottomFired = true;
      state.topFired = false;
      if (state.reachBottom) state.reachBottom();
    } else if (!nearBottom && state.bottomFired) {
      state.bottomFired = false;
    }
    return visibleRange();
  }

  // setCount 数据总数变化（新消息追加/历史加载后调用）。
  function setCount(count) {
    state.count = Math.max(0, count || 0);
    return visibleRange();
  }

  function onReachTop(callback) { state.reachTop = callback; return api; }
  function onReachBottom(callback) { state.reachBottom = callback; return api; }

  // scrollTo 外部滚动（恢复位置/锚点；返回新的可见区）。
  function scrollTo(scrollTop) { return onScroll(scrollTop); }

  const api = { onScroll, setCount, onReachTop, onReachBottom, scrollTo };
  return api;
}
