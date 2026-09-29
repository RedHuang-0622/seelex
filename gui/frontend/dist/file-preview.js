// ── 文件预览（工作树文件详情查看）──────────────────────────
// 数据源：Bridge.WorkspaceFileContent(relPath, limit)（后端权威读取：
// containment/敏感过滤/上限在 workspace 层保证；原始字节 base64 带回）。
// 渲染按扩展名分派市面组件：
//   - markdown → marked（GFM）→ DOMPurify 消毒 → highlight.js 代码块高亮；
//   - 代码/文本 → highlight.js（关键字高亮，语言缺失自动降级）或纯文本；
//   - PDF → PDF.js（pdfjs-dist，canvas 分页渲染）；
//   - Word(.docx) → docx-preview 排版渲染；.doc 旧格式提示转换；
//   - 图片 → blob URL <img>。
// 安全：文件文本永不直接 innerHTML；marked 输出必须先经 DOMPurify 消毒；
// 高亮只改写代码块内部（textContent → hljs 结果），不改写外层文档。
//
// 多文件详情：预览容器是「多文件详情」容器——每个打开的文件占一枚上标 chip
// （类似网页标签），每枚 chip 对应一个独立面板（切换只切显隐，不重读、不丢
// 滚动位置）；chip 关闭走「空态即生命周期结束」——最后一个 chip 关闭时容器
// 清空并回调 onEmpty，由 app.js 收起抽屉、把子页恢复到原来大小。

import { escapeHtml, icon } from "./components.js";
import { ensureVendorScript } from "./vendor-loader.js";

// 各类型预览读取上限（字节；后端还有 64 MiB 硬钳制）。
export const PREVIEW_LIMITS = {
  markdown: 4 << 20,
  code: 4 << 20,
  text: 4 << 20,
  pdf: 24 << 20,
  word: 24 << 20,
  image: 24 << 20,
  "word-legacy": 24 << 20,
  unsupported: 4 << 20
};

// CODE_EXTENSIONS 是可按代码高亮展示的扩展名集合（预览分派用）。
export const CODE_EXTENSIONS = new Set([
  ".go", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".py", ".java", ".c", ".h",
  ".cc", ".cpp", ".hpp", ".cs", ".rb", ".php", ".rs", ".swift", ".kt", ".kts",
  ".scala", ".sh", ".bash", ".zsh", ".fish", ".ps1", ".sql", ".json", ".jsonc",
  ".xml", ".html", ".htm", ".css", ".scss", ".less", ".vue", ".svelte",
  ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf", ".r", ".lua", ".pl",
  ".dart", ".proto", ".mk"
]);

const TEXT_EXTENSIONS = new Set([
  ".txt", ".log", ".env", ".gitignore", ".gitattributes", ".editorconfig",
  ".properties", ".csv", ".tsv", ".svg", ".lock"
]);

const IMAGE_EXTENSIONS = new Set([
  ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".ico", ".avif"
]);

// 代码语言别名（highlight.js 的语言名；index.html 同步加载的 languages.all.min.js
// 已把全部语言注册进 hljs，所以只要名字对得上就能高亮；未知别名返回 "" →
// 高亮时用 highlightAuto 兜底）。
const LANGUAGE_BY_EXT = {
  ".go": "go", ".js": "javascript", ".mjs": "javascript", ".cjs": "javascript",
  ".ts": "typescript", ".tsx": "typescript", ".jsx": "javascript",
  ".py": "python", ".java": "java", ".c": "c", ".h": "c", ".cc": "cpp",
  ".cpp": "cpp", ".hpp": "cpp", ".cs": "csharp", ".rb": "ruby", ".php": "php",
  ".rs": "rust", ".swift": "swift", ".kt": "kotlin", ".kts": "kotlin",
  ".scala": "scala", ".sh": "bash", ".bash": "bash", ".zsh": "bash",
  ".fish": "bash", ".ps1": "powershell", ".sql": "sql", ".json": "json",
  ".jsonc": "json", ".xml": "xml", ".html": "xml", ".htm": "xml",
  ".css": "css", ".scss": "scss", ".less": "less", ".vue": "xml",
  ".svelte": "xml", ".yaml": "yaml", ".yml": "yaml", ".toml": "ini",
  ".ini": "ini", ".cfg": "ini", ".conf": "ini", ".r": "r", ".lua": "lua",
  ".pl": "perl", ".dart": "dart", ".proto": "protobuf", ".mk": "makefile",
  ".md": "markdown", ".markdown": "markdown", ".mdown": "markdown"
};

const LANGUAGE_BY_NAME = {
  "dockerfile": "dockerfile", "makefile": "makefile",
  "cmakelists.txt": "makefile", "rakefile": "ruby",
  "gemfile": "ruby", "procfile": "ruby", "vagrantfile": "ruby"
};

const MIME_BY_EXT = {
  ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
  ".gif": "image/gif", ".webp": "image/webp", ".bmp": "image/bmp",
  ".svg": "image/svg+xml", ".ico": "image/x-icon", ".avif": "image/avif"
};

// previewKindForPath 按文件路径分派预览类型（纯函数）。
export function previewKindForPath(path = "") {
  const base = String(path).split("/").pop() || "";
  const lower = base.toLowerCase();
  const dot = lower.lastIndexOf(".");
  const ext = dot >= 0 ? lower.slice(dot) : "";
  if (ext === ".md" || ext === ".markdown" || ext === ".mdown") return "markdown";
  if (ext === ".pdf") return "pdf";
  if (ext === ".docx") return "word";
  if (ext === ".doc") return "word-legacy";
  if (IMAGE_EXTENSIONS.has(ext)) return "image";
  if (CODE_EXTENSIONS.has(ext)) return "code";
  if (TEXT_EXTENSIONS.has(ext)) return "text";
  // 无扩展名的常见构建/清单文件按代码处理。
  if (LANGUAGE_BY_NAME[lower] || lower === "dockerfile" || lower === "makefile") return "code";
  return "unsupported";
}

