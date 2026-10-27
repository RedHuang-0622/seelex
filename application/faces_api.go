package application

// faces_api.go 是门面**五组窄接口**与 AP-002 能力面的事实源
// （plan §1.3 的 ③ 用例方法面收窄；M5-R10a）。EP-046C 后接口定义不再寄居
// `application/assembly`，根 Application 直接拥有入站契约。
//
// 为什么要有这一层：前端只该依赖“它能用的那几件事”，而不是整个 `Service`。四个接口的
// 方法名与签名**逐字**取自今日门面已导出的方法面（`go doc -all ./application/assembly` 的
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

	conversationcontext "github.com/RedHuang-0622/seelex/application/conversation/context"
	"github.com/RedHuang-0622/seelex/application/event"
	applicationinput "github.com/RedHuang-0622/seelex/application/input"
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// ContextCompactionResult is the stable public compaction receipt.
type ContextCompactionResult = conversationcontext.Result

// FaceTypeNames 是门面 A 组（类型与事件身份）的**逐字清单**：
// `application/application.go` 里指向 `contract` / `middle/protocol` 的类型别名，按字母序。
// 新增/删除别名必须显式改这份清单（`e2e/facade_narrow_faces_test.go`解析事实源源码比对）。
var FaceTypeNames = []string{
	"AccountAddResult",
	"AccountBalance",
	"AccountBalanceInfo",
	"AccountDetails",
	"AccountDraft",
	"AccountInfo",
	"AccountUpdate",
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
	"EventRange", "Frontend",
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
	AccountBalance(ctx context.Context, name string) (dto.AccountBalance, error)
	AnyChatRunning() bool
	AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error)
	ConversationView(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationViewRequest) (dto.SessionRecordPage, error)
	GetSessionTranscript(sessionID string, fromSeq, toSeq uint64) ([]model.TranscriptEvent, error)
	GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView
	GoalSkillActive() bool
	GoalStatusFor(sessionID string) (dto.GoalStatusView, error)
	ListRoleSessions(mainSessionID string) ([]string, error)
	ListSessions() []model.SessionInfo
	ListSubagentRecovery(sessionID string) ([]dto.SubagentRecoveryView, error)
	LoadLatestHistory() error
	LoadMoreHistory(limit int) error
	PerfStats() model.PerfStats
	ProjectRootFor(sessionID string) string
	PromptLayers() []prompt.PromptLayer
	ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error)
	ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error)
	RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error)
	SearchHistory(ctx context.Context, query string, limit int) (dto.SearchResult, error)
	SessionInputIndex(sessionID string) (model.SessionInputIndex, error)
	SessionMeta(sessionID string) (model.SessionMeta, error)
	SessionReleaseWindow() int
	Snapshot() model.Snapshot
	SnapshotOf(sessionID string) (model.SessionSnapshot, error)
	Subscribe(buffer int) event.Subscription
	SubscribeSession(sessionID string, buffer int) (event.Subscription, error)
	SubscribeSessionWithReplay(sessionID string, buffer, replayWindow int) (event.Subscription, error)
	SubagentSessionDetail(nodeID string) (*model.SubagentDetail, error)
	TeammateInspectionFor(ctx context.Context, mainSessionID, role string) (dto.TeammateInspectionView, error)
	TeammateSessionLiveFor(sessionID string) dto.TeammateSessionLiveView
	TeammateSessionLivePageFor(sessionID string, offset, limit int) dto.TeammateSessionLiveView
	ToolRecordBoard() dto.ToolRecordBoard
	ToolRecordEnabled() bool
	ToolResultContent(_ context.Context, resultRef string, offset, limit int) (model.ToolResultPage, error)
	WorkItemExecutionFor(ctx context.Context, mainSessionID, itemID string, page dto.ConversationViewRequest) (dto.WorkItemExecutionView, error)
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
	CompactContextNow(ctx context.Context) (ContextCompactionResult, error)
	ConversationChat(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationChatRequest) (dto.ChatReceipt, error)
	CreateWorkspace(name, rootPath, gitRemote string) error
	CreateWorkspaces(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error)
	DeleteSession(sessionID string) error
	EditUserMessageAndFork(ctx context.Context, parentID string, request model.EditUserMessageForkRequest) (model.EditUserMessageForkResult, error)
	ForkSessionLatest(parentID string) (string, error)
	ForkSubagents(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error)
	GoalBeginFor(ctx context.Context, sessionID string, request dto.BeginGoalRequest) (*dto.GoalRecord, error)
	GoalDoneFor(ctx context.Context, sessionID string, request dto.FinishGoalRequest, abort bool) (*dto.GoalRecord, error)
	GoalUpdateFor(ctx context.Context, sessionID string, request dto.UpdateGoalRequest) (*dto.GoalRecord, error)
	RequestFileReview(ctx context.Context, files []dto.ReviewFileRef) (dto.ReviewRequest, error)
	RecoverConversation(ctx context.Context, ref dto.ConversationSessionRef) (dto.SessionRecovery, error)
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

