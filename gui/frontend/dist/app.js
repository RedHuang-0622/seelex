import { escapeHtml, hydrateIcons, icon } from "./components.js";
import { createChatView } from "./chat-view.js";
import { createGUIClient } from "./client-state.js";
import { createConversationView } from "./conversation-view.js";
import { createTrajectoryView } from "./trajectory-view.js";
import { buildTrajectory } from "./trajectory.js";
import { createEffortControl } from "./effort-control.js";
import { planToDSL, renderNodeDetail, setNodeDetailConversation, bindNodeDetailTabs, subagentTreeNodeToDSL } from "./plan-dsl.js";
import { createWorkTableView, countUnread, workTableSignatures } from "./work-table.js";
import { createWorkTreeView } from "./worktree-view.js";
import { createGitLogView } from "./git-log-view.js";
import { renderContextCompactions } from "./context-summary.js";
import { createRuntimeEventBinder } from "./runtime-events.js";
import { createActiveChatSnapshotSync } from "./active-chat-sync.js";
import { renderScheduledTasks, renderScheduledTasksTable } from "./scheduled-tasks-view.js";
import { renderHistorySearchResults } from "./history-search.js";
import { truncateTitle, isPinned, togglePinned } from "./sidebar.js";
import { createPerfHooks } from "./perf-hooks.js";

const state = {
  info: null,
  commandTrigger: "/",
  commandSuggestions: [],
  commandSelected: 0,
  inlineSuggestions: [],
  inlineSelected: 0,
  inlineRequest: 0,
  resumingSessionID: "",
  tab: "conversation",
  rightTab: "status",
  trajectoryFilter: "all"
};

const elements = Object.fromEntries([
  "app-title", "app-version", "connection-dot", "provider-label", "token-label",
  "session-list", "session-count", "new-session",
  "plugin-list", "plugin-count", "account-list", "account-count", "conversation", "conversation-tabs", "trajectory",
  "empty-state", "composer", "prompt", "composer-status", "stop-button", "send-button",
  "runtime-details", "effort-control", "effort-range", "effort-value", "work-section", "work-count", "work-unread", "work-table-open", "work-table-summary", "work-table-modal", "work-table-modal-close", "work-table-modal-view", "scheduled-task-section", "scheduled-task-view", "scheduled-task-count", "new-scheduled-task", "scheduled-task-modal", "scheduled-task-close", "sched-name", "sched-kind", "sched-mode", "sched-period-value", "sched-period-unit", "sched-period-field", "sched-datetime", "sched-datetime-field", "sched-command", "sched-command-field", "sched-prompt", "sched-prompt-field", "sched-enabled", "sched-enabled-field", "sched-submit", "history-search-section", "history-search-form", "history-search-input", "history-search-view", "history-search-count", "skill-list", "history-bar",
  "project-name", "project-root", "project-status", "project-overview", "worktree-view", "file-count", "context-compactions",
  "right-tabs", "goal-section", "goal-badge", "goal-view", "code-panes", "code-pane-worktree", "code-pane-gitlog", "git-log-view", "git-log-count",
  "runtime-button", "runtime-modal", "runtime-close", "settings-button", "settings-modal", "settings-close", "storage-backend", "storage-path", "storage-path-field", "storage-dsn", "storage-dsn-field", "storage-test", "storage-save", "storage-status", "inline-suggestions",
  "command-button", "command-modal", "command-close", "command-triggers", "command-search", "command-results",
  "load-history", "interaction-modal", "perm-toggle", "interaction-risk", "interaction-title",
  "new-session-modal", "new-session-close", "new-session-task", "new-session-workspace", "new-session-back", "new-session-workspace-list", "new-session-pick-folder", "new-session-step-1", "new-session-step-2",
  "scheduled-table-modal", "scheduled-table-close", "scheduled-table-open", "scheduled-table-summary", "scheduled-table-view",
  "interaction-question", "interaction-preview", "interaction-options",
  "node-detail-modal", "node-detail-close", "node-detail-title", "node-detail-content", "toast"
].map(id => [id, document.getElementById(id)]));

const conversationView = createConversationView(elements.conversation, {
  copyText: value => navigator.clipboard.writeText(value),
  notify: showToast,
  loadMore: loadOlderHistory,
  loadResultRef: async (ref, offset, limit) => {
    try {
      return await invoke("ToolResultContent", ref, offset, limit);
    } catch (error) {
      showToast(error);
      throw error;
    }
  },
  resultPageLimit: 12000
});
const chatView = createChatView(elements, conversationView);
// 轨迹视图（对话区「轨迹」子页）：Network 风格响应日志。数据从权威
// Snapshot.conversation 派生（buildTrajectory，先分类响应类型再投影轨迹），
// 本地过滤/展开/滚动状态只存前端，不进入 Snapshot。
const trajectoryView = createTrajectoryView(elements.trajectory, {
  copyText: value => navigator.clipboard.writeText(value),
  notify: showToast,
  loadResultRef: async (ref, offset, limit) => {
    try {
      return await invoke("ToolResultContent", ref, offset, limit);
    } catch (error) {
      showToast(error);
      throw error;
    }
  },
  resultPageLimit: 12000,
  onFilterChange: kind => { state.trajectoryFilter = kind; }
});
// 性能追踪钩子：渲染进程可观测指标（DOM/JS heap/渲染耗时）与后端快照
// 载荷对照，10s 轮询；徽标挂在顶部状态区。
const perfHooks = createPerfHooks({
  getStats: async () => {
    try { return await invoke("PerfStats"); } catch { return null; }
  },
  onError: showToast
});
if (elements["perf-badge-host"]) {
  elements["perf-badge-host"].appendChild(perfHooks.badge);
}
perfHooks.start();
window.__seelexPerf = perfHooks;
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
const workTableView = createWorkTableView(elements["work-table-modal-view"]);
const workTreeView = createWorkTreeView(elements["worktree-view"], {
  loadDir: async relPath => invoke("WorkspaceTree", relPath, 1)
});
const gitLogView = createGitLogView(elements["git-log-view"], {
  onCopy: async hash => {
    try {
      await navigator.clipboard.writeText(hash);
    } catch (error) {
      showToast(error);
    }
  }
});
// workTableSeen 是“已读”快照（status|retry_count 签名）；workTableOpen
// 控制弹窗打开期间不显示未读角标。
let workTableSeen = new Map();
let workTableOpen = false;
// worktreeRoot 是已加载文件树的工作区 root；worktreeFileCount 是递归文件
// 统计；lastChatRunning 用于在 chat 结束（文件可能变化）时刷新。
let worktreeRoot = "";
let worktreeFileCount = null;
let lastChatRunning = false;
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
  const started = performance.now();
  renderSessions(snapshot.sessions || [], snapshot.session || {}, snapshot.capabilities || {}, snapshot.session_workspaces || {}, snapshot.workspaces || []);
  renderProject(snapshot);
  renderRuntime(snapshot.runtime || {});
  renderPlugins(snapshot.runtime || {});
  renderAccounts(snapshot.runtime || {});
  chatView.render(snapshot, options.scrollMode);
  renderTrajectory(snapshot);
  refreshPlanDetailData(snapshot.runtime?.plan, snapshot.runtime?.subagent_tree);
  renderWorkTable(snapshot.runtime?.work_table, snapshot.runtime?.work_table_batches);
  renderGoal(snapshot);
  renderScheduledTaskPanel(snapshot.runtime || {});
  renderSkills(snapshot.runtime?.skills || []);
  renderInteraction(snapshot.interaction);
  activeChatSync.observe(snapshot);
  perfHooks.markRender(performance.now() - started);
}