// codeLanguageForPath 返回 hljs 语言别名（可能为 "" → highlightAuto 兜底）。
export function codeLanguageForPath(path = "") {
  const base = String(path).split("/").pop() || "";
  const lower = base.toLowerCase();
  const dot = lower.lastIndexOf(".");
  const ext = dot >= 0 ? lower.slice(dot) : "";
  return LANGUAGE_BY_EXT[ext] || LANGUAGE_BY_NAME[lower] || "";
}

// decodeFileText 把字节解码为展示文本：UTF-8（严格）→ UTF-16 BOM →
// GBK（中文 Windows 常见）→ windows-1252（永不失败兜底）。
export function decodeFileText(bytes, fallbackHint = true) {
  const text = new TextDecoder("utf-8", { fatal: true });
  try {
    return text.decode(bytes);
  } catch {
    /* 非 UTF-8，继续探测 */
  }
  if (bytes.length >= 2) {
    if (bytes[0] === 0xFF && bytes[1] === 0xFE) {
      return new TextDecoder("utf-16le").decode(bytes.slice(2));
    }
    if (bytes[0] === 0xFE && bytes[1] === 0xFF) {
      return new TextDecoder("utf-16be").decode(bytes.slice(2));
    }
  }
  try {
    return new TextDecoder("gbk", { fatal: true }).decode(bytes);
  } catch {
    /* GBK 也不可用/失败 → 最后兜底 */
  }
  return new TextDecoder("windows-1252").decode(bytes);
}

export function base64ToBytes(base64) {
  if (typeof atob !== "function") {
    // node（单测）环境无 atob：Buffer 兜底。
    return Uint8Array.from(Buffer.from(String(base64), "base64"));
  }
  const binary = atob(String(base64));
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}

export function formatPreviewSize(bytes) {
  const value = Number(bytes);
  if (!Number.isFinite(value) || value <= 0) return "";
  const units = ["B", "KB", "MB", "GB"];
  let index = 0;
  let size = value;
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024;
    index += 1;
  }
  return `${index === 0 ? size : size.toFixed(1)} ${units[index]}`;
}

// shouldRejectBinaryPayload 判断截断二进制是否可直接放弃（分页渲染类
// 需要完整文件；文本类截断仍可展示并提示）。
export function needsWholeFile(kind) {
  return kind === "pdf" || kind === "word" || kind === "word-legacy" || kind === "image";
}

// ── 编辑（文件详情面板的写入面）──────────────────────────────
// 「编辑参考 VSCode」：编辑一段缓冲区、**只有 Ctrl+S 才落盘**，退出/关闭前若还有
// 未保存内容就弹窗让用户选保存/不保存/取消。因此这里需要三件纯事实：
//   1. 这个文件能不能编辑（类型 + 是否文本 + 是否被截断 + 编码是否可回写）；
//   2. 编辑器缓冲区与磁盘基线是不是同一份（脏判定）；
//   3. 回写时怎么把编辑器正文还原成原文件的编码事实（EOL / BOM）。
// 全部纯函数，node --test 直接钉住；控制器只做 DOM 与 Bridge 调用。

// EDITABLE_PREVIEW_KINDS：可按文本编辑的预览类型。图片 / PDF / Word / 二进制
// 不在其中——它们的"正文"不是文本，编辑面无从下手。
export const EDITABLE_PREVIEW_KINDS = new Set(["code", "text", "markdown"]);

// canEditPreview 判定一次读取结果能不能进编辑态：
//   - 类型必须是文本类（见上）；
//   - 后端二进制探测必须为文本（text_like=false 一律只读）；
//   - **截断的读取不能编辑**：缓冲区里只有文件前半段，保存会把"看了一半"写成全文。
export function canEditPreview(kind, payload) {
  if (!EDITABLE_PREVIEW_KINDS.has(kind)) return false;
  if (!payload) return false;
  if (payload.truncated) return false;
  return payload.text_like !== false;
}

// decodeEditableText 把字节解成编辑器基线 { text, eol, bom }；**不可回写时返回 null**：
//   - 非 UTF-8（GBK / windows-1252 / UTF-16 都落在这里）：保存时会静默把文件改成
//     UTF-8，那是用户没同意的编码变更，宁可不给编辑入口；
//   - 其余按 UTF-8 解码，记下 BOM 与 EOL 风格（正文一律归一成 LF 供编辑器使用，
//     回写时再由 serializeEditableText 还原）。BOM 从字节判定：UTF-8 解码器会把它吃掉。
export function decodeEditableText(bytes) {
  let text;
  try {
    text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    return null;
  }
  // BOM 只能从**字节**判：TextDecoder("utf-8") 默认把开头的 EF BB BF 当 BOM 吃掉，
  // 解码后的字符串里已经没有 \uFEFF 了（回写时要按字节事实还原）。
  const bom = bytes.length >= 3 && bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf;
  const eol = text.includes("\r\n") ? "\r\n" : "\n";
  return { text: text.replace(/\r\n/g, "\n"), eol, bom };
}

// normalizeEditorText 把任意输入归一成 LF 正文（textarea 的 value 在不同浏览器/
// 平台对换行的处理不完全一致，归一后所有比较都在同一套换行上做）。
export function normalizeEditorText(value) {
  return String(value ?? "").replace(/\r\n?/g, "\n");
}

// serializeEditableText 把编辑器正文还原成落盘字符串：EOL 风格与 BOM 回到原文件
// 的样子（只改内容，不改文件的"编码事实"——否则一次保存会把整个文件的重行风格刷掉，
// diff 里看起来像重写了每一行）。
export function serializeEditableText(value, baseline = {}) {
  const normalized = normalizeEditorText(value);
  const body = baseline.eol === "\r\n" ? normalized.replace(/\n/g, "\r\n") : normalized;
  return (baseline.bom ? "\ufeff" : "") + body;
}

// editDirty 脏判定：编辑器正文与磁盘基线是否不同（换行归一后比较）。
export function editDirty(baselineText, currentText) {
  return normalizeEditorText(currentText) !== normalizeEditorText(baselineText);
}

// baselineDriftNotice 给出"保存后读回"的提示文案：写入成功但读回的内容与写入不一致
// （外部同时改了同一文件、编码回落等），基线必须**以磁盘为准**并如实告诉用户，
// 而不是让编辑器继续相信"我刚写进去的就是磁盘内容"。
export function baselineDriftNotice(writtenText, diskText) {
  return editDirty(writtenText, diskText) ? "保存后读回的内容与写入不一致，已按磁盘内容刷新基线" : "";
}

