// 统一子代理会话的呈现层：subagent 与 teammate 都从
// ConversationView(SessionRecordPage) 进入这里，再复用主会话的
// renderConversationModel / message / tool-run 组件与 composer 模板。
//
// 这里不持有 Bridge，也不缓存业务状态。invoke、失败重试和当前打开的是哪一个
// 弹层都由 app.js 编排；本模块只负责：
//   1. 把 ConversationRecord[] 映射到主会话熟悉的消息/工具形状；
//   2. 画状态头、会话滚动区和未被占用的 composer 宿主；
//   3. 把唯一 composer 模板挂进当前子代理会话。

import { escapeHtml, hydrateIcons, renderConversationLoading, renderConversationModel } from "./components.js";
import { mountComposerTemplate } from "./composer-shell.js";
import { createConversationView } from "./conversation-view.js";

const KIND_LABEL = {
  subagent: "SUBAGENT",
  teammate: "TEAMMATE"
};

const RECOVERY_LABEL = {
  live: "实时",
  cold: "冷恢复",
  interrupted: "已中断",
  missing: "无记录"
};

function clean(value) {
  return String(value ?? "").trim();
}

function stableToken(value) {
  let hash = 2166136261;
  for (const char of String(value || "")) {
    hash ^= char.codePointAt(0);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0).toString(36);
}

export function normalizeAgentConversationRef(ref = {}) {
  const value = ref && typeof ref === "object" ? ref : {};
  return {
    kind: clean(value.kind),
    session_id: clean(value.session_id ?? value.sessionID),
    main_session_id: clean(value.main_session_id ?? value.mainSessionID),
    team_id: clean(value.team_id ?? value.teamID),
    role: clean(value.role),
    work_item_id: clean(value.work_item_id ?? value.workItemID)
  };
}

export function agentConversationKey(ref = {}) {
  const value = normalizeAgentConversationRef(ref);
  return [
    value.kind || "agent",
    value.session_id || "-",
    value.main_session_id || "-",
    value.team_id || "-",
    value.role || "-",
    value.work_item_id || "-"
  ].join(":");
}

function composerPrefix(ref) {
  return `agent-${stableToken(agentConversationKey(ref))}`;
}

function toolStatus(record) {
  const status = clean(record.status).toLowerCase();
  if (record.error) return "error";
  if (status === "requested" || status === "pending" || status === "running") return status === "running" ? "running" : "pending";
  return status || "success";
}

// agentConversationMessages 是统一记录到主会话组件的唯一映射点。
// system 是装配上下文，不属于对话正文；recovery_note 与 stage 则作为系统行
// 保留，用户才能看懂“为什么这个子会话从这里恢复/停在哪个阶段”。
export function agentConversationMessages(page = {}) {
  const records = Array.isArray(page.records) ? page.records : [];
  const messages = [];
  for (const [index, record] of records.entries()) {
    if (!record || typeof record !== "object") continue;
    const id = clean(record.id) || `record-${index + 1}`;
    const kind = clean(record.kind);
    if (kind === "system") continue;
    if (kind === "reasoning") continue;
    if (kind === "tool_call") {
      messages.push({
        id,
        role: "assistant",
        round_id: 0,
        content: "",
        tool: {
          id: clean(record.tool_call_id),
          name: clean(record.tool_name) || "tool",
          arguments: clean(record.content),
          status: toolStatus(record)
        }
      });
      continue;
    }
    if (kind === "tool_result") {
      messages.push({
        id,
        role: "tool_result",
        round_id: 0,
        content: clean(record.content),
        tool: {
          id: clean(record.tool_call_id),
          name: clean(record.tool_name) || "tool",
          result: clean(record.content),
          error: clean(record.error),
          status: toolStatus(record),
          truncated: record.truncated === true
        }
      });
      continue;
    }
    if (kind === "stage") {
      const title = clean(record.content) || "阶段";
      const preview = clean(record.reasoning_content);
      messages.push({
        id,
        role: "system",
        round_id: 0,
        content: preview ? `阶段 · ${title}\n${preview}` : `阶段 · ${title}`
      });
      continue;
    }
    if (kind === "recovery_note") {
      messages.push({
        id,
        role: "system",
        round_id: 0,
        content: clean(record.content) || "该子会话从持久记录恢复。"
      });
      continue;
    }
    const role = clean(record.role);
    if (role !== "user" && role !== "assistant") continue;
    const content = clean(record.content);
    if (!content) continue;
    messages.push({
      id,
      role,
      round_id: 0,
      role_name: role === "assistant" ? clean(record.role_name) : "",
      content
    });
  }
  return messages;
}

