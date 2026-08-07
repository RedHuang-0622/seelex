import { escapeHtml, hydrateIcons, icon, renderSources } from "./components.js";
import { createChatView } from "./chat-view.js";
import { createGUIClient } from "./client-state.js";
import { createConversationView } from "./conversation-view.js";
import { createEffortControl } from "./effort-control.js";
import { planToDSL, reconcilePlanDSL, renderNodeDetail, setNodeDetailConversation, bindNodeDetailTabs, renderSubagentTree, subagentTreeNodeToDSL } from "./plan-dsl.js";
import { collectReadFileSources } from "./read-sources.js";
import { renderContextCompactions } from "./context-summary.js";
import { createRuntimeEventBinder } from "./runtime-events.js";
import { createActiveChatSnapshotSync } from "./active-chat-sync.js";
import { renderTodoList } from "./todo-view.js";
import { renderScheduledTasks } from "./scheduled-tasks-view.js";
import { renderHistorySearchResults } from "./history-search.js";

const state = {
  info: null,
  commandTrigger: "/",
  commandSuggestions: [],
  commandSelected: 0,
  inlineSuggestions: [],
  inlineSelected: 0,
  inlineRequest: 0,
  resumingSessionID: ""
};

const elements = Object.fromEntries([
  "app-title", "app-version", "connection-dot", "provider-label", "token-label",
  "session-list", "session-count", "new-session",
  "plugin-list", "plugin-count", "account-list", "account-count", "conversation",
  "empty-state", "composer", "prompt", "composer-status", "stop-button", "send-button",
  "runtime-details", "effort-control", "effort-range", "effort-value", "plan-section", "plan-view", "subagent-section", "subagent-count", "subagent-tree-view", "todo-section", "todo-view", "todo-count", "scheduled-task-section", "scheduled-task-view", "scheduled-task-count", "new-scheduled-task", "scheduled-task-modal", "scheduled-task-close", "sched-name", "sched-kind", "sched-interval", "sched-command", "sched-command-field", "sched-prompt", "sched-prompt-field", "sched-enabled", "sched-submit", "history-search-section", "history-search-form", "history-search-input", "history-search-view", "history-search-count", "skill-list", "history-bar",
  "project-name", "project-root", "project-status", "project-overview", "project-sources", "source-count", "context-compactions",
  "runtime-button", "runtime-modal", "runtime-close", "settings-button", "settings-modal", "settings-close", "storage-backend", "storage-path", "storage-path-field", "storage-dsn", "storage-dsn-field", "storage-test", "storage-save", "storage-status", "inline-suggestions",
  "command-button", "command-modal", "command-close", "command-triggers", "command-search", "command-results",
  "load-history", "interaction-modal", "perm-toggle", "new-workspace", "workspace-info", "workspace-list", "interaction-risk", "interaction-title",
  "interaction-question", "interaction-preview", "interaction-options",
  "node-detail-modal", "node-detail-close", "node-detail-title", "node-detail-content", "toast"
].map(id => [id, document.getElementById(id)]));

const conversationView = createConversationView(elements.conversation, {
  copyText: value => navigator.clipboard.writeText(value),
  notify: showToast,
  loadMore: loadOlderHistory
});
const chatView = createChatView(elements, conversationView);
const client = createGUIClient({
  loadSnapshot: () => invoke("Snapshot"),
  onSnapshot: (snapshot, options) => render(snapshot, options),
  onIncremental: renderIncremental,
  onError: showToast
});
const bindRuntimeEvents = createRuntimeEventBinder({ client, onError: showToast });
const activeChatSync = createActiveChatSnapshotSync({
  refresh: () => refresh({ scroll: false }),
  onError: showToast
});
const effortControl = createEffortControl({
  root: elements["effort-control"],
  input: elements["effort-range"],
  output: elements["effort-value"],
  selectEffort: async level => {
    await invoke("SwitchEffort", level);
    await refresh({ scroll: false });
  },
  onError: showToast
});

function bridge() {
  return window.go?.gui?.Bridge;
}

async function invoke(method, ...args) {
  const api = bridge();
  if (!api || typeof api[method] !== "function") {
    throw new Error("GUI bridge 尚未就绪");
  }
  return api[method](...args);
}

function showToast(error) {
  elements.toast.textContent = error?.message || String(error);
  elements.toast.classList.remove("hidden");
  window.clearTimeout(showToast.timer);
  showToast.timer = window.setTimeout(() => elements.toast.classList.add("hidden"), 4200);
}

async function refresh(options = {}) {
  return client.refresh(options);
}

function render(snapshot, options = {}) {
  renderSessions(snapshot.sessions || [], snapshot.session || {}, snapshot.capabilities || {}, snapshot.session_workspaces || {}, snapshot.workspaces || []);
  renderProject(snapshot);
  renderRuntime(snapshot.runtime || {});
  renderPlugins(snapshot.runtime || {});
  renderAccounts(snapshot.runtime || {});
  chatView.render(snapshot, options.scrollMode);
  renderPlan(snapshot.runtime?.plan, snapshot.runtime?.subagent_tree);
  renderTodo(snapshot.runtime?.todo_items);
  renderScheduledTaskPanel(snapshot.runtime || {});
  renderSkills(snapshot.runtime?.skills || []);
  renderInteraction(snapshot.interaction);
  renderWorkspace(snapshot);
  activeChatSync.observe(snapshot);
}