// PolicyFace 档位与策略（plan §1.3 D 组）：限制、档位、账号、插件、角色与调度策略。
type PolicyFace interface {
	ActivateSkill(name string) (model.SkillInfo, error)
	AccountDetails(name string) (dto.AccountDetails, error)
	// AddAccount 写账号档 + 热注册（角色卡下的「添加账户」）：账号是策略面
	// （与 SelectAccount / SetAccountWindow 同一类"用户对账号的显式决定"）。
	AddAccount(_ context.Context, draft dto.AccountDraft) (dto.AccountAddResult, error)
	DeleteAccount(name string) (dto.AccountDetails, error)
	UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error)
	CancelScheduledTask(id string) error
	CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error)
	ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error
	ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error
	ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error
	ScheduleTask(ctx context.Context, spec dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error)
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
	UpdateScheduledTask(ctx context.Context, id string, spec dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error)
}

// QueueFace 队列面（plan §1.3 E 组）：排队输入的召回/调序与输入建议；同组的无状态函数（`HasSigilPrefix` / `SigilOf` / `RenderDiag` / `ValidEffortLevels` / `AccountWindowPresets`）不是接口成员，仍以包级函数提供。
type QueueFace interface {
	RecallQueuedInput(sessionID string, index int) (string, error)
	ReorderQueuedInput(sessionID string, from, to int) error
	Suggestions(input string) []applicationinput.Suggestion
}

// 以下能力接口是 AP-002 的新入站面：调用方只声明自己实际使用的一小组方法，
// 不再为了一个操作依赖完整 Service 或旧的四组兼容接口。旧接口继续保留，
// 直到调用方归零；兼容层退场记录见工作包 implementation.md。

// ConversationAPI 覆盖一段对话的受理、取消、恢复、视图读取与显式审批决议。
type ConversationAPI interface {
	AddNotice(notice string)
	CancelAllChats()
	CancelChat(requestID string) bool
	CompactContextNow(ctx context.Context) (ContextCompactionResult, error)
	ConversationChat(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationChatRequest) (dto.ChatReceipt, error)
	ConversationView(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationViewRequest) (dto.SessionRecordPage, error)
	RecoverConversation(ctx context.Context, ref dto.ConversationSessionRef) (dto.SessionRecovery, error)
	ResolveInteraction(ctx context.Context, id, optionID string) error
	Submit(ctx context.Context, text string) error
	SubmitToSession(ctx context.Context, sessionID, text string) error
}

// SessionAPI 覆盖会话目录、快照、元数据、历史窗口、订阅、恢复和 fork 生命周期。
type SessionAPI interface {
	ActivateSession(sessionID string) error
	ArchiveSession(sessionID string) error
	BeginNewSession() error
	DeleteSession(sessionID string) error
	EditUserMessageAndFork(ctx context.Context, parentID string, request model.EditUserMessageForkRequest) (model.EditUserMessageForkResult, error)
	ForkSession(parentID string, request model.ForkRequest) (string, error)
	ForkSessionLatest(parentID string) (string, error)
	ListSessions() []model.SessionInfo
	LoadLatestHistory() error
	LoadMoreHistory(limit int) error
	ResumeSession(sessionID string) error
	SessionInputIndex(sessionID string) (model.SessionInputIndex, error)
	SessionMeta(sessionID string) (model.SessionMeta, error)
	SetSessionMeta(sessionID string, meta model.SessionMeta) error
	SnapshotOf(sessionID string) (model.SessionSnapshot, error)
	SubscribeSession(sessionID string, buffer int) (event.Subscription, error)
	SubscribeSessionWithReplay(sessionID string, buffer, replayWindow int) (event.Subscription, error)
	UnloadSession(sessionID string) error
	WaitCatalogRefresh(ctx context.Context) error
}

