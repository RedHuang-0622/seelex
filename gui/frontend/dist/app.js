import { escapeHtml, hydrateIcons, icon, queueMoveTarget } from "./components.js";
import { createChatView } from "./chat-view.js";
import { createGUIClient } from "./client-state.js";
import { clearSubmittedText, composerSubmitPlan, composerViewSwitch, isComposingEnter, shouldRestoreDraft } from "./composer-input.js";
import { createConversationView } from "./conversation-view.js";
import { createTrajectoryView } from "./trajectory-view.js";
import { buildTrajectory } from "./trajectory.js";
import { createEffortControl } from "./effort-control.js";
import { permissionTierCatalog, permissionTierChip, permissionTierChipForRuntime, permissionTierMenuItems, permissionTierOptions, nextTierIndex } from "./permission-tier.js";
import {
  planToDSL, renderNodeDetail, setNodeDetailConversation, bindNodeDetailTabs, subagentTreeNodeToDSL, workItemToDetailNode
} from "./plan-dsl.js";
import { createWorkTableView, countUnread, workTableSignatures } from "./work-table.js";
import { createWorkTreeView } from "./worktree-view.js";
import { createGitLogView } from "./git-log-view.js";
import { createWorkspaceChangesView } from "./workspace-changes.js";
import { createFilePreviewController } from "./file-preview.js";
import { renderCompactionFrameModal, renderContextCompactions } from "./context-summary.js";
import { compactionRangeText, compactionReasonLabel, mergeCompactionProgress } from "./compaction-format.js";
import { renderGoalInFlight, renderGoalStack } from "./goal-stack-view.js";
import { createRuntimeEventBinder } from "./runtime-events.js";
import { renderScheduledTasks, renderScheduledTasksTable } from "./scheduled-tasks-view.js";
import { agentTeamOrderForDrag, employeePool, hirePanel, isPinnedRole, nextAgentTeamOrder, normalizeAgentTeam, normalizeTeamGlobal, normalizeTeamLibrary, PERMISSION_CUSTOM_TOOLS, PERMISSION_GROUPS, PERMISSION_BITS, renderAgentTeam, renderRoleSessionDetail, renderTeamMemberList, roleDisplayName, teamEditorPanel, teamMemberNames } from "./agent-team-view.js";
import { renderHistorySearchResults } from "./history-search.js";
import { createThemeController, loadThemeManifest } from "./theme.js";
import {
  bindHorizontalDragDelegate,
  bindHorizontalWheelDelegate,
  bindScrollShadows,
  prefersReducedMotion,
  rollNumber,
  syncAllScrollShadows
} from "./motion.js";
import { flashResizePill, hideResizePill, showResizePill } from "./resize-pill.js";
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
import {
  EXPLORER_PAGES,
  EXPLORER_PAGE_META,
  EXPLORER_STORAGE_KEY,
  LEGACY_PANE_ORDER_KEY,
  isExplorerPage,
  normalizeExplorerState,
  resolveExplorerState,
  serializeExplorerState,
  withExplorerPage
} from "./explorer-pages.js";
import { REFRESH_STATUS, createExplorerRefresh } from "./explorer-refresh.js";
import { createPerfHooks } from "./perf-hooks.js";
import { createLiveDiag } from "./live-diag.js";
import { createTerminalPanel } from "./terminal-panel.js";

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
  // 顶栏两个诊断徽标宿主：缺了它们，下面 `if (elements["perf-badge-host"])` 永远为假，
  // 徽标会静默不渲染（性能/事件诊断数据照采，但用户看不到，见 element-registry.test.mjs）。
  "perf-badge-host", "live-diag-host",
  "session-list", "session-count", "new-session",
  "plugin-list", "plugin-count", "account-list", "account-count", "conversation", "conversation-tabs", "trajectory",
  "empty-state", "composer", "prompt", "composer-status", "stop-button", "send-button",
  "runtime-details", "effort-control", "effort-range", "effort-value", "work-section", "work-count", "work-unread", "work-table-open", "work-table-summary", "work-table-modal", "work-table-modal-close", "work-table-modal-view", "scheduled-task-section", "scheduled-task-view", "scheduled-task-count", "new-scheduled-task", "scheduled-task-modal", "scheduled-task-close", "sched-name", "sched-kind", "sched-mode", "sched-period-value", "sched-period-unit", "sched-period-field", "sched-datetime", "sched-datetime-field", "sched-command", "sched-command-field", "sched-prompt", "sched-prompt-field", "sched-enabled", "sched-enabled-field", "sched-submit", "history-search-section", "history-search-form", "history-search-input", "history-search-view", "history-search-count", "skill-list", "history-bar",
  "project-name", "project-root", "project-status", "worktree-view", "file-count", "context-compactions",
  "compaction-frame-modal", "compaction-frame-modal-close", "compaction-frame-modal-title", "compaction-frame-modal-meta", "compaction-frame-modal-view",
  "team-section", "team-view", "team-count",
  "role-session-modal", "role-session-close", "role-session-modal-title", "role-session-view",
  "right-tabs", "goal-section", "goal-badge", "goal-view", "code-panes", "code-pane-tabs", "code-pane-worktree", "code-pane-gitlog", "git-log-view", "git-log-count", "code-pane-changes", "changes-view", "changes-count",
  "file-preview-pane", "file-preview-view", "file-preview-tabs", "file-preview-hide-panes", "file-preview-close", "file-preview-divider", "file-preview-collapse", "file-preview-rail",
  "runtime-button", "runtime-modal", "runtime-close", "settings-button", "settings-modal", "settings-close", "storage-backend", "storage-path", "storage-path-field", "storage-dsn", "storage-dsn-field", "storage-test", "storage-save", "storage-status", "terminal-scrollback", "theme-picker", "mode-picker", "inline-suggestions",
  "command-button", "command-modal", "command-close", "command-triggers", "command-search", "command-results",
  "load-history", "latest-history", "interaction-modal", "perm-toggle", "perm-menu", "interaction-risk", "interaction-title", "permission-tier-list",
  "new-session-modal", "new-session-close", "new-session-task", "new-session-workspace", "new-session-back", "new-session-workspace-list", "new-session-pick-folder", "new-session-step-1", "new-session-step-2",
  "scheduled-table-modal", "scheduled-table-close", "scheduled-table-open", "scheduled-table-summary", "scheduled-table-view",
  "interaction-question", "interaction-preview", "interaction-options",
  "node-detail-modal", "node-detail-close", "node-detail-title", "node-detail-content", "toast", "ui-tooltip",
  "toggle-left-panel", "toggle-right-panel",
  "terminal-panel", "terminal-body", "terminal-tabs", "terminal-resize", "terminal-collapse",
  "terminal-new", "terminal-close", "terminal-hide", "terminal-button"
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
      snapshot.session?.status === "restoring",
      Boolean(state.resumingSessionID),
      composerDraftPageText(snapshot),
      snapshot.task?.context_compactions || []
    );
    chatView.renderControls(snapshot, Boolean(state.resumingSessionID));
    return;
  }
  if (view === "trajectory") {
    if (snapshot) renderTrajectory(snapshot, true);
    refreshPromptInjection();
    return;
  }
  if (view === "code") {
    // 「资源管理器」子页激活（含由非激活变激活、以及重新点开已激活的页签）：
    // 整批刷新三个数据面。不能只在首次激活拉一次——工作区内容会随回合变化。
    refreshExplorerPages(EXPLORER_PAGES);
  }
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
  if (dockState.active[region] === view) {
    // 再次点击已激活的页签 = 用户明确「重新打开」：资源管理器要重拉数据面
    // （首次切换已有 runViewActivation 那条路；这条覆盖"点同一个页签"）。
    if (view === "code") refreshExplorerPages(EXPLORER_PAGES);
    return;
  }
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
      // 门禁进度按会话路由投递：跟着视图走的那条进度条属于上一个会话的折叠，
      // 切过来还挂着就是把别的会话的压缩说成当前会话的。权威快照随后重绘面板。
      dropCompactionProgress();
    }
    render(snapshot, options);
  },
  onIncremental: renderIncrementalBuffered,
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
const workTableView = createWorkTableView(elements["work-table-modal-view"], {
  // 会话筛选轴（「仅本会话」）按**当前视图会话**取值：表格是跨会话台账，
  // 会话切换后 render 会重跑，用函数取值避免取到创建视图时的旧会话。
  viewSessionID: () => client.current()?.session?.id || ""
});
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
// 工作区更改面板：行点击复用同一个文件详情抽屉（已删除的文件不可点）。
const workspaceChangesView = createWorkspaceChangesView(elements["changes-view"], {
  onOpenFile: entry => openFilePreview(entry)
});
// 文件预览（「资源管理器」子页左抽屉）：工作树文件点击 → 后端读取受控字节
// （containment/敏感过滤/上限在 workspace 层保证）→ 按类型分派渲染。
// 容器是「多文件详情」：每个文件一枚上标 chip + 一个独立面板；最后一个 chip
// 关闭（容器为空）时回调 onEmpty → 抽屉收起、子页恢复原来大小（工作树/提交
// 记录重新占满）。
const filePreviewController = createFilePreviewController({
  view: elements["file-preview-view"],
  tabsHost: elements["file-preview-tabs"],
  loader: async (entry, kind, limit) => invoke("WorkspaceFileContent", entry.path, limit),
  onError: showToast,
  onEmpty: () => closeFilePreview()
});
let previewPaneOpen = false;
let previewPanesHidden = false;
// previewCollapsed = 详情抽屉收成竖轨、内容页（工作树/提交记录）独占子页。
let previewCollapsed = false;
let previewRoot = "";
elements["file-preview-close"].addEventListener("click", closeFilePreview);
elements["file-preview-hide-panes"].addEventListener("click", togglePanesHidden);
// 「收起详情，让出内容页」：抽屉收成竖轨（内容页独占），竖轨本身是展开入口。
elements["file-preview-collapse"].addEventListener("click", togglePreviewCollapsed);
elements["file-preview-rail"].addEventListener("click", togglePreviewCollapsed);
// workTableSeen 是“已读”快照（status|retry_count 签名）；workTableOpen
// 控制弹窗打开期间不显示未读角标。
let workTableSeen = new Map();
let workTableOpen = false;
// worktreeRoot 是三个数据面当前所属的工作区 root（也是刷新机的提交基准）；
// worktreeFileCount 是递归文件统计；lastChatRunning 用于在 chat 结束（文件/
// 提交/改动都可能变化）时整批刷新一次。
let worktreeRoot = "";
let worktreeFileCount = null;
let lastChatRunning = false;
// promptLayersCache 是轨迹视图"前缀注入"的本地缓存（后端 PromptLayers
// 桥接数据，不进 Snapshot；打开轨迹子页时刷新）。
let promptLayersCache = null;
// 账户栏的最近一次 runtime：供应商切换是纯前端视图切换，不必为它再拉一份快照。
let lastAccountsRuntime = {};
let composerDirty = false;
// composerStash / composerSessionID：输入框正文按会话归属的本地留存表与"当前
// 归属的视图会话"（规则见 composer-input.js composerViewSwitch）。留存表只装
// 未发送正文（有上限），因此切走再切回来时属于该会话的字还在，而它绝不会跟着
// 视图跑到别的会话去被提交。
let composerStash = new Map();
let composerSessionID = "";
// composerComposing 跟踪输入法合成态（compositionstart/end）：合成中的 Enter
// 是确认候选词，不是发送。
let composerComposing = false;
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

// markComposerEdited 记录一次本地编辑：置脏并按需防抖落盘。程序化写入
// （召回排队消息、接受建议）之后也要走它，否则这些内容会被当成"没编辑过"。
function markComposerEdited() {
  composerDirty = true;
}

// syncComposerSession 在整份快照渲染时把「输入框正文 ↔ 视图会话」对齐：正文按
// 会话归属（规则集中在 composer-input.js `composerViewSwitch`）。
//
// 存在理由（用户报告）：一个会话运行中（A），用户在它的输入框里写了插话、或撤回
// 了一条排队消息，接着切到一个**没在运行**的会话（B）继续干活。此前输入框正文不
// 按会话归属，A 的字跟着视图留在框里，于是按 Enter 时它被当成 B 的内容提交出去
// （`composerSubmitPlan` 只认当前视图会话）——"运行中会话污染了空闲会话的输入框
// 内容提交"。同一处脏位还会挡住 B 自己的草稿回填（`shouldRestoreDraft` 要求非脏），
// 于是 B 的输入框显示的反而是 A 的字。
//
// 只在会话 ID 真的变了时动手（同一会话的整份渲染/事件密集期是 no-op），且必须
// 先于 restoreComposerDraft：归属清楚之后，草稿会话的正文回填才有正确的脏位前提。
function syncComposerSession(snapshot) {
  const sessionID = snapshot?.session?.id || "";
  const result = composerViewSwitch({
    fromSessionID: composerSessionID,
    toSessionID: sessionID,
    current: elements.prompt.value,
    dirty: composerDirty,
    stash: composerStash
  });
  composerStash = result.stash;
  if (!result.switched) return;
  composerSessionID = sessionID;
  composerDirty = result.dirty;
  if (elements.prompt.value !== result.text) {
    elements.prompt.value = result.text;
    resizePrompt();
  }
  // 正文换了归属：上一个会话的内联建议（命令/插件/技能/团队前缀）已不适用。
  hideInlineSuggestions();
}

