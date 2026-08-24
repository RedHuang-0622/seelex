// 左侧栏纯函数工具：会话置顶（localStorage 持久化）与标题截断。
// 与 DOM 无关，可独立单测；默认存储取 window.localStorage，测试可注入假存储。
export const PIN_STORAGE_KEY = "seelex.pinned-sessions";

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
