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
  team: '<circle cx="9" cy="8" r="3"/><path d="M3 19a6 6 0 0 1 12 0"/><path d="M16 6.2a3 3 0 0 1 0 5.6M20 19a6 6 0 0 0-1.6-4.1"/>',
  search: '<circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/>',
  table: '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M12 3v18M3 12h18"/>',
  check: '<path d="m5 12 4 4L19 6"/>',
  error: '<circle cx="12" cy="12" r="9"/><path d="M12 7v6M12 17h.01"/>',
  grip: '<circle cx="9" cy="5" r="1"/><circle cx="15" cy="5" r="1"/><circle cx="9" cy="12" r="1"/><circle cx="15" cy="12" r="1"/><circle cx="9" cy="19" r="1"/><circle cx="15" cy="19" r="1"/>',
  "arrow-up": '<path d="M12 19V5M5 12l7-7 7 7"/>',
  "arrow-down": '<path d="M12 5v14M5 12l7 7 7-7"/>',
  recall: '<path d="M9 14 4 9l5-5"/><path d="M4 9h10a6 6 0 0 1 0 12h-3"/>',
  "corner-down-right": '<path d="M4 4v7a4 4 0 0 0 4 4h12"/><path d="m15 10 5 5-5 5"/>',
  refresh: '<path d="M20 11a8 8 0 1 0-2.4 5.7"/><path d="M20 5v6h-6"/>',
  branch: '<circle cx="6" cy="6" r="2.5"/><circle cx="6" cy="18" r="2.5"/><circle cx="18" cy="8" r="2.5"/><path d="M6 8.5v7M8.5 6h4a5.5 5.5 0 0 1 3 5v-0.5a5.5 5.5 0 0 1-3 5h-4"/>',
  "chevron-left": '<path d="m14.5 6-6 6 6 6"/>',
  "chevron-right": '<path d="m9.5 6 6 6-6 6"/>',
  "chevron-up": '<path d="m6 14.5 6-6 6 6"/>',
  "chevron-down": '<path d="m6 9.5 6 6 6-6"/>',
  circle: '<circle cx="12" cy="12" r="8"/>',
  dot: '<circle cx="12" cy="12" r="4.5" fill="currentColor" stroke="none"/>',
  more: '<circle cx="5" cy="12" r="1.6" fill="currentColor" stroke="none"/><circle cx="12" cy="12" r="1.6" fill="currentColor" stroke="none"/><circle cx="19" cy="12" r="1.6" fill="currentColor" stroke="none"/>',
  star: '<path d="m12 4 2.5 5.1 5.5.8-4 3.9.95 5.5L12 16.7l-4.95 2.6.95-5.5-4-3.9 5.5-.8z" fill="currentColor" stroke="none"/>',
  "star-outline": '<path d="m12 4 2.5 5.1 5.5.8-4 3.9.95 5.5L12 16.7l-4.95 2.6.95-5.5-4-3.9 5.5-.8z"/>',
  user: '<circle cx="12" cy="8" r="3.5"/><path d="M5 20a7 7 0 0 1 14 0"/>',
  layer: '<path d="m12 3 9 5-9 5-9-5z"/><path d="m3 13 9 5 9-5"/>'
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

export function renderConversationComponent(messages = [], chat = {}, draft = "", anchor = null) {
  const model = renderConversationModel(messages, chat, draft, anchor);
  return { html: model.items.map(item => item.html).join(""), payloads: model.payloads };
}

// DRAFT_ROW_KEY 是页面 context 里「未发送草稿」那一行的稳定 key（渲染层据此对账
// DOM，见 conversation-view.js reconcile）。它不属于任何 message：草稿是投影、
// 不是消息，所以用独立的 chat: 前缀而不是 message:<id>。
export const DRAFT_ROW_KEY = "chat:draft";

// COMPACTION_FRONTIER_KEY 是对话页里「压缩分界」那一行的稳定 key。它不属于任何
// message：分界是会话级事实（单例），所以用独立的 chat: 前缀（同草稿行），渲染层
// 据此对账 DOM。
export const COMPACTION_FRONTIER_KEY = "chat:compaction-frontier";