// restoreComposerDraft 在整份快照渲染时把后端恢复的草稿正文回填输入框：
// 仅在"没有本地未落盘输入"（未聚焦、非脏）时才回填，避免后端旧副本覆盖
// 用户刚敲的内容（判据集中在 composer-input.js）。
function restoreComposerDraft(snapshot) {
  if (!shouldRestoreDraft({
    draft: Boolean(snapshot?.session?.draft),
    snapshotComposer: snapshot?.session?.composer,
    current: elements.prompt.value,
    focused: document.activeElement === elements.prompt,
    dirty: composerDirty
  })) return;
  elements.prompt.value = snapshot.session.composer;
  resizePrompt();
  elements.prompt.setSelectionRange(elements.prompt.value.length, elements.prompt.value.length);
}

// composerDraftPageText 已撤回：未发送输入不再作为页面 context 里的草稿行出现
// （draft 语义改由「未完成会话」承担）。保留该函数只为调用点稳定，恒返回空串。
function composerDraftPageText() {
  return "";
}

function render(snapshot, options = {}) {
  const started = performance.now();
  // 输入框正文先按会话归属对齐，再谈回填（顺序见 syncComposerSession）。
  syncComposerSession(snapshot);
  restoreComposerDraft(snapshot);
  renderSessions(snapshot.sessions || [], snapshot.session || {}, snapshot.capabilities || {}, snapshot.session_workspaces || {}, snapshot.workspaces || []);
  renderProject(snapshot);
  renderRuntime(snapshot.runtime || {});
  renderPlugins(snapshot.runtime || {});
  renderAccounts(snapshot.runtime || {});
  chatView.render(snapshot, options.scrollMode, Boolean(state.resumingSessionID), composerDraftPageText(snapshot));
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
  // 限高滚动块刚被重绘：补一次边缘阴影（增量/滚动期间由委托监听维护）。
  refreshScrollShadows();
  perfHooks.markRender(performance.now() - started);
}

