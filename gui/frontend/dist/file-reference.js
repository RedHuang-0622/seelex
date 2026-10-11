// file-reference.js — 「路径引用」的唯一载体（2026-10-12 J-1b）
//
// 用户口径（docs/2026-10-12-frontend-consistency-and-fixes/README.md §3 J-1b，
// §7.1 第 5 条）：`@文件` / composer 的 `+` 加号 / 右栏工作树的拖拽**三个入口产出
// 同一条引用** —— **工作区相对路径**。引用**不是内容、不是字节**：正文里就是路径
// 文本（提交后一个字都不改），接到它的 agent 自己用 `read_file` / `grep` 去读。
//
// 于是本文件只管三件事，三个入口共用同一份实现：
//   1) **路径判据**：唯一归一化实现是 `file-drop.js` 的 `normalizeDropPath`（拖拽入口
//      本来就用它）。这里把它以 `normalizeFilePath` 的名字**原样转发**（同一个函数
//      引用，测试可断言 identity），不再复制第二份判据——否则"什么算合法路径"会出现
//      两套口径（绝对路径 / `..` / 超长各判一次，迟早不一致）。
//   2) **候选**：从已加载的工作区树条目里筛路径（前缀/子串收敛、目录在前、有上限）。
//      纯函数：不读盘、不碰 DOM、不调 Bridge。
//   3) **渲染**：**同一枚 chip**（`fileReferenceChipHTML`），两种宿主 —— composer
//      草稿区（带删除）与对话正文（点击开右栏详情）。**识别只发生在渲染层**：正文里的
//      路径文本一个字都不改（agent 看到的是路径本身）。
//
// 识别两种形态（composer 草稿条的 chip 与正文 chip 用同一套识别）：
//   - `@<路径>`：`@文件` / `+` 插入的形态（`@` 只是标记，不参与路径）。目录引用会带上
//     尾斜杠（`@gui/`）——用户从选择器点目录时就是这个形状；
//   - **裸路径**：拖拽入口落的「读取工作树文件：\n- src/a.js」里的那一条。
//
// 判定刻意**保守**：宁可漏认（退化成纯文本，无害：agent 照样能看到路径），也不把普通
// 词认成文件引用（`@someone`、「and/or」渲染成一枚点不开的 chip 才是真错）。
//
// 与引擎/后端的关系：**零**。`@` 不是输入前缀（J-1a 已把团队召唤删掉，后端 `sigils`
// 表只剩 / # $），引用就是正文的一部分，所以这里没有 sigil 语义、没有新 RPC、没有新
// 角色（不新增 `reader`、不搬内容字节）。

import { normalizeDropPath } from "./file-drop.js";

// FILE_REF_TRIGGER 是引用的标记字符（`@`）。它**只在前端有语义**：后端不认它是前缀。
// 单测据此钉住"三入口同一载体"的文本形状。
export const FILE_REF_TRIGGER = "@";

// FILE_REF_CANDIDATE_LIMIT 是一次列出的候选上限（内联面板与 `+` 选择器同值）。
export const FILE_REF_CANDIDATE_LIMIT = 8;

// FILE_REF_INDEX_LIMIT 是候选索引的条目上限：工作区再大也不让一次列举把内存/渲染
// 拖垮（索引本身有界，选择器只是对它做筛）。
export const FILE_REF_INDEX_LIMIT = 4000;

// normalizeFilePath 是文件引用的**唯一**路径判据（与拖拽入口同一个函数）。
export const normalizeFilePath = normalizeDropPath;

// fileReferenceIndex 把一次工作树列举（`dto.TreeListing.entries`）压成候选索引：
// 只留能通过归一化的条目（路径必须是工作区相对路径）、按路径去重、**目录在前**
// （引用目录是"读这个目录"的合法意图，排前面便于用 `@dir/` 收窄），最后截到上限。
export function fileReferenceIndex(entries, limit = FILE_REF_INDEX_LIMIT) {
  const seen = new Set();
  const dirs = [];
  const files = [];
  for (const entry of Array.isArray(entries) ? entries : []) {
    if (!entry || typeof entry !== "object") continue;
    const path = normalizeFilePath(entry.path);
    if (path === "" || seen.has(path)) continue;
    seen.add(path);
    (entry.type === "dir" ? dirs : files).push({ path, dir: entry.type === "dir" });
  }
  return [...dirs, ...files].slice(0, Math.max(0, Math.trunc(limit)));
}