// ── 多文件详情标签（纯函数）────────────────────────────────
// 上标 chip 条的数据面：一份「已打开文件详情」的有序列表 + 当前激活项。
// 全部纯函数，node --test 直接覆盖，不触碰 DOM / Bridge。

// previewTabLabel 取路径末段作为 chip 标签（无分隔符则原样返回）。
export function previewTabLabel(path = "") {
  const text = String(path);
  const parts = text.split(/[\\/]/).filter(Boolean);
  return parts.length ? parts[parts.length - 1] : text;
}

// normalizePreviewTab 归一化一个待打开的条目；无 path 时返回 null（丢弃）。
export function normalizePreviewTab(entry) {
  if (!entry || typeof entry !== "object" || Array.isArray(entry)) return null;
  const path = typeof entry.path === "string" ? entry.path : "";
  if (!path) return null;
  const name = typeof entry.name === "string" && entry.name ? entry.name : previewTabLabel(path);
  return { path, name };
}

// openPreviewTab 打开一个文件详情：已在列表则原样返回（再次点击同一个文件
// 只是把它激活，不重复读盘、不重排），否则追加到末尾。返回
// { tabs, path, added }。
export function openPreviewTab(tabs, entry) {
  const list = Array.isArray(tabs) ? [...tabs] : [];
  const tab = normalizePreviewTab(entry);
  if (!tab) return { tabs: list, path: "", added: false };
  if (list.some(item => item.path === tab.path)) {
    return { tabs: list, path: tab.path, added: false };
  }
  list.push(tab);
  return { tabs: list, path: tab.path, added: true };
}

// closePreviewTab 关闭一个文件详情，返回 { tabs, active }：active = 关闭后
// 应激活的路径。
//   - 关闭的不是当前激活项 → 保持当前激活项（关掉旁边的文件不打断正在看的）；
//   - 关闭当前项 → 右邻居优先、其次左邻居；
//   - 列表清空 → 空串（容器为空，生命周期结束）。
export function closePreviewTab(tabs, path, activePath = "") {
  const list = Array.isArray(tabs) ? [...tabs] : [];
  const index = list.findIndex(item => item.path === path);
  if (index < 0) return { tabs: list, active: activePath };
  list.splice(index, 1);
  if (list.length === 0) return { tabs: list, active: "" };
  if (path !== activePath && list.some(item => item.path === activePath)) {
    return { tabs: list, active: activePath };
  }
  return { tabs: list, active: list[Math.min(index, list.length - 1)].path };
}

// renderPreviewTabsHTML 渲染上标 chip 条：每枚 chip = 一个已打开的文件详情，
// 尾部一枚关闭按钮。dirtyPaths 里的 chip 缀一枚未保存标记（●）——「编辑过但还没
// Ctrl+S」这件事必须一眼看得见，否则用户关掉抽屉时才发现丢了内容。全部文本 escape。
export function renderPreviewTabsHTML(tabs, activePath = "", dirtyPaths = []) {
  const list = Array.isArray(tabs) ? tabs : [];
  const dirty = new Set(Array.isArray(dirtyPaths) ? dirtyPaths : []);
  return list.map(tab => {
    const active = tab.path === activePath;
    const label = tab.name || previewTabLabel(tab.path);
    const isDirty = dirty.has(tab.path);
    return `<span class="file-preview-chip${active ? " is-active" : ""}${isDirty ? " is-dirty" : ""}" role="tab" aria-selected="${String(active)}" data-preview-tab="${escapeHtml(tab.path)}" title="${escapeHtml(isDirty ? `${tab.path}（未保存）` : tab.path)}" tabindex="${active ? "0" : "-1"}">
        <span class="file-preview-chip-label">${escapeHtml(label)}</span>
        ${isDirty ? `<span class="file-preview-chip-dirty" title="有未保存的修改" aria-label="有未保存的修改">${icon("dot", 10)}</span>` : ""}
        <button type="button" class="file-preview-chip-close" data-preview-tab-close="${escapeHtml(tab.path)}" title="关闭 ${escapeHtml(tab.path)}" aria-label="关闭 ${escapeHtml(tab.path)}">${icon("close", 11)}</button>
      </span>`;
  }).join("");
}

// ── 控制器（DOM 依赖部分）──────────────────────────────────

