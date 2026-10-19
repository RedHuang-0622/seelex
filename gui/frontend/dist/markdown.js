import { fileReferenceChipHTML, fileReferenceTokens } from "./file-reference.js";
import { isHtmlEmbedLanguage, renderHtmlEmbed } from "./html-embed.js";

const SAFE_PROTOCOLS = new Set(["http:", "https:", "mailto:"]);
const BLOCK_PATTERN = /^(?: {0,3}(?:#{1,6}\s+|>|```|~~~|<think>\s*)|\s*(?:[-+*]|\d+[.)])\s+|\s*(?:-{3,}|\*{3,}|_{3,})\s*$)/i;
const REASONING_OPEN = /^ {0,3}<think>\s*/i;
const REASONING_CLOSE = /<\/think>/i;

// renderMarkdown 渲染一段 markdown。options.fileRefs = true 时额外把**用户附的文件
// 引用**（`@路径` / 裸路径，见 file-reference.js）渲染成 chip —— 只有 user 消息的正文
// 走这条（components.js renderMessage），assistant/系统正文保持原样：引用是"用户附了
// 哪个文件"的表述，渲染层识别它，正文文本一个字都不改。
export function renderMarkdown(value = "", options = {}) {
  const source = String(value).replace(/\r\n?/g, "\n");
  if (!source) return "";
  // 渲染块序号只在**一次 renderMarkdown 内**计数：沙箱帧的身份是
  // `<行 key>#<序号>`（html-embed.js 的 data-embed-key）。同一条消息里第 n 个
  // seelex-html 围栏每次重渲染都拿到同一个 key，帧才能被 reconcile 认回来并复用；
  // 序号按消息算而不是按围栏内容算——流式期间围栏内容每帧都在变，内容算不出稳定身份。
  const context = { ...options, embed: { rowKey: String(options.rowKey ?? ""), index: 0 } };
  return renderBlocks(source.split("\n"), context);
}

function renderBlocks(lines, options) {
  const output = [];
  let index = 0;
  while (index < lines.length) {
    if (!lines[index].trim()) {
      index += 1;
      continue;
    }
    const renderer = selectBlockRenderer(lines, index);
    const block = renderer(lines, index, options);
    output.push(block.html);
    index = block.next;
  }
  return output.join("\n");
}

function selectBlockRenderer(lines, index) {
  const line = lines[index];
  if (/^ {0,3}(`{3,}|~{3,})/.test(line)) return renderFence;
  if (REASONING_OPEN.test(line)) return renderReasoning;
  if (/^ {0,3}#{1,6}\s+/.test(line)) return renderHeading;
  if (isHorizontalRule(line)) return renderHorizontalRule;
  if (/^ {0,3}>/.test(line)) return renderQuote;
  if (matchListItem(line)) return renderList;
  if (isTableStart(lines, index)) return renderTable;
  return renderParagraph;
}

// ── mermaid 围栏 ─────────────────────────────────────────
// 语言标记为 `mermaid` 的围栏走**图表容器**（不是裸代码块）：容器里始终先渲染一份
// 源码代码块（`pre.mermaid-source > code.language-mermaid`），画布初始为空且 hidden。
// 库未就绪 / 超时 / 渲染失败 / 流式中间态都退回这份源码块——**不会白屏**。真正的
// 渲染（懒加载 vendor + DOMPurify 白名单放行 SVG）在 mermaid-diagram.js: hydrateMermaid。
const MERMAID_LANGUAGE = "mermaid";

export function isMermaidLanguage(language) {
  return String(language || "").trim().toLowerCase() === MERMAID_LANGUAGE;
}

// mermaidBlockHTML 把一段 mermaid 源码包成图表容器（源码转义后放进代码块）。
export function mermaidBlockHTML(source) {
  return mermaidBlockFromEscaped(escapeHtml(source));
}

// rewriteMermaidFences 把 marked 产出的
// `<pre><code class="language-mermaid">…</code></pre>` 换成同一套容器（文件预览的
// markdown 走 marked，围栏形状固定）。正文已是 HTML 转义态，原样搬进 `<code>`——
// 纯字符串改写，不解析 DOM，可单测；与 mermaidBlockHTML 共用同一份结构。
export function rewriteMermaidFences(html) {
  return String(html || "").replace(
    /<pre><code class="language-mermaid">([\s\S]*?)<\/code><\/pre>/gi,
    (_match, escaped) => mermaidBlockFromEscaped(escaped)
  );
}

function mermaidBlockFromEscaped(escaped) {
  return `<div class="mermaid-diagram" data-mermaid-diagram>`
    + `<div class="mermaid-canvas" data-mermaid-canvas hidden></div>`
    + `<pre class="mermaid-source"><code class="language-${MERMAID_LANGUAGE}">${escaped}</code></pre>`
    + `</div>`;
}

function renderFence(lines, index, options) {
  const opening = lines[index].match(/^ {0,3}(`{3,}|~{3,})\s*([^\s`~]+)?\s*(.*)$/);
  if (!opening) return renderParagraph(lines, index, options);
  const marker = opening[1];
  const closePattern = new RegExp(`^ {0,3}${escapeRegExp(marker[0])}{${marker.length},}\\s*$`);
  const body = [];
  let cursor = index + 1;
  while (cursor < lines.length && !closePattern.test(lines[cursor])) {
    body.push(lines[cursor]);
    cursor += 1;
  }
  if (cursor < lines.length) cursor += 1;
  const language = opening[2] || "";
  // 显式标记的块走沙箱 iframe 渲染（见 html-embed.js 的安全边界）；
  // 普通 ```html 仍是源码块，避免既有会话里的代码片段被突然执行。
  if (isHtmlEmbedLanguage(language)) {
    return { html: renderHtmlEmbed(body.join("\n"), opening[3] || "", { key: nextEmbedKey(options) }), next: cursor };
  }
  // mermaid 围栏 → 图表容器（源码块 + 空画布；渲染由 mermaid-diagram.js 接管，
  // 失败/未就绪退回源码块，见 mermaidBlockHTML）。
  if (isMermaidLanguage(language)) {
    return { html: mermaidBlockHTML(body.join("\n")), next: cursor };
  }
  const languageClass = language ? ` class="language-${escapeAttribute(language)}"` : "";
  return { html: `<pre><code${languageClass}>${escapeHtml(body.join("\n"))}</code></pre>`, next: cursor };
}

// nextEmbedKey 给本次渲染里的第 n 个 seelex-html 围栏一个稳定身份 `<行 key>#<n>`。
// 没有行 key（独立的 markdown 渲染：测试、预览）时返回空串，让 html-embed.js 退回
// 内容哈希——宁可身份弱一点，也不要把"没有行"的帧硬绑到同一个 key 上。
function nextEmbedKey(options) {
  const state = options && (options.embed || (options.embed = { rowKey: "", index: 0 }));
  if (!state) return "";
  const index = Number(state.index) || 0;
  state.index = index + 1;
  const rowKey = String(state.rowKey ?? "").trim();
  return rowKey ? `${rowKey}#${index}` : "";
}

function renderReasoning(lines, index, options) {
  const body = [];
  let cursor = index;
  let line = lines[cursor].replace(REASONING_OPEN, "");
  let closed = false;
  let tail = "";
  while (cursor < lines.length) {
    const closing = line.match(REASONING_CLOSE);
    if (closing) {
      body.push(line.slice(0, closing.index));
      tail = line.slice(closing.index + closing[0].length);
      closed = true;
      cursor += 1;
      break;
    }
    body.push(line);
    cursor += 1;
    line = lines[cursor] || "";
  }
  const content = body.join("\n").trim();
  const inner = content ? renderBlocks(content.split("\n"), options) : '<p class="reasoning-empty">暂无思考内容</p>';
  const live = closed ? "" : ' open class="reasoning-block is-streaming"';
  const details = closed ? '<details class="reasoning-block">' : `<details${live}>`;
  const state = closed ? "" : '<span class="reasoning-state">LIVE</span>';
  const trailing = tail.trim() ? renderBlocks([tail], options) : "";
  return {
    html: `${details}<summary><span class="reasoning-chevron" aria-hidden="true"></span><span>思考过程</span>${state}</summary><div class="reasoning-content">${inner}</div></details>${trailing}`,
    next: cursor
  };
}

function renderHeading(lines, index, options) {
  const match = lines[index].match(/^ {0,3}(#{1,6})\s+(.+?)\s*#*\s*$/);
  const level = match[1].length;
  return { html: `<h${level}>${renderInline(match[2], options)}</h${level}>`, next: index + 1 };
}

function renderHorizontalRule(_lines, index) {
  return { html: "<hr>", next: index + 1 };
}

function renderQuote(lines, index, options) {
  const quoted = [];
  let cursor = index;
  while (cursor < lines.length) {
    const match = lines[cursor].match(/^ {0,3}>\s?(.*)$/);
    if (!match) break;
    quoted.push(match[1]);
    cursor += 1;
  }
  return { html: `<blockquote>${renderBlocks(quoted, options)}</blockquote>`, next: cursor };
}

function renderList(lines, index, options) {
  const first = matchListItem(lines[index]);
  const tag = first.ordered ? "ol" : "ul";
  const items = [];
  let cursor = index;
  while (cursor < lines.length) {
    const item = matchListItem(lines[cursor]);
    if (!item || item.ordered !== first.ordered) break;
    const task = item.content.match(/^\[([ xX])\]\s+(.*)$/);
    const checkbox = task ? `<input type="checkbox" disabled${task[1].toLowerCase() === "x" ? " checked" : ""}>` : "";
    items.push(`<li${task ? ' class="task-item"' : ""}>${checkbox}${renderInline(task ? task[2] : item.content, options)}</li>`);
    cursor += 1;
  }
  const start = first.ordered && first.number !== 1 ? ` start="${first.number}"` : "";
  return { html: `<${tag}${start}>${items.join("")}</${tag}>`, next: cursor };
}

function renderTable(lines, index, options) {
  const headers = splitTableRow(lines[index]);
  const alignments = splitTableRow(lines[index + 1]).map(tableAlignment);
  const rows = [];
  let cursor = index + 2;
  while (cursor < lines.length && lines[cursor].trim() && lines[cursor].includes("|")) {
    rows.push(splitTableRow(lines[cursor]));
    cursor += 1;
  }
  const header = headers.map((cell, cellIndex) => renderTableCell("th", cell, alignments[cellIndex], options)).join("");
  const body = rows.map(row => `<tr>${headers.map((_, cellIndex) => renderTableCell("td", row[cellIndex] || "", alignments[cellIndex], options)).join("")}</tr>`).join("");
  return { html: `<div class="table-wrap"><table><thead><tr>${header}</tr></thead>${body ? `<tbody>${body}</tbody>` : ""}</table></div>`, next: cursor };
}

function renderParagraph(lines, index, options) {
  const content = [];
  let cursor = index;
  while (cursor < lines.length && lines[cursor].trim()) {
    if (cursor > index && isBlockStart(lines, cursor)) break;
    content.push(lines[cursor].trim());
    cursor += 1;
  }
  return { html: `<p>${content.map(line => renderInline(line, options)).join("<br>")}</p>`, next: cursor };
}

function renderInline(value, options = {}) {
  const tokens = [];
  let text = String(value).replaceAll("\u0000", "\uFFFD");
  text = stashCodeSpans(text, tokens);
  text = stashLinks(text, tokens, options);
  text = stashFileReferences(text, tokens, options);
  text = escapeHtml(text)
    .replace(/\*\*([^*\n]+)\*\*/g, "<strong>$1</strong>")
    .replace(/__([^_\n]+)__/g, "<strong>$1</strong>")
    .replace(/~~([^~\n]+)~~/g, "<del>$1</del>")
    .replace(/(^|[^*])\*([^*\n]+)\*(?!\*)/g, "$1<em>$2</em>")
    .replace(/(^|[^_\w])_([^_\n]+)_(?!_)/g, "$1<em>$2</em>");
  return restoreTokens(text, tokens);
}

function stashCodeSpans(value, tokens) {
  return value.replace(/(`+)([^`\n]|(?!\1)`)*?\1/g, match => {
    const marker = match.match(/^`+/)[0];
    const code = match.slice(marker.length, -marker.length).replace(/^ | $/g, "");
    return stash(tokens, `<code>${escapeHtml(code)}</code>`);
  });
}

function stashLinks(value, tokens, options) {
  let text = value.replace(/!\[([^\]]*)\]\((\S+?)(?:\s+["']([^"']*)["'])?\)/g, (_match, alt, url, title) => {
    const href = safeUrl(url, false);
    if (!href) return `![${alt}](${url})`;
    const titleAttr = title ? ` title="${escapeAttribute(title)}"` : "";
    return stash(tokens, `<img src="${escapeAttribute(href)}" alt="${escapeAttribute(alt)}"${titleAttr} loading="lazy">`);
  });
  text = text.replace(/\[([^\]]+)\]\((\S+?)(?:\s+["']([^"']*)["'])?\)/g, (_match, label, url, title) => {
    const href = safeUrl(url, true);
    if (!href) return `[${label}](${url})`;
    const titleAttr = title ? ` title="${escapeAttribute(title)}"` : "";
    return stash(tokens, `<a href="${escapeAttribute(href)}"${titleAttr} target="_blank" rel="noopener noreferrer">${renderInline(label, options)}</a>`);
  });
  return text.replace(/<((?:https?:\/\/|mailto:)[^ >]+)>/gi, (_match, url) => {
    const href = safeUrl(url, true);
    return href ? stash(tokens, `<a href="${escapeAttribute(href)}" target="_blank" rel="noopener noreferrer">${escapeHtml(url)}</a>`) : url;
  });
}

// stashFileReferences 把**用户附的文件引用**（`@路径` / 裸路径）换成 chip 占位符。
// 必须在 escapeHtml/强调规则之前 stash（与代码段、链接同一手法）：chip 的 HTML 一旦
// 流进后面的正则，路径里的 `_`/`*` 可能被当成强调标记，属性会被改坏。
// 只有 options.fileRefs 打开时才做（见 renderMarkdown 的注释）。
function stashFileReferences(value, tokens, options) {
  if (!options?.fileRefs) return value;
  const references = fileReferenceTokens(value);
  if (references.length === 0) return value;
  let output = "";
  let cursor = 0;
  for (const reference of references) {
    output += value.slice(cursor, reference.start);
    output += stash(tokens, fileReferenceChipHTML(reference.path, { host: "message", dir: reference.dir }));
    cursor = reference.end;
  }
  return output + value.slice(cursor);
}

function safeUrl(value, allowMail) {
  const url = String(value).trim();
  if (/^(?:#|\/|\.\/|\.\.\/)/.test(url)) return url;
  try {
    const parsed = new URL(url);
    if (!SAFE_PROTOCOLS.has(parsed.protocol) || (!allowMail && parsed.protocol === "mailto:")) return "";
    return url;
  } catch {
    return "";
  }
}

function splitTableRow(line) {
  let value = line.trim().replace(/^\|/, "").replace(/\|$/, "");
  const cells = [];
  let cell = "";
  for (let index = 0; index < value.length; index += 1) {
    if (value[index] === "\\" && value[index + 1] === "|") {
      cell += "|";
      index += 1;
    } else if (value[index] === "|") {
      cells.push(cell.trim());
      cell = "";
    } else {
      cell += value[index];
    }
  }
  cells.push(cell.trim());
  return cells;
}

function isTableStart(lines, index) {
  if (index + 1 >= lines.length || !lines[index].includes("|")) return false;
  const delimiters = splitTableRow(lines[index + 1]);
  return delimiters.length > 0 && delimiters.every(cell => /^:?-{3,}:?$/.test(cell));
}

function renderTableCell(tag, value, alignment, options) {
  const align = alignment ? ` style="text-align:${alignment}"` : "";
  return `<${tag}${align}>${renderInline(value, options)}</${tag}>`;
}

function tableAlignment(value) {
  if (/^:-+:$/.test(value)) return "center";
  if (/^-+:$/.test(value)) return "right";
  if (/^:-+$/.test(value)) return "left";
  return "";
}

function matchListItem(line) {
  const match = line.match(/^\s*([-+*]|(\d+)[.)])\s+(.+)$/);
  if (!match) return null;
  return { ordered: Boolean(match[2]), number: Number(match[2] || 1), content: match[3] };
}

function isBlockStart(lines, index) {
  return BLOCK_PATTERN.test(lines[index]) || isTableStart(lines, index);
}

function isHorizontalRule(line) {
  const compact = line.trim().replace(/\s/g, "");
  return compact.length >= 3 && /^(-+|\*+|_+)$/.test(compact);
}

function stash(tokens, html) {
  const index = tokens.push(html) - 1;
  return `\u0000MD${index}\u0000`;
}

function restoreTokens(value, tokens) {
  return value.replace(/\u0000MD(\d+)\u0000/g, (_match, index) => tokens[Number(index)] || "");
}

function escapeHtml(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

function escapeAttribute(value) {
  return escapeHtml(value).replaceAll("`", "&#096;");
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