// WorkItemQueryAPI 覆盖工作项进度、执行记录与工作表格只读投影。
type WorkItemQueryAPI interface {
	WorkItemExecutionFor(ctx context.Context, mainSessionID, itemID string, page dto.ConversationViewRequest) (dto.WorkItemExecutionView, error)
	WorkTableRowsFor(sessionID string) []model.WorkItem
}

// TeammateQueryAPI 覆盖 teammate 安全详情与 WorkItem 会话实时只读投影。
type TeammateQueryAPI interface {
	TeammateInspectionFor(ctx context.Context, mainSessionID, role string) (dto.TeammateInspectionView, error)
	TeammateSessionLiveFor(sessionID string) dto.TeammateSessionLiveView
	TeammateSessionLivePageFor(sessionID string, offset, limit int) dto.TeammateSessionLiveView
}

// SubagentQueryAPI 覆盖子代理详情与中断恢复目录。
type SubagentQueryAPI interface {
	ListSubagentRecovery(sessionID string) ([]dto.SubagentRecoveryView, error)
	SubagentSessionDetail(nodeID string) (*model.SubagentDetail, error)
}

// TranscriptQueryAPI 覆盖会话 transcript、检索与工具结果正文回读。
type TranscriptQueryAPI interface {
	GetSessionTranscript(sessionID string, fromSeq, toSeq uint64) ([]model.TranscriptEvent, error)
	SearchHistory(ctx context.Context, query string, limit int) (dto.SearchResult, error)
	ToolResultContent(ctx context.Context, resultRef string, offset, limit int) (model.ToolResultPage, error)
}

// ExecutionQueryAPI 是 AP-002 的组合兼容面；新调用方应按实际能力依赖下列切片，
// 不再为了一个查询依赖全部 13 个方法。
type ExecutionQueryAPI interface {
	WorkItemQueryAPI
	TeammateQueryAPI
	SubagentQueryAPI
	TranscriptQueryAPI
}

// ExecutionCommandAPI 覆盖执行读模型里唯一的用户可写动作。
type ExecutionCommandAPI interface {
	UpdateWorkItemStatus(id, status string) error
}

// WorkspaceAPI 覆盖工作区生命周期、文件/Git 读取、文件写入和审阅请求。
type WorkspaceAPI interface {
	BindWorkspace(workspaceID string) error
	CreateWorkspace(name, rootPath, gitRemote string) error
	CreateWorkspaces(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error)
	ProjectRootFor(sessionID string) string
	RequestFileReview(ctx context.Context, files []dto.ReviewFileRef) (dto.ReviewRequest, error)
	UnbindWorkspace()
	WorkspaceChanges(repo string, limit int) (dto.WorkspaceChangesResult, error)
	WorkspaceFileContent(relPath string, limit int64) (dto.FileContent, error)
	WorkspaceFileCount() (dto.TreeCount, error)
	WorkspaceGitCommitDetail(repo, hash string, limit int) (dto.GitCommitDetail, error)
	WorkspaceGitCommitFileContent(repo, hash, relPath string, limit int64) (dto.FileContent, error)
	WorkspaceGitLog(repo string, limit int) (dto.GitLogResult, error)
	WorkspaceGitRepos() (dto.GitReposResult, error)
	WorkspaceTree(relPath string, depth int) (dto.TreeListing, error)
	WorkspaceWriteFile(relPath, content string) (dto.FileWriteResult, error)
}

// GoalAPI 覆盖目标状态机与只读治理视图。
type GoalAPI interface {
	GoalBeginFor(ctx context.Context, sessionID string, request dto.BeginGoalRequest) (*dto.GoalRecord, error)
	GoalDoneFor(ctx context.Context, sessionID string, request dto.FinishGoalRequest, abort bool) (*dto.GoalRecord, error)
	GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView
	GoalSkillActive() bool
	GoalStatusFor(sessionID string) (dto.GoalStatusView, error)
	GoalUpdateFor(ctx context.Context, sessionID string, request dto.UpdateGoalRequest) (*dto.GoalRecord, error)
}