// createFilePreviewController 管理「多文件详情」容器的完整生命周期：
//   loader(entry, kind, limit) → { base64, size, truncated, text_like }；
//   writer(entry, text) → 落盘（Bridge.WorkspaceWriteFile）；
//   confirmSave({ paths }) → "save" | "discard" | "cancel"（未接弹窗的宿主按"取消"
//   处理：宁可不动作，也不静默丢掉用户刚编辑的内容）。
// 每个文件详情一个独立面板（切换只切显隐，不重读、不丢滚动位置）；写入面板前
// 递增该面板代数，废弃未完成的异步渲染（防串台）。最后一个 chip 关闭时清空
// 容器并回调 onEmpty（app.js 据此收起抽屉、恢复子页原来大小）。
// 没有独立的标题 / 元信息行：文件身份由 chip 标签条（tabsHost）承担。
//
// 编辑语义（与 VS Code 对齐）：**只有 Ctrl+S（或"保存"按钮）才落盘**；进入编辑态
// 的正文与磁盘基线分开两份，脏状态由 editDirty 判定，chip 上以 ● 标出；关闭 chip /
// 关闭抽屉 / 退出编辑时若有未保存内容，先弹窗让用户选保存 / 不保存 / 取消。保存成功
// 后**读回实际文件**并把基线换成读回的那一份——基线只认磁盘，不认"我刚写进去的"。
export function createFilePreviewController({
  view, tabsHost, loader, writer, confirmSave, onSaved, onNotice, onError, onEmpty
}) {
  const tabs = [];
  const panels = new Map(); // path -> record
  let activePath = "";

  // record = {
  //   panel: 面板（显隐单位，含工具栏 + 正文两段）,
  //   el: 正文容器（只读渲染与编辑器都写这里；也是滚动容器）,
  //   toolbar: 编辑/保存动作条,
  //   loaded: { kind, payload, bytes }（最后一次读取/保存后读回的事实）,
  //   baseline: { text, eol, bom } | null（可编辑时的磁盘基线）,
  //   edit: { textarea, status, saving } | null（编辑态）,
  //   rendered: 只读视图是否已是当前内容,
  //   generation / cleanups: 异步渲染防串台与资源回收,
  // }

  if (tabsHost) {
    tabsHost.addEventListener("click", event => {
      const closeButton = event.target?.closest?.("[data-preview-tab-close]");
      if (closeButton) {
        event.preventDefault();
        // 关 chip 前先过脏守卫（有未保存内容就弹窗问）。
        void requestClose(closeButton.dataset.previewTabClose || "");
        return;
      }
      const chip = event.target?.closest?.("[data-preview-tab]");
      if (chip) activateTab(chip.dataset.previewTab || "");
    });
    tabsHost.addEventListener("keydown", event => {
      if (event.target?.closest?.("[data-preview-tab-close]")) return; // 关闭按钮交给原生 click
      const chip = event.target?.closest?.("[data-preview-tab]");
      if (!chip) return;
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        activateTab(chip.dataset.previewTab || "");
        return;
      }
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
      const list = Array.from(tabsHost.querySelectorAll("[data-preview-tab]"));
      const index = list.indexOf(chip);
      if (index < 0) return;
      const step = event.key === "ArrowRight" ? 1 : -1;
      const target = list[(index + step + list.length) % list.length];
      const targetPath = target?.dataset?.previewTab || "";
      if (!targetPath) return;
      event.preventDefault();
      activateTab(targetPath);
      focusChip(targetPath);
    });
  }

  // Ctrl+S = 保存当前激活面板正在编辑的内容（VS Code 口径：落盘只发生在这一个
  // 动作上，不自动保存）。监听挂容器：编辑器 textarea 与动作条按钮的按键都冒泡到这里。
  view?.addEventListener("keydown", event => {
    if (!(event.ctrlKey || event.metaKey) || event.shiftKey || event.altKey) return;
    if (String(event.key || "").toLowerCase() !== "s") return;
    event.preventDefault();
    if (activePath) void saveFile(activePath);
  });

  function focusChip(path) {
    if (!tabsHost) return;
    const chip = Array.from(tabsHost.querySelectorAll("[data-preview-tab]"))
      .find(node => node.dataset.previewTab === path);
    chip?.focus?.();
  }

  // dirtyPathList / isDirtyRecord 是脏状态的唯一判据（chip 的 ●、关闭前的弹窗、
  // 「保存全部」都用它）：编辑器正文与磁盘基线不同即脏。
  function isDirtyRecord(record) {
    if (!record?.edit || !record.baseline) return false;
    return editDirty(record.baseline.text, record.edit.textarea.value);
  }

  function dirtyPathList() {
    const paths = [];
    for (const [path, record] of panels) {
      if (isDirtyRecord(record)) paths.push(path);
    }
    return paths;
  }

  function syncChips() {
    if (!tabsHost) return;
    tabsHost.innerHTML = renderPreviewTabsHTML(tabs, activePath, dirtyPathList());
    tabsHost.classList.toggle("hidden", tabs.length === 0);
  }

  // askDirtyChoice 把"有未保存内容"这件事交给宿主弹窗（保存 / 不保存 / 取消）。
  // 未接弹窗（confirmSave 缺失或返回值不可识别）一律按"取消"：静默丢内容比多问一次
  // 严重得多。
  async function askDirtyChoice(paths) {
    const list = Array.isArray(paths) ? paths.filter(Boolean) : [];
    if (!list.length) return "discard";
    if (typeof confirmSave !== "function") return "cancel";
    const choice = await confirmSave({ paths: [...list] });
    return choice === "save" || choice === "discard" ? choice : "cancel";
  }

  function showActivePanel() {
    for (const [path, record] of panels) {
      record.panel.classList.toggle("hidden", path !== activePath);
    }
  }

  function renderEmptyState() {
    if (!view) return;
    view.classList.add("muted");
    view.innerHTML = '<div class="file-preview-notice">从工作树选择文件后在此查看详情</div>';
  }

  // ensurePanel 建立面板骨架：面板 = 动作条（编辑/保存，按状态显隐）+ 正文容器。
  // 正文容器才是渲染目标与滚动容器（只读渲染函数与编辑器都写它），因此切换编辑态
  // 不会污染/重建另一段结构。
  function ensurePanel(tab) {
    const existing = panels.get(tab.path);
    if (existing) return existing;
    if (tabs.length === 1 && view) {
      // 从空态进入首个详情：清掉占位文本，容器转为「装得下多个详情」的宿主。
      view.innerHTML = "";
      view.classList.remove("muted");
    }
    const panel = document.createElement("div");
    panel.className = "file-preview-panel hidden";
    panel.setAttribute("role", "tabpanel");
    panel.dataset.previewPanel = tab.path;
    const toolbar = document.createElement("div");
    toolbar.className = "file-preview-toolbar";
    toolbar.hidden = true;
    const body = document.createElement("div");
    body.className = "file-preview-panel-body";
    panel.appendChild(toolbar);
    panel.appendChild(body);
    view?.appendChild(panel);
    const record = {
      path: tab.path, panel, el: body, toolbar,
      generation: 0, cleanups: [], loaded: null, baseline: null, edit: null, rendered: false
    };
    panels.set(tab.path, record);
    return record;
  }

  function runCleanups(record) {
    while (record.cleanups.length) {
      const task = record.cleanups.pop();
      try { task(); } catch { /* 忽略单项清理失败 */ }
    }
  }

  function disposePanel(path) {
    const record = panels.get(path);
    if (!record) return;
    record.generation += 1; // 废弃仍在飞行的异步渲染
    runCleanups(record);
    record.panel.remove();
    panels.delete(path);
  }

  // ── 编辑态动作条 ─────────────────────────────────────────

  function setEditStatus(record, text, tone = "") {
    const status = record?.edit?.status;
    if (!status) return;
    status.textContent = text || "";
    status.classList.toggle("is-dirty", tone === "dirty");
    status.classList.toggle("is-failed", tone === "failed");
  }

  function refreshEditStatus(record) {
    if (!record?.edit || record.edit.saving) return;
    if (isDirtyRecord(record)) setEditStatus(record, "未保存", "dirty");
    else setEditStatus(record, "已同步", "");
  }

  function actionButton(label, title, onClick, extraClass = "") {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `file-preview-action${extraClass ? ` ${extraClass}` : ""}`;
    button.textContent = label;
    button.title = title;
    button.addEventListener("click", onClick);
    return button;
  }

  // renderToolbar 按记录状态重绘动作条：只读 + 可编辑 → 「编辑」；编辑态 →
  // 状态行 + 「保存」（Ctrl+S 等价）+ 「完成」；不可编辑/未加载 → 整条隐藏。
  function renderToolbar(record) {
    const toolbar = record?.toolbar;
    if (!toolbar) return;
    toolbar.innerHTML = "";
    if (record.edit) {
      const status = document.createElement("span");
      status.className = "file-preview-edit-status";
      record.edit.status = status;
      toolbar.appendChild(status);
      toolbar.appendChild(actionButton("保存", "保存到文件（Ctrl+S）", () => { void saveFile(record.path); }, "is-primary"));
      toolbar.appendChild(actionButton("完成", "退出编辑（有未保存修改时会先问）", () => { void requestExitEdit(record.path); }));
      toolbar.hidden = false;
      refreshEditStatus(record);
      return;
    }
    if (record.baseline && record.loaded) {
      toolbar.appendChild(actionButton("编辑", "编辑并保存这个文件（Ctrl+S 保存）", () => beginEdit(record.path)));
      toolbar.hidden = false;
      return;
    }
    toolbar.hidden = true;
  }

  // renderPanel 把面板正文切到当前状态：编辑态 = 编辑器；其余按最后一次读取
  // （或保存后读回）的字节重绘只读视图。
  function renderPanel(record) {
    renderToolbar(record);
    if (record.edit) {
      record.el.innerHTML = "";
      record.el.classList.remove("muted");
      record.el.appendChild(record.edit.textarea);
      syncChips();
      return;
    }
    if (record.loaded && !record.rendered) {
      void renderLoaded(record);
    } else {
      syncChips();
    }
  }

  function beginEdit(path) {
    const record = panels.get(path);
    if (!record?.baseline || record.edit || !record.loaded) return false;
    const textarea = document.createElement("textarea");
    textarea.className = "file-preview-editor";
    textarea.spellcheck = false;
    textarea.value = record.baseline.text;
    textarea.setAttribute("aria-label", `${path} 编辑`);
    textarea.addEventListener("input", () => {
      refreshEditStatus(record);
      syncChips();
    });
    record.edit = { textarea, status: null, saving: false };
    // 编辑态不是"只读渲染"：退出编辑时必须按最新字节重绘（否则留在面板里的还是
    // 编辑器的 DOM——一个已经不该存在的 textarea）。
    record.rendered = false;
    renderPanel(record);
    textarea.focus?.();
    return true;
  }

  // saveFile 是唯一的落盘路径（Ctrl+S 与「保存」按钮共用）：
  //   1. 把编辑器正文按原文件的编码事实（EOL/BOM）序列化后交给 writer；
  //   2. **读回实际文件**，基线换成读回的那一份（磁盘为准）；
  //   3. 读回内容与刚写入的不一致（外部并发改动/编码回落）时提示用户。
  async function saveFile(path) {
    const record = panels.get(path);
    if (!record?.edit || !record.baseline || !record.loaded) return false;
    if (record.edit.saving) return false;
    if (typeof writer !== "function") {
      setEditStatus(record, "保存失败：宿主未提供写入面", "failed");
      return false;
    }
    const text = normalizeEditorText(record.edit.textarea.value);
    const outgoing = serializeEditableText(text, record.baseline);
    record.edit.saving = true;
    setEditStatus(record, "保存中…", "");
    try {
      await writer({ path, name: previewTabLabel(path) }, outgoing);
      const kind = record.loaded.kind;
      const limit = PREVIEW_LIMITS[kind] || PREVIEW_LIMITS.text;
      const fresh = await loader({ path, name: previewTabLabel(path) }, kind, limit);
      if (!fresh || !fresh.base64) throw new Error("保存后读回失败：文件内容不可读");
      const freshBytes = base64ToBytes(fresh.base64);
      const nextBaseline = decodeEditableText(freshBytes);
      applyLoaded(record, kind, fresh);
      let notice = "";
      if (nextBaseline) {
        notice = baselineDriftNotice(text, nextBaseline.text);
        record.edit.textarea.value = nextBaseline.text;
      } else {
        // 读回的内容已不是可回写的 UTF-8（外部改成了别的编码）：基线作废，只提示。
        record.baseline = null;
        record.edit.textarea.value = decodeFileText(freshBytes);
        notice = "保存后读回的内容无法按 UTF-8 解码，已按磁盘内容刷新基线（该文件不再可编辑）";
      }
      record.rendered = false;
      record.edit.saving = false;
      refreshEditStatus(record);
      if (notice && onNotice) onNotice(notice);
      if (onSaved) onSaved(path);
      syncChips();
      return true;
    } catch (error) {
      record.edit.saving = false;
      setEditStatus(record, `保存失败：${error?.message || String(error)}`, "failed");
      if (onError) onError(error);
      return false;
    }
  }

  // saveAll 按打开顺序逐个保存脏文件；任一失败即整体判失败（调用方据此不关闭面板，
  // 让用户看得见失败原因）。
  async function saveAll() {
    let ok = true;
    for (const path of dirtyPathList()) {
      if (!(await saveFile(path))) ok = false;
    }
    return ok;
  }

  // requestExitEdit 退出编辑态：有未保存内容先问（保存 / 不保存 / 取消）。
  async function requestExitEdit(path) {
    const record = panels.get(path);
    if (!record?.edit) return;
    if (isDirtyRecord(record)) {
      const choice = await askDirtyChoice([path]);
      if (choice === "cancel") return;
      if (choice === "save" && !(await saveFile(path))) return;
    }
    record.edit = null;
    renderPanel(record);
  }

  // requestClose 关一个 chip：脏则先问（保存失败就不关，让用户看到失败）。
  async function requestClose(path) {
    const record = panels.get(path);
    if (!record) return;
    if (isDirtyRecord(record)) {
      const choice = await askDirtyChoice([path]);
      if (choice === "cancel") return;
      if (choice === "save" && !(await saveFile(path))) return;
    }
    closeTab(path);
  }

  // requestCloseAll 收起整个抽屉前的守卫：返回 false 表示用户取消（或保存失败），
  // 调用方必须原样保留当前状态。
  async function requestCloseAll() {
    const dirty = dirtyPathList();
    if (!dirty.length) return true;
    const choice = await askDirtyChoice(dirty);
    if (choice === "cancel") return false;
    if (choice === "save" && !(await saveAll())) return false;
    return true;
  }

  async function loadInto(tab, record) {
    const generation = ++record.generation;
    runCleanups(record);
    record.loaded = null;
    record.baseline = null;
    record.rendered = false;
    showBusy(record.el);
    const kind = previewKindForPath(tab.path);
    const limit = PREVIEW_LIMITS[kind] || PREVIEW_LIMITS.text;
    try {
      const payload = await loader(tab, kind, limit);
      if (generation !== record.generation) return;
      applyLoaded(record, kind, payload);
      await renderLoaded(record, generation);
    } catch (error) {
      if (generation !== record.generation) return;
      record.rendered = true;
      renderNotice(record.el, `无法预览：${error?.message || String(error)}`);
      if (onError) onError(error);
    }
    renderToolbar(record);
  }

  // applyLoaded 记录一次读取（或保存后读回）的事实：字节、可编辑时的编码基线。
  // 基线是 null 就是"这个文件不给编辑入口"（类型不支持 / 二进制 / 截断 / 非 UTF-8）。
  function applyLoaded(record, kind, payload) {
    const bytes = payload?.base64 ? base64ToBytes(payload.base64) : null;
    record.loaded = { kind, payload, bytes };
    record.baseline = canEditPreview(kind, payload) && bytes ? decodeEditableText(bytes) : null;
    record.rendered = false;
  }

  // renderLoaded 按最后一次读取的字节重绘只读正文（初始加载与"退出编辑"共用；
  // generation 校验保证被更新的一代不会被旧渲染覆盖）。渲染分派本身在
  // renderReadOnlyContent（与「提交内文件内容」视图共用同一套）。
  async function renderLoaded(record, generation = record.generation) {
    const loaded = record.loaded;
    if (!loaded || !loaded.bytes) {
      record.rendered = true;
      renderNotice(record.el, "文件内容为空或不可读");
      return;
    }
    const addCleanup = task => { if (typeof task === "function") record.cleanups.push(task); };
    runCleanups(record);
    try {
      await renderReadOnlyContent(record.el, loaded.payload, {
        path: record.path,
        kind: loaded.kind,
        isCurrent: () => generation === record.generation,
        addCleanup
      });
    } catch (error) {
      if (generation !== record.generation) return;
      renderNotice(record.el, `无法预览：${error?.message || String(error)}`);
      if (onError) onError(error);
    }
    if (generation === record.generation) record.rendered = true;
  }

  async function open(entry) {
    const result = openPreviewTab(tabs, entry);
    if (!result.path) return;
    tabs.length = 0;
    tabs.push(...result.tabs);
    const tab = tabs.find(item => item.path === result.path);
    const record = ensurePanel(tab);
    activePath = tab.path;
    syncChips();
    showActivePanel();
    if (result.added) await loadInto(tab, record);
  }

  function activateTab(path) {
    if (!panels.has(path)) return;
    activePath = path;
    showActivePanel();
    syncChips();
  }

  function closeTab(path) {
    if (!panels.has(path)) return;
    const result = closePreviewTab(tabs, path, activePath);
    tabs.length = 0;
    tabs.push(...result.tabs);
    disposePanel(path);
    if (tabs.length === 0) {
      activePath = "";
      syncChips();
      renderEmptyState();
      if (onEmpty) onEmpty(); // 容器为空 → 生命周期结束，子页恢复原来大小
      return;
    }
    activePath = result.active;
    syncChips();
    showActivePanel();
  }

  // clear 由渲染层在收起抽屉 / 工作区切换时调用：清空全部文件详情，但不回调
  // onEmpty（收起是主动动作，不会递归）。
  function clear() {
    for (const path of Array.from(panels.keys())) disposePanel(path);
    tabs.length = 0;
    activePath = "";
    syncChips();
    renderEmptyState();
  }

  return {
    open,
    activateTab,
    closeTab,
    clear,
    // 编辑面（抽屉与 chip 的守卫都经这里，UI 不自己判脏）：
    edit: beginEdit,
    save: saveFile,
    saveAll,
    exitEdit: requestExitEdit,
    dirtyPaths: dirtyPathList,
    isDirty: path => isDirtyRecord(panels.get(path)),
    requestClose,
    requestCloseAll,
    tabs: () => tabs.slice(),
    active: () => activePath
  };
}