function renderIncremental(snapshot, kind) {
  if (!snapshot) return;
  const started = performance.now();
  activeChatSync.observe(snapshot);
  if (["message.added", "message.delta", "tool.started", "tool.completed"].includes(kind)) {
    chatView.renderConversation(snapshot.conversation || [], snapshot.chat || {}, "auto", snapshot.has_more_history);
    chatView.renderControls(snapshot);
    renderTrajectory(snapshot);
    if (kind !== "message.delta") renderProject(snapshot);
    // 轨迹子页激活时：对话视图隐藏，empty-state / 加载更早按钮一并隐藏。
    if (state.tab !== "conversation") {
      elements["empty-state"].classList.add("hidden");
      elements["history-bar"].classList.add("hidden");
    }
    perfHooks.markRender(performance.now() - started);
    return;
  }
  if (kind === "runtime.changed") {
    renderRuntime(snapshot.runtime || {});
    renderPlugins(snapshot.runtime || {});
    renderAccounts(snapshot.runtime || {});
    refreshPlanDetailData(snapshot.runtime?.plan, snapshot.runtime?.subagent_tree);
    renderWorkTable(snapshot.runtime?.work_table, snapshot.runtime?.work_table_batches);
    renderGoal(snapshot);
    renderScheduledTaskPanel(snapshot.runtime || {});
    renderSkills(snapshot.runtime?.skills || []);
    renderProject(snapshot);
    return;
  }
  if (kind === "worktable.changed") {
    refreshPlanDetailData(snapshot.runtime?.plan, snapshot.runtime?.subagent_tree);
    renderWorkTable(snapshot.runtime?.work_table, snapshot.runtime?.work_table_batches);
    return;
  }
  if (kind === "task.changed") {
    refreshPlanDetailData(snapshot.runtime?.plan, snapshot.runtime?.subagent_tree);
    renderWorkTable(snapshot.runtime?.work_table, snapshot.runtime?.work_table_batches);
    return;
  }
  if (["subagent.changed", "subagent.tool.started", "subagent.tool.completed"].includes(kind)) {
    refreshPlanDetailData(snapshot.runtime?.plan, snapshot.runtime?.subagent_tree);
    if (activeNodeDetailKey) refreshOpenNodeDetail();
    return;
  }
  if (kind === "interaction.opened" || kind === "interaction.closed") renderInteraction(snapshot.interaction);
}

// ── 对话区子页（对话 / 轨迹）──────────────────────────────
// 轨迹与对话共用对话区，通过 workspace 顶部 tab 切换；当前 tab、过滤类型、
// 展开与滚动都是本地 UI 状态，不进入 Snapshot。

// renderTrajectory 从权威 conversation 派生轨迹记录并渲染。
// active=false（轨迹子页未激活）时只缓存数据面，不碰轨迹 DOM。
function renderTrajectory(snapshot, active = state.tab === "trajectory") {
  if (!snapshot) return;
  trajectoryView.render(buildTrajectory(snapshot.conversation || []), state.trajectoryFilter, active);
}

function setConversationTab(tab) {
  if (tab !== "conversation" && tab !== "trajectory") return;
  state.tab = tab;
  elements["conversation-tabs"].querySelectorAll(".conversation-tab").forEach(button => {
    const active = button.dataset.tab === tab;
    button.classList.toggle("is-active", active);
    button.setAttribute("aria-selected", String(active));
  });
  elements.conversation.classList.toggle("hidden", tab !== "conversation");
  elements.trajectory.classList.toggle("hidden", tab !== "trajectory");
  const snapshot = client.current();
  if (tab === "trajectory") {
    // 轨迹子页：对话视图与加载更早入口隐藏；空态文案由轨迹视图自己渲染。
    elements["history-bar"].classList.add("hidden");
    elements["empty-state"].classList.add("hidden");
    renderTrajectory(snapshot);
    return;
  }
  if (!snapshot) return;
  // 切回对话子页：恢复对话视图，empty-state / 加载更早由 chatView 重新判定。
  chatView.renderConversation(snapshot.conversation || [], snapshot.chat || {}, "preserve", Boolean(snapshot.has_more_history));
  chatView.renderControls(snapshot);
}

elements["conversation-tabs"].addEventListener("click", event => {
  const button = event.target.closest(".conversation-tab");
  if (!button || button.dataset.tab === state.tab) return;
  setConversationTab(button.dataset.tab);
});

// ── 右侧栏子页（状态 / 工作台 / 代码）───────────────────────
// 子页切换是纯 UI 状态（localStorage 记忆）；业务事实仍来自 Snapshot/Event。
const RIGHT_TAB_KEY = "seelex.right.tab";
const RIGHT_PANE_ORDER_KEY = "seelex.right.codePanes";

function storedRightTab() {
  const value = localStorage.getItem(RIGHT_TAB_KEY);
  return value === "status" || value === "workbench" || value === "code" ? value : "status";
}

function setRightTab(tab) {
  if (tab !== "status" && tab !== "workbench" && tab !== "code") return;
  localStorage.setItem(RIGHT_TAB_KEY, tab);
  elements["right-tabs"].querySelectorAll(".right-tab").forEach(button => {
    const active = button.dataset.rightTab === tab;
    button.classList.toggle("is-active", active);
    button.setAttribute("aria-selected", String(active));
  });
  // 三个 tabpanel 是 #right-tabs 的兄弟节点（.right-panel 的直接子节点），
  // 不能从 nav 内查询；从父容器作用域查询才能正确切换 hidden。
  elements["right-tabs"].parentElement.querySelectorAll("[data-right-panel]").forEach(panel => {
    panel.classList.toggle("hidden", panel.dataset.rightPanel !== tab);
  });
  if (tab === "code") refreshGitLogIfStale();
}

elements["right-tabs"].addEventListener("click", event => {
  const button = event.target.closest(".right-tab");
  if (!button || button.dataset.rightTab === state.rightTab) return;
  state.rightTab = button.dataset.rightTab;
  setRightTab(state.rightTab);
});

// ── 子页3：工作树 / 提交记录 面板顺序（拖拽调换，localStorage 记忆）────
const CODE_PANES = ["worktree", "gitlog"];
// gitLogRoot / gitLogLoadedAt 记录 git log 数据面（工作区切换/chat 结束
// 后重新拉取；子页未激活时缓存，激活时按需刷新）。
let gitLogRoot = "";
let gitLogLoaded = false;