function statusChips(page, ref) {
  const status = clean(page?.status);
  const recovery = clean(page?.recovery);
  return [
    status ? `<span class="chip agent-conversation-status">${escapeHtml(status)}</span>` : "",
    recovery ? `<span class="chip agent-conversation-recovery is-${escapeHtml(recovery)}" title="会话恢复状态">${escapeHtml(RECOVERY_LABEL[recovery] || recovery)}</span>` : "",
    page?.truncated === true ? '<span class="chip agent-conversation-truncated" title="记录正文或分页已裁剪">已裁剪</span>' : "",
    ref.session_id ? `<span class="agent-conversation-session" title="${escapeHtml(ref.session_id)}">${escapeHtml(ref.session_id)}</span>` : ""
  ].filter(Boolean).join("");
}

function windowLabel(page) {
  const total = Number(page?.total) || 0;
  const shown = Array.isArray(page?.records) ? page.records.length : 0;
  if (total > shown) return `最近 ${shown}/${total} 条记录`;
  return total > 0 ? `${total} 条记录` : "尚无记录";
}

function agentConversationModel(page, options) {
  if (options.error) {
    return {
      items: [{
        key: "agent:error",
        html: `<div class="agent-conversation-empty is-error" role="alert">
            <strong>会话读取失败</strong>
            <span>${escapeHtml(options.error)}</span>
          </div>`,
        meta: { kind: "error" }
      }],
      payloads: new Map()
    };
  }
  if (options.loading === true || !page) {
    return {
      items: [{
        key: "agent:loading",
        html: renderConversationLoading("restoring"),
        meta: { kind: "loading" }
      }],
      payloads: new Map()
    };
  }
  const messages = agentConversationMessages(page);
  if (!messages.length) {
    return {
      items: [{
        key: "agent:empty",
        html: `<div class="agent-conversation-empty">
            <strong>这个子代理会话还没有对话记录</strong>
            <span>在下方输入框发消息，它会直接进入当前 ${escapeHtml(KIND_LABEL[options.ref.kind] || "agent")} 会话。</span>
          </div>`,
        meta: { kind: "empty" }
      }],
      payloads: new Map()
    };
  }
  return renderConversationModel(
    messages,
    { running: options.sending === true },
    "",
    null,
    options.sending ? "receiving" : "idle"
  );
}

function conversationBody(page, options) {
  return agentConversationModel(page, options).items.map(item => item.html).join("");
}

// renderAgentConversationPanel 是 Work Item、teammate 与 subagent 详情共用的
// 唯一会话面板。form 先留空，宿主插入 DOM 后调用
// mountAgentConversationComposer 克隆主 composer 模板。
export function renderAgentConversationPanel(page = null, options = {}) {
  const ref = normalizeAgentConversationRef(options.ref);
  const kind = KIND_LABEL[ref.kind] || "AGENT";
  const windowNote = windowLabel(page);
  const peer = ref.kind === "teammate"
    ? `${ref.role || "teammate"}${ref.work_item_id ? ` · ${ref.work_item_id}` : ""}`
    : ref.session_id;
  return `<section class="agent-conversation-panel" data-agent-conversation="${escapeHtml(agentConversationKey(ref))}" data-agent-kind="${escapeHtml(ref.kind)}">
      <header class="agent-conversation-head">
        <div class="agent-conversation-title">
          <span class="agent-conversation-kind">${escapeHtml(kind)}</span>
          <strong>${escapeHtml(peer || "会话")}</strong>
          <span class="agent-conversation-window">${escapeHtml(windowNote)}</span>
        </div>
        <div class="agent-conversation-chips">${statusChips(page, ref)}</div>
      </header>
      <div class="conversation agent-conversation-stream" data-agent-conversation-stream aria-live="polite"${options.sending ? ' aria-busy="true"' : ""}>
        ${conversationBody(page, { ...options, ref })}
      </div>
      <div class="agent-conversation-composer-slot">
        <div class="agent-conversation-target-note">
          <span>消息只发送到这个 ${escapeHtml(ref.kind || "agent")} 会话</span>
          <code title="${escapeHtml(ref.session_id)}">${escapeHtml(ref.session_id)}</code>
        </div>
        <form class="composer agent-conversation-composer"
          data-agent-chat-form
          data-agent-kind="${escapeHtml(ref.kind)}"
          data-agent-session-id="${escapeHtml(ref.session_id)}"
          data-agent-main-session-id="${escapeHtml(ref.main_session_id)}"
          data-agent-team-id="${escapeHtml(ref.team_id)}"
          data-agent-role="${escapeHtml(ref.role)}"
          data-agent-work-item-id="${escapeHtml(ref.work_item_id)}"></form>
      </div>
    </section>`;
}