// ── 渲染工具 ───────────────────────────────────────────────

function showBusy(panel) {
  if (!panel) return;
  panel.classList.remove("muted");
  panel.innerHTML = '<div class="file-preview-busy"><span class="tree-loading" aria-hidden="true"></span>正在读取文件…</div>';
}

export function renderNotice(panel, message) {
  if (!panel) return;
  panel.classList.add("muted");
  panel.innerHTML = `<div class="file-preview-notice">${escapeHtml(message)}</div>`;
}

function previewShell(panel, extraClass) {
  if (!panel) return null;
  panel.classList.remove("muted");
  panel.innerHTML = "";
  const content = document.createElement("div");
  content.className = `file-preview-content${extraClass ? ` ${extraClass}` : ""}`;
  panel.appendChild(content);
  return content;
}

function appendTruncatedBanner(content, payloadSizeText) {
  if (!content) return;
  const banner = document.createElement("div");
  banner.className = "file-preview-banner";
  banner.textContent = `内容已截断：仅显示文件前 ${payloadSizeText || "一部分"}`;
  content.prepend(banner);
}

// renderReadOnlyContent 把一份受控字节（Bridge 的 FileContent 形状）渲染进任意
// 只读容器：markdown / 代码 / 文本 / 图片 / PDF / Word / 二进制提示，与「文件详情」
// 面板共用同一条分派。它**只渲染**——没有编辑器、不写盘、不碰标签生命周期与脏状态，
// 因此「提交内文件内容」这类只读视图可以安全地复用它（历史版本不该有任何写入口）。
//
// 调用方给容器与选项：
//
//	options.path             文件路径（分派类型、代码语言、图片 alt）
//	options.kind             已算好的预览类型（省略则按 path 现算）
//	options.isCurrent()      异步渲染（PDF/Word）落地前的一代校验
//	options.addCleanup(task) 资源回收登记（blob URL、渲染任务）
//
// 渲染失败向上抛（调用方决定怎么呈现）；"没有可看的内容"（空文件、二进制、超上限的
// 分页类）由本函数渲染成提示，不算失败。返回实际使用的预览类型。
export async function renderReadOnlyContent(panel, payload, options = {}) {
  const path = String(options.path || "");
  const kind = typeof options.kind === "string" && options.kind ? options.kind : previewKindForPath(path);
  const addCleanup = typeof options.addCleanup === "function" ? options.addCleanup : () => {};
  const isCurrent = typeof options.isCurrent === "function" ? options.isCurrent : () => true;
  const bytes = payload?.base64 ? base64ToBytes(payload.base64) : null;
  if (!bytes || bytes.length === 0) {
    renderNotice(panel, "文件内容为空或不可读");
    return kind;
  }
  const sizeText = formatPreviewSize(payload.size);
  if (payload.truncated && needsWholeFile(kind)) {
    renderNotice(panel, `文件超过 ${sizeText}，暂不支持预览完整内容`);
    return kind;
  }
  const truncated = Boolean(payload.truncated);
  switch (kind) {
    case "markdown":
      renderMarkdown(panel, bytes, truncated);
      break;
    case "code":
      renderHighlighted(panel, bytes, codeLanguageForPath(path), truncated);
      break;
    case "text":
      renderPlainText(panel, bytes, truncated);
      break;
    case "image":
      renderImage(panel, bytes, path, sizeText, addCleanup);
      break;
    case "pdf":
      await renderPDF(panel, bytes, isCurrent, addCleanup);
      break;
    case "word":
      await renderWord(panel, bytes, isCurrent, addCleanup);
      break;
    case "word-legacy":
      renderNotice(panel, ".doc 为旧版 Word 格式，暂无浏览器内预览组件；请用 Word 另存为 .docx 后查看。");
      break;
    default:
      if (payload.text_like === false) {
        renderNotice(panel, "该文件是二进制文件，暂不支持预览。");
      } else {
        renderPlainText(panel, bytes, truncated);
      }
  }
  return kind;
}