function renderIncremental(snapshot, kind, payload) {
  if (!snapshot) return;
  const started = performance.now();
  if (["message.added", "message.delta", "tool.started", "tool.completed"].includes(kind)) {
    chatView.renderConversation(snapshot.conversation || [], snapshot.chat || {}, "auto", snapshot.has_more_history, snapshot.session?.status === "restoring", Boolean(state.resumingSessionID), composerDraftPageText(snapshot), snapshot.task?.context_compactions || []);
    chatView.renderControls(snapshot, Boolean(state.resumingSessionID));
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
  if (kind === "compaction.progress") {
    // 门禁进度逐帧落地：它不进快照，若跟表格三类一起进 120ms 尾随合并，中间关
    // 会被 latest-wins 丢掉，进度条就只剩"开始/结束"两帧——而"走到哪一关了"
    // 正是这条事件唯一的信息。
    applyCompactionProgress(payload);
    repaintCompactions(currentCompactions());
    perfHooks.markRender(performance.now() - started);
    return;
  }
  if (["subagent.changed", "subagent.tool.started", "subagent.tool.completed"].includes(kind)) {
    refreshPlanDetailData(snapshot.runtime?.plan, snapshot.runtime?.subagent_tree);
    if (activeNodeDetailKey) refreshOpenNodeDetail();
    return;
  }
  if (kind === "interaction.opened" || kind === "interaction.closed") renderInteraction(snapshot.interaction);
  if (kind === "team.changed") {
    // 团队面板的数据不在快照里（按需 RPC 拉取），这条事件只带来"变了"：作废面板
    // 缓存，可见时立刻重取。发声方是后端装配面（MaterializeAgentTeam 等），因此
    // `@` 召唤、goal 自动装配与面板 RPC 一键装配都走同一条路，前端不再从自己的
    // 输入文本里猜"这次提交会不会改团队"。
    invalidateAgentTeam();
  }
}

// ── 表格三轴事件的尾随合并（缓冲）────────────────────────────
// runtime.changed / worktable.changed / task.changed 三条事件都把工作表格整块
// 重绘一遍（批次条重排、变更行 replaceWith、新行插入带动滚动位置跳变）。任务
// 突发时（子代理每状态迁移都打点）逐条重绘在视觉上就是"表格总在跳"。这里做
// ~120ms 尾随合并：窗口内只重绘一次，且始终用最新快照（latest-wins）——渲染是
// 快照的纯函数，丢掉中间态不会丢信息。
//
// 消息/工具/交互/团队/压缩门禁类事件不合并：流式增量必须逐帧落地，团队面板走自己的
// 失效重取路径，compaction.progress 的中间关一旦按 latest-wins 合并就再也看不见
// "走到哪一关"。合并只改变"什么时候重绘"，不改变快照应用与回执水位
// （reportAppliedEvents 仍在 client-state 里逐事件推进）。
const BUFFERED_INCREMENTAL_KINDS = new Set(["runtime.changed", "worktable.changed", "task.changed"]);
let bufferedIncrementalSnapshot = null;
let bufferedIncrementalKinds = new Set();
let bufferedIncrementalTimer = null;

function renderIncrementalBuffered(snapshot, kind, payload) {
  if (!BUFFERED_INCREMENTAL_KINDS.has(kind)) {
    renderIncremental(snapshot, kind, payload);
    return;
  }
  bufferedIncrementalSnapshot = snapshot;
  bufferedIncrementalKinds.add(kind);
  if (bufferedIncrementalTimer !== null) return;
  bufferedIncrementalTimer = window.setTimeout(flushBufferedIncrementals, 120);
}

function flushBufferedIncrementals() {
  bufferedIncrementalTimer = null;
  const snapshot = bufferedIncrementalSnapshot;
  const kinds = bufferedIncrementalKinds;
  bufferedIncrementalSnapshot = null;
  bufferedIncrementalKinds = new Set();
  if (!snapshot) return;
  // 单次重绘取并集里最强的一类：runtime.changed ⊇ task.changed ⊇ worktable.changed
  // （都含 plan 详情 + 工作表格；runtime 那一支还带账户/插件/技能/项目面）。
  const kind = kinds.has("runtime.changed")
    ? "runtime.changed"
    : kinds.has("task.changed") ? "task.changed" : "worktable.changed";
  renderIncremental(snapshot, kind);
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
  markComposerEdited();
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

// ── 子页3「资源管理器」内部：三个平级子页 + 原子刷新 ──────────────
// 「工作树 / 提交记录 / 工作区更改」是同一子页内的三个平级子页：页签切换、激活态
// 高亮，顺序与激活落盘在 seelex.right.explorer.v1（旧版三面板堆叠的拖拽顺序记忆
// seelex.right.codePanes 一次性迁移，收敛逻辑见 explorer-pages.js）。
// 三个数据面都是只读元数据（Bridge.WorkspaceTree/FileCount、WorkspaceGitLog、
// WorkspaceChanges），刷新统一走 explorerRefresh：同一时刻只有一次在飞请求，结果按
// 「工作区根 + 代次」整批提交，过期响应直接丢弃（见 explorer-refresh.js）。
// explorerLoadedPages 记哪些子页已经有数据面：刷新失败时这些子页保留旧数据只提示。
const explorerLoadedPages = new Set();

const explorerRefresh = createExplorerRefresh({
  load: loadExplorerPage,
  commit: commitExplorerPages,
  onError: reportExplorerRefreshFailure
});

let explorerState = readExplorerState();

// readExplorerState 读取子页状态：新键优先；缺新键时消费旧版面板顺序（含更早的
// 两项版本——长度不匹配即安全回退默认顺序），消费过就写回新键并清掉旧键，旧值不再
// 留在存储里充当第二种事实。
function readExplorerState() {
  const { state, migrated } = resolveExplorerState(
    storageGet(EXPLORER_STORAGE_KEY),
    storageGet(LEGACY_PANE_ORDER_KEY)
  );
  if (migrated) {
    storageSet(EXPLORER_STORAGE_KEY, serializeExplorerState(state));
    storageRemove(LEGACY_PANE_ORDER_KEY);
  }
  return state;
}

function persistExplorerState() {
  storageSet(EXPLORER_STORAGE_KEY, serializeExplorerState(explorerState));
}

// renderExplorerTabs 渲染子页页签：顺序即 explorerState.order（旧顺序记忆迁移过来
// 的结果也走这里），激活页高亮并标记 aria-selected。
function renderExplorerTabs() {
  const tabs = elements["code-pane-tabs"];
  if (!tabs) return;
  tabs.innerHTML = explorerState.order.map(page => {
    const meta = EXPLORER_PAGE_META[page];
    const active = page === explorerState.active;
    return `<button class="code-pane-tab${active ? " is-active" : ""}" type="button" role="tab" `
      + `data-explorer-page="${page}" aria-controls="${meta.panelId}" aria-selected="${String(active)}">`
      + `${escapeHtml(meta.label)}</button>`;
  }).join("");
}

// applyExplorerPageState 是子页显隐的唯一入口：一次只显示激活子页的面板（内容区
// 自己滚动）。切换子页只动显隐，不触发任何 Bridge 调用。
function applyExplorerPageState() {
  explorerState = normalizeExplorerState(explorerState);
  renderExplorerTabs();
  for (const page of EXPLORER_PAGES) {
    elements[EXPLORER_PAGE_META[page].panelId]?.classList.toggle("is-hidden", page !== explorerState.active);
  }
}

function setExplorerPage(page) {
  if (!isExplorerPage(page) || page === explorerState.active) return;
  explorerState = withExplorerPage(explorerState, page);
  applyExplorerPageState();
  persistExplorerState();
}

// 子页内的点击走容器委托（一条监听）：页签切换 + 各子页头部的刷新按钮。页签是真
// 按钮，Enter/Space 由浏览器转成 click，不必另写键盘分支。
elements["code-panes"]?.addEventListener("click", event => {
  const refreshButton = event.target.closest?.("[data-page-refresh]");
  if (refreshButton) {
    refreshExplorerPages([refreshButton.dataset.pageRefresh]);
    return;
  }
  const tab = event.target.closest?.("[data-explorer-page]");
  if (tab) setExplorerPage(tab.dataset.explorerPage);
});

applyExplorerPageState();

// ── 资源管理器数据面：刷新入口 / 拉取 / 整批提交 / 失败提示 ──────────
// refreshExplorerPages 是资源管理器数据面的唯一刷新入口：缺省 = 三个子页一起刷
// （激活或重新点开子页）；单个子页的刷新按钮只刷它自己。未绑定工作区时不发请求。
function refreshExplorerPages(pages = EXPLORER_PAGES) {
  const list = (Array.isArray(pages) ? pages : [pages]).filter(isExplorerPage);
  if (list.length === 0 || !explorerRefresh.currentRoot()) {
    return Promise.resolve({ status: REFRESH_STATUS.NOOP, pages: [] });
  }
  markExplorerPagesLoading(list);
  return explorerRefresh.refresh(list);
}

// markExplorerPagesLoading 只在子页还没有数据面时显示「读取中」：已有数据的子页在
// 刷新期间保留旧内容与旧计数，不闪空。
function markExplorerPagesLoading(pages) {
  for (const page of pages) {
    if (explorerLoadedPages.has(page)) continue;
    elements[EXPLORER_PAGE_META[page].badgeId].textContent = "…";
  }
}

// loadExplorerPage 拉取单个子页的只读元数据面：工作树一次取统计 + 根层列表，两笔
// 请求同属一个代次（不会一半新一半旧）。失败交给刷新机整批作废（旧数据保留）。
function loadExplorerPage(page) {
  if (page === "worktree") {
    return Promise.all([invoke("WorkspaceFileCount"), invoke("WorkspaceTree", "", 1)])
      .then(([count, listing]) => ({ count, listing }));
  }
  if (page === "gitlog") return invoke("WorkspaceGitLog", 20);
  if (page === "changes") return invoke("WorkspaceChanges", 200);
  return Promise.reject(new Error(`未知的资源管理器子页：${page}`));
}

// commitExplorerPages 整批提交（刷新机只在根与代次都仍有效时调用）：三个面板要么
// 一起换成新一代次的数据，要么一个都不动。文本一律由 view 模块 escapeHtml，
// 前端不解释 git 语义——状态字母、分类与统计全部来自 Bridge 的下发结果。
function commitExplorerPages(entries) {
  for (const { page, data } of entries) {
    if (page === "worktree") {
      worktreeFileCount = data?.count ?? null;
      elements["file-count"].textContent = String(data?.count?.files ?? 0);
      workTreeView.renderRoot(data?.listing?.entries || []);
    } else if (page === "gitlog") {
      gitLogView.renderRoot(data);
      elements["git-log-count"].textContent = String(data?.commits?.length ?? 0);
    } else if (page === "changes") {
      workspaceChangesView.renderRoot(data);
      // badge 用后端的总数（过滤后全部条目）：截断时列表短、数字仍是真的。
      elements["changes-count"].textContent = String(data?.total ?? data?.entries?.length ?? 0);
    }
    explorerLoadedPages.add(page);
  }
  // 文件数进「状态」子页的项目状态表：工作树提交后重算一次，两处保持同代次。
  if (entries.some(entry => entry.page === "worktree")) {
    const snapshot = client.current();
    if (snapshot) renderProjectStatus(snapshot, Boolean(snapshot.chat?.running));
  }
}

// reportExplorerRefreshFailure 失败提示：整批结果被丢弃，三个面板保留旧数据，只
// 提示一次；从未成功加载过的子页回落到空态与 0 计数（不残留「…」）。
function reportExplorerRefreshFailure(error, info) {
  showToast(error);
  for (const page of info.pages) {
    if (explorerLoadedPages.has(page)) continue;
    const meta = EXPLORER_PAGE_META[page];
    elements[meta.badgeId].textContent = "0";
    const view = elements[meta.viewId];
    if (!view) continue;
    view.classList.add("muted");
    view.textContent = meta.failedHint;
  }
}

// ── 上下文压缩条目：展开查看折叠帧正文 ────────────────────────
// 帧正文不进快照（快照只带 frame_ref），展开时按 ref 分页读回；展开与分页都是
// 本地 UI 状态。容器与分页组件与轨迹详情同一套（.axis-detail +
// data-compact-frame-load），不自造第二套面板。
function emptyCompactionDetail() {
  return { index: -1, loading: false, error: "", text: "", hasMore: false, nextOffset: 0, totalBytes: 0 };
}

let compactionDetail = emptyCompactionDetail();
let contextCompactionsBound = false;

// ── 压缩门禁进度（一轮压缩的瞬态）────────────────────────────
// 后端每收一关发一条 compaction.progress，终局（done/failed）恰好一条，本轮
// 进度生命周期即结束。载荷不进快照（同 team.changed 口径），所以"现在走到哪
// 一关"只活在视图侧这份暂存里：终局后保留片刻让人读完结论（含逐关耗时清单），
// 再自动撤条。
//
// 累计逻辑在 compaction-format.mergeCompactionProgress（纯函数，node 测试直接
// 覆盖）；这里是薄薄一层"存 + 定时撤条"。
let compactionProgress = null;
let compactionProgressTimer = null;
const COMPACTION_PROGRESS_HOLD_MS = 2500;
const COMPACTION_PROGRESS_FAILED_HOLD_MS = 6000;

// applyCompactionProgress 收一帧门禁进度（显式压缩还会有一条起手帧，见后端
// event.CompactionPhaseBegin）：按下回车立刻有反馈，不必等判据估算跑完。
function applyCompactionProgress(payload) {
  const next = mergeCompactionProgress(compactionProgress, payload);
  if (!next || next === compactionProgress) return;
  compactionProgress = next;
  if (compactionProgressTimer !== null) {
    window.clearTimeout(compactionProgressTimer);
    compactionProgressTimer = null;
  }
  if (next.state !== "running") {
    const hold = next.state === "failed" ? COMPACTION_PROGRESS_FAILED_HOLD_MS : COMPACTION_PROGRESS_HOLD_MS;
    compactionProgressTimer = window.setTimeout(() => {
      compactionProgressTimer = null;
      dropCompactionProgress();
      repaintCompactions(currentCompactions());
    }, hold);
  }
}

// dropCompactionProgress 只作废状态与定时器，不重绘：调用方随后会自己重绘
// （会话切换由权威快照的 render 负责，此处再画一次会读到切换中的旧记录）。
function dropCompactionProgress() {
  if (compactionProgressTimer !== null) {
    window.clearTimeout(compactionProgressTimer);
    compactionProgressTimer = null;
  }
  compactionProgress = null;
}

function bindContextCompactions() {
  const host = elements["context-compactions"];
  if (contextCompactionsBound || !host) return;
  contextCompactionsBound = true;
  host.addEventListener("click", onContextCompactionsClick);
}

function currentCompactions() {
  const compactions = client.current()?.task?.context_compactions;
  return Array.isArray(compactions) ? compactions : [];
}

// onContextCompactionsClick 一条委托监听：data-compact-open 展开/收起条目，
// data-compact-frame-load=first|more 首读/续读帧正文（重绘前先落状态，避免闪烁）。
async function onContextCompactionsClick(event) {
  const openButton = event.target?.closest?.("[data-compact-open]");
  const pageButton = event.target?.closest?.("[data-compact-frame-load]");
  if (!openButton && !pageButton) return;
  const compactions = currentCompactions();
  if (openButton) {
    const index = Number(openButton.dataset.compactOpen);
    if (compactionDetail.index === index) {
      compactionDetail = emptyCompactionDetail();
      repaintCompactions(compactions);
      return;
    }
    compactionDetail = { ...emptyCompactionDetail(), index };
    repaintCompactions(compactions);
    const ref = String(compactions[index]?.frame_ref || "");
    if (ref) await loadCompactionFrame(ref, 0, index, "");
    repaintCompactions(compactions);
    return;
  }
  const index = compactionDetail.index;
  const ref = String(compactions[index]?.frame_ref || "");
  if (!ref) return;
  const mode = pageButton.dataset.compactFrameLoad;
  const offset = mode === "more" ? Number(compactionDetail.nextOffset || 0) : 0;
  if (mode === "more" && !(offset > 0)) return;
  await loadCompactionFrame(ref, offset, index, mode === "more" ? compactionDetail.text : "");
  repaintCompactions(compactions);
}

// loadCompactionFrame 按 ref 分页读取折叠帧正文。失败只更新条目内的错误文案
// （用户就在这里，不再弹全局提示）；不改变展开状态本身。
async function loadCompactionFrame(ref, offset, index, previousText) {
  const base = compactionDetail;
  compactionDetail = {
    index, loading: true, error: "", text: String(previousText || ""),
    hasMore: base.hasMore, nextOffset: Number(base.nextOffset || 0), totalBytes: Number(base.totalBytes || 0)
  };
  repaintCompactions(currentCompactions());
  try {
    const page = await invoke("ToolResultContent", ref, offset, 12000);
    compactionDetail = {
      index, loading: false, error: "",
      text: String(previousText || "") + String(page?.content || ""),
      hasMore: Boolean(page?.has_more), nextOffset: Number(page?.next_offset || 0),
      totalBytes: Number(page?.total_bytes || 0)
    };
  } catch (error) {
    compactionDetail = { ...compactionDetail, index, loading: false, error: String(error) };
  }
}

// repaintCompactions 是右栏「上下文压缩」面板的唯一出口：记录列表 + 本轮门禁
// 进度条一起画，可见性判据必须同源——折叠发生在写记录之前，"零记录"时面板
// 仍可能有一轮压缩正在跑，按记录数判隐藏会让第一次折叠看不到进度条。
//
// 块本身挂在 状态/概要 折叠区**里面**（用户口径：压缩内容放概要下面），折叠区默认
// 收起——所以"本轮正在压 / 刚压完"这条瞬态要自己把折叠区打开（见 revealStatusPanel）；
// 历次记录不触发打开：那是用户展开 状态 才读的静态事实。
function repaintCompactions(compactions) {
  const host = elements["context-compactions"];
  if (!host) return;
  const list = Array.isArray(compactions) ? compactions : [];
  host.innerHTML = renderContextCompactions(list, { detail: compactionDetail, progress: compactionProgress });
  host.classList.toggle("hidden", list.length === 0 && !compactionProgress);
  if (compactionProgress) revealStatusPanel();
}

// revealStatusPanel 把 状态 折叠区打开（只开不收）：不这么做，块一挪进折叠区，
// "按下回车到底动没动"又回到"用户得先想起去展开 状态"的老问题。
function revealStatusPanel() {
  const panel = document.getElementById("status-panel");
  if (panel && !panel.open) panel.open = true;
}

// ── 折叠帧正文弹框 ───────────────────────────────────────────
// 帧正文入口（data-compact-frame-ref）出现在三处：右栏「上下文压缩」条目、对话区
// 「以上已折叠」分界行、以及将来任何一处——委托因此挂在 document 上，一处接住所有
// 入口，组件侧只携带 ref（不持有 invoke 依赖）。
//
// 弹框与右栏展开共用同一份正文区（renderCompactionFrameModal → renderFrameDetail），
// 因此不存在"同一个 ref 两种读法"。状态只在视图侧（不进快照）：快照刷新会重建记录
// 数组，但弹框里的正文是独立读回来的，不受影响。
let compactionFrameModal = { record: null, detail: null };
// 读取是异步的：期间可能又开了另一条记录、点了重试、或关掉弹框。只有最后一次请求的
// 结果可以落到弹框上——否则先发的响应回来会把新正文盖掉（同会话切换里的过期响应）。
let compactionFrameLoadToken = 0;

function emptyCompactionFrameDetail(previousText = "") {
  return { loading: true, error: "", text: String(previousText || ""), hasMore: false, nextOffset: 0, totalBytes: 0 };
}

// currentCompactionRecord 按 frame_ref 在当前快照的压缩记录里找回那条记录（弹框只
// 存 ref 的话，记录被刷新换掉就画不出元数据与大小）。
function currentCompactionRecord(frameRef) {
  const ref = String(frameRef || "");
  if (!ref) return null;
  return currentCompactions().find(item => String(item?.frame_ref || "") === ref) || null;
}

// openCompactionFrame 打开弹框并按 ref 读第一页正文。记录找不到时也照样打开：弹框
// 里明确说"没有帧正文引用"，比点了没反应好。
async function openCompactionFrame(frameRef) {
  const ref = String(frameRef || "");
  const record = currentCompactionRecord(ref);
  compactionFrameModal = { record, detail: ref ? emptyCompactionFrameDetail() : null };
  repaintCompactionFrameModal();
  setModal("compaction-frame-modal", true);
  if (record && ref) await loadCompactionFramePage(ref, 0, false);
}

function closeCompactionFrame() {
  compactionFrameLoadToken += 1;
  setModal("compaction-frame-modal", false);
  compactionFrameModal = { record: null, detail: null };
}

function repaintCompactionFrameModal() {
  const view = elements["compaction-frame-modal-view"];
  if (!view) return;
  const record = compactionFrameModal.record || {};
  const version = Number(record.version || 0);
  const range = compactionRangeText(record);
  const facts = [
    range,
    compactionReasonLabel(record.reason),
    record.compacted_at ? String(record.compacted_at) : ""
  ].filter(Boolean).join(" · ");
  if (elements["compaction-frame-modal-title"]) {
    elements["compaction-frame-modal-title"].textContent = version > 0 ? `折叠帧正文 · 压缩 #${version}` : "折叠帧正文";
  }
  if (elements["compaction-frame-modal-meta"]) elements["compaction-frame-modal-meta"].textContent = facts;
  view.innerHTML = renderCompactionFrameModal({ record, detail: compactionFrameModal.detail });
}

// loadCompactionFramePage 按 ref 分页读取帧正文（offset=0 首读，append=true 续读）。
async function loadCompactionFramePage(frameRef, offset, append) {
  const ref = String(frameRef || "");
  if (!ref) return;
  const token = ++compactionFrameLoadToken;
  const previous = append ? String(compactionFrameModal.detail?.text || "") : "";
  const totalBytes = Number(compactionFrameModal.detail?.totalBytes || 0);
  compactionFrameModal.detail = { loading: true, error: "", text: previous, hasMore: false, nextOffset: Number(offset) || 0, totalBytes };
  repaintCompactionFrameModal();
  let next = null;
  try {
    const page = await invoke("ToolResultContent", ref, offset, 12000);
    next = {
      loading: false, error: "",
      text: previous + String(page?.content || ""),
      hasMore: Boolean(page?.has_more), nextOffset: Number(page?.next_offset || 0),
      totalBytes: Number(page?.total_bytes || 0)
    };
  } catch (error) {
    next = { ...compactionFrameModal.detail, loading: false, error: String(error) };
  }
  // 过期响应直接丢掉：弹框这时可能已经关了、或已经指向另一条记录。
  if (token !== compactionFrameLoadToken) return;
  compactionFrameModal.detail = next;
  repaintCompactionFrameModal();
}

document.addEventListener("click", event => {
  const trigger = event.target?.closest?.("[data-compact-frame-ref]");
  if (!trigger) return;
  openCompactionFrame(String(trigger.dataset.compactFrameRef || ""));
});

elements["compaction-frame-modal-view"]?.addEventListener("click", event => {
  const page = event.target?.closest?.("[data-compact-frame-load]");
  const ref = String(compactionFrameModal.record?.frame_ref || "");
  if (!page || !ref) return;
  if (page.dataset.compactFrameLoad === "more") {
    const offset = Number(compactionFrameModal.detail?.nextOffset || 0);
    if (!(offset > 0)) return;
    loadCompactionFramePage(ref, offset, true);
    return;
  }
  loadCompactionFramePage(ref, 0, false);
});

elements["compaction-frame-modal-close"]?.addEventListener("click", closeCompactionFrame);

function renderProject(snapshot) {
  const workspace = snapshot.current_workspace || null;
  const task = snapshot.task || null;
  const running = Boolean(snapshot.chat?.running);
  const compactions = task?.context_compactions || [];
  elements["project-name"].textContent = workspace?.name || "No project selected";
  elements["project-root"].textContent = workspace?.root_path || "";
  renderProjectStatus(snapshot, running);
  // 概要区不再放那句英文作用域说明：用户口径是「概要有且仅有压缩栈表格」，而它说的
  // 两件事（哪个工作区、能不能跑）分别由项目名/根路径与上面的状态行承载。
  repaintCompactions(compactions);
  bindContextCompactions();
  syncExplorerData(snapshot, running);
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

// syncExplorerData 收敛资源管理器三个数据面的刷新时机：
// - 绑定工作区后根变了：旧根的数据面立即失效并清空，再按新根整批重拉；
// - 一轮 chat 结束（文件/提交/改动都可能变）：整批重拉一次；
// - 同一根、同一状态重复 render：不重复拉（render 每次快照到达都会跑）。
// 子页未激活也照常刷新——数据面是缓存，激活时会再整批刷新一次（runViewActivation）。
function syncExplorerData(snapshot, running) {
  const rootPath = snapshot.current_workspace?.root_path || "";
  // 预览的文件属于旧工作区时：抽屉内容失效，随数据面一起清空。
  if (previewPaneOpen && rootPath !== previewRoot) closeFilePreview();
  const chatFinished = lastChatRunning && !running;
  lastChatRunning = running;
  if (!rootPath) {
    if (worktreeRoot || explorerRefresh.currentRoot()) {
      worktreeRoot = "";
      explorerRefresh.setRoot("");
      resetExplorerData();
    }
    return;
  }
  const rootChanged = rootPath !== worktreeRoot;
  if (!rootChanged && !chatFinished) return;
  worktreeRoot = rootPath;
  explorerRefresh.setRoot(rootPath);
  if (rootChanged) resetExplorerData();
  refreshExplorerPages(EXPLORER_PAGES);
}

// resetExplorerData 把三个面板一起清空（未绑定工作区 / 工作区已切换）：旧根的内容
// 绝不留在面板上与新一代次的数据混搭。计数归零、工作树空态文案复位（worktree-view
// 的 reset 只清内部状态，容器文案由这里给）。
function resetExplorerData() {
  explorerLoadedPages.clear();
  worktreeFileCount = null;
  workTreeView.reset();
  gitLogView.reset();
  workspaceChangesView.reset();
  elements["file-count"].textContent = "0";
  elements["git-log-count"].textContent = "0";
  elements["changes-count"].textContent = "0";
  const view = elements["worktree-view"];
  if (view) {
    view.classList.add("muted");
    view.textContent = "绑定工作区后显示项目文件树";
  }
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
  // ⋯ 浮层菜单挂在 body 上，重绘后要按新的 ⋯ 按钮重新贴位（关了就清掉）。
  syncSessionMenu();
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
  await dispatchSessionListAction(button.dataset || {});
}

// dispatchSessionListAction 按 data-* 键分派会话列表动作：列表本体与 ⋯ 浮层菜单
// （挂在 body 上）共用这一份分派，两个入口不会各自漂移。
async function dispatchSessionListAction(data) {
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
  // 输入区锁（切换在途）：后端视图指针已随 ResumeSession 移动，而渲染层要等
  // 权威快照才渲染到 restoring 空壳——这个窗口里提交会落到看不见/没装载完的
  // 会话。显式重渲一次 composer，让锁在置位那一刻生效（不是等下一次快照）。
  chatView.renderControls(client.current(), true);
  rerenderSessions();
  let failureMessage = "";
  try {
    await invoke("ResumeSession", sessionID);
    await refresh({ scroll: "bottom" });
  } catch (error) {
    failureMessage = `恢复会话失败：${error?.message || String(error)}`;
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
      // 解锁输入区：以最新快照重算（若后端仍在 restoring，renderControls 会
      // 继续锁着——锁是「切换在途 ∨ 目标 restoring」的单调 OR）。
      chatView.renderControls(latest);
      // 失败文案写在解锁之后：renderControls 会重写 composer-status。
      if (failureMessage) elements["composer-status"].textContent = failureMessage;
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
  if (event.target?.closest?.(".session-more, #session-menu")) return; // ⋯ 段与浮层菜单内部自己处理
  closeSessionMenu();
});
// 浮层菜单挂在 body 上（不在 #session-list 里），点击要单独分派给同一套动作：
// 先把菜单收掉（免得 confirm 弹窗期间它还悬着），再执行动作。
document.addEventListener("click", event => {
  const item = event.target?.closest?.("#session-menu button");
  if (!item || item.disabled) return;
  const data = { ...item.dataset };
  closeSessionMenu();
  void dispatchSessionListAction(data);
});
// 菜单是"贴住 ⋯ 按钮"的浮层：滚动/缩放后位置会失效，直接关掉比跟错位置稳。
document.addEventListener("scroll", () => closeSessionMenu(), true);
window.addEventListener("resize", () => {
  closeSessionMenu();
  // 侧栏宽度上限按视口动态计算：窗口变小要把已存宽度收回到可用范围，避免
  // 超出容器把主视图挤成 0（内容详情宽度由 CSS 封顶，无需在这里收回）。
  applyPanelWidths();
});

// sessionRow 渲染一条会话条目。条目刻意分成两段（用户口径）：
//   标题段：状态点 + 完整标题（CSS 省略号截断），**不在条目里放时间/ token**；
//   ⋯ 段：省略号按钮常驻在行尾，点开是一个**浮层选项菜单**（见 syncSessionMenu），
//          不再向右撑出三个按钮。
// 时间与 token 只在鼠标常驻（或键盘聚焦）时随完整标题一起出现在共享提示气泡里
// ——data-tip 的第一行是完整标题，第二行是「时间 · tokens」。
function sessionRow(session, currentID, nameIndex = 1) {
  // 保留的"新建会话"草稿槽位已撤回：未发送输入不再在会话列表里占一行
  // （draft 语义改由「未完成会话」承担）；无 ID 的行直接不渲染。
  if (session.id === "") return "";
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
      <button class="session-more-toggle" type="button" data-session-more="${escapeHtml(session.id)}" aria-expanded="${menuOpen}" aria-haspopup="menu" aria-label="更多操作">${icon("more", 14)}</button>
    </span>
  </div>`;
}

// ── ⋯ 浮层菜单（选项面板）────────────────────────────────────
// 菜单挂在 body 上（position: fixed）：左栏是可滚动容器，行内绝对定位会被裁掉。
// 菜单项与行内动作同源（同一批 data-* 键），点击复用 onSessionListClick 的分派。

function sessionMenuHTML(sessionID) {
  const pinned = Boolean(sessionMetaByID(sessionID)?.pinned);
  return `<button type="button" class="session-menu-item" role="menuitem" data-pin-session="${escapeHtml(sessionID)}">${icon(pinned ? "star-outline" : "star", 13)}${pinned ? "取消置顶" : "置顶会话"}</button>
    <button type="button" class="session-menu-item" role="menuitem" data-fork="${escapeHtml(sessionID)}">${icon("branch", 13)}分支出新会话</button>
    <div class="session-menu-sep" role="separator"></div>
    <button type="button" class="session-menu-item is-danger" role="menuitem" data-session-del="${escapeHtml(sessionID)}">${icon("close", 13)}删除会话</button>`;
}

// syncSessionMenu 让浮层菜单与 state.openSessionMenu 保持一致（开 / 关 / 跟随重绘）。
function syncSessionMenu() {
  const sessionID = state.openSessionMenu;
  const menus = document.querySelectorAll("#session-menu");
  if (!sessionID || !elements["session-list"]) {
    menus.forEach(node => node.remove());
    return;
  }
  const toggle = elements["session-list"].querySelector(`[data-session-more="${CSS.escape(sessionID)}"]`);
  if (!toggle) {
    menus.forEach(node => node.remove());
    return;
  }
  const menu = menus[0] || document.createElement("div");
  menu.id = "session-menu";
  menu.className = "session-menu";
  menu.setAttribute("role", "menu");
  menu.setAttribute("aria-label", "会话操作");
  menu.innerHTML = sessionMenuHTML(sessionID);
  if (!menus.length) document.body.appendChild(menu);
  positionSessionMenu(menu, toggle);
}

// positionSessionMenu 把菜单贴在 ⋯ 按钮旁：默认下方右对齐；下方放不下就翻到上方，
// 左右都夹在视口内。
function positionSessionMenu(menu, toggle) {
  const anchor = toggle.getBoundingClientRect();
  menu.style.visibility = "hidden";
  menu.style.top = "0px";
  menu.style.left = "0px";
  const box = menu.getBoundingClientRect();
  const gap = 4;
  let top = anchor.bottom + gap;
  if (top + box.height > window.innerHeight - 8) top = Math.max(8, anchor.top - box.height - gap);
  let left = anchor.right - box.width;
  if (left < 8) left = 8;
  if (left + box.width > window.innerWidth - 8) left = Math.max(8, window.innerWidth - box.width - 8);
  menu.style.top = `${Math.round(top)}px`;
  menu.style.left = `${Math.round(left)}px`;
  menu.style.visibility = "";
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

  renderPermissionTier(runtime);

  effortControl.setLevel(runtime.effort);
}

// permissionTierCatalog / currentPermissionTier 已抽到 ./permission-tier.js（纯函数，
// 由 node --test 直接钉住）。这里只做 DOM 写入。
//
// renderPermissionTier 渲染权限档：composer 芯片显示当前档短名（全权档保留 ✓），
// **芯片本身就是切换入口**——点它就地展开下拉（见下方 togglePermissionMenu），
// 一步切档；运行状态弹窗里的档位列表仍在，两处同源（permissionTierMenuItems），
// 所以不会出现"下拉一份、弹窗一份"的漂移。
function renderPermissionTier(runtime) {
  const chipModel = permissionTierChipForRuntime(runtime);
  const chip = elements["perm-toggle"];
  if (chip) {
    chip.dataset.tier = chipModel.id;
    chip.classList.toggle("is-on", chipModel.isFull);
    chip.innerHTML = permissionChipInner(chipModel);
  }
  renderPermissionMenu(runtime);
  const host = elements["permission-tier-list"];
  if (!host) return;
  host.innerHTML = permissionTierOptions(runtime).map(item => `
    <button type="button" class="tier-option ${item.active ? "is-active" : ""}" data-tier="${escapeHtml(item.id)}" role="radio" aria-checked="${item.active}">
      <span class="tier-option-label">${escapeHtml(item.label)}</span>
      <span class="tier-option-desc">${escapeHtml(item.description)}</span>
    </button>`).join("");
}

// permissionChipInner 是芯片内容：当前档短名（全权档缀 ✓）+ 下拉箭头（可点开）。
function permissionChipInner(model) {
  const short = escapeHtml(model.short);
  const check = model.isFull ? ` ${icon("check", 12)}` : "";
  return `${short}${check} <span class="perm-caret" aria-hidden="true">▾</span>`;
}

// renderPermissionMenu 把档位条目写进芯片下拉。模型来自纯函数 permissionTierMenuItems
// （后端目录 + 本会话生效档），这里只做 DOM 写入与转义；条目的选中态由后端真值决定，
// 前端不自己记一份"我以为选了什么"。
function renderPermissionMenu(runtime) {
  const menu = elements["perm-menu"];
  if (!menu) return;
  menu.innerHTML = permissionTierMenuItems(runtime).map(item => `
    <button type="button" class="perm-menu-item ${item.active ? "is-active" : ""}" data-tier="${escapeHtml(item.id)}" role="menuitemradio" aria-checked="${item.active}" title="${escapeHtml(item.description)}">
      <span class="perm-menu-label">${escapeHtml(item.label)}</span>
      <span class="perm-menu-mark">${item.active ? icon("check", 12) : ""}</span>
      <span class="perm-menu-desc">${escapeHtml(item.description)}</span>
    </button>`).join("");
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
    stopGoalInFlightPoller(view);
    return;
  }
  // 活动栈分块：会话里嵌套压栈时，栈下目标也要能逐帧查看（不只是栈顶一帧）。
  const stackLine = governance ? renderGoalStack(governance.stack) : "";
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
  view.innerHTML = `${stackLine}${goalLine}${taskLine}${governanceLine}${chips}`;
  if (governance) {
    startGoalInFlightPoller(view, governance);
  } else {
    stopGoalInFlightPoller(view);
  }
}

// renderGoalGovernance 渲染「目标 + 治理」只读面板：goal 状态/轮次/座次/
// TL 最近指令/本轮治理未完成/断环横幅（governance 视图来自
// runtime.goal_governance，goal 栈不入模型上下文）。
//
// 面板上**没有墙钟推断**：只说后端给的事实。「治理没在推进」以前由前端用
// heartbeat_at + 10s 猜成 governance stalled，于是"空闲等你输入"与"回合被中止"
// 印成同一句话；现在回合失败由协调器登记成 round_error，这里直接渲染它。
function renderGoalGovernance(governance) {
  const status = escapeHtml(governance.status || "active");
  const round = Number.isFinite(governance.round) ? governance.round : 0;
  const seat = governance.current_seat ? escapeHtml(governance.current_seat) : "";
  const peer = governance.peer_state ? escapeHtml(governance.peer_state) : "";
  const directive = governance.last_directive
    ? `<div class="goal-gov-directive" title="${escapeHtml(governance.last_directive)}">TL: ${escapeHtml(truncateGoalText(governance.last_directive, 160))}</div>`
    : "";
  const roundError = governance.round_error
    ? `<div class="goal-gov-error" title="${escapeHtml(governance.round_error)}">本轮治理未完成: ${escapeHtml(truncateGoalText(governance.round_error, 160))}</div>`
    : "";
  const broken = governance.broken
    ? `<div class="goal-gov-broken">断环: ${escapeHtml(governance.break_reason || "已收束")}</div>`
    : "";
  // 进行中的 ADVISOR 正文（只读快照）：评审期间有，回合结束即清空。
  const inFlight = renderGoalInFlight(governance);
  const meta = [
    `<span class="goal-gov-status">${status}</span>`,
    `Round ${round}`,
    seat ? `座次 ${seat}` : "",
    peer ? `peer ${peer}` : "",
  ].filter(Boolean).join(" · ");
  return `<div class="goal-governance"><div class="goal-gov-meta">${meta}</div>${inFlight}${directive}${roundError}${broken}</div>`;
}

// refreshGoalInFlight 在 ADVISOR 回合进行中按节拍补一次只读快照：治理回合是
// **同步**跑完的（后端在回合结束才推一次状态），所以进行中正文必须靠轮询快照
// 才能及时渲染出来。只在 peer=evaluating 或已有进行中正文时拉取，避免平时空转。
//
// 方向性（重要）：只 invoke("Snapshot") 读，绝不回写——后端持有 goal 状态与
// team work 前缀的唯一真值，前端渲染结果不回传（否则前缀会与后端帧账本分叉）。
async function refreshGoalInFlight(view, governance) {
  if (!view || !view.isConnected) return;
  const peer = String(governance?.peer_state || "");
  const hasInFlight = Boolean(governance?.in_flight) || Number(governance?.in_flight_chars || 0) > 0;
  if (!hasInFlight && peer !== "evaluating") return;
  try {
    const snapshot = await invoke("Snapshot");
    renderGoal(snapshot);
  } catch {
    // 拉取失败只放弃这一次补渲染：下一次 tick 会重试，不动已有面板内容。
  }
}

// startGoalInFlightPoller 评审进行中的快照轮询节拍：治理回合是**同步**跑完的
// （后端在回合结束才推一次状态），进行中正文只能靠轮询快照才及时可见。
//
// 这里不做任何时间判断——是否真的去拉快照由 refreshGoalInFlight 按后端字段
// （peer_state / in_flight）决定；旧实现同处还兼着 heartbeat 墙钟比较，把
// "空闲等你输入"印成过 governance stalled。
function startGoalInFlightPoller(view, governance) {
  const timer = view.__goalInFlightTimer;
  if (timer) clearInterval(timer);
  // 每次渲染都会重建本定时器，因此闭包里的 governance 始终是最新一份。
  view.__goalInFlightTimer = setInterval(() => void refreshGoalInFlight(view, governance), 1000);
  void refreshGoalInFlight(view, governance);
}

function stopGoalInFlightPoller(view) {
  if (!view || !view.__goalInFlightTimer) return;
  clearInterval(view.__goalInFlightTimer);
  delete view.__goalInFlightTimer;
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
  // 计数滚动（拟物里程表）：只在"同一个计数换了值"时滚动，首次渲染/文案不同
  // 时直接落值（规则见 motion.js countParts/rollNumber）。
  rollNumber(elements["work-count"], String(normalized.length));
  elements["work-table-summary"].textContent = `${normalized.length} 项任务`;
  elements["work-section"]?.classList.toggle("hidden", normalized.length === 0);
  const unread = workTableOpen ? 0 : countUnread(normalized, workTableSeen);
  elements["work-unread"]?.classList.toggle("hidden", unread === 0);
  if (elements["work-unread"]) rollNumber(elements["work-unread"], `${unread} 未读`);
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
// agentTeamDirty 记录"后端会话团队事实变过、本地面板数据可能过期"：置位后下一次
// render/展开一定重取（而不是复用按会话缓存的旧成员表），但不清屏——旧数据先留在
// 屏幕上，新数据回来再整体重绘。置位源只有后端的 team.changed 事件。
let agentTeamDirty = false;
// agentTeamDragRole 是"正在被拖拽的员工"：拖拽只在内部状态里过渡，落点一确定就
// 提交整表（没有乐观重排、没有第二份顺序事实）。agentTeamDragSource 记录拖拽来源
// （library / staff / member），落点决定这次拖拽是写会话顺序还是改团队草稿。
let agentTeamDragRole = "";
let agentTeamDragSource = "";

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
// 未变、也没有待作废的通告（agentTeamDirty）时复用上次结果（面板每次 render 都会
// 调用它，避免高频 RPC）。
async function refreshAgentTeam({ force = false } = {}) {
  if (agentTeamLoading) return;
  const sessionID = client.current()?.session?.id || "";
  if (!force && !agentTeamDirty && sessionID && sessionID === agentTeamSessionID && agentTeamView) return;
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
    // 本次已经按"最新事实"取过一次：无论成败都消掉脏位，否则每个 render 都会
    // 再发一轮 RPC（流式回合里 render 很密）。取数期间到达的新通告会重新置位。
    agentTeamDirty = false;
    agentTeamLoading = false;
  }
  renderAgentTeamPanel();
}

// invalidateAgentTeam 作废 Agent Team 面板缓存：后端会话团队事实变了（装配/工作
// 顺序/入职/编辑成员），而面板缓存的是上一次 RPC 读回的成员表——不置脏位的话，
// 展开面板只会复用旧数据（面板按会话键命中缓存就早退）。
//
// 只在面板真的看得到时立刻取；收起或状态子页不可见时只置脏位，等下一次 render /
// 展开再取（不可见的面板不该产生请求）。
function invalidateAgentTeam() {
  agentTeamDirty = true;
  const section = elements["team-section"];
  if (section?.open && section.offsetParent !== null) refreshAgentTeam({ force: true });
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

// scheduleAgentTeamRefresh 只在状态子页可见且 Agent Team 区块展开时拉取，会话变更
// 或收到 team.changed（agentTeamDirty）后下一次 render 自然刷新（切走/未展开不产生
// 额外请求）。
function scheduleAgentTeamRefresh(snapshot) {
  const section = elements["team-section"];
  if (!section?.open || section.offsetParent === null) return;
  const sessionID = snapshot?.session?.id || "";
  if (!agentTeamDirty && sessionID && sessionID === agentTeamSessionID && agentTeamView) return;
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

// openAgentTeamHire 打开入职 / 修改面板。scope=library 落在**全局员工库**上（员工库与
// 团队解耦：库里的增删改不依赖团队，也不动任何会话的副本；保存走 AgentTeamSaveEmployee）——
// 员工库的「新建员工」与「修改」两条入口都走它。scope=session 只服务员工栏的「+ 入职」
// （空角色名 = 新装配一个人；填已存在的角色名即幂等覆盖本会话那份副本——员工栏不再有
// 单独的「编辑」入口，改档案的唯一编辑入口是员工库）。
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
  const team = normalizeAgentTeam(agentTeamView);
  slot.innerHTML = teamEditorPanel(team, entry, agentTeamPresets || [], agentTeamEmployeePool(team));
  slot.hidden = false;
  slot.querySelector?.("[data-team-form-name]")?.focus?.();
}

// agentTeamEmployeePool 取面板里的"可用员工"清单（员工库 ∪ 本会话在编）：拖拽、
// 团队面板的成员候选、入库动作共用这一份口径。
function agentTeamEmployeePool(team = normalizeAgentTeam(agentTeamView)) {
  return employeePool(normalizeTeamGlobal(agentTeamGlobal), team);
}

// agentTeamRolePayload 把矩阵里的一行换算成后端 RoleSpec 载荷（入库 / 入职共用）。
function agentTeamRolePayload(roleName) {
  const found = agentTeamEmployeePool().find(role => role.roleName === roleName);
  if (!found) return null;
  const payload = {
    role_name: found.roleName,
    role_kind: found.roleKind || "agent",
    join_policy: found.joinPolicy || "on_team_create",
    tools_policy: found.toolsPolicy || "",
    model_policy: found.modelPolicy || "",
    presence_policy: found.presencePolicy || "",
    system_prompt: found.systemPrompt || ""
  };
  // 逐格装配的权限要跟着角色一起走：入库/入职是"把这个员工搬过去"，只带档位不带
  // 格子 = 搬过去的人被降级成档位默认（静默丢权限）。
  if (found.permissionGroups) payload.permission_groups = found.permissionGroups;
  return payload;
}

// ── 团队面板里的成员表（草稿，保存才落盘）────────────────────
// 行序 = 发言顺序；动作全是本地 DOM 编辑，团队表单提交时按行序序列化。

function teamMemberListNames(list) {
  if (!list) return [];
  return [...list.querySelectorAll("[data-team-member-item]")].map(node => node.dataset.teamMemberItem);
}

// teamMemberListFilter 重排 / 增删成员表：keep(name) 决定保留谁，append 追加到末尾。
function teamMemberListFilter(keep, append = "") {
  const slot = agentTeamSlot("team");
  const list = slot?.querySelector?.("[data-team-member-list]");
  if (!list) return;
  const names = teamMemberListNames(list).filter(keep);
  if (append && !names.includes(append)) names.push(append);
  writeTeamMemberList(list, names, slot);
}

// writeTeamMemberList 用新的成员顺序重绘成员表与"添加成员"下拉（候选随成员变化）。
function writeTeamMemberList(list, names, slot = agentTeamSlot("team")) {
  const pool = agentTeamEmployeePool();
  list.innerHTML = renderTeamMemberList(names, pool);
  const pick = slot?.querySelector?.("[data-team-member-pick]");
  if (!pick) return;
  const previous = pick.value;
  const candidates = pool.filter(role => !names.includes(role.roleName));
  pick.innerHTML = candidates
    .map(role => `<option value="${escapeHtml(role.roleName)}">${escapeHtml(roleDisplayName(role.roleName, role.roleKind))} · ${escapeHtml(role.roleName)}</option>`)
    .join("");
  if (candidates.some(role => role.roleName === previous)) pick.value = previous;
  const add = slot?.querySelector?.("[data-team-member-add]");
  if (add) add.disabled = candidates.length === 0;
}

// teamFormMemberNames 读团队表单里当前的成员顺序（提交时用它，而不是文本域）。
function teamFormMemberNames(form) {
  return teamMemberListNames(form.querySelector("[data-team-member-list]"));
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
  // 员工栏行内的「编辑」已下线（员工档案的唯一编辑入口 = 员工库，见 agent-team-view.js），
  // 这里不再接 data-team-edit；团队条目点击仍开团队面板（面板标题写明在改哪一支）。
  const editTeam = event.target.closest?.("[data-team-edit-team]");
  if (editTeam?.dataset.teamEditTeam) {
    openAgentTeamTeamPanel(editTeam.dataset.teamEditTeam);
    return;
  }

  // 2.5) 团队面板里改成员表（行序 = 发言顺序，纯本地草稿，保存才落盘）。
  const memberRemove = event.target.closest?.("[data-team-member-remove]");
  if (memberRemove?.dataset.teamMemberRemove) {
    teamMemberListFilter(name => name !== memberRemove.dataset.teamMemberRemove);
    return;
  }
  if (event.target.closest?.("[data-team-member-add]")) {
    const pick = agentTeamSlot("team")?.querySelector?.("[data-team-member-pick]");
    if (pick?.value) teamMemberListFilter(() => true, pick.value);
    return;
  }
  const employeeEdit = event.target.closest?.("[data-team-employee-edit]");
  if (employeeEdit?.dataset.teamEmployeeEdit) {
    openAgentTeamHire(employeeEdit.dataset.teamEmployeeEdit, "library");
    return;
  }

  // 3) 团队库动作：删条目（装配与点团队名在上面的分支里）。
  const deleteTeam = event.target.closest?.("[data-team-delete-team]");
  if (deleteTeam?.dataset.teamDeleteTeam) {
    const teamID = deleteTeam.dataset.teamDeleteTeam;
    if (!confirm(`确认从团队库删除 ${teamID}？已装配的会话不受影响。`)) return;
    await runAgentTeamAction(() => invoke("AgentTeamDeleteTeam", "", teamID));
    return;
  }

  // 3.5) 员工库动作：把"只在本会话在编"的一行写进员工库 / 删库里的行。
  const saveEmployee = event.target.closest?.("[data-team-employee-save]");
  if (saveEmployee?.dataset.teamEmployeeSave) {
    const roleName = saveEmployee.dataset.teamEmployeeSave;
    const role = agentTeamRolePayload(roleName);
    if (!role) return;
    await runAgentTeamAction(async () => {
      await invoke("AgentTeamSaveEmployee", "", role);
      showToast({ message: `${roleName} 已入库（全局事实，不装配到会话）` });
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
  // 员工库的新建：面板落在员工库作用域上（不装配、不动会话副本）。
  if (event.target.closest?.("[data-team-employee-new]")) {
    openAgentTeamHire("", "library");
    return;
  }

  // 4) 员工会话查看 / 顺序调整（摘除，与拖拽同一条提交路径）/ 删除。
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
    const slot = agentTeamSlot("team");
    const list = slot?.querySelector?.("[data-team-member-list]");
    if (list) writeTeamMemberList(list, names, slot);
    return;
  }
  const template = event.target.closest?.("[data-team-template]");
  if (template?.dataset.teamTemplate) {
    fillAgentTeamFormFromPreset(template.dataset.teamTemplate);
  }
});

// fillAgentTeamFormFromPreset 用内置形态起手（只填表单，不落盘）：用户改完点
// 「新建团队」才写团队库。
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
  const list = slot.querySelector("[data-team-member-list]");
  if (list) writeTeamMemberList(list, names, slot);
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

// ── 拖拽：员工库里的人 / 员工栏的行 / 团队面板的成员行 ──────────
// 三类源（员工库 ≡、员工栏 ≡、团队面板成员行）与三类落点：
//   员工栏某行 / "顺序末尾"落区 → 写会话发言顺序（AgentTeamSetOrder）；
//   团队面板成员表 → 只改本地草稿（保存团队才落盘）；
//   员工库里的行拖进会话时，先在会话入职（InstantiateRole），再落到拖放位置。
elements["team-view"]?.addEventListener("dragstart", event => {
  const handle = event.target.closest?.("[data-team-drag]");
  const employee = event.target.closest?.("[data-team-employee-drag]");
  const memberItem = event.target.closest?.("[data-team-member-item]");
  const row = event.target.closest?.("[data-team-staff-role]");
  const roleName = handle?.dataset.teamDrag || employee?.dataset.teamEmployeeDrag
    || memberItem?.dataset.teamMemberItem || row?.dataset.teamStaffRole || "";
  if (!roleName) return;
  agentTeamDragRole = roleName;
  agentTeamDragSource = employee ? "library" : memberItem ? "member" : "staff";
  (row || memberItem)?.classList.add("is-dragging");
  event.dataTransfer?.setData?.("text/plain", roleName);
  if (event.dataTransfer) event.dataTransfer.effectAllowed = "move";
});

// agentTeamDropTarget 解析一次拖拽落点：团队面板成员表 / 员工栏行 / 顺序末尾。
function agentTeamDropTarget(event) {
  const memberList = event.target.closest?.("[data-team-member-list]");
  const memberItem = event.target.closest?.("[data-team-member-item]");
  if (memberList || memberItem) return { kind: "member", node: memberItem || memberList, member: memberItem?.dataset.teamMemberItem || "" };
  const row = event.target.closest?.("[data-team-staff-role]");
  const endZone = event.target.closest?.("[data-team-order-drop]");
  if (row || endZone) return { kind: "order", node: row || endZone, role: row?.dataset.teamStaffRole || "" };
  return null;
}

elements["team-view"]?.addEventListener("dragover", event => {
  const target = agentTeamDropTarget(event);
  if (!target) return;
  event.preventDefault();
  if (event.dataTransfer) event.dataTransfer.dropEffect = "move";
  clearAgentTeamDropMarkers(target.node);
  target.node.classList.add("is-drop-target");
});

elements["team-view"]?.addEventListener("dragleave", event => {
  const zone = event.target.closest?.("[data-team-staff-role], [data-team-order-drop], [data-team-member-item]");
  if (zone) zone.classList.remove("is-drop-target");
});

elements["team-view"]?.addEventListener("drop", async event => {
  const target = agentTeamDropTarget(event);
  if (!target) return;
  event.preventDefault();
  const source = agentTeamDragRole || event.dataTransfer?.getData?.("text/plain") || "";
  agentTeamDragRole = "";
  agentTeamDragSource = "";
  clearAgentTeamDropMarkers();
  if (!source) return;
  if (target.kind === "member") {
    dropAgentTeamMember(source, target.member);
    return;
  }
  await dropAgentTeamOrder(source, target.role);
});

// dropAgentTeamMember 把一次拖拽落到团队面板的成员表上（本地草稿：插到目标成员之前，
// 空落点 = 追加到末尾）。成员的增删都只在表单里，点「保存团队」才写团队库。
function dropAgentTeamMember(source, beforeRole) {
  const slot = agentTeamSlot("team");
  const list = slot?.querySelector?.("[data-team-member-list]");
  if (!list) return;
  const current = teamMemberListNames(list);
  const rest = current.filter(name => name !== source);
  const at = beforeRole ? current.indexOf(beforeRole) : -1;
  const insertAt = at < 0 ? rest.length : current.slice(0, at).filter(name => name !== source).length;
  rest.splice(insertAt, 0, source);
  if (rest.join("\u0000") === current.join("\u0000")) return;
  writeTeamMemberList(list, rest, slot);
}

// dropAgentTeamOrder 把一次拖拽落到会话发言顺序上：已在编的员工直接改顺序；只在
// 员工库里的员工先入职（等 join_policy 决定进不进顺序），再落到拖放位置。
async function dropAgentTeamOrder(source, targetRole) {
  const team = normalizeAgentTeam(agentTeamView);
  if (!team.members.some(member => member.roleName === source)) {
    const role = agentTeamRolePayload(source);
    if (!role) return;
    try {
      await invoke("AgentTeamInstantiateRole", "", role, 0);
      agentTeamView = await invoke("AgentTeamView", "");
    } catch (error) {
      agentTeamError = error?.message || String(error);
      renderAgentTeamPanel();
      showToast(error);
      return;
    }
  }
  const next = agentTeamOrderForDrag(agentTeamView, source, targetRole);
  const policy = agentTeamCurrentPolicy();
  if (!next) return;
  if (!policy) {
    // 会话还没装过团队（没有顺序策略）：入职本身已经按 join_policy 决定了顺序，
    // 不硬塞一个空策略去写 lifecycle。
    await refreshAgentTeam({ force: true });
    return;
  }
  await runAgentTeamAction(() => invoke("AgentTeamSetOrder", "", policy, next.orderRoles));
}

elements["team-view"]?.addEventListener("dragend", () => {
  agentTeamDragRole = "";
  agentTeamDragSource = "";
  clearAgentTeamDropMarkers();
  elements["team-view"]?.querySelectorAll?.(".is-dragging").forEach(node => node.classList.remove("is-dragging"));
});

function clearAgentTeamDropMarkers(keep = null) {
  elements["team-view"]?.querySelectorAll?.(".is-drop-target").forEach(node => {
    if (node !== keep) node.classList.remove("is-drop-target");
  });
}

// readHirePermissionGrid 读出"逐格装配"面板的勾选：**每个已知组都写一条**（未勾 = 0）,
// 因为后端的显式格子是"这张表说了算"——只提交勾中的组，未列出的组语义会变成"没装配"
// （而用户看到的是"没勾 = 不开"）。
function readHirePermissionGrid(form) {
  const groups = {};
  for (const group of PERMISSION_GROUPS) {
    let bits = 0;
    for (const item of PERMISSION_BITS) {
      const box = form.querySelector(`[data-team-hire-perm="${group.name}"][data-team-hire-perm-bit="${item.bit}"]`);
      if (box?.checked) bits |= item.bit;
    }
    groups[group.name] = bits;
  }
  return groups;
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
    const toolsValue = String(hireForm.querySelector("[data-team-hire-tools]")?.value || "").trim();
    const customPermission = toolsValue === PERMISSION_CUSTOM_TOOLS;
    const role = {
      role_name: roleName,
      role_kind: hireForm.querySelector("[data-team-hire-kind]")?.value || "agent",
      join_policy: hireForm.querySelector("[data-team-hire-join]")?.value || "on_team_create",
      // 「逐格装配」是 UI 哨兵：它不落盘为 tools_policy，而是把勾选的格子提交成
      // permission_groups（后端按显式格子优先于档位分配主体条目）。
      tools_policy: customPermission ? "" : toolsValue,
      permission_groups: customPermission ? readHirePermissionGrid(hireForm) : undefined,
      model_policy: String(hireForm.querySelector("[data-team-hire-model]")?.value || "").trim(),
      presence_policy: String(hireForm.querySelector("[data-team-hire-presence]")?.value || "").trim(),
      system_prompt: String(hireForm.querySelector("[data-team-hire-prompt]")?.value || "").trim()
    };
    // undefined 的键在 JSON 序列化时被丢掉 = 不装配格子（继承/按档位判），
    // 而不是提交一份空 map（那是"装配了一个空格子"的另一回事）。
    if (!customPermission) delete role.permission_groups;
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

// agentTeamEntryFromForm 把"新建/编辑团队"表单换算成团队库条目：成员表行序 =
// 发言顺序（user → main → 成员），角色配置的细节（提示词/权限）在员工库/员工栏里
// 逐个编辑，团队库只回答"有谁、什么顺序"。成员规格优先取员工库里的那一份，
// 库里没有的（只在本会话在编）回落到注册表的角色配置。
function agentTeamEntryFromForm(form) {
  const name = String(form.querySelector("[data-team-form-name]")?.value || "").trim();
  if (!name) return null;
  const teamID = String(form.querySelector("[data-team-form-id]")?.value || "").trim() || name;
  const kind = String(form.querySelector("[data-team-form-kind]")?.value || "").trim() || teamID;
  const memberNames = teamFormMemberNames(form);
  const roles = memberNames.map(roleName => {
    const known = agentTeamRolePayload(roleName);
    const role = { role_name: roleName, role_kind: known?.role_kind || "agent", tools_policy: known?.tools_policy || "", system_prompt: known?.system_prompt || "" };
    // 团队库条目也要带上逐格装配的权限：库里存的是一整套员工配置，装配团队时
    // 按它复原——不带就等于"从库里装配一次，权限被降级成档位默认"。
    if (known?.permission_groups) role.permission_groups = known.permission_groups;
    return role;
  });
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

elements["team-view"]?.addEventListener("change", async event => {
  // 权限下拉的「逐格装配」：只是**展开/收起**权限位面板（不落盘、不发请求）——
  // 用户还没保存，只是想让格子可见。
  const toolsSelect = event.target.closest?.("[data-team-hire-tools]");
  if (toolsSelect) {
    const grid = toolsSelect.closest("[data-team-hire-form]")?.querySelector("[data-team-hire-perm-grid]");
    if (grid) grid.hidden = toolsSelect.value !== PERMISSION_CUSTOM_TOOLS;
    return;
  }
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

// setModal 统一弹窗开合：打开时摘掉收起态、移除 .hidden；关闭时先挂 .is-closing
// 播反向"收回"动效，动画结束（或兜底超时 / 减少动效偏好）再落到 .hidden。
// 所有弹窗都走这一条路径，所以详情、表格、设置、命令面板的开合手感一致。
// 减少动效偏好的判据只有一处：motion.js 的 prefersReducedMotion（见 import）。
function setModal(id, open) {
  const el = elements[id];
  if (!el) return;
  if (open) {
    if (el._closeTimer) { window.clearTimeout(el._closeTimer); el._closeTimer = 0; }
    el.classList.remove("is-closing");
    el.classList.remove("hidden");
    // 弹窗里的限高滚动块刚从 display:none 变可见：下一拍补一次边缘阴影
    // （几何此刻才可测）。setTimeout 同时避开模块求值期的 TDZ。
    window.setTimeout(refreshScrollShadows, 0);
    return;
  }
  if (el.classList.contains("hidden") || el.classList.contains("is-closing")) return;
  const finish = () => {
    if (!el.classList.contains("is-closing")) return;
    if (el._closeTimer) { window.clearTimeout(el._closeTimer); el._closeTimer = 0; }
    el.classList.remove("is-closing");
    el.classList.add("hidden");
  };
  if (prefersReducedMotion()) {
    el.classList.add("hidden");
    return;
  }
  el.classList.add("is-closing");
  el.addEventListener("animationend", finish, { once: true });
  // 兜底：动画被吞掉（元素不可见 / 事件丢失）时也要收起，不能让弹窗卡在半开态。
  el._closeTimer = window.setTimeout(finish, 320);
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
  const skinHost = elements["theme-picker"];
  const modeHost = elements["mode-picker"];
  if (!themeController) {
    if (skinHost) skinHost.innerHTML = '<span class="muted">皮肤清单未就绪</span>';
    if (modeHost) modeHost.innerHTML = '<span class="muted">深浅清单未就绪</span>';
    return;
  }
  const current = themeController.current();
  const currentSkin = current?.skin?.id || "";
  const currentMode = current?.mode || "";
  if (skinHost) {
    skinHost.innerHTML = themeController.skins.map(skin => {
      const active = skin.id === currentSkin;
      const swatches = skin.swatches.map(color => `<i style="background:${escapeHtml(color)}"></i>`).join("");
      return `<button type="button" class="theme-card${active ? " is-active" : ""}" data-skin-id="${escapeHtml(skin.id)}" role="radio" aria-checked="${active ? "true" : "false"}" title="${escapeHtml(skin.description)}">
      <span class="theme-swatches" aria-hidden="true">${swatches}</span>
      <span class="theme-name">${escapeHtml(skin.name)}${active ? '<span class="theme-current">当前</span>' : ""}</span>
      <span class="theme-desc">${escapeHtml(skin.description)}</span>
    </button>`;
    }).join("");
  }
  if (modeHost) {
    modeHost.innerHTML = themeController.modes.map(mode => {
      const active = mode.id === currentMode;
      return `<button type="button" class="theme-card${active ? " is-active" : ""}" data-mode-id="${escapeHtml(mode.id)}" role="radio" aria-checked="${active ? "true" : "false"}" title="${escapeHtml(mode.description)}">
      <span class="theme-name">${escapeHtml(mode.name)}${active ? '<span class="theme-current">当前</span>' : ""}</span>
      <span class="theme-desc">${escapeHtml(mode.description)}</span>
    </button>`;
    }).join("");
  }
}

async function initialiseTheme() {
  const manifest = await loadThemeManifest(window.fetch.bind(window));
  themeController = createThemeController({
    document,
    storage: themeStorage(),
    manifest,
    // 换肤要回流到 token 的 JS 消费方：xterm 的配色只在创建时取一次 token，
    // 不跟 CSS 变量走，必须由这里显式通知终端面板重取（否则换浅色皮肤后
    // 终端仍是深色底）。
    onApplied: () => terminalPanel.refreshTheme()
  });
  // 启动时套回上次的皮肤与深浅：皮肤 <link> 与 <html data-theme> 都由控制器决定。
  themeController.apply();
  renderThemePicker();
}

elements["theme-picker"]?.addEventListener("click", event => {
  const card = event.target.closest("[data-skin-id]");
  if (!card || !themeController) return;
  themeController.applySkin(card.dataset.skinId);
  renderThemePicker();
});

elements["mode-picker"]?.addEventListener("click", event => {
  const card = event.target.closest("[data-mode-id]");
  if (!card || !themeController) return;
  themeController.applyMode(card.dataset.modeId);
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

// 输入前缀（sigil）契约：与后端 application/core/completion.go 同一条表，前端
// 只消费不发明。权威说明见 docs/gui/modules/shell-and-interactions.md。
//   /  命令与工具   #  切换 Plugin   $  召回 Skill   @  手动召唤团队
const SIGIL_COMMAND = "/";
const SIGIL_PLUGIN = "#";
const SIGIL_SKILL = "$";
const SIGIL_TEAM = "@";
const SIGILS = [SIGIL_COMMAND, SIGIL_PLUGIN, SIGIL_SKILL, SIGIL_TEAM];

// SIGIL_PATTERN 判定"内联建议该不该弹"：前缀开头且还没进入参数区（无空白）。
const SIGIL_PATTERN = new RegExp(`^[${SIGILS.map(sigil => `\\${sigil}`).join("")}][^\\s]*$`);

async function openCommandPalette(trigger = "/") {
  state.commandTrigger = SIGILS.includes(trigger) ? trigger : "/";
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
  if (!SIGILS.includes(input[0])) {
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
  return ({ skill: "skill", plugin: "plugin", command: "command", team: "team" })[kind] || "command";
}

function acceptSuggestion(suggestion, trigger) {
  if (!suggestion) return;
  elements.prompt.value = `${trigger}${suggestion.text} `;
  resizePrompt();
  markComposerEdited();
  closeCommandPalette();
  hideInlineSuggestions();
  elements.prompt.focus();
  elements.prompt.setSelectionRange(elements.prompt.value.length, elements.prompt.value.length);
}

async function updateInlineSuggestions() {
  const input = elements.prompt.value.trimStart();
  if (!SIGIL_PATTERN.test(input)) {
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
  const sent = elements.prompt.value;
  const text = sent.trim();
  if (!text) return;
  try {
    // 提交路由：普通对话显式钉给**当前视图会话**（SubmitToSession），sigil
    // 输入交回后端路由器（规则见 composer-input.js composerSubmitPlan）。这样
    // "一个会话在跑、想发给另一个空闲会话"时，运行中会话不会因为后端视图指针
    // 的切换窗口把输入吸进自己的队列。视图会话 ID 在 await 之前取：这是本次
    // 提交要发往的会话，不是 RPC 回来时的会话。
    const plan = composerSubmitPlan({ text, viewedSessionID: client.current()?.session?.id });
    await invoke(plan.rpc, ...plan.args);
    // 提交是异步 RPC：往返期间用户可能已继续输入，只移除已发送的那段（规则见
    // composer-input.js），不整框清空——整框清空会把这段新输入一起吞掉。
    elements.prompt.value = clearSubmittedText(elements.prompt.value, sent);
    composerDirty = false;
    hideInlineSuggestions();
    resizePrompt();
    // 面板不在这里猜：`@` 召唤是否真的装了团队、goal 是否顺手拉起了 goal-a2a，
    // 都由后端的 team.changed 事件（见 invalidateAgentTeam）告诉面板。
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
  // 中文输入法确认候选词时也会触发 Enter（isComposing / composition 跟踪 /
  // keyCode 229 三重判据，见 composer-input.js），此时不能发送。
  if (event.key === "Enter" && !event.shiftKey && !isComposingEnter(event, composerComposing)) {
    event.preventDefault();
    elements.composer.requestSubmit();
  }
});
elements.prompt.addEventListener("compositionstart", () => { composerComposing = true; });
elements.prompt.addEventListener("compositionend", () => { composerComposing = false; });
elements.prompt.addEventListener("input", () => {
  resizePrompt();
  markComposerEdited();
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

for (const [modalID, close] of [["runtime-modal", closeRuntime], ["command-modal", closeCommandPalette], ["settings-modal", closeSettings], ["scheduled-task-modal", closeScheduledTaskDialog], ["node-detail-modal", closeNodeDetail], ["work-table-modal", closeWorkTable], ["new-session-modal", closeNewSessionModal], ["role-session-modal", closeRoleSessionDetail], ["compaction-frame-modal", closeCompactionFrame]]) {
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
    closePermissionMenu();
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

// 权限档 chip：点击**就地展开下拉**切换档位（档位是**主 agent 在本会话**的粒度）。
// 一步到位，不用先打开运行状态弹窗、再在大面板里找档位——切档不该是一次"进入配置页"。
elements["perm-toggle"].addEventListener("click", function(event) {
  event.stopPropagation();
  togglePermissionMenu();
});

// 下拉条目：点一条即提交（同 applyPermissionTier 单一路径，与运行状态里的列表共用）。
elements["perm-menu"].addEventListener("click", async function(event) {
  const button = event.target.closest("[data-tier]");
  if (!button || button.classList.contains("is-active")) return;
  await applyPermissionTier(button.dataset.tier);
});

// 下拉键盘：↑/↓ 在档位间循环（nextTierIndex 纯函数），Enter/空格提交。
elements["perm-menu"].addEventListener("keydown", async function(event) {
  if (!["ArrowDown", "ArrowUp", "Enter", " "].includes(event.key)) return;
  const items = Array.from(this.querySelectorAll(".perm-menu-item"));
  if (!items.length) return;
  event.preventDefault();
  const current = items.indexOf(document.activeElement);
  if (event.key === "Enter" || event.key === " ") {
    const target = current >= 0 ? items[current] : null;
    if (target) await applyPermissionTier(target.dataset.tier);
    return;
  }
  items[nextTierIndex(items.length, current, event.key === "ArrowDown" ? 1 : -1)]?.focus?.();
});

// 点浮层外部收下拉（与运行状态/命令面板同一"点外面就收"语义）。
document.addEventListener("click", event => {
  if (!event.target.closest?.("[data-perm-picker]")) closePermissionMenu();
});

// togglePermissionMenu 只做显隐与无障碍态；条目内容永远由后端投影重绘（renderPermissionMenu），
// 所以打开时不需要再拉一次数据，也不会出现"前端记的选中态"。
function togglePermissionMenu(force) {
  const menu = elements["perm-menu"];
  const chip = elements["perm-toggle"];
  if (!menu || !chip) return;
  const open = typeof force === "boolean" ? force : menu.hidden;
  menu.hidden = !open;
  chip.setAttribute("aria-expanded", open ? "true" : "false");
  chip.classList.toggle("is-open", open);
  if (open) menu.querySelector(".perm-menu-item:not(.is-active)")?.focus?.();
}

function closePermissionMenu() {
  togglePermissionMenu(false);
}

// applyPermissionTier 是唯一的切档路径（下拉与运行状态列表共用）：
// 按 Bridge 返回的**真正生效档位**渲染芯片，再拉整份快照（档位是会话级投影，切会话即换）。
async function applyPermissionTier(tier) {
  if (!tier) return;
  try {
    // Bridge 返回真正生效的档位：避免快照滞后造成"点了没生效"的滞后观感。
    const effective = await invoke("SetPermissionTier", tier);
    renderPermissionChip(effective);
    closePermissionMenu();
    await refresh({ scroll: false });
  } catch (error) {
    showToast(error);
  }
}

// 档位列表（运行状态弹窗）：与下拉同一条提交路径。
elements["permission-tier-list"].addEventListener("click", async function(event) {
  const button = event.target.closest("[data-tier]");
  if (!button || button.classList.contains("is-active")) return;
  await applyPermissionTier(button.dataset.tier);
});

// renderPermissionChip 只改芯片本身（乐观回执），完整运行时投影仍随后端快照刷新。
function renderPermissionChip(tier) {
  const chip = elements["perm-toggle"];
  if (!chip) return;
  const id = typeof tier === "string" && tier ? tier : String(tier?.id || "");
  const model = permissionTierChip(permissionTierCatalog(client.current()?.runtime || {}), id);
  chip.dataset.tier = model.id;
  chip.classList.toggle("is-on", model.isFull);
  chip.innerHTML = permissionChipInner(model);
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
// 侧栏无上限拉伸：下界保留可用最小宽，上界按视口动态计算（sidePaneMaxWidth）
// ——窗口越大能拉开越宽，不再有固定 420/480 的硬顶。
const LEFT_WIDTH_MIN = 200;
const RIGHT_WIDTH_MIN = 220;
const PREVIEW_WIDTH_KEY = "seelex.preview-pane-width";
const PREVIEW_MIN_WIDTH = 200;

function storageGet(key) {
  try { return window.localStorage.getItem(key); } catch { return null; }
}
function storageSet(key, value) {
  try { window.localStorage.setItem(key, value); } catch { /* 无存储环境忽略 */ }
}
// storageRemove 用于一次性迁移后清理旧键（旧值不该留在存储里当第二种事实）。
function storageRemove(key) {
  try { window.localStorage.removeItem(key); } catch { /* 无存储环境忽略 */ }
}
function clampPanelWidth(value, min, max) {
  return Math.min(max, Math.max(min, Math.round(value)));
}
// sidePaneMaxWidth 侧栏宽度上限：视口宽减去中间主视图与留白（主视图至少留
// ~280px），窗口越大侧栏能拉得越宽。
function sidePaneMaxWidth() {
  const viewport = window.innerWidth || 1280;
  return Math.max(320, viewport - 280);
}
function applyPanelWidths() {
  const left = clampPanelWidth(Number(storageGet(LEFT_WIDTH_KEY)) || 268, LEFT_WIDTH_MIN, sidePaneMaxWidth());
  const right = clampPanelWidth(Number(storageGet(RIGHT_WIDTH_KEY)) || 300, RIGHT_WIDTH_MIN, sidePaneMaxWidth());
  document.documentElement.style.setProperty("--left-w", `${left}px`);
  document.documentElement.style.setProperty("--right-w", `${right}px`);
}
// ── 拖拽尺寸读条（拟物标尺）────────────────────────────────────────────
// 拖动分区/终端时，在指针旁贴一枚小药丸显示当前像素尺寸；实现与终端面板
// 共用一份（见 resize-pill.js），不在这里再造一套 DOM 与计时器。
function setupPanelDividers() {
  applyPanelWidths();
  const shell = document.querySelector(".app-shell");
  const leftDivider = document.getElementById("left-divider");
  const rightDivider = document.getElementById("right-divider");
  if (!shell || !leftDivider || !rightDivider) return;

  function setWidth(variable, key, min, width) {
    const clamped = clampPanelWidth(width, min, sidePaneMaxWidth());
    document.documentElement.style.setProperty(variable, `${clamped}px`);
    storageSet(key, String(clamped));
    return clamped;
  }
  // beginDrag：统一拖拽骨架——挂 is-dragging（分隔条点亮）+ body 上的
  // is-resizing-col（全局禁选/统一光标），onMove 返回新宽度用于读条。
  function beginDrag(divider, onMove) {
    return event => {
      if (event.button !== 0) return;
      event.preventDefault();
      divider.classList.add("is-dragging");
      document.body.classList.add("is-resizing-col");
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      const move = moveEvent => {
        const width = onMove(moveEvent);
        if (Number.isFinite(width)) showResizePill(`${width} px`, moveEvent.clientX, moveEvent.clientY);
      };
      const up = () => {
        divider.classList.remove("is-dragging");
        document.body.classList.remove("is-resizing-col");
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
        hideResizePill();
        window.removeEventListener("pointermove", move);
        window.removeEventListener("pointerup", up);
      };
      window.addEventListener("pointermove", move);
      window.addEventListener("pointerup", up);
    };
  }
  leftDivider.addEventListener("pointerdown", beginDrag(leftDivider, event => {
    return setWidth("--left-w", LEFT_WIDTH_KEY, LEFT_WIDTH_MIN, event.clientX - shell.getBoundingClientRect().left);
  }));
  rightDivider.addEventListener("pointerdown", beginDrag(rightDivider, event => {
    return setWidth("--right-w", RIGHT_WIDTH_KEY, RIGHT_WIDTH_MIN, shell.getBoundingClientRect().right - event.clientX);
  }));
  // 双击分隔条 = 复位到默认宽度（左 268 / 右 300），拖歪了不用来回找。
  leftDivider.addEventListener("dblclick", () => setWidth("--left-w", LEFT_WIDTH_KEY, LEFT_WIDTH_MIN, 268));
  rightDivider.addEventListener("dblclick", () => setWidth("--right-w", RIGHT_WIDTH_KEY, RIGHT_WIDTH_MIN, 300));

  function keyboardAdjust(divider, isRight) {
    divider.addEventListener("keydown", event => {
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
      event.preventDefault();
      const step = event.key === "ArrowLeft" ? -16 : 16;
      let width;
      if (isRight) {
        const current = parseFloat(document.documentElement.style.getPropertyValue("--right-w")) || 300;
        width = setWidth("--right-w", RIGHT_WIDTH_KEY, RIGHT_WIDTH_MIN, current - step);
      } else {
        const current = parseFloat(document.documentElement.style.getPropertyValue("--left-w")) || 268;
        width = setWidth("--left-w", LEFT_WIDTH_KEY, LEFT_WIDTH_MIN, current + step);
      }
      const rect = divider.getBoundingClientRect();
      flashResizePill(`${width} px`, rect.left + rect.width / 2, rect.top + rect.height / 2);
    });
  }
  keyboardAdjust(leftDivider, false);
  keyboardAdjust(rightDivider, true);
}
setupPanelDividers();

// ── 滚动/滑动的拟物手感 ────────────────────────────────────
// 三件事，各只写一份、都挂在同一个根容器上（委托，不逐元素绑）：
//  1) 纵向滚动边缘阴影：容器滚到中间时，上/下沿浮出内阴影（"还有内容"），
//     到边即隐——滚动位置自己说话，不用工具栏提示；
//  2) 纵向滚轮 → 横向滚动：`.scroll-edges-x` 的横条（页签栏 / 工作表格 sheet
//     栏）在鼠标滚轮下也能左右翻；到边放行给外层容器继续纵滚；
//  3) 按住拖动 → 横向拨动：把横条当成一根可拨的滚轴（拖动过阈值才算，
//     随后的那次 click 被吞掉，不会误切页签）。
// 判据/几何都在 motion.js（纯函数、node --test 覆盖），这里只负责挂根监听
// 与命中哪些容器。
const SCROLL_SHADOW_TARGETS = [
  ".work-table-scroll",
  ".session-group-body",
  "#account-list",
  "#plugin-list",
  "#team-view",
  "#goal-view",
  "#scheduled-task-view",
  "#history-search-view",
  "#git-log-view"
].join(", ");

function scrollAffordanceRoot() {
  return document.querySelector(".app-shell") || document.body || document.documentElement;
}

// refreshScrollShadows 给当前所有限高滚动块补一次边缘阴影状态：整份快照重绘
// 后、以及弹窗打开后（容器刚从 display:none 变成可见，几何才可测）调用。
function refreshScrollShadows() {
  return syncAllScrollShadows(document, SCROLL_SHADOW_TARGETS);
}

function setupScrollAffordances() {
  // 边缘阴影是静态提示（不是动画）：任何偏好下都挂——长度/位置自己说话。
  bindScrollShadows(document, SCROLL_SHADOW_TARGETS);
  refreshScrollShadows();
  const root = scrollAffordanceRoot();
  if (!root) return;
  // 滚轮翻译与按住拖动是"手势替代"：减少动效偏好下不接管，交还原生滚动。
  if (prefersReducedMotion()) return;
  bindHorizontalWheelDelegate(root);
  bindHorizontalDragDelegate(root);
}
setupScrollAffordances();

// ── 「资源管理器」文件预览抽屉（代码子页左分栏）──────────────
// 预览抽屉是代码子页内部结构：left 内容详情 / divider / right（工作树+提交记录）。
// 抽屉宽度以 CSS 变量 + localStorage 记忆（默认 380px，无上限：拖分隔条可把
// 任一侧拉到几乎占满子页）。内容详情是「多文件详情」容器：每个文件一枚上标
// chip + 一个独立面板；chip 全部关闭（容器为空）时容器生命周期结束，抽屉自动
// 收起、子页恢复原来大小。另可一键「隐藏工作树与提交记录」，让内容详情独占
// 整个子页（`.is-panes-hidden`，见 styles.css「子页3」）。布局两态由
// `.code-split` 上的 `.is-preview-open` 切换，抽屉内容不因收起而销毁。
const FILE_PREVIEW_PANES_KEY = "seelex.preview-panes-hidden";

// previewMaxWidth 预览列上限：子页可视宽减去分隔条（只留 6px），因此内容详情
// 可以拉伸到几乎占满子页（「没有限制的拉伸」），不预设固定上限。
function previewMaxWidth() {
  const split = document.getElementById("code-split");
  const width = split?.getBoundingClientRect?.().width || 0;
  return Math.max(PREVIEW_MIN_WIDTH, Math.round(width - 6));
}

// syncPreviewLayout 同步代码子页布局：展开=多列（preview/divider/panes），
// 收起=单列（panes 占满，防止预览抽屉收起后内容被裁成空白）；展开且勾选
// 「隐藏工作树/提交记录」时 = 仅 preview；详情被「收起让出内容页」时 =
// 28px 竖轨 + panes（详情内容不销毁，点竖轨即还原）。
function syncPreviewLayout() {
  const split = document.getElementById("code-split");
  if (!split) return;
  const collapsed = previewPaneOpen && previewCollapsed;
  split.classList.toggle("is-preview-open", previewPaneOpen);
  split.classList.toggle("is-preview-collapsed", collapsed);
  split.classList.toggle("is-panes-hidden", previewPaneOpen && previewPanesHidden && !collapsed);
  syncPanesHiddenButton();
  syncPreviewCollapseButton();
}

// syncPreviewCollapseButton 维护「收起详情，让出内容页」按钮的开关态与文案。
function syncPreviewCollapseButton() {
  const button = elements["file-preview-collapse"];
  if (!button) return;
  const collapsed = previewPaneOpen && previewCollapsed;
  const label = collapsed ? "展开文件详情" : "收起详情，让出内容页";
  button.setAttribute("aria-pressed", collapsed ? "true" : "false");
  button.setAttribute("title", label);
  button.setAttribute("aria-label", label);
  button.classList.toggle("is-on", collapsed);
}

// togglePreviewCollapsed 只在详情抽屉展开时有意义：把详情收成一条竖轨，让
// 工作树 / 提交记录独占子页（「躲开内容页」）；再次点击（或点竖轨）还原。
// 这个收起态刻意**不落盘**：它只在"当前抽屉里正开着文件详情"的语境下成立
// （打开新文件会解除它），记忆一个瞬时姿态只会让下次打开文件时莫名什么都不出。
function togglePreviewCollapsed() {
  if (!previewPaneOpen) return;
  previewCollapsed = !previewCollapsed;
  // 详情收起时「隐藏工作树/提交记录」没有意义（内容页正是被让出的那一侧），
  // 一并复位，避免还原后出现两侧都隐蔽的空子页。
  if (previewCollapsed) previewPanesHidden = false;
  storageSet(FILE_PREVIEW_PANES_KEY, previewPanesHidden ? "1" : "0");
  syncPreviewLayout();
}

// syncPanesHiddenButton 维护「隐藏工作树/提交记录」按钮的开关态与文案。
function syncPanesHiddenButton() {
  const button = elements["file-preview-hide-panes"];
  if (!button) return;
  const hidden = previewPaneOpen && previewPanesHidden && !previewCollapsed;
  const label = hidden ? "显示工作树与提交记录" : "隐藏工作树与提交记录";
  button.setAttribute("aria-pressed", hidden ? "true" : "false");
  button.setAttribute("title", label);
  button.setAttribute("aria-label", label);
  button.classList.toggle("is-on", hidden);
}

// togglePanesHidden 只在内容详情展开（且未被收起成竖轨）时有意义：隐蔽右栏
// （工作树 + 提交记录），让内容详情独占整个子页；再次点击恢复。
function togglePanesHidden() {
  if (!previewPaneOpen || previewCollapsed) return;
  previewPanesHidden = !previewPanesHidden;
  storageSet(FILE_PREVIEW_PANES_KEY, previewPanesHidden ? "1" : "0");
  syncPreviewLayout();
}

function openFilePreview(entry) {
  if (!entry || !entry.path) return;
  const snapshot = client.current();
  previewRoot = snapshot?.current_workspace?.root_path || previewRoot;
  previewPaneOpen = true; // open must flip the state flag, otherwise closeFilePreview guard always returns and the X button never closes
  // 打开文件 = 要看详情：把「收起让出内容页」解除（否则点了文件树却什么都不显示）。
  previewCollapsed = false;
  const pane = elements["file-preview-pane"];
  if (pane) {
    // 抽屉从"收起"到"展开"的这一次：播一遍"把文件从文件夹里抽出来摊开"的动效
    // （打开已有抽屉里的另一个文件不重播，避免切换文件时反复抽动）。
    const wasClosed = pane.classList.contains("is-closed");
    pane.classList.remove("is-closed");
    document.documentElement.style.setProperty("--preview-w", previewPaneWidth());
    syncPreviewLayout();
    if (wasClosed && !prefersReducedMotion()) {
      pane.classList.remove("is-drawing");
      void pane.offsetWidth; // 强制重排，保证同一动画可重放
      pane.classList.add("is-drawing");
      const clear = () => pane.classList.remove("is-drawing");
      pane.addEventListener("animationend", clear, { once: true });
      window.setTimeout(clear, 420);
    }
  }
  filePreviewController.open(entry);
  // 资源管理器可能停靠在主视图或右栏：无论当前在哪，打开文件预览前
  // 先让该子页成为所在栏的激活页。
  revealView("code");
}

function closeFilePreview() {
  const pane = elements["file-preview-pane"];
  if (!previewPaneOpen && (!pane || pane.classList.contains("is-closed"))) return;
  previewPaneOpen = false;
  // 容器生命周期结束（主动收起 / 最后一个 chip 关闭）→ 恢复原来大小：
  // 工作树与提交记录重新占满子页。
  previewPanesHidden = false;
  previewCollapsed = false;
  storageSet(FILE_PREVIEW_PANES_KEY, "0");
  if (pane) pane.classList.add("is-closed");
  syncPreviewLayout();
  filePreviewController.clear();
}

// previewPaneWidth 预览列宽度：只保留下界，**不设固定上限**——容器封顶交给
// CSS 的 min(var(--preview-w), calc(100% - 6px))。因此窗口/子页变窄时不会把已存
// 宽度改小（拉大后原样记住），拖拽时的即时钳制才用 previewMaxWidth()。
function previewPaneWidth() {
  const stored = Number(storageGet(PREVIEW_WIDTH_KEY));
  const width = Number.isFinite(stored) && stored > 0 ? Math.max(PREVIEW_MIN_WIDTH, Math.round(stored)) : 380;
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
    document.body.classList.add("is-resizing-col");
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    const move = moveEvent => {
      const rect = split.getBoundingClientRect();
      // 无上限：只受容器宽封顶（留 6px 给分隔条），内容详情与工作树谁宽谁窄
      // 完全由拖动决定。
      const width = clampPanelWidth(moveEvent.clientX - rect.left, PREVIEW_MIN_WIDTH, Math.max(PREVIEW_MIN_WIDTH, rect.width - 6));
      document.documentElement.style.setProperty("--preview-w", `${width}px`);
      showResizePill(`${width} px`, moveEvent.clientX, moveEvent.clientY);
    };
    const up = () => {
      divider.classList.remove("is-dragging");
      document.body.classList.remove("is-resizing-col");
      document.body.style.cursor = "";
      document.body.style.userSelect = "";
      hideResizePill();
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
    const width = clampPanelWidth(current + step, PREVIEW_MIN_WIDTH, previewMaxWidth());
    document.documentElement.style.setProperty("--preview-w", `${width}px`);
    storageSet(PREVIEW_WIDTH_KEY, String(width));
    const rect = divider.getBoundingClientRect();
    flashResizePill(`${width} px`, rect.left + rect.width / 2, rect.top + rect.height / 2);
  });
}

function applyPreviewWidth() {
  document.documentElement.style.setProperty("--preview-w", previewPaneWidth());
}

// 初始化：内容详情容器默认空 → 抽屉收起（生命周期以「有文件详情」为前提，
// 因此不记忆展开态，只记忆宽度与「隐藏工作树/提交记录」偏好）。
(function initFilePreviewPane() {
  applyPreviewWidth();
  previewPanesHidden = storageGet(FILE_PREVIEW_PANES_KEY) === "1";
  previewCollapsed = false;
  previewPaneOpen = false;
  elements["file-preview-pane"]?.classList.add("is-closed");
  syncPreviewLayout();
})();
setupFilePreviewResize();

// ── 下栏终端（VS Code 式面板）───────────────────────────────
// 后端权威在 gui/terminal（PTY 会话管理：多开、读写、resize、退出码），前端
// 只画布局：面板是否展开/收起、高度、当前标签是本页状态（localStorage 记忆），
// 会话与输出全部来自 Bridge 与 seelex:terminal 事件。快捷键与 VS Code 对齐：
// Ctrl+` 切换面板（已展开则收起），Ctrl+Shift+` 新建终端。
const terminalPanel = createTerminalPanel({
  host: elements["terminal-panel"],
  body: elements["terminal-body"],
  tabsHost: elements["terminal-tabs"],
  resizeHandle: elements["terminal-resize"],
  collapseButton: elements["terminal-collapse"],
  newButton: elements["terminal-new"],
  closeButton: elements["terminal-close"],
  hideButton: elements["terminal-hide"],
  toggleButton: elements["terminal-button"],
  invoke,
  onError: showToast
});

// 终端回滚行数（设置面板 → 终端）：启动时把当前值填进下拉框，change 时落盘并立刻
// 作用到已打开的标签。放在面板构造之后——这里要用到 terminalPanel（避免 TDZ）。
function syncTerminalScrollbackSelect() {
  const select = elements["terminal-scrollback"];
  if (!select) return;
  const current = String(terminalPanel.scrollback());
  if (!select.querySelector(`option[value="${current}"]`)) {
    // 落盘值不在预置选项里（手改过 localStorage）：补一条，别让下拉框显示成别的值。
    const option = document.createElement("option");
    option.value = current;
    option.textContent = `${current} 行`;
    select.appendChild(option);
  }
  select.value = current;
}

elements["terminal-scrollback"]?.addEventListener("change", event => {
  terminalPanel.setScrollback(event.target.value);
  syncTerminalScrollbackSelect();
});

syncTerminalScrollbackSelect();

document.addEventListener("keydown", event => {
  if (!(event.ctrlKey || event.metaKey) || event.altKey) return;
  if (event.key !== "`" && event.key !== "~") return;
  event.preventDefault();
  if (event.shiftKey) terminalPanel.newTerminal();
  else terminalPanel.toggle();
});

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
    // 终端输出走独立事件名（seelex:terminal），与 seelex:event 的 delivery_seq
    // 水位无关，因此单独绑定。
    terminalPanel.bindRuntime(window.runtime);
    const info = await invoke("Info");
    state.info = info;
    elements["app-title"].textContent = info.title || "Seelex";
    elements["app-version"].textContent = info.version || "dev";
    await refresh({ scroll: "bottom" });
    // 面板上次是展开的就恢复展开（VS Code 同口径）：恢复时按需要新建一个终端。
    if (terminalPanel.state().open) terminalPanel.open();
  } catch (error) {
    showToast(error);
    window.setTimeout(initialise, 600);
  }
}
initialise();
