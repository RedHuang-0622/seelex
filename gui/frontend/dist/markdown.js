const SAFE_PROTOCOLS = new Set(["http:", "https:", "mailto:"]);
const BLOCK_PATTERN = /^(?: {0,3}(?:#{1,6}\s+|>|```|~~~|<think>\s*)|\s*(?:[-+*]|\d+[.)])\s+|\s*(?:-{3,}|\*{3,}|_{3,})\s*$)/i;
const REASONING_OPEN = /^ {0,3}<think>\s*/i;
const REASONING_CLOSE = /<\/think>/i;

export function renderMarkdown(value = "") {
  const source = String(value).replace(/\r\n?/g, "\n");
  return source ? renderBlocks(source.split("\n")) : "";
}

function renderBlocks(lines) {
  const output = [];
  let index = 0;
  while (index < lines.length) {
    if (!lines[index].trim()) {
      index += 1;
      continue;
    }
    const renderer = selectBlockRenderer(lines, index);
    const block = renderer(lines, index);
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

function renderFence(lines, index) {
  const opening = lines[index].match(/^ {0,3}(`{3,}|~{3,})\s*([^\s`~]+)?\s*$/);
  if (!opening) return renderParagraph(lines, index);
  const marker = opening[1];
  const closePattern = new RegExp(`^ {0,3}${escapeRegExp(marker[0])}{${marker.length},}\\s*$`);
  const body = [];
  let cursor = index + 1;
  while (cursor < lines.length && !closePattern.test(lines[cursor])) {
    body.push(lines[cursor]);
    cursor += 1;
  }
  if (cursor < lines.length) cursor += 1;
  const language = opening[2] ? ` class="language-${escapeAttribute(opening[2])}"` : "";
  return { html: `<pre><code${language}>${escapeHtml(body.join("\n"))}</code></pre>`, next: cursor };
}

function renderReasoning(lines, index) {
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
  const inner = content ? renderBlocks(content.split("\n")) : '<p class="reasoning-empty">暂无思考内容</p>';
  const live = closed ? "" : ' open class="reasoning-block is-streaming"';
  const details = closed ? '<details class="reasoning-block">' : `<details${live}>`;
  const state = closed ? "" : '<span class="reasoning-state">LIVE</span>';
  const trailing = tail.trim() ? renderBlocks([tail]) : "";
  return {
    html: `${details}<summary><span class="reasoning-chevron" aria-hidden="true"></span><span>思考过程</span>${state}</summary><div class="reasoning-content">${inner}</div></details>${trailing}`,
    next: cursor
  };
}

function renderHeading(lines, index) {
  const match = lines[index].match(/^ {0,3}(#{1,6})\s+(.+?)\s*#*\s*$/);
  const level = match[1].length;
  return { html: `<h${level}>${renderInline(match[2])}</h${level}>`, next: index + 1 };
}

function renderHorizontalRule(_lines, index) {
  return { html: "<hr>", next: index + 1 };
}

function renderQuote(lines, index) {
  const quoted = [];
  let cursor = index;
  while (cursor < lines.length) {
    const match = lines[cursor].match(/^ {0,3}>\s?(.*)$/);
    if (!match) break;
    quoted.push(match[1]);
    cursor += 1;
  }
  return { html: `<blockquote>${renderBlocks(quoted)}</blockquote>`, next: cursor };
}

function renderList(lines, index) {
  const first = matchListItem(lines[index]);
  const tag = first.ordered ? "ol" : "ul";
  const items = [];
  let cursor = index;
  while (cursor < lines.length) {
    const item = matchListItem(lines[cursor]);
    if (!item || item.ordered !== first.ordered) break;
    const task = item.content.match(/^\[([ xX])\]\s+(.*)$/);
    const checkbox = task ? `<input type="checkbox" disabled${task[1].toLowerCase() === "x" ? " checked" : ""}>` : "";
    items.push(`<li${task ? ' class="task-item"' : ""}>${checkbox}${renderInline(task ? task[2] : item.content)}</li>`);
    cursor += 1;
  }
  const start = first.ordered && first.number !== 1 ? ` start="${first.number}"` : "";
  return { html: `<${tag}${start}>${items.join("")}</${tag}>`, next: cursor };
}

function renderTable(lines, index) {
  const headers = splitTableRow(lines[index]);
  const alignments = splitTableRow(lines[index + 1]).map(tableAlignment);
  const rows = [];
  let cursor = index + 2;
  while (cursor < lines.length && lines[cursor].trim() && lines[cursor].includes("|")) {
    rows.push(splitTableRow(lines[cursor]));
    cursor += 1;
  }
  const header = headers.map((cell, cellIndex) => renderTableCell("th", cell, alignments[cellIndex])).join("");
  const body = rows.map(row => `<tr>${headers.map((_, cellIndex) => renderTableCell("td", row[cellIndex] || "", alignments[cellIndex])).join("")}</tr>`).join("");
  return { html: `<div class="table-wrap"><table><thead><tr>${header}</tr></thead>${body ? `<tbody>${body}</tbody>` : ""}</table></div>`, next: cursor };
}

function renderParagraph(lines, index) {
  const content = [];
  let cursor = index;
  while (cursor < lines.length && lines[cursor].trim()) {
    if (cursor > index && isBlockStart(lines, cursor)) break;
    content.push(lines[cursor].trim());
    cursor += 1;
  }
  return { html: `<p>${content.map(renderInline).join("<br>")}</p>`, next: cursor };
}

function renderInline(value) {
  const tokens = [];
  let text = String(value).replaceAll("\u0000", "\uFFFD");
  text = stashCodeSpans(text, tokens);
  text = stashLinks(text, tokens);
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

function stashLinks(value, tokens) {
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
    return stash(tokens, `<a href="${escapeAttribute(href)}"${titleAttr} target="_blank" rel="noopener noreferrer">${renderInline(label)}</a>`);
  });
  return text.replace(/<((?:https?:\/\/|mailto:)[^ >]+)>/gi, (_match, url) => {
    const href = safeUrl(url, true);
    return href ? stash(tokens, `<a href="${escapeAttribute(href)}" target="_blank" rel="noopener noreferrer">${escapeHtml(url)}</a>`) : url;
  });
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

function renderTableCell(tag, value, alignment) {
  const align = alignment ? ` style="text-align:${alignment}"` : "";
  return `<${tag}${align}>${renderInline(value)}</${tag}>`;
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
