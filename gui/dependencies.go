package gui

// dependencies.go — GUI 自己的窄端口与装配面（M3，宽门面退场）。
//
// 为什么端口按 owner 切：一个端口的形状必须与某个 owner 的方法逐字一致，装配处
// （`wiring/bootstrap/frontend_gui.go`）才能直接交出组件指针——绑定语句本身就是
// 编译期结构断言。按"界面关切"切会把两个 owner 的方法塞进同一个端口，那就又要一份
// 转发层把它们缝合起来，正是本工作包要删掉的东西（见 headless/dependencies.go 与
// tui/dependencies.go 的同一条口径）。
//
// 为什么方法名跟着 owner 走（`Begin` 而不是 `BeginNewSession`、`Resolve` 而不是
// `ResolveInteraction`、`ForkLatest` 而不是 `ForkSessionLatest`）：改名只可能发生在
// 转发层，而转发层不拥有任何判据。桥面的 Go 方法名与 Wails 暴露给 JS 的名字
// （`/rpc` 线上名）保持不变，改名吸收在 `bridge.go` 的方法层一行转发里。
//
// 所有字段都只承载一个 owner 的最小消费面；能力有无只由装配表说话，缺 owner
// 就是装配错误。这里不存在运行时能力探测或宽门面回退。

import (
	"context"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// Accounts 是账号管理面（唯一 owner：`application/admin` 的 AccountApplication）。
//
// 七个动作都是"用户对账号的显式决定"：选中、改上下文窗口、增改删、读非秘密字段、
// 查一次余额。校验、落盘与热注册判据都在 owner 内，桥面只做参数归一与转发。
type Accounts interface {
	AddAccount(ctx context.Context, draft application.AccountDraft) (application.AccountAddResult, error)
	AccountDetails(name string) (application.AccountDetails, error)
	UpdateAccount(update application.AccountUpdate) (application.AccountDetails, error)
	DeleteAccount(name string) (application.AccountDetails, error)
	AccountBalance(ctx context.Context, name string) (application.AccountBalance, error)
	SelectAccount(ctx context.Context, name string) error
	SetAccountWindow(ctx context.Context, name string, window int) (int, error)
}

// Switches 是用户的策略旋钮（唯一 owner：`application/policy` 的 SwitchApplication）：
// 权限档位（全权开关 / manual·edit·auto·full 档）与 composer 底部的选择
// （推理强度 / 循环次数 / 会话驻留窗口 / effort / 插件切换）。
//
// 这些方法都不带 ctx：档位是即时旋钮，owner 从来没有读过那个形参（旧门面上的 ctx
// 是门面自己的形状，不是判据）。
type Switches interface {
	SetFullAccess(on bool) bool
	SetPermissionTier(tier string) (string, error)
	SwitchEffort(level string) error
	SetReasoningEffort(effort string) error
	SetLoopLimit(limit int) error
	SetSessionReleaseWindow(window int) (int, error)
	SessionReleaseWindow() int
	SwitchPlugin(ctx context.Context, name string) error
}

// RoleSettings 是"设置 → 角色设置"的写读面（唯一 owner：`application/policy` 的
// RoleApplication）。
//
// 三条读路径（PermissionVector / PermissionRoles / RoleDefaultVector）与六条写路径都
// 只是把归一后的 (sessionID, role) 交给 owner：登记优先、缺失按五级链路解析这套判据
// 在 owner 里，桥面一格都不重算。
//
// 角色默认向量（RoleDefaultVector / SetRoleDefaultVector）是**跨会话**的那一份，会话
// 登记压在它之上；explicit=false 表示这个角色没定过默认，此时 Vector 是链路解析结果。
// 空 account = 未设置，由账号池按角色选择；空向量 = 显式不给任何能力。
type RoleSettings interface {
	RolePermissionVector(sessionID, role string) ([]string, error)
	SetRolePermissionVector(sessionID, role string, vector []string) error
	RoleDefaultVector(sessionID, role string) ([]string, bool, error)
	SetRoleDefaultVector(sessionID, role string, vector []string) error
	RoleAccount(sessionID, role string) (string, bool, error)
	SetRoleAccount(sessionID, role, account string) error
	RoleInheritMainAgent(sessionID, role string) (bool, error)
	SetRoleInheritMainAgent(sessionID, role string, inherit bool) error
	RoleSSHUserSpaces(sessionID, role string) ([]string, error)
	RoleDefaultSSHUserSpaces(sessionID, role string) ([]string, error)
	SetRoleSSHUserSpaces(sessionID, role string, paths []string) error
	SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error
}

// Completion 是 composer 输入补全面（唯一 owner：`application/input` 的 Application）。
type Completion interface {
	Suggestions(value string) []application.Suggestion
}

// History 是会话历史的翻页读面（唯一 owner：`application/history` 的 Application）。
//
// 翻页只移动可见窗口，数据不动；页大小与"还有没有更早一页"的判据在 owner 里。
type History interface {
	LoadMoreHistory(limit int) error
	LoadLatestHistory() error
}

// ViewSnapshot 是当前视图会话的快照读数（唯一 owner：`application/session/viewstate`
// 的 SnapshotApplication）。
//
// 快照是渲染层的唯一权威基线：状态、会话目录、运行期投影都在这一份里。owner 负责
// 深拷贝与"应用态补充"（状态行、草稿行、目录富化）的加锁顺序，桥面只读它，不拼
// 第二份视图。
type ViewSnapshot interface {
	Snapshot() application.Snapshot
}

// ViewEvents 是视图事件订阅（唯一 owner：`application/session/viewstate` 的
// Coordinator）。
//
// 三条订阅形状都是 owner 自己的方法：全量、按会话、按会话带重放窗口。会话级订阅
// 的过滤判据（哪些事件算这个会话的、哪些进程级事件对每个会话都要投递）在 owner
// 里，桥面只决定"用哪个窗口"，不重算路由。
type ViewEvents interface {
	Subscribe(buffer int) application.Subscription
	SubscribeSession(sessionID string, buffer int) (application.Subscription, error)
	SubscribeSessionWithReplay(sessionID string, buffer, replayWindow int) (application.Subscription, error)
}

// ── S4：会话动作与内容面（一条动作一个 owner）───────────────────────────
//
// 会话的六个动作各自属于一个 owner：草稿入口、恢复、分叉、删除、归档、展示
// 元数据。它们**不能**被合成一个端口：没有哪个组件同时拥有这六件事，合成后的
// 接口只能由一个转发层满足，而转发层不拥有判据（正是要删掉的东西）。

// SessionCatalog 是会话目录/记录读面与目录收敛等待（唯一 owner：
// `application/session/query` 的 Application）。
//
// 五条读法都只读持久化事实（不要求目标会话驻留）：目录、单会话快照、事件日志
// 区间、用户输入索引，以及"会改目录的命令返回前等一轮覆盖本次变更的刷新"。
type SessionCatalog interface {
	ListSessions() []application.SessionInfo
	SnapshotOf(sessionID string) (application.SessionSnapshot, error)
	GetSessionTranscript(sessionID string, fromSeq, toSeq uint64) ([]application.TranscriptEvent, error)
	SessionInputIndex(sessionID string) (model.SessionInputIndex, error)
	WaitCatalogRefresh(ctx context.Context) error
}

// DraftStarter 进入新草稿会话（唯一 owner：`application/session` 的
// DraftEntryApplication）。草稿早分配 SID、旧会话收尾都在 owner 里。
type DraftStarter interface {
	Begin() error
}

// SessionResumer 把视图切到某个已存在会话（唯一 owner：`application/session`
// 的 ResumeApplication）：hot / restoring / cold 三条分支的选择在 owner 里。
type SessionResumer interface {
	Resume(sessionID string) error
}

// SessionForker 从父会话最新完整轮次分叉（唯一 owner：`application/session`
// 的 ForkApplication）。
type SessionForker interface {
	ForkLatest(parentID string) (string, error)
}

// SessionDeleter 删除一条会话记录并释放驻留资源（唯一 owner：
// `application/session` 的 DeleteApplication）。
type SessionDeleter interface {
	Delete(sessionID string) error
}

// SessionArchiver 归档一条会话（唯一 owner：`application/session` 的
// ArchiveApplication）；运行中/待批的拒绝判据在 owner 里。
type SessionArchiver interface {
	Archive(sessionID string) error
}

// SessionMetaWriter 写会话展示元数据（唯一 owner：`application/session` 的
// MetaApplication）：置顶/别名/排序位，随会话目录下发。
type SessionMetaWriter interface {
	SetSessionMeta(sessionID string, meta application.SessionMeta) error
}

// SessionSubmitter 向**指定**会话提交输入（唯一 owner：`application/session`
// 的 SubmitApplication）；与视图会话无关，因此显式带 sessionID。
//
// SubmitToSessionWithImages 是同一个 owner 的带图入口：图片元数据由桥面按会话
// 媒体分区解析后传入，随这条输入一起落进用户消息（对话区据此画出缩略图）。
type SessionSubmitter interface {
	SubmitToSession(ctx context.Context, sessionID, text string) error
	SubmitToSessionWithImages(ctx context.Context, sessionID, text string, images []dto.ComposerImage) error
}

// Submitter 把一条输入提交给当前视图会话（唯一 owner：`application/input` 的
// SubmissionApplication）。
type Submitter interface {
	Submit(ctx context.Context, text string) error
}

// TurnControl 是回合与进程级活动面（唯一 owner：`application/lifecycle` 的
// Application）：关窗前的停机闸门、空闲等待、单会话/全会话取消。
//
// 五条方法都在同一个 owner 里，因此可以是一个端口；`AnyChatRunning` 读的是
// **全部会话单元**的权威状态（视图空闲、后台在跑也算活动），关窗路径不看快照
// 里的视图回合状态。
type TurnControl interface {
	BeginGracefulShutdown()
	AnyChatRunning() bool
	CancelAllChats()
	CancelChat(requestID string) bool
	WaitForIdle(ctx context.Context) error
}

// InteractionResolver 裁决一条待决交互（唯一 owner：`application/session` 的
// InteractionApplication）。
type InteractionResolver interface {
	Resolve(ctx context.Context, interactionID, optionID string) error
}

// Prompts 是 prompt 前缀层读数（唯一 owner：`application/prompt` 的
// PromptStack）。层内容不进快照（轨迹视图单独拉取，避免私有指令泄漏）。
type Prompts interface {
	Layers() []application.PromptLayer
}

// PerfReader 是进程性能读数（唯一 owner：`application/observability` 的
// PerfApplication）：无内容指标，进不了会话快照。
type PerfReader interface {
	PerfStats() application.PerfStats
}

// MessageEdit 是「编辑用户消息并自动分叉」的原子能力面（唯一 owner：
// `application/conversation/edit` 的 EditApplication）。
//
// 它由 owner 一次调用完成分叉 + 提交，桥面不得拆成 ForkSession + Submit 两条
// 请求（那会把原子性交给前端）。
type MessageEdit interface {
	EditUserMessageAndFork(ctx context.Context, parentID string, request model.EditUserMessageForkRequest) (model.EditUserMessageForkResult, error)
}

// Queue 是排队输入的编辑面（唯一 owner：`application/conversation` 的
// QueueApplication）：只作用于回合运行期间排队的输入，不触碰正在跑的回合。
type Queue interface {
	ReorderQueuedInput(sessionID string, from, to int) error
	RecallQueuedInput(sessionID string, index int) (string, error)
}

// ConversationSessions 是统一主/子代理会话读写面（唯一 owner：
// `application/conversation.UnifiedApplication`）。
type ConversationSessions interface {
	View(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationViewRequest) (dto.SessionRecordPage, error)
	Chat(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationChatRequest) (dto.ChatReceipt, error)
	Recover(ctx context.Context, ref dto.ConversationSessionRef) (dto.SessionRecovery, error)
}

// HistorySearch 是压缩栈索引的历史检索面（唯一 owner：
// `application/conversation/search.Application`）。
type HistorySearch interface {
	SearchHistory(ctx context.Context, query string, limit int) (dto.SearchResult, error)
}

// References 是工具结果引用读面（唯一 owner：`application/reference.Application`）。
type References interface {
	ToolResultContent(resultRef string, offset, limit int) (model.ToolResultPage, error)
}

// WorkspaceCatalog 是项目环境与工作区查询面（唯一 owner：
// `application/workspace.Application`）。
type WorkspaceCatalog interface {
	WorkspaceEnvironments(projectID string) ([]model.WorkspaceEnvironmentInfo, string, error)
	SetWorkspaceEnvironment(projectID, name string, change model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error)
	DeleteWorkspaceEnvironment(projectID, name string) error
	SetDefaultWorkspaceEnvironment(projectID, name string) error
	WorkspaceGitRepos() (dto.GitReposResult, error)
	GitLog(repo string, limit int) (dto.GitLogResult, error)
	Changes(repo string, limit int) (dto.WorkspaceChangesResult, error)
	CommitDetail(repo, hash string, limit int) (dto.GitCommitDetail, error)
	CommitFileContent(repo, hash, relPath string, limit int64) (dto.FileContent, error)
	CreateWorkspaces(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error)
}

// WorkspaceActions 是工作区绑定、树与文件写面（唯一 owner：
// `application/workspace/switch.UseCase`）。
type WorkspaceActions interface {
	CreateWorkspace(name, rootPath, gitRemote string) error
	BindWorkspace(workspaceID string) error
	UnbindWorkspace()
	Tree(relPath string, depth int) (dto.TreeListing, error)
	FileCount() (dto.TreeCount, error)
	FileContent(relPath string, limit int64) (dto.FileContent, error)
	WriteFile(relPath, content string) (dto.FileWriteResult, error)
}

// ComposerMedia 是「输入框图片附件」的唯一 owner 面（`backend/tool/io/media` 的
// SessionMedia，经组合根以 Runtime.ComposerMedia 注入）。
//
// 四格都是**用户动作或它们的只读配套**：把拖拽/粘贴进来的图片落进会话媒体分区并
// 挂进下一次请求随图（模型因此真的看见这张图）、给出原图位置让系统默认查看器打开、
// 把输入框里那几个 ref 解析成「这条消息带了哪几张图」（提交时写进用户消息），以及
// 按 ref 读回字节让对话区画出缩略图。四件事都不过权限档位——用户自己按下的动作
// 不是主代理动作（与文件保存同一条口径）。
//
// 形状由消费方（就是这里）声明：媒体域不需要认识 GUI，它只要在结构上满足这几格，
// 装配处的赋值语句本身就是编译期断言。
type ComposerMedia interface {
	AttachUserImage(ctx context.Context, request dto.ComposerImageRequest) (dto.ComposerImage, error)
	MediaFilePath(ctx context.Context, sessionID, ref string) (string, error)
	// ResolveUserImages 把一个会话里已落盘的图片 ref 解析成展示元数据；ref 不在
	// 该会话分区里就报错（提交不许把打不开的图写进消息）。
	ResolveUserImages(ctx context.Context, sessionID string, refs []string) ([]dto.ComposerImage, error)
	// ReadUserImage 读回一张已落盘图片的字节（渲染层缩略图；路径不回渲染层）。
	ReadUserImage(ctx context.Context, sessionID, ref string) (dto.ComposerImageData, error)
}

// ScheduledTasks 是定时任务增改删面（唯一 owner：
// `application/admin/scheduler.SchedulerApplication`）。
type ScheduledTasks interface {
	ScheduleTask(ctx context.Context, spec dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error)
	UpdateScheduledTask(ctx context.Context, id string, spec dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error)
	CancelScheduledTask(id string) error
}

// WorkItems 是工作项、teammate 与会话实时投影面（唯一 owner：
// `application/execution.WorkItemApplication`）。
type WorkItems interface {
	WorkItemExecution(ctx context.Context, mainSessionID, itemID string, page dto.ConversationViewRequest) (dto.WorkItemExecutionView, error)
	TeammateInspection(ctx context.Context, mainSessionID, role string) (dto.TeammateInspectionView, error)
	SessionLive(sessionID string) dto.TeammateSessionLiveView
	SessionPage(sessionID string, offset, limit int) dto.TeammateSessionLiveView
	UpdateWorkItemStatus(id, status string) error
}

// Subagents 是子代理详情与清理面（唯一 owner：
// `application/execution.SubagentApplication`）。
type Subagents interface {
	Detail(nodeID string) (*application.SubagentDetail, error)
	ClearTree() error
}

// RoleSessionReader 是角色会话只读观察面（唯一 owner：装配后的
// `contract.RoleSessionsPort`）。
type RoleSessionReader interface {
	RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error)
}