// PolicyAPI 覆盖当前进程/会话的用户策略选择；账号与调度管理归 AdminAPI。
type PolicyAPI interface {
	ActivateSkill(name string) (model.SkillInfo, error)
	SessionReleaseWindow() int
	SetFullAccess(on bool) bool
	SetLoopLimit(ctx context.Context, limit int) error
	SetPermissionTier(tier string) (string, error)
	SetReasoningEffort(ctx context.Context, effort string) error
	SetSessionReleaseWindow(ctx context.Context, window int) (int, error)
	SwitchEffort(ctx context.Context, level string) error
	SwitchPlugin(ctx context.Context, name string) error
}

// AdminAPI 覆盖账号、调度任务等特权管理动作。
type AdminAPI interface {
	AccountBalance(ctx context.Context, name string) (dto.AccountBalance, error)
	AccountDetails(name string) (dto.AccountDetails, error)
	AddAccount(ctx context.Context, draft dto.AccountDraft) (dto.AccountAddResult, error)
	CancelScheduledTask(id string) error
	DeleteAccount(name string) (dto.AccountDetails, error)
	ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error
	ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error
	ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error
	ScheduleTask(ctx context.Context, spec dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error)
	SelectAccount(ctx context.Context, name string) error
	SetAccountWindow(ctx context.Context, name string, window int) (int, error)
	UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error)
	UpdateScheduledTask(ctx context.Context, id string, spec dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error)
}

// RoleAPI 覆盖群聊角色会话、草稿、生命周期顺序与角色权限读面。
type RoleAPI interface {
	AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error
	AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error
	AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error)
	CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error)
	ListRoleSessions(mainSessionID string) ([]string, error)
	ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error)
	ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error)
	RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error)
	SetLifecycleOrder(sessionID, policy string, roles []string) error
	SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error
	SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error)
}

// SubagentAPI 覆盖子代理派发、详情、恢复和树清理。
type SubagentAPI interface {
	ClearSubagentTree() error
	ForkSubagents(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error)
	ListSubagentRecovery(sessionID string) ([]dto.SubagentRecoveryView, error)
	ResumeInterruptedSubagents(ctx context.Context, sessionID string) (dto.SubagentResumeReport, error)
	ResumeSubagent(ctx context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error)
	SubagentSessionDetail(nodeID string) (*model.SubagentDetail, error)
}

// StateAPI 覆盖 Snapshot、订阅、空闲读数和工具记录等进程级只读投影。
type StateAPI interface {
	AnyChatRunning() bool
	PerfStats() model.PerfStats
	PromptLayers() []prompt.PromptLayer
	Snapshot() model.Snapshot
	Subscribe(buffer int) event.Subscription
	ToolRecordBoard() dto.ToolRecordBoard
	ToolRecordEnabled() bool
	WaitForIdle(ctx context.Context) error
}

// QueueAPI 覆盖运行中会话的排队输入编辑。
type QueueAPI interface {
	RecallQueuedInput(sessionID string, index int) (string, error)
	ReorderQueuedInput(sessionID string, from, to int) error
	Suggestions(input string) []applicationinput.Suggestion
}

// LifecycleAPI 只表达进程级停机意图。
type LifecycleAPI interface {
	BeginGracefulShutdown()
	Shutdown()
}

// SessionQuery 是 AP-004 的只读会话面：目录、项目根、展示元数据与目录刷新等待。
// 它只持有窄读端口，不持有完整 Service，也不得进入共享写锁或慢 I/O。
type SessionQuery interface {
	GetSessionTranscript(sessionID string, fromSeq, toSeq uint64) ([]model.TranscriptEvent, error)
	ListSessions() []model.SessionInfo
	ProjectRootFor(sessionID string) string
	SessionInputIndex(sessionID string) (model.SessionInputIndex, error)
	SessionMeta(sessionID string) (model.SessionMeta, error)
	SnapshotOf(sessionID string) (model.SessionSnapshot, error)
	WaitCatalogRefresh(ctx context.Context) error
}

// ConversationHistoryQuery 是 AP-005 的只读历史窗口面。分页只移动会话可见窗口，
// 不写会话持久化事实；磁盘冷读必须在共享 ViewMu 之外完成。
type ConversationHistoryQuery interface {
	LoadLatestHistory() error
	LoadMoreHistory(limit int) error
}