function storedPaneOrder() {
  const value = localStorage.getItem(RIGHT_PANE_ORDER_KEY);
  if (Array.isArray(value)) {
    const parsed = value.filter(pane => CODE_PANES.includes(pane));
    if (parsed.length === CODE_PANES.length) return parsed;
  }
  return CODE_PANES;
}

function persistPaneOrder() {
  const order = [];
  elements["code-panes"].querySelectorAll("[data-pane]").forEach(section => {
    order.push(section.dataset.pane);
  });
  localStorage.setItem(RIGHT_PANE_ORDER_KEY, JSON.stringify(order));
}

function applyPaneOrder(order) {
  const panes = elements["code-panes"];
  if (!panes) return;
  order.forEach(pane => {
    const section = panes.querySelector(`[data-pane="${pane}"]`);
    if (section) panes.appendChild(section);
  });
}

function initCodePanes() {
  applyPaneOrder(storedPaneOrder());
  const handles = elements["code-panes"]?.querySelectorAll("[data-pane]");
  handles?.forEach(section => {
    section.setAttribute("draggable", "true");
    section.addEventListener("dragstart", event => {
      event.dataTransfer.effectAllowed = "move";
      event.dataTransfer.setData("text/plain", section.dataset.pane);
      section.classList.add("is-dragging");
    });
    section.addEventListener("dragend", () => {
      section.classList.remove("is-dragging");
      panes().forEach(other => other.classList.remove("is-drag-over"));
    });
    section.addEventListener("dragover", event => {
      event.preventDefault();
      event.dataTransfer.dropEffect = "move";
      panes().forEach(other => other.classList.toggle("is-drag-over", other === section));
    });
    section.addEventListener("drop", event => {
      event.preventDefault();
      const fromPane = event.dataTransfer.getData("text/plain");
      const fromSection = panes().find(other => other.dataset.pane === fromPane);
      if (!fromSection || fromSection === section) return;
      const panesList = panes();
      const fromIndex = panesList.indexOf(fromSection);
      const toIndex = panesList.indexOf(section);
      if (fromIndex < 0 || toIndex < 0) return;
      if (fromIndex < toIndex) {
        section.after(fromSection);
      } else {
        section.before(fromSection);
      }
      panes().forEach(other => other.classList.remove("is-drag-over"));
      persistPaneOrder();
    });
  });
}

function panes() {
  return Array.from(elements["code-panes"]?.querySelectorAll("[data-pane]") || []);
}

initCodePanes();

window.addEventListener("beforeunload", () => activeChatSync.stop());

function renderProject(snapshot) {
  const workspace = snapshot.current_workspace || null;
  const runtime = snapshot.runtime || {};
  const task = snapshot.task || null;
  const running = Boolean(snapshot.chat?.running);
  const compactions = task?.context_compactions || [];
  elements["project-name"].textContent = workspace?.name || "No project selected";
  elements["project-root"].textContent = workspace?.root_path || "";
  renderProjectStatus(snapshot, running);
  elements["project-overview"].textContent = workspace
    ? (running
      ? `Current task is running with ${runtime.plugin || "default"} capabilities in this project scope.`
      : `This session can read and write only within ${workspace.name}.`)
    : "Select a project to define this session's read and write scope.";
  elements["context-compactions"].innerHTML = renderContextCompactions(compactions);
  elements["context-compactions"].classList.toggle("hidden", compactions.length === 0);
  refreshWorkTree(snapshot, running);
}

function renderProjectStatus(snapshot, running) {
  elements["project-status"].innerHTML = [
    ["状态", running ? "Agent 执行中" : "Ready"],
    ["会话", snapshot.session?.draft ? "待发送" : shortSessionID(snapshot.session?.id || "—")],
    ["消息", String(snapshot.conversation?.length || 0)],
    ["任务", snapshot.task ? snapshot.task.status : "idle"],
    ["文件数", fileCountLabel()]
  ].map(([label, value]) => `<div class="status-item"><span>${escapeHtml(label)}</span><strong title="${escapeHtml(value)}">${escapeHtml(value)}</strong></div>`).join("");
}

function fileCountLabel() {
  if (!worktreeRoot) return "—";
  if (worktreeFileCount == null) return "…";
  return String(worktreeFileCount.files ?? 0);
}

// refreshWorkTree 惰性加载工作树：绑定工作区后拉一次文件统计与根目录列表；
// chat 结束（文件可能变化）时刷新；重复 render 不重复拉取。
async function refreshWorkTree(snapshot, running) {
  const view = elements["worktree-view"];
  const rootPath = snapshot.current_workspace?.root_path || "";
  if (!rootPath) {
    worktreeRoot = "";
    worktreeFileCount = null;
    view.classList.add("muted");
    view.textContent = "绑定工作区后显示项目文件树";
    elements["file-count"].textContent = "0";
    resetGitLog(snapshot);
    return;
  }
  const chatFinished = lastChatRunning && !running;
  lastChatRunning = running;
  if (rootPath === worktreeRoot && !chatFinished) return;
  worktreeRoot = rootPath;
  worktreeFileCount = null;
  workTreeView.reset();
  elements["file-count"].textContent = "…";
  renderProjectStatus(snapshot, running);
  try {
    const count = await invoke("WorkspaceFileCount");
    worktreeFileCount = count;
    elements["file-count"].textContent = String(count.files ?? 0);
    renderProjectStatus(snapshot, running);
    const listing = await invoke("WorkspaceTree", "", 1);
    workTreeView.renderRoot(listing?.entries || []);
  } catch (error) {
    view.classList.add("muted");
    view.textContent = "工作树暂不可用";
    showToast(error);
  }
  refreshGitLog(snapshot, chatFinished);
}

// ── 提交记录树（工作台「代码」子页）────────────────────────
// refreshGitLog 惰性加载提交记录：绑定工作区后拉一次；chat 结束（可能产生
// 新提交）或工作区切换时刷新；子页未激活时数据面缓存，激活时按需刷新。
async function refreshGitLog(snapshot, force = false) {
  const view = elements["git-log-view"];
  const rootPath = snapshot.current_workspace?.root_path || "";
  if (!rootPath) {
    resetGitLog(snapshot);
    return;
  }
  if (rootPath === gitLogRoot && gitLogLoaded && !force) return;
  gitLogRoot = rootPath;
  gitLogLoaded = false;
  elements["git-log-count"].textContent = "…";
  try {
    const result = await invoke("WorkspaceGitLog", 20);
    gitLogView.renderRoot(result);
    gitLogLoaded = true;
    elements["git-log-count"].textContent = String(result.commits?.length ?? 0);
  } catch (error) {
    view.classList.add("muted");
    view.textContent = "提交记录暂不可用";
    showToast(error);
  }
}