// insertCompactionFrontier 在分界所在的那条消息之后插入一行「以上已折叠」虚线。
//
// anchor 是 compaction-format.js 里的 conversationCompactionAnchor 返回值（已算好
// 落点与文案；null = 本页不画线）。分界是**会话单例**，因此每个对话页最多一行——
// 历次压缩各插一行，读者会以为两条分界之间的对话还发给模型。
function insertCompactionFrontier(items, anchor) {
  if (!anchor || !anchor.messageID) return items;
  const at = items.findIndex(item => item.messageID && item.messageID === anchor.messageID);
  if (at < 0) return items;
  const rows = items.slice();
  rows.splice(at + 1, 0, { kind: "frontier", key: COMPACTION_FRONTIER_KEY, html: renderCompactionFrontierRow(anchor) });
  return rows;
}

// renderCompactionFrontierRow 渲染「以上已折叠」分界行：左右两条虚线夹一枚说明牌，
// 牌上是三件事——线在哪（label）、上方原文还能不能读（hint，明写不靠悬停）、帧正文
// 怎么打开（按钮）。入口只携带 data 属性（data-compact-frame-ref），由 app.js 的
// document 级委托接住——组件不持有 invoke 依赖（同排队卡片的动作按钮）。
function renderCompactionFrontierRow(anchor) {
  const frameRef = String(anchor.frameRef || "");
  const label = String(anchor.label || "");
  const note = String(anchor.note || "");
  return `<div class="conversation-compaction-frontier" data-conversation-key="${COMPACTION_FRONTIER_KEY}" data-wheel-kind="system" data-wheel-label="${escapeHtml(label)}" title="${escapeHtml(String(anchor.title || label))}">
      <span class="conversation-compaction-frontier-line" aria-hidden="true"></span>
      <span class="conversation-compaction-frontier-chip">
        <strong class="conversation-compaction-frontier-label">${escapeHtml(label)}</strong>
        <em class="conversation-compaction-frontier-hint">分界以上的原文仍在下方，可继续上翻阅读；只是不再随请求发给模型</em>
        ${note ? `<span class="conversation-compaction-frontier-note">${escapeHtml(note)}</span>` : ""}
        ${frameRef ? `<button type="button" class="conversation-compaction-frontier-open" data-compact-frame-ref="${escapeHtml(frameRef)}" title="按 ref ${escapeHtml(frameRef)} 读取折叠帧正文（弹出查看）">查看折叠帧正文</button>` : ""}
      </span>
      <span class="conversation-compaction-frontier-line" aria-hidden="true"></span>
    </div>`;
}

