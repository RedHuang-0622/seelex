import { escapeHtml, hydrateIcons, icon, queueMoveTarget } from "./components.js";
import { createChatView } from "./chat-view.js";
import { createGUIClient } from "./client-state.js";
import { createConversationView } from "./conversation-view.js";
import { createTrajectoryView } from "./trajectory-view.js";
import { buildTrajectory } from "./trajectory.js";
import { createEffortControl } from "./effort-control.js";
import {
  planToDSL, renderNodeDetail, setNodeDetailConversation, bindNodeDetailTabs, subagentTreeNodeToDSL, workItemToDetailNode
} from "./plan-dsl.js";
import { createWorkTableView, countUnread, workTableSignatures } from "./work-table.js";
import { createWorkTreeView } from "./worktree-view.js";
import { createGitLogView } from "./git-log-view.js";
import { createFilePreviewController } from "./file-preview.js";
import { renderContextCompactions } from "./context-summary.js";
import { createRuntimeEventBinder } from "./runtime-events.js";
import { renderScheduledTasks, renderScheduledTasksTable } from "./scheduled-tasks-view.js";
import { agentTeamOrderForDrag, hirePanel, isPinnedRole, nextAgentTeamOrder, normalizeAgentTeam, normalizeTeamGlobal, normalizeTeamLibrary, renderAgentTeam, renderRoleSessionDetail, roleDisplayName, teamEditorPanel } from "./agent-team-view.js";
import { renderHistorySearchResults } from "./history-search.js";
import { createThemeController, loadThemeManifest } from "./theme.js";
import { duplicateSuffix, titleSuffix, readTitleTails, writeTitleTails } from "./sidebar.js";
import {
  DOCK_STORAGE_KEY,
  VIEW_META,
  DEFAULT_DOCK_STATE,
  REGIONS,
  normalizeDockState,
  regionOf,
  swapViews,
  isViewActive
} from "./dock-layout.js";
import { createPerfHooks } from "./perf-hooks.js";
import { createLiveDiag } from "./live-diag.js";

const state = {
  info: null,
  commandTrigger: "/",
  commandSuggestions: [],
  commandSelected: 0,
  inlineSuggestions: [],
  inlineSelected: 0,
  inlineRequest: 0,
  resumingSessionID: "",
  openSessionMenu: "",
  tab: "conversation",
  rightTab: "status",
  trajectoryFilter: "all",
  // 账户是 provider → model 两级：这是"当前停在哪个供应商"的纯 UI 状态。
  accountProvider: ""
};

const elements = Object.fromEntries([
  "app-title", "app-version", "connection-dot", "provider-label", "token-label",
  "session-list", "session-count", "new-session",
  "plugin-list", "plugin-count", "account-list", "account-count", "conversation", "conversation-tabs", "trajectory",
  "empty-state", "composer", "prompt", "composer-status", "stop-button", "send-button",
  "runtime-details", "effort-control", "effort-range", "effort-value", "work-section", "work-count", "work-unread", "work-table-open", "work-table-summary", "work-table-modal", "work-table-modal-close", "work-table-modal-view", "scheduled-task-section", "scheduled-task-view", "scheduled-task-count", "new-scheduled-task", "scheduled-task-modal", "scheduled-task-close", "sched-name", "sched-kind", "sched-mode", "sched-period-value", "sched-period-unit", "sched-period-field", "sched-datetime", "sched-datetime-field", "sched-command", "sched-command-field", "sched-prompt", "sched-prompt-field", "sched-enabled", "sched-enabled-field", "sched-submit", "history-search-section", "history-search-form", "history-search-input", "history-search-view", "history-search-count", "skill-list", "history-bar",
  "project-name", "project-root", "project-status", "project-overview", "worktree-view", "file-count", "context-compactions",
  "team-section", "team-view", "team-count",
  "role-session-modal", "role-session-close", "role-session-modal-title", "role-session-view",
  "right-tabs", "goal-section", "goal-badge", "goal-view", "code-panes", "code-pane-worktree", "code-pane-gitlog", "git-log-view", "git-log-count",
  "file-preview-pane", "file-preview-meta", "file-preview-view", "file-preview-close", "file-preview-divider",
  "runtime-button", "runtime-modal", "runtime-close", "settings-button", "settings-modal", "settings-close", "storage-backend", "storage-path", "storage-path-field", "storage-dsn", "storage-dsn-field", "storage-test", "storage-save", "storage-status", "theme-picker", "inline-suggestions",
  "command-button", "command-modal", "command-close", "command-triggers", "command-search", "command-results",
  "load-history", "latest-history", "interaction-modal", "perm-toggle", "interaction-risk", "interaction-title",
  "new-session-modal", "new-session-close", "new-session-task", "new-session-workspace", "new-session-back", "new-session-workspace-list", "new-session-pick-folder", "new-session-step-1", "new-session-step-2",
  "scheduled-table-modal", "scheduled-table-close", "scheduled-table-open", "scheduled-table-summary", "scheduled-table-view",
  "interaction-question", "interaction-preview", "interaction-options",
  "node-detail-modal", "node-detail-close", "node-detail-title", "node-detail-content", "toast", "ui-tooltip",
  "toggle-left-panel", "toggle-right-panel"
].map(id => [id, document.getElementById(id)]));

// ── 子页停靠布局（主视图 / 右栏）────────────────────────────
// 五个子页（conversation/trajectory/status/workbench/code）按 dockState
// 分区到主视图与右栏：点击切换激活页，拖拽可在栏内换序、跨栏置换。
// 布局与激活是纯前端本地状态（localStorage，键 seelex.dock.v1），
// 不新增后端/Bridge 契约；布局演算见 dock-layout.js。
const dockHosts = {
  main: document.getElementById("main-host"),
  right: document.getElementById("right-host")
};
const viewPanels = {
  conversation: document.getElementById("conversation-shell"),
  trajectory: elements.trajectory,
  status: document.querySelector('[data-right-panel="status"]'),
  workbench: document.querySelector('[data-right-panel="workbench"]'),
  code: document.querySelector('[data-right-panel="code"]')
};

function readDockState() {
  let raw = null;
  try {
    const value = localStorage.getItem(DOCK_STORAGE_KEY);
    raw = value ? JSON.parse(value) : null;
  } catch {
    raw = null;
  }
  if (!raw) {
    const legacyRight = (() => {
      try { return localStorage.getItem("seelex.right.tab"); } catch { return null; }
    })();
    raw = {
      layout: {
        main: [...DEFAULT_DOCK_STATE.layout.main],
        right: [...DEFAULT_DOCK_STATE.layout.right]
      },
      active: {
        main: DEFAULT_DOCK_STATE.active.main,
        right: ["status", "workbench", "code"].includes(legacyRight)
          ? legacyRight
          : DEFAULT_DOCK_STATE.active.right
      }
    };
  }
  return normalizeDockState(raw);
}

let dockState = readDockState();

function persistDockState() {
  try {
    localStorage.setItem(DOCK_STORAGE_KEY, JSON.stringify(dockState));
  } catch {
    /* 无存储环境忽略：本次布局不记忆，功能不受影响 */
  }
}

function dockTabBar(region) {
  return region === "main" ? elements["conversation-tabs"] : elements["right-tabs"];
}

function renderTabBar(region) {
  const bar = dockTabBar(region);
  const className = region === "main" ? "conversation-tab" : "right-tab";
  bar.innerHTML = dockState.layout[region].map(view => {
    const active = view === dockState.active[region];
    const meta = VIEW_META[view];
    return `<div class="${className}${active ? " is-active" : ""}" role="tab" data-view="${view}" aria-selected="${String(active)}" draggable="true" tabindex="0" title="点击切换；拖拽可换序，拖到另一栏可与对应子页置换">${escapeHtml(meta.label)}</div>`;
  }).join("");
}

function renderTabBars() {
  renderTabBar("main");
  renderTabBar("right");
}

// syncSessionChrome 强制“会话类页面不可见时不显示会话专属悬浮件”，
// 避免隐藏容器里的渲染把空态/加载更早/输入框带出来；对话页可见时
// 由 chatView 自行管理空态与历史按钮。
function syncSessionChrome() {
  if (!elements.composer) return;
  const conversationShown = isViewActive(dockState, "conversation");
  const mainShowsSession = dockState.active.main === "conversation" || dockState.active.main === "trajectory";
  // 输入框只在主视图处于会话类子页时显示：其它主视图（工作台/状态/资源
  // 管理器）全宽展示时不能被底部输入框遮住内容。
  elements.composer.classList.toggle("hidden", !mainShowsSession);
  // effort 是"下一回合"设置：本回合 running 时后端按 G0b/INV-G7 拒绝切换，
  // 因此把入口先锁上并说明原因，而不是让用户拖完再看失败提示。
  effortControl.setEnabled(!Boolean(client.current()?.chat?.running));
  if (!conversationShown) {
    elements["empty-state"].classList.add("hidden");
    elements["history-bar"].classList.add("hidden");
  }
}

function runViewActivation(view) {
  const snapshot = client.current();
  if (view === "conversation") {
    if (!snapshot) return;
    chatView.renderConversation(
      snapshot.conversation || [],
      snapshot.chat || {},
      "preserve",
      snapshot.has_more_history,
      snapshot.session?.status === "restoring"
    );
    chatView.renderControls(snapshot);
    return;
  }
  if (view === "trajectory") {
    if (snapshot) renderTrajectory(snapshot, true);
    refreshPromptInjection();
    return;
  }
  if (view === "code") refreshGitLogIfStale();
}

// applyDockState 是停靠布局的唯一渲染入口：面板归属、激活页、页签条、
// localStorage 与激活钩子都从这里收敛。
function applyDockState() {
  const prevMain = state.tab;
  const prevRight = state.rightTab;
  dockState = normalizeDockState(dockState);
  for (const region of REGIONS) {
    for (const view of dockState.layout[region]) {
      const panel = viewPanels[view];
      if (panel && panel.parentElement !== dockHosts[region]) {
        dockHosts[region].appendChild(panel);
      }
    }
  }
  for (const region of REGIONS) {
    for (const view of dockState.layout[region]) {
      viewPanels[view]?.classList.toggle("hidden", view !== dockState.active[region]);
    }
  }
  state.tab = dockState.active.main;
  state.rightTab = dockState.active.right;
  renderTabBars();
  syncSessionChrome();
  persistDockState();
  if (state.tab !== prevMain) runViewActivation(state.tab);
  if (state.rightTab !== prevRight) runViewActivation(state.rightTab);
}

function setDockTab(region, view) {
  if (!REGIONS.includes(region) || !dockState.layout[region].includes(view)) return;
  if (dockState.active[region] === view) return;
  dockState.active[region] = view;
  applyDockState();
}

// revealView 让指定子页在其所在栏成为激活页（文件预览打开 / chip 跳转轨迹
// 等入口需要视图立即可见）。
function revealView(view) {
  const region = regionOf(dockState.layout, view);
  if (!region) return;
  if (dockState.active[region] !== view) {
    dockState.active[region] = view;
    applyDockState();
  }
}

function clearDockDragState() {
  document.querySelectorAll(
    ".conversation-tab.is-dragging, .conversation-tab.is-drag-over, .right-tab.is-dragging, .right-tab.is-drag-over"
  ).forEach(button => button.classList.remove("is-dragging", "is-drag-over"));
}
document.addEventListener("dragend", clearDockDragState);

function bindDockTabs(region) {
  const bar = dockTabBar(region);
  bar.addEventListener("click", event => {
    const tab = event.target.closest?.("[data-view]");
    if (tab) setDockTab(region, tab.dataset.view);
  });
  bar.addEventListener("keydown", event => {
    if (event.key !== "Enter" && event.key !== " ") return;
    const tab = event.target.closest?.("[data-view]");
    if (!tab) return;
    event.preventDefault();
    setDockTab(region, tab.dataset.view);
  });
  bar.addEventListener("dragstart", event => {
    const tab = event.target.closest?.("[data-view]");
    if (!tab) return;
    event.dataTransfer.effectAllowed = "move";
    event.dataTransfer.setData("text/plain", tab.dataset.view);
    tab.classList.add("is-dragging");
  });
  bar.addEventListener("dragend", clearDockDragState);
  bar.addEventListener("dragover", event => {
    event.preventDefault();
    event.dataTransfer.dropEffect = "move";
    clearDockDragState();
    const target = event.target.closest?.("[data-view]");
    if (target) target.classList.add("is-drag-over");
  });
  bar.addEventListener("drop", event => {
    event.preventDefault();
    const target = event.target.closest?.("[data-view]");
    const sourceId = event.dataTransfer.getData("text/plain");
    clearDockDragState();
    if (!sourceId) return;
    const sourceRegion = regionOf(dockState.layout, sourceId);
    if (!sourceRegion) return;
    // 拖到页签上 = 与该页签置换；拖到页签条空白处（仅跨栏） = 与该栏当前
    // 激活页置换，避免“拖进中间却落不到目标”的空操作。
    const targetView = target?.dataset.view || (sourceRegion !== region ? dockState.active[region] : "");
    if (!targetView) return;
    const next = swapViews(dockState, sourceRegion, sourceId, region, targetView);
    if (next === dockState) return;
    // drop 里同步重建页签条会删掉拖动源，WebView2 可能收不到 dragend、
    // 残留拖拽鼠标状态（表现为拖完后其它点击失灵）。延到下一帧再落布局，
    // 让浏览器先结束本次 drag 会话。
    window.requestAnimationFrame(() => {
      dockState = next;
      applyDockState();
    });
  });
}

