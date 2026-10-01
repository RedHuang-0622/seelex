// 会话内 HTML 渲染块的「动作通道」**宿主侧判据**——iframe → 宿主唯一一条被允许的路。
//
// 块本身关在 `sandbox="allow-scripts"`（无 `allow-same-origin`）的 iframe 里：脚本够不到
// 宿主 DOM / storage / Wails bridge，CSP 又断掉了网络与弹窗。所以块内交互默认自娱自乐；
// 要让"画布里的动作"变成宿主动作（填输入框、向本会话发一条请求、复制文本、展开源码），
// 反方向只有 postMessage 一条路。**协议**（标签、两个 data 属性、动作表、注入脚本）住在
// html-embed.js（谁是发送端谁定协议）；本文件是**宿主一侧怎么判**：解析 → 白名单 →
// 载荷上限 → 块级自愿（driving）→ 限流。规则是纯函数，可离线单测
// （embed-bridge.test.mjs）；app.js 只负责把它们接到 `window` 的 message 事件与落点上。
//
// 三条边界，改这块前先读它们：
//   1) **来源身份按 `event.source` 比对**，不按 origin：无 `allow-same-origin` 的
//      iframe 是 opaque origin，`event.origin` 恒为 `"null"`，按 origin 判等于不判。
//      宿主拿 `event.source` 与本页 `.html-embed-frame` 的 contentWindow 逐个比对。
//   2) **动作白名单是封闭集合**（EMBED_ACTION_SPECS）：新增一个动作 = 新增一次能力
//      授予，必须同时写出载荷上限与有没有后果（driving）。
//   3) **有后果的动作（driving）要三道闸门**：块级自愿（围栏 `interactive=1`，渲染时
//      落成 `data-embed-interactive="1"`）+ 块内最近一次**真实**用户手势（帧内脚本按
//      `event.isTrusted` 记时，脚本自己 setTimeout 发的消息过不了这道闸）+ 宿主侧滑动
//      窗口限流与去重（连点、自动重放不给第二次）。

import { EMBED_ACTION_SPECS, EMBED_BRIDGE_TAG } from "./html-embed.js";

// parseEmbedAction 只认协议标签正确、动作名非空的消息；其余一律 null（调用方静默
// 忽略——沙箱外还有别的消息来源，报错刷屏没有意义）。
export function parseEmbedAction(data) {
  if (!data || typeof data !== "object") return null;
  if (data.source !== EMBED_BRIDGE_TAG) return null;
  const action = typeof data.action === "string" ? data.action.trim() : "";
  if (!action) return null;
  return { action, payload: data.payload === undefined ? null : data.payload };
}

// normalizeEmbedPayload 把块内来的载荷收敛成宿主认得的形状：`{ text }` 或 null。
// 允许裸字符串（`data-seelex-payload="解释这个节点"` 不是合法 JSON 时桥会原样透传），
// 也允许 `{ text }` 对象；超过上限**拒绝而不是截断**——把要发给 agent 的话从中间砍掉
// 比不支持更危险（作者会以为整句都送到了）。
export function normalizeEmbedPayload(action, raw) {
  const spec = EMBED_ACTION_SPECS[action];
  if (!spec) return { ok: false, reason: "action-not-allowlisted" };
  if (spec.maxText === 0) return { ok: true, payload: null };
  const candidate = typeof raw === "string" ? raw : (raw && typeof raw === "object" ? raw.text : undefined);
  if (typeof candidate !== "string") return { ok: false, reason: "payload-missing-text" };
  const text = candidate.trim();
  if (!text) return { ok: false, reason: "payload-missing-text" };
  if (text.length > spec.maxText) return { ok: false, reason: "payload-too-long" };
  return { ok: true, payload: { text } };
}

// resolveEmbedAction 是一次跨帧请求的**唯一判据**（顺序即闸门顺序）：
// 协议 → 白名单 → 块级自愿（driving）→ 载荷 → 限流。任一步不过就返回 ok:false，
// 调用方什么都不做（不回执、不提示：块内本来就没有回执通道，安静丢弃最不容易被
// 误读成"宿主没收到"）。
export function resolveEmbedAction({ data, interactive = false, allow } = {}) {
  const parsed = parseEmbedAction(data);
  if (!parsed) return { ok: false, reason: "not-an-embed-message" };
  const spec = EMBED_ACTION_SPECS[parsed.action];
  if (!spec) return { ok: false, reason: "action-not-allowlisted" };
  if (spec.driving && !interactive) return { ok: false, reason: "block-not-interactive" };
  const normalized = normalizeEmbedPayload(parsed.action, parsed.payload);
  if (!normalized.ok) return { ok: false, reason: normalized.reason };
  const decision = { ok: true, action: parsed.action, payload: normalized.payload, driving: spec.driving };
  if (!spec.driving) return decision;
  // fail-closed：有后果的动作没有接限流器时不放行，而不是"没限流就算过"。
  if (typeof allow !== "function") return { ok: false, reason: "gate-missing" };
  const seed = `${parsed.action}\u0000${normalized.payload?.text || ""}`;
  if (!allow(seed)) return { ok: false, reason: "rate-limited" };
  return decision;
}

// createEmbedActionGate 是 driving 动作的宿主侧限流：滑动窗口内最多 limit 次，同一
// 种子（动作 + 正文）在 dedupeMs 内最多一次。它防的不是"用户点得快"，是**块内脚本把
// 一个动作反复发出去**（重放、循环、双击）。窗口与去重都以 `now` 为单位注入，测试
// 不需要真等时间。
export function createEmbedActionGate({ limit = 3, windowMs = 10000, dedupeMs = 1500, now = () => Date.now() } = {}) {
  let hits = [];
  const seen = new Map();
  return {
    allow(seed = "") {
      const at = now();
      hits = hits.filter(stamp => at - stamp < windowMs);
      const previous = seen.get(seed);
      if (previous !== undefined && at - previous < dedupeMs) return false;
      if (hits.length >= limit) return false;
      hits.push(at);
      seen.set(seed, at);
      for (const [key, stamp] of seen) if (at - stamp >= dedupeMs) seen.delete(key);
      return true;
    }
  };
}
