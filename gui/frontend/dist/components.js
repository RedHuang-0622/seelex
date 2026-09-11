import { renderMarkdown } from "./markdown.js";

const ICONS = {
  command: '<path d="M4 6h16M4 12h10M4 18h7"/><path d="m17 16 3 2-3 2"/>',
  runtime: '<path d="M4 7h10M18 7h2M4 17h2M10 17h10"/><circle cx="16" cy="7" r="2"/><circle cx="8" cy="17" r="2"/>',
  settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2.12 2.12-.06-.06a1.7 1.7 0 0 0-1.88-.34 1.7 1.7 0 0 0-1.04 1.56V20.3h-3v-.08A1.7 1.7 0 0 0 10.66 18.66a1.7 1.7 0 0 0-1.88.34l-.06.06-2.12-2.12.06-.06A1.7 1.7 0 0 0 7 15a1.7 1.7 0 0 0-1.56-1.04h-.08v-3h.08A1.7 1.7 0 0 0 7 9.92a1.7 1.7 0 0 0-.34-1.88L6.6 7.98l2.12-2.12.06.06a1.7 1.7 0 0 0 1.88.34A1.7 1.7 0 0 0 11.7 4.7v-.08h3v.08a1.7 1.7 0 0 0 1.04 1.56 1.7 1.7 0 0 0 1.88-.34l.06-.06 2.12 2.12-.06.06a1.7 1.7 0 0 0-.34 1.88 1.7 1.7 0 0 0 1.56 1.04h.08v3h-.08A1.7 1.7 0 0 0 19.4 15Z"/>',
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
  search: '<circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/>',
  table: '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M12 3v18M3 12h18"/>',
  check: '<path d="m5 12 4 4L19 6"/>',
  error: '<circle cx="12" cy="12" r="9"/><path d="M12 7v6M12 17h.01"/>',
  grip: '<circle cx="9" cy="5" r="1"/><circle cx="15" cy="5" r="1"/><circle cx="9" cy="12" r="1"/><circle cx="15" cy="12" r="1"/><circle cx="9" cy="19" r="1"/><circle cx="15" cy="19" r="1"/>',
  branch: '<circle cx="6" cy="6" r="2.5"/><circle cx="6" cy="18" r="2.5"/><circle cx="18" cy="8" r="2.5"/><path d="M6 8.5v7M8.5 6h4a5.5 5.5 0 0 1 3 5v-0.5a5.5 5.5 0 0 1-3 5h-4"/>'
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
  const model = renderConversationModel(messages, chat);
  return { html: model.items.map(item => item.html).join(""), payloads: model.payloads };
}

export function renderConversationModel(messages = [], chat = {}) {
  const payloads = new Map();
  const items = buildConversationItems(messages);
  const grouped = groupConversationItems(items, payloads);
  const rendered = grouped.map((item, index) => {
    const key = item.key || `${item.kind}-${index}`;
    const html = item.kind === "axis"
      ? item.html
      : item.kind === "tool"
        ? renderToolCall(item, key, payloads)
        : renderMessage(item.message, key);
    const meta = item.kind === "message"
      ? {
        kind: "message",
        role: item.message?.role || "",
        hasReasoning: Boolean(String(item.message?.reasoning_content || "").trim()),
        hasContent: Boolean(String(item.message?.content || "").trim())
      }
      : { kind: item.kind };
    return { key, html, meta };
  });
  const activity = renderChatActivity(chat);
  if (activity) {
    const key = "chat:activity";
    rendered.push({ key, html: `<div class="chat-activity-tail" data-conversation-key="${key}" data-wheel-kind="system" data-wheel-label="${escapeHtml(chat.running ? "执行中" : "等待发送")}">${activity}</div>` });
  }
  return { items: rendered, payloads };
}

export function renderChatActivity(chat = {}) {
  const queue = Array.isArray(chat.input_queue) ? chat.input_queue : [];
  const running = Boolean(chat.running);
  if (!running && queue.length === 0) return "";
  const loader = running ? `<section class="runtime-activity" role="status" aria-live="polite">
    <span class="runtime-spinner">${icon("source", 15)}</span>
    <strong>执行中</strong>
    <span class="runtime-pulse" aria-hidden="true"><i></i><i></i><i></i></span>
  </section>` : "";
  const queued = queue.length ? `<section class="message-queue" aria-label="等待发送的消息">
    ${queue.map((input, index) => `<article class="queued-message">
      <header><span>${icon("message", 13)}</span><strong>排队 ${String(index + 1).padStart(2, "0")}</strong><small>等待</small></header>
      <div class="queued-message-body">${markdown(input)}</div>
    </article>`).join("")}
  </section>` : "";
  return loader + queued;
}

