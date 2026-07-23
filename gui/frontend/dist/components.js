import { renderMarkdown } from "./markdown.js";

const ICONS = {
  command: '<path d="M4 6h16M4 12h10M4 18h7"/><path d="m17 16 3 2-3 2"/>',
  runtime: '<path d="M4 7h10M18 7h2M4 17h2M10 17h10"/><circle cx="16" cy="7" r="2"/><circle cx="8" cy="17" r="2"/>',
  send: '<path d="m5 12 14-7-4 14-3-6-7-1Z"/><path d="m12 13 7-8"/>',
  stop: '<rect x="7" y="7" width="10" height="10" rx="1"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  close: '<path d="m6 6 12 12M18 6 6 18"/>',
  terminal: '<path d="m5 7 4 4-4 4M11 17h8"/>',
  copy: '<rect x="8" y="8" width="11" height="11" rx="2"/><path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h3"/>',
  expand: '<path d="m8 3-5 5M3 3v5h5M16 3l5 5M21 3v5h-5M8 21l-5-5M3 21v-5h5M16 21l5-5M21 21v-5h-5"/>',
  file: '<path d="M6 3h8l4 4v14H6z"/><path d="M14 3v5h5M9 13h6M9 17h5"/>',
  folder: '<path d="M3 6h7l2 2h9v11H3z"/>',
  source: '<circle cx="12" cy="12" r="3"/><path d="M12 3v3M12 18v3M3 12h3M18 12h3M5.6 5.6l2.1 2.1M16.3 16.3l2.1 2.1M18.4 5.6l-2.1 2.1M7.7 16.3l-2.1 2.1"/>',
  message: '<path d="M4 5h16v12H8l-4 4z"/>',
  skill: '<path d="M12 3 4 7v10l8 4 8-4V7z"/><path d="m4 7 8 4 8-4M12 11v10"/>',
  plugin: '<path d="M8 3v5H3v8h5v5h8v-5h5V8h-5V3z"/>',
  check: '<path d="m5 12 4 4L19 6"/>',
  error: '<circle cx="12" cy="12" r="9"/><path d="M12 7v6M12 17h.01"/>'
};

export function icon(name, size = 16) {
  const paths = ICONS[name] || ICONS.source;
  return `<svg class="icon" width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths}</svg>`;
}

export function hydrateIcons(root = document) {
  root.querySelectorAll("[data-icon]").forEach(element => {
    element.innerHTML = icon(element.dataset.icon, Number(element.dataset.iconSize || 16));
  });
}

