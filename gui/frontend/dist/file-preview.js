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

import { escapeHtml } from "./components.js";

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

// ── 控制器（DOM 依赖部分）──────────────────────────────────

// createFilePreviewController 管理一次文件预览的完整生命周期：
// loader(entry, kind, limit) → { base64, size, truncated, text_like }。
// 打开新文件或 clear() 时递增代数，废弃未完成的异步渲染（防串台）。
export function createFilePreviewController({ view, meta, loader, onError }) {
  let generation = 0;

  function invalidate() {
    generation += 1;
    flushPreviewCleanups();
  }

  async function open(entry) {
    const current = ++generation;
    flushPreviewCleanups();
    if (meta) meta.textContent = entry?.path ? `${entry.path} · ` : "";
    showBusy(view);
    const kind = previewKindForPath(entry?.path || "");
    const limit = PREVIEW_LIMITS[kind] || PREVIEW_LIMITS.text;
    try {
      const payload = await loader(entry, kind, limit);
      if (current !== generation) return;
      if (!payload || !payload.base64) {
        renderNotice(view, "文件内容为空或不可读");
        return;
      }
      const bytes = base64ToBytes(payload.base64);
      const sizeText = formatPreviewSize(payload.size);
      if (meta) meta.textContent = `${entry.path} · ${sizeText}`;
      if (payload.truncated && needsWholeFile(kind)) {
        renderNotice(view, `文件超过 ${sizeText}，暂不支持预览完整内容`);
        return;
      }
      const truncated = Boolean(payload.truncated);
      switch (kind) {
        case "markdown":
          renderMarkdown(view, bytes, truncated);
          break;
        case "code":
          renderHighlighted(view, bytes, codeLanguageForPath(entry.path), truncated);
          break;
        case "text":
          renderPlainText(view, bytes, truncated);
          break;
        case "image":
          renderImage(view, bytes, entry.path, sizeText);
          break;
        case "pdf":
          await renderPDF(view, bytes, () => (current === generation));
          break;
        case "word":
          await renderWord(view, bytes, () => (current === generation));
          break;
        case "word-legacy":
          renderNotice(view, ".doc 为旧版 Word 格式，暂无浏览器内预览组件；请用 Word 另存为 .docx 后查看。");
          break;
        default:
          if (payload.text_like === false) {
            renderNotice(view, "该文件是二进制文件，暂不支持预览。");
          } else {
            renderPlainText(view, bytes, truncated);
          }
      }
    } catch (error) {
      if (current !== generation) return;
      renderNotice(view, `无法预览：${error?.message || String(error)}`);
      if (onError) onError(error);
    }
  }

  function clear() {
    invalidate();
    if (meta) meta.textContent = "";
    renderNotice(view, "从工作树选择文件后在此查看详情");
  }

  return { open, clear };
}

// ── 渲染工具 ───────────────────────────────────────────────

function showBusy(view) {
  if (!view) return;
  view.classList.remove("muted");
  view.innerHTML = '<div class="file-preview-busy"><span class="tree-loading" aria-hidden="true"></span>正在读取文件…</div>';
}

export function renderNotice(view, message) {
  if (!view) return;
  view.classList.add("muted");
  view.innerHTML = `<div class="file-preview-notice">${escapeHtml(message)}</div>`;
}

function previewShell(view, extraClass) {
  if (!view) return null;
  view.classList.remove("muted");
  view.innerHTML = "";
  const content = document.createElement("div");
  content.className = `file-preview-content${extraClass ? ` ${extraClass}` : ""}`;
  view.appendChild(content);
  return content;
}

function appendTruncatedBanner(content, payloadSizeText) {
  if (!content) return;
  const banner = document.createElement("div");
  banner.className = "file-preview-banner";
  banner.textContent = `内容已截断：仅显示文件前 ${payloadSizeText || "一部分"}`;
  content.prepend(banner);
}

function renderPlainText(view, bytes, truncated) {
  const content = previewShell(view, "is-plain");
  if (!content) return;
  const text = decodeFileText(bytes);
  const lineCount = text.split("\n").length;
  const pre = document.createElement("pre");
  pre.className = "file-preview-plain";
  pre.textContent = text;
  content.appendChild(pre);
  if (truncated) appendTruncatedBanner(content, formatPreviewSize(bytes.length));
  appendLineCount(view, lineCount, truncated);
}

function appendLineCount(view, lines) {
  const footer = document.createElement("div");
  footer.className = "file-preview-line-count";
  footer.textContent = `${lines} 行`;
  if (view) view.appendChild(footer);
}

// renderHighlighted 代码文件：textContent 注入 + highlight.js 就地高亮。
function renderHighlighted(view, bytes, language, truncated) {
  const content = previewShell(view, "is-code");
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
  appendLineCount(view, text.split("\n").length);
}