function resetGitLog(snapshot) {
  gitLogRoot = "";
  gitLogLoaded = false;
  gitLogView.reset();
  elements["git-log-count"].textContent = "0";
}

// refreshGitLogIfStale 子页3激活时按需刷新（数据面未加载或已失效）。
function refreshGitLogIfStale() {
  const snapshot = client.current();
  const rootPath = snapshot?.current_workspace?.root_path || "";
  if (rootPath === gitLogRoot && gitLogLoaded) return;
  refreshGitLog(snapshot, true);
}

function renderSessions(sessions, current, capabilities, sessionWorkspaces, workspaces) {
  lastSessionsRender = { sessions, current, capabilities, sessionWorkspaces, workspaces };
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
    ? renderSessionGroups(items, currentID, sessionWorkspaces, workspaceNames)
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
  elements["session-list"].querySelectorAll(".session-fork").forEach(button => {
    button.addEventListener("click", async event => {
      event.stopPropagation();
      const sessionID = button.dataset.fork;
      if (!sessionID) return;
      if (!confirm(`从会话 ${shortSessionID(sessionID)} 分支出新会话？`)) return;
      elements["composer-status"].textContent = "正在分支出新会话…";
      try {
        const childID = await invoke("ForkSessionLatest", sessionID);
        elements["composer-status"].textContent = `已分支出新会话 ${shortSessionID(childID)}`;
        await refresh({ scroll: "bottom" });
      } catch (error) {
        elements["composer-status"].textContent = `分支失败：${error?.message || String(error)}`;
        showToast(error);
      } finally {
        elements["composer-status"].textContent = "";
        const latest = client.current() || { sessions, session: current, capabilities, session_workspaces: sessionWorkspaces, workspaces };
        renderSessions(latest.sessions || sessions, latest.session || current, latest.capabilities || capabilities, latest.session_workspaces || sessionWorkspaces, latest.workspaces || workspaces);
      }
    });
  });
  elements["session-list"].querySelectorAll("[data-collapse-group]").forEach(button => {
    button.addEventListener("click", event => {
      event.stopPropagation();
      toggleWorkspaceGroup(button.dataset.collapseGroup);
    });
  });
  elements["session-list"].querySelectorAll("[data-pin-session]").forEach(button => {
    button.addEventListener("click", event => {
      event.stopPropagation();
      togglePinned(button.dataset.pinSession);
      rerenderSessions();
    });
  });
  elements["session-list"].querySelectorAll("[data-workspace-new-session]").forEach(button => {
    button.addEventListener("click", event => {
      event.stopPropagation();
      const workspaceID = button.dataset.workspaceNewSession;
      if (workspaceID) bindWorkspaceAndStart(workspaceID);
    });
  });
}

const UNBOUND_WORKSPACE = "__unbound__";

// 折叠状态：key → 是否折叠（工作区消失后旧 key 自然失效）；持久化到
// localStorage，重渲染后仍然保持。
const collapsedWorkspaceGroups = new Set(
  (JSON.parse(storageGet("seelex.collapsed-workspace-groups") || "[]") || [])
    .filter(key => typeof key === "string")
);

let lastSessionsRender = null;

// renderSessionGroups 把会话按工作区（session_workspaces 投影）分组渲染；
// 未绑定工作区或工作区已消失的会话收进「未关联会话」组，置底展示。
function renderSessionGroups(items, currentID, sessionWorkspaces, workspaceNames) {
  const groups = new Map();
  for (const session of items) {
    const workspaceID = sessionWorkspaces[session.id];
    const key = workspaceID && workspaceNames.has(workspaceID) ? workspaceID : UNBOUND_WORKSPACE;
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push(session);
  }
  const keys = [...groups.keys()].sort((a, b) => {
    if (a === UNBOUND_WORKSPACE) return 1;
    if (b === UNBOUND_WORKSPACE) return -1;
    return String(workspaceNames.get(a)).localeCompare(String(workspaceNames.get(b)), "zh-Hans-CN");
  });
  return keys.map(key => {
    const label = key === UNBOUND_WORKSPACE ? "未关联会话" : workspaceNames.get(key) || key;
    const sessions = groups.get(key).slice().sort((a, b) => Number(isPinned(b.id)) - Number(isPinned(a.id)));
    const rows = sessions.map(session => sessionRow(session, currentID)).join("");
    const collapsed = collapsedWorkspaceGroups.has(key);
    const addButton = key === UNBOUND_WORKSPACE
      ? ""
      : `<button type="button" class="session-group-add" data-workspace-new-session="${escapeHtml(key)}" title="在该工作区新建会话" aria-label="在该工作区新建会话">${icon("plus", 12)}</button>`;
    return `<div class="session-group${collapsed ? " is-collapsed" : ""}" data-workspace-group="${escapeHtml(key)}">
      <div class="session-group-head-row">
        <button type="button" class="session-group-head" data-collapse-group="${escapeHtml(key)}" aria-expanded="${!collapsed}">
          <span class="session-group-chevron" aria-hidden="true"></span>
          <span class="session-group-name">${escapeHtml(label)}</span>
          <span class="badge">${groups.get(key).length}</span>
        </button>
        ${addButton}
      </div>
      <div class="session-group-body">${rows}</div>
    </div>`;
  }).join("");
}

function toggleWorkspaceGroup(key) {
  if (collapsedWorkspaceGroups.has(key)) collapsedWorkspaceGroups.delete(key);
  else collapsedWorkspaceGroups.add(key);
  storageSet("seelex.collapsed-workspace-groups", JSON.stringify([...collapsedWorkspaceGroups]));
  rerenderSessions();
}

function rerenderSessions() {
  if (!lastSessionsRender) return;
  renderSessions(
    lastSessionsRender.sessions, lastSessionsRender.current, lastSessionsRender.capabilities,
    lastSessionsRender.sessionWorkspaces, lastSessionsRender.workspaces
  );
}

