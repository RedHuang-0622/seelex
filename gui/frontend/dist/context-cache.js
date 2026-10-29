// context-cache.js formats the local L1 prefix-reuse projection. It never
// presents these numbers as provider cached_tokens.

export function formatContextCacheChars(value) {
  const chars = Math.max(0, Number(value) || 0);
  if (chars < 1000) return `${Math.round(chars)}`;
  if (chars < 1000000) return `${(chars / 1000).toFixed(1)}k`;
  return `${(chars / 1000000).toFixed(1)}m`;
}

export function contextCacheStatus(cache = null) {
  if (!cache?.available) {
    return { value: "尚未建立", title: "当前会话尚未发生 L1 前缀复用判定" };
  }
  const hits = Math.max(0, Number(cache.hits) || 0);
  const misses = Math.max(0, Number(cache.misses) || 0);
  const attempts = hits + misses;
  const ratio = attempts > 0 ? Math.round((hits / attempts) * 100) : 0;
  const reused = formatContextCacheChars(cache.reused_chars);
  const changed = Array.isArray(cache.last_changed) ? cache.last_changed.filter(Boolean) : [];
  const value = `L1 ${ratio}% · 复用 ${reused} 字符`;
  const facts = [
    `hit=${hits}`,
    `miss=${misses}`,
    `rebuilt=${Number(cache.rebuilt) || 0}`,
    `reused_events=${Number(cache.reused_events) || 0}`,
    `last_reused_chars=${Number(cache.last_reused_chars) || 0}`,
  ];
  if (changed.length) facts.push(`changed=${changed.join(",")}`);
  const key = cache.prefix_key || {};
  facts.push(
    `retained_from=${Number(key.retained_from) || 0}`,
    `compact=${Number(key.compact_seq) || 0}`,
    `tools_epoch=${Number(key.tools_epoch) || 0}`,
  );
  return { value, title: `本地 L1 前缀复用（非 provider usage） · ${facts.join(" · ")}` };
}
