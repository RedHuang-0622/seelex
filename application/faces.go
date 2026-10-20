package application

// faces.go 是门面的**五组窄接口**（plan §1.3 的 ③ 用例方法面收窄；M5-R10a）。
//
// 为什么要有这一层：前端只该依赖“它能用的那几件事”，而不是整个 `Service`。四个接口的
// 方法名与签名**逐字**取自今日门面已导出的方法面（`go doc -all ./application/core` 的
// `func (service *Service) …`），不新增、不删除、不改名、不改签名——它们就是 gui/tui 今天
// 消费的那份契约（I5：前端零改动）。
//
// 三类内容：
//
//  1. A 组（类型与事件身份）：**不是接口**，是 `application/application.go` 里的类型别名集合，
//     由 `FaceTypeNames` 逐字钉住（别名表达不了方法集，所以用清单）；
//  2. B/C/D/E 四组行为面：下面四个接口；
//  3. 不在四组的第三类：装配注入口（`Set*` / `Apply*`，由 `wiring` 持有）、工具注册面
//     （`*Handler`）、事件钩子（`Handle*`）、内部发布/落盘编排（`Publish*` / `Refresh*` / `Flush*` /
//     `Record*` / `Prepare*`）与调度装配面（含 `StartScheduledSession` / `AssembleScheduledRun`）——
//     清单见 `e2e/facade_narrow_faces_test.go`。
//
// 依赖边界：接口签名逐字复述今日方法面，因此这里按需 import 承载类型的包
// （`contract/dto`、`middle/protocol/model`、`store` 等）——**只借类型身份，不新建类型**（I4），
// 也不引入新的行为实现。此文件由 `tmp/reshape/r10a_gen_faces.py` 生成；手改即改契约。

import (
	"context"

	"github.com/RedHuang-0622/seelex/application/core"
	"github.com/RedHuang-0622/seelex/application/prompt"
	assemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	seelexctxsearch "github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/search"
	goaldomain "github.com/RedHuang-0622/seelex/backend/multiagent/goal"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/event"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/store"
)

// FaceTypeNames 是门面 A 组（类型与事件身份）的**逐字清单**：
// `application/application.go` 里指向 `contract` / `middle/protocol` 的类型别名，按字母序。
// 新增/删除别名必须显式改这份清单（`e2e/facade_narrow_faces_test.go`解析事实源源码比对）。
var FaceTypeNames = []string{
	"AccountInfo",
	"ActivePlanProjection",
	"ActiveSkill",
	"ApprovalBroker",
	"ApprovalDecision",
	"ApprovalRequest",
	"Capabilities",
	"ChatEngine",
	"ChatState",
	"Command",
	"CommandResult",
	"CompactionProgress",
	"ContextCompaction",
	"ConversationRecord",
	"Dependencies",
	"EffortManager",
	"EngineMessage",
	"EngineToolCall",
	"Event",
	"EventHub",
	"EventKind",
	"EventRange",
	"Interaction",
	"InteractionOption",
	"Message",
	"MessageDelta",
	"NodeStatus",
	"PerfStats",
	"PlanNode",
	"PlanState",
	"PlanStatus",
	"PluginInfo",
	"PluginPort",
	"ProcessRuntime",
	"ProcessSnapshot",
	"PromptLayer",
	"PromptStack",
	"ReadFileRef",
	"ReplanMonitor",
	"ReplayResult",
	"RuntimePort",
	"RuntimeState",
	"SearchResult",
	"Service",
	"SessionArchive",
	"SessionExecutionRecord",
	"SessionInfo",
	"SessionMeta",
	"SessionPlanFrame",
	"SessionPort",
	"SessionRecord",
	"SessionRuntime",
	"SessionSnapshot",
	"SessionState",
	"SessionStatus",
	"SessionStorageConfig",
	"SessionTitle",
	"SkillInfo",
	"SkillPort",
	"Snapshot",
	"StoredToolResult",
	"SubagentDetail",
	"SubagentEvent",
	"SubagentToolEvent",
	"Subscription",
	"Suggestion",
	"TaskCheckpoint",
	"TaskContextProjection",
	"TaskState",
	"TokenAudit",
	"Tool",
	"ToolCall",
	"ToolHookBridge",
	"ToolHookDiagnosticEvent",
	"ToolHookDiagnosticObserver",
	"ToolRecordBoard",
	"ToolRecordReason",
	"ToolRecordSnapshot",
	"ToolRecordStat",
	"ToolResultPage",
	"ToolResultRef",
	"TranscriptEvent",
	"TranscriptToolCall",
	"WebSearchConfig",
	"WorkspaceInfo",
	"WorkspacePort",
}

