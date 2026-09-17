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

// 代码语言别名（与 dist/vendor/highlightjs/lang 内已 vendor 的语言对应；
// 未知别名返回 "" → 高亮时用 highlightAuto 兜底）。
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
// 尾部一枚关闭按钮。全部文本 escape。
export function renderPreviewTabsHTML(tabs, activePath = "") {
  const list = Array.isArray(tabs) ? tabs : [];
  return list.map(tab => {
    const active = tab.path === activePath;
    const label = tab.name || previewTabLabel(tab.path);
    return `<span class="file-preview-chip${active ? " is-active" : ""}" role="tab" aria-selected="${String(active)}" data-preview-tab="${escapeHtml(tab.path)}" title="${escapeHtml(tab.path)}" tabindex="${active ? "0" : "-1"}">
        <span class="file-preview-chip-label">${escapeHtml(label)}</span>
        <button type="button" class="file-preview-chip-close" data-preview-tab-close="${escapeHtml(tab.path)}" title="关闭 ${escapeHtml(tab.path)}" aria-label="关闭 ${escapeHtml(tab.path)}">${icon("close", 11)}</button>
      </span>`;
  }).join("");
}

// ── 控制器（DOM 依赖部分）──────────────────────────────────

// createFilePreviewController 管理「多文件详情」容器的完整生命周期：
//   loader(entry, kind, limit) → { base64, size, truncated, text_like }。
// 每个文件详情一个独立面板（切换只切显隐，不重读、不丢滚动）；写入面板前
// 递增该面板代数，废弃未完成的异步渲染（防串台）。最后一个 chip 关闭时清空
// 容器并回调 onEmpty（app.js 据此收起抽屉、恢复子页原来大小）。
// 没有独立的标题 / 元信息行：文件身份由 chip 标签条（tabsHost）承担。
export function createFilePreviewController({ view, tabsHost, loader, onError, onEmpty }) {
  const tabs = [];
  const panels = new Map(); // path -> { el, generation, cleanups: [] }
  let activePath = "";

  if (tabsHost) {
    tabsHost.addEventListener("click", event => {
      const closeButton = event.target?.closest?.("[data-preview-tab-close]");
      if (closeButton) {
        event.preventDefault();
        closeTab(closeButton.dataset.previewTabClose || "");
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

  function focusChip(path) {
    if (!tabsHost) return;
    const chip = Array.from(tabsHost.querySelectorAll("[data-preview-tab]"))
      .find(node => node.dataset.previewTab === path);
    chip?.focus?.();
  }

  function syncChips() {
    if (!tabsHost) return;
    tabsHost.innerHTML = renderPreviewTabsHTML(tabs, activePath);
    tabsHost.classList.toggle("hidden", tabs.length === 0);
  }

  function showActivePanel() {
    for (const [path, record] of panels) {
      record.el.classList.toggle("hidden", path !== activePath);
    }
  }

  function renderEmptyState() {
    if (!view) return;
    view.classList.add("muted");
    view.innerHTML = '<div class="file-preview-notice">从工作树选择文件后在此查看详情</div>';
  }

  function ensurePanel(tab) {
    const existing = panels.get(tab.path);
    if (existing) return existing;
    if (tabs.length === 1 && view) {
      // 从空态进入首个详情：清掉占位文本，容器转为「装得下多个详情」的宿主。
      view.innerHTML = "";
      view.classList.remove("muted");
    }
    const el = document.createElement("div");
    el.className = "file-preview-panel hidden";
    el.setAttribute("role", "tabpanel");
    el.dataset.previewPanel = tab.path;
    view?.appendChild(el);
    const record = { el, generation: 0, cleanups: [] };
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
    record.el.remove();
    panels.delete(path);
  }

  async function loadInto(tab, record) {
    const generation = ++record.generation;
    runCleanups(record);
    showBusy(record.el);
    const kind = previewKindForPath(tab.path);
    const limit = PREVIEW_LIMITS[kind] || PREVIEW_LIMITS.text;
    const addCleanup = task => { if (typeof task === "function") record.cleanups.push(task); };
    try {
      const payload = await loader(tab, kind, limit);
      if (generation !== record.generation) return;
      if (!payload || !payload.base64) {
        renderNotice(record.el, "文件内容为空或不可读");
        return;
      }
      const bytes = base64ToBytes(payload.base64);
      const sizeText = formatPreviewSize(payload.size);
      if (payload.truncated && needsWholeFile(kind)) {
        renderNotice(record.el, `文件超过 ${sizeText}，暂不支持预览完整内容`);
        return;
      }
      const truncated = Boolean(payload.truncated);
      switch (kind) {
        case "markdown":
          renderMarkdown(record.el, bytes, truncated);
          break;
        case "code":
          renderHighlighted(record.el, bytes, codeLanguageForPath(tab.path), truncated);
          break;
        case "text":
          renderPlainText(record.el, bytes, truncated);
          break;
        case "image":
          renderImage(record.el, bytes, tab.path, sizeText, addCleanup);
          break;
        case "pdf":
          await renderPDF(record.el, bytes, () => generation === record.generation, addCleanup);
          break;
        case "word":
          await renderWord(record.el, bytes, () => generation === record.generation, addCleanup);
          break;
        case "word-legacy":
          renderNotice(record.el, ".doc 为旧版 Word 格式，暂无浏览器内预览组件；请用 Word 另存为 .docx 后查看。");
          break;
        default:
          if (payload.text_like === false) {
            renderNotice(record.el, "该文件是二进制文件，暂不支持预览。");
          } else {
            renderPlainText(record.el, bytes, truncated);
          }
      }
    } catch (error) {
      if (generation !== record.generation) return;
      renderNotice(record.el, `无法预览：${error?.message || String(error)}`);
      if (onError) onError(error);
    }
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

async function renderPDF(panel, bytes, isCurrent, addCleanup) {
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

async function renderWord(panel, bytes, isCurrent, addCleanup) {
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