function sessionRow(session, currentID) {
  const active = session.id === currentID;
  const resuming = session.id === state.resumingSessionID;
  const pinned = isPinned(session.id);
  const updated = session.updated_at
    ? new Date(session.updated_at).toLocaleString([], { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" })
    : "当前会话";
  const detail = session.token_count ? `${updated} · ${session.token_count} tokens` : updated;
  const display = resuming ? "恢复中…" : (session.name || shortSessionID(session.id));
  const truncated = truncateTitle(display, 5);
  return `<div class="session-row${pinned ? " is-pinned" : ""}">
    <button class="stack-button session-button ${active ? "active" : ""}" data-session="${escapeHtml(session.id)}" title="${escapeHtml(session.name || "")}" ${resuming ? "disabled" : ""}>
      <span class="entry-name">${icon("message", 13)} ${escapeHtml(truncated)}</span><small>${escapeHtml(detail)}</small>
    </button>
    <button class="session-pin${pinned ? " is-on" : ""}" data-pin-session="${escapeHtml(session.id)}" title="${pinned ? "取消置顶" : "置顶会话"}" aria-label="置顶会话">📌</button>
    <button class="session-fork" data-fork="${escapeHtml(session.id)}" title="分支出新会话" aria-label="分支出新会话">⑂</button>
    <button class="session-del" data-session="${escapeHtml(session.id)}" title="删除会话" aria-label="删除会话">✕</button>
  </div>`;
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
  elements["perm-toggle"].textContent = fullAccess ? "全权 ✓" : "全权";

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
  elements["account-list"].innerHTML = accounts.length
    ? accounts.map(account => `
      <button class="stack-button ${runtime.account === account.name ? "active" : ""}" data-account="${escapeHtml(account.name)}" ${account.disabled ? "disabled" : ""}>
        ${escapeHtml(account.name)}<small>${escapeHtml(`${account.provider || ""} ${account.model || ""}`.trim())}</small>
      </button>`).join("")
    : '<span class="muted list-empty">暂无账户</span>';
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

// ── 「目标」面板（工作台子页）──────────────────────────────
// 数据源：runtime.goal_skill_active / runtime.active_skills（任务级 skill
// 激活权威投影，backend 锁内快照）+ snapshot.task（当前任务状态/摘要）+
// 最近用户输入（目标文本，本地派生展示）。无内容时隐藏整个 section。
function renderGoal(snapshot) {
  const runtime = snapshot.runtime || {};
  const task = snapshot.task || null;
  const goalActive = Boolean(runtime.goal_skill_active);
  const activeSkills = Array.isArray(runtime.active_skills) ? runtime.active_skills : [];
  const goalSection = elements["goal-section"];
  if (!goalSection) return;
  const goalText = latestUserInput(snapshot);
  const hasContent = goalActive || activeSkills.length > 0 || task || goalText;
  goalSection.classList.toggle("hidden", !hasContent);
  const badge = elements["goal-badge"];
  if (badge) {
    badge.classList.toggle("hidden", !goalActive);
    badge.textContent = "GOAL";
    badge.title = goalActive ? "GOAL 方法论已激活" : "";
  }
  const view = elements["goal-view"];
  if (!hasContent) {
    view.classList.add("muted");
    view.innerHTML = "当前无目标任务";
    return;
  }
  view.classList.remove("muted");
  const goalLine = goalText
    ? `<div class="goal-text" title="${escapeHtml(goalText)}">${escapeHtml(truncateGoalText(goalText))}</div>`
    : "";
  const taskLine = task
    ? `<div class="goal-task"><span class="goal-task-status is-${escapeHtml(task.status || "idle")}">${escapeHtml(task.status || "idle")}</span><span class="goal-task-summary" title="${escapeHtml(task.summary || "")}">${escapeHtml(task.summary || "任务进行中")}</span></div>`
    : "";
  const chips = activeSkills.length
    ? `<div class="goal-skills">${activeSkills.map(skill => `<span class="chip">#${escapeHtml(skill)}</span>`).join("")}</div>`
    : "";
  view.innerHTML = `${goalLine}${taskLine}${chips}`;
}

// latestUserInput 返回会话最近一条非空用户消息（目标文本数据源）。
function latestUserInput(snapshot) {
  const conversation = snapshot.conversation || [];
  for (let index = conversation.length - 1; index >= 0; index -= 1) {
    const message = conversation[index];
    if (message?.role === "user" && message.content && String(message.content).trim()) {
      return String(message.content).trim();
    }
  }
  return "";
}

function truncateGoalText(text, max = 120) {
  return text.length <= max ? text : `${text.slice(0, max)}…`;
}

// lastPlanDsl 保存最近一次渲染的 Plan DSL（节点详情弹窗的数据源；
// 权威 JSON 来自 snapshot.runtime.plan，弹窗只读展示）。
// lastSubagentTree 保存子代理树投影（snapshot.runtime.subagent_tree；
// fork 节点不在 Plan 快照里时详情弹窗的兜底数据源）。
let lastPlanDsl = null;
let lastSubagentTree = [];

// refreshPlanDetailData 只刷新详情弹窗的数据面（Plan DSL + 子代理树投影），
// 不做任何面板 DOM 渲染：右侧工作台统一由工作表格（renderWorkTable）呈现，
// 详情弹窗数据源保持 Plan/子代理树权威投影（planToDSL 只算不改 DOM）。
function refreshPlanDetailData(plan, subagentTree = null) {
  lastPlanDsl = planToDSL(plan);
  lastSubagentTree = Array.isArray(subagentTree) ? subagentTree : [];
}

// renderWorkTable 渲染工作表格详情（弹窗内 keyed reconciliation）并同步
// 右侧按钮摘要与未读角标（未读 = 新增或状态/retry 变化的条目）。
function renderWorkTable(rows, batches) {
  workTableView.render(rows, batches);
  const normalized = workTableView.current();
  elements["work-count"].textContent = String(normalized.length);
  elements["work-table-summary"].textContent = `${normalized.length} 项任务`;
  elements["work-section"]?.classList.toggle("hidden", normalized.length === 0);
  const unread = workTableOpen ? 0 : countUnread(normalized, workTableSeen);
  elements["work-unread"]?.classList.toggle("hidden", unread === 0);
  if (elements["work-unread"]) elements["work-unread"].textContent = `${unread} 未读`;
}

// openWorkTable 打开完整表格详情：记录当前已读快照并清角标。
function openWorkTable() {
  workTableOpen = true;
  workTableSeen = workTableSignatures(workTableView.current());
  elements["work-unread"]?.classList.add("hidden");
  setModal("work-table-modal", true);
}

function closeWorkTable() {
  workTableOpen = false;
  setModal("work-table-modal", false);
}

// 工作表格行内交互（todo 状态更新、条目展开、筛选、打点展开）走委托。
workTableView.bind({
  onStatus: async (id, status) => {
    try {
      await invoke("UpdateWorkItemStatus", id, status);
      await refresh({ scroll: false });
    } catch (error) {
      showToast(error);
    }
  },
  onDetail: id => openNodeDetail(id)
});

elements["work-table-open"]?.addEventListener("click", openWorkTable);
elements["work-table-modal-close"]?.addEventListener("click", closeWorkTable);

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

// renderScheduledTaskPanel 渲染定时周期任务面板：数据来自 snapshot.runtime
// （权威投影，调度器状态变化经 runtime.changed 增量带到）；任务只读展示，
// 新建按钮常驻（命令白名单来自 runtime.scheduled_commands）。
function renderScheduledTaskPanel(runtime) {
  const tasks = Array.isArray(runtime.scheduled_tasks) ? runtime.scheduled_tasks : [];
  const commands = Array.isArray(runtime.scheduled_commands) ? runtime.scheduled_commands : [];
  elements["scheduled-task-count"].textContent = String(tasks.length);
  elements["scheduled-table-summary"].textContent = `${tasks.length} 项任务`;
  elements["scheduled-task-view"].innerHTML = renderScheduledTasks(tasks, commands);
  elements["scheduled-table-view"].innerHTML = renderScheduledTasksTable(tasks, commands);
}

// openScheduledTable / closeScheduledTable：定时任务 Excel 表格弹窗
// （右栏只留入口按钮，表格放弹窗；新建复用表单弹窗）。
function openScheduledTable() {
  setModal("scheduled-table-modal", true);
}

function closeScheduledTable() {
  setModal("scheduled-table-modal", false);
}

elements["scheduled-table-open"]?.addEventListener("click", openScheduledTable);
elements["scheduled-table-close"]?.addEventListener("click", closeScheduledTable);
elements["scheduled-table-modal"]?.addEventListener("click", event => {
  if (event.target === elements["scheduled-table-modal"]) closeScheduledTable();
});
document.addEventListener("keydown", event => {
  if (event.key === "Escape") closeScheduledTable();
});

// 定时任务表格内取消按钮（事件委托挂表格容器；ID 是操作键）。
elements["scheduled-table-view"]?.addEventListener("click", async event => {
  const button = event.target.closest?.("[data-sched-cancel]");
  if (!button?.dataset.schedCancel) return;
  if (!confirm("确认取消该定时任务？")) return;
  try {
    await invoke("CancelScheduledTask", button.dataset.schedCancel);
    await refresh({ scroll: false });
  } catch (error) {
    showToast(error);
  }
});

// initModalResize 通用弹窗拉伸：右下角手柄 pointer 拖动调整宽高
// （覆盖工作表格/定时任务表格等 data-resizable 弹窗，仅尺寸调整）。
function initModalResize() {
  document.querySelectorAll(".modal-card[data-resizable]").forEach(card => {
    const handle = card.querySelector(".modal-resize-handle");
    if (!handle || handle.dataset.resizableBound) return;
    handle.dataset.resizableBound = "1";
    let tracking = false;
    let startX = 0;
    let startY = 0;
    let startW = 0;
    let startH = 0;
    handle.addEventListener("pointerdown", event => {
      event.preventDefault();
      tracking = true;
      const rect = card.getBoundingClientRect();
      startX = event.clientX;
      startY = event.clientY;
      startW = rect.width;
      startH = rect.height;
      handle.setPointerCapture?.(event.pointerId);
    });
    handle.addEventListener("pointermove", event => {
      if (!tracking) return;
      const width = Math.max(360, startW + (event.clientX - startX));
      const height = Math.max(240, startH + (event.clientY - startY));
      card.style.width = `${Math.min(width, window.innerWidth - 40)}px`;
      card.style.height = `${Math.min(height, window.innerHeight - 40)}px`;
    });
    const stop = () => { tracking = false; };
    handle.addEventListener("pointerup", stop);
    handle.addEventListener("pointercancel", stop);
  });
}
initModalResize();

// openNodeDetail 渲染并打开节点详情弹窗（子代理详情页）：
// 会话记录（invoke SubagentSessionDetail，运行中 2s 轮询）+ 事件时间线 +
// 状态/耗时/输出。
let nodeDetailPollTimer = null;
let activeNodeDetailKey = "";
let activeNodeDetailID = "";
let nodeDetailGeneration = 0;
let subagentLiveBound = false;
let nodeLiveBuffer = [];

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
  // node 第一视角实时流：订阅 seelex:subagent_live（阶段/工具事件到达即显示）。
  nodeLiveBuffer = [];
  if (window.runtime && !subagentLiveBound) {
    subagentLiveBound = true;
    window.runtime.EventsOn("seelex:subagent_live", handleSubagentLive);
  }
  invoke("SubagentDetailStreamStart", node.id)
    .then(history => {
      if (node.id !== activeNodeDetailID) return;
      // 历史回放：subagent start 以来的完整事件流 + 打开瞬间已到的实时事件。
      nodeLiveBuffer = (history || []).concat(nodeLiveBuffer);
      if (nodeLiveBuffer.length > 500) nodeLiveBuffer = nodeLiveBuffer.slice(-500);
      renderLiveFeed();
    })
    .catch(() => {});
  renderLiveFeed();
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
  renderLiveFeed();
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
    // 兜底超时：详情接口异常/挂起时不再让“加载会话记录…”永久占位，
    // 超时按空详情渲染（确定性节点/会话未落盘的既有空态文案）。
    const timeout = new Promise((_, reject) => {
      window.setTimeout(() => reject(new Error("subagent detail timeout")), 10000);
    });
    detail = await Promise.race([invoke("SubagentSessionDetail", nodeID), timeout]);
  } catch { /* 节点无会话记录或非 agent 节点 → 面板保持占位 */ }
  if (generation !== nodeDetailGeneration || nodeID !== activeNodeDetailID || !document.querySelector("[data-node-detail]")) return;
  setNodeDetailConversation(detail || null);
  if (detail?.running) {
    nodeDetailPollTimer = setTimeout(() => refreshNodeDetail(nodeID, generation), 2000);
  }
}

function closeNodeDetail() {
  const nodeID = activeNodeDetailID;
  nodeDetailGeneration += 1;
  if (nodeDetailPollTimer) {
    clearTimeout(nodeDetailPollTimer);
    nodeDetailPollTimer = null;
  }
  activeNodeDetailKey = "";
  activeNodeDetailID = "";
  nodeLiveBuffer = [];
  if (nodeID) invoke("SubagentDetailStreamStop", nodeID).catch(() => {});
  setModal("node-detail-modal", false);
}

// ── node 第一视角实时流（即时输出：阶段/工具事件到达即渲染）──

function renderLiveFeed() {
  const feed = document.querySelector("[data-node-detail] [data-node-live-feed]");
  if (!feed) return;
  feed.innerHTML = nodeLiveBuffer.length === 0
    ? '<div class="node-timeline-empty">等待实时事件（打开即订阅；阶段/工具事件到达即显示）…</div>'
    : nodeLiveBuffer.map(liveRowHTML).join("");
  feed.scrollTop = feed.scrollHeight;
}

function handleSubagentLive(event) {
  if (!event || event.node_id !== activeNodeDetailID) return;
  nodeLiveBuffer.push(event);
  if (nodeLiveBuffer.length > 500) nodeLiveBuffer.shift();
  renderLiveFeed();
}

function liveRowHTML(event) {
  const at = liveTime(event.at);
  if (event.kind === "tool") {
    const tool = event.tool || {};
    const result = tool.result ? ` <code>${escapeHtml(livePreview(tool.result))}</code>` : "";
    return `<div class="node-live-row"><span class="node-live-time">${escapeHtml(at)}</span><span class="node-live-kind is-tool">工具</span><strong>${escapeHtml(tool.name || "")}</strong><span class="node-live-status is-${escapeHtml(tool.status || "")}">${escapeHtml(tool.status || "")}</span>${result}</div>`;
  }
  const stage = event.stage || {};
  const turn = stage.turn ? ` <em>#${stage.turn}</em>` : "";
  const preview = stage.preview ? ` <code>${escapeHtml(livePreview(stage.preview))}</code>` : "";
  return `<div class="node-live-row"><span class="node-live-time">${escapeHtml(at)}</span><span class="node-live-kind is-stage">阶段</span><strong>${escapeHtml(stage.stage || "")}</strong>${turn}${preview}</div>`;
}

function liveTime(value) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  return date.toTimeString().slice(0, 12) + "." + String(date.getMilliseconds()).padStart(3, "0");
}

