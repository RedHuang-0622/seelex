// 左侧栏纯函数工具：重名消歧编号（渲染期派生的显示逻辑）。会话置顶/别名不再
// 存 localStorage —— 它们属于会话展示元数据，由后端持久化并随快照
// `session.meta` 下发（见 application/core/session_meta.go）。
//
// 标题截断（原 truncateTitle）已删除：会话条目改成「标题段 + ⋯ 段」后，标题段
// 由 CSS 省略号按栏宽截断，完整标题 + 时间 + token 走共享提示气泡；在数据层把
// 标题砍成固定字数会让同前缀的会话无法区分（真机上有多个「继续完善…」）。
export const TITLE_TAILS_KEY = "seelex.session-title-tails";

function defaultStorage() {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

// duplicateSuffix 为重名条目生成序号后缀：total <= 1 返回 ""；否则返回
// " (n)"（n 为 1 基出现序号）。用于左侧栏同名会话/同名项目的消歧。
export function duplicateSuffix(index, total) {
  const n = Number(index);
  const count = Number(total);
  if (!Number.isFinite(n) || n < 1 || !Number.isFinite(count) || count <= 1) return "";
  return ` (${n})`;
}

// ── 会话标题尾号存档（键值对：名字 → 尾号）────────────────────
// 同名会话编号按渲染顺序给：第一个不编号，重复的从 2 开始续。每次渲染后
// 把该名当前用到的最大编号（尾号）持久化，跨重启可查"编到第几号"。

export function readTitleTails(storage = defaultStorage()) {
  try {
    const parsed = JSON.parse(storage?.getItem(TITLE_TAILS_KEY) || "{}");
    if (parsed && typeof parsed === "object") {
      return { ...parsed };
    }
  } catch {
    // 损坏数据回退空存档
  }
  return {};
}

export function writeTitleTails(tails, storage = defaultStorage()) {
  try {
    storage?.setItem(TITLE_TAILS_KEY, JSON.stringify(tails));
  } catch {
    // 无存储环境（隐私模式/测试）静默忽略
  }
}

// titleSuffix 按编号生成显示后缀：1 = 不编号，>1 = " (n)"。
export function titleSuffix(number) {
  const n = Number(number);
  if (!Number.isFinite(n) || n <= 1) return "";
  return ` (${n})`;
}