function renderPlainText(panel, bytes, truncated) {
  const content = previewShell(panel, "is-plain");
  if (!content) return;
  const text = decodeFileText(bytes);
  const lineCount = text.split("\n").length;
  const pre = document.createElement("pre");
  pre.className = "file-preview-plain";
  pre.textContent = text;
  content.appendChild(pre);
  if (truncated) appendTruncatedBanner(content, formatPreviewSize(bytes.length));
  appendLineCount(panel, lineCount, truncated);
}

function appendLineCount(panel, lines) {
  const footer = document.createElement("div");
  footer.className = "file-preview-line-count";
  footer.textContent = `${lines} 行`;
  if (panel) panel.appendChild(footer);
}

// renderHighlighted 代码文件：textContent 注入 + highlight.js 就地高亮。
function renderHighlighted(panel, bytes, language, truncated) {
  const content = previewShell(panel, "is-code");
  if (!content) return;
  const text = decodeFileText(bytes);
  const pre = document.createElement("pre");
  pre.className = "file-preview-code";
  const code = document.createElement("code");
  if (language) code.className = `language-${escapeHtml(language)}`;
  code.textContent = text;
  pre.appendChild(code);
  content.appendChild(pre);
  highlightElement(code, language);
  if (truncated) appendTruncatedBanner(content, formatPreviewSize(bytes.length));
  appendLineCount(panel, text.split("\n").length);
}