function renderIncremental(snapshot, kind) {
  if (!snapshot) return;
  activeChatSync.observe(snapshot);
  if (["message.added", "message.delta", "tool.started", "tool.completed"].includes(kind)) {
    chatView.renderConversation(snapshot.conversation || [], snapshot.chat || {}, "auto", snapshot.has_more_history);
    chatView.renderControls(snapshot);
    if (kind !== "message.delta") renderProject(snapshot);
    return;
  }
  if (kind === "runtime.changed") {
    renderRuntime(snapshot.runtime || {});
    renderPlugins(snapshot.runtime || {});
    renderAccounts(snapshot.runtime || {});
    renderPlan(snapshot.runtime?.plan, snapshot.runtime?.subagent_tree);
    renderTodo(snapshot.runtime?.todo_items);
    renderScheduledTaskPanel(snapshot.runtime || {});
    renderSkills(snapshot.runtime?.skills || []);
    renderProject(snapshot);
    return;
  }
  if (["subagent.changed", "subagent.tool.started", "subagent.tool.completed"].includes(kind)) {
    renderPlan(snapshot.runtime?.plan, snapshot.runtime?.subagent_tree);
    if (activeNodeDetailKey) refreshOpenNodeDetail();
    return;
  }
  if (kind === "interaction.opened" || kind === "interaction.closed") renderInteraction(snapshot.interaction);
}

window.addEventListener("beforeunload", () => activeChatSync.stop());

function renderProject(snapshot) {
  const workspace = snapshot.current_workspace || null;
  const runtime = snapshot.runtime || {};
  const task = snapshot.task || null;
  const running = Boolean(snapshot.chat?.running);
  const sources = collectReadFileSources(snapshot.conversation || [], snapshot.read_files || []);
  const compactions = task?.context_compactions || [];
  elements["project-name"].textContent = workspace?.name || "No project selected";
  elements["project-root"].textContent = workspace?.root_path || "";
  elements["project-status"].innerHTML = [
    ["状态", running ? "Agent 执行中" : "Ready"],
    ["会话", snapshot.session?.draft ? "待发送" : shortSessionID(snapshot.session?.id || "—")],
    ["消息", String(snapshot.conversation?.length || 0)],
    ["任务", task ? task.status : "idle"],
    ["资料源", String(sources.length)]
  ].map(([label, value]) => `<div class="status-item"><span>${escapeHtml(label)}</span><strong title="${escapeHtml(value)}">${escapeHtml(value)}</strong></div>`).join("");
  elements["project-overview"].textContent = workspace
    ? (running
      ? `Current task is running with ${runtime.plugin || "default"} capabilities in this project scope.`
      : `This session can read and write only within ${workspace.name}.`)
    : "Select a project to define this session's read and write scope.";
  elements["context-compactions"].innerHTML = renderContextCompactions(compactions);
  elements["context-compactions"].classList.toggle("hidden", compactions.length === 0);
  elements["source-count"].textContent = String(sources.length);
  elements["project-sources"].innerHTML = renderSources(sources);
}