export function escapeHtml(value = "") {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

export const markdown = renderMarkdown;

export function renderConversationComponent(messages = [], chat = {}) {
  const payloads = new Map();
  const items = buildConversationItems(messages);
  const conversation = items.map((item, index) => item.kind === "tool"
    ? renderToolCall(item, `tool-${index}`, payloads)
    : renderMessage(item.message)).join("");
  return { html: conversation + renderChatActivity(chat), payloads };
}

export function renderChatActivity(chat = {}) {
  const queue = Array.isArray(chat.input_queue) ? chat.input_queue : [];
  const running = Boolean(chat.running);
  if (!running && queue.length === 0) return "";
  const loader = running ? `<section class="runtime-activity" role="status" aria-live="polite">
    <span class="runtime-spinner">${icon("source", 15)}</span>
    <strong>WORKING</strong>
    <span class="runtime-pulse" aria-hidden="true"><i></i><i></i><i></i></span>
  </section>` : "";
  const queued = queue.length ? `<section class="message-queue" aria-label="等待发送的消息">
    ${queue.map((input, index) => `<article class="queued-message">
      <header><span>${icon("message", 13)}</span><strong>QUEUE ${String(index + 1).padStart(2, "0")}</strong><small>WAIT</small></header>
      <div class="queued-message-body">${markdown(input)}</div>
    </article>`).join("")}
  </section>` : "";
  return loader + queued;
}

export function buildConversationItems(messages = []) {
  const items = [];
  const pendingByID = new Map();
  const pendingTools = [];
  for (const message of messages) {
    if (!message.tool) {
      items.push({ kind: "message", message });
      continue;
    }
    const tool = message.tool;
    const isOutput = message.role === "tool_result";
    if (isOutput) {
      let target = tool.id ? pendingByID.get(tool.id) : null;
      if (!target) {
        target = [...pendingTools].reverse().find(item => item.name === tool.name);
      }
      if (target) {
        target.output = tool.error || tool.result || message.content || "";
        target.error = tool.error || "";
        target.status = tool.error ? "error" : (tool.status || "success");
        target.duration = tool.duration || target.duration;
        target.outputAttached = true;
        continue;
      }
    }
    const item = {
      kind: "tool",
      id: tool.id || "",
      name: tool.name || "tool",
      input: tool.arguments || "",
      output: tool.error || tool.result || (isOutput ? message.content || "" : ""),
      error: tool.error || "",
      status: tool.status || (isOutput ? "success" : "pending"),
      duration: tool.duration || 0,
      outputAttached: isOutput || Boolean(tool.result || tool.error)
    };
    items.push(item);
    pendingTools.push(item);
    if (item.id) pendingByID.set(item.id, item);
  }
  return items;
}

export function renderSources(sources = []) {
  if (!sources.length) return '<span class="muted list-empty">暂无项目资料</span>';
  return sources.map(source => {
    const sourceIcon = source.kind === "capability" ? "folder" : "file";
    return `<article class="source-item">
      <span class="source-icon">${icon(sourceIcon, 15)}</span>
      <div><strong>${escapeHtml(source.name || source.path)}</strong><small title="${escapeHtml(source.path || "")}">${escapeHtml(source.path || "")}</small></div>
      <span class="source-kind">${escapeHtml(shortKind(source.kind))}</span>
    </article>`;
  }).join("");
}

function renderMessage(message) {
  const role = message.role || "assistant";
  const time = message.created_at ? new Date(message.created_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) : "";
  const label = role === "user" ? "YOU" : role === "assistant" ? "AGENT" : role.toUpperCase();
  return `<article class="message ${escapeHtml(role)}">
    <div class="message-head"><span class="role-mark">${role === "user" ? icon("message", 13) : icon("source", 13)}</span><strong>${escapeHtml(label)}</strong><span>${escapeHtml(time)}</span></div>
    <div class="message-body">${markdown(message.content || "")}</div>
  </article>`;
}

function renderToolCall(tool, key, payloads) {
  const input = prettyValue(tool.input) || "—";
  const output = prettyValue(tool.output) || (tool.status === "pending" || tool.status === "running" ? "Waiting for output…" : "—");
  const inputKey = `${key}-in`;
  const outputKey = `${key}-out`;
  payloads.set(inputKey, input);
  payloads.set(outputKey, output);
  const inputView = limitText(input, 1400, 28);
  const outputView = limitText(output, 2400, 40);
  const status = statusMeta(tool.status, tool.error);
  return `<article class="tool-run ${status.className}">
    <header class="tool-run-head">
      <span class="tool-symbol">${icon("terminal", 15)}</span>
      <strong>${escapeHtml(tool.name)}</strong>
      ${tool.duration ? `<span class="tool-duration">${escapeHtml(formatDuration(tool.duration))}</span>` : ""}
      <span class="tool-state">${icon(status.icon, 13)} ${status.label}</span>
    </header>
    <div class="tool-io-grid">
      ${renderIOPanel("IN", inputView, inputKey, false)}
      ${renderIOPanel("OUT", outputView, outputKey, Boolean(tool.error))}
    </div>
  </article>`;
}

function renderIOPanel(label, view, payloadKey, error) {
  return `<section class="io-panel ${error ? "io-error" : ""}" data-payload="${payloadKey}">
    <header><span class="io-label">${label}</span><span class="io-meta">${view.total} chars</span><button class="icon-button subtle" type="button" data-copy="${payloadKey}" title="复制 ${label}" aria-label="复制 ${label}">${icon("copy", 13)}</button></header>
    <pre>${escapeHtml(view.preview)}</pre>
    ${view.truncated ? `<button class="io-expand" type="button" data-expand="${payloadKey}" title="展开完整内容">${icon("expand", 12)} <span>+${view.hidden} chars</span></button>` : ""}
  </section>`;
}

function limitText(value, maxChars, maxLines) {
  const text = String(value || "");
  const lines = text.split("\n");
  let preview = lines.slice(0, maxLines).join("\n");
  if (preview.length > maxChars) preview = preview.slice(0, maxChars);
  const truncated = preview.length < text.length;
  return { preview, truncated, hidden: Math.max(text.length - preview.length, 0), total: text.length };
}

function prettyValue(value) {
  const text = String(value || "").trim();
  if (!text) return "";
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text;
  }
}

function statusMeta(status, error) {
  if (error || status === "error" || status === "failed") return { label: "ERR", icon: "error", className: "is-error" };
  if (status === "running" || status === "pending") return { label: "RUN", icon: "source", className: "is-running" };
  return { label: "OK", icon: "check", className: "is-success" };
}

function formatDuration(duration) {
  const milliseconds = Number(duration) / 1e6;
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) return "";
  return milliseconds >= 1000 ? `${(milliseconds / 1000).toFixed(1)}s` : `${Math.round(milliseconds)}ms`;
}

function shortKind(kind = "source") {
  return ({ documentation: "DOC", configuration: "CFG", capability: "CAP" })[kind] || String(kind).slice(0, 3).toUpperCase();
}