// ViewFace 读投影（plan §1.3 B 组）：只读查询、订阅与历史窗口读回；不改内存/磁盘状态。
type ViewFace interface {
	ActiveSkillIDs() []string
	AnyChatRunning() bool
	AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error)
	GetSessionTranscript(sessionID string, fromSeq, toSeq uint64) ([]model.TranscriptEvent, error)
	GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView
	GoalSkillActive() bool
	GoalStatusFor(sessionID string) (goaldomain.StatusView, error)
	ListRoleSessions(mainSessionID string) ([]string, error)
	ListSessions() []model.SessionInfo
	ListSubagentRecovery(sessionID string) ([]dto.SubagentRecoveryView, error)
	LoadLatestHistory() error
	LoadMoreHistory(limit int) error
	PerfStats() model.PerfStats
	ProjectBoardTeamPlanFor(sessionID string) *store.TeamworkPlan
	ProjectBoardTeamStateFor(sessionID string) *store.TeamworkState
	ProjectRootFor(sessionID string) string
	PromptLayers() []prompt.PromptLayer
	ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error)
	ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error)
	RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error)
	SearchHistory(ctx context.Context, query string, limit int) (seelexctxsearch.Result, error)
	SessionInputIndex(sessionID string) (model.SessionInputIndex, error)
	SessionMeta(sessionID string) (model.SessionMeta, error)
	SessionReleaseWindow() int
	SessionStorageConfig() (store.Config, error)
	Snapshot() model.Snapshot
	SnapshotOf(sessionID string) (model.SessionSnapshot, error)
	Subscribe(buffer int) event.Subscription
	SubscribeSession(sessionID string, buffer int) (event.Subscription, error)
	SubscribeSessionWithReplay(sessionID string, buffer, replayWindow int) (event.Subscription, error)
	SubagentSessionDetail(nodeID string) (*model.SubagentDetail, error)
	TeammateSessionLiveFor(sessionID string) dto.TeammateSessionLiveView
	TeammateSessionLivePageFor(sessionID string, offset, limit int) dto.TeammateSessionLiveView
	TeamworkBoardViewFor(sessionID string) *dto.TeamworkBoardView
	ToolRecordBoard() dto.ToolRecordBoard
	ToolRecordEnabled() bool
	ToolResultContent(_ context.Context, resultRef string, offset, limit int) (model.ToolResultPage, error)
	WaitCatalogRefresh(ctx context.Context) error
	WaitForIdle(ctx context.Context) error
	WorkTableRowsFor(sessionID string) []model.WorkItem
	WorkspaceChanges(repo string, limit int) (dto.WorkspaceChangesResult, error)
	WorkspaceFileContent(relPath string, limit int64) (dto.FileContent, error)
	WorkspaceFileCount() (dto.TreeCount, error)
	WorkspaceGitCommitDetail(repo, hash string, limit int) (dto.GitCommitDetail, error)
	WorkspaceGitCommitFileContent(repo, hash, relPath string, limit int64) (dto.FileContent, error)
	WorkspaceGitLog(repo string, limit int) (dto.GitLogResult, error)
	WorkspaceGitRepos() (dto.GitReposResult, error)
	WorkspaceTree(relPath string, depth int) (dto.TreeListing, error)
}

