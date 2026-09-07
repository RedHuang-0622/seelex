// 停靠布局纯函数：五个子页在主视图与右栏之间的分区、排序、激活与置换。
// 会话区子页（对话/轨迹）与右栏子页（状态/工作台/资源管理器）共用同一套
// 视图 id。本文件只做布局演算，不触碰 DOM、localStorage 或 Bridge，
// 渲染与持久化由 app.js 完成，便于 node --test 直接覆盖。
export const DOCK_STORAGE_KEY = "seelex.dock.v1";

export const VIEW_META = Object.freeze({
  conversation: Object.freeze({ label: "对话", session: true }),
  trajectory: Object.freeze({ label: "轨迹", session: true }),
  status: Object.freeze({ label: "状态", session: false }),
  workbench: Object.freeze({ label: "工作台", session: false }),
  code: Object.freeze({ label: "资源管理器", session: false })
});

export const ALL_VIEWS = Object.freeze(Object.keys(VIEW_META));
export const REGIONS = Object.freeze(["main", "right"]);

export const DEFAULT_DOCK_STATE = Object.freeze({
  layout: Object.freeze({
    main: Object.freeze(["conversation", "trajectory"]),
    right: Object.freeze(["status", "workbench", "code"])
  }),
  active: Object.freeze({ main: "conversation", right: "status" })
});

function isView(view) {
  return Object.prototype.hasOwnProperty.call(VIEW_META, view);
}

function defaultRegionFor(view) {
  return DEFAULT_DOCK_STATE.layout.main.includes(view) ? "main" : "right";
}

function pickActive(candidate, list, fallback) {
  if (list.includes(candidate)) return candidate;
  return list.includes(fallback) ? fallback : list[0];
}

// normalizeDockState 保证每个视图恰好出现在一个区、激活页属于所在区；
// 脏数据（缺视图、重复、未知 id、坏 active）一律收敛到合法状态。
export function normalizeDockState(raw = null) {
  const source = raw && typeof raw === "object" ? raw : {};
  const rawLayout = source.layout && typeof source.layout === "object" ? source.layout : {};
  const seen = new Set();
  const layout = { main: [], right: [] };
  for (const region of REGIONS) {
    const list = Array.isArray(rawLayout[region]) ? rawLayout[region] : [];
    for (const view of list) {
      if (isView(view) && !seen.has(view)) {
        layout[region].push(view);
        seen.add(view);
      }
    }
  }
  for (const view of ALL_VIEWS) {
    if (!seen.has(view)) layout[defaultRegionFor(view)].push(view);
  }
  const active = {
    main: pickActive(source.active?.main, layout.main, DEFAULT_DOCK_STATE.active.main),
    right: pickActive(source.active?.right, layout.right, DEFAULT_DOCK_STATE.active.right)
  };
  return { layout, active };
}

export function regionOf(layout, view) {
  if (!isView(view)) return null;
  if (layout.main.includes(view)) return "main";
  if (layout.right.includes(view)) return "right";
  return null;
}

export function isViewActive(dockState, view) {
  if (!isView(view)) return false;
  const region = regionOf(dockState.layout, view);
  return region !== null && dockState.active[region] === view;
}

// swapViews 返回新区：
// - 同区拖到另一页签 = 换序（把 source 移到 target 前），激活页不跟着跳；
// - 跨区拖到另一页签 = 置换（source 落到 target 槽位、target 回到 source
//   槽位），被拖入页成为目标区激活页，原区激活页由移入页接管。
// 非法输入返回原状态（调用方不应持久化任何变化）。
export function swapViews(dockState, sourceRegion, sourceId, targetRegion, targetId) {
  if (!REGIONS.includes(sourceRegion) || !REGIONS.includes(targetRegion)) return dockState;
  if (!isView(sourceId) || !isView(targetId) || sourceId === targetId) return dockState;
  const sourceList = [...dockState.layout[sourceRegion]];
  const targetList = [...dockState.layout[targetRegion]];
  const sourceIndex = sourceList.indexOf(sourceId);
  const targetIndex = targetList.indexOf(targetId);
  if (sourceIndex < 0 || targetIndex < 0) return dockState;
  const layout = { main: [...dockState.layout.main], right: [...dockState.layout.right] };
  const active = { main: dockState.active.main, right: dockState.active.right };
  if (sourceRegion === targetRegion) {
    sourceList.splice(sourceIndex, 1);
    const insertAt = sourceList.indexOf(targetId);
    sourceList.splice(insertAt, 0, sourceId);
    layout[sourceRegion] = sourceList;
    return { layout, active };
  }
  sourceList.splice(sourceIndex, 1, targetId);
  targetList.splice(targetIndex, 1, sourceId);
  layout[sourceRegion] = sourceList;
  layout[targetRegion] = targetList;
  active[sourceRegion] = targetId;
  active[targetRegion] = sourceId;
  return normalizeDockState({ layout, active });
}