// fileReferenceCandidates 在已加载的索引里筛候选：**子串**命中（用户常只记得中间一段，
// 如 `conversa` 命中 `gui/frontend/dist/conversation-wheel.js`），大小写不敏感；空查询
// 返回索引头部（目录在前）。命中顺序 = 索引顺序（稳定），上限 8 条。纯函数。
export function fileReferenceCandidates(index, query, limit = FILE_REF_CANDIDATE_LIMIT) {
  const source = Array.isArray(index) ? index : [];
  const needle = String(query ?? "").trim().toLowerCase();
  const hits = [];
  for (const item of source) {
    if (!item || typeof item.path !== "string") continue;
    if (needle !== "" && !item.path.toLowerCase().includes(needle)) continue;
    hits.push(item);
    if (hits.length >= limit) break;
  }
  return hits;
}

// fileReferenceQuery 取出"光标所在的那个 `@` 词"：`@` 必须开启这个词（前面是行首或
// 空白），`@` 之后没有空白。返回 {query, start, end}，区间覆盖**整枚 token（含 `@`、
// 含光标之后的剩余字符）**——这样光标停在词中间按 Tab 也是替换整枚引用，不会留下
// `@src/app` 的尾巴。不在 `@` 词里返回 null（调用方据此只做插入、不吞句子）。
export function fileReferenceQuery(text, cursor) {
  const source = String(text ?? "");
  const rawCursor = cursor === undefined || cursor === null ? source.length : Number(cursor) || 0;
  const at = Math.max(0, Math.min(rawCursor, source.length));
  let start = at;
  while (start > 0 && !isSpace(source[start - 1])) start -= 1;
  if (!source.slice(start, start + 1).startsWith(FILE_REF_TRIGGER)) return null;
  let end = at;
  while (end < source.length && !isSpace(source[end])) end += 1;
  return { query: source.slice(start + 1, end), start, end };
}

// applyFileReference 把引用写成**正文文本**：光标在 `@` 词里 → 整枚 token 换成
// `@<路径> `（`+` 选择器与 `@` 内联面板走同一条）；光标不在 `@` 词里 → 只在光标处插入
// `@<路径> `，**不覆盖用户正在写的句子**。非法路径（绝对路径 / `..` / 超长）什么都不做
// （ok=false），由调用方提示——不猜、不回退到文件名。
export function applyFileReference(text, cursor, rawPath) {
  const source = String(text ?? "");
  const at = Math.max(0, Math.min(Number(cursor) || 0, source.length));
  const path = normalizeFilePath(rawPath);
  if (path === "") return { ok: false, text: source, cursor: at, path: "" };
  const insertion = `${FILE_REF_TRIGGER}${path} `;
  const token = fileReferenceQuery(source, cursor);
  if (!token) {
    return { ok: true, path, cursor: at + insertion.length, text: source.slice(0, at) + insertion + source.slice(at) };
  }
  // 引用后面紧跟的那一个空格一并吃掉：插入串自带尾空格，不吞会留下两个空格。
  const end = isSpace(source[token.end]) ? token.end + 1 : token.end;
  return {
    ok: true,
    path,
    cursor: token.start + insertion.length,
    text: source.slice(0, token.start) + insertion + source.slice(end)
  };
}

// REFERENCE_SCAN 扫两种形态：可选的前缀 boundary（不得是词字符或 `@`/路径字符，避免把
// `user@example.com`、`a/b` 这类粘连词切进来），可选的 `@` 标记，再是一段路径字符。
const REFERENCE_SCAN = /(^|[^\w@/.-])(@?)([A-Za-z0-9_][A-Za-z0-9_./-]*)/g;