// CommandFace 命令面（plan §1.3 C 组）：提交、取消、会话与工作区生命周期，以及子代理/团队动作。
type CommandFace interface {
	ActivateSession(sessionID string) error
	AddNotice(notice string)
	AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error
	AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error
	ArchiveSession(sessionID string) error
	BeginGracefulShutdown()
	BeginNewSession() error
	BindWorkspace(workspaceID string) error
	CancelAllChats()
	CancelChat(requestID string) bool
	ClearSubagentTree() error
	CompactContextNow(ctx context.Context) (core.ContextCompactionResult, error)
	CreateWorkspace(name, rootPath, gitRemote string) error
	CreateWorkspaces(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error)
	DeleteSession(sessionID string) error
	ForkSessionLatest(parentID string) (string, error)
	ForkSubagents(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error)
	GoalBegin(ctx context.Context, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)
	GoalBeginFor(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error)
	GoalDone(ctx context.Context, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error)
	GoalDoneFor(ctx context.Context, sessionID string, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error)
	GoalUpdate(ctx context.Context, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)
	GoalUpdateFor(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error)
	RequestFileReview(ctx context.Context, files []dto.ReviewFileRef) (dto.ReviewRequest, error)
	ResolveInteraction(ctx context.Context, id, optionID string) error
	ResumeInterruptedSubagents(ctx context.Context, sessionID string) (dto.SubagentResumeReport, error)
	ResumeSession(sessionID string) error
	ResumeSubagent(ctx context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error)
	Shutdown()
	Submit(ctx context.Context, text string) error
	SubmitToSession(ctx context.Context, sessionID, text string) error
	SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error)
	UnbindWorkspace()
	UnloadSession(sessionID string) error
	UpdateWorkItemStatus(id, status string) error
	WorkspaceWriteFile(relPath, content string) (dto.FileWriteResult, error)
	ForkSession(parentID string, request model.ForkRequest) (string, error)
}

// PolicyFace 档位与策略（plan §1.3 D 组）：限制、档位、账号、存储配置、插件、角色与调度策略。
type PolicyFace interface {
	ActivateSkill(name string) (model.SkillInfo, error)
	CancelScheduledTask(id string) error
	ConfigureSessionStorage(ctx context.Context, config store.Config) error
	CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error)
	ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error
	ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error
	ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error
	ScheduleTask(ctx context.Context, spec assemble.ScheduledTaskSpec) (*assemble.ScheduledTaskStatus, error)
	SelectAccount(_ context.Context, name string) error
	SetAccountWindow(_ context.Context, name string, window int) (int, error)
	SetFullAccess(on bool) bool
	SetLifecycleOrder(sessionID, policy string, roles []string) error
	SetLoopLimit(_ context.Context, limit int) error
	SetPermissionTier(tier string) (string, error)
	SetReasoningEffort(_ context.Context, effort string) error
	SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error
	SetSessionMeta(sessionID string, meta model.SessionMeta) error
	SetSessionReleaseWindow(_ context.Context, window int) (int, error)
	SwitchEffort(_ context.Context, level string) error
	SwitchPlugin(ctx context.Context, name string) error
	TestSessionStorage(ctx context.Context, config store.Config) error
	UpdateScheduledTask(ctx context.Context, id string, spec assemble.ScheduledTaskSpec) (*assemble.ScheduledTaskStatus, error)
}

// QueueFace 队列面（plan §1.3 E 组）：排队输入的召回/调序与输入建议；同组的无状态函数（`HasSigilPrefix` / `SigilOf` / `RenderDiag` / `ValidEffortLevels` / `AccountWindowPresets`）不是接口成员，仍以包级函数提供。
type QueueFace interface {
	RecallQueuedInput(sessionID string, index int) (string, error)
	ReorderQueuedInput(sessionID string, from, to int) error
	Suggestions(input string) []core.Suggestion
}

// 编译期钉子（契约 §1.2 第 1 条）：`Service` 在结构上满足四组行为面。
var (
	_ ViewFace    = (*Service)(nil)
	_ CommandFace = (*Service)(nil)
	_ PolicyFace  = (*Service)(nil)
	_ QueueFace   = (*Service)(nil)
)
