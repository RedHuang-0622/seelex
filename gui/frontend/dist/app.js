import { escapeHtml, hydrateIcons, icon, queueDragTarget, queueMoveTarget } from "./components.js";
import { FILE_DRAG_MIME, dropPaths, dropPlan, fileDragPayload } from "./file-drop.js";
import {
  FILE_REF_CANDIDATE_LIMIT,
  FILE_REF_TRIGGER,
  applyFileReference,
  fileReferenceCandidates,
  fileReferenceIndex,
  fileReferenceQuery,
  fileReferenceStripHTML,
  removeFileReference
} from "./file-reference.js";
import { createChatView } from "./chat-view.js";
import { createGUIClient } from "./client-state.js";
import { clearSubmittedText, composerSubmitPlan, composerViewSwitch, isComposingEnter, shouldRestoreDraft } from "./composer-input.js";
import { createConversationView } from "./conversation-view.js";
import { branchOriginModel } from "./branch-origin.js";
import { createEmbedActionGate, resolveEmbedAction } from "./embed-bridge.js";
import { createTrajectoryView } from "./trajectory-view.js";
import { buildTrajectory } from "./trajectory.js";
import { createComposerKnobs, REASONING_EFFORT_LEVELS, REASONING_EFFORT_LABELS } from "./composer-knobs.js";
import { approvalRiskLabel } from "./interaction-risk.js";
import { nextTierIndex } from "./permission-tier.js";
import {
  planToDSL, renderNodeDetail, setNodeDetailConversation, bindNodeDetailTabs, subagentTreeNodeToDSL, workItemToDetailNode
} from "./plan-dsl.js";
import { createWorkbenchSummaryView } from "./workbench-summary.js";
import { createWorkTreeView } from "./worktree-view.js";
import { createGitLogView } from "./git-log-view.js";
import { buildRepoTabs, renderRepoTabs, resolveSelectedRepo } from "./git-repos.js";
import { createWorkspaceChangesView } from "./workspace-changes.js";
import { createFilePreviewController } from "./file-preview.js";
import { decideReviewApply, reviewRequestEntries } from "./review-request.js";
import { renderCompactionFrameModal, renderContextCompactions } from "./context-summary.js";
import { compactionRangeText, compactionReasonLabel, mergeCompactionProgress } from "./compaction-format.js";
import { renderGoalDetail, renderGoalPanel } from "./goal-board-view.js";
import { TEAM_BOARD_CSS, TEAMMATE_LIVE_PAGE_SIZE, parseTeamPageRef, renderTeamBoard, renderTeamPage, renderTeammateLiveSession, teamPageTitle, teammateSessionEntry } from "./team-board-view.js";
import { createRuntimeEventBinder } from "./runtime-events.js";
import { buildScheduledTaskSpec, renderScheduledTasks, scheduledTaskFormFields } from "./scheduled-tasks-view.js";
import { renderToolRecordBoard, TOOL_RECORD_CSS } from "./tool-record-board.js";
import { renderRoleSessionDetail, roleDisplayName } from "./role-session-view.js";
// 提交侧的装配（按会话插件）规整：**只 trim + 丢空项，不去重**（重复由后端显式拒绝）。
import { escapePluginSourceText, pluginSourceBadge } from "./plugin-source.js";
import { renderHistorySearchResults } from "./history-search.js";
import { createThemeController, loadThemeManifest } from "./theme.js";
import { resolveWindowTreatment } from "./window-treatment.js";
import { normalizeReleaseWindow, releaseWindowLabel, releaseWindowOptions } from "./release-window.js";
import { ACCOUNT_WINDOW_CUSTOM, accountWindowLabel, normalizeAccountWindowInput } from "./account-window.js";
// 角色卡下账户的增删改查与查余额：判据是纯函数，DOM 与 invoke 接线在本文件。
import { accountDraftFromForm } from "./account-admin.js";
// 角色设置四项的取值面：权限向量的标签表 / 归一化 / 开关 / lint，以及三个选择器的选项串。
import {
  normalizePermissionVector,
  permissionVectorSummary,
  permissionVectorLint,
  renderPermissionVectorHTML,
  rolePluginOptionsHTML,
  cyclePermissionVector,
} from "./role-settings.js";
import {
  rolePermissionBoardHTML,
} from "./role-permission-board.js";
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
  // inlineTrigger = 当前候选列表属于哪个触发字符（`/` `#` `$` 或 `@`）。它不能从
  // "输入首字符"现推：`@` 是**光标处**的标记，可以出现在一句话中间（见
  // updateInlineSuggestions），于是上下键重画与 Tab 接受都必须按这个记录走。
  inlineTrigger: "",
  // 文件引用选择器（`+` 加号）的候选与选中项：候选来自已加载的工作区树索引。
  filePickerItems: [],
  filePickerSelected: 0,
  resumingSessionID: "",
  openSessionMenu: "",
  tab: "conversation",
  rightTab: "status",
  trajectoryFilter: "all",
  // 账户是 provider → model 两级：这是"当前停在哪个供应商"的纯 UI 状态。
  accountProvider: "",
  // 插件页面**只看不切**：这是「停在哪一个插件上浏览它的 Skill」的纯视图状态，
  // 与 runtime.plugin（进程级激活态，只有 `#` 命令/定时任务会改）是两条独立事实。
  selectedPlugin: ""
};

const elements = Object.fromEntries([
  "app-title", "app-version", "connection-dot", "provider-label", "token-label",
  // 顶栏两个诊断徽标宿主：缺了它们，下面 `if (elements["perf-badge-host"])` 永远为假，
  // 徽标会静默不渲染（性能/事件诊断数据照采，但用户看不到，见 element-registry.test.mjs）。
  "perf-badge-host", "live-diag-host",
  "session-list", "session-count", "new-session",
  "plugin-list", "plugin-count", "plugin-management", "plugin-management-title", "plugin-management-description", "plugin-management-state", "plugin-management-meta", "plugin-skill-count", "account-list", "account-count", "conversation", "conversation-tabs", "trajectory",
  "empty-state", "composer", "prompt", "composer-status", "stop-button", "send-button", "branch-origin",
  "runtime-details", "reasoning-effort-control", "reasoning-effort-range", "reasoning-effort-value", "loop-limit-control", "loop-limit-range", "loop-limit-value", "loop-limit-infinite", "work-section", "work-count", "workbench-summary-view", "scheduled-task-section", "scheduled-task-view", "scheduled-task-count", "new-scheduled-task", "scheduled-editor", "scheduled-task-modal-title", "scheduled-task-close", "scheduled-task-cancel", "sched-name", "sched-workspace", "sched-permission", "sched-plugins", "sched-effort", "sched-loops", "sched-mode", "sched-period-value", "sched-period-unit", "sched-period-field", "sched-anchor-field", "sched-start-clock", "sched-start-weekday", "sched-anchor-now", "sched-anchor-now-field", "sched-datetime", "sched-datetime-field", "sched-prompt", "sched-prompt-field", "sched-enabled", "sched-enabled-field", "sched-enabled-label", "sched-submit", "history-search-section", "history-search-form", "history-search-input", "history-search-view", "history-search-count", "skill-list", "history-bar",
  "left-nav", "toggle-left-nav", "task-list-context", "left-page-tasks", "left-page-scheduled", "left-page-plugins",
  "project-name", "project-root", "project-status", "project-settings-button", "project-settings-modal", "project-settings-close", "project-settings-name", "session-environment-summary", "session-environment-select", "session-environment-confirm", "session-environment-status", "worktree-view", "file-count", "context-compactions",
  "compaction-frame-modal", "compaction-frame-modal-close", "compaction-frame-modal-title", "compaction-frame-modal-meta", "compaction-frame-modal-view",
  "role-session-modal", "role-session-close", "role-session-modal-title", "role-session-view",
  // 团队详情页（里程碑 / Work Item / teammate 三种页共用一套壳）：壳与落点在这里登记，
  // 页的内容全归渲染件 team-board-view.js 的 renderTeamPage。
  "team-page-modal", "team-page-close", "team-page-modal-title", "team-page-view",
  "right-tabs", "goal-section", "goal-badge", "goal-view", "goal-detail-modal", "goal-detail-close", "goal-detail-title", "goal-detail-view", "team-board-section", "team-board-badge", "team-board-view", "code-panes", "code-pane-tabs", "git-repo-tabs", "code-pane-worktree", "code-pane-gitlog", "git-log-view", "git-log-count", "code-pane-changes", "changes-view", "changes-count",
  "tool-record-section", "tool-record-view", "tool-record-total",
  "file-preview-pane", "file-preview-view", "file-preview-tabs", "file-preview-hide-panes", "file-preview-close", "file-preview-divider", "file-preview-collapse", "file-preview-rail",
  "file-save-modal", "file-save-title", "file-save-message", "file-save-keep", "file-save-discard", "file-save-cancel",
  "runtime-button", "runtime-modal", "runtime-close", "settings-button", "settings-modal", "settings-close", "settings-nav", "terminal-scrollback", "release-window", "release-window-status", "role-default-plugin", "default-plugin-status", "role-settings-status", "permission-role-board", "ssh-env-list", "ssh-env-add", "ssh-env-name", "ssh-env-host", "ssh-env-port", "ssh-env-user", "ssh-env-remote-root", "ssh-env-password", "ssh-env-default", "ssh-env-save", "ssh-env-delete", "ssh-env-status", "environment-editor-modal", "environment-editor-title", "environment-editor-note", "environment-editor-close", "environment-editor-cancel", "environment-editor-status", "account-add-modal", "account-add-modal-title", "account-add-note", "account-add-cancel", "account-add-cancel-footer", "account-add-role", "account-add-model", "account-add-api-key", "account-add-provider", "account-add-base-url", "account-add-effort", "account-add-submit", "account-add-status", "account-delete-modal", "account-delete-title", "account-delete-message", "account-delete-close", "account-delete-cancel", "account-delete-confirm", "account-delete-status", "ssh-userspace-modal", "ssh-userspace-close", "ssh-userspace-cancel", "ssh-userspace-paths", "ssh-userspace-save", "ssh-userspace-status", "theme-picker", "mode-picker", "inline-suggestions",
  "tool-record-enabled-setting", "tool-record-setting-status",
  "command-button", "command-modal", "command-close", "command-triggers", "command-search", "command-results",
  // 文件引用（J-1b）：草稿 chip 条 + `+` 加号的候选面板。
  "composer-file-refs", "file-button", "composer-file-picker", "composer-file-close", "composer-file-search", "composer-file-results",
  "load-history", "latest-history", "interaction-modal", "perm-toggle", "perm-menu", "interaction-risk", "interaction-title", "permission-tier-list",
  "new-session-modal", "new-session-close", "new-session-task", "new-session-workspace", "new-session-back", "new-session-workspace-list", "new-session-pick-folder", "new-session-step-1", "new-session-step-2", "new-project-drafts", "new-project-create",
  "interaction-question", "interaction-preview", "interaction-options",
  "node-detail-modal", "node-detail-close", "node-detail-title", "node-detail-content", "toast", "ui-tooltip",
  "toggle-left-panel", "toggle-right-panel",
  "message-queue",
  "terminal-panel", "terminal-body", "terminal-tabs", "terminal-resize", "terminal-collapse",
  "terminal-new", "terminal-close", "terminal-hide", "terminal-button",
  "session-permission-modal", "session-permission-close", "session-permission-board"
].map(id => [id, document.getElementById(id)]));

// ── 左侧主导航 ───────────────────────────────────────────────
// 一级导航与任务列表是两个独立纵列：导航只决定第二列显示哪种列表；
// 页面切换与导航收缩只属于视图，不写快照。
const LEFT_PAGE_IDS = { tasks: "left-page-tasks", scheduled: "left-page-scheduled", plugins: "left-page-plugins" };
const LEFT_PAGE_LABELS = { tasks: "聊天", scheduled: "定时任务", plugins: "插件 Skill" };
const LEFT_PAGE_STORAGE_KEY = "seelex.left-page";
const LEFT_NAV_COLLAPSED_KEY = "seelex.left-nav.collapsed";

function normalizeLeftPage(page) {
  const value = String(page || "");
  if (value === "skills") return "plugins";
  return Object.hasOwn(LEFT_PAGE_IDS, value) ? value : "tasks";
}

function closePluginManagement() {
  document.querySelector(".workspace")?.classList.remove("is-plugin-manager");
  elements["plugin-management"]?.classList.add("hidden");
}

function openPluginManagement() {
  if (document.querySelector(".workspace")?.classList.contains("is-task-schedule")) closeScheduledTaskDialog();
  document.querySelector(".workspace")?.classList.add("is-plugin-manager");
  elements["plugin-management"]?.classList.remove("hidden");
}

function showLeftPage(page, persist = true) {
  const active = normalizeLeftPage(page);
  for (const [key, id] of Object.entries(LEFT_PAGE_IDS)) {
    const panel = elements[id];
    panel?.classList.toggle("hidden", key !== active);
  }
  for (const button of elements["left-nav"]?.querySelectorAll?.("[data-left-page]") || []) {
    const selected = button.dataset.leftPage === active;
    button.classList.toggle("is-active", selected);
    if (selected) button.setAttribute("aria-current", "page");
    else button.removeAttribute("aria-current");
  }
  document.documentElement.dataset.leftPage = active;
  if (elements["task-list-context"]) elements["task-list-context"].textContent = LEFT_PAGE_LABELS[active];
  const workspace = document.querySelector(".workspace");
  if (active !== "plugins") closePluginManagement();
  if (active === "plugins") openPluginManagement();
  if (active === "tasks" && workspace?.classList.contains("is-task-schedule")) {
    closeScheduledTaskDialog();
  }
  if (persist) storageSet(LEFT_PAGE_STORAGE_KEY, active);
}

function setLeftNavCollapsed(collapsed, persist = true) {
  const next = Boolean(collapsed);
  document.documentElement.dataset.leftNavCollapsed = next ? "true" : "false";
  elements["toggle-left-nav"]?.setAttribute("aria-expanded", String(!next));
  const toggle = elements["toggle-left-nav"];
  if (toggle) {
    const name = next ? "chevron-right" : "chevron-left";
    toggle.dataset.icon = name;
    toggle.innerHTML = icon(name, 14);
  }
  if (persist) storageSet(LEFT_NAV_COLLAPSED_KEY, next ? "1" : "0");
}

