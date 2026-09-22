// 「资源管理器」子页内的三个平级子页（工作树 / 提交记录 / 工作区更改）：
// 顺序、激活与持久化收敛的纯函数。本文件不触碰 DOM、不读 localStorage、不调
// Bridge——读取与落盘由 app.js 的接线完成，便于 node --test 直接覆盖。
export const EXPLORER_STORAGE_KEY = "seelex.right.explorer.v1";
// 旧版把三块面板上下堆叠并允许拖拽换序，顺序记在这个键里（更早的版本只记
// 两项）；现在只作为一次性迁移来源读取，不再写入。
export const LEGACY_PANE_ORDER_KEY = "seelex.right.codePanes";

export const EXPLORER_PAGES = Object.freeze(["worktree", "gitlog", "changes"]);

// 每个子页的 DOM 归属（面板 id / 内容区 id / 计数徽标 id）与展示文案都放在
// 这里：三块面板的 id 只有一份事实，渲染、显隐与刷新失败提示都按它取。
export const EXPLORER_PAGE_META = Object.freeze({
  worktree: Object.freeze({
    label: "工作树",
    panelId: "code-pane-worktree",
    viewId: "worktree-view",
    badgeId: "file-count",
    failedHint: "工作树暂不可用"
  }),
  gitlog: Object.freeze({
    label: "提交记录",
    panelId: "code-pane-gitlog",
    viewId: "git-log-view",
    badgeId: "git-log-count",
    failedHint: "提交记录暂不可用"
  }),
  changes: Object.freeze({
    label: "工作区更改",
    panelId: "code-pane-changes",
    viewId: "changes-view",
    badgeId: "changes-count",
    failedHint: "工作区更改暂不可用"
  })
});

export function isExplorerPage(page) {
  return EXPLORER_PAGES.includes(page);
}

function parseJson(raw) {
  if (typeof raw !== "string" || raw === "") return null;
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

// normalizeExplorerState 把任意输入收敛成合法状态：order 必须是三个子页的一
// 次排列（未知项丢弃、缺失项按默认顺序补齐），active 必须落在 order 里；脏值
// 一律回退默认（回退后的激活页是可见的第一个页签）。
export function normalizeExplorerState(raw = null) {
  const source = raw && typeof raw === "object" ? raw : {};
  const order = [];
  for (const page of Array.isArray(source.order) ? source.order : []) {
    if (isExplorerPage(page) && !order.includes(page)) order.push(page);
  }
  for (const page of EXPLORER_PAGES) {
    if (!order.includes(page)) order.push(page);
  }
  const active = order.includes(source.active) ? source.active : order[0];
  return { order, active };
}

// withExplorerPage 切换激活子页（调用方先确认 page 合法且与当前激活页不同）。
export function withExplorerPage(state, page) {
  const current = normalizeExplorerState(state);
  return { order: current.order, active: page };
}

// serializeExplorerState 产出落盘载荷：始终是收敛后的形状，脏值不会被写回。
export function serializeExplorerState(state) {
  return JSON.stringify(normalizeExplorerState(state));
}

// legacyPaneOrder 解析旧版面板顺序：只有"恰好是三个子页的一次排列"才认（更早
// 的两项版本、重复项、未知 id、坏 JSON 都返回 null，由调用方回退默认顺序）。
export function legacyPaneOrder(raw) {
  const parsed = Array.isArray(raw) ? raw : parseJson(raw);
  if (!Array.isArray(parsed) || parsed.length !== EXPLORER_PAGES.length) return null;
  const order = normalizeExplorerState({ order: parsed }).order;
  return parsed.every((page, index) => page === order[index]) ? order : null;
}

// resolveExplorerState 是 app.js 的唯一读取入口：新键优先；没有新键时消费旧版
// 面板顺序（旧的首块面板 = 旧的第一枚页签/激活页），否则回退默认。
// migrated=true 表示旧键已被消费（含"旧值脏、已回退默认"），可以安全删掉。
export function resolveExplorerState(storedRaw = null, legacyRaw = null) {
  const stored = parseJson(storedRaw);
  if (stored && typeof stored === "object") {
    return { state: normalizeExplorerState(stored), migrated: false };
  }
  const legacy = legacyPaneOrder(legacyRaw);
  if (legacy) return { state: { order: legacy, active: legacy[0] }, migrated: true };
  const hasLegacy = typeof legacyRaw === "string" && legacyRaw !== "";
  return { state: normalizeExplorerState(null), migrated: hasLegacy };
}