// renderConversationModel 把一页的内容组装成渲染行：既定 message + （可选）本页
// 未发送的草稿行 + 运行时活动带。draft 为空/全空白时绝不插行——页面不会凭空多出
// 一条空白草稿（判据同 draft-lifecycle.js composerDraftRows）。
// anchor 是会话单例压缩分界（conversationCompactionAnchor 的返回值；null = 本会话
// 没折叠过、或分界不在本页），只决定「以上已折叠」那一行的落点。
export function renderConversationModel(messages = [], chat = {}, draft = "", anchor = null) {
  const payloads = new Map();
  const items = insertCompactionFrontier(buildConversationItems(messages), anchor);
  const grouped = groupConversationItems(items, payloads);
  const rendered = grouped.map((item, index) => {
    const key = item.key || `${item.kind}-${index}`;
    const html = item.kind === "axis" || item.kind === "frontier"
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
  // 页面 context = 既定 message + 本页未发送的草稿：草稿排在既有消息之后、
  // 运行时活动带之前——它是"这一页还没提交出去的那一份"（正文由壳层给出，判据
  // 见 draft-lifecycle.js composerDraftRows）。
  const draftText = String(draft ?? "");
  if (draftText.trim() !== "") {
    rendered.push({ key: DRAFT_ROW_KEY, html: renderDraftMessage(draftText, DRAFT_ROW_KEY), meta: { kind: "draft" } });
  }
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
    ${queue.map((input, index) => renderQueuedMessage(input, index, queue.length)).join("")}
  </section>` : "";
  return loader + queued;
}

// renderQueuedMessage 渲染一条排队输入：**单行条**（用户口径：形状照 Qoder 的队列
// 做法——一行一条：折返箭头 + 单行正文 + 右侧动作），不再是带表头与正文区的卡片。
// 正文走纯文本而非 markdown：单行省略号要求这段内容属于该元素本身，块级 <p> 会让
// text-overflow 失效；完整原文挂在 data-tip 上（悬停/聚焦出应用自己的提示，\n 会被
// paintTip 换成 <br>），撤回后原文回到输入框继续编辑。
// 动作按钮只携带纯数据（data-queue-action/index），由渲染层（app.js）统一委托到
// Bridge，组件本身不持有 invoke 依赖。
function renderQueuedMessage(input, index, length) {
  const label = String(index + 1).padStart(2, "0");
  const row = queueRowText(input);
  const move = (action, disabled) => `<button type="button" class="queue-action" data-queue-action="${action}" data-queue-index="${index}"
        title="${action === "up" ? "上移" : "下移"}" aria-label="${action === "up" ? "上移" : "下移"}排队 ${label}"${disabled ? " disabled" : ""}>${icon(action === "up" ? "arrow-up" : "arrow-down", 13)}</button>`;
  return `<article class="queued-message" data-queue-index="${index}" aria-label="排队 ${label}，等待发送">
      <span class="queued-message-lead" aria-hidden="true">${icon("corner-down-right", 14)}</span>
      <span class="queued-message-text"${row.tip ? ` data-tip="${escapeHtml(row.tip)}"` : ""}>${escapeHtml(row.line)}</span>
      <span class="queued-message-actions">
        ${move("up", index === 0)}
        ${move("down", index === length - 1)}
        <button type="button" class="queue-action" data-queue-action="recall" data-queue-index="${index}"
          title="撤回编辑" aria-label="撤回排队 ${label} 到输入框">${icon("recall", 13)}</button>
      </span>
    </article>`;
}

// queueRowText 把排队正文压成单行条要的两份文本：line 是条上显示的那一行（连续
// 空白折叠成单个空格，免得换行把一行撕出空洞），tip 是完整原文（保留换行，交给
// data-tip）。两份都原样交给 escapeHtml，正文里的标签不会被当成 HTML。
function queueRowText(value) {
  const original = String(value ?? "");
  return { line: original.replace(/\s+/g, " ").trim(), tip: original.trim() };
}

// renderDraftMessage 渲染「未发送草稿」行：本会话当前还没提交出去的正文（仍活在
// 输入框里，生命周期见 draft-lifecycle.js）。形状与排队条同族（单行条），差别用
// 强调色 + 右侧「待发送」标签表达。身份有三处可判：meta.kind="draft"、`is-draft`
// 类、`data-draft`/`data-unsent` 标记——渲染层与测试都不靠正文猜。
// 它是投影：不进 conversation、不派 message id、也不回写输入框（输入框正文才是
// 本地事实源）。
function renderDraftMessage(text, key) {
  const row = queueRowText(text);
  return `<article class="queued-message draft-message is-draft" data-conversation-key="${escapeHtml(key)}" data-draft="true" data-unsent="true" aria-label="未发送草稿，待发送">
      <span class="queued-message-lead" aria-hidden="true">${icon("message", 14)}</span>
      <span class="queued-message-text"${row.tip ? ` data-tip="${escapeHtml(row.tip)}"` : ""}>${escapeHtml(row.line)}</span>
      <span class="queued-message-flag">待发送</span>
    </article>`;
}

// queueMoveTarget 把队列条目的 ↑/↓ 动作映射为一次调换（from → to）。边界
// 动作（首行上移 / 末行下移）与非法下标返回 null——与渲染层按钮 disabled
// 的判定同源，渲染层与测试共用这一份规则。
export function queueMoveTarget(action, index, length) {
  if (action !== "up" && action !== "down") return null;
  if (!Number.isInteger(index) || index < 0 || index >= length) return null;
  const to = action === "up" ? index - 1 : index + 1;
  if (to < 0 || to >= length) return null;
  return { from: index, to };
}

export function buildConversationItems(messages = []) {
  const items = [];
  // 配对键 = 框架 tool-call id（tool.started 与 tool_result 同源）；不按名字回退
  // 猜配对，否则同名并发工具会被并成一行。
  const pendingByID = new Map();
  for (const [messageIndex, message] of messages.entries()) {
    if (!message.tool) {
      // messageID：分界行要按事件序号与消息行对位（判据见 compaction-format.js 的
      // messageOrdinal / conversationCompactionAnchor），
      // 因此每条渲染行都带上自己的消息 id，不靠渲染层回查 messages。
      items.push({ kind: "message", key: `message:${message.id || messageIndex}`, messageID: String(message.id || ""), message });
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
      messageID: String(message.id || ""),
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
    if (item.kind === "frontier") {
      // 分界行自成一组：它不是工具行（不能并进"工具过程"折叠组），也不属于任何
      // 消息——先收掉挂起的工具行，再原样放行。
      flushTools();
      grouped.push(item);
      continue;
    }
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
  const label = roleIdentity(message, role);
  const reasoning = String(message.reasoning_content || "").trim();
  const debugID = String(message.id || key || "");
  const roundID = Number(message.round_id || 0);
  const roundChip = roundID > 0 ? `<span class="message-round" title="群聊轮次（一条 user 输入开启一轮）">R${roundID}</span>` : "";
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
  const wheelLabel = `${role === "user" ? "你" : role === "system" ? "系统" : label} · ${wheelSnippet(String(message.content || "").trim() || reasoning)}`;
  return `<article class="message ${escapeHtml(role)}${messageRoleClass(message)}" data-conversation-key="${escapeHtml(key)}" data-trajectory-key="${escapeHtml(key)}" data-wheel-kind="${wheelKind}" data-wheel-label="${escapeHtml(wheelLabel)}" data-role-name="${escapeHtml(String(message.role_name || ""))}" data-round-id="${roundID}">
    <div class="message-debug"><code class="item-id">${escapeHtml(debugID)}</code></div>
    <div class="message-head"><span class="role-mark">${role === "user" ? icon("message", 13) : icon("source", 13)}</span><strong>${escapeHtml(label)}</strong>${roundChip}<span>${escapeHtml(time)}</span></div>
    ${thinking}
    <div class="message-body">${markdown(message.content || "")}</div>
  </article>`;
}

// messageRoleClass 给群聊归属加一层视觉分区（CSS class 后缀）：同一轮对话里
// EXEC（main）与 ADVISOR（tl）都是 provider role=assistant，光看 role 分不出来；
// 背景色于是成为"谁在说话"的第一线索。无归属的消息（普通 assistant）不加类，
// 保持既有观感不变。
export function messageRoleClass(message) {
  const roleName = String(message?.role_name || "").trim();
  if (roleName === "main") return " is-exec";
  if (roleName === "tl" || roleName === "techlead") return " is-advisor";
  if (roleName === "user") return " is-user";
  if (roleName) return " is-role";
  return "";
}

// roleIdentity 返回消息所属 agent 的展示身份：群聊角色归属优先
// （main → EXEC、tl/techlead → ADVISOR），无归属时回退到 provider role 文案。
// 两个 agent 都渲染成同一个 AGENT/Seelex 会让"主持该轮次的 agent"不可辨。
export function roleIdentity(message, role = message?.role || "assistant") {
  const roleName = String(message?.role_name || "").trim();
  if (roleName && roleName !== "user") {
    if (roleName === "main") return "EXEC";
    if (roleName === "tl" || roleName === "techlead") return "ADVISOR";
    return roleName.toUpperCase();
  }
  return role === "user" ? "YOU" : role === "assistant" ? "AGENT" : role.toUpperCase();
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
