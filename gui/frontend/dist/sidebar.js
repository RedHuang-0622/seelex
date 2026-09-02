// 左侧栏纯函数工具：会话置顶（localStorage 持久化）与标题截断。
// 与 DOM 无关，可独立单测；默认存储取 window.localStorage，测试可注入假存储。
export const PIN_STORAGE_KEY = "seelex.pinned-sessions";
export const TITLE_TAILS_KEY = "seelex.session-title-tails";

function defaultStorage() {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

export function truncateTitle(name, max = 5) {
  const text = String(name == null ? "" : name);
  const chars = Array.from(text); // Unicode 码点迭代（代理对安全）
  if (chars.length <= max) return text;
  return chars.slice(0, max).join("") + "…";
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

export function readPinnedSessions(storage = defaultStorage()) {
  try {
    const parsed = JSON.parse(storage?.getItem(PIN_STORAGE_KEY) || "[]");
    return Array.isArray(parsed) ? parsed.filter(id => typeof id === "string") : [];
  } catch {
    return [];
  }
}

export function writePinnedSessions(ids, storage = defaultStorage()) {
  try {
    storage?.setItem(PIN_STORAGE_KEY, JSON.stringify(ids));
  } catch {
    // 无存储环境（隐私模式/测试）静默忽略
  }
}

export function isPinned(id, storage = defaultStorage()) {
  return readPinnedSessions(storage).includes(String(id));
}

export function togglePinned(id, storage = defaultStorage()) {
  const key = String(id);
  const current = readPinnedSessions(storage);
  const next = current.includes(key)
    ? current.filter(item => item !== key)
    : [...current, key];
  writePinnedSessions(next, storage);
  return next;
}