// renderMarkdown markdown 文件：marked 渲染 → DOMPurify 消毒 → 代码块高亮。
// 任一组件缺失时安全降级为纯文本（绝不 innerHTML 未消毒内容）。
export function renderMarkdown(panel, bytes, truncated) {
  const content = previewShell(panel, "is-markdown");
  if (!content) return;
  const text = decodeFileText(bytes);
  const marked = window.marked;
  const purify = window.DOMPurify;
  if (marked && purify) {
    try {
      const raw = marked.parse(text, { gfm: true, breaks: true });
      const html = purify.sanitize(raw, {
        FORBID_TAGS: ["style", "iframe", "object", "embed", "form", "input", "button"],
        FORBID_ATTR: ["style"]
      });
      content.innerHTML = html;
      content.querySelectorAll("pre code").forEach(el => highlightElement(el));
      if (truncated) appendTruncatedBanner(content, formatPreviewSize(bytes.length));
      appendLineCount(panel, text.split("\n").length);
      return;
    } catch { /* 组件异常 → 降级纯文本 */ }
  }
  renderPlainText(panel, bytes, truncated);
}

function highlightElement(codeElement, explicitLanguage) {
  const hljs = window.hljs;
  if (!hljs || !codeElement) return;
  const language = explicitLanguage || parseCodeLanguage(codeElement);
  const source = codeElement.textContent;
  let result = null;
  if (language && hljs.getLanguage && hljs.getLanguage(language)) {
    try { result = hljs.highlight(source, { language, ignoreIllegals: true }); } catch { result = null; }
  }
  if (!result) {
    try { result = hljs.highlightAuto(source); } catch { result = null; }
  }
  if (result && result.value) {
    codeElement.innerHTML = result.value;
    codeElement.classList.add("hljs");
  }
}