function livePreview(value) {
  const compact = String(value).replace(/\s+/g, " ").trim();
  return compact.length > 140 ? compact.slice(0, 140) + "…" : compact;
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
  elements.prompt.focus();
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
  // 中文输入法确认候选词时也会触发 Enter（isComposing=true），此时不能发送。
  if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
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

elements["new-session"].addEventListener("click", openNewSessionModal);

// ── 新建会话弹窗（两步：任务会话 / 工作区会话）──────────────────────────
function openNewSessionModal() {
  showNewSessionStep(1);
  setModal("new-session-modal", true);
}

function closeNewSessionModal() {
  setModal("new-session-modal", false);
}

function showNewSessionStep(step) {
  elements["new-session-step-1"].classList.toggle("hidden", step !== 1);
  elements["new-session-step-2"].classList.toggle("hidden", step !== 2);
}

async function beginNewSession() {
  const previous = client.current();
  const previousSessions = Array.isArray(previous?.sessions) ? previous.sessions : null;
  try {
    await invoke("BeginNewSession");
    await refresh({ scroll: "bottom" });
    // 防御：会话目录由后端异步 worker 刷新，新建会话后首轮快照理论上仍携带旧列表；
    // 若竞态导致返回空 sessions，则保留上一次可见列表并稍后重拉权威目录收敛，
    // 避免左侧栏会话"全部消失"的假象。
    const latest = client.current();
    if (previousSessions && previousSessions.length > 0 && latest &&
        (!Array.isArray(latest.sessions) || latest.sessions.length === 0)) {
      latest.sessions = previousSessions;
      latest.session_workspaces = previous?.session_workspaces || {};
      latest.workspaces = previous?.workspaces || [];
      render(latest, { scroll: "bottom" });
      window.setTimeout(() => refresh({ scroll: false }), 250);
    }
  }
  catch (error) { showToast(error); }
}

async function bindWorkspaceAndStart(workspaceID) {
  try {
    closeNewSessionModal();
    // 新会话默认未关联工作区：BeginNewSession 会清空上一个会话继承的项目
    // 绑定（任务会话真正未关联）。因此先进入草稿，再在草稿上显式绑定
    // 工作区——「工作区会话」仍带项目上下文，首次提交时物化到该项目。
    await beginNewSession();
    await invoke("BindWorkspace", workspaceID);
  } catch (error) { showToast(error); }
}

function normalizePath(value) {
  return String(value || "").replace(/\\/g, "/").replace(/\/+$/, "").toLowerCase();
}

function renderNewSessionWorkspaces() {
  const snapshot = client.current() || {};
  const workspaces = Array.isArray(snapshot.workspaces) ? snapshot.workspaces : [];
  const current = snapshot.current_workspace || null;
  const rows = workspaces.map(workspace => `
    <button type="button" class="stack-button new-session-ws${current && current.id === workspace.id ? " active" : ""}" data-new-session-ws="${escapeHtml(workspace.id)}">
      <span class="entry-name">${icon("folder", 13)} ${escapeHtml(workspace.name)}</span>
      <small>${escapeHtml(workspace.root_path || "")}</small>
    </button>`).join("");
  const unbind = current
    ? `<button type="button" class="text-button new-session-unbind" data-new-session-unbind="1">解除当前绑定：${escapeHtml(current.name)}</button>`
    : "";
  elements["new-session-workspace-list"].innerHTML =
    (rows || '<span class="muted list-empty">暂无工作区</span>') + unbind;
}

elements["new-session-task"].addEventListener("click", async () => {
  closeNewSessionModal();
  await beginNewSession();
});

elements["new-session-workspace"].addEventListener("click", () => {
  showNewSessionStep(2);
  renderNewSessionWorkspaces();
});

elements["new-session-back"].addEventListener("click", () => showNewSessionStep(1));

elements["new-session-close"].addEventListener("click", closeNewSessionModal);

elements["new-session-workspace-list"].addEventListener("click", async event => {
  const workspaceButton = event.target.closest?.("[data-new-session-ws]");
  if (workspaceButton) {
    await bindWorkspaceAndStart(workspaceButton.dataset.newSessionWs);
    return;
  }
  if (event.target.closest?.("[data-new-session-unbind]")) {
    if (!confirm("确认解除当前项目绑定？")) return;
    try {
      await invoke("UnbindWorkspace");
      await refresh({ scroll: false });
      renderNewSessionWorkspaces();
    } catch (error) { showToast(error); }
  }
});

elements["new-session-pick-folder"].addEventListener("click", async () => {
  try {
    const dir = await invoke("PickDirectory");
    if (!dir) return;
    const name = dir.split(/[\\/]/).pop() || "workspace";
    await invoke("CreateWorkspace", name, dir, "");
    await refresh({ scroll: false });
    const latest = client.current() || {};
    const workspaces = Array.isArray(latest.workspaces) ? latest.workspaces : [];
    const created = workspaces.find(workspace => normalizePath(workspace.root_path) === normalizePath(dir));
    if (!created?.id) {
      showToast("创建工作区失败：未找到新工作区");
      return;
    }
    await bindWorkspaceAndStart(created.id);
  } catch (error) { showToast(error); }
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
  elements["sched-mode"].value = "period";
  elements["sched-datetime"].value = "";
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
  const atMode = elements["sched-mode"].value === "at";
  elements["sched-period-field"].classList.toggle("hidden", atMode);
  elements["sched-datetime-field"].classList.toggle("hidden", !atMode);
  elements["sched-enabled-field"].classList.toggle("hidden", atMode);
}

// submitScheduledTask 组装任务入参并提交 Bridge ScheduleTask
// （周期模式：周期单位 → 等价秒 → Go time.Duration 纳秒，month 由后端按
// 日历月推进；定时模式：runAt 传 RFC3339，后端创建即启用、执行后自动停用；
// sessionId 留空 = 绑定当前主会话）。
async function submitScheduledTask() {
  const name = elements["sched-name"].value.trim();
  const kind = elements["sched-kind"].value;
  const mode = elements["sched-mode"].value;
  const periodValue = Number(elements["sched-period-value"].value);
  const periodUnit = elements["sched-period-unit"].value;
  if (!name) {
    showToast("请填写任务名称");
    return;
  }
  let runAt = "";
  if (mode === "at") {
    const runAtValue = elements["sched-datetime"].value;
    if (!runAtValue) {
      showToast("请选择定时执行时间");
      return;
    }
    const parsed = new Date(runAtValue);
    if (Number.isNaN(parsed.getTime())) {
      showToast("定时时间格式无效");
      return;
    }
    if (parsed.getTime() <= Date.now()) {
      showToast("定时时间必须晚于当前时间");
      return;
    }
    runAt = parsed.toISOString();
  } else if (!Number.isInteger(periodValue) || periodValue < 1) {
    showToast("周期数值至少为 1");
    return;
  }
  if (kind === "command" && !elements["sched-command"].value) {
    showToast("当前没有可用的白名单命令");
    return;
  }
  const spec = {
    name,
    kind,
    interval: mode === "at" ? 0 : periodToSeconds(periodUnit, periodValue) * 1e9,
    periodUnit: mode === "at" ? "" : periodUnit,
    periodValue: mode === "at" ? 0 : periodValue,
    runAt,
    command: kind === "command" ? elements["sched-command"].value : "",
    prompt: kind === "prompt" ? elements["sched-prompt"].value.trim() : "",
    sessionId: "",
    enabled: mode === "at" ? true : elements["sched-enabled"].checked
  };
  try {
    await invoke("ScheduleTask", spec);
    closeScheduledTaskDialog();
    await refresh({ scroll: false });
  } catch (error) {
    showToast(error);
  }
}

// periodToSeconds 周期单位 → 等价秒（month 用 30 天名义值，仅用于 interval
// 字段与后端最小周期校验；真实排期由调度器按日历月推进）。
function periodToSeconds(unit, value) {
  switch (unit) {
    case "day": return value * 86400;
    case "week": return value * 604800;
    case "month": return value * 2592000;
    case "hour":
    default: return value * 3600;
  }
}

elements["new-scheduled-task"].addEventListener("click", openScheduledTaskDialog);
elements["scheduled-task-close"].addEventListener("click", closeScheduledTaskDialog);
elements["sched-kind"].addEventListener("change", syncScheduledTaskFields);
elements["sched-mode"].addEventListener("change", syncScheduledTaskFields);
elements["sched-submit"].addEventListener("click", submitScheduledTask);

// 取消按钮事件委托（任务列表渲染全量刷新，事件挂容器层；ID 是操作键）。
elements["scheduled-task-view"].addEventListener("click", async event => {
  const button = event.target.closest?.("[data-sched-cancel]");
  if (!button?.dataset.schedCancel) return;
  if (!confirm("确认取消该定时任务？")) return;
  try {
    await invoke("CancelScheduledTask", button.dataset.schedCancel);
    await refresh({ scroll: false });
  } catch (error) {
    showToast(error);
  }
});

for (const [modalID, close] of [["runtime-modal", closeRuntime], ["command-modal", closeCommandPalette], ["settings-modal", closeSettings], ["scheduled-task-modal", closeScheduledTaskDialog], ["node-detail-modal", closeNodeDetail], ["work-table-modal", closeWorkTable], ["new-session-modal", closeNewSessionModal]]) {
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
    closeWorkTable();
    closeNewSessionModal();
  }
});