elements["left-nav"]?.addEventListener("click", event => {
  const button = event.target?.closest?.("[data-left-page]");
  if (button?.dataset.leftPage) showLeftPage(button.dataset.leftPage);
});
elements["toggle-left-nav"]?.addEventListener("click", event => {
  event.stopPropagation();
  setLeftNavCollapsed(document.documentElement.dataset.leftNavCollapsed !== "true");
  applyPanelWidths();
});
setLeftNavCollapsed(storageGet(LEFT_NAV_COLLAPSED_KEY) === "1", false);
showLeftPage(storageGet(LEFT_PAGE_STORAGE_KEY) || "tasks", false);

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
  // composer 的两条运行参数拉条是"下一回合"设置：本回合 running 时后端按
  // G0b/INV-G7 拒绝切换，因此把入口先锁上并说明原因，而不是让用户拖完再看失败提示。
  composerKnobs.setEnabled(!Boolean(client.current()?.chat?.running));
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
      snapshot.task?.context_compactions || [],
      snapshot.conversation_revision || 0
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
  editFork: async request => {
    const parentID = client.current()?.session?.id || "";
    if (!parentID) throw new Error("当前没有可编辑的会话");
    const result = await invoke("EditUserMessageAndFork", parentID, request);
    await refresh({ scroll: "bottom" });
    return result;
  },
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
      // 门禁进度按会话路由投递：跟着视图走的那条进度条属于上一个会话的压缩，
      // 切过来还挂着就是把别的会话的压缩说成当前会话的。权威快照随后重绘面板。
      dropCompactionProgress();
      // 帧正文视图态（右栏展开的正文 + 帧正文弹框）同属上一个会话：切过来还挂着
      // 就是把别的会话的压缩正文说成当前会话的。清掉即"按会话重读"——用户再展开
      // 时按 ref 从当前会话的内容存储重读一次。
      resetCompactionViewState();
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
const workbenchSummaryView = createWorkbenchSummaryView(elements["workbench-summary-view"], {
  viewSessionID: () => client.current()?.session?.id || "",
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
  },
  // 提交记录的两层下钻：点开某一条 → 这次提交改了哪些文件；再点开某个文件 →
  // 它**在那个提交时**的内容（只读，字节来自 git 对象库，不会写回工作区）。
  // limit 是展示预算（视图侧给），后端另有自己的钳制上限。
  loadCommit: async (hash, limit) => invoke("WorkspaceGitCommitDetail", selectedGitRepo(), hash, limit),
  loadFile: async (hash, path, limit) => invoke("WorkspaceGitCommitFileContent", selectedGitRepo(), hash, path, limit)
});
// 工作区更改面板：行点击复用同一个文件详情抽屉（已删除的文件不可点）。
const workspaceChangesView = createWorkspaceChangesView(elements["changes-view"], {
  onOpenFile: entry => openFilePreview(entry)
});
// 文件预览（「资源管理器」子页左抽屉）：工作树文件点击 → 后端读取受控字节
// （containment/敏感过滤/上限在 workspace 层保证）→ 按类型分派渲染。
// 容器是「多文件详情」：每个文件一枚上标 chip + 一个独立面板；最后一个 chip
// 关闭（容器为空）时回调 onEmpty → 抽屉收起、子页恢复原来大小（工作树/提交记录
// 重新占满）。
//
// 编辑面（Ctrl+S 保存，见 file-preview.js）：writer 是落盘通道，confirmSave 是
// 「还有未保存内容」时的保存选择弹窗，onSaved 在保存成功后把改动面刷回资源管理器
// （文件的 git 状态变了，工作区更改面板必须跟上）。
const filePreviewController = createFilePreviewController({
  view: elements["file-preview-view"],
  tabsHost: elements["file-preview-tabs"],
  loader: async (entry, kind, limit) => invoke("WorkspaceFileContent", entry.path, limit),
  writer: async (entry, text) => {
    // 工作区切换守卫：面板里的文件属于打开它的那个工作区。切换后若还拿着旧面板
    // 保存，同样的相对路径会落到新工作区里的另一个文件上——静默覆盖别人的文件是
    // 最不可接受的失败面，因此宁可显式拒绝（重新打开该文件即恢复可保存）。
    const currentRoot = client.current()?.current_workspace?.root_path || "";
    if (previewRoot && currentRoot && currentRoot !== previewRoot) {
      throw new Error("工作区已切换，请重新打开该文件后再保存");
    }
    return invoke("WorkspaceWriteFile", entry.path, text);
  },
  confirmSave: confirmPreviewSave,
  onSaved: () => { refreshExplorerPages(["changes"]); },
  onNotice: message => showToast(message),
  onError: showToast,
  onEmpty: () => closeFilePreview()
});
let previewPaneOpen = false;
let previewPanesHidden = false;
// previewCollapsed = 详情抽屉收成竖轨、内容页（工作树/提交记录）独占子页。
let previewCollapsed = false;
let previewRoot = "";
// reviewAppliedKey 是最近一次**已应用**的「请 GUI 打开这些待审阅文件」召唤（会话 ID +
// request_id，见 review-request.js）。同一个 key 再次到达 = 同一份快照的补放：不再开
// tab、不抢当前文件（契约 dto.ReviewRequest 的"补放"口径）。用户关掉抽屉后它保持不动，
// 因此"人可忽略、可撤销"这件事是**结构性**的——不靠额外状态，只靠这个水位不被回退。
let reviewAppliedKey = "";
elements["file-preview-close"].addEventListener("click", closeFilePreview);
elements["file-preview-hide-panes"].addEventListener("click", togglePanesHidden);
// 「收起详情，让出内容页」：抽屉收成竖轨（内容页独占），竖轨本身是展开入口。
elements["file-preview-collapse"].addEventListener("click", togglePreviewCollapsed);
elements["file-preview-rail"].addEventListener("click", togglePreviewCollapsed);
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
// composer 的运行参数分两处：思考强度（"想多深"）贴发送键，循环次数（"跑多少轮"）
// 落在运行状态弹窗里（含「无限循环」勾选）。各自只改自己那一项，另一项一个字都不动。
// 原先还有一个 Effort 档位拉条（一次同时设置这两条读数的"预设"），已从 composer
// 下线：档位只经 `/effort` 命令切换，SwitchEffort 仍是 prompt 层 / plan 策略 /
// ReAct 预算的唯一入口，界面不再有一个能同时改两项的滑杆。
// 运行守卫与档位同一条（视图会话 running 时后端拒绝），所以两条一起上锁（含勾选）。
const composerKnobs = createComposerKnobs({
  reasoning: {
    root: elements["reasoning-effort-control"],
    input: elements["reasoning-effort-range"],
    output: elements["reasoning-effort-value"]
  },
  loop: {
    root: elements["loop-limit-control"],
    input: elements["loop-limit-range"],
    output: elements["loop-limit-value"]
  },
  infinite: {
    checkbox: elements["loop-limit-infinite"]
  },
  setReasoningEffort: async value => {
    const applied = await invoke("SetReasoningEffort", value);
    await refresh({ scroll: false });
    if (isViewActive(dockState, "trajectory")) refreshPromptInjection();
    return applied;
  },
  setLoopLimit: async limit => {
    const applied = await invoke("SetLoopLimit", limit);
    await refresh({ scroll: false });
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

function renderBranchOrigin(snapshot) {
  const host = elements["branch-origin"];
  if (!host) return;
  const origin = branchOriginModel(snapshot?.session?.forked_from, snapshot?.sessions || []);
  if (!origin) {
    host.classList.add("hidden");
    host.replaceChildren();
    return;
  }
  host.classList.remove("hidden");
  host.innerHTML = `<span class="branch-origin-mark" aria-hidden="true">${icon("branch", 14)}</span>
    <span>从 <strong>${escapeHtml(origin.parentName)}</strong> 分叉</span>
    <button type="button" class="branch-origin-return" data-return-parent="${escapeHtml(origin.parentID)}" aria-label="${escapeHtml(origin.returnLabel)}">返回父会话</button>`;
}

elements["branch-origin"]?.addEventListener("click", async event => {
  const button = event.target.closest?.("[data-return-parent]");
  if (!button || button.disabled) return;
  button.disabled = true;
  try {
    await resumeSessionFromList(button.dataset.returnParent || "");
  } finally {
    button.disabled = false;
  }
});

async function refresh(options = {}) {
  return client.refresh(options);
}

// markComposerEdited 记录一次本地编辑：置脏并按需防抖落盘。程序化写入
// （召回排队消息、接受建议）之后也要走它，否则这些内容会被当成"没编辑过"。
// 顺带重画草稿区的引用 chip 条（J-1b）：正文每次变化都从正文重新识别引用，
// 于是 `@文件` / `+` / 拖拽三个入口落进草稿后都自动长出一枚同样的 chip。
function markComposerEdited() {
  composerDirty = true;
  renderComposerFileRefs();
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
    // 正文换了会话：草稿 chip 条跟着换（否则会留下上一个会话的引用 chip）。
    renderComposerFileRefs();
  }
  // 正文换了归属：上一个会话的内联建议（命令/插件/技能候选，以及文件引用候选）已不适用。
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
  renderComposerFileRefs();
  elements.prompt.setSelectionRange(elements.prompt.value.length, elements.prompt.value.length);
}

// composerDraftPageText 已撤回：未发送输入不再作为页面 context 里的草稿行出现
// （draft 语义改由「未完成会话」承担）。保留该函数只为调用点稳定，恒返回空串。
function composerDraftPageText() {
  return "";
}

// 工具记录看板（2026-10-15，V10 会话粒度）：当前视图会话自己的读数，因此**按需拉取**
// （Bridge.ToolRecordBoard），不进会话快照。触发面：整轮渲染后一次、每次工具完成帧后一次、
// 以及用户点柱子/拨开关之后——成功与失败都会改变这份数据（开了计数之后两条都记）。
//
// 拉取失败（未装配台账 / 桥不可用）→ 静默保持现状，不弹 toast：它是只读读数，
// 拿不到不等于用户做错了什么。
let toolRecordStyleInjected = false;
// 选中的工具名（点柱子）：选中状态只影响"下面那张原因饼图"，不进任何后端状态。
let toolRecordSelection = "";

async function renderToolRecordPanel() {
  const section = elements["tool-record-section"];
  const host = elements["tool-record-view"];
  if (!section || !host) return;
  let board = null;
  try {
    board = await invoke("ToolRecordBoard");
  } catch {
    return;
  }
  const enabled = Boolean(board?.enabled);
  const empty = !enabled && Number(board?.total || 0) === 0;
  if (empty) {
    // 计数关着且没有历史读数：整块退场（关就是关，不留空壳）。开着但还没记过时
    // 仍然显示（表头那个开关要让用户看得见、能拨）。
    section.classList.add("hidden");
    host.innerHTML = "";
    return;
  }
  if (!toolRecordStyleInjected) {
    // 样式与渲染件同源（TOOL_RECORD_CSS 一处给出），注入一次即可。
    const style = document.createElement("style");
    style.id = "tool-record-styles";
    style.textContent = TOOL_RECORD_CSS;
    document.head.appendChild(style);
    toolRecordStyleInjected = true;
  }
  if (toolRecordSelection && !(board?.tools || []).some(row => row.tool === toolRecordSelection)) {
    toolRecordSelection = ""; // 选中的工具消失了（台账清空/换了机器）：不要留一个指向不存在柱子的选择
  }
  section.classList.remove("hidden");
  host.classList.remove("muted");
  host.innerHTML = renderToolRecordBoard(board, { selectedTool: toolRecordSelection });
  const badge = elements["tool-record-total"];
  if (badge) badge.textContent = String(Number(board?.total) || 0);
}

// 工具记录看板的交互（两件事，都只走既有桥方法）：
//   1. 点柱子 → 选中/取消选中，重画（选中才画下面那张原因饼图）；
//   2. 拨开关 → 走 Bridge.SetToolRecordEnabled（落盘在数据根 runtime-limits.json，
//      失败就回滚到后端返回的生效值——与其它设置控件同一条纪律）。
// 用**事件委托**而不是逐柱绑监听：柱子会随每次刷新重建，逐柱绑会漏绑/泄漏。
// 注意：计数开关**不在**这块看板上（它在设置弹窗里，见 syncToolRecordSetting）。
// 这里只绑柱子的选择——与 TUI 面板同口径：看板只读读数。
function bindToolRecordPanel() {
  const host = elements["tool-record-view"];
  if (!host) return;
  host.addEventListener("click", async event => {
    const bar = event.target.closest?.("[data-tool-record-tool]");
    if (!bar) return;
    const tool = bar.getAttribute("data-tool-record-tool") || "";
    toolRecordSelection = toolRecordSelection === tool ? "" : tool;
    await renderToolRecordPanel();
  });
  // 键盘可达：柱子是 role=button + tabindex=0，回车/空格等价于点击。
  host.addEventListener("keydown", async event => {
    if (event.key !== "Enter" && event.key !== " ") return;
    const bar = event.target.closest?.("[data-tool-record-tool]");
    if (!bar) return;
    event.preventDefault();
    const tool = bar.getAttribute("data-tool-record-tool") || "";
    toolRecordSelection = toolRecordSelection === tool ? "" : tool;
    await renderToolRecordPanel();
  });
}

function render(snapshot, options = {}) {
  const started = performance.now();
  // 输入框正文先按会话归属对齐，再谈回填（顺序见 syncComposerSession）。
  syncComposerSession(snapshot);
  restoreComposerDraft(snapshot);
  renderSessions(snapshot.sessions || [], snapshot.session || {}, snapshot.capabilities || {}, snapshot.session_workspaces || {}, snapshot.workspaces || []);
  renderBranchOrigin(snapshot);
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
  renderTeam(snapshot);
  renderScheduledTaskPanel(snapshot.runtime || {});
  renderToolRecordPanel();
  renderSelectedPluginSkills(snapshot.runtime || {});
  renderInteraction(snapshot.interaction);
  // J-2：agent 请求打开的待审阅文件（会话作用域的可重放状态，随快照到达）。放在这里
  // 而不是增量分支——后端按整份快照事件发布（见 application/core/review_files.go），
  // 前端只需在快照渲染时按 request_id 幂等地补放一次（见 applyReviewRequest）。
  applyReviewRequest(snapshot);
  syncSessionChrome();
  // 限高滚动块刚被重绘：补一次边缘阴影（增量/滚动期间由委托监听维护）。
  refreshScrollShadows();
  perfHooks.markRender(performance.now() - started);
}

function renderIncremental(snapshot, kind, payload) {
  if (!snapshot) return;
  const started = performance.now();
  if (["message.added", "message.delta", "tool.started", "tool.completed"].includes(kind)) {
    chatView.renderConversation(
      snapshot.conversation || [],
      snapshot.chat || {},
      "auto",
      snapshot.has_more_history,
      snapshot.session?.status === "restoring",
      Boolean(state.resumingSessionID),
      composerDraftPageText(snapshot),
      snapshot.task?.context_compactions || [],
      snapshot.conversation_revision || 0
    );
    chatView.renderControls(snapshot, Boolean(state.resumingSessionID));
    renderTrajectory(snapshot);
    // 新用户输入会多出一条索引刻度（助手增量不动索引），按指纹去重后拉取。
    if (kind === "message.added") refreshInputIndex(snapshot);
    if (kind !== "message.delta") renderProject(snapshot);
    // 工具记录只被"一次工具调用"改变：完成帧到了才拉一次看板（会话级读数，
    // 不进快照；见 renderToolRecordPanel 的口径说明）。
    if (kind === "tool.completed") renderToolRecordPanel();
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
    renderTeam(snapshot);
    renderScheduledTaskPanel(snapshot.runtime || {});
    renderSelectedPluginSkills(snapshot.runtime || {});
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
  if (kind === "teammate.tool.started" || kind === "teammate.tool.completed") {
    // 员工在做工：这是**逐帧**的实时钩子（subagent.tool.* 的员工侧对称面）。载荷不进
    // 快照（员工的权威记录在角色会话里），所以这里不整份重绘、也不跑 RPC——把这一帧并进
    // 员工详情的实时区即可。开着的正是这一位时立刻重绘；否则只记忆（切过去带出来）。
    //
    // 修前事实：员工侧只有 team.changed（装配/母本 CRUD 才发）+ 手动刷新键，于是
    // "这一轮到底在干什么"要等它跑完才看得到（2026-10-03 现场）。
    applyTeammateToolActivity(payload);
    return;
  }
  if (kind === "interaction.opened" || kind === "interaction.closed") renderInteraction(snapshot.interaction);
  if (kind === "team.changed") {
    // 团队事实变了的**通告**：载荷不在快照里（按需 RPC 拉取），所以 reducer 只透传
    // kind。Agent Team 面板已退场（2026-10-12 J-1a），这条事件现在只服务**角色会话
    // 只读观察面**（团队看板「当前会话」入口）：视图开着时重取当前目标，关着时不做
    // 任何事（没有"最后心跳时间"这类推着走的东西）。发声方是后端装配面，前端不从
    // 自己的输入文本里猜"这次提交会不会改团队"。
    if (roleSessionDetail) refreshRoleSessionDetail();
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
//
// 委托宿主必须是 elements["message-queue"]（输入框正上方那一层），不能是
// elements.conversation：宿主 #message-queue 在 index.html 里是 #conversation /
// #conversation-shell 的兄弟节点，排队条压根不在对话滚动区内，按钮的 click 不会
// 冒泡到对话区——挂在对话区上时上移/下移/撤回三颗按钮全死（现场「队列的东西回退不了」）。
elements["message-queue"].addEventListener("click", async event => {
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
    // 批量创建可能已写入前几项后才遇到 I/O 错误：刷新权威项目表，并从 temp list
    // 移除已经在后端的根路径，避免重试时拿同一目录再创建一次。
    try {
      await refresh({ scroll: false });
      const existingRoots = new Set(
        (client.current()?.workspaces || []).map(workspace => normalizePath(workspace.root_path))
      );
      newProjectDrafts = newProjectDrafts.filter(draft => !existingRoots.has(normalizePath(draft.rootPath)));
      renderNewSessionWorkspaces();
    } catch {
      /* 刷新失败时保留 temp list，让用户看得到尚未处理的输入。 */
    }
    showToast(error);
  } finally {
    button.disabled = false;
  }
});

// 排队条拖拽换序：把手（data-queue-drag）是拖动源，同宿主里的另一条是落点。
// 落点只标一条缝（上半 = 插到它前面，下半 = 插到它后面），换算交给纯函数
// queueDragTarget——后端口径是"条目最终落在 to 位置"，与纸面落点差一层（见
// components.js 的注释与 queue-edit.test.mjs 的用例）。
// 载荷只写自定义 mime：写 text/plain 会被 file-drop.js 的"跨窗口兜底"当成工作树
// 路径读走，一次排序就会变成一条"读文件"的排队消息。
const QUEUE_DRAG_MIME = "application/x-seelex-queue";
const queueRows = () => elements["message-queue"].querySelectorAll(".queued-message");
function clearQueueDropMarks() {
  for (const row of queueRows()) row.classList.remove("is-drop-before", "is-drop-after", "is-dragging");
}
function queueDropSeam(event, row) {
  const box = row.getBoundingClientRect();
  return { hoverIndex: Number(row.dataset.queueIndex), after: event.clientY > box.top + box.height / 2 };
}
elements["message-queue"].addEventListener("dragstart", event => {
  const handle = event.target.closest?.("[data-queue-drag]");
  if (!handle) return;
  event.dataTransfer?.setData(QUEUE_DRAG_MIME, String(handle.dataset.queueDrag ?? ""));
  if (event.dataTransfer) event.dataTransfer.effectAllowed = "move";
  handle.closest(".queued-message")?.classList.add("is-dragging");
});
elements["message-queue"].addEventListener("dragover", event => {
  if (!event.dataTransfer?.types?.includes(QUEUE_DRAG_MIME)) return;
  const row = event.target.closest?.(".queued-message");
  if (!row) return;
  event.preventDefault(); // 不拦住默认行为就不会有 drop
  event.dataTransfer.dropEffect = "move";
  const { after } = queueDropSeam(event, row);
  for (const other of queueRows()) {
    if (other === row) continue;
    other.classList.remove("is-drop-before", "is-drop-after");
  }
  row.classList.toggle("is-drop-before", !after);
  row.classList.toggle("is-drop-after", after);
});
elements["message-queue"].addEventListener("dragend", clearQueueDropMarks);
elements["message-queue"].addEventListener("drop", async event => {
  const row = event.target.closest?.(".queued-message");
  if (!row || !event.dataTransfer?.types?.includes(QUEUE_DRAG_MIME)) return;
  event.preventDefault();
  const from = Number(event.dataTransfer.getData(QUEUE_DRAG_MIME));
  const { hoverIndex, after } = queueDropSeam(event, row);
  const sessionID = client.current()?.session?.id || "";
  const length = (client.current()?.chat?.input_queue || []).length;
  const target = queueDragTarget(from, hoverIndex, length, after);
  clearQueueDropMarks();
  if (!target) return; // 原地落下：不打扰后端，也不刷快照
  try {
    await invoke("ReorderQueuedInput", sessionID, target.from, target.to);
    await refresh({ scroll: "auto" });
  } catch (error) {
    showToast(error);
  }
});

// recallQueuedInput 把撤回的排队原文交还输入框重新编辑：输入框已有未发送
// 正文时把撤回内容追加在后（不覆盖用户草稿），随后聚焦并把光标放到末尾。
function recallQueuedInput(text) {  const recalled = String(text ?? "");
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
  // 仓库标签栏只在 git 两个子页上出现：切页后重算一次显隐。
  renderGitRepoTabs();
}

// 子页内的点击走容器委托（一条监听）：页签切换 + 各子页头部的刷新按钮。页签是真
// 按钮，Enter/Space 由浏览器转成 click，不必另写键盘分支。
elements["code-panes"]?.addEventListener("click", event => {
  const refreshButton = event.target.closest?.("[data-page-refresh]");
  if (refreshButton) {
    const page = refreshButton.dataset.pageRefresh;
    // git 两个子页的刷新先重发现仓库（新仓库/被删仓库要能反映到标签栏）。
    if (page === "gitlog" || page === "changes") {
      refreshGitPages([page]);
      return;
    }
    refreshExplorerPages([page]);
    return;
  }
  const repoTab = event.target.closest?.("[data-git-repo]");
  if (repoTab) {
    selectGitRepo(repoTab.dataset.gitRepo);
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
  if (page === "gitlog") return invoke("WorkspaceGitLog", selectedGitRepo(), 20);
  if (page === "changes") return invoke("WorkspaceChanges", selectedGitRepo(), 200);
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

// ── 多仓库 git：仓库子页标签 ─────────────────────────────
// 一个工作区下可能有多棵互不相干的 git 仓库（根本身是仓库 / 根下若干子目录各自
// 是仓库 / 仓库里套仓库）。提交记录与工作区更改**共用同一个选中仓库**：标签栏
// 只列仓库，选中哪个就只显示那个仓库的 git，其余仓库作为子页标签等用户翻。
//
// 三条纪律（对应 git-repos.js 的纯函数）：
//   - 只有一个仓库时不显示标签栏（保持原观感）；
//   - 选择按工作区 ID 记忆（gitRepoSelectionByWorkspace），快照刷新不重置；
//     工作区切换即重置（整张记忆表清空，回落 primary）；
//   - 仓库清单变化时用 resolveSelectedRepo 收敛：选中的那个没了就回落。
const gitRepoSelectionByWorkspace = new Map();

// gitReposState 是最近一次仓库发现的读数（只读缓存；root 只用于判断"同一份"）。
let gitReposState = { workspaceID: "", root: "", repos: [], selected: "", truncated: false, scanned: 0, error: "" };

function currentWorkspaceID() {
  return client.current()?.current_workspace?.id || "";
}

// gitRepoTabsModel 是标签栏模型的唯一出口：显隐、单选、回落都在 git-repos.js。
function gitRepoTabsModel() {
  return buildRepoTabs(gitReposState.repos, gitReposState.selected);
}

// selectedGitRepo 返回当前选中仓库的相对路径（"" = 工作区根，与旧的单仓库查询
// 完全等价：后端把空串按根处理）。
function selectedGitRepo() {
  return gitRepoTabsModel().selected || "";
}

// renderGitRepoTabs 重绘标签栏：只有仓库多于一个、且当前激活的是 git 子页（提交
// 记录 / 工作区更改）时才显示。
function renderGitRepoTabs() {
  const host = elements["git-repo-tabs"];
  if (!host) return;
  const model = gitRepoTabsModel();
  host.innerHTML = renderRepoTabs(model);
  const gitPage = explorerState.active === "gitlog" || explorerState.active === "changes";
  const visible = model.show && gitPage;
  host.classList.toggle("is-hidden", !visible);
  // 容器行模板跟着换成三行（见 styles.css 的 .code-panes.has-git-repos）。
  elements["code-panes"]?.classList.toggle("has-git-repos", visible);
}

// selectGitRepo 记下这次选择（按工作区 ID）并只刷新两个 git 子页。
function selectGitRepo(rel) {
  if (typeof rel !== "string" || rel.trim() === "") return;
  const workspaceID = currentWorkspaceID();
  if (workspaceID) gitRepoSelectionByWorkspace.set(workspaceID, rel.trim());
  gitReposState = { ...gitReposState, selected: rel.trim() };
  renderGitRepoTabs();
  refreshExplorerPages(["gitlog", "changes"]);
}

// refreshGitRepos 拉一次仓库清单（只读有界扫描）并收敛选中项：记忆里的选择还在
// 就继续用它，不在了就回落 primary / 第一项。发现失败不阻断面板——退回"只有工作
// 区根"的单一仓库，两个 git 子页照常按根查询。
function refreshGitRepos() {
  const workspaceID = currentWorkspaceID();
  return invoke("WorkspaceGitRepos").then(result => {
    const repos = Array.isArray(result?.repos) ? result.repos : [];
    const memorised = workspaceID ? gitRepoSelectionByWorkspace.get(workspaceID) || "" : "";
    const selected = resolveSelectedRepo(repos, memorised);
    if (workspaceID && selected) gitRepoSelectionByWorkspace.set(workspaceID, selected);
    gitReposState = {
      workspaceID,
      root: result?.root || worktreeRoot,
      repos,
      selected,
      truncated: result?.truncated === true,
      scanned: Number(result?.scanned || 0) || 0,
      error: typeof result?.error === "string" ? result.error : ""
    };
    renderGitRepoTabs();
    return gitReposState;
  }).catch(error => {
    gitReposState = {
      workspaceID, root: worktreeRoot, repos: [], selected: "",
      truncated: false, scanned: 0, error: String(error?.message || error)
    };
    renderGitRepoTabs();
    return gitReposState;
  });
}

// refreshGitPages 是 git 两个子页刷新按钮的唯一入口：先重发现仓库，再按（可能
// 已经收敛过的）选中仓库拉数据面。
function refreshGitPages(pages) {
  return refreshGitRepos().then(() => refreshExplorerPages(pages));
}

// resetGitRepoState 在换工作区/解绑时清空仓库读数与选择记忆（旧根的仓库清单绝不
// 留在新根的标签栏上）。
function resetGitRepoState() {
  gitRepoSelectionByWorkspace.clear();
  gitReposState = { workspaceID: "", root: "", repos: [], selected: "", truncated: false, scanned: 0, error: "" };
  renderGitRepoTabs();
}


// ── 上下文压缩条目：点条目 → 弹框查看压缩帧正文 ───────────────
// 帧正文不进快照（快照只带 frame_ref），打开弹框时按 ref 分页读回；条目只有一行
// 元数据摘要，正文走可调大小的弹框（#compaction-frame-modal）。容器与分页组件与
// 轨迹详情同一套（.axis-detail + data-compact-frame-load），不自造第二套面板。
//
// 帧正文视图态的身份是 **(会话, frame_ref)**，不是记录数组下标：下标既会随记录数组
// 重排，更会随会话切换指向另一条记录——上一个会话读回来的正文挂在当前会话的同一
// 序号行上，就是把别的会话的压缩说成当前会话的。ref 是内容存储里的引用，跨会话不会
// 撞；会话字段把"这份正文属于谁"写进视图态（渲染侧据此拒绝跨会话正文，见
// renderContextCompactions 的 sessionID 选项），会话切换时整份清空（见
// resetCompactionViewState：用户再打开就按 ref 从当前会话重读一次）。
function emptyCompactionDetail() {
  return { ref: "", sessionID: "" };
}

let compactionDetail = emptyCompactionDetail();
let contextCompactionsBound = false;

// clearCompactionDetail 忘掉"当前正在读哪一帧"（右栏据此标打开态，续读只认它）。
function clearCompactionDetail() {
  compactionDetail = emptyCompactionDetail();
}

// compactionViewSessionID 返回当前**视图会话**：帧正文视图态按它归属。
function compactionViewSessionID() {
  return String(client.current()?.session?.id || "");
}

// openedCompactionRef 返回当前正在读的那一帧的 ref（会话 + ref 身份）。续读与重试
// 只认视图态里存的 ref：从 compactions[index] 重取会让重排后的续页接到别人尾巴上。
function openedCompactionRef() {
  const ref = String(compactionDetail.ref || "");
  return ref;
}

// resetCompactionViewState 清空帧正文视图态（右栏展开条目 + 帧正文弹框），由会话
// 切换调用（见 onSnapshot 的会话切换分支）。
//
// 为什么必须整份清而不是只靠 ref 判定：帧正文是**按 ref 从当前会话的内容存储**
// 读回来的，上一个会话读回来的正文还挂在视图里，就是"把别的会话的压缩说成当前
// 会话的"。清掉之后用户再展开会按 ref 重读一次——即用户口径的"根据会话重读压缩帧"。
// 在途读取一并作废（两枚 token 都推进）：它们的响应不得落回切换后的视图。
function resetCompactionViewState() {
  clearCompactionDetail();
  compactionFrameLoadToken += 1;
  compactionFrameModal = { record: null, detail: null };
  setModal("compaction-frame-modal", false);
}

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

// onContextCompactionsClick 一条委托监听：右栏条目的单一动作「查看帧正文」
// （data-compact-open 带的是这条记录在**当时**数组里的下标，只用来取这一刻的
// frame_ref）。再点同一行 = 收起弹框（行内展开路径已下线）。
//
// 身份一律是 (会话, ref)：按钮上的下标只用来**取**这一行的 ref，此后"是否已打开"、
// 续读与重试都只认视图态里存的会话 + ref——不然记录数组一变（快照刷新 / 会话切换），
// 续读就会把另一条记录的续页接到当前正文尾巴上。
async function onContextCompactionsClick(event) {
  const openButton = event.target?.closest?.("[data-compact-open]");
  if (!openButton) return;
  const compactions = currentCompactions();
  const ref = String(compactions[Number(openButton.dataset.compactOpen)]?.frame_ref || "");
  if (!ref) return; // 没有帧引用的行本来就没有入口
  if (compactionDetail.ref === ref) {
    closeCompactionFrame();
    return;
  }
  await openCompactionFrame(ref);
}

// repaintCompactions 是右栏「上下文压缩」面板的唯一出口：记录列表 + 本轮门禁过程
// 一起画，可见性判据必须同源——压缩发生在写记录之前，"零记录"时面板仍可能有一轮
// 压缩正在跑，按记录数判隐藏会让第一次压缩看不到门禁过程。
//
// 块本身挂在 状态/概要 折叠区**里面**（用户口径：压缩内容放概要下面），折叠区默认
// 收起——所以"本轮正在压 / 刚压完"这条瞬态要自己把折叠区打开（见 revealStatusPanel）；
// 历次记录不触发打开：那是用户展开 状态 才读的静态事实。
function repaintCompactions(compactions) {
  const host = elements["context-compactions"];
  if (!host) return;
  const list = Array.isArray(compactions) ? compactions : [];
  // sessionID 一起给渲染侧：打开态那一行按 (会话, ref) 判定，上个会话的身份不许认。
  host.innerHTML = renderContextCompactions(list, {
    detail: compactionDetail, progress: compactionProgress, sessionID: compactionViewSessionID()
  });
  host.classList.toggle("hidden", list.length === 0 && !compactionProgress);
  if (compactionProgress) revealStatusPanel();
}

// revealStatusPanel 把 状态 折叠区打开（只开不收）：不这么做，块一挪进折叠区，
// "按下回车到底动没动"又回到"用户得先想起去展开 状态"的老问题。
function revealStatusPanel() {
  const panel = document.getElementById("status-panel");
  if (panel && !panel.open) panel.open = true;
}

// ── 压缩帧正文弹框 ───────────────────────────────────────────
// 帧正文入口（data-compact-frame-ref）出现在对话区「以上已压缩」分界行等处，右栏
// 条目走自己的委托（data-compact-open，见 onContextCompactionsClick）；两者都汇到
// openCompactionFrame。组件侧只携带 ref（不持有 invoke 依赖）。
//
// 弹框是帧正文的**唯一**展示面（行内展开已下线）：正文区来自 renderFrameDetail，
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
  // 记下"当前正在读哪一帧"（会话 + ref）：右栏据此把该行标成打开态，续读/重试只认
  // 它。记录找不到时也照记——弹框照样打开，身份照样有。
  compactionDetail = { ...emptyCompactionDetail(), ref, sessionID: compactionViewSessionID() };
  compactionFrameModal = { record, detail: ref ? emptyCompactionFrameDetail() : null };
  repaintCompactions(currentCompactions());
  repaintCompactionFrameModal();
  setModal("compaction-frame-modal", true);
  if (record && ref) await loadCompactionFramePage(ref, 0, false);
}

function closeCompactionFrame() {
  compactionFrameLoadToken += 1;
  setModal("compaction-frame-modal", false);
  compactionFrameModal = { record: null, detail: null };
  // 忘掉"正在读哪一帧"并重绘右栏：打开态的那一行要跟着暗下来。
  clearCompactionDetail();
  repaintCompactions(currentCompactions());
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
    elements["compaction-frame-modal-title"].textContent = version > 0 ? `压缩帧正文 · 压缩 #${version}` : "压缩帧正文";
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
  const ref = openedCompactionRef();
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
  const projectSettingsButton = elements["project-settings-button"];
  if (projectSettingsButton) {
    projectSettingsButton.disabled = !workspace?.id;
    projectSettingsButton.title = workspace?.id
      ? `管理 ${workspace.name || "当前项目"} 的环境`
      : "先选择一个项目，再管理环境";
  }
  if (elements["project-settings-name"]) {
    elements["project-settings-name"].textContent = workspace?.id
      ? `${workspace.name || "当前项目"} · ${workspace.root_path || ""}`
      : "当前没有绑定项目";
  }
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
  // 仓库清单是 git 两个子页的共同前提：先发现仓库（并收敛选中项），再按选中仓库
  // 拉数据面——否则面板会先用旧选中仓库发一轮请求，再多发一轮。
  refreshGitRepos().then(() => refreshExplorerPages(EXPLORER_PAGES));
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
  // 仓库标签栏与选中记忆属于上一个根：一起清（回落 primary）。
  resetGitRepoState();
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
  // 当前会话的补行：目录可能慢一拍（例如刚物化、目录刷新还在路上），补一行
  // 免得视图会话在侧栏里"没有自己"。**草稿不补**：草稿行由后端的草稿槽下发
  // （status=draft，带工作区绑定），而一份还没发送输入的草稿不属于会话树——
  // 前端补出来的那行既没有工作区归属（只能落进「未关联会话」），也没有标题，
  // 正是"新建会话一开始跑到未关联里"的第二现场。
  if (currentID && !current.draft && !items.some(session => session.id === currentID)) {
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
  if (data.renameSession) {
    beginSessionRename(data.renameSession);
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
  showLeftPage("tasks");
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

// beginSessionRename 让会话行就地改名（Enter 提交 / Esc 或失焦取消）。
//
// 为什么改名走 SetSessionMeta 的 alias 而不是另开一个"改名"接口：展示名本来就是
// 会话元数据的一部分（置顶 / 别名 / 排序同一条记录），别名一旦写进去，目录刷新后
// 所有窗口看到的都是同一个名字——浏览器不自己记一份"我改过名"。
// 留空 = 清掉别名（展示回落到自动生成的标题），这也是"改错了怎么退回去"的答案。
function beginSessionRename(sessionID) {
  closeSessionMenu();
  const row = document.querySelector(`[data-session-row="${CSS.escape(sessionID)}"]`);
  const title = row?.querySelector(".session-title");
  if (!row || !title || row.querySelector(".session-rename-input")) return;
  const meta = sessionMetaByID(sessionID);
  const input = document.createElement("input");
  input.type = "text";
  input.className = "session-rename-input";
  input.value = meta.alias || "";
  input.placeholder = "会话名（留空 = 用默认标题）";
  input.maxLength = 120;
  title.classList.add("hidden");
  row.insertBefore(input, title);
  input.focus();
  input.select();

  let settled = false;
  const finish = async commit => {
    if (settled) return;
    settled = true;
    input.remove();
    title.classList.remove("hidden");
    if (!commit) return;
    const alias = String(input.value || "").trim();
    try {
      await invoke("SetSessionMeta", sessionID, Boolean(meta.pinned), alias, meta.sort_order || 0);
      await refresh({ scroll: false });
    } catch (error) {
      showToast(error);
    }
  };
  input.addEventListener("keydown", event => {
    if (event.key === "Enter") { event.preventDefault(); finish(true); }
    if (event.key === "Escape") { event.preventDefault(); finish(false); }
  });
  input.addEventListener("blur", () => finish(false));
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
  // 输入框会因换行/字体重排而变高（--composer-h 是队列叠与对话区留白的依据）。
  syncComposerMetrics();
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
    <button type="button" class="session-menu-item" role="menuitem" data-rename-session="${escapeHtml(sessionID)}">${icon("message", 13)}重命名会话</button>
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

  // composer 两条独立拉条的权威读数（缺字段的旧投影/冷加载空 runtime 由组件自己忽略）。
  composerKnobs.setReasoningEffort(runtime.reasoning_effort);
  composerKnobs.setLoopLimit(runtime.loop_limit);
}

// 纯函数在 ./permission-tier.js（nextTierIndex 用来给标签做 ↑/↓ 循环）与
// ./role-settings.js（标签表 / 三态 / lint），这里只做 DOM 写入。
//
// renderPermissionTier 渲染 composer 的**主代理权限向量**入口（2026-10-23 改口径）。
//
// 为什么不再是档位：档位（手动 / 改文件免问 / 命令免问 / 全权）只能"往松里拨一格"，
// 表达不了"只给 SSH 不给写"或"明确拒掉 SSH"这类逐格点名——而权限装配已经是按组配的、
// ssh 是唯一远端能力。所以底部这颗芯片渲染的是**本会话主代理的向量**，
// 点开就地展开同一组标签（三态：空 / 给 / 显式拒绝），改一格立即整组提交。
//
// 芯片读的是 permissionVectorCommitted（后端回执），不读档位目录——两处各记一份就会漂移。
function renderPermissionTier(runtime) {
  renderPermissionVectorChip();
  renderPermissionMenu(runtime);
  // 运行状态弹窗里只放**只读镜像**：同一组标签在这里看一眼，改仍走芯片/设置面板
  // （`inert` 让这排标签不可聚焦、不可点——不然会变成第二个“点了没反应”的入口）。
  const host = elements["permission-tier-list"];
  if (!host) return;
  host.innerHTML = `<div class="perm-tag-row" inert>${renderPermissionVectorHTML(permissionVectorCommitted)}</div>`
    + `<div class="perm-menu-hint">在输入框下方的权限芯片里修改（点一下给 / 两下拒 / 三下空）</div>`;
}

// renderPermissionVectorChip 芯片内容 = 主代理当前向量的一行读数（空向量说"未选"）。
// 不再有"全权"这一档：全权 = 七个标签全给，那就是一行读数本身，不需要另设一枚芯片值。
function renderPermissionVectorChip() {
  const chip = elements["perm-toggle"];
  if (!chip) return;
  const summary = permissionVectorSummary(permissionVectorCommitted);
  chip.classList.remove("is-on");
  chip.dataset.permission = "vector";
  chip.innerHTML = `${escapeHtml(summary)} <span class="perm-caret" aria-hidden="true">▾</span>`;
}

// renderPermissionMenu 把**同一组权限标签**写进芯片下拉：与设置面板共用
// renderPermissionVectorHTML（一份标签表、一份三态渲染），lint 提示一并给出（只提示、不拦）。
function renderPermissionMenu(runtime) {
  const menu = elements["perm-menu"];
  if (!menu) return;
  const warnings = permissionVectorLint(permissionVectorCommitted);
  const notes = warnings.length
    ? `<div class="perm-menu-hint">${warnings.map(item => escapeHtml(item.message)).join("<br/>")}</div>`
    : "";
  menu.innerHTML = `<div class="perm-menu-head">主代理权限（本会话）· 点一下给 / 两下拒 / 三下空</div>`
    + `<div class="perm-tag-row" role="group" aria-label="主代理权限向量">${renderPermissionVectorHTML(permissionVectorCommitted)}</div>`
    + notes
    + `<button type="button" class="perm-menu-action" data-open-session-permissions>按 Agent role 管理本会话权限</button>`;
}

// renderPlugins 渲染左栏「插件 Skill」页的插件列表 + 该插件的 Skill 目录。
//
// **这一页只看不切（2026 用户定调）**：点插件只是把本页停到它上面、列出它随包携带的
// Skill，**不调用 SwitchPlugin** —— 改进程级激活态只走 `#` 命令（input_router 的
// pluginRoute）与定时任务的装配面。因此：
//   - `runtime.plugin` 是**只读现状**：列表里用一枚「当前」点标记，页面状态也照它显示；
//   - `state.selectedPlugin` 是本页自己的停留位（纯视图，切走再回来还在）。
//
// 每个插件除名字 / 描述，还要回答"这个插件从哪来"——用户当面问过「这些前端显示出来的
// plugin 我没在我的 plugins/ 下见过」。来源读数（source_kind/source_url/source_root）
// 由后端下发，判定与文案全在 plugin-source.js 的纯函数里，这里只做**摆位 + 转义**。
// Skill 目录随插件一起下发（PluginInfo.skills）：插件页不能只靠 runtime.skills，
// 那是当前激活插件的合并视图，切走就看不到别的插件带什么。
let lastPluginsRuntime = null;

function renderPlugins(runtime) {
  lastPluginsRuntime = runtime;
  const plugins = runtime.plugins || [];
  const currentName = runtime.plugin || "";
  elements["plugin-count"].textContent = String(plugins.length);
  // 停留位回落顺序：仍存在的旧选择 > 当前激活插件 > 第一个插件。
  if (!plugins.some(plugin => plugin.name === state.selectedPlugin)) {
    state.selectedPlugin = plugins.some(plugin => plugin.name === currentName)
      ? currentName
      : (plugins[0]?.name || "");
  }
  const selected = plugins.find(plugin => plugin.name === state.selectedPlugin) || null;
  elements["plugin-list"].innerHTML = plugins.map(plugin => {
    const source = pluginSourceBadge(plugin);
    // 来源标签**可见**（不能只靠 tooltip）：local 必须一眼看出是"本机自建"；类型未登记
    // 时这枚标签是"来源未登记"（判据①：那是"不知道谁给的"，不是"随发行包"）。
    const badge = source.label
      ? `<span class="chip" title="${escapePluginSourceText(source.note)}">${escapePluginSourceText(source.label)}</span>`
      : "";
    const title = source.note ? ` title="${escapePluginSourceText(source.note)}"` : "";
    const isSelected = plugin.name === state.selectedPlugin;
    const isCurrent = plugin.name === currentName;
    const skillCount = Array.isArray(plugin.skills) ? plugin.skills.length : 0;
    return `
    <button class="stack-button plugin-entry${isSelected ? " selected" : ""}${isCurrent ? " is-current" : ""}" data-plugin="${escapeHtml(plugin.name)}" aria-pressed="${isSelected}"${title}>
      <span class="plugin-entry-name">${icon(isCurrent ? "dot" : "circle", 12)}${escapeHtml(plugin.name)}</span>
      <span class="chip plugin-entry-count" title="随包携带 ${skillCount} 个 Skill">${skillCount}</span>
      ${badge}
    </button>`;
  }).join("");
  if (elements["plugin-management-title"]) {
    elements["plugin-management-title"].textContent = selected?.name || "插件管理";
  }
  if (elements["plugin-management-description"]) {
    elements["plugin-management-description"].textContent =
      selected?.description || "浏览各插件随包携带的 Skill；这一页只做查看与管理，不切换当前插件。";
  }
  if (elements["plugin-management-state"]) {
    const isCurrent = selected && selected.name === currentName;
    elements["plugin-management-state"].textContent = selected ? (isCurrent ? "当前使用" : "仅查看") : "未选择";
    elements["plugin-management-state"].classList.toggle("is-on", Boolean(isCurrent));
  }
  if (elements["plugin-management-meta"]) {
    if (!selected) {
      elements["plugin-management-meta"].innerHTML = '<span class="chip">暂无插件</span>';
    } else {
      const source = pluginSourceBadge(selected);
      const skillCount = Array.isArray(selected.skills) ? selected.skills.length : 0;
      elements["plugin-management-meta"].innerHTML = [
        `<span class="chip">plugin ${escapeHtml(selected.name)}</span>`,
        `<span class="chip">${skillCount} 个 Skill</span>`,
        source.label ? `<span class="chip" title="${escapePluginSourceText(source.note)}">${escapePluginSourceText(source.label)}</span>` : "",
        source.hint ? `<span class="chip" title="${escapePluginSourceText(source.note)}">${escapePluginSourceText(source.hint)}</span>` : ""
      ].filter(Boolean).join("");
    }
  }
  renderSelectedPluginSkills(runtime);
  // 「角色设置 → 默认 plugin」下拉同源：插件清单随快照来，刷一次就重画一次。
  renderRoleSettingsOptions();
}

// 插件页的 Skill 目录：跟着**停留位**走（不是 runtime.plugin）。旧后端不带宽 skills
// 字段时回落到 runtime.skills（当前激活插件的合并视图），页面不至于空成一张白纸。
function renderSelectedPluginSkills(runtime) {
  const plugins = runtime.plugins || [];
  const selected = plugins.find(plugin => plugin.name === state.selectedPlugin) || null;
  const skills = selected && Array.isArray(selected.skills) ? selected.skills : null;
  if (skills) {
    renderSkillList(skills, `插件 ${selected.name} 没有随包携带 Skill。`);
    return;
  }
  renderSkillList(runtime.skills || [], "当前没有可召回的 Skill。");
}

// 插件页选插件：**纯视图**，只改停留位并重画本页（绝不 invoke SwitchPlugin）。
// 容器上一条委托（列表每次重绘不再逐行绑监听）。
elements["plugin-list"]?.addEventListener("click", event => {
  const button = event.target?.closest?.("button[data-plugin]");
  if (!button) return;
  const name = button.dataset.plugin || "";
  if (!name || name === state.selectedPlugin) return;
  state.selectedPlugin = name;
  if (lastPluginsRuntime) renderPlugins(lastPluginsRuntime);
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
  // 设置里的「角色设置 → 账号」下拉同源：快照一刷新就跟着重画（弹窗开着也不落队）。
  renderRoleSettingsOptions();
  // 余额、窗口与账户操作都挂在角色卡里的账号行上。
  renderPermissionRoleRows();
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

function renderSkillList(skills, emptyText = "当前插件没有可召回的 Skill。") {
  // Skill 的召回前缀是 `$`（`#` 是切换 Plugin、`/` 是命令）：与
  // application/core/input_router/router.go 的 skillRoute/pluginRoute 一字对应。
  // 这里曾写死成 `#`，于是用户用 `$goal` 召回的 Skill 在列表里显示成 `#goal`。
  if (elements["plugin-skill-count"]) elements["plugin-skill-count"].textContent = String(skills.length);
  elements["skill-list"].innerHTML = skills.length
    ? skills.map(skill => `<div class="plugin-skill-row">
        <span class="plugin-skill-icon" aria-hidden="true">${icon("skill", 14)}</span>
        <span class="plugin-skill-copy">
          <strong class="plugin-skill-name">${escapeHtml(skill.name)}</strong>
          <span class="plugin-skill-desc" title="${escapeHtml(skill.description || "")}">${escapeHtml(skill.description || "当前插件提供的 Skill")}</span>
        </span>
      </div>`).join("")
    : `<div class="plugin-skill-empty">${escapeHtml(emptyText)}</div>`;
}

// ── 「团队看板」面板（工作台子页）──────────────────────────
// 数据源：runtime.teamwork_board（后端只读投影：计划 + 作业行 + 审计流水）。
//
// 面板只做**搬运**：把 DTO 喂给纯渲染件 renderTeamBoard（team-board-view.js）——
// 里程碑屏障的拓扑排序、工作项 DAG 的层号与"被依赖卡住"折算全在渲染件里，这里不重算一遍
// （重算就是第二份事实）。单向：只读快照，前端没有任何写入口。
//
// 退场语义与「目标」面板同口径：没有计划（快照里没有 teamwork_board、或计划里没有里程碑
// 也没有工作项）就是没有了——整块退场，不留空壳。
//
// currentTeamPlan 是最近一帧看板计划（没有看板 = null）：只服务"会话入口"这一处
// （openRoleSessionDetail 解析"这位 teammate 此刻那件事的会话"，见那里的注释）。
let currentTeamPlan = null;

// currentTeamBoardInput 是最近一帧看板的**整份只读入参**（plan + jobs + events + 标记）：
// 详情页（里程碑 / Work Item / teammate）读的就是它。
//
// 为什么不从 DOM 里捞：详情页要的字段（描述 / 结论 / 作业行 / 审计流水）大多**没有**进侧边栏
// 那一份 HTML（那正是"侧边栏只报最小可读集"的意思）。要读全量，只能拿同一份投影。
let currentTeamBoardInput = null;

function renderTeam(snapshot) {
  const section = elements["team-board-section"];
  const view = elements["team-board-view"];
  const badge = elements["team-board-badge"];
  if (!section || !view) return;
  const input = teamBoardInput(snapshot?.runtime || {});
  if (input) input.mainSessionID = String(snapshot?.session?.id || "");
  // 「这位 teammate 此刻那件事的会话」的解析面留给会话入口用（见 openRoleSessionDetail）：
  // 团队看板是这条事实的权威投影（在编行的角色名/会话号都出自它），少了这一份，
  // 入口就只能退到角色会话（那是主代理的会话，不是 teammate 自己的）。
  currentTeamPlan = input ? input.plan : null;
  // 详情页与侧边栏同源同帧：快照一到，两边一起重绘（开着的详情页因此不会停在旧读数上）。
  currentTeamBoardInput = input;
  const html = input ? renderTeamBoard(input) : "";
  if (!html) {
    section.classList.add("hidden");
    if (badge) badge.classList.add("hidden");
    view.innerHTML = "";
    // 没有计划 = 详情页也没有可读的东西：关掉它，不留一个指向已消失计划的空壳。
    closeTeamPage();
    return;
  }
  ensureTeamBoardStyles();
  section.classList.remove("hidden");
  view.classList.remove("muted");
  view.innerHTML = html;
  refreshTeamPage();
  if (badge) {
    badge.classList.remove("hidden");
    // 徽标写里程碑数（与同栏「工作表格 / 定时任务」的计数徽标同口径）。不写 "TEAM"：
    // 渲染件自己在看板头里已经有一个 TEAM 标记，再来一个就是同屏两个 TEAM。
    // 也不写阶段数：阶段口径已退场，看板的单位是里程碑与工作项。
    const milestones = input.plan.milestones.length;
    const items = input.plan.work_items.length;
    badge.textContent = String(milestones);
    badge.title = `团队 ${input.plan.team_id || "—"} · v${input.plan.version || 0} · ${milestones} 个里程碑 · ${items} 个工作项`;
  }
}

// teamBoardInput 把后端 DTO 搬成渲染件的入参；没有可看的编排 → null（调用方据此退场）。
//
// 「可看的编排」的判据 = 有里程碑**或**工作项（与后端 teamworkPlanHasOrchestration
// 同口径）。**没有 stages 这个概念**（2026-10-04）：阶段口径整条退场，把它留在搬运里
// 就等于把已退场的形状又接回前端一次。
//
// events[].at 由后端给 **unix 秒**：这里转成 "YYYY-MM-DDTHH:MM"（**本地时间**，与渲染件
// formatEventTime 的正则同形）。不转成 toISOString()：那是 UTC，面板上会显示成差 8 小时的
// 时间——面板要的是"用户看到几点"。
function teamBoardInput(runtime) {
  const board = runtime?.teamwork_board;
  const milestones = Array.isArray(board?.milestones) ? board.milestones : [];
  const workItems = Array.isArray(board?.work_items) ? board.work_items : [];
  if (!board || (milestones.length === 0 && workItems.length === 0)) return null;
  return {
    plan: {
      team_id: board.team_id || "",
      version: board.version || 0,
      members: Array.isArray(board.members) ? board.members : [],
      milestones,
      // 工作项是**扁平**投影（每条自带 milestone），按 milestone 分组由渲染件做。
      // 之前这里漏搬了 work_items，甘特里每个里程碑都只剩"尚未排活"——工作项与里程碑的
      // 关系因此在看板上不可见（用户当面指出的那条）。
      work_items: workItems.map(item => ({
        id: item?.id || "",
        milestone: item?.milestone || "",
        role: item?.role || "",
        name: item?.name || "",
        description: item?.description || "",
        goal: item?.goal || "",
        depends_on: Array.isArray(item?.depends_on) ? item.depends_on : [],
        status: item?.status || "",
        session_id: item?.session_id || "",
        worktree: item?.worktree || "",
        workspace_mode: item?.workspace_mode || "",
        handle: item?.handle || "",
        note: item?.note || "",
        live: item?.live === true,
        interrupted: item?.interrupted === true,
      })),
    },
    // 在编上限来自后端（TeamworkBackend.MaxTeammates）；缺失/0 = 不限制，
    // 渲染件据此把「在编 n」写成「在编 n/max」。
    maxMembers: Number(board.max_members) || 0,
    maxJobs: Number(board.max_jobs) || 0,
    jobs: (Array.isArray(board.jobs) ? board.jobs : []).map(job => ({
      handle: job?.handle || "",
      state: job?.state || "",
      exit_code: job?.exit_code || 0,
      bytes: job?.bytes || 0,
      // 归属只有一格：node（工作项 id / 里程碑 id）。阶段口径已退场，不再搬 stage。
      node: job?.node || "",
      role: job?.role || "",
      scope: job?.scope || {},
    })),
    events: (Array.isArray(board.events) ? board.events : []).map(event => ({
      ...event,
      at: localMinuteStamp(event?.at),
    })),
    stale: board.stale === true,
    // recovered = 这份看板由后端从会话存档兜底恢复（活体投影给不出）。它只是**痕迹**：
    // 前端不为它改任何事实，也不缓存它——活体一恢复，下一次快照自然把它撤掉。
    recovered: board.recovered === true,
  };
}

// localMinuteStamp 把 unix 秒转成本地时间的 "YYYY-MM-DDTHH:MM"（解析不出来 → ""，
// 渲染件据此写 "—"，不把渲染层的猜测当成事实）。
function localMinuteStamp(at) {
  const seconds = Number(at || 0);
  if (!Number.isFinite(seconds) || seconds <= 0) return "";
  const date = new Date(seconds * 1000);
  if (Number.isNaN(date.getTime())) return "";
  const pad = value => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
    + `T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

// ensureTeamBoardStyles 把看板样式注入一次，来源 = team-board-view.js 的 TEAM_BOARD_CSS。
// 不往 styles.css 里抄第二份：抄一份就是两处色值，改一处漏一处。
function ensureTeamBoardStyles() {
  if (document.getElementById("team-board-styles")) return;
  const style = document.createElement("style");
  style.id = "team-board-styles";
  style.textContent = TEAM_BOARD_CSS;
  document.head.appendChild(style);
}

// ── 团队详情页（里程碑 / Work Item / teammate 三种页共用一套壳）──────────────
//
// 侧边栏只报最小可读集（口径见 team-board-view.js 的文件头），细节一律走这里。三种页由
// **同一个 renderTeamPage** 渲染，而 Work Item 页被另外两种页复用（里程碑页的「Work Item」
// 表、teammate 页的「负责的 Work Item」表都点进它）——前端不各写一份"差不多的详情"。
//
// 页面栈在**前端**：只有这里知道用户是从哪儿点进来的（返回键要回到那一页），渲染件只认
// "当前页 ref"与"上一层是什么"。栈是本地读状态：不进快照、不落盘、没有写入口。

const teamPageStack = [];

// teamPageLive 是当前 Work Item 页「执行会话」页签的读数（进程内执行面的投影）。
// 按页记 + 只在**切到那个页签时**才去读：进详情页就为每个工作项拉一次正文，等于把一次
// 点击变成一串 RPC，而用户多半只是想看一眼字段。
let teamPageLive = null;
let teamPageExecution = null;
let teamPageInspection = null;

function teamPageTop() {
  return teamPageStack[teamPageStack.length - 1] || null;
}

// openTeamPage 打开一页并压栈。同一页重复点不重复压栈（否则返回键要在同一页上按两次）。
function openTeamPage(kind, id) {
  const ref = `${String(kind || "")}:${String(id || "")}`;
  if (!parseTeamPageRef(ref)) return;
  if (teamPageTop()?.ref === ref) {
    setModal("team-page-modal", true);
    return;
  }
  teamPageStack.push({ ref, kind: String(kind), id: String(id), tab: "" });
  teamPageLive = null;
  teamPageExecution = null;
  teamPageInspection = null;
  ensureTeamBoardStyles();
  setModal("team-page-modal", true);
  refreshTeamPage();
}

// teamPageBack 出栈一层；栈空了就关页（返回键走到头 = 回到看板）。
function teamPageBack() {
  if (teamPageStack.length === 0) return;
  teamPageStack.pop();
  teamPageLive = null;
  teamPageExecution = null;
  teamPageInspection = null;
  if (teamPageStack.length === 0) {
    closeTeamPage();
    return;
  }
  refreshTeamPage();
}

function closeTeamPage() {
  teamPageStack.length = 0;
  teamPageLive = null;
  teamPageExecution = null;
  teamPageInspection = null;
  setModal("team-page-modal", false);
}

// teamPagePainted 记住"上一次画下去的是什么"：快照一到就重绘是必要的（状态会变），但
// **重绘一样的内容**只有一个副作用——把用户滚到一半的位置和选中的文字清掉。
// 渲染件是幂等的（同一份入参 → 逐字节相同的字符串），所以这里按字符串比一比就能安全跳过。
let teamPagePainted = { title: "", html: "" };

// paintTeamPage 只画当前页（不发请求、不改栈）：快照刷新、压栈、出栈、切页签都复用它，
// 于是"刷新一下弹窗闪一下"这件事不会发生，页签位置也不会被拽回第一页（tab 存在栈里）。
function paintTeamPage() {
  const top = teamPageTop();
  if (!top) return;
  const input = currentTeamBoardInput;
  if (!input) return;
  const parent = teamPageStack[teamPageStack.length - 2] || null;
  // 弹窗头只写"这是什么"(团队详情)：**标题归页自己的抬头**（teamPageHead：类种 + id · 名字）。
  // 之前两处都写标题，同一句话在屏幕上出现两遍——上栏看着就是乱的。
  const title = '<span class="eyebrow">团队详情</span>';
  const html = renderTeamPage({
    plan: input.plan,
    jobs: input.jobs,
    events: input.events,
    ref: top.ref,
    tab: top.tab,
    // 只画**属于这一页**的读数：换页时上一页的执行面正文不许跟着漂过来。
    live: teamPageLive?.ref === top.ref ? teamPageLive.view : null,
    execution: teamPageExecution?.ref === top.ref ? teamPageExecution.view : null,
    inspection: teamPageInspection?.ref === top.ref ? teamPageInspection.view : null,
    liveLoading: top.kind === "item" && top.tab === "session" && teamPageLive?.ref !== top.ref,
    parent: parent ? { ref: parent.ref, label: teamPageTitle({ plan: input.plan, ref: parent.ref }) } : null,
  });
  if (teamPagePainted.title === title && teamPagePainted.html === html) return;
  teamPagePainted = { title, html };
  elements["team-page-modal-title"].innerHTML = title;
  elements["team-page-view"].className = "team-page-view";
  elements["team-page-view"].innerHTML = html;
}

function planItemOf(id) {
  const items = Array.isArray(currentTeamBoardInput?.plan?.work_items) ? currentTeamBoardInput.plan.work_items : [];
  return items.find(item => String(item?.id || "") === String(id || "")) || null;
}

// loadTeamPageSession 读这一页对应的**执行面**（一 Work Item 一套 Session，正文不落盘）。
// 读失败不抛给用户一个红 toast：这一页本来就有"读不到"的正当情形（重启过 / 已收口），
// 渲染件会把话说清楚（running=false 的那一支）。
async function loadTeamPageSession(top) {
  if (!top || top.kind !== "item") return;
  const sessionID = String(planItemOf(top.id)?.session_id || "").trim();
  if (!sessionID) {
    teamPageLive = { ref: top.ref, sessionID: "", view: null };
    paintTeamPage();
    return;
  }
  let view = null;
  try {
    view = await invoke("TeammateSessionLive", sessionID);
  } catch (error) {
    view = null;
  }
  // 取数期间用户可能已经返回/换页：只有还停在同一页时才回写。
  if (teamPageTop()?.ref !== top.ref) return;
  teamPageLive = { ref: top.ref, sessionID, view };
  paintTeamPage();
}

async function loadWorkItemExecution(top, itemID) {
  const mainSessionID = String(currentTeamBoardInput?.mainSessionID || "").trim();
  if (!top || !mainSessionID || !itemID) return;
  try {
    const view = await invoke("WorkItemExecution", mainSessionID, String(itemID), 0, TEAMMATE_LIVE_PAGE_SIZE);
    if (teamPageTop()?.ref !== top.ref) return;
    teamPageExecution = { ref: top.ref, itemID: String(itemID), view };
    paintTeamPage();
  } catch {
    if (teamPageTop()?.ref !== top.ref) return;
    teamPageExecution = { ref: top.ref, itemID: String(itemID), view: null };
    paintTeamPage();
  }
}

async function loadTeammateInspection(top) {
  const mainSessionID = String(currentTeamBoardInput?.mainSessionID || "").trim();
  if (!top || top.kind !== "teammate" || !mainSessionID) return;
  try {
    const view = await invoke("TeammateInspection", mainSessionID, top.id);
    if (teamPageTop()?.ref !== top.ref) return;
    teamPageInspection = { ref: top.ref, view };
    paintTeamPage();
    const currentItemID = String(view?.current_work_item?.id || "").trim();
    if (currentItemID) await loadWorkItemExecution(top, currentItemID);
  } catch {
    if (teamPageTop()?.ref !== top.ref) return;
    teamPageInspection = { ref: top.ref, view: null };
    paintTeamPage();
  }
}

// openTeamPageLivePage 翻「这一页的执行会话」的一页（读法是后端的 TeammateSessionLivePage：
// 判据只有一份，前端只搬 offset）。
async function openTeamPageLivePage(offset) {
  const top = teamPageTop();
  if (!top || top.kind !== "item" || teamPageLive?.ref !== top.ref) return;
  const sessionID = String(teamPageLive.sessionID || "");
  if (!sessionID) return;
  try {
    const view = await invoke("TeammateSessionLivePage", sessionID, offset, TEAMMATE_LIVE_PAGE_SIZE);
    if (teamPageTop()?.ref !== top.ref) return;
    teamPageLive = { ref: top.ref, sessionID, view };
    paintTeamPage();
  } catch (error) {
    showToast(error);
  }
}

// switchTeamPageTab 切页签：**只动 DOM 的双类**（渲染件把三块面板全渲出来了），不重算、
// 不重渲——切回去永远给出与第一次相同的读数。唯一会发请求的是「执行会话」页签第一次打开。
function switchTeamPageTab(key) {
  const top = teamPageTop();
  if (!top) return;
  top.tab = String(key || "");
  const page = elements["team-page-view"]?.querySelector?.(".team-page");
  if (page) {
    for (const tab of page.querySelectorAll("[data-team-tab]")) {
      const on = tab.dataset.teamTab === top.tab;
      tab.classList.toggle("is-active", on);
      tab.setAttribute("aria-selected", String(on));
    }
    for (const panel of page.querySelectorAll("[data-team-panel]")) {
      panel.classList.toggle("is-active", panel.dataset.teamPanel === top.tab);
    }
  }
  paintTeamPage();
  if (top.kind === "item" && top.tab === "session" && teamPageExecution?.ref !== top.ref) {
    loadWorkItemExecution(top, top.id);
  }
  if (top.kind === "teammate" && top.tab === "session" && teamPageExecution?.ref !== top.ref) {
    if (teamPageInspection?.ref !== top.ref) loadTeammateInspection(top);
    else loadWorkItemExecution(top, teamPageInspection.view?.current_work_item?.id || "");
  }
}

// refreshTeamPage 重绘当前页 + 按需补读执行面（快照刷新 / 打开 / 返回都走这一条）。
async function refreshTeamPage() {
  const top = teamPageTop();
  if (!top || elements["team-page-modal"]?.classList?.contains("hidden")) return;
  if (!currentTeamBoardInput) {
    closeTeamPage();
    return;
  }
  paintTeamPage();
  if (top.kind === "teammate" && teamPageInspection?.ref !== top.ref) {
    await loadTeammateInspection(top);
  } else if (top.kind === "item" && top.tab === "session" && teamPageExecution?.ref !== top.ref) {
    await loadWorkItemExecution(top, top.id);
  }
}

// bindTeamBoardActions 绑看板自己的三个入口：
//   - 里程碑行的名字  → `data-team-ms-open`：开**里程碑详情页**；
//   - teammate 的名字 → `data-team-member-open`：开**teammate 详情页**；
//   - 「当前会话」chip → `data-team-role-open`：开这位**此刻那件事的会话**（实时执行面，
//     走同一个 openRoleSessionDetail + 角色会话只读观察面，不另造"员工会话"概念）。
//
// 为什么必须单独绑一条：这条委托挂在 `#team-board-view` 上（看板是独立 section），
// 事件不会从别的子树冒泡进来。渲染件把钩子写上了却不绑监听，得到的就是一个
// **点不动的入口**（比没有入口更坏）——Agent Team 面板退场（2026-10-12 J-1a）后，
// 看板这条委托就是角色会话读面的**唯一**入口，更不能缺。
//
// 只读：这里只开读视图（详情页读的是同一份快照投影，会话读面读的是执行面），不写任何后端
// 状态——看板"单向只读投影"的口径不变。
function bindTeamBoardActions() {
  const host = elements["team-board-view"];
  if (!host) return;
  host.addEventListener("click", async event => {
    const openMilestone = event.target.closest?.("[data-team-ms-open]");
    if (openMilestone?.dataset.teamMsOpen) {
      openTeamPage("milestone", openMilestone.dataset.teamMsOpen);
      return;
    }
    // 工作项行（左列的编号 + 右列那一格）走的是与详情页同一条下钻钩子 `data-team-page-open`
    // （"该看哪一页"只有 parseTeamPageRef 一处判据）：看板上点一行 = 打开这一件。
    const openItem = event.target.closest?.("[data-team-page-open]");
    if (openItem?.dataset.teamPageOpen) {
      const spec = parseTeamPageRef(openItem.dataset.teamPageOpen);
      if (spec) openTeamPage(spec.kind, spec.id);
      return;
    }
    const openMember = event.target.closest?.("[data-team-member-open]");
    if (openMember?.dataset.teamMemberOpen) {
      openTeamPage("teammate", openMember.dataset.teamMemberOpen);
      return;
    }
    const openRole = event.target.closest?.("[data-team-role-open]");
    if (!openRole?.dataset.teamRoleOpen) return;
    // 在编行同理：有"此刻那件事"就开那件事的会话（data-team-item 由渲染件按当前工作项写）。
    await openRoleSessionDetail(
      openRole.dataset.teamRoleOpen, openRole.dataset.teamRoleSession, openRole.dataset.teamItem || ""
    );
  });
}
bindTeamBoardActions();

// 工具记录看板（状态子页）：柱形图点击 → 原因饼图，表头开关 → 后端计数开关。
bindToolRecordPanel();

// ── 「目标」面板（工作台子页）──────────────────────────────
// 数据源：runtime.goal_governance（goal 状态机只读投影）+ runtime.active_skills
// （任务级 skill 激活权威投影）+ 最近用户输入（目标文本，本地派生展示）。
//
// 面板与它的标签都归 goal 状态机（纯渲染件见 goal-board-view.js renderGoalPanel）：
// 栈上还有 active 帧（目标在跑）→ 面板可见（看板 + 治理只读块 + goal 域 skill
// chips，GOAL 徽标亮）；目标结束（栈上没有 active 帧）→ 整块退场，徽标与 chips
// 一起退场（用户口径：结束就是没有了，不留空壳）。
// 标签的判据**不是** skill 激活态：`$goal`/`$teamwork` 一召回就长期为真，拿它当
// 判据就是"goal 已经结束了、标签还贴着"（2026-10-02 现场）。
// 点开看板 = 详情弹窗（goal-detail-view），像资源管理器的"内容详情"那样逐项列全。
function renderGoal(snapshot) {
  const runtime = snapshot.runtime || {};
  const governance = runtime.goal_governance && runtime.goal_governance.active
    ? runtime.goal_governance
    : null;
  const activeSkills = Array.isArray(runtime.active_skills) ? runtime.active_skills : [];
  const goalSection = elements["goal-section"];
  if (!goalSection) return;
  const goalText = latestUserInput(snapshot);
  // 详情弹窗与看板共用同一份只读输入：刷新时一起重绘（见 refreshGoalDetail）。
  lastGoalView = { governance, goalText };
  refreshGoalDetail();
  const panel = renderGoalPanel({ governance, goalText, activeSkills });
  goalSection.classList.toggle("hidden", panel.hidden);
  const badge = elements["goal-badge"];
  if (badge) {
    badge.classList.toggle("hidden", panel.badgeHidden);
    badge.textContent = "GOAL";
    badge.title = panel.badgeTitle;
  }
  const view = elements["goal-view"];
  if (panel.hidden) {
    // 没有 active goal：不留空壳，也不留上一轮面板里的标签与正文。
    view.innerHTML = "";
    stopGoalInFlightPoller(view);
    return;
  }
  view.classList.remove("muted");
  view.innerHTML = panel.html;
  startGoalInFlightPoller(view, governance);
}

// lastGoalView 是「目标」面板最近一次渲染的只读输入（治理视图 + 最近用户输入）：
// 详情弹窗打开时，每个快照/轮询帧都拿它重绘，保证弹窗内容与看板同源同帧。
let lastGoalView = { governance: null, goalText: "" };

// openGoalDetail 打开详情弹窗（看板卡片的点击落点）：内容与看板同源，只读。
function openGoalDetail() {
  const modal = elements["goal-detail-modal"];
  if (!modal) return;
  refreshGoalDetail();
  setModal("goal-detail-modal", true);
}

function closeGoalDetail() {
  setModal("goal-detail-modal", false);
}

// refreshGoalDetail 用最近一份只读输入重绘详情弹窗。弹窗没开时**什么都不做**：
// 没有 active goal（或没有帧）时也不留旧内容，避免下次打开看到上一轮的残留。
function refreshGoalDetail() {
  const modal = elements["goal-detail-modal"];
  const view = elements["goal-detail-view"];
  const title = elements["goal-detail-title"];
  if (!modal || !view) return;
  const governance = lastGoalView.governance;
  const body = governance ? renderGoalDetail(governance, lastGoalView.goalText) : "";
  if (!body) {
    view.className = "goal-detail-view muted";
    view.innerHTML = "当前无可查看的目标";
    if (title) title.innerHTML = "<span class=\"eyebrow\">目标</span><h2>目标详情</h2>";
    return;
  }
  const active = governance.stack && governance.stack.length ? governance.stack[governance.stack.length - 1] : null;
  if (title) {
    title.innerHTML = `<span class="eyebrow">目标 · 内容详情</span><h2>${escapeHtml(String(active?.title || governance.title || "目标"))}</h2>`;
  }
  view.className = "goal-detail-view";
  view.innerHTML = body;
}

// 看板点击 → 详情弹窗；弹窗内点遮罩 / ✕ / Esc 关闭。
elements["goal-view"]?.addEventListener("click", event => {
  if (event.target.closest?.("[data-goal-board-open]")) openGoalDetail();
});
elements["goal-detail-close"]?.addEventListener("click", closeGoalDetail);
elements["goal-detail-modal"]?.addEventListener("click", event => {
  if (event.target === elements["goal-detail-modal"]) closeGoalDetail();
});
document.addEventListener("keydown", event => {
  if (event.key === "Escape") closeGoalDetail();
});

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

// lastPlanDsl 保存最近一次渲染的 Plan DSL（节点详情弹窗的数据源；
// 权威 JSON 来自 snapshot.runtime.plan，弹窗只读展示）。
// lastSubagentTree 保存子代理树投影（snapshot.runtime.subagent_tree；
// fork 节点不在 Plan 快照里时详情弹窗的兜底数据源）。
let lastPlanDsl = null;
let lastSubagentTree = [];

// refreshPlanDetailData 只刷新详情弹窗的数据面（Plan DSL + 子代理树投影），
// 不做任何面板 DOM 渲染：右侧工作台统一由内联摘要（renderWorkTable）呈现，
// 详情弹窗数据源保持 Plan/子代理树权威投影（planToDSL 只算不改 DOM）。
function refreshPlanDetailData(plan, subagentTree = null) {
  lastPlanDsl = planToDSL(plan);
  lastSubagentTree = Array.isArray(subagentTree) ? subagentTree : [];
}

// renderWorkTable 把工作表格投影交给内联工作台摘要。Todo 是打点表，
// Task / Subagent 是同一套后台长任务语言；不再打开独立表格弹窗。
function renderWorkTable(rows, batches) {
  workbenchSummaryView.render(rows, batches);
  const normalized = workbenchSummaryView.current();
  rollNumber(elements["work-count"], String(normalized.length));
  elements["work-section"]?.classList.remove("hidden");
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

// renderScheduledTaskPanel 渲染定时周期任务面板：数据来自 snapshot.runtime
// （权威投影，调度器状态变化经 runtime.changed 增量带到）；任务只读展示，
// 新建按钮常驻（命令白名单来自 runtime.scheduled_commands）。
function renderScheduledTaskPanel(runtime) {
  const tasks = Array.isArray(runtime.scheduled_tasks) ? runtime.scheduled_tasks : [];
  elements["scheduled-task-count"].textContent = String(tasks.length);
  elements["scheduled-task-view"].innerHTML = renderScheduledTasks(tasks);
}

// roleSessionDetail 记录当前打开的员工会话视图目标（身份 + 视图内可切换的员工名单）：
// 刷新键、切员工、以及 team.changed 事件都要靠它原样重放/换人，而不是从 DOM 里猜。
// 名册来源是团队看板的在编名单（roleSessionTargets，见下）。
let roleSessionDetail = null;

// roleSessionTargets 汇总「员工会话视图」里可切换的员工：来源 = **团队看板的在编名单**
// （`runtime.teamwork_board` 的 `members[]`，与看板行同一份投影——不在前端另编一份名册）。
// 每行给 roleName / roleKind / roleSessionID（没有"这件事自己的会话"时为空串，切过去
// 由后端兜底解析：teammateSessionEntry 的判定不变，没有自己的会话就是不打开）。
function roleSessionTargets() {
  const plan = currentTeamPlan;
  const members = Array.isArray(plan?.members) ? plan.members : [];
  return members
    .map(member => String(member?.role || "").trim())
    .filter(Boolean)
    .map(roleName => {
      const entry = teammateSessionEntry(plan, roleName);
      return {
        roleName,
        roleKind: "",
        roleSessionID: entry?.kind === "live" ? String(entry.session_id || "") : ""
      };
    });
}

// openRoleSessionDetail 打开某个角色的独立会话（入口 = 团队看板在编行的「当前会话」
// chip / 详情页同一个钩子）：DS-A2A 里
// EXEC（main）与 ADVISOR（tl）是两个会话，主对话只显示 EXEC 的可见消息，
// 这里按角色身份单独展示该 agent 自己的行，避免两个 agent 都渲染成 AGENT。
// 视图顶部的「切员工」条把目标换成同会话的另一位员工——这就是"对话视图切员工"。
//
// **teammate 的会话口径**（2026-10-04，用户口径修正：查看 teammate 的会话"总是看到主代理
// 的会话"）：入口只带角色名与"员工的长期角色会话号"，而那条读面（`AgentTeamRoleSnapshot`
// → `RoleSnapshot`）是**群聊车道**形状——main 车道 = 主会话整段，teammate 自己那条车道在
// 存储里从来没有行（worker 回合是进程内执行面，不写存储）。于是"没有工作项上下文"的调用
// 会把主代理的会话画成这位的会话。修法：**同一份事实只有一个判定面**——`teammateSessionEntry`
// 按团队看板（看板的 teammate 行就是按它写入口的）判定一次：
//   - live：这位是 teammate 且有"这件事自己的会话" → 走实时读面（那才是 teammate 自己的正文）；
//   - none：这位是 teammate 但此刻没有自己的会话 → **不开**（如实说一句，而不是退回角色会话）；
//   - role：不是 teammate（goal-a2a 的 tl 等真有自己角色会话的角色）→ 照旧走角色会话。
async function openRoleSessionDetail(roleName, roleSessionID, workItemID) {
  const name = String(roleName || "").trim();
  if (!name) return;
  let workItem = String(workItemID || "").trim();
  let sessionID = String(roleSessionID || "").trim();
  if (!workItem && currentTeamPlan) {
    const entry = teammateSessionEntry(currentTeamPlan, name);
    if (entry.kind === "live") {
      workItem = String(entry.work_item || "");
      sessionID = String(entry.session_id || "");
    } else if (entry.kind === "none") {
      showToast(new Error(entry.reason));
      return;
    }
  }
  try {
    if (workItem) {
      // 这件事自己的会话：读**实时执行面**（进程内、正文不落盘）。读不到时渲染件会如实
      // 说明（而不是把主会话历史画成"当前会话"）。
      const live = await invoke("TeammateSessionLive", sessionID);
      roleSessionDetail = {
        roleName: name, roleSessionID: sessionID, workItem,
        live: true, members: roleSessionTargets(), snapshot: null
      };
      renderTeammateSessionView(live);
      setModal("role-session-modal", true);
      return;
    }
    const snapshot = await invoke("AgentTeamRoleSnapshot", "", name, sessionID);
    roleSessionDetail = {
      roleName: name, roleSessionID: sessionID, workItem: "",
      live: false, members: roleSessionTargets(), snapshot
    };
    renderRoleSessionView(snapshot);
    setModal("role-session-modal", true);
  } catch (error) {
    showToast(error);
  }
}

// renderTeammateSessionView 画"这件事自己的会话"（实时执行面那一路）。
//
// 与 renderRoleSessionView 分开：两者读的是**两个不同的东西**（进程内执行面 vs 落盘的
// 角色会话），标题也各自说清是哪个——混成一个标题，用户就分不清看到的是当前的活还是历史。
function renderTeammateSessionView(live) {
  const detail = roleSessionDetail || { roleName: "", roleSessionID: "", workItem: "" };
  // 记住这一页的位置与"后面还有没有"：刷新键/实时事件重绘时按**同一页**再读一次，
  // 而不是把用户翻到的位置拽回尾巴（后端读数里的 offset/has_more 是权威值，前端只记）。
  detail.liveOffset = Number.isFinite(Number(live?.offset)) ? Number(live.offset) : 0;
  detail.liveHasMore = live?.has_more === true;
  elements["role-session-modal-title"].innerHTML =
    `<span class="eyebrow">teammate · 这件事的会话</span><h2>${escapeHtml(roleDisplayName(detail.roleName))} · ${escapeHtml(detail.workItem || "—")}</h2>`;
  elements["role-session-view"].className = "role-session-view";
  elements["role-session-view"].innerHTML = renderTeammateLiveSession(live, {
    role: detail.roleName, work_item: detail.workItem
  });
}

// openTeammateLivePage 翻「这件事的会话」的一页（读法是后端的
// TeammateSessionLivePage：判据只有一份，前端只搬 offset）。
async function openTeammateLivePage(offset) {
  const detail = roleSessionDetail;
  if (!detail?.live || !detail.roleSessionID) return;
  try {
    const page = await invoke("TeammateSessionLivePage", detail.roleSessionID, offset, TEAMMATE_LIVE_PAGE_SIZE);
    // 取数期间用户可能已关掉/切走：只有目标还是同一份时才回写。
    if (roleSessionDetail === detail) renderTeammateSessionView(page);
  } catch (error) {
    showToast(error);
  }
}

// roleSessionLiveTools 是按**角色会话**缓存的有界实时工具活动（teammate.tool.* 载荷）。
//
// 它刻意不进快照：那是"此刻在做什么"的瞬态，权威记录在角色会话投影里（AgentTeamRoleSnapshot）。
// 缓存按角色会话分组 → 打开的是哪一位就只画哪一位；没打开的在缓存里等着（切回来立刻有）。
// 有界（只留最近 ROLE_LIVE_TOOL_LIMIT 条）：一轮 ReAct 可以跑很多步，无界积累会把详情
// 面板撑成流水账。
const ROLE_LIVE_TOOL_LIMIT = 40;
const roleSessionLiveTools = new Map();

// applyTeammateToolActivity 收下一条 teammate 工具活动（started/completed 同 ID → upsert）。
//
// 这是**逐帧**的热更新路径（subagent.tool.* 的员工侧对称面）：不改快照、不跑 RPC——
// 载荷本身就是这一步的读数，直接并进缓存后重绘开着的详情。视图没开、或开的不是这位，
// 就只记忆不重绘（切过去时自然带出来）。
function applyTeammateToolActivity(payload) {
  const roleSessionID = String(payload?.role_session_id || "");
  const id = String(payload?.id || "");
  if (!roleSessionID || !id) return;
  const steps = roleSessionLiveTools.get(roleSessionID) || [];
  const index = steps.findIndex(step => step.id === id);
  if (index >= 0) steps[index] = payload;
  else steps.push(payload);
  if (steps.length > ROLE_LIVE_TOOL_LIMIT) steps.splice(0, steps.length - ROLE_LIVE_TOOL_LIMIT);
  roleSessionLiveTools.set(roleSessionID, steps);

  const detail = roleSessionDetail;
  if (!detail || detail.roleSessionID !== roleSessionID) return;
  if (elements["role-session-modal"]?.classList?.contains("hidden")) return;
  if (!detail.snapshot) return;
  renderRoleSessionView(detail.snapshot);
}

// renderRoleSessionView 只重绘视图内容（标题 + 正文），不动弹窗开合：刷新键、切员工
// 与实时工具活动都复用它，避免"刷新一下弹窗闪一下"。
function renderRoleSessionView(snapshot) {
  if (roleSessionDetail?.live) {
    // 实时那一路由 renderTeammateSessionView 独占（形状不同：它是这件事自己的会话）。
    return;
  }
  const detail = roleSessionDetail || { roleName: "", roleSessionID: "", members: [] };
  elements["role-session-modal-title"].innerHTML = `<span class="eyebrow">角色会话 · 只读观察面</span><h2>${escapeHtml(roleDisplayName(detail.roleName))}</h2>`;
  elements["role-session-view"].className = "role-session-view";
  // 实时工具活动按角色会话取（没有就是空数组：渲染件据此不画空壳）。
  const liveTools = roleSessionLiveTools.get(detail.roleSessionID) || [];
  elements["role-session-view"].innerHTML = renderRoleSessionDetail(snapshot, { ...detail, liveTools });
}

// refreshRoleSessionDetail 重取当前视图目标的快照（刷新键 / team.changed 事件）：
// 权威读数走这条**拉取**路；"此刻在做什么"走 teammate.tool.* 的**推送**路（不进快照）。
async function refreshRoleSessionDetail() {
  const detail = roleSessionDetail;
  if (!detail || !detail.roleName) return;
  if (elements["role-session-modal"]?.classList?.contains("hidden")) return;
  try {
    if (detail.live) {
      // 用户翻在**尾巴**（后面没有更多）时按默认页再读：新消息自然露出来；翻在中间页
      // 时停在**同一页**（否则"刷新一下跳回尾巴"）。
      const live = detail.liveHasMore
        ? await invoke("TeammateSessionLivePage", detail.roleSessionID, detail.liveOffset || 0, TEAMMATE_LIVE_PAGE_SIZE)
        : await invoke("TeammateSessionLive", detail.roleSessionID);
      if (roleSessionDetail === detail) {
        renderTeammateSessionView(live);
      }
      return;
    }
    const snapshot = await invoke("AgentTeamRoleSnapshot", "", detail.roleName, detail.roleSessionID);
    // 取数期间用户可能已切到别人/关掉视图：只有目标还是同一份时才回写。
    if (roleSessionDetail === detail) {
      detail.snapshot = snapshot;
      renderRoleSessionView(snapshot);
    }
  } catch (error) {
    showToast(error);
  }
}

function closeRoleSessionDetail() {
  roleSessionDetail = null;
  setModal("role-session-modal", false);
}

// 团队详情页的点击委托（一个容器管三种页）：下钻 / 返回 / 切页签 / 开当前会话 / 翻会话页。
// 与 bindTeamBoardActions 同一条口径：钩子写在渲染件上，监听必须真的挂上——不挂就是点不动的
// 按钮（比没有入口更坏）。这一条挂在 `#team-page-view` 上（它不是看板的子树）。
elements["team-page-view"]?.addEventListener("click", event => {
  const open = event.target.closest?.("[data-team-page-open]");
  if (open?.dataset.teamPageOpen) {
    const spec = parseTeamPageRef(open.dataset.teamPageOpen);
    if (spec) openTeamPage(spec.kind, spec.id);
    return;
  }
  if (event.target.closest?.("[data-team-page-back]")) {
    teamPageBack();
    return;
  }
  const tab = event.target.closest?.("[data-team-tab]");
  if (tab?.dataset.teamTab) {
    switchTeamPageTab(tab.dataset.teamTab);
    return;
  }
  // 详情页里的「当前会话」chip 与看板行上那枚是同一个入口（同一份 teammateSessionEntry 判定）。
  const openRole = event.target.closest?.("[data-team-role-open]");
  if (openRole?.dataset.teamRoleOpen) {
    openRoleSessionDetail(openRole.dataset.teamRoleOpen, openRole.dataset.teamRoleSession, openRole.dataset.teamItem || "");
    return;
  }
  const livePage = event.target.closest?.("[data-teammate-live-page]");
  if (livePage?.dataset?.teammateLivePage !== undefined && !livePage.disabled) {
    openTeamPageLivePage(Number.parseInt(livePage.dataset.teammateLivePage, 10) || 0);
  }
});
elements["team-page-close"]?.addEventListener("click", closeTeamPage);
document.addEventListener("keydown", event => {
  if (event.key === "Escape") closeTeamPage();
});

elements["role-session-close"]?.addEventListener("click", closeRoleSessionDetail);
elements["role-session-modal"]?.addEventListener("click", event => {
  if (event.target === elements["role-session-modal"]) {
    closeRoleSessionDetail();
    return;
  }
  if (event.target.closest?.("[data-role-session-refresh]")) {
    refreshRoleSessionDetail();
    return;
  }
  const livePage = event.target.closest?.("[data-teammate-live-page]");
  if (livePage?.dataset?.teammateLivePage !== undefined && !livePage.disabled) {
    openTeammateLivePage(Number.parseInt(livePage.dataset.teammateLivePage, 10) || 0);
    return;
  }
  const swap = event.target.closest?.("[data-role-session-switch]");
  if (swap?.dataset.roleSessionSwitch) {
    openRoleSessionDetail(swap.dataset.roleSessionSwitch, swap.dataset.roleSessionSwitchSid);
  }
});
document.addEventListener("keydown", event => {
  if (event.key === "Escape") closeRoleSessionDetail();
});

// ── 档案夹 → 会话：把工作树文件"抽出来"发出去（拖拽） ────────────────────────
// 纯映射在 file-drop.js（可脱 DOM 单测），这里只做 DOM/Bridge 接线：
//   1) 工作树文件行 draggable（worktree-view.js 输出 data-file-drag）；
//   2) dragstart 把**相对路径**写进 dataTransfer（自定义 mime + text/plain 兜底）；
//   3) 聊天框与消息队列是 drop 目标（队列每次重渲染都换 DOM，所以用 document 委托）：
//      草稿非空 → 只把引用追加到草稿（不打断用户正在写的话，更不替他发出去）；
//      草稿为空 → 整条交出去发送：会话在跑时后端排进消息队列，就渲染成聊天框上沿
//      的那叠排队卡片；空闲时就是一条普通消息。复用 composer 的提交路径（语义、
//      清除草稿、刷新都与手打发送完全一致），不另开一条发送通道。
const FILE_DROP_TARGETS = "#composer, .message-queue";
let fileDropTarget = null;

function fileDropZone(node) {
  return node?.closest?.(FILE_DROP_TARGETS) || null;
}

function setFileDropZone(zone) {
  if (fileDropTarget === zone) return;
  fileDropTarget?.classList?.remove("is-drop-target");
  fileDropTarget = zone;
  fileDropTarget?.classList?.add("is-drop-target");
}

function dropPayload(transfer) {
  if (!transfer) return {};
  let mime = "";
  let text = "";
  try { mime = transfer.getData(FILE_DRAG_MIME) || ""; } catch { mime = ""; }
  try { text = transfer.getData("text/plain") || ""; } catch { text = ""; }
  return { mime, text, files: Array.from(transfer.files || []) };
}

function initFileDrop() {
  document.addEventListener("dragstart", event => {
    const source = event.target?.closest?.("[data-file-drag]");
    if (!source) return;
    const payload = fileDragPayload(source.dataset.fileDrag);
    if (!payload) return;
    event.dataTransfer?.setData(payload.mime, payload.path);
    event.dataTransfer?.setData("text/plain", payload.path);
    if (event.dataTransfer) event.dataTransfer.effectAllowed = "copy";
    source.classList.add("is-dragging");
  });
  document.addEventListener("dragend", event => {
    event.target?.closest?.("[data-file-drag]")?.classList.remove("is-dragging");
    // 取消拖拽（Esc / 窗口失焦）时 dragleave 可能不到：这里兜底撤掉高亮。
    setFileDropZone(null);
  });
  document.addEventListener("dragover", event => {
    // 队列条自己的拖拽换序（QUEUE_DRAG_MIME）不是文件投放：不点亮文件投放区，
    // 免得一次排序被读成"往队列里丢文件"（高亮会把整叠与输入框描上强调边）。
    if (event.dataTransfer?.types?.includes(QUEUE_DRAG_MIME)) return;
    const zone = fileDropZone(event.target);
    if (!zone) return;
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = "copy";
    setFileDropZone(zone);
  });
  document.addEventListener("dragleave", event => {
    if (!fileDropTarget) return;
    // 只有真的离开高亮区才撤（进入子元素也会触发 dragleave）。
    if (!fileDropTarget.contains(event.relatedTarget)) setFileDropZone(null);
  });
  document.addEventListener("drop", event => {
    const zone = fileDropZone(event.target);
    setFileDropZone(null);
    if (!zone) return;
    // 队列条换序的落点（QUEUE_DRAG_MIME）由队列宿主自己的 drop 处理器接走：
    // 载荷里没有工作树路径，这里不该再按"投放文件"处理一次。
    if (event.dataTransfer?.types?.includes(QUEUE_DRAG_MIME)) return;
    const plan = dropPlan({ paths: dropPaths(dropPayload(event.dataTransfer)), draftText: elements.prompt.value });
    if (!plan) return;
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = "copy";
    if (plan.mode === "insert") {
      // 草稿非空：只追加引用，让用户接着写完再自己发。
      elements.prompt.value = plan.draft;
      markComposerEdited();
      resizePrompt();
      elements.prompt.focus();
      showToast(`已引用 ${plan.paths.length} 个工作树文件`);
      return;
    }
    // 草稿为空：走与手打发送完全相同的提交路径（后端在会话运行中会把它排进队列）。
    elements.prompt.value = plan.text;
    resizePrompt();
    elements.composer.requestSubmit();
  });
}
initFileDrop();

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
  const row = workbenchSummaryView.current().find(item => item.source_id === nodeKey || item.id === nodeKey);
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
  elements["interaction-risk"].textContent = approvalRiskLabel(interaction.risk);
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

const SETTINGS_PAGES = ["roles", "application"];

// setSettingsPage 只切换设置弹窗内部的页面，不触碰快照或业务状态。页面归属是
// 结构契约：账户新增在二级弹窗，项目连接在项目设置，会话只保留放权。
function setSettingsPage(page) {
  const target = SETTINGS_PAGES.includes(page) ? page : "roles";
  for (const button of elements["settings-nav"]?.querySelectorAll?.("[data-settings-page]") || []) {
    const active = button.dataset.settingsPage === target;
    button.classList.toggle("is-active", active);
    button.setAttribute("aria-current", active ? "page" : "false");
  }
  for (const panel of elements["settings-modal"]?.querySelectorAll?.("[data-settings-page-panel]") || []) {
    panel.classList.toggle("hidden", panel.dataset.settingsPagePanel !== target);
  }
}

async function openSettings() {
  setModal("settings-modal", true);
  setSettingsPage("roles");
  renderThemePicker();
  // 角色设置：账号下拉与 plugin 下拉来自最近一份快照，权限向量先读一次权威值
  // （读不到就进"预览态"，见 syncRoleSettings）。
  await syncRoleSettings();
  // 会话驻留窗口：**进程级**读数，另拉一次（不进快照）；读不到只说明一句，不拖累
  // 别的分段。
  await syncReleaseWindowReading();
  // 工具记录开关：**进程级**设置（落数据根 runtime-limits.json），与上面那条同一条读法。
  await syncToolRecordSetting();
  // 每账号上下文窗口：账号与生效值来自最近一份快照（后端已按优先级算好），挡位表另拉。
  await syncAccountWindowReading();
  await syncPermissionRoles();
  // 账户：角色词表（宿主下发）+ 逐账号余额行。读数不进快照，点一次查一次。
  await syncAccountAdminForm();
}

function closeSettings() { setModal("settings-modal", false); }

async function openSessionPermissions() {
  if (!client.current()?.session?.id) {
    showToast("当前没有可编辑的会话");
    return;
  }
  setModal("session-permission-modal", true);
  await syncPermissionRoles();
}

function closeSessionPermissions() {
  setModal("session-permission-modal", false);
}

let sessionEnvironmentEnvs = [];
let sessionEnvironmentDefault = "";
let sessionEnvironmentCurrent = "";

async function syncSessionEnvironment() {
  const workspaceID = client.current()?.current_workspace?.id || "";
  if (!workspaceID) {
    sessionEnvironmentEnvs = [];
    sessionEnvironmentDefault = "";
    sessionEnvironmentCurrent = "";
    renderSessionEnvironmentSection();
    return;
  }
  sessionEnvironmentEnvs = sshEnvRows.slice();
  sessionEnvironmentDefault = sshEnvDefault;
  try {
    const sessionState = await invoke("SessionSSHState");
    sessionEnvironmentCurrent = String(sessionState?.current_env || "").trim();
  } catch {
    sessionEnvironmentCurrent = "";
  }
  renderSessionEnvironmentSection();
}

function sessionEnvironmentOptionLabel(item) {
  return item.host
    ? `${item.name} · SSH ${item.user || ""}@${item.host}${item.port ? ":" + item.port : ""}`
    : `${item.name} · 本地`;
}

function renderSessionEnvironmentSection() {
  const select = elements["session-environment-select"];
  if (!select) return;
  const hasSession = Boolean(client.current()?.session?.id);
  const fixed = Boolean(sessionEnvironmentCurrent);
  select.innerHTML = sessionEnvironmentEnvs.length
    ? sessionEnvironmentEnvs.map(item => {
      const selected = item.name === (sessionEnvironmentCurrent || sessionEnvironmentDefault);
      return `<option value="${escapeHtml(item.name)}"${selected ? " selected" : ""}>${escapeHtml(sessionEnvironmentOptionLabel(item))}</option>`;
    }).join("")
    : '<option value="">项目还没有工作环境</option>';
  select.disabled = fixed || !hasSession || !sessionEnvironmentEnvs.length;
  if (elements["session-environment-confirm"]) {
    elements["session-environment-confirm"].disabled = fixed || !hasSession || !sessionEnvironmentEnvs.length;
    elements["session-environment-confirm"].textContent = fixed ? "已固定" : "固定此环境";
  }
  if (elements["session-environment-status"]) {
    elements["session-environment-status"].textContent = !hasSession
      ? "当前没有打开的会话；项目环境仍可维护，固定操作需在会话中完成。"
      : fixed
        ? `已固定为 ${sessionEnvironmentCurrent}；agent 与工具入参都不能切换，换环境请新建会话。`
        : sessionEnvironmentEnvs.length
          ? "固定后本会话、其 subagent/teammate 与后台工具都使用该环境。"
          : "先在下方添加一个项目环境，再回到这里固定。";
  }
}

let editingAccountName = "";

function configureAccountForm(mode, details = {}) {
  const editing = mode === "edit";
  if (elements["account-add-modal-title"]) {
    elements["account-add-modal-title"].textContent = editing ? `编辑账户 ${details.name || ""}` : "添加账户";
  }
  if (elements["account-add-note"]) {
    elements["account-add-note"].textContent = editing
      ? "只改这条账户；API Key 留空表示保留原密钥。角色与账户名是身份，不从编辑窗口迁移。"
      : "完整表单写入 config/accounts.yaml 并热注册；API Key 只在宿主侧读取，不进入快照或界面回读。";
  }
  if (elements["account-add-submit"]) elements["account-add-submit"].textContent = editing ? "保存修改" : "添加账户";
  if (elements["account-add-role"]) elements["account-add-role"].disabled = editing;
  if (elements["account-add-model"]) elements["account-add-model"].value = details.model || "";
  if (elements["account-add-provider"]) elements["account-add-provider"].value = details.provider || "";
  if (elements["account-add-base-url"]) elements["account-add-base-url"].value = details.base_url || "";
  if (elements["account-add-effort"]) elements["account-add-effort"].value = details.reasoning_effort || "";
  if (elements["account-add-api-key"]) {
    elements["account-add-api-key"].value = "";
    elements["account-add-api-key"].placeholder = editing && details.api_key_set ? "留空保留现有密钥" : "sk-…";
  }
}

async function openAccountAdd(role = "") {
  editingAccountName = "";
  setAccountAddStatus("");
  await syncAccountAdminForm();
  configureAccountForm("add");
  if (role && elements["account-add-role"]) {
    elements["account-add-role"].value = role;
  }
  setModal("account-add-modal", true);
}

function closeAccountAdd() {
  if (elements["account-add-api-key"]) elements["account-add-api-key"].value = "";
  editingAccountName = "";
  setModal("account-add-modal", false);
  configureAccountForm("add");
}

let deletingAccountName = "";

async function openAccountEdit(name) {
  if (!name) return;
  try {
    const details = await invoke("AccountDetails", name);
    editingAccountName = name;
    setAccountAddStatus("");
    await syncAccountAdminForm();
    configureAccountForm("edit", details || {});
    if (elements["account-add-role"]) elements["account-add-role"].value = details?.role || "";
    setModal("account-add-modal", true);
  } catch (error) {
    showToast(error);
  }
}

function openAccountDelete(name, role = "") {
  deletingAccountName = name || "";
  if (elements["account-delete-message"]) {
    elements["account-delete-message"].textContent =
      `确定删除 ${name || "这条账户"}${role ? `（${role}）` : ""}？删除会改写 config/accounts.yaml；正在承流时需重启后彻底释放。`;
  }
  if (elements["account-delete-status"]) elements["account-delete-status"].textContent = "";
  setModal("account-delete-modal", true);
}

function closeAccountDelete() {
  deletingAccountName = "";
  setModal("account-delete-modal", false);
}

async function openProjectSettings() {
  if (!client.current()?.current_workspace?.id) {
    showToast("先选择一个项目，再管理环境");
    return;
  }
  setModal("settings-modal", false);
  setModal("project-settings-modal", true);
  await syncWorkspaceEnvironments();
  await syncSessionEnvironment();
}

function closeProjectSettings() {
  closeEnvironmentEditor();
  setModal("project-settings-modal", false);
}

// ── 皮肤（组件库 token 层）────────────────────────────────
// 三层分工见 themes/README.md：组件库给元素基线，styles.css 给 Seelex 外观，
// 皮肤包只覆盖语义 token。颜色事实在皮肤 CSS 里，这里只负责选哪一套。
let themeController = null;
// lastWindowAppearanceKey 是"已经交给 Go 的窗口外观"（`皮肤:材质`）：换肤回流会被
// 调用两次（<link> 换上是异步的，控制器先立即回流一次、onload 再回流一次），深浅
// 切换也会重跑同一条 onApplied——没有这个钥匙，一次换肤会白发几次跨进程写盘。
let lastWindowAppearanceKey = "";

// syncWindowAppearance 把"当前皮肤 → 窗口材质"经责任链确认后交给 Go 落盘。
// 窗口透明/亚克力只能在建窗时设（Wails 无运行时开关），所以这一步只写设置：
// 换 glass 后要**重启**才真的透，其余皮肤一字不动。
async function syncWindowAppearance() {
  const skinId = themeController?.current()?.skin?.id || "";
  const treatment = resolveWindowTreatment(skinId);
  const key = `${skinId}:${treatment}`;
  if (key === lastWindowAppearanceKey) return;
  try {
    await invoke("SetWindowAppearance", skinId, treatment);
    lastWindowAppearanceKey = key;
  } catch {
    // 桥未就绪（浏览器预览 / headless）或宿主不落盘：换肤本身不受影响，下次回流再试。
  }
}

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
    onApplied: () => {
      terminalPanel.refreshTheme();
      // 同时把"这套皮肤要哪种窗口"交出去（毛玻璃要做到真透，见 window-treatment.js）。
      void syncWindowAppearance();
    }
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

// 输入前缀（sigil）契约：与后端 application/core/completion.go 同一条表，前端
// 只消费不发明。权威说明见 docs/gui/modules/shell-and-interactions.md。
//   /  命令与工具   #  切换 Plugin   $  召回 Skill
// `@` **不是前缀**：J-1a 把"手动召唤团队"整条删掉后，用户拍板把这个字符让给**文件
// 指代**；J-1b 的定案是"`@` / `+` / 拖拽 = 同一条工作区相对路径引用"，而引用就是
// **正文文本**的一部分，因此它既不进后端路由器、也没有 sigil 语义（提交走与普通对话
// 同一条 SubmitToSession，见 composer-input.js）。前端只多做两件事：给候选（来自已
// 加载的工作区树）与渲染 chip（file-reference.js）。所以 SIGILS 与后端一一对应，
// `@` 另立一条：INLINE_TRIGGERS = 面板触发字符（含前端专属的 `@`）。
const SIGIL_COMMAND = "/";
const SIGIL_PLUGIN = "#";
const SIGIL_SKILL = "$";
const SIGILS = [SIGIL_COMMAND, SIGIL_PLUGIN, SIGIL_SKILL];
const INLINE_TRIGGERS = [...SIGILS, FILE_REF_TRIGGER];

// SIGIL_PATTERN 判定"内联建议该不该弹"：触发字符开头且还没进入参数区（无空白）。
const SIGIL_PATTERN = new RegExp(`^[${INLINE_TRIGGERS.map(sigil => `\\${sigil}`).join("")}][^\\s]*$`);

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

function renderSuggestionList(container, suggestions, selected, trigger, limit = suggestions.length, emptyText = "没有匹配的指令") {
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
    : `<span class="muted list-empty">${escapeHtml(emptyText)}</span>`;
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
  return ({ skill: "skill", plugin: "plugin", command: "command", team: "team", file: "file", folder: "folder" })[kind] || "command";
}

function acceptSuggestion(suggestion, trigger) {
  if (!suggestion) return;
  // `@` 是文件引用：走**同一套插入代码**（file-reference.js 的 applyFileReference），
  // 草稿不会被整段替换（`/` `#` `$` 那三个前缀的候选本来就是"整条输入 = 这个候选"，
  // 引用不是：它插在用户正在写的那句话里）。
  if (trigger === FILE_REF_TRIGGER) {
    acceptFileReference(suggestion.text);
    return;
  }
  elements.prompt.value = `${trigger}${suggestion.text} `;
  resizePrompt();
  markComposerEdited();
  closeCommandPalette();
  hideInlineSuggestions();
  elements.prompt.focus();
  elements.prompt.setSelectionRange(elements.prompt.value.length, elements.prompt.value.length);
}

// ── 文件引用（J-1b）──────────────────────────────────────────────────────────
// 三入口同一载体：`@文件`（内联面板）/ `+` 加号（选择器）/ 右栏工作树拖拽（原有接线）
// 都只产出**工作区相对路径**，共用 file-reference.js 的归一化与 chip。这里只管接线
// （DOM / Bridge），规则全在那个纯模块里。
//
// 候选索引：**已加载的工作区树**。为了让"按键路径只走已加载的树"成立，索引按工作区
// root 缓存（fileRefIndexRoot），只在首次需要时用一次 WorkspaceTree 列举拉取（depth 4），
// 之后按键只做内存筛选；换工作区即失效重取。**不逐键读盘**。
const FILE_REF_TREE_DEPTH = 4;
let fileRefIndex = [];
// fileRefIndexRoot = 索引属于哪个工作区 root（`null` = 还没拉过；空串也算"拉过了"，
// 免得不支持工作区的会话每次按键都重拉一遍）。
let fileRefIndexRoot = null;
let fileRefIndexLoading = null;

// fileRefIndexStale 判定索引要不要（重新）拉取：换工作区即失效。
function fileRefIndexStale() {
  return fileRefIndexRoot === null || fileRefIndexRoot !== (client.current()?.current_workspace?.root_path || "");
}

async function ensureFileRefIndex() {
  const root = client.current()?.current_workspace?.root_path || "";
  if (!fileRefIndexStale()) return fileRefIndex;
  if (fileRefIndexLoading) return fileRefIndexLoading;
  fileRefIndexLoading = invoke("WorkspaceTree", "", FILE_REF_TREE_DEPTH)
    .then(listing => {
      fileRefIndex = fileReferenceIndex(listing?.entries);
      fileRefIndexRoot = root;
      return fileRefIndex;
    })
    .catch(error => {
      // 索引是增强面：拉取失败只是"没有候选"，不打断输入（非法路径也进不了引用）。
      // 失败不记 root（下一次需要时再试一次），也不清掉上一次的索引。
      showToast(error);
      return fileRefIndex;
    })
    .finally(() => { fileRefIndexLoading = null; });
  return fileRefIndexLoading;
}

// 候选行的形状与命令面板同一套（renderSuggestionList 的 kind → 图标）：目录用 folder、
// 文件用 file，描述里给出完整路径（长路径被截断时仍可读全）。
function fileReferenceRows(items) {
  return items.map(item => ({
    text: item.path,
    description: item.dir ? "目录（agent 会自己列目录读）" : "文件（agent 用 read_file 读）",
    kind: item.dir ? "folder" : "file"
  }));
}

// acceptFileReference 是三个入口的**唯一插入点**：把路径写进草稿（`@路径 `，就是正文
// 里的文本），然后重画草稿 chip 条。非法路径不插（统一由一个判据拒绝）。
function acceptFileReference(rawPath) {
  const applied = applyFileReference(elements.prompt.value, elements.prompt.selectionStart ?? elements.prompt.value.length, rawPath);
  closeFilePicker();
  hideInlineSuggestions();
  if (!applied.ok) {
    showToast("只能引用工作区内的相对路径（不能是绝对路径、不能含 ..、不能超长）");
    return;
  }
  elements.prompt.value = applied.text;
  markComposerEdited();
  resizePrompt();
  renderComposerFileRefs();
  elements.prompt.focus();
  elements.prompt.setSelectionRange(applied.cursor, applied.cursor);
}

// renderComposerFileRefs 重画草稿区的引用 chip 条（与对话正文**同一枚 chip**）。
function renderComposerFileRefs() {
  const host = elements["composer-file-refs"];
  if (!host) return;
  host.innerHTML = fileReferenceStripHTML(elements.prompt.value, { host: "composer" });
}

function closeFilePicker() {
  const picker = elements["composer-file-picker"];
  if (!picker) return;
  picker.classList.add("hidden");
  elements["file-button"]?.setAttribute("aria-expanded", "false");
}

async function openFilePicker() {
  const picker = elements["composer-file-picker"];
  if (!picker) return;
  picker.classList.remove("hidden");
  elements["file-button"]?.setAttribute("aria-expanded", "true");
  elements["composer-file-search"].value = "";
  state.filePickerSelected = 0;
  renderFilePickerResults();
  elements["composer-file-search"].focus();
  // 首次打开（或换了工作区）补一次索引：此后按键只筛内存，不读盘。
  if (fileRefIndexStale()) {
    await ensureFileRefIndex();
    if (!picker.classList.contains("hidden")) renderFilePickerResults();
  }
}

function renderFilePickerResults() {
  const query = elements["composer-file-search"]?.value || "";
  const items = fileReferenceCandidates(fileRefIndex, query, FILE_REF_CANDIDATE_LIMIT);
  state.filePickerItems = items;
  state.filePickerSelected = Math.min(state.filePickerSelected, Math.max(items.length - 1, 0));
  renderSuggestionList(elements["composer-file-results"], fileReferenceRows(items), state.filePickerSelected, FILE_REF_TRIGGER, FILE_REF_CANDIDATE_LIMIT, "没有匹配的工作区路径");
}

// 内联面板的 `@` 分支：候选只来自已加载索引（首次为空时补一次），**不调 Bridge**
// （后端的 Suggestions 表里没有 `@`——它不是前缀）。
function renderFileReferenceSuggestions(request, query) {
  const items = fileReferenceCandidates(fileRefIndex, query, FILE_REF_CANDIDATE_LIMIT);
  if (request !== state.inlineRequest) return;
  state.inlineSuggestions = fileReferenceRows(items);
  state.inlineSelected = Math.min(state.inlineSelected, Math.max(state.inlineSuggestions.length - 1, 0));
  elements["inline-suggestions"].classList.toggle("hidden", state.inlineSuggestions.length === 0);
  renderSuggestionList(elements["inline-suggestions"], state.inlineSuggestions, state.inlineSelected, FILE_REF_TRIGGER, FILE_REF_CANDIDATE_LIMIT, "没有匹配的工作区路径");
}

function wireFileReferenceEntry() {
  elements["file-button"]?.addEventListener("click", () => {
    if (elements["composer-file-picker"]?.classList.contains("hidden")) openFilePicker();
    else closeFilePicker();
  });
  elements["composer-file-close"]?.addEventListener("click", () => { closeFilePicker(); elements.prompt.focus(); });
  elements["composer-file-search"]?.addEventListener("input", () => {
    state.filePickerSelected = 0;
    renderFilePickerResults();
  });
  elements["composer-file-search"]?.addEventListener("keydown", event => {
    const items = state.filePickerItems || [];
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      if (items.length === 0) return;
      const direction = event.key === "ArrowDown" ? 1 : -1;
      state.filePickerSelected = (state.filePickerSelected + direction + items.length) % items.length;
      renderFilePickerResults();
      return;
    }
    if (event.key === "Enter" || event.key === "Tab") {
      event.preventDefault();
      acceptFileReference(items[state.filePickerSelected]?.path || "");
      return;
    }
    if (event.key === "Escape") {
      event.preventDefault();
      closeFilePicker();
      elements.prompt.focus();
    }
  });
  // 一条容器委托管两处 chip：草稿条的 × 删除、正文 chip 的"点开右栏详情"。
  // （草稿 chip 的正文点击不做事：它是引用标记，不是入口——入口是 × 与面板。）
  document.addEventListener("click", event => {
    // 点在选择器/加号之外 → 收起面板（不吞这次点击）。
    const picker = elements["composer-file-picker"];
    if (picker && !picker.classList.contains("hidden")
      && !event.target?.closest?.("#composer-file-picker")
      && !event.target?.closest?.("#file-button")) {
      closeFilePicker();
    }
    const remove = event.target?.closest?.("[data-file-ref-remove]");
    if (remove) {
      const chip = remove.closest("[data-file-ref]");
      if (chip) {
        elements.prompt.value = removeFileReference(elements.prompt.value, chip.dataset.fileRef);
        markComposerEdited();
        resizePrompt();
        renderComposerFileRefs();
        elements.prompt.focus();
      }
      return;
    }
    const chip = event.target?.closest?.(".message-body [data-file-ref]");
    if (!chip) {
      // 草稿区的 chip 只是引用标记（入口是 × 与面板）：点它把焦点还给输入框。
      if (event.target?.closest?.("[data-file-ref-host=\"composer\"]")) elements.prompt.focus();
      return;
    }
    const path = chip.dataset.fileRef || "";
    if (path === "") return;
    // 目录引用不是可预览的"文件"：说清它归 agent 自己列，别打开一个读不了的面板。
    if (chip.dataset.fileRefDir === "1") {
      showToast(`目录引用：${path}/（内容由 agent 自行列目录读取）`);
      return;
    }
    openFilePreview({ path, name: path.split("/").pop() || path });
  });
}
wireFileReferenceEntry();

async function updateInlineSuggestions() {
  // `@` 是**光标处的引用标记**，不是"整条输入必须以 @ 开头"：用户常在一句话中间引文件
  // （"看看 @src/app"）。判据因此落在**光标所在的那个词**上（file-reference.js 的
  // fileReferenceQuery：`@` 必须开启这个词，`user@example.com` 这类粘连词不误认）——
  // 光标在 `@词` 里就给候选，在别处就不给。命中时**不调 Bridge**：后端的 Suggestions
  // 表里没有 `@`（J-1a 起它不是前缀），候选只来自已加载的工作区树。
  const cursor = elements.prompt.selectionStart ?? elements.prompt.value.length;
  const reference = fileReferenceQuery(elements.prompt.value, cursor);
  if (reference) {
    const request = ++state.inlineRequest;
    state.inlineTrigger = FILE_REF_TRIGGER;
    renderFileReferenceSuggestions(request, reference.query);
    if (fileRefIndexStale()) {
      // 首次（或换工作区后）补一次索引，此后按键只筛内存（见 ensureFileRefIndex）。
      ensureFileRefIndex().then(() => renderFileReferenceSuggestions(request, reference.query));
    }
    return;
  }
  const input = elements.prompt.value.trimStart();
  if (!SIGIL_PATTERN.test(input)) {
    hideInlineSuggestions();
    return;
  }
  const request = ++state.inlineRequest;
  state.inlineTrigger = input[0];
  try {
    const suggestions = await invoke("Suggestions", input) || [];
    if (request !== state.inlineRequest) return;
    state.inlineSuggestions = suggestions.slice(0, 8);
    state.inlineSelected = Math.min(state.inlineSelected, Math.max(state.inlineSuggestions.length - 1, 0));
    elements["inline-suggestions"].classList.toggle("hidden", state.inlineSuggestions.length === 0);
    renderSuggestionList(elements["inline-suggestions"], state.inlineSuggestions, state.inlineSelected, state.inlineTrigger, 8);
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
    closeFilePicker();
    // 已发出的引用进了正文（后面 refresh 会渲染消息 chip）；草稿里剩下的那部分
    // （提交往返期间追加的新输入）重新算一次 chip 条。
    renderComposerFileRefs();
    resizePrompt();
    // 团队事实不在这里猜，也不由前端从提交文本里推：`@` 手动召唤已退场（J-1a），
    // 装配只剩 goal / teamwork 的自动通道，事实变化由后端的 team.changed 事件通告
    // （前端只拿它热更新开着的角色会话读面，见 renderIncremental）。
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
      renderSuggestionList(elements["inline-suggestions"], state.inlineSuggestions, state.inlineSelected, state.inlineTrigger, state.inlineTrigger === FILE_REF_TRIGGER ? FILE_REF_CANDIDATE_LIMIT : 8);
      return;
    }
    if (event.key === "Tab") {
      event.preventDefault();
      acceptSuggestion(state.inlineSuggestions[state.inlineSelected], state.inlineTrigger);
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
  // 草稿里的引用 chip 条跟着正文重画（同一枚 chip 的草稿宿主）。
  renderComposerFileRefs();
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
let newProjectDrafts = [];

function openNewSessionModal() {
  showNewSessionStep(1);
  renderNewProjectDrafts();
  setModal("new-session-modal", true);
}

function closeNewSessionModal() {
  setModal("new-session-modal", false);
  showNewSessionStep(1);
}

function showNewSessionStep(step) {
  elements["new-session-step-1"].classList.toggle("hidden", step !== 1);
  elements["new-session-step-2"].classList.toggle("hidden", step !== 2);
}

async function beginNewSession() {
  try {
    showLeftPage("tasks");
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

function newProjectNameFromPath(path) {
  return String(path || "").replace(/[\\/]+$/, "").split(/[\\/]/).pop() || "project";
}

function addNewProjectDraft(rootPath) {
  const path = String(rootPath || "").trim();
  if (!path) return false;
  const key = normalizePath(path);
  if (newProjectDrafts.some(draft => normalizePath(draft.rootPath) === key)) return false;
  newProjectDrafts.push({ name: newProjectNameFromPath(path), rootPath: path });
  renderNewProjectDrafts();
  return true;
}

function renderNewProjectDrafts() {
  const host = elements["new-project-drafts"];
  const create = elements["new-project-create"];
  if (!host) return;
  host.innerHTML = newProjectDrafts.length
    ? newProjectDrafts.map((draft, index) => `
      <div class="new-project-draft-row" data-new-project-draft="${index}">
        <span class="new-project-draft-index">${String(index + 1).padStart(2, "0")}</span>
        <span class="new-project-draft-main">
          <input class="new-project-draft-name" data-new-project-name="${index}" value="${escapeHtml(draft.name)}" aria-label="项目名称" spellcheck="false">
          <span class="new-project-draft-path" title="${escapeHtml(draft.rootPath)}">${escapeHtml(draft.rootPath)}</span>
        </span>
        <button class="icon-button subtle new-project-draft-remove" type="button" data-new-project-remove="${index}" title="移出待创建列表" aria-label="移出待创建列表" data-icon="close"></button>
      </div>`).join("")
    : '<div class="new-project-empty">尚未加入项目文件夹</div>';
  hydrateIcons(host);
  if (create) {
    create.disabled = newProjectDrafts.length === 0;
    create.textContent = newProjectDrafts.length ? `创建 ${newProjectDrafts.length} 个项目` : "创建项目";
  }
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

elements["new-project-drafts"].addEventListener("input", event => {
  const input = event.target.closest?.("[data-new-project-name]");
  if (!input) return;
  const index = Number(input.dataset.newProjectName);
  if (Number.isInteger(index) && newProjectDrafts[index]) newProjectDrafts[index].name = input.value;
});

elements["new-project-drafts"].addEventListener("click", event => {
  const button = event.target.closest?.("[data-new-project-remove]");
  if (!button) return;
  const index = Number(button.dataset.newProjectRemove);
  if (!Number.isInteger(index)) return;
  newProjectDrafts.splice(index, 1);
  renderNewProjectDrafts();
});

elements["new-project-create"].addEventListener("click", async () => {
  if (newProjectDrafts.length === 0) return;
  const requests = newProjectDrafts.map(draft => ({
    name: String(draft.name || "").trim(),
    root_path: draft.rootPath,
    git_remote: ""
  }));
  if (requests.some(request => !request.name)) {
    showToast("项目名称不能为空");
    return;
  }
  const button = elements["new-project-create"];
  button.disabled = true;
  try {
    const result = await invoke("CreateWorkspaces", requests);
    const created = Array.isArray(result?.workspaces) ? result.workspaces : [];
    newProjectDrafts = [];
    renderNewProjectDrafts();
    await refresh({ scroll: false });
    renderNewSessionWorkspaces();
    showToast(created.length ? `已创建 ${created.length} 个项目` : "项目已创建");
  } catch (error) {
    showToast(error);
  } finally {
    renderNewProjectDrafts();
  }
});

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
    if (!addNewProjectDraft(dir)) showToast("这个文件夹已在待创建列表中");
  } catch (error) { showToast(error); }
});

elements["runtime-button"].addEventListener("click", openRuntime);
elements["runtime-close"].addEventListener("click", closeRuntime);
elements["settings-button"].addEventListener("click", openSettings);
elements["settings-close"].addEventListener("click", closeSettings);
elements["settings-nav"]?.addEventListener("click", event => {
  const button = event.target?.closest?.("[data-settings-page]");
  if (button?.dataset.settingsPage) setSettingsPage(button.dataset.settingsPage);
});
elements["account-add-cancel"]?.addEventListener("click", closeAccountAdd);
elements["account-add-cancel-footer"]?.addEventListener("click", closeAccountAdd);
elements["project-settings-button"]?.addEventListener("click", () => { void openProjectSettings(); });
elements["session-permission-close"]?.addEventListener("click", closeSessionPermissions);
elements["session-environment-confirm"]?.addEventListener("click", async () => {
  const select = elements["session-environment-select"];
  const env = String(select?.value || "").trim();
  if (!env) return;
  if (elements["session-environment-confirm"]) elements["session-environment-confirm"].disabled = true;
  if (elements["session-environment-status"]) elements["session-environment-status"].textContent = `正在固定 ${env}…`;
  try {
    const state = await invoke("SetSessionSSHEnv", currentWorkspaceID(), env);
    sessionEnvironmentCurrent = String(state?.current_env || env);
    renderSessionEnvironmentSection();
    showToast(`本会话已固定工作环境：${sessionEnvironmentCurrent}`);
  } catch (error) {
    if (elements["session-environment-status"]) elements["session-environment-status"].textContent = `固定失败：${error?.message || error}`;
    showToast(error);
  }
});
elements["project-settings-close"]?.addEventListener("click", closeProjectSettings);
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

// 周期锚点只在"日及以上"的单位上出现：子日周期（分钟/小时）没有"几点开始"
// 可言，它的起点就是创建时刻。与后端 validatePeriod 同一份口径（那边拒绝，
// 这边不显示，免得用户点了才被拒）。
const SCHED_ANCHOR_UNITS = new Set(["day", "week", "month"]);

// editingScheduledTaskID 非空 = 弹窗处于编辑态（提交走 UpdateScheduledTask）。
// 与"草稿"同一条纪律：弹窗是**同一套控件**的两个模式，不另建一份表单。
let editingScheduledTaskID = "";

// openScheduledTaskDialog 打开中栏编辑器：新建（无参）= 回到默认（周期重复 / 每天 /
// 开始时间空 → 需要用户在"开始时间"与"每个周期按当前时间"里选一个）；编辑
// (= 任务快照) = 用 scheduledTaskFormFields 把这条任务填回同一套控件。
function openScheduledTaskDialog(task) {
  const editing = Boolean(task?.id);
  editingScheduledTaskID = editing ? String(task.id) : "";
  const fields = editing
    ? scheduledTaskFormFields(task)
    : { name: "", prompt: "", mode: "period", periodValue: "1", periodUnit: "day", startClock: "", startWeekday: "1", anchorNow: false, runAtValue: "", enabled: true, workspaceId: "", reasoningEffort: "", loopLimit: "" };
  elements["sched-name"].value = fields.name;
  elements["sched-prompt"].value = fields.prompt;
  renderScheduledWorkspaceOptions(fields.workspaceId);
  renderScheduledAssemblyOptions(fields);
  elements["sched-mode"].value = fields.mode;
  elements["sched-period-value"].value = fields.periodValue;
  elements["sched-period-unit"].value = fields.periodUnit;
  elements["sched-start-clock"].value = fields.startClock;
  elements["sched-start-weekday"].value = fields.startWeekday;
  elements["sched-anchor-now"].checked = fields.anchorNow;
  elements["sched-datetime"].value = fields.runAtValue;
  elements["sched-enabled"].checked = fields.enabled;
  // 同一个中栏编辑器的两个模式：标题、提交按钮、启用勾的文案都跟着走。
  elements["scheduled-task-modal-title"].textContent = editing ? "编辑定时任务" : "新建定时任务";
  elements["sched-enabled-label"].textContent = editing ? "启用" : "创建后立即启用";
  elements["sched-submit"].textContent = editing ? "保存修改" : "创建任务";
  syncScheduledTaskFields();
  showLeftPage("scheduled");
  document.querySelector(".workspace")?.classList.add("is-task-schedule");
  elements["scheduled-editor"]?.classList.remove("hidden");
  elements["sched-name"].focus();
}

// renderScheduledWorkspaceOptions 用快照里的工作区列表填新建弹窗的下拉：
// 空值 = 无工作区（新会话不绑项目）。任务存的是工作区 ID，名称只做展示。
// preferredID = 要选中的工作区（新建时是当前会话的工作区；编辑时是这条任务
// 自己绑的工作区——编辑不该把工作区悄悄改成"当前会话那个"）。
function renderScheduledWorkspaceOptions(preferredID) {
  const select = elements["sched-workspace"];
  if (!select) return;
  const snapshot = client.current() || {};
  const workspaces = Array.isArray(snapshot.workspaces) ? snapshot.workspaces : [];
  const current = snapshot.current_workspace || null;
  select.innerHTML = [`<option value="">（无工作区）</option>`]
    .concat(workspaces.map(workspace =>
      `<option value="${escapeHtml(workspace.id)}">${escapeHtml(workspace.name)}${workspace.root_path ? ` — ${escapeHtml(workspace.root_path)}` : ""}</option>`))
    .join("");
  // 新建默认跟随当前会话的工作区：用户多半就是想让任务在这个项目里跑。
  const preferred = preferredID || current?.id || "";
  select.value = workspaces.some(workspace => workspace.id === preferred) ? preferred : "";
}

// renderScheduledAssemblyOptions 填两格装配选项：
//   - 权限档位：选项来自快照的 runtime.permission_tiers（后端下发，前端不自造
//     档位名——两处各写一套就会漂移）。目录为空（裸宿主/测试桩）时不加选项，
//     提交的档位是空串 = 后端按默认 full access 处理。
//   - 插件装配：选项来自快照的 runtime.plugins；多选，空选 = 继承宿主当前激活
//     插件。默认选中 preferred.plugins（编辑态 = 这条任务自己声明的那几个）。
function renderScheduledAssemblyOptions(preferred) {
  const runtime = (client.current() || {}).runtime || {};
  const tierSelect = elements["sched-permission"];
  if (tierSelect) {
    const tiers = Array.isArray(runtime.permission_tiers) ? runtime.permission_tiers : [];
    tierSelect.innerHTML = [`<option value="">（默认：全权）</option>`]
      .concat(tiers.map(tier => `<option value="${escapeHtml(tier.id)}">${escapeHtml(tier.label || tier.short || tier.id)}</option>`))
      .join("");
    const tier = String(preferred?.permissionTier || "");
    tierSelect.value = tiers.some(item => item.id === tier) ? tier : "";
  }
  const pluginSelect = elements["sched-plugins"];
  if (pluginSelect) {
    const plugins = Array.isArray(runtime.plugins) ? runtime.plugins : [];
    pluginSelect.innerHTML = plugins
      .filter(plugin => plugin?.name)
      .map(plugin => `<option value="${escapeHtml(plugin.name)}" title="${escapeHtml(plugin.description || plugin.name)}">${escapeHtml(plugin.name)}</option>`)
      .join("");
    const selected = new Set(Array.isArray(preferred?.plugins) ? preferred.plugins : []);
    for (const option of pluginSelect.options) {
      option.selected = selected.has(option.value);
    }
  }
  // 思考强度：停位与文案复用 composer 那条拉条的**同一份**清单——拉条与任务声明停的
  // 是同一组 wire 值（low/medium/high/max），界面不该有第二套词；空选项 = 不声明，
  // 后端按默认档位落默认值。
  const effortSelect = elements["sched-effort"];
  if (effortSelect) {
    effortSelect.innerHTML = [`<option value="">（默认：与默认档位一致）</option>`]
      .concat(REASONING_EFFORT_LEVELS.map(level =>
        `<option value="${escapeHtml(level)}">${escapeHtml(REASONING_EFFORT_LABELS[level] || level)}</option>`))
      .join("");
    const effort = String(preferred?.reasoningEffort || "");
    effortSelect.value = REASONING_EFFORT_LEVELS.includes(effort) ? effort : "";
  }
  // 循环次数：数字控件，空 = 不声明（后端落默认档位的那条读数）。区间（1..input_loop_limit）
  // 由后端判，前端只给最小步进，不替它猜上界。
  const loopsInput = elements["sched-loops"];
  if (loopsInput) loopsInput.value = String(preferred?.loopLimit ?? "");
}

function closeScheduledTaskDialog() {
  editingScheduledTaskID = "";
  document.querySelector(".workspace")?.classList.remove("is-task-schedule");
  elements["scheduled-editor"]?.classList.add("hidden");
  syncSessionChrome();
}

// syncScheduledTaskFields 联动字段可见性：一次性模式只要执行时间；周期模式
// 按单位给锚点（周多一个"星期几"），子日单位不给锚点。
function syncScheduledTaskFields() {
  const atMode = elements["sched-mode"].value === "at";
  const anchorUnit = SCHED_ANCHOR_UNITS.has(elements["sched-period-unit"].value);
  elements["sched-period-field"].classList.toggle("hidden", atMode);
  elements["sched-datetime-field"].classList.toggle("hidden", !atMode);
  elements["sched-enabled-field"].classList.toggle("hidden", atMode);
  elements["sched-anchor-field"].classList.toggle("hidden", atMode || !anchorUnit);
  elements["sched-anchor-now-field"].classList.toggle("hidden", atMode || !anchorUnit);
  elements["sched-start-weekday"].classList.toggle("hidden", atMode || elements["sched-period-unit"].value !== "week");
  // 两个锚点口径互斥：勾了"当前时间"就不必再填墙钟（填了也不生效，置灰更诚实）。
  const anchorNow = elements["sched-anchor-now"].checked;
  elements["sched-start-clock"].disabled = anchorNow;
  if (anchorNow) elements["sched-start-clock"].value = "";
}

// submitScheduledTask 组装任务入参并提交 Bridge：新建走 ScheduleTask，编辑走
// UpdateScheduledTask（ID 是操作键）。字段口径全在 scheduled-tasks-view.js 的
// buildScheduledTaskSpec（纯函数，node 用例钉住载荷形状——Wails 绑定层用
// encoding/json 反序列化，time.Time 字段收到空串会当场报
// "error parsing arguments: parsing time ..."，Go 侧根本进不去）。
async function submitScheduledTask() {
  const editingTaskID = editingScheduledTaskID;
  const built = buildScheduledTaskSpec({
    name: elements["sched-name"].value,
    prompt: elements["sched-prompt"].value,
    workspaceId: elements["sched-workspace"]?.value || "",
    // 编辑态把原任务的会话绑定原样带回（面板不编辑它，也不该在保存时清掉）。
    sessionId: editingTaskID ? scheduledTaskByID(editingTaskID)?.session_id || "" : "",
    // 装配：档位空 = 后端默认 full access；插件空 = 继承宿主当前激活插件。
    permissionTier: elements["sched-permission"]?.value || "",
    plugins: [...(elements["sched-plugins"]?.selectedOptions || [])].map(option => option.value),
    // 执行参数：留空 = 不声明（后端按默认档位落默认参数）；取值合法性由后端判。
    reasoningEffort: elements["sched-effort"]?.value || "",
    loopLimit: elements["sched-loops"]?.value ?? "",
    mode: elements["sched-mode"].value,
    periodValue: elements["sched-period-value"].value,
    periodUnit: elements["sched-period-unit"].value,
    startClock: elements["sched-start-clock"].value,
    startWeekday: elements["sched-start-weekday"].value,
    anchorNow: elements["sched-anchor-now"].checked,
    runAtValue: elements["sched-datetime"].value,
    now: Date.now(),
    enabled: elements["sched-enabled"].checked
  });
  if (built.error) {
    showToast(built.error);
    return;
  }
  try {
    if (editingTaskID) {
      await invoke("UpdateScheduledTask", editingTaskID, built.spec);
    } else {
      await invoke("ScheduleTask", built.spec);
    }
    closeScheduledTaskDialog();
    await refresh({ scroll: false });
  } catch (error) {
    showToast(error);
  }
}

// scheduledTaskByID 从当前快照里按 ID 找任务（ID 是操作键；找不到返回 null）。
function scheduledTaskByID(id) {
  const runtime = (client.current() || {}).runtime || {};
  const tasks = Array.isArray(runtime.scheduled_tasks) ? runtime.scheduled_tasks : [];
  return tasks.find(task => task?.id === id) || null;
}

// openScheduledTaskEditor 打开中栏编辑视图：按 ID 取当前快照里的那条任务填表
// （取不到就按"任务已经没了"提示，不打开一个空表单）。
function openScheduledTaskEditor(id) {
  const task = scheduledTaskByID(id);
  if (!task) {
    showToast("任务不存在（可能已被取消），请刷新后重试");
    return;
  }
  openScheduledTaskDialog(task);
}

elements["new-scheduled-task"].addEventListener("click", () => openScheduledTaskDialog());
elements["scheduled-task-close"].addEventListener("click", closeScheduledTaskDialog);
elements["scheduled-task-cancel"]?.addEventListener("click", closeScheduledTaskDialog);
elements["sched-mode"].addEventListener("change", syncScheduledTaskFields);
elements["sched-period-unit"].addEventListener("change", syncScheduledTaskFields);
elements["sched-anchor-now"].addEventListener("change", syncScheduledTaskFields);
elements["sched-start-clock"].addEventListener("change", () => {
  // 填了墙钟就把"当前时间"这把勾松开——两者互斥，留着会让提交口径含糊。
  if (elements["sched-start-clock"].value) elements["sched-anchor-now"].checked = false;
  syncScheduledTaskFields();
});
elements["sched-submit"].addEventListener("click", submitScheduledTask);

// 编辑/取消按钮事件委托（任务列表渲染全量刷新，事件挂容器层；ID 是操作键）。
elements["scheduled-task-view"].addEventListener("click", async event => {
  const editButton = event.target.closest?.("[data-sched-edit]");
  if (editButton?.dataset.schedEdit) {
    openScheduledTaskEditor(editButton.dataset.schedEdit);
    return;
  }
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

for (const [modalID, close] of [["runtime-modal", closeRuntime], ["command-modal", closeCommandPalette], ["settings-modal", closeSettings], ["session-permission-modal", closeSessionPermissions], ["node-detail-modal", closeNodeDetail], ["new-session-modal", closeNewSessionModal], ["role-session-modal", closeRoleSessionDetail], ["team-page-modal", closeTeamPage], ["compaction-frame-modal", closeCompactionFrame]]) {
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

// ── 会话内 HTML 渲染块的动作通道（iframe → 宿主 · postMessage）────────────
// 块被关在无 `allow-same-origin` 的沙箱 iframe 里，块内脚本够不到应用；反方向只有
// postMessage 一条路。身份**按 `event.source` 与本页既有渲染块的 contentWindow 比对**，
// 不按 origin：opaque origin 恒为 `"null"`，按它判等于不判（判据与白名单在
// embed-bridge.js，这里只做接线与落点）。
const embedActionGate = createEmbedActionGate();

function embedFrameFor(source) {
  if (!source) return null;
  for (const frame of document.querySelectorAll("iframe.html-embed-frame")) {
    if (frame.contentWindow === source) return frame;
  }
  return null;
}

window.addEventListener("message", event => {
  const frame = embedFrameFor(event.source);
  // 来源不是本页的渲染块（别的窗口、扩展、父页面）：静默忽略。块内本来就没有回执
  // 通道，报错刷屏只会把"没收到"误读成故障。
  if (!frame) return;
  const decision = resolveEmbedAction({
    data: event.data,
    interactive: frame.closest(".html-embed")?.dataset.embedInteractive === "1",
    allow: seed => embedActionGate.allow(seed)
  });
  if (!decision.ok) return;
  void applyEmbedAction(decision, frame);
});

// applyEmbedAction 把一次通过判据的动作落到宿主动作上。落点只有四个，且都复用既有
// 通道：复制同对话区的复制、发消息同 composer 的提交规划（`SubmitToSession` + 显式
// 视图会话），所以队列、草稿清理、sigil 前缀的行为与用户自己按 Enter 完全一致。
async function applyEmbedAction(decision, frame) {
  if (decision.action === "copy-text") {
    try {
      await navigator.clipboard.writeText(decision.payload.text);
      showToast("已复制图形视图的文本");
    } catch (error) { showToast(error); }
    return;
  }
  if (decision.action === "open-source") {
    const source = frame.closest(".html-embed")?.querySelector("details.html-embed-source");
    if (source) source.open = !source.open;
    return;
  }
  if (decision.action === "fill-composer") {
    // 追加而不是覆盖：用户手边的半句草稿不是这一块可以拿去的东西（同召回排队
    // 消息的语义）。块的动作只负责把话放进去，发不发由用户按 Enter 决定。
    const existing = elements.prompt.value;
    elements.prompt.value = existing.trim() ? `${existing.trimEnd()}\n${decision.payload.text}` : decision.payload.text;
    markComposerEdited();
    resizePrompt();
    elements.prompt.focus();
    showToast("图形视图已写入输入框（未发送）");
    return;
  }
  if (decision.action === "ask-agent") {
    // 不动输入框：这是块自己那条请求，不该覆盖用户手边的草稿。走与输入框提交
    // 同一条规划（composer-input.js），因此发往的会话、队列与 sigil 路由一致。
    const plan = composerSubmitPlan({ text: decision.payload.text, viewedSessionID: client.current()?.session?.id });
    try {
      await invoke(plan.rpc, ...plan.args);
      showToast("图形视图已向本会话发送一条请求");
    } catch (error) { showToast(error); }
    await refresh({ scroll: "bottom" });
  }
}

document.addEventListener("keydown", event => {
  if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
    event.preventDefault();
    openCommandPalette("/");
  }
  if (event.key === "Escape") {
    // 保存选择弹窗优先：Esc = 取消这次决定（不动作、不丢内容），不顺势往下关抽屉。
    if (pendingSaveChoice) {
      resolveSaveChoice("cancel");
      return;
    }
    if (!elements["account-add-modal"]?.classList.contains("hidden")) {
      closeAccountAdd();
      return;
    }
    if (!elements["environment-editor-modal"]?.classList.contains("hidden")) {
      closeEnvironmentEditor();
      return;
    }
    if (!elements["project-settings-modal"]?.classList.contains("hidden")) {
      closeProjectSettings();
      return;
    }
    closePermissionMenu();
    closeRuntime();
    closeCommandPalette();
    closeSettings();
    closeScheduledTaskDialog();
    closeNodeDetail();
    closeNewSessionModal();
    closeFilePreview();
  }
});
elements["account-add-modal"]?.addEventListener("click", event => {
  if (event.target === elements["account-add-modal"]) closeAccountAdd();
});
elements["project-settings-modal"]?.addEventListener("click", event => {
  if (event.target === elements["project-settings-modal"]) closeProjectSettings();
});
elements["environment-editor-modal"]?.addEventListener("click", event => {
  if (event.target === elements["environment-editor-modal"]) closeEnvironmentEditor();
});

// 权限芯片：点击**就地展开**主代理权限向量（不再是档位；档位只能往松里拨一格，
// 表达不了“只给 SSH”或“明确拒掉 SSH”这类逐格点名）。
elements["perm-toggle"].addEventListener("click", function(event) {
  event.stopPropagation();
  togglePermissionMenu();
});

// 下拉条目 = 主代理权限标签：点一下三态循环（空→给→拒→空），改完立刻整组提交。
// 与设置面板的向量编辑走同一条提交路径（submitPermissionVector），不各写一份。
elements["perm-menu"].addEventListener("click", async function(event) {
  const tag = event.target?.closest?.("button[data-perm-type]");
  if (!tag) {
    if (event.target?.closest?.("[data-open-session-permissions]")) {
      event.stopPropagation();
      closePermissionMenu();
      await openSessionPermissions();
    }
    return;
  }
  event.stopPropagation();
  const next = cyclePermissionVector(permissionVectorDraft, tag.dataset.permType);
  if (next.join(",") === permissionVectorDraft.join(",")) return;
  permissionVectorDraft = next;
  renderPermissionMenu(client.current()?.runtime || {});
  await submitPermissionVector();
});

// 下拉键盘：↑/↓ 在标签间循环（nextTierIndex 是通用循环取模），Enter/空格 = 点一下。
elements["perm-menu"].addEventListener("keydown", async function(event) {
  if (!["ArrowDown", "ArrowUp", "Enter", " "].includes(event.key)) return;
  const items = Array.from(this.querySelectorAll("button[data-perm-type]"));
  if (!items.length) return;
  event.preventDefault();
  const current = items.indexOf(document.activeElement);
  if (event.key === "Enter" || event.key === " ") {
    const target = current >= 0 ? items[current] : null;
    if (!target) return;
    permissionVectorDraft = cyclePermissionVector(permissionVectorDraft, target.dataset.permType);
    renderPermissionMenu(client.current()?.runtime || {});
    await submitPermissionVector();
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
  if (open) menu.querySelector("button[data-perm-type]")?.focus?.();
}

function closePermissionMenu() {
  togglePermissionMenu(false);
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
// ── 两栏宽度预算 ──────────────────────────────────────────────────────────
// .app-shell 的列是 `nav + 任务列表 + minmax(0,1fr) + 右栏`：任务列表与右栏
// 宽度之和 + 一级导航 + 主视图必须 ≤ 视口，否则网格整体溢出容器，最右那一栏
// （右栏）被挤出视口——现场（2026-10-01）「调整边框大小时右栏消失」就是这么来的：
// 旧口径只按 `viewport-280` **逐栏**钳制，两栏之和可以远超视口，而左栏锚在视口
// 左边、看起来没事。用户口径：右栏要「跟左栏一样保持大小，最好保持比例」。
const MAIN_VIEW_MIN = 280; // 中间主视图至少保留的宽度
const PANEL_DIVIDERS = 0; // 分隔条覆盖在边界上，不占列宽
const PANEL_WIDTH_FLOOR = 200; // 单栏下限（预算极小时的最后兜底）

function viewportWidth() {
  return window.innerWidth || 1280;
}
function navPanelWidth() {
  const value = parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--nav-w"));
  if (Number.isFinite(value) && value > 0) return value;
  return document.documentElement.dataset.leftNavCollapsed === "true" ? 52 : 112;
}
// sidePaneWidthBudget 两栏**共用**的可用宽度预算。
function sidePaneWidthBudget() {
  return Math.max(0, viewportWidth() - navPanelWidth() - MAIN_VIEW_MIN - PANEL_DIVIDERS);
}
// sidePaneMaxWidth 单栏宽度上限。拖拽时传入另一栏当前宽度：一栏拉宽不该把另一栏
// 挤出视口（旧口径只留 320 的绝对下限，等同于放任溢出）。
function sidePaneMaxWidth(otherWidth = 0) {
  const budget = sidePaneWidthBudget() - Math.max(0, Number(otherWidth) || 0);
  return Math.max(PANEL_WIDTH_FLOOR, budget);
}
// applyPanelWidths 把两栏宽度落到 CSS 变量：先各自钳制，两栏之和超预算时**按
// 比例收敛**（用户口径「最好保持比例」）。写回的是 CSS 变量而不是 localStorage
// ——收起再展开、或窗口重新变大，用户原来拖出来的宽度仍原样恢复（与
// previewPaneWidth 同一条口径：存的是用户意图，钳制只影响当前这一次渲染）。
function applyPanelWidths() {
  const budget = sidePaneWidthBudget();
  const cap = Math.max(PANEL_WIDTH_FLOOR, budget);
  let left = clampPanelWidth(Number(storageGet(LEFT_WIDTH_KEY)) || 268, LEFT_WIDTH_MIN, cap);
  let right = clampPanelWidth(Number(storageGet(RIGHT_WIDTH_KEY)) || 300, RIGHT_WIDTH_MIN, cap);
  if (budget > 0 && left + right > budget) {
    const scale = budget / (left + right);
    left = Math.max(LEFT_WIDTH_MIN, Math.round(left * scale));
    right = Math.max(RIGHT_WIDTH_MIN, Math.round(right * scale));
  }
  // 收敛到两栏下限后仍装不下（预算小于两栏下限之和）：把余量差让给右栏，宁可
  // 它窄到下界也不让它整条被顶出视口。主视图由 minmax(0,1fr) 自行收窄。
  if (budget > 0 && left + right > budget) {
    right = Math.max(RIGHT_WIDTH_MIN, budget - left);
  }
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

  // setWidth：把拖出来的宽度落到 CSS 变量并落盘。otherWidth 是**另一栏**当前
  // 宽度——上限从两栏共用预算里再扣掉它，一栏拉宽因此不会把另一栏挤出视口。
  function setWidth(variable, key, min, width, otherWidth = 0) {
    const clamped = clampPanelWidth(width, min, sidePaneMaxWidth(otherWidth));
    document.documentElement.style.setProperty(variable, `${clamped}px`);
    storageSet(key, String(clamped));
    return clamped;
  }
  // currentPanelWidth 读当前 CSS 变量里的栏宽（拖拽钳制时要扣掉的那一栏）。
  function currentPanelWidth(variable, fallback) {
    const raw = parseFloat(document.documentElement.style.getPropertyValue(variable));
    return Number.isFinite(raw) && raw > 0 ? raw : fallback;
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
    return setWidth("--left-w", LEFT_WIDTH_KEY, LEFT_WIDTH_MIN, event.clientX - shell.getBoundingClientRect().left - navPanelWidth(), currentPanelWidth("--right-w", 300));
  }));
  rightDivider.addEventListener("pointerdown", beginDrag(rightDivider, event => {
    return setWidth("--right-w", RIGHT_WIDTH_KEY, RIGHT_WIDTH_MIN, shell.getBoundingClientRect().right - event.clientX, currentPanelWidth("--left-w", 268));
  }));
  // 双击分隔条 = 复位到默认宽度（左 268 / 右 300），拖歪了不用来回找。
  leftDivider.addEventListener("dblclick", () => setWidth("--left-w", LEFT_WIDTH_KEY, LEFT_WIDTH_MIN, 268, currentPanelWidth("--right-w", 300)));
  rightDivider.addEventListener("dblclick", () => setWidth("--right-w", RIGHT_WIDTH_KEY, RIGHT_WIDTH_MIN, 300, currentPanelWidth("--left-w", 268)));

  function keyboardAdjust(divider, isRight) {
    divider.addEventListener("keydown", event => {
      if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
      event.preventDefault();
      const step = event.key === "ArrowLeft" ? -16 : 16;
      let width;
      if (isRight) {
        const current = parseFloat(document.documentElement.style.getPropertyValue("--right-w")) || 300;
        width = setWidth("--right-w", RIGHT_WIDTH_KEY, RIGHT_WIDTH_MIN, current - step, currentPanelWidth("--left-w", 268));
      } else {
        const current = parseFloat(document.documentElement.style.getPropertyValue("--left-w")) || 268;
        width = setWidth("--left-w", LEFT_WIDTH_KEY, LEFT_WIDTH_MIN, current + step, currentPanelWidth("--right-w", 300));
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
  ".scheduled-editor-body",
  ".session-group-body",
  "#account-list",
  "#plugin-list",
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

// applyReviewRequest 消费快照里的 review_request（J-2 契约见 dto.ReviewRequest /
// review-request.js）：agent 请求「在侧边资源管理器打开这些待审阅文件」→ 展开内容详情
// 抽屉 → 逐个开 chip（**只有第一枚成为当前显示的详情**，其余只出 chip）。
//
// 抢视线取舍（§7.2，本轮保守默认）——**不强制切换用户当前正在看的子页 / 文件**：
//   1) 不调 revealView("code")：用户停在别的子页时，chip 与详情就绪但界面**不跳页**；
//   2) 抽屉里已有正在看的文件就保留它（不把视线拽到请求的第一份）；只有抽屉为空
//      （用户没在看任何详情）时才让第一枚成为当前显示；
//   3) 用户正在编辑（预览编辑器开着）时**整轮不抢**：不展开、不切文件，等下一次快照
//      再补放（decideReviewApply 不推进已应用水位，所以不会漏）。
// 这与契约一致：请求是**人可忽略、可撤销**的详情抽屉请求，不是模态框、不是置顶窗。
// 返回值是取舍原因（absent/replay/editing/new），便于诊断与测试。
function applyReviewRequest(snapshot) {
  const decision = decideReviewApply(snapshot, {
    appliedKey: reviewAppliedKey,
    editing: filePreviewController.editing()
  });
  if (!decision.apply) return decision.reason;
  reviewAppliedKey = decision.key;
  openReviewRequest(decision.request);
  return decision.reason;
}

// openReviewRequest 执行一次「打开这些待审阅文件」：展开内容详情抽屉，逐个开 chip。
// 与 openFilePreview 共用同一套抽屉展开逻辑，但**刻意不切子页、不播抽出示意动效**——
// 打开抽屉是"就绪"，不是"把用户的视线拉过来"（§7.2）。
function openReviewRequest(request) {
  previewRoot = client.current()?.current_workspace?.root_path || previewRoot;
  previewPaneOpen = true;
  previewCollapsed = false; // 展开 = 解除"收起让出内容页"
  const pane = elements["file-preview-pane"];
  if (pane) {
    pane.classList.remove("is-closed");
    document.documentElement.style.setProperty("--preview-w", previewPaneWidth());
  }
  syncPreviewLayout();
  filePreviewController.openDeferred(reviewRequestEntries(request), {
    activePath: filePreviewController.active()
  });
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

async function closeFilePreview() {
  const pane = elements["file-preview-pane"];
  if (!previewPaneOpen && (!pane || pane.classList.contains("is-closed"))) return;
  // 关之前先过未保存守卫：还有编辑没落盘时弹窗问保存/不保存/取消；用户取消
  // （或保存失败）就原样留着抽屉，不静默丢内容。
  if (!(await filePreviewController.requestCloseAll())) return;
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

// ── 文件详情的「保存选择」弹窗 ──────────────────────────────
// 「编辑参考 VS Code」的另一半：只有 Ctrl+S 才落盘，因此**退出编辑 / 关闭 chip /
// 收起抽屉**这三条路上若还有未保存内容，必须先问一次——保存 / 不保存 / 取消。
// 三个选项都是显式选择：没有默认动作，直接丢掉用户刚写的字是最不可接受的失败面。
// 返回 "save" | "discard" | "cancel"（未决弹窗只允许一枚：再次调用把上一枚按取消收口）。
let pendingSaveChoice = null;

function confirmPreviewSave({ paths = [] } = {}) {
  const list = Array.isArray(paths) ? paths.filter(Boolean) : [];
  if (!list.length) return Promise.resolve("discard");
  elements["file-save-message"].textContent = list.length === 1
    ? `${list[0]} 有未保存的修改。`
    : `${list.length} 个文件有未保存的修改：${list.join("、")}`;
  // 上一枚仍未决：按"取消"收口（绝不让两枚弹窗叠着，也绝不替用户选"不保存"）。
  if (pendingSaveChoice) pendingSaveChoice("cancel");
  setModal("file-save-modal", true);
  return new Promise(resolve => {
    pendingSaveChoice = choice => {
      pendingSaveChoice = null;
      setModal("file-save-modal", false);
      resolve(choice);
    };
  });
}

function resolveSaveChoice(choice) {
  const resolve = pendingSaveChoice;
  pendingSaveChoice = null;
  setModal("file-save-modal", false);
  if (resolve) resolve(choice);
}

elements["file-save-keep"].addEventListener("click", () => resolveSaveChoice("save"));
elements["file-save-discard"].addEventListener("click", () => resolveSaveChoice("discard"));
elements["file-save-cancel"].addEventListener("click", () => resolveSaveChoice("cancel"));
elements["file-save-modal"].addEventListener("click", event => {
  // 点遮罩 = 放弃这次决定（不动作，不丢内容）。
  if (event.target === elements["file-save-modal"]) resolveSaveChoice("cancel");
});

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

// ── 会话驻留窗口（设置面板 → 会话驻留）─────────────────────────────
// 权威在**后端进程级** limits（seelexctx 的 session_release_window）：设置弹窗是
// "打开时读一次"的面（invoke("SessionReleaseWindow")），提交走
// invoke("SetSessionReleaseWindow", n) 并**采纳后端返回的生效值**——与 composer 两条
// 拉条同一口径（后端可能拒绝或归一化，界面显示的必须是真正生效的那个）。
//
// 为什么不像拉条那样从快照读数：窗口是进程级设置，而快照的 runtime 是**按会话**组
// 装的投影，把它塞进去会让"切会话时它跟着动"变成一个假事实（见 model.SessionRuntime
// 的字段归属）。同一姿势：进程级设置都在弹窗打开时拉一次、不进快照。
//
// releaseWindowCommitted 是"最后一份权威读数"：提交失败时把控件回滚到它（后端在写盘
// 失败时**整步不改内存**，所以回滚是诚实的——不会回滚一个其实已经生效的值）。
let releaseWindowCommitted = null;

function renderReleaseWindowOptions() {
  const select = elements["release-window"];
  if (!select) return;
  select.innerHTML = releaseWindowOptions()
    .map(option => `<option value="${option.value}">${escapeHtml(option.label)}</option>`)
    .join("");
}

function setReleaseWindowStatus(text) {
  const status = elements["release-window-status"];
  if (status) status.textContent = text || "";
}

// syncReleaseWindowSelect 把控件对齐到一份权威值。null（没有可用读数）时保持原样并
// 说明原因，而不是把它兜成一个"看起来像默认"的值。
function syncReleaseWindowSelect(value) {
  const select = elements["release-window"];
  const normalized = normalizeReleaseWindow(value);
  if (!select) return;
  if (normalized === null) {
    setReleaseWindowStatus("读不到当前窗口（桥未就绪）；取值见 config/seelex.yaml 的 limits.session_release_window");
    return;
  }
  releaseWindowCommitted = normalized;
  select.value = String(normalized);
  setReleaseWindowStatus("");
}

// syncReleaseWindowReading 读一次后端权威（弹窗打开时调）。
// ── 工具记录开关（设置弹窗里那一格）────────────────────────
//
// 口径：它是**进程级设置**（落数据根 runtime-limits.json），开关的权威值在后端——
// 打开设置弹窗时读一次（`ToolRecordBoard().enabled`），拨动时走
// `SetToolRecordEnabled`（后端先落盘再改内存，失败整步不改），再把生效值写回控件。
// 关掉**不删**已记的读数：那是已经发生的事实，看板仍会显示它。
function setToolRecordSettingStatus(text) {
  const status = elements["tool-record-setting-status"];
  if (status) status.textContent = text || "";
}

function applyToolRecordSetting(enabled) {
  const checkbox = elements["tool-record-enabled-setting"];
  if (checkbox) checkbox.checked = Boolean(enabled);
}

async function syncToolRecordSetting() {
  try {
    const board = await invoke("ToolRecordBoard");
    applyToolRecordSetting(Boolean(board?.enabled));
    setToolRecordSettingStatus(
      Number(board?.total || 0) > 0 ? `已记录 ${board.total} 次调用（成功 ${board.success} / 失败 ${board.failure}）` : "",
    );
  } catch {
    setToolRecordSettingStatus("读不到当前开关状态（后端未装配工具记录）");
  }
  await renderToolRecordPanel();
}

elements["tool-record-enabled-setting"]?.addEventListener("change", async event => {
  const checkbox = event.target;
  const wanted = Boolean(checkbox.checked);
  checkbox.disabled = true;
  setToolRecordSettingStatus("正在应用…");
  try {
    const applied = await invoke("SetToolRecordEnabled", wanted);
    applyToolRecordSetting(Boolean(applied?.enabled));
    setToolRecordSettingStatus(Boolean(applied?.enabled) ? "已开启：成功与失败都会被记录" : "已关闭：不再记录新调用（已记的读数保留）");
  } catch (error) {
    // 失败整步不改：把控件回滚到后端当前值，并把原因说出来。
    applyToolRecordSetting(!wanted);
    setToolRecordSettingStatus("没生效：" + (error?.message || error));
  } finally {
    checkbox.disabled = false;
    await renderToolRecordPanel();
  }
});

async function syncReleaseWindowReading() {
  try {
    syncReleaseWindowSelect(await invoke("SessionReleaseWindow"));
  } catch (error) {
    syncReleaseWindowSelect(null);
  }
}

elements["release-window"]?.addEventListener("change", async event => {
  const select = event.target;
  const requested = normalizeReleaseWindow(select.value);
  if (requested === null) {
    syncReleaseWindowSelect(releaseWindowCommitted);
    return;
  }
  select.disabled = true;
  setReleaseWindowStatus("正在应用…");
  try {
    const applied = normalizeReleaseWindow(await invoke("SetSessionReleaseWindow", requested));
    syncReleaseWindowSelect(applied);
    setReleaseWindowStatus(`已生效：${releaseWindowLabel(applied) || requested}`);
  } catch (error) {
    syncReleaseWindowSelect(releaseWindowCommitted);
    showToast(error);
  } finally {
    select.disabled = false;
  }
});

renderReleaseWindowOptions();

// ── 角色设置（设置 → 角色与权限）──────────────────────────────────────
// 角色卡统一承载：角色默认权限 / 本会话覆盖 / 继承 / 账户 / 默认 plugin。
// 上下文窗口不在这条角色链上：它属于具体账户模型，由账户行内的甜点上下文控件编辑。
let permissionVectorCommitted = [];
let permissionVectorDraft = [];
let permissionVectorApplied = true;
let defaultPluginCommitted = "";

function setDefaultPluginStatus(text) {
  const node = elements["default-plugin-status"];
  if (node) node.textContent = text || "";
}

// renderRoleSettingsOptions 画默认 plugin 下拉。
function renderRoleSettingsOptions() {
  const pluginHost = elements["role-default-plugin"];
  if (pluginHost) {
    const plugins = lastPluginsRuntime?.plugins || [];
    pluginHost.innerHTML = rolePluginOptionsHTML(plugins, defaultPluginCommitted || lastPluginsRuntime?.plugin || "");
    pluginHost.disabled = !plugins.length;
  }
}

async function syncRoleSettings() {
  defaultPluginCommitted = "";
  renderRoleSettingsOptions();
  setDefaultPluginStatus("");
  try {
    permissionVectorCommitted = normalizePermissionVector(await invoke("PermissionVector"));
    permissionVectorApplied = true;
  } catch (error) {
    permissionVectorCommitted = [];
    permissionVectorApplied = false;
  }
  permissionVectorDraft = permissionVectorCommitted.slice();
  // 芯片与下拉读的是同一份回执：主代理向量在下方设置面板与底部芯片之间只有一个真值。
  renderPermissionVectorChip();
  renderPermissionMenu(client.current()?.runtime || {});
}

// submitPermissionVector 提交整组标签（不是单个）：一次写入 = 一次完整状态，
// 后端不用猜"这次是加还是减"。失败保留草稿并标未生效。
async function submitPermissionVector() {
  const wanted = normalizePermissionVector(permissionVectorDraft);
  try {
    const served = await invoke("SetPermissionVector", wanted);
    const applied = normalizePermissionVector(Array.isArray(served) ? served : wanted);
    permissionVectorCommitted = applied;
    permissionVectorDraft = applied.slice();
    permissionVectorApplied = true;
  } catch (error) {
    permissionVectorApplied = false;
  }
  renderPermissionVectorChip();
  renderPermissionMenu(client.current()?.runtime || {});
}

// ── 角色默认权限（设置 → 角色设置 → 角色默认权限）────────────────────────────
//
// 与上面那格「权限向量」的区别（这是两块，不是一块的两份）：
//   · 权限向量   = **这个会话**里主代理（root）显式登记的那一份（chip 也编辑它）；
//   · 角色默认   = 按 role（agent / subagent / goalplan / teammate）各一份，**跨会话**
//                  生效；会话里显式登记过的那一份永远压在这一层之上。
// 角色名录来自 accounts.yaml 的 roles.*：后端按账号名前缀（agent-1 → agent）归并下发，
// 界面不自己造别名，也不把"面板上的角色"与"配置里的角色"分成两份事实。
let permissionRoleRows = [];
const roleBoardDrafts = { default: new Map(), session: new Map() };
const roleBoardExpanded = { default: new Set(["agent"]), session: new Set(["agent"]) };
let pendingSSHUserSpace = null;

function replacePermissionRoleRow(applied) {
  if (!applied?.role) return;
  const index = permissionRoleRows.findIndex(row => row.role === applied.role);
  if (index >= 0) permissionRoleRows[index] = applied;
  else permissionRoleRows.push(applied);
  roleBoardDrafts.default.delete(applied.role);
  roleBoardDrafts.session.delete(applied.role);
}

function renderPermissionRoleRows() {
  const accounts = lastAccountsRuntime?.accounts || [];
  const globalHost = elements["permission-role-board"];
  if (globalHost) {
    globalHost.innerHTML = rolePermissionBoardHTML(permissionRoleRows, {
      mode: "default",
      accounts,
      balances: accountBalances,
      defaultDrafts: roleBoardDrafts.default,
      expanded: roleBoardExpanded.default,
      windowPresets: accountWindowPresets,
      windowReadings: accountWindowsCommitted,
    });
  }
  const sessionHost = elements["session-permission-board"];
  if (sessionHost) {
    sessionHost.innerHTML = rolePermissionBoardHTML(permissionRoleRows, {
      mode: "session",
      accounts,
      sessionDrafts: roleBoardDrafts.session,
      expanded: roleBoardExpanded.session,
      showAccounts: false,
    });
  }
}

async function syncPermissionRoles() {
  try {
    const rows = await invoke("PermissionRoles");
    permissionRoleRows = Array.isArray(rows) ? rows.filter(row => row && row.role) : [];
  } catch (error) {
    permissionRoleRows = [];
  }
  renderPermissionRoleRows();
}

function roleBoardTarget(event, attribute) {
  const node = event.target?.closest?.(`[${attribute}]`);
  return node ? { node, value: node.getAttribute(attribute) || "" } : null;
}

async function submitRolePermission(mode, role, vector) {
  const normalized = normalizePermissionVector(vector);
  const method = mode === "session" ? "SetRoleSessionPermission" : "SetRoleDefaultVector";
  try {
    const applied = await invoke(method, role, normalized);
    replacePermissionRoleRow(applied);
    renderPermissionRoleRows();
    const warnings = permissionVectorLint(normalized);
    if (warnings.length) showToast(warnings.map(item => item.message).join("；"));
  } catch (error) {
    roleBoardDrafts[mode].delete(role);
    showToast(error);
    renderPermissionRoleRows();
  }
}

function openSSHUserSpaceDialog(mode, role, toggle = true) {
  const row = permissionRoleRows.find(item => item.role === role);
  pendingSSHUserSpace = { mode, role, toggle };
  if (elements["ssh-userspace-paths"]) {
    const paths = mode === "session" ? row?.session_ssh_user_spaces : row?.default_ssh_user_spaces;
    elements["ssh-userspace-paths"].value = (paths || []).join("\n");
  }
  if (elements["ssh-userspace-status"]) {
    elements["ssh-userspace-status"].textContent = "路径会写入角色的 SSH 允许范围。";
  }
  setModal("ssh-userspace-modal", true);
}

function closeSSHUserSpaceDialog() {
  pendingSSHUserSpace = null;
  setModal("ssh-userspace-modal", false);
}

elements["permission-role-board"]?.addEventListener("click", event => { void handleRoleBoardClick(event); });
elements["session-permission-board"]?.addEventListener("click", event => { void handleRoleBoardClick(event); });

async function handleRoleBoardClick(event) {
  const toggle = roleBoardTarget(event, "data-role-toggle");
  if (toggle) {
    const mode = toggle.node.closest("#session-permission-board") ? "session" : "default";
    const set = roleBoardExpanded[mode];
    if (set.has(toggle.value)) set.delete(toggle.value);
    else set.add(toggle.value);
    renderPermissionRoleRows();
    return;
  }
  const add = roleBoardTarget(event, "data-role-perm-add");
  if (add) {
    const mode = add.node.dataset.board === "session" ? "session" : "default";
    const role = add.node.dataset.role;
    if (add.value === "ssh") {
      openSSHUserSpaceDialog(mode, role);
      return;
    }
    const row = permissionRoleRows.find(item => item.role === role);
    const current = roleBoardDrafts[mode].get(role) ||
      (mode === "session" ? row?.permission_vector : row?.default_permission_vector) || [];
    const next = cyclePermissionVector(current, add.value);
    roleBoardDrafts[mode].set(role, next);
    await submitRolePermission(mode, role, next);
    return;
  }
  const userSpaces = roleBoardTarget(event, "data-role-user-spaces");
  if (userSpaces) {
    const mode = userSpaces.node.dataset.board === "session" ? "session" : "default";
    openSSHUserSpaceDialog(mode, userSpaces.value, false);
    return;
  }
  const cycle = roleBoardTarget(event, "data-role-perm-cycle");
  if (cycle) {
    const mode = cycle.node.dataset.board === "session" ? "session" : "default";
    const role = cycle.node.dataset.role;
    const row = permissionRoleRows.find(item => item.role === role);
    const current = roleBoardDrafts[mode].get(role) || (mode === "session" ? row?.permission_vector : row?.default_permission_vector) || [];
    const next = cyclePermissionVector(current, cycle.value);
    roleBoardDrafts[mode].set(role, next);
    await submitRolePermission(mode, role, next);
    return;
  }
  const inherit = roleBoardTarget(event, "data-role-inherit");
  if (inherit) {
    const row = permissionRoleRows.find(item => item.role === inherit.value);
    try {
      replacePermissionRoleRow(await invoke("SetRoleInheritance", inherit.value, !row?.inherit_main_agent));
      renderPermissionRoleRows();
    } catch (error) { showToast(error); }
    return;
  }
  const pickAccount = roleBoardTarget(event, "data-role-account-pick");
  if (pickAccount) {
    try {
      replacePermissionRoleRow(await invoke("SetRoleAccount", pickAccount.node.dataset.role, pickAccount.value));
      renderPermissionRoleRows();
    } catch (error) { showToast(error); }
    return;
  }
  const balance = roleBoardTarget(event, "data-role-account-balance");
  if (balance) {
    await queryAccountBalance(balance.value);
    return;
  }
  const editAccount = roleBoardTarget(event, "data-role-account-edit");
  if (editAccount) {
    await openAccountEdit(editAccount.value);
    return;
  }
  const deleteAccount = roleBoardTarget(event, "data-role-account-delete");
  if (deleteAccount) {
    openAccountDelete(deleteAccount.value, deleteAccount.node.dataset.role || "");
    return;
  }
  const addAccount = roleBoardTarget(event, "data-role-add-account");
  if (addAccount) {
    await openAccountAdd(addAccount.value);
  }
}

elements["ssh-userspace-close"]?.addEventListener("click", closeSSHUserSpaceDialog);
elements["ssh-userspace-cancel"]?.addEventListener("click", closeSSHUserSpaceDialog);
elements["ssh-userspace-save"]?.addEventListener("click", async () => {
  if (!pendingSSHUserSpace) return;
  const { mode, role, toggle } = pendingSSHUserSpace;
  const paths = String(elements["ssh-userspace-paths"]?.value || "").split(/\r?\n/).map(item => item.trim()).filter(Boolean);
  try {
    const method = mode === "session" ? "SetRoleSessionSSHUserSpaces" : "SetRoleSSHUserSpaces";
    replacePermissionRoleRow(await invoke(method, role, paths));
    if (toggle) {
      const row = permissionRoleRows.find(item => item.role === role);
      const current = roleBoardDrafts[mode].get(role) ||
        (mode === "session" ? row?.permission_vector : row?.default_permission_vector) || [];
      const next = cyclePermissionVector(current, "ssh");
      roleBoardDrafts[mode].set(role, next);
      await submitRolePermission(mode, role, next);
    } else {
      renderPermissionRoleRows();
    }
    closeSSHUserSpaceDialog();
  } catch (error) {
    if (elements["ssh-userspace-status"]) elements["ssh-userspace-status"].textContent = `保存失败：${error?.message || error}`;
  }
});

// ── 工作环境（项目侧本地/SSH 环境表）────────────────────────────────
//
// 项目持有唯一事实源：环境名为空主机 = 本地执行，有主机 = SSH。会话首次固定环境后，
// 适配器把它无感注入工具调用；环境清单作为请求尾部动态块，不进入 system 前缀。
let sshEnvRows = [];       // 项目环境表读面（host 空 = local）
let sshEnvDefault = "";    // 项目默认环境名
let sshEnvAvailable = true;
let environmentEditorMode = "add";
let environmentEditorOriginalName = "";

function setSSHEnvStatus(text) {
  const status = elements["ssh-env-status"];
  if (status) status.textContent = text || "";
}

function setEnvironmentEditorStatus(text) {
  const status = elements["environment-editor-status"];
  if (status) status.textContent = text || "";
}

// syncWorkspaceEnvironments 只拉项目连接表：调用方是项目设置。它不读取也不改写
// 当前会话的放权状态，避免“项目连接”和“会话授权”两个作用域互相带状态。
async function syncWorkspaceEnvironments() {
  setSSHEnvStatus("");
  try {
    const [list, defaultEnv] = await invoke("WorkspaceEnvironments", currentWorkspaceID());
    sshEnvRows = Array.isArray(list) ? list : [];
    sshEnvDefault = String(defaultEnv || "");
    sshEnvAvailable = true;
  } catch (error) {
    sshEnvRows = [];
    sshEnvDefault = "";
    sshEnvAvailable = false;
  }
  renderSSHEnvRows();
}

function renderSSHEnvRows() {
  const host = elements["ssh-env-list"];
  if (!host) return;
  if (!sshEnvAvailable) {
    host.innerHTML = '<span class="muted list-empty">读不到项目环境表（请先选择项目；环境属于项目，不依赖会话绑定）</span>';
    return;
  }
  if (!sshEnvRows.length) {
    host.innerHTML = '<span class="muted list-empty">这个项目还没有环境：点「添加环境」创建一个（例如 dev）。</span>';
    return;
  }
  host.innerHTML = sshEnvRows.map(row => {
    const address = [row.host, row.port ? `:${row.port}` : ""].filter(Boolean).join("");
    const mode = row.host ? "ssh" : "local";
    const flags = [mode, row.default ? "默认" : "", row.password_set ? "已设口令" : ""].filter(Boolean).join(" · ");
    const account = row.host ? (row.user ? `${address} @${row.user}` : address) : "本地项目根";
    return `<button type="button" class="ssh-env-row" data-ssh-env="${escapeHtml(row.name)}" title="点击调整 ${escapeHtml(row.name)}">
      <strong>${escapeHtml(row.name)}</strong>
      <span class="muted">${escapeHtml(account)}</span>
      ${row.remote_root ? `<span class="muted">${escapeHtml(row.remote_root)}</span>` : ""}
      ${flags ? `<em>${escapeHtml(flags)}</em>` : ""}
    </button>`;
  }).join("");
}

function openEnvironmentEditor(name = "") {
  if (!client.current()?.current_workspace?.id) {
    showToast("先选择一个项目，再管理环境");
    return;
  }
  const found = name ? sshEnvRows.find(item => item.name === name) : null;
  if (name && !found) {
    showToast(`环境 ${name} 已不存在，请刷新后再试`);
    return;
  }
  const editing = Boolean(found);
  environmentEditorMode = editing ? "edit" : "add";
  environmentEditorOriginalName = found?.name || "";
  if (elements["environment-editor-title"]) {
    elements["environment-editor-title"].textContent = editing ? `调整环境 · ${found.name}` : "添加环境";
  }
  if (elements["environment-editor-note"]) {
    elements["environment-editor-note"].textContent = editing
      ? "环境名是项目内的稳定主键；这里调整连接信息与默认环境。口令留空表示保持原值。"
      : "创建后会在环境列表里出现；主机留空即本地环境。口令留空表示不设置口令。";
  }
  const nameInput = elements["ssh-env-name"];
  if (nameInput) {
    nameInput.value = found?.name || "";
    nameInput.disabled = editing;
  }
  if (elements["ssh-env-host"]) elements["ssh-env-host"].value = found?.host || "";
  if (elements["ssh-env-port"]) elements["ssh-env-port"].value = found?.port ? String(found.port) : "";
  if (elements["ssh-env-user"]) elements["ssh-env-user"].value = found?.user || "";
  if (elements["ssh-env-remote-root"]) elements["ssh-env-remote-root"].value = found?.remote_root || "";
  if (elements["ssh-env-password"]) elements["ssh-env-password"].value = "";
  if (elements["ssh-env-default"]) elements["ssh-env-default"].checked = Boolean(found?.default);
  if (elements["ssh-env-delete"]) elements["ssh-env-delete"].hidden = !editing;
  setEnvironmentEditorStatus("");
  setModal("environment-editor-modal", true);
}

function closeEnvironmentEditor() {
  setModal("environment-editor-modal", false);
  environmentEditorMode = "add";
  environmentEditorOriginalName = "";
  setEnvironmentEditorStatus("");
  if (elements["ssh-env-password"]) elements["ssh-env-password"].value = "";
  if (elements["ssh-env-name"]) elements["ssh-env-name"].disabled = false;
}

async function refreshEnvironmentViews() {
  await syncWorkspaceEnvironments();
  await syncSessionEnvironment();
}

// submitSSHEnvironment 保存一个项目环境（口令留空 = 不改，是后端的三态语义）。
async function submitSSHEnvironment() {
  const name = String(elements["ssh-env-name"]?.value || "").trim();
  if (!name) {
    setEnvironmentEditorStatus("先填环境名（dev / test / prod 这类，字母数字 - _ .）。");
    return;
  }
  const portText = String(elements["ssh-env-port"]?.value || "").trim();
  const passwordText = String(elements["ssh-env-password"]?.value || "");
  const change = {
    Host: String(elements["ssh-env-host"]?.value || "").trim(),
    Port: portText ? Number(portText) : 0,
    User: String(elements["ssh-env-user"]?.value || "").trim(),
    RemoteRoot: String(elements["ssh-env-remote-root"]?.value || "").trim(),
    // 口令三态：null = 不改（留空就是"我没动它"），空串才表示清除。
    Password: passwordText === "" ? null : passwordText,
    MakeDefault: Boolean(elements["ssh-env-default"]?.checked),
    Delete: false,
  };
  try {
    const applied = await invoke("SetWorkspaceEnvironment", currentWorkspaceID(), name, change);
    await refreshEnvironmentViews();
    setSSHEnvStatus(applied && applied.host
      ? `${name}：已保存 SSH 环境（${applied.host}${applied.port ? ":" + applied.port : ""}）`
      : `${name}：已保存本地环境`);
    closeEnvironmentEditor();
  } catch (error) {
    setEnvironmentEditorStatus(`保存 ${name} 失败：${error}`);
    showToast(error);
  }
}

elements["ssh-env-list"]?.addEventListener("click", event => {
  const row = event.target?.closest?.("[data-ssh-env]");
  if (!row) return;
  const name = String(row.dataset.sshEnv || "");
  openEnvironmentEditor(name);
});

elements["ssh-env-add"]?.addEventListener("click", () => openEnvironmentEditor());
elements["ssh-env-save"]?.addEventListener("click", submitSSHEnvironment);
elements["environment-editor-close"]?.addEventListener("click", closeEnvironmentEditor);
elements["environment-editor-cancel"]?.addEventListener("click", closeEnvironmentEditor);

elements["ssh-env-delete"]?.addEventListener("click", async () => {
  const name = environmentEditorOriginalName;
  if (environmentEditorMode !== "edit" || !name) {
    setEnvironmentEditorStatus("只有已有环境可以删除。");
    return;
  }
  try {
    await invoke("DeleteWorkspaceEnvironment", currentWorkspaceID(), name);
    await refreshEnvironmentViews();
    setSSHEnvStatus(`${name}：已删除`);
    closeEnvironmentEditor();
  } catch (error) {
    setEnvironmentEditorStatus(`删除 ${name} 失败：${error}`);
    showToast(error);
  }
});

elements["role-default-plugin"]?.addEventListener("change", async event => {
  const name = String(event.target.value || "").trim();
  if (!name) return;
  try {
    await invoke("SetDefaultPlugin", name);
    defaultPluginCommitted = name;
    setDefaultPluginStatus(`默认 plugin 已设为 ${name}`);
  } catch (error) {
    defaultPluginCommitted = "";
    renderRoleSettingsOptions();
    setDefaultPluginStatus(`未生效：桥面还没有 SetDefaultPlugin（待后端补方法），当前仍是 ${lastPluginsRuntime?.plugin || "未定"}。`);
  }
});

// ── 每账号上下文窗口（设置 → 角色设置 → 上下文窗口）─────────────────────
// 权威在后端三跳：账号级覆盖（这里设的，落数据根 runtime-limits.json）>
// config/seelex.yaml 的 limits.context_window > 账号档自报值——判定归
// seelebridge 的 ContextWindowFor 一处。所以这里：
//   · 账号列表与**当前生效值**都来自快照的 runtime.accounts（后端已把生效窗口随账号
//     下发，前端不自己算优先级）；
//   · 挡位表来自 invoke("AccountWindowPresets")（源头 seelexctx/window.go）；
//   · 提交走 invoke("SetAccountWindow", name, value) 并**采纳后端返回的生效值**。
//
// accountWindowsCommitted 是"最后一份权威读数"（账号名 → 生效窗口）：提交失败时把
// 控件回滚到它（后端写盘失败时整步不改内存，所以回滚是诚实的）。
let accountWindowsCommitted = new Map();
let accountWindowPresets = [];

function setAccountWindowsStatus(text) {
  const status = elements["role-settings-status"];
  if (status) status.textContent = text || "";
}

// syncAccountWindowReading 打开弹窗时拉一次挡位表并重画（账号读数来自最近一份快照）。
async function syncAccountWindowReading() {
  try {
    const presets = await invoke("AccountWindowPresets");
    accountWindowPresets = Array.isArray(presets) ? presets.map(preset => Number(preset)) : [];
  } catch (error) {
    accountWindowPresets = [];
    setAccountWindowsStatus("读不到挡位表（桥未就绪）；仍可用「自定义」填 token 数。");
  }
  renderPermissionRoleRows();
}

// submitAccountWindow 提交一个账号的窗口，然后**读回来一嘴**（用户口径的 send/ack）：
//   1. 写：invoke("SetAccountWindow") 返回的只是写入那一刻的生效值；
//   2. 拉：再走一次 refresh，拿一份新快照（账号列表里的 context_window 是后端算好的
//      生效值：账号覆盖 > config limits.context_window > 账号档自报）；
//   3. 核对：以**读回来的值**为准重画；写入值与读回值不一致就把差异说清楚
//      （多半是被 limits.context_window 夹了，或者窗口 <= 0 = 取消覆盖回到账号档默认）。
// 不拿请求值当结果——这正是"改完之后显示还是旧的"的根因。
async function submitAccountWindow(name, window) {
  try {
    const applied = Number(await invoke("SetAccountWindow", name, window));
    await refresh({ scroll: false });
    const confirmed = accountWindowFromSnapshot(name);
    accountWindowsCommitted.set(name, confirmed !== null ? confirmed : (applied > 0 ? applied : null));
    renderPermissionRoleRows();
    const shown = accountWindowsCommitted.get(name);
    if (shown === null || shown === undefined) {
      setAccountWindowsStatus(`${name}：已改，读回的是账号档默认（这一层没有覆盖）。`);
      return;
    }
    const drift = applied > 0 && applied !== shown ? `（写入 ${accountWindowLabel(applied)}，读回 ${accountWindowLabel(shown)}：被上限夹过或按默认值落地）` : "";
    setAccountWindowsStatus(`${name}：读回生效窗口 ${accountWindowLabel(shown)}${drift}`);
  } catch (error) {
    renderPermissionRoleRows(); // 回滚到最后一份权威读数（后端失败时整步不改）
    setAccountWindowsStatus(`改 ${name} 的窗口失败：${error}`);
    showToast(error);
  }
}

// accountWindowFromSnapshot 从最近一份快照里读某个账号的生效窗口（读不到 → null）。
function accountWindowFromSnapshot(name) {
  const accounts = lastAccountsRuntime?.accounts || [];
  const found = accounts.find(account => account && account.name === name);
  const value = Number(found?.context_window);
  return value > 0 ? value : null;
}

async function handleRoleBoardChange(event) {
  const select = event.target?.closest?.("[data-role-account-window-select]");
  if (select) {
    const row = select.closest(".role-account-window");
    const name = select.getAttribute("data-role-account-window-select") || "";
    const input = row?.querySelector?.("[data-role-account-window-input]");
    const custom = select.value === ACCOUNT_WINDOW_CUSTOM;
    if (input) input.hidden = !custom;
    if (custom) {
      input?.focus();
      return; // 等自定义输入框确认后再提交
    }
    await submitAccountWindow(name, normalizeAccountWindowInput(select.value));
    return;
  }
  const input = event.target?.closest?.("[data-role-account-window-input]");
  if (input) {
    const name = input.getAttribute("data-role-account-window-input") || "";
    const window = normalizeAccountWindowInput(input.value);
    if (window === null) {
      setAccountWindowsStatus("自定义窗口要填正整数（token 数，例如 500000）。");
      renderPermissionRoleRows();
      return;
    }
    await submitAccountWindow(name, window);
  }
}

for (const id of ["permission-role-board", "session-permission-board"]) {
  elements[id]?.addEventListener("change", event => { void handleRoleBoardChange(event); });
}

document.addEventListener("keydown", event => {
  if (!(event.ctrlKey || event.metaKey) || event.altKey) return;
  if (event.key !== "`" && event.key !== "~") return;
  event.preventDefault();
  if (event.shiftKey) terminalPanel.newTerminal();
  else terminalPanel.toggle();
});

// ── 角色卡下账户：增删改查 与 逐账号查余额 ────────────────────────────────
// 两条链路的判据都在后端，这里只做三件事：把表单整理成草稿、把草稿递过去、把回执
// 与读数显示出来。
//   · 添加：invoke("AddAccount", draft) 回的是**生效的账号名**（`agent-2` 由账号档
//     里的位置推导）——后续选中/改窗口/查余额都按这个名字打，界面不自己猜序号；
//   · 余额：invoke("AccountBalance", name) **点一次查一次**（余额是查询当下的快照，
//     不进快照缓存）。读数按账号名存在 accountBalances 里，快照重画时回读，所以
//     "画一遍就把刚查到的读数冲掉"不会发生。
let accountBalances = new Map(); // 账号名 → { balance } | { error }

function setAccountAddStatus(text) {
  const status = elements["account-add-status"];
  if (status) status.textContent = text || "";
}

// syncAccountAdminForm 打开弹窗时补两件事：角色词表（宿主下发）与余额行。
async function syncAccountAdminForm() {
  await syncAccountRoles();
}

// syncAccountRoles 拉角色词表并填下拉；顺带把思考强度下拉填好（词表来自
// composer-knobs 的 provider 词表 + 账号特有的 session）。
async function syncAccountRoles() {
  renderAccountEffortOptions();
  const host = elements["account-add-role"];
  if (!host) return;
  let roles = [];
  try {
    const result = await invoke("AccountRoles");
    roles = Array.isArray(result) ? result : [];
  } catch {
    roles = [];
  }
  if (!roles.length) {
    // 读不到就说读不到：不拿一份写死的词表顶上（那会在后端加角色时悄悄失配）。
    host.innerHTML = '<option value="">读不到角色词表</option>';
    host.disabled = true;
    return;
  }
  const previous = host.value;
  host.disabled = false;
  host.innerHTML = roles.map(role => `<option value="${escapeHtml(role)}">${escapeHtml(role)}</option>`).join("");
  if (roles.includes(previous)) host.value = previous;
}

// renderAccountEffortOptions 填思考强度下拉：空 = 继承 defaults（账号档里通常就是它），
// session = 跟随会话档位，其余是 provider 词表。
function renderAccountEffortOptions() {
  const select = elements["account-add-effort"];
  if (!select) return;
  const previous = select.value;
  const options = ['<option value="">继承 defaults</option>', '<option value="session">session（跟随会话档位）</option>'];
  for (const level of REASONING_EFFORT_LEVELS) {
    options.push(`<option value="${escapeHtml(level)}">${escapeHtml(REASONING_EFFORT_LABELS[level] || level)}</option>`);
  }
  select.innerHTML = options.join("");
  select.value = options.some(option => option.includes(`value="${previous}"`)) ? previous : "";
}

// queryAccountBalance 查一个账号的余额：成功与失败都落到这一行的读数上（失败不当成
// "余额为 0"——那是两回事）。
async function queryAccountBalance(name) {
  if (!name) return;
  try {
    const balance = await invoke("AccountBalance", name);
    accountBalances.set(name, { balance });
  } catch (error) {
    accountBalances.set(name, { error: error?.message || String(error) });
  }
  renderPermissionRoleRows();
}

// submitAccountAdd 提交新账号。
//
// 成功后必须**拉一份新快照**：热注册让账号池立刻多了一条，快照是界面看见它的唯一
// 通道（顺带把「角色设置 → 账号」下拉一起刷新）。失败按后端给的原话显示——其中
// "已写入账号档，重启后生效"这一句要原样透出来，否则用户会以为没加上而重复添加。
async function submitAccountAdd() {
  if (editingAccountName) {
    const model = String(elements["account-add-model"]?.value || "").trim();
    if (!model) {
      setAccountAddStatus("模型是必填项。");
      return;
    }
    setAccountAddStatus("正在更新账号档…");
    try {
      const result = await invoke("UpdateAccount", {
        name: editingAccountName,
        provider: String(elements["account-add-provider"]?.value || "").trim(),
        model,
        base_url: String(elements["account-add-base-url"]?.value || "").trim(),
        api_key: String(elements["account-add-api-key"]?.value || ""),
        reasoning_effort: String(elements["account-add-effort"]?.value || "").trim(),
      });
      if (elements["account-add-api-key"]) elements["account-add-api-key"].value = "";
      await refresh({ scroll: false });
      await syncPermissionRoles();
      setAccountAddStatus(`已更新 ${result?.name || editingAccountName}。`);
      showToast(`账户 ${result?.name || editingAccountName} 已更新`);
    } catch (error) {
      setAccountAddStatus(`更新失败：${error?.message || error}`);
      showToast(error);
    }
    return;
  }
  const parsed = accountDraftFromForm({
    role: elements["account-add-role"]?.value,
    model: elements["account-add-model"]?.value,
    apiKey: elements["account-add-api-key"]?.value,
    provider: elements["account-add-provider"]?.value,
    baseUrl: elements["account-add-base-url"]?.value,
    reasoningEffort: elements["account-add-effort"]?.value,
  });
  if (parsed.error) {
    setAccountAddStatus(parsed.error);
    return;
  }
  setAccountAddStatus("正在写入账号档…");
  try {
    const result = await invoke("AddAccount", parsed.draft);
    // 密钥不留在输入框里；模型也是"这次要加的那条"，清掉免得重复提交同一条。
    if (elements["account-add-api-key"]) elements["account-add-api-key"].value = "";
    if (elements["account-add-model"]) elements["account-add-model"].value = "";
    await refresh({ scroll: false });
    await syncPermissionRoles();
    const summary = [result?.provider, result?.model].filter(Boolean).join(" / ");
    setAccountAddStatus(`已添加 ${result?.name || ""}${summary ? `（${summary}）` : ""}，已生效。`);
    showToast(`账户 ${result?.name || ""} 已添加并生效`);
  } catch (error) {
    setAccountAddStatus(`添加失败：${error?.message || error}`);
    showToast(error);
  }
}

elements["account-add-submit"]?.addEventListener("click", () => { void submitAccountAdd(); });
elements["account-delete-close"]?.addEventListener("click", closeAccountDelete);
elements["account-delete-cancel"]?.addEventListener("click", closeAccountDelete);
elements["account-delete-confirm"]?.addEventListener("click", async () => {
  if (!deletingAccountName) return;
  const name = deletingAccountName;
  if (elements["account-delete-confirm"]) elements["account-delete-confirm"].disabled = true;
  if (elements["account-delete-status"]) elements["account-delete-status"].textContent = `正在删除 ${name}…`;
  try {
    await invoke("DeleteAccount", name);
    accountBalances.delete(name);
    accountWindowsCommitted.delete(name);
    await refresh({ scroll: false });
    await syncPermissionRoles();
    closeAccountDelete();
    setAccountWindowsStatus(`${name}：已删除。`);
    showToast(`账户 ${name} 已删除`);
  } catch (error) {
    if (elements["account-delete-status"]) elements["account-delete-status"].textContent = `删除失败：${error?.message || error}`;
    showToast(error);
  } finally {
    if (elements["account-delete-confirm"]) elements["account-delete-confirm"].disabled = false;
  }
});
function resizePrompt() {
  elements.prompt.style.height = "auto";
  elements.prompt.style.height = `${Math.min(elements.prompt.scrollHeight, 180)}px`;
  syncComposerMetrics();
}

// syncComposerMetrics 把输入框的实测高度交给 CSS（--composer-h）：输入框与贴它
// 上沿的消息队列叠都是绝对定位的浮层，对话区底部留白与队列叠的 `bottom` 都要知道
// 它到底多高——多行草稿会把输入框撑高，写死一个数就会在长草稿里露缝或被压住。
function syncComposerMetrics() {
  if (!elements.composer) return;
  document.documentElement.style.setProperty("--composer-h", `${elements.composer.offsetHeight}px`);
}

async function initialise() {
  try {
    hydrateIcons();
    applyDockState();
    // 输入框 / 队列叠 / 对话区留白三者的高度关系要先立起来（--composer-h），
    // 否则首帧的底部留白按 CSS 缺省值算，队列一出现就会压住对话尾部。
    syncComposerMetrics();
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
    // 底部权限芯片读的是主代理权限向量（后端回执），首帧必须先拉一次——
    // 否则在用户打开设置面板之前，芯片会一直显示"未选"这个假读数。
    await syncRoleSettings();
    // 面板上次是展开的就恢复展开（VS Code 同口径）：恢复时按需要新建一个终端。
    if (terminalPanel.state().open) terminalPanel.open();
  } catch (error) {
    showToast(error);
    window.setTimeout(initialise, 600);
  }
}
initialise();