// ToolRecords 是工具台账看板与开关面（唯一 owner：
// `application/execution/toolrecord.Application`）。
type ToolRecords interface {
	Board() application.ToolRecordBoard
	Enabled() bool
	SetEnabled(enabled bool) (bool, error)
}

// SessionSSH 是会话固定环境与放权面（唯一 owner：
// `application/session.SSHApplication`）。
type SessionSSH interface {
	State(sessionID string) (model.SessionSSHInfo, error)
	SetAllowed(sessionID string, envs []string) (model.SessionSSHInfo, error)
	SetEnv(sessionID, projectID, env string) (model.SessionSSHInfo, error)
}

// Dependencies 是 GUI 从这里取应用能力的装配面：一个字段一个 owner 端口。
//
// 绑定语句（`gui/dependencies.go` 的字段类型 ← `assembly.Components` 的具体组件指针）
// 就是编译期结构断言，因此 gui 包内既没有适配器类型，也不再需要
// 额外的统一接口断言来钉住真机。
type Dependencies struct {
	// Accounts：S1 账号管理面。
	Accounts Accounts
	// Switches：权限档位（S1）+ composer 旋钮（S2）。
	Switches Switches
	// RoleSettings：S2 角色设置读写面。
	RoleSettings RoleSettings
	// Completion：S2 输入补全。
	Completion Completion
	// History：S2 历史翻页。
	History History
	// ViewSnapshot：S3 视图快照读数。
	ViewSnapshot ViewSnapshot
	// ViewEvents：S3 视图事件订阅（全量 / 按会话 / 带重放窗口）。
	ViewEvents ViewEvents

	// SessionCatalog：S4 会话目录/记录读面与目录收敛等待。
	SessionCatalog SessionCatalog
	// Draft：S4 新草稿入口。
	Draft DraftStarter
	// Resume：S4 视图切到已存在会话。
	Resume SessionResumer
	// Fork：S4 从父会话最新轮次分叉。
	Fork SessionForker
	// Delete：S4 删除会话记录。
	Delete SessionDeleter
	// Archive：S4 归档会话。
	Archive SessionArchiver
	// Meta：S4 会话展示元数据。
	Meta SessionMetaWriter
	// SessionSubmit：S4 向指定会话提交输入。
	SessionSubmit SessionSubmitter
	// Submission：S4 向当前视图会话提交输入。
	Submission Submitter
	// Turn：S4 回合/进程级活动面（停机闸门、空闲等待、取消）。
	Turn TurnControl
	// Interaction：S4 待决交互裁决。
	Interaction InteractionResolver
	// Prompts：S4 prompt 前缀层读数。
	Prompts Prompts
	// Perf：S4 进程性能读数。
	Perf PerfReader
	// MessageEdit：S4 编辑用户消息并原子分叉。
	MessageEdit MessageEdit
	// Queue：S4 排队输入编辑。
	Queue Queue
	// Conversation：统一主/子代理会话读写。
	Conversation ConversationSessions
	// HistorySearch：压缩栈索引历史检索。
	HistorySearch HistorySearch
	// References：工具结果引用读面。
	References References
	// WorkspaceCatalog：项目环境、git 与批量项目查询。
	WorkspaceCatalog WorkspaceCatalog
	// WorkspaceActions：工作区绑定、树与文件写面。
	WorkspaceActions WorkspaceActions
	// Composer：输入框图片附件（落会话媒体分区 + 原图位置）。
	Composer ComposerMedia
	// Schedules：定时任务增改删。
	Schedules ScheduledTasks
	// WorkItems：工作项、teammate 与会话实时投影。
	WorkItems WorkItems
	// Subagents：子代理详情与清理。
	Subagents Subagents
	// RoleSessions：角色会话只读观察。
	RoleSessions RoleSessionReader
	// ToolRecords：工具台账看板与开关。
	ToolRecords ToolRecords
	// SessionSSH：会话固定环境与放权。
	SessionSSH SessionSSH
}