// FA toggle
elements["perm-toggle"].addEventListener("click", async function() {
  const next = !Boolean(client.current()?.runtime?.full_access);
  try { await invoke("SetFullAccess", next); await refresh({ scroll: false }); }
  catch (error) { showToast(error); }
});

// ── 左右栏宽度拖拽 ─────────────────────────────────────────
const LEFT_WIDTH_KEY = "seelex.left-panel-width";
const RIGHT_WIDTH_KEY = "seelex.right-panel-width";
const LEFT_WIDTH_RANGE = [200, 420];
const RIGHT_WIDTH_RANGE = [220, 480];

function storageGet(key) {
  try { return window.localStorage.getItem(key); } catch { return null; }
}
function storageSet(key, value) {
  try { window.localStorage.setItem(key, value); } catch { /* 无存储环境忽略 */ }
}
function clampPanelWidth(value, min, max) {
  return Math.min(max, Math.max(min, Math.round(value)));
}
function applyPanelWidths() {
  const left = clampPanelWidth(Number(storageGet(LEFT_WIDTH_KEY)) || 268, ...LEFT_WIDTH_RANGE);
  const right = clampPanelWidth(Number(storageGet(RIGHT_WIDTH_KEY)) || 300, ...RIGHT_WIDTH_RANGE);
  document.documentElement.style.setProperty("--left-w", `${left}px`);
  document.documentElement.style.setProperty("--right-w", `${right}px`);
}
function setupPanelDividers() {
  applyPanelWidths();
  const shell = document.querySelector(".app-shell");
  const leftDivider = document.getElementById("left-divider");
  const rightDivider = document.getElementById("right-divider");
  if (!shell || !leftDivider || !rightDivider) return;

  function setWidth(variable, key, range, width) {
    const clamped = clampPanelWidth(width, ...range);
    document.documentElement.style.setProperty(variable, `${clamped}px`);
    storageSet(key, String(clamped));
  }
  function beginDrag(divider, onMove) {
    return event => {
      if (event.button !== 0) return;
      event.preventDefault();
      divider.classList.add("is-dragging");
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      const move = moveEvent => onMove(moveEvent);
      const up = () => {
        divider.classList.remove("is-dragging");
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
        window.removeEventListener("pointermove", move);
        window.removeEventListener("pointerup", up);
      };
      window.addEventListener("pointermove", move);
      window.addEventListener("pointerup", up);
    };
  }
  leftDivider.addEventListener("pointerdown", beginDrag(leftDivider, event => {
    setWidth("--left-w", LEFT_WIDTH_KEY, LEFT_WIDTH_RANGE, event.clientX - shell.getBoundingClientRect().left);
  }));
  rightDivider.addEventListener("pointerdown", beginDrag(rightDivider, event => {
    setWidth("--right-w", RIGHT_WIDTH_KEY, RIGHT_WIDTH_RANGE, shell.getBoundingClientRect().right - event.clientX);
  }));

  function keyboardAdjust(divider, isRight) {
    divider.addEventListener("keydown", event => {
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
      event.preventDefault();
      const step = event.key === "ArrowLeft" ? -16 : 16;
      if (isRight) {
        const current = parseFloat(document.documentElement.style.getPropertyValue("--right-w")) || 300;
        setWidth("--right-w", RIGHT_WIDTH_KEY, RIGHT_WIDTH_RANGE, current - step);
      } else {
        const current = parseFloat(document.documentElement.style.getPropertyValue("--left-w")) || 268;
        setWidth("--left-w", LEFT_WIDTH_KEY, LEFT_WIDTH_RANGE, current + step);
      }
    });
  }
  keyboardAdjust(leftDivider, false);
  keyboardAdjust(rightDivider, true);
}
setupPanelDividers();

function resizePrompt() {
  elements.prompt.style.height = "auto";
  elements.prompt.style.height = `${Math.min(elements.prompt.scrollHeight, 180)}px`;
}

async function initialise() {
  try {
    hydrateIcons();
    state.rightTab = storedRightTab();
    setRightTab(state.rightTab);
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