// fileReferenceTokens 扫出文本里的全部引用（按出现顺序），返回
// {path, start, end, form, dir}：`start/end` 是**原文里的 token 区间**（`@` 形态含
// `@`，目录形态含尾斜杠），`path` 是归一化后的工作区相对路径。渲染层与草稿条都用它。
export function fileReferenceTokens(text) {
  const source = String(text ?? "");
  if (source === "") return [];
  const tokens = [];
  for (const match of source.matchAll(REFERENCE_SCAN)) {
    const boundary = match[1] || "";
    const marked = match[2] === FILE_REF_TRIGGER;
    const raw = match[3];
    const dir = marked && raw.endsWith("/");
    const trimmed = trimTrailingNoise(raw);
    if (trimmed === "") continue;
    const path = normalizeFilePath(trimmed);
    if (path === "" || !looksLikePath(path, marked, dir)) continue;
    const start = match.index + boundary.length;
    const end = start + trimmed.length + (marked ? FILE_REF_TRIGGER.length : 0);
    tokens.push({ path, start, end, form: marked ? "at" : "bare", dir });
  }
  return tokens;
}

// fileReferencePaths 返回文本里引用到的路径（去重、保序）——composer 草稿条的 chip 集合。
export function fileReferencePaths(text) {
  const paths = [];
  for (const token of fileReferenceTokens(text)) {
    if (!paths.includes(token.path)) paths.push(token.path);
  }
  return paths;
}

// removeFileReference 删掉一枚引用：命中第一处同路径的 token（含 `@` 形态的尾随空格），
// 其余文本原样保留。删除只在草稿上发生（正文已发出，不可改）。
export function removeFileReference(text, rawPath) {
  const source = String(text ?? "");
  const path = normalizeFilePath(rawPath);
  if (path === "") return source;
  const token = fileReferenceTokens(source).find(candidate => candidate.path === path);
  if (!token) return source;
  let end = token.end;
  if (token.form === "at" && isSpace(source[end])) end += 1;
  return source.slice(0, token.start) + source.slice(end);
}

// fileReferenceChipHTML **就是那一枚 chip**：两种宿主共用（`host` 只决定删除按钮与数据
// 属性的去留）。chip 只是渲染层的识别——标签文本就是路径本身，正文里的原文不受影响。
export function fileReferenceChipHTML(rawPath, options = {}) {
  const path = normalizeFilePath(rawPath);
  if (path === "") return "";
  const host = options.host === "composer" ? "composer" : "message";
  const dir = Boolean(options.dir);
  const label = dir ? `${path}/` : path;
  const removable = host === "composer" ? '<span class="file-ref-remove" data-file-ref-remove aria-hidden="true">×</span>' : "";
  return `<button type="button" class="file-ref-chip is-${host}${dir ? " is-dir" : ""}" data-file-ref="${escapeAttr(path)}" data-file-ref-host="${host}"${dir ? ' data-file-ref-dir="1"' : ""} title="${escapeAttr(label)}（引用工作区相对路径，内容由 agent 自己读）"><span class="file-ref-mark" aria-hidden="true">${FILE_REF_TRIGGER}</span><span class="file-ref-path">${escapeAttr(label)}</span>${removable}</button>`;
}

// fileReferenceStripHTML 把一整段草稿渲染成 chip 条（composer 宿主）。没有引用时返回
// 空串，调用方据此整块收起。
export function fileReferenceStripHTML(text, options = {}) {
  const host = options.host === "composer" ? "composer" : "message";
  return fileReferenceTokens(text)
    .map(token => fileReferenceChipHTML(token.path, { host, dir: token.dir }))
    .join("");
}

// trimTrailingNoise 去掉路径尾巴上的标点噪声：`src/a.js.`、`src/a.js/`、`src/a.js-`。
// 不这么剪会把句末的句号一起吃进路径（读不到文件）。
function trimTrailingNoise(raw) {
  let value = raw;
  while (value !== "" && /[.\-/]$/.test(value)) value = value.slice(0, -1);
  return value;
}

// looksLikePath 是保守的"这看起来像路径吗"判据：
//   - `@` 形态：含 `/` 或 `.`（`@src/app.js`、`@README.md`），或显式目录形态（`@gui/`）
//     ——单纯的 `@someone` 没有路径外形，不认；
//   - 裸形态：必须含 `/` 且末段带扩展名（`src/a.js`）：`and/or`、「前后」之类不认，
//     一整段散文也不会被整块吃掉。
function looksLikePath(path, marked, dir) {
  if (marked) return dir || path.includes("/") || path.includes(".");
  const segments = path.split("/");
  const last = segments[segments.length - 1] || "";
  return segments.length > 1 && /\.[A-Za-z0-9]+$/.test(last);
}

function isSpace(value) {
  return value === " " || value === "\t" || value === "\n" || value === "\r";
}

function escapeAttr(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}
