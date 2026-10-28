// composer-attachments.js — 输入框图片附件（拖拽 / 粘贴）的纯逻辑。
//
// 只做四件事，DOM 与 Bridge 全留在 app.js：
//   1) **收不收**：MIME 白名单、单张字节上限、一次能挂几张（判据只有这一份）；
//   2) **叫什么**：把 File 的名字 + MIME 规整成落盘名（扩展名必须与 MIME 一致——
//      扩展名是"用系统默认查看器打开"时系统挑程序的唯一线索）；
//   3) **怎么排**：去重（同一张图粘贴两次不该让模型看两遍）、追加、移除；
//   4) **怎么画**：缩略图 chip 的 HTML（不打开的预览：图留在输入框里，点开才交给
//      系统默认查看器）。
//
// 与 file-drop.js / file-reference.js 的分工：那两条链传的是**工作区相对路径文本**，
// 由 agent 自己去读文件；这里传的是**图片字节**，由模型直接看（后端把字节落进
// 会话媒体分区并挂进下一次请求随图）。两个入口都长在 composer 上，绝不互相替代：
// 图片退化成一条路径 = 把"识别"换成"猜"。

// ATTACHMENT_MIME_TYPES 是接受的图片类型（白名单，不是 `image/*` 全放）。
//
// 与后端 backend/tool/io/media 的白名单同一份口径：png/jpeg/webp/gif/bmp 是
// OpenAI 兼容端点确实认的那几种（见 backend/tool/io/attachment/README.md 的真机表）。
// 这里先挡一次只是为了让用户在选文件那一刻就得到答复，不代替后端判据。
export const ATTACHMENT_MIME_TYPES = ["image/png", "image/jpeg", "image/webp", "image/gif", "image/bmp"];

// ATTACHMENT_MAX_BYTES 是单张图片上限（8 MiB），与存储侧 media.max_item_bytes 的
// 默认值对齐：前端先挡住，省掉一次注定被拒的 RPC 往返。
export const ATTACHMENT_MAX_BYTES = 8 << 20;

// ATTACHMENT_LIMIT 是一次能挂在输入框上的图片数量上限。
//
// 为什么要有上限：每张图都随下一次请求发给模型（吃 token），一屏截图堆到十几张
// 时用户自己也没打算全发出去。宁可让他分两轮，也不要一次把上下文塞满。
export const ATTACHMENT_LIMIT = 8;

// ATTACHMENT_MIME_EXTENSIONS 是 MIME → 扩展名（落盘名与展示名共用）。
const ATTACHMENT_MIME_EXTENSIONS = {
  "image/png": ".png",
  "image/jpeg": ".jpg",
  "image/webp": ".webp",
  "image/gif": ".gif",
  "image/bmp": ".bmp"
};

// attachmentMime 归一 MIME：小写、去参数（`image/png;charset=x`）、`image/jpg`
// 归到 `image/jpeg`（浏览器与剪贴板两种写法都存在过）。
export function attachmentMime(type) {
  const raw = String(type ?? "").trim().toLowerCase();
  const base = raw.split(";")[0].trim();
  if (base === "image/jpg") return "image/jpeg";
  return base;
}

// attachmentIsImage 判定一个 MIME 是否属于白名单。
export function attachmentIsImage(type) {
  return ATTACHMENT_MIME_TYPES.includes(attachmentMime(type));
}

// attachmentReject 判定"这条附件收不收"，返回拒绝原因（收下时返回空串）。
// 判据顺序刻意固定：先类型、后大小、最后数量——用户看到的提示应当指向最先出错的
// 那一件事（"这不是图片"比"太多了"更该说）。
export function attachmentReject(file, existing = []) {
  const mime = attachmentMime(file?.type);
  if (!attachmentIsImage(mime)) return "只支持 png / jpeg / webp / gif / bmp 图片";
  const size = Number(file?.size);
  if (Number.isFinite(size) && size > ATTACHMENT_MAX_BYTES) {
    return `图片超过 ${Math.round(ATTACHMENT_MAX_BYTES / (1 << 20))} MB 上限`;
  }
  if ((Array.isArray(existing) ? existing.length : 0) >= ATTACHMENT_LIMIT) {
    return `一次最多附 ${ATTACHMENT_LIMIT} 张图片`;
  }
  return "";
}