export function agentConversationTargetFromForm(form) {
  if (!form) return null;
  return normalizeAgentConversationRef({
    kind: form.dataset.agentKind,
    session_id: form.dataset.agentSessionId,
    main_session_id: form.dataset.agentMainSessionId,
    team_id: form.dataset.agentTeamId,
    role: form.dataset.agentRole,
    work_item_id: form.dataset.agentWorkItemId
  });
}

// mountAgentConversationComposer 用与主会话完全相同的模板挂载 composer。
// 子代理不支持主会话专属的文件/图片/命令工具，模板里的 data-main-only 节点
// 由 CSS 收起；textarea、状态、发送键仍是同一份结构。
export function mountAgentConversationComposer(root, options = {}) {
  const form = root?.querySelector?.("[data-agent-chat-form]");
  if (!form) return null;
  const ref = agentConversationTargetFromForm(form) || normalizeAgentConversationRef(options.ref);
  const controls = mountComposerTemplate(form, composerPrefix(ref));
  form.dataset.agentComposerMounted = "true";
  for (const node of form.querySelectorAll("[data-main-only]")) node.hidden = true;
  if (controls?.prompt) {
    controls.prompt.rows = 2;
    controls.prompt.placeholder = ref.kind === "teammate"
      ? `继续对 ${ref.role || "teammate"} 说（${ref.work_item_id || "WorkItem"}）`
      : `继续对 ${ref.session_id} 说`;
  }
  if (controls?.status) controls.status.textContent = "该会话专属";
  if (controls?.send) {
    controls.send.title = `发送到 ${ref.session_id}`;
    controls.send.setAttribute("aria-label", controls.send.title);
  }
  return controls;
}

// mountAgentConversationPanel 复用主会话的 createConversationView：滚动锚点、
// 增量入场、完成态 settle、embed 帧缓冲与 reduced-motion 均由同一条实现决定。
// composer 仍由 mountComposerTemplate 提供，因而加载态、消息态和输入态共享
// 主会话的组件与动效语义。
export function mountAgentConversationPanel(root, options = {}) {
  const panel = root?.matches?.(".agent-conversation-panel")
    ? root
    : root?.querySelector?.(".agent-conversation-panel");
  if (!panel) return null;
  const stream = panel.querySelector("[data-agent-conversation-stream]");
  const ref = normalizeAgentConversationRef(options.ref);
  if (!stream) return mountAgentConversationComposer(panel, { ref });
  stream.replaceChildren();
  const view = createConversationView(stream, {
    notify: options.notify,
    copyText: options.copyText,
    openImage: options.openImage,
    fetchResult: options.fetchResult,
    openFile: options.openFile,
    embedMountRoot: options.embedMountRoot,
    doubleBuffer: options.doubleBuffer
  });
  const model = agentConversationModel(options.page || null, {
    ref,
    loading: options.loading === true,
    error: options.error || "",
    sending: options.sending === true
  });
  const messageCount = agentConversationMessages(options.page || null).length;
  view.render(model, {
    scrollMode: "bottom",
    hasMoreHistory: false,
    streaming: options.sending === true,
    streamPhase: options.sending ? "receiving" : "idle",
    revision: Number(options.page?.total) || 0,
    conversationCount: messageCount,
    loading: options.loading === true || !options.page
  });
  const composer = mountAgentConversationComposer(panel, { ref });
  hydrateIcons(panel);
  const controller = { panel, stream, view, composer, render(page, renderOptions = {}) {
    const chips = panel.querySelector(".agent-conversation-chips");
    const windowNode = panel.querySelector(".agent-conversation-window");
    if (chips) chips.innerHTML = statusChips(page, ref);
    if (windowNode) windowNode.textContent = windowLabel(page);
    stream.setAttribute("aria-busy", renderOptions.sending === true ? "true" : "false");
    const nextModel = agentConversationModel(page, {
      ref,
      loading: renderOptions.loading === true,
      error: renderOptions.error || "",
      sending: renderOptions.sending === true
    });
    view.render(nextModel, {
      scrollMode: renderOptions.scrollMode || "auto",
      hasMoreHistory: false,
      streaming: renderOptions.sending === true,
      streamPhase: renderOptions.sending ? "receiving" : "idle",
      revision: Number(page?.total) || 0,
      conversationCount: agentConversationMessages(page || null).length,
      loading: renderOptions.loading === true || !page
    });
  } };
  panel.__seelexAgentConversationController = controller;
  return controller;
}

export function refreshAgentConversationPanel(root, options = {}) {
  const panel = root?.matches?.(".agent-conversation-panel")
    ? root
    : root?.querySelector?.(".agent-conversation-panel");
  if (!panel) return null;
  const controller = panel.__seelexAgentConversationController;
  if (controller?.render) {
    controller.render(options.page || null, options);
    return controller;
  }
  return mountAgentConversationPanel(panel, options);
}