function parseCodeLanguage(codeElement) {
  const match = String(codeElement.className || "").match(/language-([\w-]+)/);
  return match ? match[1] : "";
}

function renderImage(panel, bytes, path, sizeText, addCleanup) {
  const ext = (String(path).toLowerCase().match(/\.[a-z0-9]+$/) || [""])[0];
  const mime = MIME_BY_EXT[ext] || "application/octet-stream";
  const url = URL.createObjectURL(new Blob([bytes], { type: mime }));
  const content = previewShell(panel, "is-image");
  const img = document.createElement("img");
  img.className = "file-preview-image";
  img.alt = path || "预览图片";
  img.src = url;
  content.appendChild(img);
  // blob URL 随面板生命周期释放（切换/关闭/重读该文件时回收）。
  if (typeof addCleanup === "function") addCleanup(() => URL.revokeObjectURL(url));
}

// ── PDF（pdfjs-dist）───────────────────────────────────────

const PDF_WORKER_SRC = new URL("./vendor/pdfjs/pdf.worker.min.js", import.meta.url).toString();
// 库本体按需加载（index.html 不再同步加载它）：首次打开 PDF 预览才注入，
// 免得每次启动都为 368KB 的 PDF.js 付 parse/compile 与内部缓存的内存。
const PDF_LIB_SRC = new URL("./vendor/pdfjs/pdf.min.js", import.meta.url).toString();

async function renderPDF(panel, bytes, isCurrent, addCleanup) {
  if (!(window.pdfjsLib || globalThis.pdfjsLib)) {
    try {
      await ensureVendorScript(PDF_LIB_SRC, () => Boolean(window.pdfjsLib || globalThis.pdfjsLib));
    } catch {
      renderNotice(panel, "PDF 查看组件加载失败（vendor/pdfjs）");
      return;
    }
  }
  const pdfjsLib = window.pdfjsLib || globalThis.pdfjsLib;
  if (!pdfjsLib) {
    renderNotice(panel, "PDF 查看组件未加载（vendor/pdfjs 缺失？）");
    return;
  }
  pdfjsLib.GlobalWorkerOptions = pdfjsLib.GlobalWorkerOptions || {};
  if (!pdfjsLib.GlobalWorkerOptions.workerSrc) {
    pdfjsLib.GlobalWorkerOptions.workerSrc = PDF_WORKER_SRC;
  }
  const content = previewShell(panel, "is-pdf");
  if (!content) return;
  const loadingTask = pdfjsLib.getDocument({
    data: bytes,
    isEvalSupported: false,
    disableAutoFetch: true
  });
  const loading = () => { try { loadingTask.destroy(); } catch { /* 已销毁 */ } };
  if (typeof addCleanup === "function") addCleanup(loading);

  const pdf = await loadingTask.promise;
  if (!isCurrent()) return;

  const total = pdf.numPages;
  const header = document.createElement("div");
  header.className = "file-pdf-toolbar";
  header.innerHTML = `
    <button type="button" class="file-pdf-nav" data-pdf="prev" aria-label="上一页">‹</button>
    <span class="file-pdf-page-label">1 / ${total}</span>
    <button type="button" class="file-pdf-nav" data-pdf="next" aria-label="下一页">›</button>`;
  const pages = document.createElement("div");
  pages.className = "file-pdf-pages";
  content.appendChild(header);
  content.appendChild(pages);

  let current = 1;
  let rendering = false;

  async function drawPage(pageNumber) {
    const page = await pdf.getPage(pageNumber);
    if (!isCurrent()) return;
    const base = pdfjsLib.getViewport ? page.getViewport({ scale: 1 }) : page.getViewport(1);
    const availWidth = Math.max(240, (pages.clientWidth || 320) - 24);
    const scale = Math.min(2.5, Math.max(0.5, availWidth / base.width));
    const viewport = page.getViewport({ scale });
    const canvas = document.createElement("canvas");
    canvas.className = "file-pdf-canvas";
    canvas.width = Math.floor(viewport.width);
    canvas.height = Math.floor(viewport.height);
    pages.appendChild(canvas);
    const context = canvas.getContext("2d");
    await page.render({ canvasContext: context, viewport }).promise;
  }

  async function goTo(pageNumber) {
    if (rendering) return;
    rendering = true;
    const target = Math.min(total, Math.max(1, pageNumber));
    header.querySelector(".file-pdf-page-label").textContent = `${target} / ${total}`;
    pages.innerHTML = "";
    try {
      await drawPage(target);
      current = target;
    } catch (error) {
      if (isCurrent()) renderNotice(panel, `PDF 第 ${target} 页渲染失败：${error?.message || error}`);
    } finally {
      rendering = false;
    }
  }

  header.querySelector('[data-pdf="prev"]').addEventListener("click", () => goTo(current - 1));
  header.querySelector('[data-pdf="next"]').addEventListener("click", () => goTo(current + 1));
  await goTo(1);
}

// ── Word（docx-preview）────────────────────────────────────

// docx-preview 同样按需加载（index.html 不再同步加载）。
const DOCX_LIB_SRC = new URL("./vendor/docx-preview/docx-preview.min.js", import.meta.url).toString();

async function renderWord(panel, bytes, isCurrent, addCleanup) {
  if (!(window.docx && typeof window.docx.renderAsync === "function")) {
    try {
      await ensureVendorScript(DOCX_LIB_SRC, () => Boolean(window.docx && typeof window.docx.renderAsync === "function"));
    } catch {
      renderNotice(panel, "Word 查看组件加载失败（vendor/docx-preview）");
      return;
    }
  }
  const docx = window.docx;
  if (!docx || typeof docx.renderAsync !== "function") {
    renderNotice(panel, "Word 查看组件未加载（vendor/docx-preview 缺失？）");
    return;
  }
  const content = previewShell(panel, "is-word");
  if (!content) return;
  if (typeof addCleanup === "function") addCleanup(() => { content.innerHTML = ""; });
  try {
    await docx.renderAsync(bytes, content, null, {
      inWrapper: true,
      breakPages: true,
      ignoreLastRenderedPageBreak: false
    });
  } catch (error) {
    if (isCurrent()) renderNotice(panel, `Word 渲染失败：${error?.message || error}`);
  }
}
