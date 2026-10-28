// message-images.js — 用户消息随行图片的渲染配套（缩略图懒加载 + 失败标记）。
//
// 分工：
//   - components.js 只画结构：`<img data-message-image="<ref>">`，不碰 Bridge；
//   - 本模块负责把 ref 换成**能显示的地址**：一次 RPC 读回字节（base64）拼成
//     data URL 写进 img.src，并按 ref 缓存。
//
// 为什么必须缓存：对话区每次重渲染都会重新扫到同一批节点（流式期间每帧都在
// 重排），没有缓存就会对同一张图反复打 RPC——一张 8 MB 的截图足够把渲染线程
// 拖到掉帧。缓存键是 ref（内容寻址，同一张图永远同一个 ref），因此跨消息、
// 跨重渲染都命中。
//
// 读图能力由应用层注入（registerMessageImageLoader），本模块不 import app.js：
// 这一层只管"节点怎么被填满"，不决定"从哪个会话读"。
//
// 失败不静默：读不回来（图被回收 / 会话已换）时给节点打 `is-missing`，由 CSS
// 画出可辨认的占位块；不抛错、不留一个永远空白的方块。

// LOADER 是当前的读图口：`(ref) => Promise<{ mime_type, data_base64 }>`；
// null 时注水什么都不做（预览测试页 / 未装配宿主）。
let LOADER = null;

// CACHE 是 ref → 状态。状态三种：
//   { state: "loading" }（RPC 在飞，别重复发）
//   { state: "ready", url }（可直接写进 src）
//   { state: "failed", reason }（读不回来，标记占位）
const CACHE = new Map();

// DATA_URL_PREFIXES 给 MIME 兜一个前缀：后端只回裸 base64，前缀（`data:`）
// 由渲染层拼——它才知道自己要塞进哪个上下文。MIME 缺失时按 PNG 兜底（白名单
// 里最常见的一种），绝不用 octet-stream：浏览器不会渲染它。
const FALLBACK_MIME = "image/png";

// CACHE_LIMIT 是缓存条数上限：每条是一个完整 data URL（截图常有数 MB），
// 不设上限的长会话会把渲染进程内存吃满。淘汰按插入序（Map 迭代序）从旧到新，
// 被淘汰的图只丢内存副本，重渲染时会按 ref 重新读回。
const CACHE_LIMIT = 24;

// registerMessageImageLoader 注入读图口（app.js 在启动时装一次）。
export function registerMessageImageLoader(loader) {
  LOADER = typeof loader === "function" ? loader : null;
}

// hydrateMessageImages 给 root 里所有还没填 src 的消息图片注水。
//
// 幂等：已经在飞的不重发，已就绪的直接写 src，失败的打标记。所以调用方可以在
// 每次渲染后无脑调一次（与 hydrateIcons 同款用法）。
export function hydrateMessageImages(root) {
  if (!root || typeof root.querySelectorAll !== "function") return 0;
  const nodes = [...root.querySelectorAll("[data-message-image]")];
  let filled = 0;
  for (const node of nodes) {
    const ref = String(node.dataset?.messageImage || "");
    if (ref === "") continue;
    const entry = CACHE.get(ref);
    if (entry?.state === "ready") {
      if (node.src !== entry.url) node.src = entry.url;
      filled += 1;
      continue;
    }
    if (entry?.state === "failed") {
      markMissing(node, entry.reason);
      continue;
    }
    if (entry?.state === "loading") continue;
    void loadMessageImage(ref, root);
  }
  return filled;
}

// loadMessageImage 发起一次读图，并把结果写回 root 里所有引用同一 ref 的节点。
//
// 注意写回是**再查一次 DOM**：注水期间可能有新行渲染进来（流式追加），这时它们
// 也引用着同一个 ref，应该一起被填满；只填发起时那一个节点会留下空白。
async function loadMessageImage(ref, root) {
  if (LOADER === null) return;
  CACHE.set(ref, { state: "loading" });
  let entry;
  try {
    const payload = await LOADER(ref);
    const base64 = String(payload?.data_base64 ?? "");
    if (base64 === "") {
      entry = { state: "failed", reason: "图片内容为空" };
    } else {
      entry = { state: "ready", url: `data:${mimeOf(payload)};base64,${base64}` };
    }
  } catch (error) {
    entry = { state: "failed", reason: String(error?.message || error || "读取失败") };
  }
  CACHE.set(ref, entry);
  trimCache();
  applyToNodes(root, ref, entry);
}

// trimCache 把缓存收进上限：Map 迭代序 = 插入序，最旧的先走。
//
// 只收"已就绪/已失败"的条目：正在飞的条目由自己的 Promise 负责写回，删掉它会让
// 下一次 hydrate 重复发一次同样的 RPC。
function trimCache() {
  if (CACHE.size <= CACHE_LIMIT) return;
  for (const [ref, entry] of CACHE) {
    if (CACHE.size <= CACHE_LIMIT) break;
    if (entry?.state === "loading") continue;
    CACHE.delete(ref);
  }
}

// applyToNodes 把一条缓存状态刷到当前 DOM 上（含注水期间新长出来的行）。
function applyToNodes(root, ref, entry) {
  const scope = root?.querySelectorAll ? root : null;
  if (!scope) return;
  for (const node of scope.querySelectorAll(`[data-message-image="${cssEscape(ref)}"]`)) {
    if (entry.state === "ready") node.src = entry.url;
    else markMissing(node, entry.reason);
  }
}

// markMissing 给读不回来的图片打失败标记：占位块 + 悬停说明原因。
function markMissing(node, reason) {
  node.dataset.messageImageState = "missing";
  const figure = node.closest?.("[data-message-image-figure]");
  if (figure) {
    figure.dataset.messageImageState = "missing";
    figure.title = `图片读不回来：${String(reason || "未知原因")}`;
  }
}

// mimeOf 取一条图片载荷的 MIME（缺省按 PNG 兜底）。
function mimeOf(payload) {
  const mime = String(payload?.mime_type ?? "").trim().toLowerCase();
  return mime === "" ? FALLBACK_MIME : mime;
}

// cssEscape 给 ref 做属性选择器转义：ref 形如 `media:<hash>`，冒号在 CSS
// 选择器里有含义，不转义会抛错（或用上全局 CSS.escape）。
function cssEscape(value) {
  if (globalThis.CSS?.escape) return globalThis.CSS.escape(value);
  return String(value).replace(/["\\]/g, "\\$&");
}

// clearMessageImageCache 清空缓存（会话切换 / 预览测试重置用）。
//
// 只清内存里的 data URL：图本身在会话媒体分区，清缓存不影响任何持久状态。
export function clearMessageImageCache() {
  CACHE.clear();
}

// messageImageCacheSize 返回当前缓存条数（诊断与单测断言用）。
export function messageImageCacheSize() {
  return CACHE.size;
}