// attachmentName 给出提交给后端的落盘名：保留原名主干，扩展名按 MIME 归一。
//
// 为什么必须归一扩展名：截图粘贴常常没有文件名（浏览器给 `image.png`，剪贴板有时
// 干脆给空串），而"用系统默认图片查看器打开"靠扩展名挑程序——落成 `.bin` 就只剩
// 一句"没有关联的应用"。时间戳只在没有名字时兜底，避免覆盖同名文件的心智负担
// （真正的去重是内容寻址，与名字无关）。
export function attachmentName(file, now = Date.now()) {
  const mime = attachmentMime(file?.type);
  const extension = ATTACHMENT_MIME_EXTENSIONS[mime] || "";
  const raw = String(file?.name ?? "").replace(/\\/g, "/").split("/").pop().trim();
  const dot = raw.lastIndexOf(".");
  const stem = (dot > 0 ? raw.slice(0, dot) : raw).replace(/[\\/:*?"<>|\u0000-\u001f]/g, "_").trim();
  if (stem === "") return `screenshot-${stamp(now)}${extension}`;
  return `${stem}${extension}`;
}

// attachmentFiles 从一次 drop / paste 的载荷里挑出**图片文件**（保持原顺序）。
//
// 用途：拖进来的是文件夹压缩包还是 png，判据只看 MIME；非图片留给工作区文件引用
// 那条链（file-drop.js 的 dropPaths）。两条链各自认领自己的载荷，互不吞对方的。
export function attachmentFiles(source) {
  if (!source) return [];
  const files = Array.isArray(source) ? source : Array.from(source);
  return files.filter(file => attachmentIsImage(file?.type));
}

// attachmentAdd 把一条已落盘的图片追加进列表（返回**新**列表，不原地改）。
//
// ref 去重是关键：同一张图粘贴两次时后端按内容寻址给出同一个 ref，队列里却会多出
// 第二条——模型会看到两遍同一张图、白吃一次配额。判据放在这一处，调用方不再各自判。
export function attachmentAdd(list, item) {
  const current = Array.isArray(list) ? list : [];
  const ref = String(item?.ref ?? "");
  if (ref === "") return current.slice();
  if (current.some(existing => existing?.ref === ref)) return current.slice();
  if (current.length >= ATTACHMENT_LIMIT) return current.slice();
  return [...current, item];
}

// attachmentRemove 按 ref 移除一条（返回新列表）；不存在时原样返回副本。
export function attachmentRemove(list, ref) {
  const current = Array.isArray(list) ? list : [];
  const target = String(ref ?? "");
  if (target === "") return current.slice();
  return current.filter(item => item?.ref !== target);
}

// attachmentChipHTML 画一枚附件 chip：缩略图 + 移除按钮。
//
// 「不打开的预览」就是这一枚：图以 `blob:` 缩略图留在输入框里，不抢前台、不弹窗；
// 想细看再点它（`data-attachment-open` → app.js 调 Bridge.OpenComposerImage，交给
// 系统默认图片查看器）。
export function attachmentChipHTML(item) {
  const ref = String(item?.ref ?? "");
  if (ref === "") return "";
  const name = String(item?.name ?? "") || "图片";
  const thumb = String(item?.thumbUrl ?? "");
  const media = thumb === ""
    ? `<span class="composer-attachment-thumb is-blank" aria-hidden="true"></span>`
    : `<img class="composer-attachment-thumb" src="${escapeAttr(thumb)}" alt="" draggable="false" />`;
  return `<figure class="composer-attachment" data-attachment="${escapeAttr(ref)}">`
    + `<button class="composer-attachment-open" type="button" data-attachment-open="${escapeAttr(ref)}"`
    + ` title="点击用系统默认查看器打开 ${escapeAttr(name)}" aria-label="打开图片 ${escapeAttr(name)}">`
    + media
    + `</button>`
    + `<button class="composer-attachment-remove" type="button" data-attachment-remove="${escapeAttr(ref)}"`
    + ` title="移除" aria-label="移除图片 ${escapeAttr(name)}">×</button>`
    + `</figure>`;
}

// attachmentStripHTML 把整份附件列表画成一条 chip 条；空列表返回空串（调用方据此
// 整块收起——空条不该吃高度，更不该吃指针）。
export function attachmentStripHTML(list) {
  return (Array.isArray(list) ? list : []).map(attachmentChipHTML).join("");
}

// attachmentThumbUrl 给一条已落盘附件配上缩略图 URL。
//
// 缩略图用**本地字节**的 objectURL，不是再从后端读一遍：图刚从这个进程里出去，
// 渲染层手上就有原件；也正因为它只是个本地 URL，点开时才必须走后端（渲染层拿不到
// 媒体分区里的真实路径）。create 可注入，便于脱离 DOM 单测。
export function attachmentThumbUrl(file, create = defaultObjectURL) {
  if (!file || typeof create !== "function") return "";
  try {
    return String(create(file) || "");
  } catch {
    return "";
  }
}

// defaultObjectURL 是浏览器里的本地 URL 工厂；没有该 API 时返回空串（chip 退化成
// 一个占位方块——预览缺图，但"点开原图"这条路还在）。
function defaultObjectURL(file) {
  if (typeof URL === "undefined" || typeof URL.createObjectURL !== "function") return "";
  return URL.createObjectURL(file);
}

// stamp 给"没有名字的粘贴图"生成一个可读时间戳（人类看得懂，且不含路径字符）。
function stamp(now) {
  const date = new Date(Number(now) || Date.now());
  const pad = value => String(value).padStart(2, "0");
  return `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}`
    + `-${pad(date.getHours())}${pad(date.getMinutes())}${pad(date.getSeconds())}`;
}

function escapeAttr(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}