export function buildConversationItems(messages = []) {
  const items = [];
  // 配对键 = 框架 tool-call id（tool.started 与 tool_result 同源）；不按名字回退
  // 猜配对，否则同名并发工具会被并成一行。
  const pendingByID = new Map();
  for (const [messageIndex, message] of messages.entries()) {
    if (!message.tool) {
      items.push({ kind: "message", key: `message:${message.id || messageIndex}`, message });
      continue;
    }
    const tool = message.tool;
    const isOutput = message.role === "tool_result";
    if (isOutput) {
      const target = tool.id ? pendingByID.get(tool.id) : null;
      if (target) {
        target.output = tool.error || tool.result || message.content || "";
        target.error = tool.error || "";
        target.status = tool.error ? "error" : (tool.status || "success");
        target.duration = tool.duration || target.duration;
        target.outputAttached = true;
        target.resultRef = tool.result_ref || "";
        target.truncated = Boolean(tool.truncated);
        target.totalChars = Number(tool.total_chars) || 0;
        continue;
      }
    }
    const item = {
      kind: "tool",
      key: `tool:${tool.id || message.id || messageIndex}`,
      id: tool.id || "",
      name: tool.name || "tool",
      input: tool.arguments || "",
      output: tool.error || tool.result || (isOutput ? message.content || "" : ""),
      error: tool.error || "",
      status: tool.status || (isOutput ? "success" : "pending"),
      duration: tool.duration || 0,
      outputAttached: isOutput || Boolean(tool.result || tool.error),
      resultRef: tool.result_ref || "",
      truncated: Boolean(tool.truncated),
      totalChars: Number(tool.total_chars) || 0
    };
    items.push(item);
    if (item.id) pendingByID.set(item.id, item);
  }
  return items;
}

function hasVisibleAssistantOutput(message) {
  if (!message || message.role !== "assistant") return false;
  return String(message.content || "").trim().length > 0 ||
    String(message.reasoning_content || "").trim().length > 0;
}

// 泛化“滚动轴”：对话区把 tool-calling 与思考各自收进可展开/收起的滚动轴，
// LLM 正文不进滚动轴（保持内联）。空 assistant 占位（无 content/reasoning）
// 不渲染为独立消息；它只是流式正文的落点。轨迹视图保持全量不变。
function groupConversationItems(items, payloads) {
  const grouped = [];
  const pendingTools = [];
  const flushTools = () => {
    if (pendingTools.length === 0) return;
    const first = pendingTools[0];
    const key = `roll:${first.key}`;
    const count = new Set(pendingTools.map(item => item.key)).size;
    const names = [...new Set(pendingTools.map(item => item.name))].join(" / ");
    const chips = pendingTools.map(item => renderToolCall(item, item.key, payloads)).join("");
    const wheelLabel = `工具过程 · ${count} 次 · ${names}`;
    const html = `<details class="conversation-axis is-tools" data-conversation-key="${escapeHtml(key)}" data-wheel-kind="tools" data-wheel-label="${escapeHtml(wheelLabel)}">
      <summary>
        <span class="axis-caret" aria-hidden="true"></span>
        <span class="axis-label">工具过程</span>
        <span class="axis-meta">${count} 次 · ${escapeHtml(names)}</span>
        <span class="axis-hint">展开 / 收起</span>
      </summary>
      <div class="axis-scroll tools-axis-scroll">${chips}</div>
    </details>`;
    grouped.push({ kind: "axis", key, html });
    pendingTools.length = 0;
  };
  for (const item of items) {
    if (item.kind !== "message") {
      pendingTools.push(item);
      continue;
    }
    const message = item.message;
    if (message.role === "user") {
      flushTools();
      grouped.push(item);
      continue;
    }
    if (message.role === "assistant" && !hasVisibleAssistantOutput(message)) {
      // 结构性空 assistant 占位：正文到达前不显示，到达后按 LLM 输出渲染。
      continue;
    }
    flushTools();
    grouped.push(item);
  }
  flushTools();
  return grouped;
}