// renderMarkdown markdown 文件：marked 渲染 → DOMPurify 消毒 → 代码块高亮。
// 任一组件缺失时安全降级为纯文本（绝不 innerHTML 未消毒内容）。
export function renderMarkdown(view, bytes, truncated) {
  const content = previewShell(view, "is-markdown");
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
      appendLineCount(view, text.split("\n").length);
      return;
    } catch { /* 组件异常 → 降级纯文本 */ }
  }
  renderPlainText(view, bytes, truncated);
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

function renderImage(view, bytes, path, sizeText) {
  const ext = (String(path).toLowerCase().match(/\.[a-z0-9]+$/) || [""])[0];
  const mime = MIME_BY_EXT[ext] || "application/octet-stream";
  const url = URL.createObjectURL(new Blob([bytes], { type: mime }));
  const content = previewShell(view, "is-image");
  const img = document.createElement("img");
  img.className = "file-preview-image";
  img.alt = path || "预览图片";
  img.src = url;
  content.appendChild(img);
  cleanupBlobOnClose(url);
}

// cleanupBlobOnClose 把 blob URL 注册为视图清理任务（下次 open/clear 时
// flush 释放；beforeunload 兜底防关窗泄漏）。
function cleanupBlobOnClose(url) {
  if (!url) return;
  const release = () => URL.revokeObjectURL(url);
  window.addEventListener("beforeunload", release, { once: true });
  const tracker = window.__seelexPreviewCleanups || (window.__seelexPreviewCleanups = []);
  tracker.push({ url, release });
}

// ── PDF（pdfjs-dist）───────────────────────────────────────

const PDF_WORKER_SRC = new URL("./vendor/pdfjs/pdf.worker.min.js", import.meta.url).toString();

async function renderPDF(view, bytes, isCurrent) {
  const pdfjsLib = window.pdfjsLib || globalThis.pdfjsLib;
  if (!pdfjsLib) {
    renderNotice(view, "PDF 查看组件未加载（vendor/pdfjs 缺失？）");
    return;
  }
  pdfjsLib.GlobalWorkerOptions = pdfjsLib.GlobalWorkerOptions || {};
  if (!pdfjsLib.GlobalWorkerOptions.workerSrc) {
    pdfjsLib.GlobalWorkerOptions.workerSrc = PDF_WORKER_SRC;
  }
  const content = previewShell(view, "is-pdf");
  if (!content) return;
  const loadingTask = pdfjsLib.getDocument({
    data: bytes,
    isEvalSupported: false,
    disableAutoFetch: true
  });
  const loading = () => { try { loadingTask.destroy(); } catch { /* 已销毁 */ } };
  registerCleanup(loading);

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
      if (isCurrent()) renderNotice(view, `PDF 第 ${target} 页渲染失败：${error?.message || error}`);
    } finally {
      rendering = false;
    }
  }

  header.querySelector('[data-pdf="prev"]').addEventListener("click", () => goTo(current - 1));
  header.querySelector('[data-pdf="next"]').addEventListener("click", () => goTo(current + 1));
  await goTo(1);
}

// ── Word（docx-preview）────────────────────────────────────

async function renderWord(view, bytes, isCurrent) {
  const docx = window.docx;
  if (!docx || typeof docx.renderAsync !== "function") {
    renderNotice(view, "Word 查看组件未加载（vendor/docx-preview 缺失？）");
    return;
  }
  const content = previewShell(view, "is-word");
  if (!content) return;
  registerCleanup(() => { if (content) content.innerHTML = ""; });
  try {
    await docx.renderAsync(bytes, content, null, {
      inWrapper: true,
      breakPages: true,
      ignoreLastRenderedPageBreak: false
    });
  } catch (error) {
    if (isCurrent()) renderNotice(view, `Word 渲染失败：${error?.message || error}`);
  }
}

// registerCleanup 注册视图级清理任务（打开新文件或 clear 时执行一次）。
function registerCleanup(task) {
  const tracker = window.__seelexPreviewCleanups || (window.__seelexPreviewCleanups = []);
  tracker.push({ task });
}

// flushPreviewCleanups 由控制器在 invalidate/clear 时调用：释放 blob URL、
// 销毁 PDF loadingTask、清空 docx 容器。
function flushPreviewCleanups() {
  const tracker = window.__seelexPreviewCleanups || [];
  while (tracker.length) {
    const item = tracker.pop();
    try {
      if (item.url) URL.revokeObjectURL(item.url);
      if (item.release) item.release();
      if (item.task) item.task();
    } catch { /* 忽略单项清理失败 */ }
  }
}