bindDockTabs("main");
bindDockTabs("right");

const conversationView = createConversationView(elements.conversation, {
  copyText: value => navigator.clipboard.writeText(value),
  notify: showToast,
  loadMore: loadOlderHistory,
  // 右侧全量用户输入索引：刻度数据面由 refreshInputIndex 推入，点击尚未加载
  // 的刻度时经本通道按页回读再定位（见 locateInputByReadBack；未装配时轮轴
  // 只提示，不静默空转）。
  locateInput: locateInputByReadBack,
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
  onFilterChange: kind => { state.trajectoryFilter = kind; },
  loadMore: () => loadOlderHistory()
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
// 会话新鲜度诊断角标：事件/增量/刷新/缺口/补取/缓冲计数（定位同视图内容
// 不及时；纯诊断，不参与业务状态）。
const liveDiag = createLiveDiag();
if (elements["live-diag-host"]) {
  elements["live-diag-host"].appendChild(liveDiag.badge);
}
window.__seelexLiveDiag = liveDiag;
const client = createGUIClient({
  loadSnapshot: () => invoke("Snapshot"),
  onSnapshot: (snapshot, options) => {
    // 视图会话切换 = Bridge 重建订阅：宿主侧 ack 游标随新订阅从 0 重计，
    // 渲染层的待发回执水位也必须跟着复位，否则旧会话的高水位会把新订阅的
    // 回执吞掉（seq <= ackPendingSeq 直接 return），Bridge 误判渲染层落后并
    // 反复重推同一批事件。
    const sessionID = snapshot?.session?.id;
    if (sessionID !== lastViewSessionID) {
      lastViewSessionID = sessionID;
      resetAckWatermark();
    }
    render(snapshot, options);
  },
  onIncremental: renderIncremental,
  // 缺口增量补取：宿主从重放窗口按 delivery_seq 补事件，补不齐才重拉快照。
  replay: sinceSeq => invoke("ReplayEvents", sinceSeq),
  // 应用回执：告诉宿主哪些序号已经落地，宿主因此不必用轮询猜自己漏没漏事件。
  onApplied: reportAppliedEvents,
  onDiag: stats => liveDiag.update(stats),
  onError: showToast
});
const bindRuntimeEvents = createRuntimeEventBinder({ client, onError: showToast });

// reportAppliedEvents 回报渲染层实际应用到的 delivery_seq（C4）。事件密集期按
// 150ms 尾随合并：每条事件一次跨进程往返太贵，而宿主判定"没收到回执"的延迟
// 更长（Bridge.eventResendDelay），正常投递不会被误判成丢失。宿主已关闭时回执
// 失败静默忽略——回执是尽力而为的确认，不是业务命令。
let ackTimer = null;
let ackPendingSeq = 0;
let lastViewSessionID = null;

function resetAckWatermark() {
  if (ackTimer !== null) {
    window.clearTimeout(ackTimer);
    ackTimer = null;
  }
  ackPendingSeq = 0;
}

function reportAppliedEvents(seq) {
  if (!seq || seq <= ackPendingSeq) return;
  ackPendingSeq = seq;
  if (ackTimer !== null) return;
  ackTimer = window.setTimeout(() => {
    ackTimer = null;
    invoke("AckEvents", ackPendingSeq).catch(() => {});
  }, 150);
}
const workTableView = createWorkTableView(elements["work-table-modal-view"]);
const workTreeView = createWorkTreeView(elements["worktree-view"], {
  loadDir: async relPath => invoke("WorkspaceTree", relPath, 1),
  onOpenFile: entry => openFilePreview(entry)
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
// 文件预览（「资源管理器」子页左抽屉）：工作树文件点击 → 后端读取受控字节
// （containment/敏感过滤/上限在 workspace 层保证）→ 按类型分派渲染。
const filePreviewController = createFilePreviewController({
  view: elements["file-preview-view"],
  meta: elements["file-preview-meta"],
  loader: async (entry, kind, limit) => invoke("WorkspaceFileContent", entry.path, limit),
  onError: showToast
});
let previewPaneOpen = false;
let previewRoot = "";
elements["file-preview-close"].addEventListener("click", closeFilePreview);
// workTableSeen 是“已读”快照（status|retry_count 签名）；workTableOpen
// 控制弹窗打开期间不显示未读角标。
let workTableSeen = new Map();
let workTableOpen = false;
// worktreeRoot 是已加载文件树的工作区 root；worktreeFileCount 是递归文件
// 统计；lastChatRunning 用于在 chat 结束（文件可能变化）时刷新。
let worktreeRoot = "";
let worktreeFileCount = null;
let lastChatRunning = false;
// promptLayersCache 是轨迹视图"前缀注入"的本地缓存（后端 PromptLayers
// 桥接数据，不进 Snapshot；打开轨迹子页时刷新）。
let promptLayersCache = null;
// 账户栏的最近一次 runtime：供应商切换是纯前端视图切换，不必为它再拉一份快照。
let lastAccountsRuntime = {};
// composerSaveTimer 是未发送输入草稿的防抖落盘定时器（草稿会话输入后
// 300ms 写后端，跨重启恢复；物化提交后 draft 标记消失，不再落盘）。
let composerSaveTimer = null;
const effortControl = createEffortControl({
  root: elements["effort-control"],
  input: elements["effort-range"],
  output: elements["effort-value"],
  selectEffort: async level => {
    // Bridge 返回后端**真正生效的值**：控件据此渲染，而不是假定请求值生效。
    const applied = await invoke("SwitchEffort", level);
    await refresh({ scroll: false });
    // effort 改变会改写 system 前缀的 effort 层；轨迹子页激活时刷新注入层。
    if (isViewActive(dockState, "trajectory")) refreshPromptInjection();
    return applied;
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

// scheduleComposerSave 在草稿会话输入后防抖持久化未发送正文
// （仅 draft 会话有归属；非草稿不调用后端）。
function scheduleComposerSave() {
  const snapshot = client.current();
  if (!snapshot?.session?.draft) return;
  window.clearTimeout(composerSaveTimer);
  composerSaveTimer = window.setTimeout(() => {
    const current = client.current();
    if (!current?.session?.draft) return;
    invoke("SaveComposerDraft", elements.prompt.value).catch(() => {});
  }, 300);
}

// restoreComposerDraft 在整份快照渲染时把后端恢复的草稿正文回填输入框
// （仅在未聚焦输入框时生效，避免覆盖用户正在输入的内容）。
function restoreComposerDraft(snapshot) {
  if (!snapshot?.session?.draft || !snapshot.session.composer) return;
  if (document.activeElement === elements.prompt) return;
  if (elements.prompt.value === snapshot.session.composer) return;
  elements.prompt.value = snapshot.session.composer;
  resizePrompt();
  elements.prompt.setSelectionRange(elements.prompt.value.length, elements.prompt.value.length);
}

function render(snapshot, options = {}) {
  const started = performance.now();
  restoreComposerDraft(snapshot);
  renderSessions(snapshot.sessions || [], snapshot.session || {}, snapshot.capabilities || {}, snapshot.session_workspaces || {}, snapshot.workspaces || []);
  renderProject(snapshot);
  renderRuntime(snapshot.runtime || {});
  renderPlugins(snapshot.runtime || {});
  renderAccounts(snapshot.runtime || {});
  chatView.render(snapshot, options.scrollMode);
  refreshInputIndex(snapshot);
  renderTrajectory(snapshot);
  refreshPlanDetailData(snapshot.runtime?.plan, snapshot.runtime?.subagent_tree);
  renderWorkTable(snapshot.runtime?.work_table, snapshot.runtime?.work_table_batches);
  renderGoal(snapshot);
  renderScheduledTaskPanel(snapshot.runtime || {});
  scheduleAgentTeamRefresh(snapshot);
  renderSkills(snapshot.runtime?.skills || []);
  renderInteraction(snapshot.interaction);
  syncSessionChrome();
  perfHooks.markRender(performance.now() - started);
}

function renderIncremental(snapshot, kind) {
  if (!snapshot) return;
  const started = performance.now();
  if (["message.added", "message.delta", "tool.started", "tool.completed"].includes(kind)) {
    chatView.renderConversation(snapshot.conversation || [], snapshot.chat || {}, "auto", snapshot.has_more_history);
    chatView.renderControls(snapshot);
    renderTrajectory(snapshot);
    // 新用户输入会多出一条索引刻度（助手增量不动索引），按指纹去重后拉取。
    if (kind === "message.added") refreshInputIndex(snapshot);
    if (kind !== "message.delta") renderProject(snapshot);
    // 会话类页面不可见时强制隐藏会话专属悬浮件（空态/加载更早/输入框）。
    syncSessionChrome();
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
    // 轨迹子页激活时刷新前缀注入层（effort/skill/插件等可能已变化）。
    if (isViewActive(dockState, "trajectory")) refreshPromptInjection();
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
    // 任务级变更可能带动激活 skill 变化；轨迹子页激活时刷新前缀注入层。
    if (isViewActive(dockState, "trajectory")) refreshPromptInjection();
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
// 子页是否激活由停靠布局决定（在主视图或右栏任一栏激活即渲染对应 DOM）；
// 过滤类型、展开与滚动仍是本地 UI 状态，不进入 Snapshot。

// renderTrajectory 从权威 conversation 派生轨迹记录并渲染；extras 携带轨迹
// 轴的元数据轨：前缀注入层（Bridge.PromptLayers 缓存）与压缩记录
// （Snapshot.Task.ContextCompactions，压缩发生的公开信号，随全量刷新到达）。
// active=false（轨迹子页未激活）时只缓存数据面，不碰轨迹 DOM。
function renderTrajectory(snapshot, active = isViewActive(dockState, "trajectory")) {
  if (!snapshot) return;
  const compactions = Array.isArray(snapshot.task?.context_compactions) ? snapshot.task.context_compactions : [];
  trajectoryView.render(buildTrajectory(snapshot.conversation || []), state.trajectoryFilter, active, {
    prefixLayers: promptLayersCache || [],
    compactions,
    // 轨迹基于"已加载的可见窗口"：把窗口边界显式告诉视图，长会话才不会让人
    // 以为轨迹丢了早期内容；hasMore 时提供"加载更早"入口。
    window: {
      messages: (snapshot.conversation || []).length,
      total: Number(snapshot.total_messages || 0),
      hasMore: Boolean(snapshot.has_more_history)
    }
  });
}

// refreshPromptInjection 拉取当前会话的 prompt 前缀层并重渲染轨迹视图。
async function refreshPromptInjection() {
  try {
    const layers = await invoke("PromptLayers");
    promptLayersCache = Array.isArray(layers) ? layers : [];
  } catch {
    promptLayersCache = [];
  }
  renderTrajectory(client.current());
}

// 队列条目的编辑动作（上移 / 下移 / 撤回编辑）：三处动作都只带下标，顺序
// 事实源在后端会话队列（application/service_queue.go），渲染层不自行改本地
// 顺序——成功后统一 refresh 拉权威快照，失败把后端错误原样提示。
elements.conversation.addEventListener("click", async event => {
  const button = event.target.closest?.("[data-queue-action]");
  if (!button || button.disabled) return;
  event.preventDefault();
  const sessionID = client.current()?.session?.id || "";
  const index = Number(button.dataset.queueIndex);
  button.disabled = true;
  try {
    if (button.dataset.queueAction === "recall") {
      const text = await invoke("RecallQueuedInput", sessionID, index);
      recallQueuedInput(text);
    } else {
      const length = (client.current()?.chat?.input_queue || []).length;
      const target = queueMoveTarget(button.dataset.queueAction, index, length);
      if (!target) return;
      await invoke("ReorderQueuedInput", sessionID, target.from, target.to);
    }
    await refresh({ scroll: "auto" });
  } catch (error) {
    showToast(error);
  } finally {
    button.disabled = false;
  }
});

// recallQueuedInput 把撤回的排队原文交还输入框重新编辑：输入框已有未发送
// 正文时把撤回内容追加在后（不覆盖用户草稿），随后聚焦并把光标放到末尾。
function recallQueuedInput(text) {
  const recalled = String(text ?? "");
  if (!recalled) return;
  const existing = elements.prompt.value;
  elements.prompt.value = existing.trim() ? `${existing.trimEnd()}\n${recalled}` : recalled;
  hideInlineSuggestions();
  resizePrompt();
  scheduleComposerSave();
  elements.prompt.focus();
  elements.prompt.setSelectionRange(elements.prompt.value.length, elements.prompt.value.length);
}

// 聊天区「一行带过」的思考/工具 chip 点击 → 切到轨迹子页并定位对应记录。
elements.conversation.addEventListener("click", event => {
  const chip = event.target.closest(".chat-chip[data-trajectory-key]");
  const key = chip?.dataset.trajectoryKey;
  if (!key) return;
  revealView("trajectory");
  requestAnimationFrame(() => {
    const row = document.querySelector(`[data-trajectory-key="${CSS.escape(key)}"]`);
    if (!row) return;
    const reduced = window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    row.scrollIntoView({ block: "center", behavior: reduced ? "auto" : "smooth" });
    row.classList.add("is-flash");
    setTimeout(() => row.classList.remove("is-flash"), 1400);
  });
});

// 右栏子页归属与激活由停靠布局统一管理（见上方 dockState），这里的存储键
// 只保留「资源管理器」子页内部工作树/提交记录两面板的顺序记忆。
const RIGHT_PANE_ORDER_KEY = "seelex.right.codePanes";

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
  const pendingApprovals = (snapshot.sessions || []).reduce(
    (sum, session) => sum + Number(session.approval_count || 0), 0
  );
  // 条目化（键值两列）：状态信息是"一眼扫过"的清单，两列网格卡片会把
  // 标签与数值排成自由布局，窄栏里还会错位；表格行则始终对齐。
  const rows = [
    ["状态", running ? "Agent 执行中" : "Ready"],
    ["会话", snapshot.session?.draft ? "待发送" : shortSessionID(snapshot.session?.id || "—")],
    ["消息", String(snapshot.conversation?.length || 0)],
    ["任务", snapshot.task ? snapshot.task.status : "idle"],
    ["待审批", pendingApprovals > 0 ? `${pendingApprovals} 项` : "0"],
    ["文件数", fileCountLabel()]
  ];
  elements["project-status"].innerHTML = `<div class="status-table" role="table" aria-label="项目状态">
    <div class="status-row is-head" role="row"><span role="columnheader">项</span><span role="columnheader">值</span></div>
    ${rows.map(([label, value]) => `<div class="status-row" role="row">
      <span role="cell" class="status-label">${escapeHtml(label)}</span>
      <span role="cell" class="status-value" title="${escapeHtml(value)}">${escapeHtml(value)}</span>
    </div>`).join("")}
  </div>`;
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
  // 预览的文件属于旧工作区时：抽屉内容失效，随树一起清空。
  if (previewPaneOpen && rootPath !== previewRoot) closeFilePreview();
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
  const workspaceNameCounts = new Map();
  for (const workspace of workspaces) {
    const name = workspace.name || "";
    if (name) workspaceNameCounts.set(name, (workspaceNameCounts.get(name) || 0) + 1);
  }
  const items = sessions.map(session => session.id === currentID && current.name
    ? { ...session, name: current.name }
    : session);
  if (currentID && !items.some(session => session.id === currentID)) {
    items.unshift({ id: currentID, name: current.name || "", current: true });
  }
  elements["session-count"].textContent = String(items.length);
  elements["session-list"].innerHTML = items.length
    ? renderSessionGroups(items, currentID, sessionWorkspaces, workspaceNames, workspaceNameCounts)
    : '<span class="muted list-empty">暂无会话</span>';

  // 会话行交互统一委托（见 bindSessionListActions）：逐行 addEventListener 在
  // 每次目录刷新时都会重建 N 个闭包与监听，条目越多越费内存，且旧监听随
  // innerHTML 一起变成孤儿。这里只登记一次。
  bindSessionListActions();
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
function renderSessionGroups(items, currentID, sessionWorkspaces, workspaceNames, workspaceNameCounts) {
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
  // 重名会话按渲染顺序编号（首条不编号，重复的从 2 起续号），尾号存档
  // 为键值对「名字 → 尾号」（编到第几号）。
  const sessionNameSeen = new Map();
  const sessionNameTail = new Map();
  const workspaceNameSeen = new Map();
  const groupHtml = keys.map(key => {
    let label = key === UNBOUND_WORKSPACE ? "未关联会话" : workspaceNames.get(key) || key;
    if (key !== UNBOUND_WORKSPACE) {
      const name = label;
      const total = workspaceNameCounts?.get(name) || 0;
      if (total > 1) {
        const index = (workspaceNameSeen.get(name) || 0) + 1;
        workspaceNameSeen.set(name, index);
        label = name + duplicateSuffix(index, total);
      }
    }
    const sessions = groups.get(key).slice().sort((a, b) => Number(Boolean(b.meta?.pinned)) - Number(Boolean(a.meta?.pinned)));
    const rows = sessions.map(session => {
      let nameIndex = 1;
      const name = session.name || "";
      if (name) {
        nameIndex = (sessionNameSeen.get(name) || 0) + 1;
        sessionNameSeen.set(name, nameIndex);
        sessionNameTail.set(name, Math.max(sessionNameTail.get(name) || 0, nameIndex));
      }
      return sessionRow(session, currentID, nameIndex);
    }).join("");
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
  // 尾号存档（名字 → 尾号），跨重启可查"编到第几号"。
  if (sessionNameTail.size > 0) {
    const tails = readTitleTails();
    for (const [name, tail] of sessionNameTail) {
      tails[name] = Math.max(Number(tails[name]) || 0, tail);
    }
    writeTitleTails(tails);
  }
  return groupHtml;
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

// sessionMetaByID 从最近一次目录渲染数据里取会话展示元数据（写元数据时要保留
// 其它字段，不能整份覆盖）。
function sessionMetaByID(sessionID) {
  const found = (lastSessionsRender?.sessions || []).find(item => item.id === sessionID);
  return found?.meta || {};
}

// ── 会话列表交互（一条委托监听 + ⋯ 段动作）───────────────────
// 会话行是列表里最常重绘的区域：逐行 addEventListener 会在每次目录刷新时重建
// N 个闭包与监听，条目越多越费内存，旧监听还会随 innerHTML 变成孤儿。动作统一
// 委托到 #session-list 一条监听，按 data-* 键分派。
let sessionListBound = false;

function bindSessionListActions() {
  if (sessionListBound || !elements["session-list"]) return;
  sessionListBound = true;
  elements["session-list"].addEventListener("click", onSessionListClick);
}

function sessionListSnapshot() {
  return lastSessionsRender || {
    sessions: [], current: { id: "" }, capabilities: {}, sessionWorkspaces: [], workspaces: []
  };
}

async function onSessionListClick(event) {
  const button = event.target?.closest?.("button");
  if (!button || button.disabled) return;
  const data = button.dataset || {};
  if (data.sessionMore !== undefined) {
    toggleSessionMenu(data.sessionMore);
    return;
  }
  if (data.sessionDraft !== undefined) {
    if (!client.current()?.session?.draft) await beginNewSession(); // 已在草稿则幂等
    return;
  }
  if (data.collapseGroup !== undefined) {
    toggleWorkspaceGroup(data.collapseGroup);
    return;
  }
  if (data.workspaceNewSession) {
    await bindWorkspaceAndStart(data.workspaceNewSession);
    return;
  }
  if (data.pinSession) {
    await toggleSessionPin(data.pinSession);
    return;
  }
  if (data.fork) {
    await forkSessionFromList(data.fork);
    return;
  }
  if (data.sessionDel) {
    await deleteSessionFromList(data.sessionDel);
    return;
  }
  if (data.session !== undefined) await resumeSessionFromList(data.session);
}

// toggleSessionMenu 开合条目的 ⋯ 段：同一时刻只开一条（省 DOM，也省视觉噪音）。
function toggleSessionMenu(sessionID) {
  state.openSessionMenu = state.openSessionMenu === sessionID ? "" : sessionID;
  rerenderSessions();
}

function closeSessionMenu() {
  if (state.openSessionMenu === "") return;
  state.openSessionMenu = "";
  rerenderSessions();
}

async function resumeSessionFromList(sessionID) {
  const { sessions, current, capabilities, sessionWorkspaces, workspaces } = sessionListSnapshot();
  const currentID = current?.id || "";
  if (!sessionID || sessionID === currentID) return;
  closeSessionMenu();
  if (!capabilities.session_resume) {
    showToast(capabilities.session_resume_reason || "当前版本暂不支持恢复历史会话");
    return;
  }
  state.resumingSessionID = sessionID;
  elements["composer-status"].textContent = "正在恢复会话…";
  rerenderSessions();
  try {
    await invoke("ResumeSession", sessionID);
    await refresh({ scroll: "bottom" });
  } catch (error) {
    elements["composer-status"].textContent = `恢复会话失败：${error?.message || String(error)}`;
    showToast(error);
    // 后端若已部分切换（迟到的失败），视图指针可能与用户所见不一致：立即拉一次
    // 权威快照收敛，否则后续输入会路由进用户看不到的会话。
    try { await refresh({ scroll: "preserve" }); } catch { /* 快照重拉失败按 toast 为准 */ }
  } finally {
    state.resumingSessionID = "";
    const latest = client.current();
    if (latest) {
      renderSessions(
        latest.sessions || sessions, latest.session || current,
        latest.capabilities || capabilities, latest.session_workspaces || sessionWorkspaces,
        latest.workspaces || workspaces
      );
    } else {
      rerenderSessions();
    }
  }
}

async function deleteSessionFromList(sessionID) {
  const currentID = sessionListSnapshot().current?.id || "";
  closeSessionMenu();
  if (!sessionID || sessionID === currentID) {
    showToast("不能删除当前会话");
    return;
  }
  if (!confirm(`确认删除会话 ${shortSessionID(sessionID)}？`)) return;
  try {
    await invoke("DeleteSession", sessionID);
    await refresh({ scroll: false });
  } catch (error) {
    showToast(error);
  }
}

async function forkSessionFromList(sessionID) {
  const { sessions, current, capabilities, sessionWorkspaces, workspaces } = sessionListSnapshot();
  closeSessionMenu();
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
    // 同 ResumeSession：分支可能已部分切换视图，失败后拉权威快照收敛，
    // 避免后续输入路由到用户看不到的会话。
    try { await refresh({ scroll: "preserve" }); } catch { /* 快照重拉失败按 toast 为准 */ }
  } finally {
    elements["composer-status"].textContent = "";
    const latest = client.current();
    if (latest) {
      renderSessions(
        latest.sessions || sessions, latest.session || current,
        latest.capabilities || capabilities, latest.session_workspaces || sessionWorkspaces,
        latest.workspaces || workspaces
      );
    } else {
      rerenderSessions();
    }
  }
}

async function toggleSessionPin(sessionID) {
  closeSessionMenu();
  const meta = sessionMetaByID(sessionID);
  try {
    // 展示元数据是后端状态（随目录下发）：写入后刷新快照即可，浏览器不再自行
    // 记忆置顶，避免换窗口/换设备就丢失。
    await invoke("SetSessionMeta", sessionID, !meta.pinned, meta.alias || "", meta.sort_order || 0);
    await refresh({ scroll: false });
  } catch (error) {
    showToast(error);
  }
}

// ── 共享提示气泡（一条 DOM，委托到 [data-tip]）────────────────
// 数据形态：data-tip 用 \n 分行——第一行通常是完整标题，第二行是时间 / token。
// 只在停顿后显示（鼠标扫过不闪提示），并在滚动、失焦、Esc、点击时收起。
const TIP_DELAY_MS = 320;
let tipTarget = null;
let tipTimer = 0;

function hideTip() {
  if (tipTimer) {
    window.clearTimeout(tipTimer);
    tipTimer = 0;
  }
  tipTarget = null;
  elements["ui-tooltip"]?.classList.add("hidden");
}

function queueTip(target) {
  const host = elements["ui-tooltip"];
  if (!host || !target) {
    hideTip();
    return;
  }
  if (tipTarget === target && !host.classList.contains("hidden")) return;
  hideTip();
  tipTarget = target;
  tipTimer = window.setTimeout(() => {
    tipTimer = 0;
    paintTip(target);
  }, TIP_DELAY_MS);
}

function paintTip(target) {
  const host = elements["ui-tooltip"];
  const text = String(target?.dataset?.tip || "");
  if (!host || !text) return;
  // 纯文本节点拼装：提示内容来自标题/时间，绝不进 innerHTML。
  host.replaceChildren();
  text.split("\n").forEach((line, index) => {
    if (index > 0) host.append(document.createElement("br"));
    host.append(document.createTextNode(line));
  });
  host.classList.remove("hidden");
  const anchor = target.getBoundingClientRect();
  const box = host.getBoundingClientRect();
  const margin = 8;
  let left = anchor.left;
  let top = anchor.bottom + 6;
  if (left + box.width > window.innerWidth - margin) left = Math.max(margin, window.innerWidth - box.width - margin);
  if (top + box.height > window.innerHeight - margin) top = Math.max(margin, anchor.top - box.height - 6);
  host.style.left = `${Math.round(left)}px`;
  host.style.top = `${Math.round(top)}px`;
}

document.addEventListener("pointerover", event => {
  const target = event.target?.closest?.("[data-tip]");
  if (target) queueTip(target);
});
document.addEventListener("pointerout", event => {
  const target = event.target?.closest?.("[data-tip]");
  if (!target) return;
  if (event.relatedTarget && target.contains(event.relatedTarget)) return; // 行内移动不算离开
  if (tipTarget === target) hideTip();
});
document.addEventListener("focusin", event => {
  const target = event.target?.closest?.("[data-tip]");
  if (target) queueTip(target); // 键盘聚焦也给同样的信息（可访问性）
});
document.addEventListener("focusout", hideTip);
document.addEventListener("scroll", hideTip, true);
document.addEventListener("keydown", event => {
  if (event.key !== "Escape") return;
  hideTip();
  closeSessionMenu();
});
document.addEventListener("click", event => {
  if (state.openSessionMenu === "") return;
  if (event.target?.closest?.(".session-more")) return; // ⋯ 段内部的点击自己处理
  closeSessionMenu();
});

// sessionRow 渲染一条会话条目。条目刻意分成两段（用户口径）：
//   标题段：状态点 + 完整标题（CSS 省略号截断），**不在条目里放时间/ token**；
//   ⋯ 段：省略号栏，点开就是原来的三个操作（置顶 / 分支 / 删除）。
// 时间与 token 只在鼠标常驻（或键盘聚焦）时随完整标题一起出现在共享提示气泡里
// ——data-tip 的第一行是完整标题，第二行是「时间 · tokens」。
function sessionRow(session, currentID, nameIndex = 1) {
  // 保留的"新建会话"草稿槽位：列表可见、可点击恢复（无 ID、不可恢复/删除/分支）。
  if (session.id === "" && session.status === "draft") {
    const active = !currentID;
    return `<div class="session-row is-draft">
      <button class="stack-button session-button session-draft ${active ? "active" : ""}" data-session-draft="1" data-tip="恢复新建会话草稿">
        <span class="entry-name">${icon("plus", 13)} ${escapeHtml(session.name || "新会话（草稿）")}</span><small>草稿 · 尚未发送</small>
      </button>
    </div>`;
  }
  const active = session.id === currentID;
  const resuming = session.id === state.resumingSessionID;
  const pinned = Boolean(session.meta?.pinned);
  const updated = session.updated_at
    ? new Date(session.updated_at).toLocaleString([], { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" })
    : "当前会话";
  const tokens = session.token_count ? `${session.token_count} tokens` : "";
  const tip = [updated, tokens].filter(Boolean).join(" · ");
  const display = resuming ? "恢复中…" : (session.meta?.alias || session.name || shortSessionID(session.id));
  const label = display + titleSuffix(nameIndex);
  const statusChip = session.status && session.status !== "idle"
    ? `<span class="session-status is-${escapeHtml(session.status)}">${sessionStatusLabel(session.status)}</span>`
    : "";
  const menuOpen = state.openSessionMenu !== "" && state.openSessionMenu === session.id;
  return `<div class="session-row${pinned ? " is-pinned" : ""}${menuOpen ? " is-menu-open" : ""}" data-session-row="${escapeHtml(session.id)}">
    <button class="stack-button session-button session-title ${active ? "active" : ""}" data-session="${escapeHtml(session.id)}" ${resuming ? "disabled" : ""} data-tip="${escapeHtml(`${label}\n${tip}`)}" aria-label="${escapeHtml(`${label} · ${tip}`)}">
      <span class="entry-name">${pinned ? `<span class="session-pin-mark" aria-hidden="true">${icon("star", 12)}</span>` : ""}${icon("message", 13)} ${escapeHtml(label)}</span>${statusChip}
    </button>
    <span class="session-more">
      <button class="session-more-toggle" type="button" data-session-more="${escapeHtml(session.id)}" aria-expanded="${menuOpen}" aria-label="更多操作" data-tip="更多操作：置顶 / 分支 / 删除">${icon("more", 14)}</button>
      <span class="session-more-actions"${menuOpen ? "" : " hidden"}>
        <button class="session-pin${pinned ? " is-on" : ""}" type="button" data-pin-session="${escapeHtml(session.id)}" aria-label="${pinned ? "取消置顶" : "置顶会话"}" data-tip="${pinned ? "取消置顶" : "置顶会话"}">${icon(pinned ? "star" : "star-outline", 13)}</button>
        <button class="session-fork" type="button" data-fork="${escapeHtml(session.id)}" aria-label="分支出新会话" data-tip="分支出新会话">${icon("branch", 13)}</button>
        <button class="session-del" type="button" data-session-del="${escapeHtml(session.id)}" aria-label="删除会话" data-tip="删除会话">${icon("close", 12)}</button>
      </span>
    </span>
  </div>`;
}

function sessionStatusLabel(status) {
  if (status === "running") return "运行中";
  if (status === "queued") return "排队";
  if (status === "draft") return "草稿";
  if (status === "restoring") return "恢复中";
  if (status === "awaiting_approval") return "待审批";
  if (status === "archived") return "已归档";
  return "";
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
  elements["perm-toggle"].innerHTML = fullAccess ? `全权 ${icon("check", 12)}` : "全权";

  effortControl.setLevel(runtime.effort);
}

function renderPlugins(runtime) {
  const plugins = runtime.plugins || [];
  elements["plugin-count"].textContent = String(plugins.length);
  elements["plugin-list"].innerHTML = plugins.map(plugin => `
    <button class="stack-button ${runtime.plugin === plugin.name ? "active" : ""}" data-plugin="${escapeHtml(plugin.name)}">
      ${escapeHtml(plugin.name)}<small>${escapeHtml(plugin.description || "")}</small>
    </button>`).join("");
}

// 插件切换：容器上一条委托（列表每次重绘不再逐行绑监听）。单飞语义保留——
// 切插件会附挂/卸载 MCP，重复点击只能排队等，所以命中后立刻锁住容器。
elements["plugin-list"]?.addEventListener("click", async event => {
  const button = event.target?.closest?.("button[data-plugin]");
  const host = elements["plugin-list"];
  if (!button || !host) return;
  if (host.dataset.switching === "1") return;
  host.dataset.switching = "1";
  host.classList.add("is-switching");
  button.classList.add("is-pending");
  try {
    await invoke("SwitchPlugin", button.dataset.plugin);
    // 切换结果由 runtime.changed（带完整运行时）＋ snapshot.changed 事件带回；
    // 这里再拉一次整份快照纯属叠延迟（MCP 附挂本身就可能几秒）。
    await refresh({ scroll: false });
  } catch (error) {
    showToast(error);
  } finally {
    host.dataset.switching = "";
    host.classList.remove("is-switching");
    button.classList.remove("is-pending");
  }
});

// renderAccounts 渲染账户栏（右栏「状态」子页）：**供应商 → 模型**两级级联——
// 先按 provider 分组（分组来自数据本身，不写死），选中供应商后只列它下面的模型，
// 一个模型一行账户，点行即切换账户（SelectAccount 收的仍是账户名）。
// 行内不放按钮，交互走容器委托（一条监听），列表刷新不产生 N 个闭包。
function renderAccounts(runtime) {
  const accounts = runtime.accounts || [];
  lastAccountsRuntime = runtime;
  elements["account-count"].textContent = String(accounts.length);
  if (!accounts.length) {
    elements["account-list"].innerHTML = '<span class="muted list-empty">暂无账户</span>';
    return;
  }
  const providerOf = account => account.provider || "未标注供应商";
  const current = accounts.find(account => account.name === runtime.account) || null;
  const providers = [];
  for (const account of accounts) {
    const provider = providerOf(account);
    if (!providers.includes(provider)) providers.push(provider);
  }
  // 停留的供应商优先：当前账户所属 > 上次选择 > 第一个（都不合法时回落）。
  const active = providers.includes(state.accountProvider)
    ? state.accountProvider
    : (current ? providerOf(current) : providers[0]);
  state.accountProvider = active;
  const providerRow = providers.map(provider => {
    const count = accounts.filter(account => providerOf(account) === provider).length;
    return `<button type="button" class="account-provider${provider === active ? " is-active" : ""}" data-account-provider="${escapeHtml(provider)}" aria-pressed="${provider === active}" data-tip="${escapeHtml(`${provider} · ${count} 个模型`)}">${escapeHtml(provider)}<span class="account-provider-count">${count}</span></button>`;
  }).join("");
  const rows = accounts.filter(account => providerOf(account) === active).map(account => {
    const isActive = current && current.name === account.name;
    const model = account.model || "—";
    const detail = `${account.provider || ""} ${account.model || ""}`.trim() || "—";
    return `<button type="button" class="account-row${isActive ? " is-active" : ""}" data-account="${escapeHtml(account.name)}"${account.disabled ? " disabled" : ""} aria-pressed="${isActive}" data-tip="${escapeHtml(`${account.name}\n${detail}`)}" aria-label="${escapeHtml(`${account.name} · ${detail}`)}">
        <span class="account-mark" aria-hidden="true">${icon(isActive ? "dot" : "circle", 12)}</span>
        <span class="account-name">${escapeHtml(model)}</span>
        <span class="account-meta">${escapeHtml(account.name)}</span>
        ${account.disabled ? '<span class="account-lock" title="该账户当前不可用">不可用</span>' : ""}
      </button>`;
  }).join("");
  elements["account-list"].innerHTML = `<div class="account-providers" role="group" aria-label="供应商">${providerRow}</div>
    <div class="account-models" role="group" aria-label="${escapeHtml(active)} 的模型">${rows}</div>`;
}

elements["account-list"]?.addEventListener("click", async event => {
  const providerButton = event.target?.closest?.("[data-account-provider]");
  if (providerButton?.dataset.accountProvider) {
    state.accountProvider = providerButton.dataset.accountProvider;
    renderAccounts(lastAccountsRuntime);
    return;
  }
  const row = event.target?.closest?.(".account-row");
  const name = row?.dataset?.account;
  if (!name || row.disabled) return;
  try {
    await invoke("SelectAccount", name);
    await refresh({ scroll: false });
  } catch (error) {
    showToast(error);
  }
});

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
  const governance = runtime.goal_governance && runtime.goal_governance.active
    ? runtime.goal_governance
    : null;
  const activeSkills = Array.isArray(runtime.active_skills) ? runtime.active_skills : [];
  const goalSection = elements["goal-section"];
  if (!goalSection) return;
  const goalText = latestUserInput(snapshot);
  const hasContent = governance || goalActive || activeSkills.length > 0 || task || goalText;
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
    stopGoalStallMonitor(view);
    return;
  }
  view.classList.remove("muted");
  const governanceLine = governance ? renderGoalGovernance(governance) : "";
  const goalLine = goalText
    ? `<div class="goal-text" title="${escapeHtml(goalText)}">${escapeHtml(truncateGoalText(goalText))}</div>`
    : "";
  const taskLine = task
    ? `<div class="goal-task"><span class="goal-task-status is-${escapeHtml(task.status || "idle")}">${escapeHtml(task.status || "idle")}</span><span class="goal-task-summary" title="${escapeHtml(task.summary || "")}">${escapeHtml(task.summary || "任务进行中")}</span></div>`
    : "";
  const chips = activeSkills.length
    ? `<div class="goal-skills">${activeSkills.map(skill => `<span class="chip">#${escapeHtml(skill)}</span>`).join("")}</div>`
    : "";
  view.innerHTML = `${goalLine}${taskLine}${governanceLine}${chips}`;
  if (governance) {
    startGoalStallMonitor(view, governance);
  } else {
    stopGoalStallMonitor(view);
  }
}

// renderGoalGovernance 渲染「目标 + 治理」只读面板：goal 状态/轮次/座次/
// TL 最近指令/断环横幅/心跳（字符画 §3.1；governance 视图来自
// runtime.goal_governance，goal 栈不入模型上下文）。
function renderGoalGovernance(governance) {
  const status = escapeHtml(governance.status || "active");
  const round = Number.isFinite(governance.round) ? governance.round : 0;
  const seat = governance.current_seat ? escapeHtml(governance.current_seat) : "";
  const peer = governance.peer_state ? escapeHtml(governance.peer_state) : "";
  const directive = governance.last_directive
    ? `<div class="goal-gov-directive" title="${escapeHtml(governance.last_directive)}">TL: ${escapeHtml(truncateGoalText(governance.last_directive, 160))}</div>`
    : "";
  const broken = governance.broken
    ? `<div class="goal-gov-broken">断环: ${escapeHtml(governance.break_reason || "已收束")}</div>`
    : "";
  const meta = [
    `<span class="goal-gov-status">${status}</span>`,
    `Round ${round}`,
    seat ? `座次 ${seat}` : "",
    peer ? `peer ${peer}` : "",
    `<span id="goal-stall" data-heartbeat-seq="${Number(governance.heartbeat_seq || 0)}" data-heartbeat-at="${Number(governance.heartbeat_at || 0)}"></span>`
  ].filter(Boolean).join(" · ");
  return `<div class="goal-governance"><div class="goal-gov-meta">${meta}</div>${directive}${broken}</div>`;
}

// startGoalStallMonitor 心跳停滞提示（前端只读展示）：治理推进会带来单调
// heartbeat_seq；超过 stallAfterSec 无新 seq 显示 stalled。不参与业务决策。
function startGoalStallMonitor(view, governance) {
  const stallAfterSec = 10;
  const deadline = Number(governance.heartbeat_at || 0) + stallAfterSec;
  const timer = view.__goalStallTimer;
  if (timer) clearInterval(timer);
  const tick = () => {
    const stall = view.querySelector("#goal-stall");
    if (!stall) return;
    const now = Math.floor(Date.now() / 1000);
    const stalled = now > deadline;
    stall.textContent = stalled ? "governance stalled" : `心跳 #${Number(governance.heartbeat_seq || 0)}`;
    stall.classList.toggle("goal-gov-stalled", stalled);
  };
  tick();
  view.__goalStallTimer = setInterval(tick, 1000);
}

function stopGoalStallMonitor(view) {
  if (!view || !view.__goalStallTimer) return;
  clearInterval(view.__goalStallTimer);
  delete view.__goalStallTimer;
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
  // Agent Team 的冷加载面板（入职/修改员工、新建/编辑团队）也吃 Esc：面板是
  // 悬浮在右栏上的编辑态，Esc 关掉它比"点 ✕"更快。
  if (event.key === "Escape") closeAgentTeamEditors();
});

// ── Agent Team（右侧栏 · 状态 → Agent Team）──────────────────
// 数据源是 Application API（Bridge.AgentTeam*）：员工表 / 发言顺序 / 团队库 /
// 全局母本（团队库 + 员工库 + 默认顺序）/ 定时 agent 分区。各份事实各有归属，
// 前端只提交用户改动，不做本地缓存：
//   - 发言顺序 = 会话 lifecycle.order_policy/order_roles（员工栏就是它，拖拽
//     只提交整表）；
//   - 员工配置（提示词/权限）= 会话角色注册表（session/team/roles.json）；
//   - 团队库/员工库/默认顺序 = **全局**母本（数据根下 team/），装配 = 把库条目
//     写成会话员工表；会话读的是母本深拷贝副本，只有「确认普及」才回写母本。
let agentTeamPresets = null;
let agentTeamLibrary = null;
let agentTeamGlobal = null;
let agentTeamView = null;
let agentTeamSessionID = "";
let agentTeamError = "";
let agentTeamLoading = false;
// agentTeamDragRole 是"正在被拖拽的员工"：拖拽只在内部状态里过渡，落点一确定就
// 提交整表（没有乐观重排、没有第二份顺序事实）。
let agentTeamDragRole = "";

// agentTeamCurrentPolicy 取本次提交的顺序策略：优先用面板里用户选中的值，
// 面板未渲染时回退到视图自带策略（不做隐式猜测，空值直接拒绝提交）。
function agentTeamCurrentPolicy() {
  const select = elements["team-view"]?.querySelector?.("[data-team-policy]");
  if (select && select.value) return select.value;
  const team = normalizeAgentTeam(agentTeamView);
  return team.orderPolicy;
}

async function loadAgentTeamPresets() {
  if (Array.isArray(agentTeamPresets)) return agentTeamPresets;
  try {
    const presets = await invoke("AgentTeamPresets");
    agentTeamPresets = Array.isArray(presets) ? presets : [];
  } catch (error) {
    agentTeamPresets = [];
    agentTeamError = error?.message || String(error);
  }
  return agentTeamPresets;
}

// loadAgentTeamLibrary 读全局团队库（团队表数据源）；失败不影响员工栏渲染
// （库是可选面：旧宿主没实现时不显示团队表，而不是整块报错）。
async function loadAgentTeamLibrary() {
  try {
    agentTeamLibrary = await invoke("AgentTeamLibrary", "");
  } catch (error) {
    agentTeamLibrary = null;
  }
  return agentTeamLibrary;
}

// loadAgentTeamGlobal 读全局母本（团队库 + 员工库 + 默认顺序 + 会话副本投影）。
// 同样按可选面处理：旧宿主没实现时只隐藏「全局母本」块，不影响员工栏。
async function loadAgentTeamGlobal() {
  try {
    agentTeamGlobal = await invoke("AgentTeamGlobalConfig", "");
  } catch (error) {
    agentTeamGlobal = null;
  }
  return agentTeamGlobal;
}

// refreshAgentTeam 拉取一次员工表 + 团队库 + 全局母本并重绘；force=false 且会话
// 未变时复用上次结果（面板每次 render 都会调用它，避免高频 RPC）。
async function refreshAgentTeam({ force = false } = {}) {
  if (agentTeamLoading) return;
  const sessionID = client.current()?.session?.id || "";
  if (!force && sessionID && sessionID === agentTeamSessionID && agentTeamView) return;
  agentTeamLoading = true;
  try {
    await loadAgentTeamPresets();
    agentTeamView = await invoke("AgentTeamView", "");
    await loadAgentTeamLibrary();
    await loadAgentTeamGlobal();
    agentTeamSessionID = sessionID || agentTeamSessionID;
    agentTeamError = "";
  } catch (error) {
    agentTeamView = null;
    agentTeamError = error?.message || String(error);
  } finally {
    agentTeamLoading = false;
  }
  renderAgentTeamPanel();
}

// renderAgentTeamPanel 只重绘本区块（不触发整页 render，避免与动作互锁）。
// 冷加载面板是"按需注入"的：重绘会清掉它们，所以重绘前先复位面板状态。
function renderAgentTeamPanel() {
  const host = elements["team-view"];
  if (!host) return;
  const failure = agentTeamError
    ? `<div class="team-notice is-error" role="alert">${escapeHtml(agentTeamError)}</div>`
    : "";
  if (!agentTeamView) {
    host.className = "team-view muted";
    host.innerHTML = failure || "展开后加载员工栏与团队库";
    if (elements["team-count"]) elements["team-count"].textContent = "0";
    return;
  }
  const team = normalizeAgentTeam(agentTeamView);
  host.className = "team-view";
  elements["team-count"].textContent = String(team.members.length + team.scheduled.length);
  host.innerHTML = failure + renderAgentTeam(agentTeamView, agentTeamPresets || [], agentTeamLibrary, agentTeamGlobal);
}

// scheduleAgentTeamRefresh 只在状态子页可见且 Agent Team 区块展开时拉取，
// 会话变更后下一次 render 自然刷新（切走/未展开不产生额外请求）。
function scheduleAgentTeamRefresh(snapshot) {
  const section = elements["team-section"];
  if (!section?.open || section.offsetParent === null) return;
  const sessionID = snapshot?.session?.id || "";
  if (sessionID && sessionID === agentTeamSessionID && agentTeamView) return;
  refreshAgentTeam();
}

// runAgentTeamAction 统一处理动作失败：错误既进区块内联提示也进 toast。
async function runAgentTeamAction(action) {
  try {
    await action();
    await refreshAgentTeam({ force: true });
  } catch (error) {
    agentTeamError = error?.message || String(error);
    renderAgentTeamPanel();
    showToast(error);
  }
}

// ── 冷加载面板（入职/修改员工、新建/编辑团队）──────────────────
// 面板默认不渲染：点 + 或「编辑」才把表单注入 slot，关闭键（✕ / 取消 / Esc）
// 清空 slot 并复位 hidden。这样窄右栏的常态是"两张表"，而不是常驻一张长表单。

function agentTeamSlot(name) {
  return elements["team-view"]?.querySelector?.(`[data-team-${name}-slot]`);
}

function closeAgentTeamEditors() {
  for (const name of ["hire", "team"]) {
    const slot = agentTeamSlot(name);
    if (!slot) continue;
    slot.innerHTML = "";
    slot.hidden = true;
  }
}

// openAgentTeamHire 打开入职 / 修改面板。scope=session 落在当前会话的在编员工上，
// scope=library 落在**全局员工库**上（员工库与团队解耦：库里的增删改不依赖团队，
// 也不动任何会话的副本；保存走 AgentTeamSaveEmployee）。
function openAgentTeamHire(roleName = "", scope = "session") {
  const slot = agentTeamSlot("hire");
  if (!slot) return;
  const team = normalizeAgentTeam(agentTeamView);
  const member = scope === "library"
    ? normalizeTeamGlobal(agentTeamGlobal).employees.find(item => item.roleName === roleName) || null
    : team.members.find(item => item.roleName === roleName) || null;
  slot.innerHTML = hirePanel(team, member, scope);
  slot.hidden = false;
  slot.querySelector?.("[data-team-hire-name]")?.focus?.();
}

function openAgentTeamTeamPanel(teamID = "") {
  const slot = agentTeamSlot("team");
  if (!slot) return;
  const library = normalizeTeamLibrary(agentTeamLibrary);
  const entry = library.teams.find(item => item.teamID === teamID) || null;
  slot.innerHTML = teamEditorPanel(normalizeAgentTeam(agentTeamView), entry, agentTeamPresets || []);
  slot.hidden = false;
  slot.querySelector?.("[data-team-form-name]")?.focus?.();
}

elements["team-section"]?.addEventListener("toggle", () => {
  if (elements["team-section"].open) refreshAgentTeam();
});

elements["team-view"]?.addEventListener("click", async event => {
  // 1) 装配内置形态 / 按团队库条目装配。
  const materializeTemplate = event.target.closest?.("[data-team-materialize]");
  if (materializeTemplate?.dataset.teamMaterialize) {
    const kind = materializeTemplate.dataset.teamMaterialize;
    await runAgentTeamAction(() => invoke("AgentTeamMaterialize", "", kind, 0));
    return;
  }
  const materializeTeam = event.target.closest?.("[data-team-materialize-team]");
  if (materializeTeam?.dataset.teamMaterializeTeam) {
    const teamID = materializeTeam.dataset.teamMaterializeTeam;
    await runAgentTeamAction(() => invoke("AgentTeamMaterializeTeam", "", teamID, 0));
    return;
  }
  // 把当前会话在编员工原样存成一支团队（含提示词/权限）：后端从会话注册表取
  // 团队 ID 与顺序，重名按 team_id 幂等覆盖。
  if (event.target.closest?.("[data-team-save-current]")) {
    await runAgentTeamAction(async () => {
      const library = await invoke("AgentTeamSaveCurrentTeam", "", "", "");
      const count = Array.isArray(library?.teams) ? library.teams.length : 0;
      showToast({ message: `已存入团队库（当前 ${count} 支团队）` });
    });
    return;
  }

  // 2) 打开/关闭冷加载面板。
  if (event.target.closest?.("[data-team-editor-close]")) {
    closeAgentTeamEditors();
    return;
  }
  if (event.target.closest?.("[data-team-open-hire]")) {
    openAgentTeamHire("");
    return;
  }
  if (event.target.closest?.("[data-team-open-team]")) {
    openAgentTeamTeamPanel("");
    return;
  }
  const editButton = event.target.closest?.("[data-team-edit]");
  if (editButton?.dataset.teamEdit) {
    openAgentTeamHire(editButton.dataset.teamEdit);
    return;
  }
  const editTeam = event.target.closest?.("[data-team-edit-team]");
  if (editTeam?.dataset.teamEditTeam) {
    openAgentTeamTeamPanel(editTeam.dataset.teamEditTeam);
    return;
  }

  // 3) 团队库动作：存内置形态 / 删条目。
  const saveTemplate = event.target.closest?.("[data-team-save-template]");
  if (saveTemplate?.dataset.teamSaveTemplate) {
    const entry = agentTeamEntryFromPreset(saveTemplate.dataset.teamSaveTemplate);
    if (!entry) return;
    await runAgentTeamAction(() => invoke("AgentTeamSaveTeam", "", entry));
    return;
  }
  const deleteTeam = event.target.closest?.("[data-team-delete-team]");
  if (deleteTeam?.dataset.teamDeleteTeam) {
    const teamID = deleteTeam.dataset.teamDeleteTeam;
    if (!confirm(`确认从团队库删除 ${teamID}？已装配的会话不受影响。`)) return;
    await runAgentTeamAction(() => invoke("AgentTeamDeleteTeam", "", teamID));
    return;
  }

  // 3.5) 全局母本动作：确认普及搭配到全局 / 顺序设为默认 / 删全局员工。
  // 母本是全局粒度事实：只有「确认普及」会把会话副本写回，其余动作都是库管理。
  if (event.target.closest?.("[data-team-publish-global]")) {
    await runAgentTeamAction(async () => {
      const config = await invoke("AgentTeamPublishToGlobal", "", "", "");
      const count = Array.isArray(config?.employees?.employees) ? config.employees.employees.length : 0;
      showToast({ message: `已把当前搭配普及到全局（员工库 ${count} 人）` });
    });
    return;
  }
  if (event.target.closest?.("[data-team-default-order]")) {
    const master = normalizeTeamGlobal(agentTeamGlobal);
    const policy = agentTeamCurrentPolicy() || master.composition.orderPolicy;
    const orderRoles = master.composition.orderRoles;
    if (!policy || !orderRoles.length) return;
    await runAgentTeamAction(async () => {
      await invoke("AgentTeamSetDefaultOrder", "", policy, orderRoles);
      showToast({ message: "已把当前会话顺序设为全局默认顺序" });
    });
    return;
  }
  const deleteEmployee = event.target.closest?.("[data-team-employee-delete]");
  if (deleteEmployee?.dataset.teamEmployeeDelete) {
    const roleName = deleteEmployee.dataset.teamEmployeeDelete;
    if (!confirm(`确认从员工库删除 ${roleName}？已装配会话的副本不受影响。`)) return;
    await runAgentTeamAction(() => invoke("AgentTeamDeleteEmployee", "", roleName));
    return;
  }
  // 员工库的建 / 改：面板落在员工库作用域上（不装配、不动会话副本）。
  if (event.target.closest?.("[data-team-employee-new]")) {
    openAgentTeamHire("", "library");
    return;
  }
  const editEmployee = event.target.closest?.("[data-team-employee-edit]");
  if (editEmployee?.dataset.teamEmployeeEdit) {
    openAgentTeamHire(editEmployee.dataset.teamEmployeeEdit, "library");
    return;
  }

  // 4) 员工会话查看 / 顺序调整（↑↓，与拖拽同一条提交路径）/ 删除。
  const openRole = event.target.closest?.("[data-team-role-open]");
  if (openRole?.dataset.teamRoleOpen) {
    await openRoleSessionDetail(openRole.dataset.teamRoleOpen, openRole.dataset.teamRoleSession);
    return;
  }
  const orderButton = event.target.closest?.("[data-team-action]");
  if (orderButton?.dataset.teamAction && orderButton.dataset.teamRole) {
    const next = nextAgentTeamOrder(agentTeamView, orderButton.dataset.teamAction, orderButton.dataset.teamRole);
    const policy = agentTeamCurrentPolicy();
    if (!next || !policy) return;
    await runAgentTeamAction(() => invoke("AgentTeamSetOrder", "", policy, next.orderRoles));
    return;
  }
  const deleteButton = event.target.closest?.("[data-team-delete]");
  if (deleteButton?.dataset.teamDelete) {
    const roleName = deleteButton.dataset.teamDelete;
    if (!confirm(`确认删除角色 ${roleName}？它会同时从工作顺序里摘除。`)) return;
    await runAgentTeamAction(() => invoke("AgentTeamDeleteRole", "", roleName));
    return;
  }

  // 5) 提示词优化（冷加载面板内）：只产出候选，点「应用到提示词」才写回输入框。
  if (event.target.closest?.("[data-team-optimize]")) {
    await optimizeAgentTeamPrompt();
    return;
  }
  if (event.target.closest?.("[data-team-optimize-apply]")) {
    const slot = agentTeamSlot("hire");
    const optimized = slot?.querySelector?.("[data-team-optimize-text]")?.textContent || "";
    const prompt = slot?.querySelector?.("[data-team-hire-prompt]");
    if (prompt && optimized) prompt.value = optimized;
    return;
  }
  if (event.target.closest?.("[data-team-form-fill-current]")) {
    const team = normalizeAgentTeam(agentTeamView);
    const names = team.members
      .filter(member => !isPinnedRole(member.roleName) && member.roleKind !== "timer")
      .map(member => member.roleName);
    const fields = agentTeamSlot("team")?.querySelector?.("[data-team-form-members]");
    if (fields) fields.value = names.join("\n");
    return;
  }
  const template = event.target.closest?.("[data-team-template]");
  if (template?.dataset.teamTemplate) {
    fillAgentTeamFormFromPreset(template.dataset.teamTemplate);
  }
});

// agentTeamEntryFromPreset 把内置形态投影成团队库条目（用户"存入库"后才持久：
// preset 是代码里的模板，库条目是用户数据，不在读路径上偷偷落盘）。
function agentTeamEntryFromPreset(kind) {
  const preset = (agentTeamPresets || []).find(item => item?.team_kind === kind);
  if (!preset) return null;
  const roles = (Array.isArray(preset.roles) ? preset.roles : [])
    .filter(role => role && typeof role.role_name === "string" && role.role_name)
    .map(role => ({
      role_name: role.role_name,
      role_kind: role.role_kind || "agent",
      system_prompt: role.system_prompt || "",
      tools_policy: role.tools_policy || "",
      model_policy: role.model_policy || "",
      join_policy: role.join_policy || "",
      presence_policy: role.presence_policy || ""
    }));
  return {
    team_id: kind,
    team_kind: kind,
    name: kind,
    order_policy: preset.order_policy || "",
    order_roles: Array.isArray(preset.order_roles) ? preset.order_roles : [],
    roles,
    origin: "preset"
  };
}

// fillAgentTeamFormFromPreset / fillAgentTeamFormFromCurrent 只填表单（不落盘）：
// 用户改完点「新建团队」才写团队库。
function fillAgentTeamFormFromPreset(kind) {
  const slot = agentTeamSlot("team");
  const preset = (agentTeamPresets || []).find(item => item?.team_kind === kind);
  if (!slot || !preset) return;
  const names = (Array.isArray(preset.roles) ? preset.roles : [])
    .map(role => role?.role_name)
    .filter(name => typeof name === "string" && name && !isPinnedRole(name));
  slot.querySelector("[data-team-form-name]").value = kind;
  slot.querySelector("[data-team-form-kind]").value = kind;
  slot.querySelector("[data-team-form-policy]").value = preset.order_policy || "user_main_decided";
  slot.querySelector("[data-team-form-members]").value = names.join("\n");
}

// optimizeAgentTeamPrompt 跑一次有界 LLM 回合优化提示词；结果只渲染成候选
// （应用/放弃由用户点按钮决定，不会自动改输入框）。
async function optimizeAgentTeamPrompt() {
  const slot = agentTeamSlot("hire");
  if (!slot) return;
  const state = slot.querySelector("[data-team-optimize-state]");
  const result = slot.querySelector("[data-team-prompt-result]");
  const roleName = String(slot.querySelector("[data-team-hire-name]")?.value || "").trim();
  const draft = String(slot.querySelector("[data-team-hire-prompt]")?.value || "").trim();
  if (!roleName || !draft) {
    if (state) state.textContent = "先填角色名与提示词再优化";
    return;
  }
  if (state) state.textContent = "优化中…";
  try {
    const optimized = await invoke("AgentTeamOptimizePrompt", "", {
      role_name: roleName,
      role_kind: String(slot.querySelector("[data-team-hire-kind]")?.value || "agent"),
      system_prompt: draft,
      tools_policy: String(slot.querySelector("[data-team-hire-tools]")?.value || "")
    });
    if (state) state.textContent = optimized?.model ? `由 ${optimized.model} 生成` : "";
    if (result) {
      const notes = Array.isArray(optimized?.notes) ? optimized.notes : [];
      result.innerHTML = `<strong>优化候选</strong>${notes.length ? `<div class="muted">${notes.map(escapeHtml).join("；")}</div>` : ""}
        <div class="team-prompt-text" data-team-optimize-text>${escapeHtml(optimized?.optimized || "")}</div>
        <div class="team-editor-actions"><button type="button" class="text-button" data-team-optimize-apply="1">应用到提示词</button></div>`;
      result.hidden = false;
    }
  } catch (error) {
    if (state) state.textContent = error?.message || String(error);
  }
}

// ── 员工栏拖拽调序（发言顺序在员工栏直接调整）────────────────────
// 源 = 行首 ≡（或整行）；落点 = 另一行（插到它之前）或"顺序末尾"落区。落点一定
// 就提交完整顺序表，前端不缓存顺序、不乐观重排。
elements["team-view"]?.addEventListener("dragstart", event => {
  const handle = event.target.closest?.("[data-team-drag]");
  const row = event.target.closest?.("[data-team-staff-role]");
  const roleName = handle?.dataset.teamDrag || row?.dataset.teamStaffRole || "";
  if (!roleName) return;
  agentTeamDragRole = roleName;
  row?.classList.add("is-dragging");
  event.dataTransfer?.setData?.("text/plain", roleName);
  if (event.dataTransfer) event.dataTransfer.effectAllowed = "move";
});

elements["team-view"]?.addEventListener("dragover", event => {
  const row = event.target.closest?.("[data-team-staff-role]");
  const endZone = event.target.closest?.("[data-team-order-drop]");
  if (!row && !endZone) return;
  event.preventDefault();
  if (event.dataTransfer) event.dataTransfer.dropEffect = "move";
  clearAgentTeamDropMarkers(row || endZone);
  (row || endZone).classList.add("is-drop-target");
});

elements["team-view"]?.addEventListener("dragleave", event => {
  const zone = event.target.closest?.("[data-team-staff-role], [data-team-order-drop]");
  if (zone) zone.classList.remove("is-drop-target");
});

elements["team-view"]?.addEventListener("drop", async event => {
  const row = event.target.closest?.("[data-team-staff-role]");
  const endZone = event.target.closest?.("[data-team-order-drop]");
  if (!row && !endZone) return;
  event.preventDefault();
  const source = agentTeamDragRole || event.dataTransfer?.getData?.("text/plain") || "";
  agentTeamDragRole = "";
  clearAgentTeamDropMarkers();
  const target = row?.dataset.teamStaffRole || "";
  const next = agentTeamOrderForDrag(agentTeamView, source, target);
  const policy = agentTeamCurrentPolicy();
  if (!next || !policy) return;
  await runAgentTeamAction(() => invoke("AgentTeamSetOrder", "", policy, next.orderRoles));
});

elements["team-view"]?.addEventListener("dragend", () => {
  agentTeamDragRole = "";
  clearAgentTeamDropMarkers();
  elements["team-view"]?.querySelectorAll?.(".is-dragging").forEach(node => node.classList.remove("is-dragging"));
});

function clearAgentTeamDropMarkers(keep = null) {
  elements["team-view"]?.querySelectorAll?.(".is-drop-target").forEach(node => {
    if (node !== keep) node.classList.remove("is-drop-target");
  });
}

// 员工入职 / 修改：一步到位（建角色会话 + 落注册表含提示词与权限 + 按 join_policy
// 决定是否进顺序），回执里的 notice 直接呈现"谁在什么时候真的会发言"。
elements["team-view"]?.addEventListener("submit", async event => {
  const hireForm = event.target.closest?.("[data-team-hire-form]");
  const teamForm = event.target.closest?.("[data-team-form]");
  if (!hireForm && !teamForm) return;
  event.preventDefault();
  if (hireForm) {
    const roleName = String(hireForm.querySelector("[data-team-hire-name]")?.value || "").trim();
    if (!roleName) return;
    const role = {
      role_name: roleName,
      role_kind: hireForm.querySelector("[data-team-hire-kind]")?.value || "agent",
      join_policy: hireForm.querySelector("[data-team-hire-join]")?.value || "on_team_create",
      tools_policy: String(hireForm.querySelector("[data-team-hire-tools]")?.value || "").trim(),
      model_policy: String(hireForm.querySelector("[data-team-hire-model]")?.value || "").trim(),
      presence_policy: String(hireForm.querySelector("[data-team-hire-presence]")?.value || "").trim(),
      system_prompt: String(hireForm.querySelector("[data-team-hire-prompt]")?.value || "").trim()
    };
    try {
      // 员工库作用域：只写全局事实，不装配、不建角色会话；请求与"入库"同一套字段。
      if (hireForm.dataset.teamHireScope === "library") {
        await invoke("AgentTeamSaveEmployee", "", role);
        showToast({ message: `${roleName} 已存入员工库（全局事实，未装配到会话）` });
        closeAgentTeamEditors();
        await refreshAgentTeam({ force: true });
        return;
      }
      const result = await invoke("AgentTeamInstantiateRole", "", role, 0);
      const notices = Array.isArray(result?.notice) ? result.notice : [];
      if (notices.length) showToast({ message: `${roleName}：${notices.join("；")}` });
      closeAgentTeamEditors();
      await refreshAgentTeam({ force: true });
    } catch (error) {
      agentTeamError = error?.message || String(error);
      renderAgentTeamPanel();
      showToast(error);
    }
    return;
  }
  const entry = agentTeamEntryFromForm(teamForm);
  if (!entry) return;
  await runAgentTeamAction(() => invoke("AgentTeamSaveTeam", "", entry));
});

// agentTeamEntryFromForm 把"新建/编辑团队"表单换算成团队库条目（纯函数）：
// 成员一行一个角色名，发言顺序 = user → main → 成员表顺序。角色配置的细节
// （提示词/权限）在员工栏里逐个编辑，团队库只回答"有谁、什么顺序"。
function agentTeamEntryFromForm(form) {
  const name = String(form.querySelector("[data-team-form-name]")?.value || "").trim();
  if (!name) return null;
  const teamID = String(form.querySelector("[data-team-form-id]")?.value || "").trim() || name;
  const kind = String(form.querySelector("[data-team-form-kind]")?.value || "").trim() || teamID;
  const memberNames = String(form.querySelector("[data-team-form-members]")?.value || "")
    .split(/[\n,，、]/)
    .map(value => value.trim())
    .filter(Boolean)
    .filter(uniqueValue());
  const roles = memberNames.map(roleName => ({ role_name: roleName, role_kind: "agent" }));
  return {
    team_id: teamID,
    team_kind: kind,
    name,
    order_policy: form.querySelector("[data-team-form-policy]")?.value || "",
    order_roles: ["user", "main", ...memberNames],
    roles,
    origin: "custom"
  };
}

function uniqueValue() {
  const seen = new Set();
  return value => {
    if (seen.has(value)) return false;
    seen.add(value);
    return true;
  };
}

elements["team-view"]?.addEventListener("change", async event => {
  const select = event.target.closest?.("[data-team-policy]");
  if (!select?.value) return;
  const team = normalizeAgentTeam(agentTeamView);
  await runAgentTeamAction(() => invoke("AgentTeamSetOrder", "", select.value, team.orderRoles));
});

// openRoleSessionDetail 打开某个角色的独立会话（成员行「查看」）：DS-A2A 里
// EXEC（main）与 ADVISOR（tl）是两个会话，主对话只显示 EXEC 的可见消息，
// 这里按角色身份单独展示该 agent 自己的行，避免两个 agent 都渲染成 AGENT。
async function openRoleSessionDetail(roleName, roleSessionID) {
  const name = String(roleName || "").trim();
  if (!name) return;
  try {
    const snapshot = await invoke("AgentTeamRoleSnapshot", "", name, String(roleSessionID || ""));
    elements["role-session-modal-title"].innerHTML = `<span class="eyebrow">Agent Team · 角色会话</span><h2>${escapeHtml(roleDisplayName(name))}</h2>`;
    elements["role-session-view"].className = "role-session-view";
    elements["role-session-view"].innerHTML = renderRoleSessionDetail(snapshot);
    setModal("role-session-modal", true);
  } catch (error) {
    showToast(error);
  }
}

function closeRoleSessionDetail() {
  setModal("role-session-modal", false);
}

elements["role-session-close"]?.addEventListener("click", closeRoleSessionDetail);
elements["role-session-modal"]?.addEventListener("click", event => {
  if (event.target === elements["role-session-modal"]) closeRoleSessionDetail();
});
document.addEventListener("keydown", event => {
  if (event.key === "Escape") closeRoleSessionDetail();
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

// openNodeDetail 渲染并打开节点详情弹窗（子代理详情页）：三块数据面
// （会话记录 / 上下文 / 功能打点）全部经 invoke("SubagentSessionDetail")
// 拉取；弹窗内不再有任何实时查看通道与流式生命周期。
let activeNodeDetailKey = "";
let activeNodeDetailID = "";
let nodeDetailGeneration = 0;
let nodeDetailRefreshTimer = 0;
let nodeDetailLastSignature = "";

// resolveNodeForDetail 解析详情弹窗的节点数据：优先 Plan DSL（活跃 Plan 的
// 权威投影）；fork 子代理节点在 Plan 已清除时回退到子代理树投影；树同样
// 没到（工作表格行先到：同批次还有子代理在跑，行经 worktable.changed/
// task.changed 到达，而树只由整份快照与 runtime.changed 携带）时用行自身
// 兜底，最后退回"仅身份"的节点——详情入口不再静默失败（2026-09-13 回归）。
// 会话记录/上下文/功能打点始终由 SubagentSessionDetail 数据面承载。
function resolveNodeForDetail(nodeKey) {
  if (!nodeKey) return null;
  const node = lastPlanDsl?.nodes?.find(candidate => candidate.key === nodeKey);
  if (node) return node;
  const treeNode = findSubagentTreeNode(nodeKey);
  if (treeNode) return subagentTreeNodeToDSL(treeNode);
  const row = workTableView.current().find(item => item.source_id === nodeKey || item.id === nodeKey);
  return workItemToDetailNode(row || { id: nodeKey, source_id: nodeKey });
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
  // 上下文查看是它的主诉求。
  if (fromTree) {
    elements["node-detail-content"].querySelector('[data-node-tab="context"]')?.click();
  }
  setModal("node-detail-modal", true);
  await refreshNodeDetail(node.id, generation);
  if (nodeDetailRefreshTimer) window.clearInterval(nodeDetailRefreshTimer);
  // 弹窗打开期间低频权威刷新：三块数据面（会话记录/上下文/功能打点）按内容
  // 签名变化重建，覆盖打开瞬间之后才产生的内容。
  nodeDetailRefreshTimer = window.setInterval(() => {
    if (!activeNodeDetailID || !document.querySelector("[data-node-detail]")) return;
    refreshNodeDetail(activeNodeDetailID, nodeDetailGeneration);
  }, 1500);
}

function refreshOpenNodeDetail() {
  const node = resolveNodeForDetail(activeNodeDetailKey);
  if (!node) return;
  const selectedTab = elements["node-detail-content"].querySelector("[data-node-tab].is-active")?.dataset.nodeTab || "conversation";
  elements["node-detail-content"].innerHTML = renderNodeDetail({ ...node, mode: node.mode || lastPlanDsl?.mode || "plan" });
  bindNodeDetailTabs(elements["node-detail-content"]);
  const selected = elements["node-detail-content"].querySelector(`[data-node-tab="${selectedTab}"]`);
  selected?.click();
  // 快照驱动的弹窗重建会清掉详情面板占位，重建后立即按签名补一次权威
  // 详情（会话/上下文/功能打点）。
  nodeDetailLastSignature = "";
  scheduleNodeDetailRefresh();
}

// refreshNodeDetail 拉取一次子代理详情（三块数据面）并渲染。
async function refreshNodeDetail(nodeID, generation = nodeDetailGeneration) {
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
  const signature = nodeDetailSignature(detail);
  if (signature === nodeDetailLastSignature) return;
  nodeDetailLastSignature = signature;
  setNodeDetailConversation(detail || null);
}

function closeNodeDetail() {
  nodeDetailGeneration += 1;
  activeNodeDetailKey = "";
  activeNodeDetailID = "";
  nodeDetailLastSignature = "";
  if (nodeDetailRefreshTimer) {
    window.clearInterval(nodeDetailRefreshTimer);
    nodeDetailRefreshTimer = 0;
  }
  setModal("node-detail-modal", false);
}

// scheduleNodeDetailRefresh 打开详情后的节流权威刷新（快照驱动弹窗重建后
// 补一次三块数据面；签名不变时跳过 DOM 重建，避免打断阅读）。
function scheduleNodeDetailRefresh() {
  if (!activeNodeDetailID) return;
  const generation = nodeDetailGeneration;
  window.setTimeout(() => {
    if (generation !== nodeDetailGeneration || !activeNodeDetailID) return;
    refreshNodeDetail(activeNodeDetailID, generation);
  }, 700);
}

// nodeDetailSignature 详情内容签名：仅在这三块数据面有实质变化时重建。
function nodeDetailSignature(detail) {
  if (!detail) return "";
  const conversation = detail.conversation || [];
  const last = conversation[conversation.length - 1];
  return JSON.stringify([
    detail.status || "",
    Boolean(detail.running),
    conversation.length,
    last?.content?.length || 0,
    detail.context?.message_count || 0,
    (detail.tool_events || []).length,
    (detail.trace || []).length
  ]);
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
  renderThemePicker();
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

// ── 皮肤（组件库 token 层）────────────────────────────────
// 三层分工见 themes/README.md：组件库给元素基线，styles.css 给 Seelex 外观，
// 皮肤包只覆盖语义 token。颜色事实在皮肤 CSS 里，这里只负责选哪一套。
let themeController = null;

function themeStorage() {
  try {
    return window.localStorage;
  } catch {
    return null; // 隐私模式等：皮肤仍可切换，只是记不住
  }
}

function renderThemePicker() {
  const host = elements["theme-picker"];
  if (!host) return;
  if (!themeController) {
    host.innerHTML = '<span class="muted">皮肤清单未就绪</span>';
    return;
  }
  const current = themeController.current()?.id || "";
  host.innerHTML = themeController.themes.map(theme => {
    const active = theme.id === current;
    const swatches = theme.swatches.map(color => `<i style="background:${escapeHtml(color)}"></i>`).join("");
    return `<button type="button" class="theme-card${active ? " is-active" : ""}" data-theme-id="${escapeHtml(theme.id)}" role="radio" aria-checked="${active ? "true" : "false"}" title="${escapeHtml(theme.description)}">
      <span class="theme-swatches" aria-hidden="true">${swatches}</span>
      <span class="theme-name">${escapeHtml(theme.name)}${active ? '<span class="theme-current">当前</span>' : ""}</span>
      <span class="theme-desc">${escapeHtml(theme.description)}</span>
    </button>`;
  }).join("");
}

async function initialiseTheme() {
  const manifest = await loadThemeManifest(window.fetch.bind(window));
  themeController = createThemeController({ document, storage: themeStorage(), manifest });
  // 启动时套回上次的皮肤：<html data-theme> 与皮肤 <link> 都由控制器决定。
  themeController.apply(themeController.current()?.id);
  renderThemePicker();
}

elements["theme-picker"]?.addEventListener("click", event => {
  const card = event.target.closest("[data-theme-id]");
  if (!card || !themeController) return;
  themeController.apply(card.dataset.themeId);
  renderThemePicker();
});

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

// 建议列表（命令面板 / 内联提示共用一个渲染器）：点击走容器委托，
// 候选数组放 WeakMap（每容器一份，随 DOM 回收，不给闭包留悬挂引用）。
const suggestionLists = new WeakMap();

function renderSuggestionList(container, suggestions, selected, trigger, limit = suggestions.length) {
  const visible = suggestions.slice(0, limit);
  suggestionLists.set(container, { visible, trigger });
  bindSuggestionList(container);
  container.innerHTML = visible.length
    ? visible.map((suggestion, index) => `<button class="command-result ${index === selected ? "selected" : ""}" type="button" data-index="${index}">
      <span class="command-result-icon">${icon(suggestionIcon(suggestion.kind), 14)}</span>
      <span class="command-prefix">${escapeHtml(trigger)}${escapeHtml(suggestion.text)}</span>
      <span class="command-description">${escapeHtml(suggestion.description || "")}</span>
      <span class="command-kind">${escapeHtml(suggestion.kind || "command")}</span>
    </button>`).join("")
    : '<span class="muted list-empty">没有匹配的指令</span>';
}

function bindSuggestionList(container) {
  if (!container || container.dataset.suggestionBound === "1") return;
  container.dataset.suggestionBound = "1";
  container.addEventListener("click", event => {
    const button = event.target?.closest?.("button[data-index]");
    if (!button) return;
    const current = suggestionLists.get(container);
    acceptSuggestion(current?.visible?.[Number(button.dataset.index)], current?.trigger || "");
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
  scheduleComposerSave();
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

// ── 右侧索引（会话内全量用户输入）数据面与回读通道 ─────────────────────
// 刻度来自后端全量索引（Bridge.SessionInputIndex）：只索引用户输入、覆盖整
// 会话（含尚未加载到窗口的早期轮次），且只带摘要不带正文。索引不进 Snapshot，
// 所以按「视图会话 + 已加载窗口形状」做指纹去重：指纹没变不重复拉取，会话已
// 切走的迟到响应直接丢弃（避免旧会话刻度盖到新会话上）。
let inputIndexSignature = "";
let inputIndexInFlight = false;

async function refreshInputIndex(snapshot) {
  const sessionID = snapshot?.session?.id || "";
  if (!sessionID) {
    inputIndexSignature = "";
    conversationView.inputIndex(null);
    return;
  }
  const signature = `${sessionID}:${(snapshot.conversation || []).length}:${snapshot.has_more_history ? 1 : 0}`;
  if (signature === inputIndexSignature || inputIndexInFlight) return;
  inputIndexInFlight = true;
  try {
    const payload = await invoke("SessionInputIndex", sessionID);
    if ((client.current()?.session?.id || "") !== sessionID) return;
    inputIndexSignature = signature;
    conversationView.inputIndex(payload);
  } catch (error) {
    // 索引是增强面：拉取失败退回「窗口内用户行」临时刻度，不打断会话；不记
    // 指纹，下一次整份渲染会重试。
    console.warn("[input-index]", error);
  } finally {
    inputIndexInFlight = false;
  }
}

// locateInputByReadBack 是轮轴的「回读那一页」通道：点击尚未加载的刻度时按
// 窗口步长把更早历史读进前端，并回答「是否真的多读出一页」——返回 false 让
// 轮轴停止空翻（已到最早一页 / 回读失败）。几何与刻度表由 loadOlderHistory
// 内的 refresh 与轮轴自身刷新接手。
async function locateInputByReadBack() {
  const before = (client.current()?.conversation || []).length;
  await loadOlderHistory();
  const after = (client.current()?.conversation || []).length;
  return after > before;
}

// loadOlderHistory 取更早一页（sentinel 自动触发与「加载更早」按钮共用）。
// limit=0 表示「一整窗」：半页会把窗口与页两个尺寸混在一起（翻一次只多出
// 半屏又丢掉半屏），页大小由后端 limits.history_window 单点决定。
async function loadOlderHistory() {
  try { await invoke("LoadMoreHistory", 0); await refresh({ scroll: "anchor" }); }
  catch (error) { showToast(error); }
}

// returnToLatest 从历史浏览回到最新一页（窗口重新贴尾，回看期间的新消息
// 由这次基线一并带回）。
async function returnToLatest() {
  try { await invoke("LoadLatestHistory"); await refresh({ scroll: "bottom" }); }
  catch (error) { showToast(error); }
}

elements["load-history"].addEventListener("click", loadOlderHistory);
elements["latest-history"].addEventListener("click", returnToLatest);

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
  try {
    // BeginNewSession 在返回前已等过一轮会话目录刷新，因此这次重拉的快照
    // 携带权威左侧栏列表（前端不再回填旧列表掩盖异步竞态）。
    await invoke("BeginNewSession");
    await refresh({ scroll: "bottom" });
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

for (const [modalID, close] of [["runtime-modal", closeRuntime], ["command-modal", closeCommandPalette], ["settings-modal", closeSettings], ["scheduled-task-modal", closeScheduledTaskDialog], ["node-detail-modal", closeNodeDetail], ["work-table-modal", closeWorkTable], ["new-session-modal", closeNewSessionModal], ["role-session-modal", closeRoleSessionDetail]]) {
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
    closeFilePreview();
  }
});

// FA toggle
elements["perm-toggle"].addEventListener("click", async function() {
  if (this.classList.contains("is-pending")) return;
  const next = !Boolean(client.current()?.runtime?.full_access);
  this.classList.add("is-pending");
  try {
    // Bridge 返回真正生效的值：直接按它渲染开关，避免快照滞后导致"点了全权仍被拒"。
    const effective = await invoke("SetFullAccess", next);
    renderFullAccessChip(effective);
    await refresh({ scroll: false });
  } catch (error) {
    showToast(error);
  } finally {
    this.classList.remove("is-pending");
  }
});

// renderFullAccessChip 只改开关本身（乐观回执），完整运行时投影仍随后端快照刷新。
function renderFullAccessChip(on) {
  const chip = elements["perm-toggle"];
  if (!chip) return;
  chip.classList.toggle("is-on", Boolean(on));
  chip.innerHTML = on ? `全权 ${icon("check", 12)}` : "全权";
}

// ── 左右栏宽度拖拽 ─────────────────────────────────────────
// ── 左右栏收起（用户需要让会话区占满屏） ────────────────────────────────────
// 状态只写 <html> 的 data-* 与 localStorage：CSS 负责布局，JS 不碰宽度变量，
// 因此和解拖拽调宽（--left-w/--right-w）互不覆盖——收起时记住的原宽度仍在，
// 展开即回到原样。
const LEFT_COLLAPSED_KEY = "seelex.left-panel-collapsed";
const RIGHT_COLLAPSED_KEY = "seelex.right-panel-collapsed";

function applyPanelCollapsed() {
  const root = document.documentElement;
  const left = storageGet(LEFT_COLLAPSED_KEY) === "1";
  const right = storageGet(RIGHT_COLLAPSED_KEY) === "1";
  root.dataset.leftCollapsed = left ? "true" : "false";
  root.dataset.rightCollapsed = right ? "true" : "false";
  elements["toggle-left-panel"]?.setAttribute("aria-pressed", left ? "true" : "false");
  elements["toggle-right-panel"]?.setAttribute("aria-pressed", right ? "true" : "false");
}

function togglePanel(side) {
  const key = side === "left" ? LEFT_COLLAPSED_KEY : RIGHT_COLLAPSED_KEY;
  const current = storageGet(key) === "1";
  storageSet(key, current ? "0" : "1");
  applyPanelCollapsed();
}

elements["toggle-left-panel"]?.addEventListener("click", () => togglePanel("left"));
elements["toggle-right-panel"]?.addEventListener("click", () => togglePanel("right"));
document.addEventListener("keydown", event => {
  if (!(event.ctrlKey || event.metaKey) || event.altKey) return;
  const key = event.key.toLowerCase();
  if (key === "b" && !event.shiftKey) { event.preventDefault(); togglePanel("left"); }
  if (key === "j" && !event.shiftKey) { event.preventDefault(); togglePanel("right"); }
});
applyPanelCollapsed();

const LEFT_WIDTH_KEY = "seelex.left-panel-width";
const RIGHT_WIDTH_KEY = "seelex.right-panel-width";
const LEFT_WIDTH_RANGE = [200, 420];
const RIGHT_WIDTH_RANGE = [220, 480];
const PREVIEW_WIDTH_KEY = "seelex.preview-pane-width";
const PREVIEW_WIDTH_RANGE = [280, 760];

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

// ── 「资源管理器」文件预览抽屉（代码子页左分栏）──────────────
// 预览抽屉是代码子页内部结构：left 预览 / divider / right（工作树+提交记录）。
// 宽度以 CSS 变量 + localStorage 记忆（默认 380px）；展开/收起态也记忆
// （默认收起，点文件自动展开）。关闭只收起不销毁内容，再次打开同一文件
// 直接复用（避免重复读取）。布局两态（收起单列 / 展开三列）由
// .code-split 上的 .is-preview-open 切换（见 styles.css「子页3」注释）。
const FILE_PREVIEW_OPEN_KEY = "seelex.preview-pane-open";

// syncPreviewLayout 同步代码子页两态布局：展开=三列（preview/divider/panes），
// 收起=单列（panes 占满，防止预览抽屉收起后内容被裁成空白）。
function syncPreviewLayout(open) {
  const split = document.getElementById("code-split");
  if (split) split.classList.toggle("is-preview-open", Boolean(open));
}

function openFilePreview(entry) {
  if (!entry || !entry.path) return;
  const snapshot = client.current();
  previewRoot = snapshot?.current_workspace?.root_path || previewRoot;
  previewPaneOpen = true; // open must flip the state flag, otherwise closeFilePreview guard always returns and the X button never closes
  const pane = elements["file-preview-pane"];
  if (pane) {
    pane.classList.remove("is-closed");
    syncPreviewLayout(true);
    document.documentElement.style.setProperty("--preview-w", previewPaneWidth());
  }
  try { window.localStorage.setItem(FILE_PREVIEW_OPEN_KEY, "1"); } catch { /* 无存储环境忽略 */ }
  filePreviewController.open(entry);
  // 资源管理器可能停靠在主视图或右栏：无论当前在哪，打开文件预览前
  // 先让该子页成为所在栏的激活页。
  revealView("code");
}

function closeFilePreview() {
  const pane = elements["file-preview-pane"];
  if (!previewPaneOpen && (!pane || pane.classList.contains("is-closed"))) return;
  previewPaneOpen = false;
  if (pane) {
    pane.classList.add("is-closed");
    syncPreviewLayout(false);
  }
  try { window.localStorage.setItem(FILE_PREVIEW_OPEN_KEY, "0"); } catch { /* 无存储环境忽略 */ }
  filePreviewController.clear();
}

function previewPaneWidth() {
  const stored = Number(storageGet(PREVIEW_WIDTH_KEY));
  const width = Number.isFinite(stored) ? clampPanelWidth(stored, ...PREVIEW_WIDTH_RANGE) : 380;
  return `${width}px`;
}

function setupFilePreviewResize() {
  applyPreviewWidth();
  const divider = elements["file-preview-divider"];
  if (!divider) return;
  const pane = elements["file-preview-pane"];
  const split = document.getElementById("code-split");
  if (!pane || !split) return;

  divider.addEventListener("pointerdown", event => {
    if (event.button !== 0) return;
    event.preventDefault();
    divider.classList.add("is-dragging");
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    const move = moveEvent => {
      const rect = split.getBoundingClientRect();
      const width = clampPanelWidth(moveEvent.clientX - rect.left, ...PREVIEW_WIDTH_RANGE);
      document.documentElement.style.setProperty("--preview-w", `${width}px`);
    };
    const up = () => {
      divider.classList.remove("is-dragging");
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      storageSet(PREVIEW_WIDTH_KEY, String(parseFloat(document.documentElement.style.getPropertyValue("--preview-w")) || 380));
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  });
  divider.addEventListener("keydown", event => {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    const current = parseFloat(document.documentElement.style.getPropertyValue("--preview-w")) || 380;
    const step = event.key === "ArrowLeft" ? -24 : 24;
    const width = clampPanelWidth(current + step, ...PREVIEW_WIDTH_RANGE);
    document.documentElement.style.setProperty("--preview-w", `${width}px`);
    storageSet(PREVIEW_WIDTH_KEY, String(width));
  });
}

function applyPreviewWidth() {
  const stored = Number(storageGet(PREVIEW_WIDTH_KEY));
  const width = Number.isFinite(stored) ? clampPanelWidth(stored, ...PREVIEW_WIDTH_RANGE) : 380;
  document.documentElement.style.setProperty("--preview-w", `${width}px`);
}

// 初始化：展开/收起记忆（默认收起）+ 宽度记忆；代码子页激活时惰性刷新
// git log（原有行为）。
(function initFilePreviewPane() {
  applyPreviewWidth();
  const stored = (() => { try { return window.localStorage.getItem(FILE_PREVIEW_OPEN_KEY); } catch { return null; } })();
  previewPaneOpen = stored === "1";
  const pane = elements["file-preview-pane"];
  if (pane) {
    if (!previewPaneOpen) {
      pane.classList.add("is-closed");
      syncPreviewLayout(false);
    } else {
      pane.classList.remove("is-closed");
      syncPreviewLayout(true);
      const snapshot = client.current();
      previewRoot = snapshot?.current_workspace?.root_path || "";
    }
  }
})();
setupFilePreviewResize();

function resizePrompt() {
  elements.prompt.style.height = "auto";
  elements.prompt.style.height = `${Math.min(elements.prompt.scrollHeight, 180)}px`;
}

async function initialise() {
  try {
    hydrateIcons();
    applyDockState();
    await initialiseTheme();
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