export function renderSources(sources = []) {
  if (!sources.length) return '<span class="muted list-empty">暂无 Agent 已读文件</span>';
  return sources.map(source => {
    const sourceIcon = source.kind === "capability" ? "folder" : "file";
    return `<article class="source-item">
      <span class="source-icon">${icon(sourceIcon, 15)}</span>
      <div><strong>${escapeHtml(source.name || source.path)}</strong><small title="${escapeHtml(source.path || "")}">${escapeHtml(source.path || "")}</small></div>
      <span class="source-kind">${escapeHtml(shortKind(source.kind))}</span>
    </article>`;
  }).join("");
}

// wheelSnippet 把消息正文压成轮轴标签用的一行摘要。
export function wheelSnippet(text = "", limit = 32) {
  const flat = String(text).replace(/\s+/g, " ").trim();
  if (!flat) return "（空）";
  return flat.length > limit ? `${flat.slice(0, limit)}…` : flat;
}

function renderMessage(message, key) {
  const role = message.role || "assistant";
  const time = message.created_at ? new Date(message.created_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) : "";
  const label = role === "user" ? "YOU" : role === "assistant" ? "AGENT" : role.toUpperCase();
  const reasoning = String(message.reasoning_content || "").trim();
  const debugID = String(message.id || key || "");
  const thinking = role === "assistant" && reasoning
    ? `<details class="reasoning-block is-thinking-axis" open data-trajectory-key="${escapeHtml(key)}">
        <summary>
          <span class="reasoning-chevron" aria-hidden="true"></span>
          <span>思考</span>
          <span class="reasoning-state">${formatChars(reasoning.length)}</span>
        </summary>
        <div class="reasoning-content is-plain">${escapeHtml(reasoning)}</div>
      </details>`
    : "";
  const wheelKind = role === "user" ? "user" : role === "system" ? "system" : role === "assistant" ? (String(message.content || "").trim() ? "agent" : "think") : "other";
  const wheelLabel = `${role === "user" ? "你" : role === "system" ? "系统" : role === "assistant" ? "Seelex" : label} · ${wheelSnippet(String(message.content || "").trim() || reasoning)}`;
  return `<article class="message ${escapeHtml(role)}" data-conversation-key="${escapeHtml(key)}" data-trajectory-key="${escapeHtml(key)}" data-wheel-kind="${wheelKind}" data-wheel-label="${escapeHtml(wheelLabel)}">
    <div class="message-debug"><code class="item-id">${escapeHtml(debugID)}</code></div>
    <div class="message-head"><span class="role-mark">${role === "user" ? icon("message", 13) : icon("source", 13)}</span><strong>${escapeHtml(label)}</strong><span>${escapeHtml(time)}</span></div>
    ${thinking}
    <div class="message-body">${markdown(message.content || "")}</div>
  </article>`;
}

function renderToolCall(tool, key, payloads) {
  const status = statusMeta(tool.status, tool.error);
  return `<article class="tool-run ${status.className}" data-conversation-key="${escapeHtml(key)}">
    <div class="tool-run-line">
      <code class="item-id">${escapeHtml(tool.id || key)}</code>
      <button type="button" class="chat-chip is-tool" data-trajectory-key="${escapeHtml(key)}" title="工具调用与 IN/OUT 完整内容见轨迹视图">
        <span class="tool-symbol">${icon("terminal", 13)}</span>
        <strong class="chat-chip-name">${escapeHtml(tool.name)}</strong>
        <span class="tool-state">${icon(status.icon, 11)} ${status.label}</span>
        ${tool.duration ? `<span class="chat-chip-meta">${escapeHtml(formatDuration(tool.duration))}</span>` : ""}
        ${chatChipSize(tool) ? `<span class="chat-chip-meta">${escapeHtml(chatChipSize(tool))}</span>` : ""}
        <span class="chat-chip-hint">完整内容 → 轨迹</span>
      </button>
    </div>
  </article>`;
}

function chatChipSize(tool) {
  const chars = Number(tool.totalChars) || String(tool.output || "").length;
  if (chars <= 0) return "";
  return chars < 1024 ? `${chars} B` : `${(chars / 1024).toFixed(chars < 10240 ? 1 : 0)} KB`;
}

function formatChars(length) {
  if (length < 1024) return `${length} 字符`;
  return `${(length / 1024).toFixed(length < 10240 ? 1 : 0)} KB`;
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
  return ({ documentation: "DOC", configuration: "CFG", capability: "CAP", read: "READ" })[kind] || String(kind).slice(0, 3).toUpperCase();
}