function renderSessions(sessions, current, capabilities, sessionWorkspaces, workspaces) {
  const currentID = current.id || "";
  const workspaceNames = new Map(workspaces.map(workspace => [workspace.id, workspace.name]));
  const items = sessions.map(session => session.id === currentID && current.name
    ? { ...session, name: current.name }
    : session);
  if (currentID && !items.some(session => session.id === currentID)) {
    items.unshift({ id: currentID, name: current.name || "", current: true });
  }
  elements["session-count"].textContent = String(items.length);
  elements["session-list"].innerHTML = items.length
    ? items.map(session => {
      const active = session.id === currentID;
	  const resuming = session.id === state.resumingSessionID;
      const updated = session.updated_at
        ? new Date(session.updated_at).toLocaleString([], { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" })
        : "当前会话";
      const workspaceID = sessionWorkspaces[session.id];
      const scope = workspaceID ? `Project: ${workspaceNames.get(workspaceID) || workspaceID}` : "No project";
      const detail = session.token_count ? `${updated} · ${session.token_count} tokens · ${scope}` : `${updated} · ${scope}`;
      return `<div class="session-row">
        <button class="stack-button session-button ${active ? "active" : ""}" data-session="${escapeHtml(session.id)}" ${resuming ? "disabled" : ""}>
          <span class="session-name">${icon("message", 13)} ${escapeHtml(resuming ? "恢复中…" : (session.name || shortSessionID(session.id)))}</span><small>${escapeHtml(detail)}</small>
        </button>
        <button class="session-del" data-session="${escapeHtml(session.id)}" title="删除会话" aria-label="删除会话">✕</button>
      </div>`;
    }).join("")
    : '<span class="muted list-empty">暂无会话</span>';

  elements["session-list"].querySelectorAll(".session-button").forEach(button => {
    button.addEventListener("click", async () => {
      if (button.dataset.session === currentID) return;
      if (!capabilities.session_resume) {
        showToast(capabilities.session_resume_reason || "当前版本暂不支持恢复历史会话");
        return;
      }
      const sessionID = button.dataset.session;
      state.resumingSessionID = sessionID;
      elements["composer-status"].textContent = "正在恢复会话…";
      renderSessions(sessions, current, capabilities, sessionWorkspaces, workspaces);
      try {
        await invoke("ResumeSession", sessionID);
        await refresh({ scroll: "bottom" });
        elements["composer-status"].textContent = "";
      } catch (error) {
        elements["composer-status"].textContent = `恢复会话失败：${error?.message || String(error)}`;
        showToast(error);
      } finally {
        state.resumingSessionID = "";
        const latest = client.current() || { sessions, session: current, capabilities, session_workspaces: sessionWorkspaces, workspaces };
        renderSessions(latest.sessions || sessions, latest.session || current, latest.capabilities || capabilities, latest.session_workspaces || sessionWorkspaces, latest.workspaces || workspaces);
      }
    });
  });
  elements["session-list"].querySelectorAll(".session-del").forEach(button => {
    button.addEventListener("click", async event => {
      event.stopPropagation();
      const sessionID = button.dataset.session;
      if (!sessionID || sessionID === currentID) {
        showToast("不能删除当前会话");
        return;
      }
      if (!confirm(`确认删除会话 ${shortSessionID(sessionID)}？`)) return;
      try { await invoke("DeleteSession", sessionID); await refresh({ scroll: false }); }
      catch (error) { showToast(error); }
    });
  });
}

function shortSessionID(id) {
  const value = String(id || "");
  return value.length > 18 ? `${value.slice(0, 8)}…${value.slice(-6)}` : value;
}

function renderRuntime(runtime) {
  elements["provider-label"].textContent = [runtime.provider, runtime.model].filter(Boolean).join(" · ") || "ready";
  elements["token-label"].textContent = runtime.tokens || "";
  elements["runtime-details"].innerHTML = [
    ["Model", runtime.model || "—"],
    ["Provider", runtime.provider || "—"],
    ["Plugin", runtime.plugin || "—"],
    ["Tools", String(runtime.visible_tools?.length || 0)]
  ].map(([key, value]) => `<dt>${escapeHtml(key)}</dt><dd>${escapeHtml(value)}</dd>`).join("");

  const fullAccess = Boolean(runtime.full_access);
  elements["perm-toggle"].classList.toggle("is-on", fullAccess);
  elements["perm-toggle"].textContent = fullAccess ? "FA ✓" : "FA";

  effortControl.setLevel(runtime.effort);
}

function renderPlugins(runtime) {
  const plugins = runtime.plugins || [];
  elements["plugin-count"].textContent = String(plugins.length);
  elements["plugin-list"].innerHTML = plugins.map(plugin => `
    <button class="stack-button ${runtime.plugin === plugin.name ? "active" : ""}" data-plugin="${escapeHtml(plugin.name)}">
      ${escapeHtml(plugin.name)}<small>${escapeHtml(plugin.description || "")}</small>
    </button>`).join("");
  elements["plugin-list"].querySelectorAll("button").forEach(button => {
    button.addEventListener("click", async () => {
      try { await invoke("SwitchPlugin", button.dataset.plugin); await refresh({ scroll: false }); }
      catch (error) { showToast(error); }
    });
  });
}

function renderAccounts(runtime) {
  const accounts = runtime.accounts || [];
  elements["account-count"].textContent = String(accounts.length);
  elements["account-list"].innerHTML = accounts.map(account => `
    <button class="stack-button ${runtime.account === account.name ? "active" : ""}" data-account="${escapeHtml(account.name)}" ${account.disabled ? "disabled" : ""}>
      ${escapeHtml(account.name)}<small>${escapeHtml(`${account.provider || ""} ${account.model || ""}`.trim())}</small>
    </button>`).join("");
  elements["account-list"].querySelectorAll("button").forEach(button => {
    button.addEventListener("click", async () => {
      try { await invoke("SelectAccount", button.dataset.account); await refresh({ scroll: false }); }
      catch (error) { showToast(error); }
    });
  });
}

function renderSkills(skills) {
  elements["skill-list"].innerHTML = skills.length
    ? skills.map(skill => `<span class="chip" title="${escapeHtml(skill.description || "")}">#${escapeHtml(skill.name)}</span>`).join("")
    : '<span class="muted">当前 Plugin 无 Skill</span>';
}

// lastPlanDsl 保存最近一次渲染的 Plan DSL（节点详情弹窗的数据源；
// 权威 JSON 来自 snapshot.runtime.plan，弹窗只读展示）。
// lastSubagentTree 保存子代理树投影（snapshot.runtime.subagent_tree；
// fork 节点不在 Plan 快照里时详情弹窗的兜底数据源）。
let lastPlanDsl = null;
let lastSubagentTree = [];

function renderPlan(plan, subagentTree = null) {
  elements["plan-section"].classList.toggle("hidden", !plan);
  lastPlanDsl = planToDSL(plan);
  reconcilePlanDSL(elements["plan-view"], lastPlanDsl);
  renderSubagentPanel(subagentTree);
}

// renderSubagentPanel 渲染子代理树独立 section（工作区右栏「子代理」分区；
// fork 子代理产出完整会话/上下文/工具活动都在这里，不进对话区）。空树隐藏
// 整个 section；count 徽标显示节点数（含嵌套子代理）。
function renderSubagentPanel(subagentTree) {
  lastSubagentTree = Array.isArray(subagentTree) ? subagentTree : [];
  const section = elements["subagent-section"];
  const container = elements["subagent-tree-view"];
  if (!section || !container) return;
  container.innerHTML = renderSubagentTree(lastSubagentTree);
  section.classList.toggle("hidden", !lastSubagentTree.length);
  if (elements["subagent-count"]) {
    elements["subagent-count"].textContent = String(countSubagentNodes(lastSubagentTree));
  }
}

// countSubagentNodes 统计树节点数（递归含嵌套子代理；不含合成根 main）。
function countSubagentNodes(nodes) {
  let count = 0;
  const walk = items => {
    for (const item of items || []) {
      if (!item || typeof item !== "object") continue;
      if (item.id !== "main") count += 1;
      walk(item.children);
    }
  };
  walk(nodes);
  return count;
}

// findSubagentTreeNode 在子代理树投影里按 id 找节点（含嵌套 children）。
function findSubagentTreeNode(nodeID) {
  const walk = items => {
    for (const item of items || []) {
      if (!item || typeof item !== "object") continue;
      if (item.id === nodeID) return item;
      const found = walk(item.children);
      if (found) return found;
    }
    return null;
  };
  return walk(lastSubagentTree);
}

// renderTodo 渲染 todolist 待办面板：数据来自 snapshot.runtime.todo_items
// （权威投影），无清单时隐藏整个 section。
function renderTodo(items) {
  const list = Array.isArray(items) ? items : [];
  elements["todo-section"].classList.toggle("hidden", list.length === 0);
  elements["todo-count"].textContent = String(list.length);
  elements["todo-view"].innerHTML = renderTodoList(list);
}

// renderScheduledTaskPanel 渲染定时周期任务面板：数据来自 snapshot.runtime
// （权威投影，调度器状态变化经 runtime.changed 增量带到）；任务只读展示，
// 新建按钮常驻（命令白名单来自 runtime.scheduled_commands）。
function renderScheduledTaskPanel(runtime) {
  const tasks = Array.isArray(runtime.scheduled_tasks) ? runtime.scheduled_tasks : [];
  elements["scheduled-task-count"].textContent = String(tasks.length);
  elements["scheduled-task-view"].innerHTML = renderScheduledTasks(tasks, runtime.scheduled_commands || []);
}

// openNodeDetail 渲染并打开节点详情弹窗（子代理详情页）：
// 会话记录（invoke SubagentSessionDetail，运行中 2s 轮询）+ 事件时间线 +
// 状态/耗时/输出。
let nodeDetailPollTimer = null;
let activeNodeDetailKey = "";
let activeNodeDetailID = "";
let nodeDetailGeneration = 0;

// resolveNodeForDetail 解析详情弹窗的节点数据：优先 Plan DSL（活跃 Plan 的
// 权威投影）；fork 子代理节点在 Plan 已清除时回退到子代理树投影（会话记录/
// 上下文仍由 SubagentSessionDetail 数据面承载）。
function resolveNodeForDetail(nodeKey) {
  const node = lastPlanDsl?.nodes?.find(candidate => candidate.key === nodeKey);
  if (node) return node;
  const treeNode = findSubagentTreeNode(nodeKey);
  return treeNode ? subagentTreeNodeToDSL(treeNode) : null;
}

async function openNodeDetail(nodeKey) {
  const node = resolveNodeForDetail(nodeKey);
  if (!node) return;
  const fromTree = Boolean(findSubagentTreeNode(nodeKey));
  activeNodeDetailKey = nodeKey;
  activeNodeDetailID = node.id;
  const generation = nodeDetailGeneration += 1;
  // Plan DSL 节点带 plan/tasklist 模式徽标；子代理树投影节点固定 plan 模式。
  const rendered = renderNodeDetail({ ...node, mode: node.mode || lastPlanDsl?.mode || "plan" });
  elements["node-detail-content"].innerHTML = rendered;
  elements["node-detail-title"].innerHTML = `<span class="eyebrow">Node</span>`;
  bindNodeDetailTabs(elements["node-detail-content"]);
  // 子代理树节点（fork 不在 Plan 快照里）默认打开「上下文」标签：运行时
  // 上下文查看是它的主诉求（会话记录/上下文/工具活动，2s 轮询实时刷新）。
  if (fromTree) {
    elements["node-detail-content"].querySelector('[data-node-tab="context"]')?.click();
  }
  setModal("node-detail-modal", true);
  await refreshNodeDetail(node.id, generation);
}

function refreshOpenNodeDetail() {
  const node = resolveNodeForDetail(activeNodeDetailKey);
  if (!node) return;
  const selectedTab = elements["node-detail-content"].querySelector("[data-node-tab].is-active")?.dataset.nodeTab || "conversation";
  elements["node-detail-content"].innerHTML = renderNodeDetail({ ...node, mode: node.mode || lastPlanDsl?.mode || "plan" });
  bindNodeDetailTabs(elements["node-detail-content"]);
  const selected = elements["node-detail-content"].querySelector(`[data-node-tab="${selectedTab}"]`);
  selected?.click();
  void refreshNodeDetail(activeNodeDetailID, nodeDetailGeneration);
}

// refreshNodeDetail 拉取子代理详情（会话记录）并渲染；运行中每 2s 轮询。
async function refreshNodeDetail(nodeID, generation = nodeDetailGeneration) {
  if (generation === nodeDetailGeneration && nodeID === activeNodeDetailID && nodeDetailPollTimer) {
    clearTimeout(nodeDetailPollTimer);
    nodeDetailPollTimer = null;
  }
  let detail = null;
  try {
    detail = await invoke("SubagentSessionDetail", nodeID);
  } catch { /* 节点无会话记录或非 agent 节点 → 面板保持占位 */ }
  if (generation !== nodeDetailGeneration || nodeID !== activeNodeDetailID || !document.querySelector("[data-node-detail]")) return;
  setNodeDetailConversation(detail || null);
  if (detail?.running) {
    nodeDetailPollTimer = setTimeout(() => refreshNodeDetail(nodeID, generation), 2000);
  }
}

function closeNodeDetail() {
  nodeDetailGeneration += 1;
  if (nodeDetailPollTimer) {
    clearTimeout(nodeDetailPollTimer);
    nodeDetailPollTimer = null;
  }
  activeNodeDetailKey = "";
  activeNodeDetailID = "";
  setModal("node-detail-modal", false);
}

function renderInteraction(interaction) {
  elements["interaction-modal"].classList.toggle("hidden", !interaction);
  if (!interaction) return;
  // 诊断日志（权限弹窗排查用；正常运行时无副作用）。
  console.log("[interaction] opened:", interaction.id, interaction.tool_name || interaction.kind);
  elements["interaction-risk"].textContent = interaction.risk || interaction.kind || "approval";
  elements["interaction-title"].textContent = interaction.title || "需要确认";
  elements["interaction-question"].textContent = interaction.question || interaction.tool_name || "是否继续？";
  elements["interaction-preview"].textContent = interaction.preview || "";
  elements["interaction-preview"].classList.toggle("hidden", !interaction.preview);
  elements["interaction-options"].innerHTML = (interaction.options || []).map(option =>
    `<button data-option="${escapeHtml(option.id)}" class="${escapeHtml(option.style || "")}">${escapeHtml(option.label)}</button>`
  ).join("");
  elements["interaction-options"].querySelectorAll("button").forEach(button => {
    button.addEventListener("click", async () => {
      try { await invoke("ResolveInteraction", interaction.id, button.dataset.option); await refresh({ scroll: false }); }
      catch (error) { showToast(error); }
    });
  });
  // 弹窗打开时聚焦首个选项按钮（审批等待中强制可见交互入口）。
  const firstOption = elements["interaction-options"].querySelector("button");
  if (firstOption) setTimeout(() => firstOption.focus(), 0);
}

function setModal(id, open) {
  elements[id].classList.toggle("hidden", !open);
}

function openRuntime() {
  setModal("runtime-modal", true);
}

function closeRuntime() {
  setModal("runtime-modal", false);
}

async function openSettings() {
  setModal("settings-modal", true);
  elements["storage-status"].textContent = "";
  try {
    const config = await invoke("SessionStorageConfig");
    elements["storage-backend"].value = config.backend || "json";
    elements["storage-path"].value = config.path || "";
    elements["storage-dsn"].value = "";
    elements["storage-dsn"].placeholder = config.dsn === "configured" ? "已配置；留空则保持不变" : storageDSNPlaceholder();
    updateStorageFields();
  } catch (error) { showToast(error); }
}

function closeSettings() { setModal("settings-modal", false); }

function storageConfig() {
  return { backend: elements["storage-backend"].value, path: elements["storage-path"].value.trim(), dsn: elements["storage-dsn"].value.trim() };
}

function updateStorageFields() {
  const remote = ["postgres", "redis"].includes(elements["storage-backend"].value);
  elements["storage-path-field"].classList.toggle("hidden", remote);
  elements["storage-dsn-field"].classList.toggle("hidden", !remote);
  if (elements["storage-dsn"].value === "") elements["storage-dsn"].placeholder = storageDSNPlaceholder();
}

function storageDSNPlaceholder() {
  return elements["storage-backend"].value === "redis"
    ? "redis://:password@host:6379/0"
    : "postgres://user:password@host:5432/database?sslmode=require";
}

async function testStorage() {
  elements["storage-status"].textContent = "正在测试…";
  try { await invoke("TestSessionStorage", storageConfig()); elements["storage-status"].textContent = "连接与读写初始化成功。"; }
  catch (error) { elements["storage-status"].textContent = `失败：${error}`; }
}

async function saveStorage() {
  elements["storage-status"].textContent = "正在切换…";
  try { await invoke("ConfigureSessionStorage", storageConfig()); elements["storage-status"].textContent = "已保存；后续写入将使用该存储。"; }
  catch (error) { elements["storage-status"].textContent = `失败：${error}`; }
}

async function openCommandPalette(trigger = "/") {
  state.commandTrigger = ["/", "#", "@"].includes(trigger) ? trigger : "/";
  state.commandSelected = 0;
  elements["command-search"].value = state.commandTrigger;
  syncCommandTriggers();
  setModal("command-modal", true);
  await updateCommandResults();
  elements["command-search"].focus();
  elements["command-search"].setSelectionRange(1, 1);
}

function closeCommandPalette() {
  setModal("command-modal", false);
}

function syncCommandTriggers() {
  elements["command-triggers"].querySelectorAll("button").forEach(button => {
    button.classList.toggle("active", button.dataset.trigger === state.commandTrigger);
  });
}

async function updateCommandResults() {
  let input = elements["command-search"].value.trimStart();
  if (!["/", "#", "@"].includes(input[0])) {
    input = state.commandTrigger + input;
    elements["command-search"].value = input;
  } else {
    state.commandTrigger = input[0];
    syncCommandTriggers();
  }
  try {
    state.commandSuggestions = await invoke("Suggestions", input) || [];
    state.commandSelected = Math.min(state.commandSelected, Math.max(state.commandSuggestions.length - 1, 0));
    renderSuggestionList(elements["command-results"], state.commandSuggestions, state.commandSelected, state.commandTrigger);
  } catch (error) {
    showToast(error);
  }
}

function renderSuggestionList(container, suggestions, selected, trigger, limit = suggestions.length) {
  const visible = suggestions.slice(0, limit);
  container.innerHTML = visible.length
    ? visible.map((suggestion, index) => `<button class="command-result ${index === selected ? "selected" : ""}" type="button" data-index="${index}">
      <span class="command-result-icon">${icon(suggestionIcon(suggestion.kind), 14)}</span>
      <span class="command-prefix">${escapeHtml(trigger)}${escapeHtml(suggestion.text)}</span>
      <span class="command-description">${escapeHtml(suggestion.description || "")}</span>
      <span class="command-kind">${escapeHtml(suggestion.kind || "command")}</span>
    </button>`).join("")
    : '<span class="muted list-empty">没有匹配的指令</span>';
  container.querySelectorAll("button").forEach(button => {
    button.addEventListener("click", () => acceptSuggestion(visible[Number(button.dataset.index)], trigger));
  });
}

function suggestionIcon(kind) {
  return ({ skill: "skill", plugin: "plugin", tool: "terminal", command: "command" })[kind] || "command";
}

function acceptSuggestion(suggestion, trigger) {
  if (!suggestion) return;
  elements.prompt.value = `${trigger}${suggestion.text} `;
  resizePrompt();
  closeCommandPalette();
  hideInlineSuggestions();
  elements.prompt.focus();
  elements.prompt.setSelectionRange(elements.prompt.value.length, elements.prompt.value.length);
}

async function updateInlineSuggestions() {
  const input = elements.prompt.value.trimStart();
  if (!/^[\/#@][^\s]*$/.test(input)) {
    hideInlineSuggestions();
    return;
  }
  const request = ++state.inlineRequest;
  try {
    const suggestions = await invoke("Suggestions", input) || [];
    if (request !== state.inlineRequest) return;
    state.inlineSuggestions = suggestions.slice(0, 8);
    state.inlineSelected = Math.min(state.inlineSelected, Math.max(state.inlineSuggestions.length - 1, 0));
    elements["inline-suggestions"].classList.toggle("hidden", state.inlineSuggestions.length === 0);
    renderSuggestionList(elements["inline-suggestions"], state.inlineSuggestions, state.inlineSelected, input[0], 8);
  } catch (error) {
    hideInlineSuggestions();
    showToast(error);
  }
}

function hideInlineSuggestions() {
  state.inlineRequest++;
  state.inlineSuggestions = [];
  state.inlineSelected = 0;
  elements["inline-suggestions"].classList.add("hidden");
}

elements.composer.addEventListener("submit", async event => {
  event.preventDefault();
  const text = elements.prompt.value.trim();
  if (!text) return;
  try {
    await invoke("Submit", text);
    elements.prompt.value = "";
    hideInlineSuggestions();
    resizePrompt();
    await refresh({ scroll: "bottom" });
  } catch (error) { showToast(error); }
});

elements.prompt.addEventListener("keydown", event => {
  if (!elements["inline-suggestions"].classList.contains("hidden") && state.inlineSuggestions.length) {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const direction = event.key === "ArrowDown" ? 1 : -1;
      state.inlineSelected = (state.inlineSelected + direction + state.inlineSuggestions.length) % state.inlineSuggestions.length;
      renderSuggestionList(elements["inline-suggestions"], state.inlineSuggestions, state.inlineSelected, elements.prompt.value.trimStart()[0], 8);
      return;
    }
    if (event.key === "Tab") {
      event.preventDefault();
      acceptSuggestion(state.inlineSuggestions[state.inlineSelected], elements.prompt.value.trimStart()[0]);
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      hideInlineSuggestions();
      return;
    }
  }
  if (event.key === "Enter" && !event.shiftKey) {
    event.preventDefault();
    elements.composer.requestSubmit();
  }
});
elements.prompt.addEventListener("input", () => {
  resizePrompt();
  state.inlineSelected = 0;
  updateInlineSuggestions();
});

elements["stop-button"].addEventListener("click", async () => {
  elements["stop-button"].disabled = true;
  try {
    // The backend owns the active request ID. Passing an empty ID avoids a
    // stale renderer snapshot preventing cancellation after a queued turn
    // has rotated the request ID.
    const cancelled = await invoke("CancelChat", "");
    if (!cancelled) showToast("当前任务已结束或取消请求未生效");
    await refresh({ scroll: false });
  }
  catch (error) { showToast(error); }
  finally { elements["stop-button"].disabled = false; }
});

async function loadOlderHistory() {
  try { await invoke("LoadMoreHistory", 50); await refresh({ scroll: "anchor" }); }
  catch (error) { showToast(error); }
}

elements["load-history"].addEventListener("click", loadOlderHistory);

elements["new-session"].addEventListener("click", async () => {
  try { await invoke("BeginNewSession"); await refresh({ scroll: "bottom" }); }
  catch (error) { showToast(error); }
});

elements["runtime-button"].addEventListener("click", openRuntime);
elements["runtime-close"].addEventListener("click", closeRuntime);
elements["settings-button"].addEventListener("click", openSettings);
elements["settings-close"].addEventListener("click", closeSettings);
elements["storage-backend"].addEventListener("change", updateStorageFields);
elements["storage-test"].addEventListener("click", testStorage);
elements["storage-save"].addEventListener("click", saveStorage);
elements["command-button"].addEventListener("click", () => openCommandPalette("/"));
elements["command-close"].addEventListener("click", closeCommandPalette);

elements["command-triggers"].querySelectorAll("button").forEach(button => {
  button.addEventListener("click", () => openCommandPalette(button.dataset.trigger));
});

elements["command-search"].addEventListener("input", () => {
  state.commandSelected = 0;
  updateCommandResults();
});

elements["command-search"].addEventListener("keydown", event => {
  if (event.key === "Escape") {
    event.preventDefault();
    closeCommandPalette();
    elements.prompt.focus();
    return;
  }
  if ((event.key === "ArrowDown" || event.key === "ArrowUp") && state.commandSuggestions.length) {
    event.preventDefault();
    const direction = event.key === "ArrowDown" ? 1 : -1;
    state.commandSelected = (state.commandSelected + direction + state.commandSuggestions.length) % state.commandSuggestions.length;
    renderSuggestionList(elements["command-results"], state.commandSuggestions, state.commandSelected, state.commandTrigger);
    elements["command-results"].querySelector(".selected")?.scrollIntoView({ block: "nearest" });
    return;
  }
  if ((event.key === "Enter" || event.key === "Tab") && state.commandSuggestions.length) {
    event.preventDefault();
    acceptSuggestion(state.commandSuggestions[state.commandSelected], state.commandTrigger);
  }
});

// ── 历史检索 ───────────────────────────────────────────

// runHistorySearch 提交检索：查询非空校验（空查询后端也拒绝），结果来自
// Bridge SearchHistory 的权威返回（压缩栈索引命中 → 真实聊天记录）。
// limit 固定 5 条命中；token 预算由后端 search 包硬上限约束。
async function runHistorySearch() {
  const query = elements["history-search-input"].value.trim();
  if (!query) {
    showToast("请输入检索关键词");
    return;
  }
  try {
    const result = await invoke("SearchHistory", query, 5);
    elements["history-search-count"].textContent = String((result?.hits || []).length);
    elements["history-search-view"].classList.remove("muted");
    elements["history-search-view"].innerHTML = renderHistorySearchResults(result);
  } catch (error) {
    showToast(error);
  }
}

elements["history-search-form"].addEventListener("submit", event => {
  event.preventDefault();
  runHistorySearch();
});

// ── 定时周期任务 ───────────────────────────────────────────

// openScheduledTaskDialog 打开新建弹窗：白名单命令来自权威 snapshot
// （runtime.scheduled_commands），无可用命令时下拉为空并禁用提交；
// 类型切换联动命令/提示词字段。
function openScheduledTaskDialog() {
  const runtime = client.current()?.runtime || {};
  const commands = Array.isArray(runtime.scheduled_commands) ? runtime.scheduled_commands : [];
  elements["sched-command"].innerHTML = commands.length
    ? commands.map(command => `<option value="${escapeHtml(command.key)}">${escapeHtml(command.label || command.key)}</option>`).join("")
    : '<option value="">（无可用白名单命令）</option>';
  syncScheduledTaskFields();
  setModal("scheduled-task-modal", true);
  elements["sched-name"].focus();
}

function closeScheduledTaskDialog() {
  setModal("scheduled-task-modal", false);
}

function syncScheduledTaskFields() {
  const promptKind = elements["sched-kind"].value === "prompt";
  elements["sched-prompt-field"].classList.toggle("hidden", !promptKind);
  elements["sched-command-field"].classList.toggle("hidden", promptKind);
}

// submitScheduledTask 组装任务入参并提交 Bridge ScheduleTask
// （间隔为分钟 → Go time.Duration 纳秒；sessionId 留空 = 绑定当前主会话）。
async function submitScheduledTask() {
  const name = elements["sched-name"].value.trim();
  const kind = elements["sched-kind"].value;
  const minutes = Number(elements["sched-interval"].value);
  if (!name) {
    showToast("请填写任务名称");
    return;
  }
  if (!Number.isFinite(minutes) || minutes < 1) {
    showToast("周期至少 1 分钟");
    return;
  }
  if (kind === "command" && !elements["sched-command"].value) {
    showToast("当前没有可用的白名单命令");
    return;
  }
  const spec = {
    name,
    kind,
    interval: Math.round(minutes) * 60 * 1e9,
    command: kind === "command" ? elements["sched-command"].value : "",
    prompt: kind === "prompt" ? elements["sched-prompt"].value.trim() : "",
    sessionId: "",
    enabled: elements["sched-enabled"].checked
  };
  try {
    await invoke("ScheduleTask", spec);
    closeScheduledTaskDialog();
    await refresh({ scroll: false });
  } catch (error) {
    showToast(error);
  }
}

elements["new-scheduled-task"].addEventListener("click", openScheduledTaskDialog);
elements["scheduled-task-close"].addEventListener("click", closeScheduledTaskDialog);
elements["sched-kind"].addEventListener("change", syncScheduledTaskFields);
elements["sched-submit"].addEventListener("click", submitScheduledTask);

// 取消按钮事件委托（任务列表渲染全量刷新，事件挂容器层；ID 是操作键）。
elements["scheduled-task-view"].addEventListener("click", async event => {
  const button = event.target.closest?.("[data-sched-cancel]");
  if (!button?.dataset.schedCancel) return;
  if (!confirm("确认取消该周期任务？")) return;
  try {
    await invoke("CancelScheduledTask", button.dataset.schedCancel);
    await refresh({ scroll: false });
  } catch (error) {
    showToast(error);
  }
});

for (const [modalID, close] of [["runtime-modal", closeRuntime], ["command-modal", closeCommandPalette], ["settings-modal", closeSettings], ["scheduled-task-modal", closeScheduledTaskDialog], ["node-detail-modal", closeNodeDetail]]) {
  elements[modalID].addEventListener("click", event => {
    if (event.target === elements[modalID]) close();
  });
}

elements["node-detail-close"].addEventListener("click", closeNodeDetail);

// Plan 面板节点详情入口：整卡、详情按钮均可打开；运行中子代理也可查看。
document.addEventListener("click", event => {
  const node = event.target.closest?.("[data-plan-node-open]");
  if (node) openNodeDetail(node.dataset.planNodeOpen);
});

document.addEventListener("keydown", event => {
  if (event.key !== "Enter" && event.key !== " ") return;
  const node = event.target.closest?.("[data-plan-node-open]");
  if (!node) return;
  event.preventDefault();
  openNodeDetail(node.dataset.planNodeOpen);
});

document.addEventListener("keydown", event => {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
    event.preventDefault();
    openCommandPalette("/");
  }
  if (event.key === "Escape") {
    closeRuntime();
    closeCommandPalette();
    closeSettings();
    closeScheduledTaskDialog();
    closeNodeDetail();
  }
});

function renderWorkspace(snapshot) {
  const ws = snapshot.current_workspace;
  const list = snapshot.workspaces || [];
  if (ws) {
    const gitOk = Boolean(ws.git_remote);
    elements["workspace-info"].innerHTML =
      '<div class="ws-current"><strong>' + escapeHtml(ws.name) + '</strong>' +
      '<small>' + escapeHtml(ws.root_path || "") + '</small>' +
      (gitOk ? '<a class="ws-git" href="' + escapeHtml(ws.git_remote) + '" target="_blank">' + escapeHtml(ws.git_remote) + '</a>' : '<span class="ws-warn">未关联仓库</span>') +
      '<button id="unbind-workspace" class="text-button" type="button">Unbind project</button>' +
      '</div>';
    elements["workspace-info"].querySelector("#unbind-workspace").addEventListener("click", async function() {
      try { await invoke("UnbindWorkspace"); await refresh({ scroll: false }); }
      catch (error) { showToast(error); }
    });
  } else {
    elements["workspace-info"].innerHTML = '<span class="muted">未绑定工作区 — 文件读写受限</span>';
  }
  elements["workspace-list"].innerHTML = list.map(function(w) {
    return '<button class="stack-button' + (ws && ws.id === w.id ? ' active' : '') + '" data-ws="' + escapeHtml(w.id) + '">' +
      escapeHtml(w.name) + '<small>' + escapeHtml(w.root_path || "") + '</small></button>';
  }).join("");
  elements["workspace-list"].querySelectorAll("button").forEach(function(btn) {
    btn.addEventListener("click", async function() {
      try { await invoke("BindWorkspace", btn.dataset.ws); await refresh({ scroll: false }); }
      catch (error) { showToast(error); }
    });
  });
}

// FA toggle
elements["perm-toggle"].addEventListener("click", async function() {
  const next = !Boolean(client.current()?.runtime?.full_access);
  try { await invoke("SetFullAccess", next); await refresh({ scroll: false }); }
  catch (error) { showToast(error); }
});

// New workspace
elements["new-workspace"].addEventListener("click", async function() {
  try {
    var dir = await invoke("PickDirectory");
    if (!dir) return;
    var name = dir.split(/[\\/]/).pop() || "workspace";
    await invoke("CreateWorkspace", name, dir, "");
    await refresh({ scroll: false });
  } catch (error) { showToast(error); }
});

function resizePrompt() {
  elements.prompt.style.height = "auto";
  elements.prompt.style.height = `${Math.min(elements.prompt.scrollHeight, 180)}px`;
}

async function initialise() {
  try {
    hydrateIcons();
    if (!bindRuntimeEvents(window.runtime)) {
      throw new Error("GUI event runtime 尚未就绪");
    }
    const info = await invoke("Info");
    state.info = info;
    elements["app-title"].textContent = info.title || "Seelex";
    elements["app-version"].textContent = info.version || "dev";
    await refresh({ scroll: "bottom" });
  } catch (error) {
    showToast(error);
    window.setTimeout(initialise, 600);
  }
}
initialise();
