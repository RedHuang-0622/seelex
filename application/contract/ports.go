// Package contract defines the application-owned interfaces for external systems.
package contract

import (
	"context"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/approval"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"

	seelexctxsearch "github.com/RedHuang-0622/seelex/seelexctx/search"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

type EngineMessage struct {
	Role             string
	ReasoningContent string
	Content          string
	ContentSet       bool
	ToolCallID       string
	Name             string
	ToolCalls        []EngineToolCall
}
type EngineToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// EngineBase 是引擎的公共基础面（framework 与 application 共享的最小契约）。
// ChatEngine（应用契约）与 adapters.ReactorEngine（框架适配面）都内嵌它，
// 表达"引擎 = 基础面 + 扩展面"。History/AppendHistory 因消息类型边界
// （types.Message ↔ EngineMessage）分属两侧，不并入基础面。
type EngineBase interface {
	ChatStream(context.Context, string, func(string)) (string, error)
	ClearHistory()
	SessionID() string
	SetSystemPrompt(string)
	SetMaxLoops(int)
}

type ChatEngine interface {
	EngineBase
	History() []EngineMessage
	ReplaceHistory(string, []EngineMessage) error
	StartSession() string
	TraceText() string
	TokenCount() string
	// AppendHistory 追加消息到引擎内部对话历史。
	//
	// **不要在 OnIterationComplete/OnToolComplete 这类循环回调里调用它。**
	// 那些回调是 ChatStream 同 goroutine 的同步回调，而 ChatStream 全程持有
	// 引擎会话锁（Seele session.Session.ChatStream）；AppendHistory 走同一把
	// 锁 = 同 goroutine 自锁死（2026-09-22 实测：会话永远「运行中」，排队输入
	// 再也不发出去，任务关不掉）。锁内的"注入"只能落在既有的循环内数据流
	// （ContextController 的 ReplaceHistory 决策），应用侧注入一律放到
	// ChatStream 之前/之后的锁外安全点。
	AppendHistory(msg types.Message)
	// NodeSessionConversation 返回节点子代理的会话记录（运行中实时 /
	// 结束后快照；只读子代理 actor，安全）。
	NodeSessionConversation(nodeID string) ([]types.Message, bool)
	// NodeContextSnapshot 返回节点子代理的结构化上下文快照（Goal/
	// Findings/Decisions/TokenEstimate 等；运行中实时导出、结束后快照；
	// 只读子代理 actor，安全）。
	NodeContextSnapshot(nodeID string) (*snapshot.ContextSnapshot, bool)
	// NodeToolResult 读回节点子代理的工具结果原始内容（ref 带
	// node:<nodeID>: 前缀；内存态，运行中/结束后可读；只读子代理归档器）。
	NodeToolResult(nodeID, ref string) (string, bool)
	// NodeWorktreeInfoFor 返回节点 worktree 现场信息（失败/被拒路径现场
	// 保留，Path 即人工恢复入口；成功路径已清理 → false）。
	NodeWorktreeInfoFor(nodeID string) (dto.NodeWorktreeInfo, bool)
	// SubscribeSubagentLive 订阅 node 第一视角实时流（阶段+工具事件即时
	// 投递）；返回历史回放（subagent start 以来的有界事件缓冲）+ 只读
	// 实时通道 + 取消函数。
	SubscribeSubagentLive(nodeID string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error)
	// SubAgentTree 返回 fork 子代理树的只读投影（内存态，不落盘；
	// GUI 树视图数据源，经权威 Snapshot 增量携带）。
	SubAgentTree() []dto.SubAgentTreeNode
}

// SessionChatEngine 是 ChatEngine 的会话路由扩展：多会话并行执行时，
// 执行路径必须携带显式 sessionID，避免活跃会话切换串写正在进行的请求。
// EnginePort 实现该接口；未实现的测试桩按旧活跃会话路径退化（单会话兼容）。
type SessionChatEngine interface {
	ChatEngine
	// ChatStreamFor 向指定会话的引擎提交一次流式对话。会话引擎未实例化
	// 时返回错误（执行路径必须先 ResumeSession/StartSession 注册）。
	ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error)
	// HistoryFor 返回指定会话引擎的历史（只读拷贝）。
	HistoryFor(sessionID string) []EngineMessage
	// AppendHistoryFor 追加消息到指定会话引擎历史。
	AppendHistoryFor(sessionID string, msg types.Message)
	// ClearHistoryFor 清空指定会话引擎历史。
	ClearHistoryFor(sessionID string)
	// SetSystemPromptFor 设置指定会话引擎的 system prompt。
	SetSystemPromptFor(sessionID, prompt string)
	// ReplaceHistoryFor 会话内历史替换：替换指定会话引擎历史，但不切换活跃
	// 会话（后台并行执行的 context 装配/恢复路径用）。
	ReplaceHistoryFor(sessionID string, history []EngineMessage) error
	// HasSession 报告目标会话引擎是否已实例化（后台提交前检查；未加载的
	// 会话需先 ActivateSession 恢复）。
	HasSession(sessionID string) bool
}

// 这里曾有 InLoopEngine（「回合内自持锁历史通道」）：Seele 的 ChatStream 从进函数
// 持锁到出函数，工具 handler / LoopHooks 再调 History / ReplaceHistory /
// SetSystemPrompt 就是同 goroutine 抢自己已持有的非重入锁（自锁）。Seele 现在用
// 「回合闸门 + 工作状态短临界区」替换了这个模型：回合准入不持锁，History 永不阻塞，
// ReplaceHistory 忙时排队到下一个检查点。于是环内与环外走同一套方法，宿主不再需要
// 一个"我在不在环内"的判定端口——该端口连同 ctx 透传一并删除。

type RuntimePort interface {
	Model() string
	Provider() string
	Accounts() []model.AccountInfo
	SelectAccount(string) bool
	VisibleTools(context.Context) []model.Tool
	ActivePlugin() string
	FullAccess() bool
	SetFullAccess(bool)
	// SetFullAccessFor 按会话设置全权模式：全权是会话级用户决定（G4），
	// 执行面按工具调度 ctx 的会话解析——A 的全权不得替 B 放行，B 的起点
	// 同步也不得关掉 A（多会话并行）。
	SetFullAccessFor(sessionID string, on bool)
	// PermissionTier 返回进程级默认权限档位（manual/edit/auto/full；装配期
	// 捕获面，未做会话级选择的回退值）。
	PermissionTier() string
	// SetPermissionTierFor 按会话设置权限档位（空会话 ID = 进程级默认）。
	// 档位是会话级的用户决定（G4）：A 会话切档不得影响 B；未识别的档位 id
	// 报错且不改变当前档位。full 档 = 旧 full_access。
	SetPermissionTierFor(sessionID, tier string) error
	SetRuntimeVisibilityProjection(dto.RuntimeVisibilityProjection)
	SetParentEvidenceProjection(dto.ParentEvidenceProjection)
	DrainSubagentContexts() []string
	SetPlanPolicy(dto.PlanPolicy)
	// SetPlanPolicyFor 按会话写入 plan 策略槽（G1-C：plan_load/plan_run 按
	// 执行 ctx 会话读取自己的额度；chat 起点由 application 按会话 effort 同步）。
	SetPlanPolicyFor(sessionID string, policy dto.PlanPolicy)
	PrepareReplan(context.Context, dto.ReplanRequest) (dto.PlanPreflight, error)
	ReplanMetrics() dto.ReplanMetrics
	SetPlanBranchBinding(dto.PlanBranchBinding)
	BindProjectRoot(rootPath string) error
	UnbindProjectRoot()
	// BindProjectRootFor 绑定指定会话自己的工具路径根：多项目并行时，工具按
	// 执行 ctx 的会话解析项目根（后台会话不得借用视图会话的根）。
	// UnbindProjectRootFor 在会话解绑工作区/归档时清空该会话的根。
	BindProjectRootFor(sessionID, rootPath string) error
	UnbindProjectRootFor(sessionID string)
	// SetCurrentTaskBatch 设置会话级 task 注册表默认批次（startChat 写入
	// requestID；按会话保存，后台会话不覆盖活跃注册表默认批次——L3）。
	SetCurrentTaskBatch(sessionID, batchID string)
	// TodoSnapshot 返回当前 todolist 清单只读拷贝（GUI 待办面板数据源）。
	TodoSnapshot() []dto.TodoItem
	// SetTodoStatus 设置待办项三态（pending/doing/done；GUI 工作表格状态
	// 更新入口；越界/非法状态返回错误）。
	SetTodoStatus(index int, status dto.TodoItemStatus) error
	// TaskSnapshot 返回**项目/全局** task 表只读快照（worktable 投影数据源）：
	// 实时注册表（当前会话）与各会话 scope 分区合并。工作表格是跨会话台账，
	// 不随会话切换丢行；自动条目的行 ID 由进程级分配器保证进程内唯一
	// （同 ID 在台账里是同一行）。
	TaskSnapshot() []dto.TaskRecord
	// TaskSnapshotFor 返回指定会话的 task 注册表快照（**会话级**读面：
	// 落盘 SessionRecord.Tasks 与请求尾部打点块用；后台会话收尾不得读活跃
	// 注册表，对应 R6/P2）。
	TaskSnapshotFor(sessionID string) []dto.TaskRecord
	// ReleaseSessionAsync 终止某会话名下所有在途作业（bash_bg 进程 / read_batch
	// 扇出 / subagent 编排），返回被登记的句柄数。会话删除/归档**必须**调用：
	// 句柄表按会话持有执行体，会话没了就再没有 job_manage 能拿到它——不杀就是
	// 无人认领的孤儿，而工作打点表会一路跟着它显示 running。
	ReleaseSessionAsync(sessionID string) int
	// AsyncPendingFor 返回某会话此刻还在跑的后台命令数（能力未开时恒 0）。
	// 消费点是"无进展预算"：安静的长命令被反复取回时载荷逐字节相同，字节口径的
	// 进展不会推进，但有在途执行被查询本身就是进展。
	AsyncPendingFor(sessionID string) int
	// AsyncRunsSnapshot 返回后台命令执行表的**只读投影**（按派发顺序）。消费点是
	// 工作表格与请求尾部打点块（application/core/work_table_async.go）。
	// 它刻意不是 task 注册表条目：句柄表在内存，进注册表就会随会话落盘并在重启后
	// 复活成一条永远 running 的假行（不变量 I-21）。
	AsyncRunsSnapshot() []dto.AsyncRunRecord
	// AsyncRunEvents 返回后台执行表的变化信号口：派发、终态、驱逐、新字节各发一次
	// （latest-wins，容量 1）。消费者必须自己汇聚，不得在信号回调里读表。
	AsyncRunEvents() <-chan struct{}
	// TaskAdd 主动登记 task（幂等：Key 命中返回既有记录）。
	TaskAdd(spec dto.TaskSpec) (dto.TaskRecord, bool, error)
	// TaskAddFor 按归属会话登记 task：当前任务会话写实时注册表，后台会话写
	// 自身 scope 分区（写自有域；跨会话污染收口）。
	TaskAddFor(sessionID string, spec dto.TaskSpec) (dto.TaskRecord, bool, error)
	// ResolveTaskByKey 按幂等键查 task（子代理装配现成 task_id 用）。
	ResolveTaskByKey(key string) (dto.TaskRecord, bool, error)
	// ResolveTaskByKeyFor 按归属会话查 task。
	ResolveTaskByKeyFor(sessionID, key string) (dto.TaskRecord, bool, error)
	// TaskSetStatus 更新 task 状态（retry 自增计数）。
	TaskSetStatus(id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error)
	// TaskSetStatusFor 按归属会话更新 task 状态。
	TaskSetStatusFor(sessionID, id string, status dto.TaskStatus, evidence string) (dto.TaskRecord, error)
	// TaskAttachParticipant 把子代理挂为 task 参与者（幂等）。
	TaskAttachParticipant(id, participant string) (dto.TaskRecord, error)
	// TaskChangedChannel 返回 task.changed 输出 channel（CSP：变更即投递，
	// application 消费者直发增量，不拉脏）。
	TaskChangedChannel() <-chan dto.TaskRecord
	// SubagentTreeEvents 返回子代理树生命周期信号 channel（CSP 消费者刷新
	// 工作表格；取代同步回调 observer）。
	SubagentTreeEvents() <-chan struct{}
	// PlanNodeEventChannel 返回 plan 节点事件 channel（CSP 消费者串行处理；
	// 取代同步回调）。
	PlanNodeEventChannel() <-chan dto.PlanNodeEvent
	// SwitchSessionTasks 会话级 task 隔离：离开当前会话时把其注册表快照
	// 存入该会话的 scope 分区，切换后整体替换为目标会话 task（复用 session
	// stack 存储）。它只改变"当前会话"的实时注册表——**工作表格取全局读面**
	// （TaskSnapshot = 实时注册表 + 全部分区），不随本调用丢行。
	// sessionID 为空表示进入草稿（无会话归属）。
	SwitchSessionTasks(sessionID string, records []dto.TaskRecord)
	// SetSessionWorkspace 记录会话绑定的 workspace ID（framework
	// DurableHistory 按显式键落盘用；R3 键漂移收敛）。
	SetSessionWorkspace(sessionID, workspaceID string)
	// ScheduledCommands 返回定时/周期任务白名单命令展示信息（GUI 新建弹窗数据源）。
	ScheduledCommands() []dto.ScheduledCommandInfo
	// ScheduledTasksSnapshot 返回定时/周期任务只读快照（GUI 定时任务面板数据源）。
	ScheduledTasksSnapshot() []dto.ScheduledTaskStatus
	// ScheduleTask 创建并启动一个定时/周期任务（变更入口；Runtime 调度器执行）。
	ScheduleTask(context.Context, dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error)
	// CancelScheduledTask 取消并移除定时/周期任务。
	CancelScheduledTask(string) error
	// ClearSubagentTree 清空子代理树（GUI「清空」入口；失败节点显式清走，
	// 详情数据面不受影响）。
	ClearSubagentTree() error
	// RestoreSubagentAnchors 从持久化重建目标会话的子代理恢复锚点
	// （重启/切页后：fork 树 + worktable 认领 subagent:<节点会话ID>）。
	RestoreSubagentAnchors(sessionID string) error
	// ListSubagentRecovery 列出目标会话下残留子代理单元的恢复态（只读：
	// active/是否有结论/是否可续跑；不改状态、不重跑）。
	ListSubagentRecovery(sessionID string) ([]dto.SubagentRecoveryView, error)
	// ResumeInterruptedSubagents 冷恢复续跑目标会话下未完成的子代理
	// （七步模板：补占位 → 重建现场 → system 注入 → 同键重跑 → 收敛）。
	ResumeInterruptedSubagents(ctx context.Context, sessionID string) (dto.SubagentResumeReport, error)
	// ResumeSubagent 定点续跑单个子代理（幂等键 = 节点 ID；失败可重试）。
	ResumeSubagent(ctx context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error)
	// ForkSubagents 直接派发一批子代理（自动化/冒烟入口；与模型调用
	// fork_subagents 同一条执行链，不新增旁路）。
	ForkSubagents(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error)
	// SetSubagentParentRepairer 注入父侧历史补齐钩子（缺失的子代理结果 →
	// provider-only tool 占位）；组合根接线，未接线时该步退化为 no-op。
	SetSubagentParentRepairer(fn func(sessionID string) error)
	// SearchHistory 在会话压缩栈（语义索引）上检索历史聊天记录
	// （GUI 历史检索面板数据源；无压缩栈时尾部扫描兜底）。
	SearchHistory(context.Context, string, int) (seelexctxsearch.Result, error)
}

// TeamworkBoardProjection 是**窄可选**能力面：Runtime 提供某会话的团队看板只读投影
// （计划 + 作业行 + 审计流水）。
//
// 为什么是"可选窄接口 + 类型断言"而不是 RuntimePort 的成员：RuntimePort 是"每个后端
// 都必须给出"的能力面，往里加一个成员会逼所有 fake / harness 长出空方法；而团队看板只在
// 装配了 teamwork 的宿主上有意义。口径与 context_runtime.CompactionIndexPort 一致：
// **有就有、没有就是没装配**——未装配时投影留空，前端整块退场（不留空壳）。
//
// 契约：docs/arch/team-board-gui-tui-contract.md §3。
type TeamworkBoardProjection interface {
	// TeamworkBoardSnapshot 返回该会话的团队看板只读投影。未装配 teamwork、
	// 解析不出会话作用域、或该会话尚无计划时返回 nil。
	TeamworkBoardSnapshot(sessionID string) *dto.TeamworkBoardView
}

// TeamworkJobCompletion 是**窄可选**能力面：teammate 作业（Seele jobs.Manager 里的
// worker 作业）的终态读数 + 变化信号口。
//
// 为什么需要它（2026-10-04 现场：teammate 干完了，leader 上下文里没有任何回声）：
// 后台作业的"做完自动返回"只在**一张表**上接好了——tools 的登记表（bash_bg / read_batch
// / subagent，`AsyncRunsSnapshot` + `AsyncRunEvents`，见 async_completion.go）。teammate
// 作业活在 jobs.Manager（另一张表），既不在那个投影里、也不在它的信号口上，于是同一件事
// （跑完了）在两条链上只有一条会被唤醒。
//
// 两个成员的分工与 tools 那对完全同形：Completions 是**只读全量**（终态判据在消费方），
// Events 是"有事发生"的信号（不进上下文、不推进任何游标）——消费方被唤醒后重读全量。
type TeamworkJobCompletion interface {
	// TeamworkJobCompletions 返回在册 teammate 作业的只读投影（按在册顺序）。
	// 未装配 teamwork 的宿主返回 nil。
	TeamworkJobCompletions() []dto.TeamworkJobCompletionRecord
	// TeamworkJobEvents 返回 teammate 作业表的变化信号口（派发 / 终态 / 新字节）。
	// 未装配时返回 nil（消费方的 select 会忽略 nil 通道）。
	TeamworkJobEvents() <-chan struct{}
}

// TeammateSessionProjection 是**窄可选**能力面：**当前 teammate 会话**的实时只读投影
// （"这件事的会话此刻在说什么"）。
//
// 为什么需要它（2026-10-04 用户口径：看板点开的要是"当前的 teammate 的会话"，不是员工的
// 长期历史会话）：员工的角色会话落盘、可回读；而一个 Work Item 自己的会话是**进程内
// 执行面**（刻意不接 DurableHistory），正文不在会话库里。于是"查看这件事的会话"这件事
// 只能从这个读面来——会话库里没有它的正文，读出来的只会是主会话的历史（看起来像"全是
// 历史会话"）。
type TeammateSessionProjection interface {
	// TeammateSessionLive 返回该会话的执行面实时读数。会话不在本进程里
	// （重启过 / 从未开过）时返回 Running=false 的视图，而不是 nil——调用方要能
	// 区分"没有这个会话"与"这个会话此刻是空的"。
	TeammateSessionLive(sessionID string) dto.TeammateSessionLiveView
}

type PluginPort interface {
	All() []model.PluginInfo
	Activate(context.Context, string) error
	Deactivate(context.Context) error
	Current() (model.PluginInfo, bool)
}
type SkillPort interface {
	All() []model.SkillInfo
	Get(string) (model.SkillInfo, bool)
}
type SessionPort interface {
	SaveCurrent(string) error
	Delete(string) error
	List() []model.SessionInfo
	LoadHistory(string) ([]EngineMessage, error)
	// LoadHistoryRange 按偏移量窗口加载，返回 [offset, offset+limit) 和总数。
	LoadHistoryRange(sessionID string, offset, limit int) ([]EngineMessage, int, error)
	// SetWorkspace routes subsequent Save/Load/List to the workspace directory.
	SetWorkspace(workspaceID string)
	// Workspace returns the active workspace ID ("" = default).
	Workspace() string
}

// RoleSessionPort 是 R2/R4 群聊角色会话与 role draft 的应用契约（S27 收口：
// 只用 application/contract/dto 纯 DTO，不出现存储类型）。
//
// 装配口径：会话端口实现（internal/adapters.SessionPort）在实现 SessionPort 的
// 同时实现本接口；`application/core` 用类型断言发现它，未装配时显式返回“不可用”，
// 不静默退化、不新增旁路。真正的排序、幂等、同步即删与 message head 发布仍由
// 存储侧 sequencer 入口执行，本端口只做窄转发。
type RoleSessionPort interface {
	CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error)
	AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error
	ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error)
	SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error)
	AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error
	ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error)
	RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error)
	AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error)
	SetLifecycleOrder(sessionID, policy string, roles []string) error
	SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error
	ListRoleSessions(mainSessionID string) ([]string, error)
}

// SchedulePort 是定时任务式插话 EVENT 的应用契约（§8.3）：注册/取消/触发都写
// `schedule.*` 结构性事件，冷启动按其重放重建 timer。触发本身由运行期执行，
// 本端口只落事件、不做调度。
type SchedulePort interface {
	ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error
	ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error
	ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error
}

type WorkspacePort interface {
	Create(name, rootPath, gitRemote string) (model.WorkspaceInfo, error)
	Get(id string) (model.WorkspaceInfo, error)
	List() []model.WorkspaceInfo
	Delete(id string) error
	BindSession(sessionID, workspaceID string)
	UnbindSession(sessionID string)
	SessionWorkspace(sessionID string) (model.WorkspaceInfo, bool)
	AllBindings() map[string]string
	DetectGitRemote(rootPath string) string
}

// ApprovalBroker 是异步审批的窄契约（实现：application/approval.ApprovalBroker）。
// 合约层只依赖该接口，装配根负责注入具体实现，便于替换审批机制或注入测试桩。
type ApprovalBroker interface {
	// SetObserver 注册审批开/结观察回调（波 4 会话级归属：open →
	// (sessionID, requestID, interaction)，close → (sessionID, requestID,
	// nil)；requestID 区分同会话多笔待批；sessionID 空 = 进程级/无会话
	// 审批）。
	SetObserver(observer func(sessionID, requestID string, interaction *model.Interaction))
	// SetPermissionAutoApproval 进程级权限自动放行（无会话归属/legacy 面）。
	SetPermissionAutoApproval(on bool)
	// SetPermissionAutoApprovalFor 会话级权限自动放行（GUI 全权按钮）：
	// 只放行指定会话的权限请求，不替其它会话放行（跨会话污染）。
	SetPermissionAutoApprovalFor(sessionID string, on bool)
	Request(ctx context.Context, request approval.ApprovalRequest) (approval.ApprovalDecision, error)
	Resolve(id string, decision approval.ApprovalDecision) error
	// ResolveAll 结掉无会话归属（进程级/legacy）的待批审批。
	ResolveAll(decision approval.ApprovalDecision) int
	// ResolveAllFor 结掉指定会话当前全部待批审批（会话级全权放行用；
	// 别的会话的待批不在此列）。
	ResolveAllFor(sessionID string, decision approval.ApprovalDecision) int
	// Pending 返回待批审批的会话归属快照（awaiting_approval/镜像补取用）。
	Pending() []approval.PendingApproval
	// PendingBySession 返回指定会话的待批审批（会话激活镜像用）。
	PendingBySession(sessionID string) []model.Interaction
	Shutdown()
}

// RolePromptPort 是「角色提示词一次性优化」的可选能力面（员工入职面板的
// "提示词优化"按钮）。它是**不写会话消息**的一次有界 LLM 回合：只把候选文本
// 返回给调用方，落盘仍走角色注册表（用户点"入职/保存"才写）。
//
// 未装配（nil）时应用层返回可展示错误，不静默返回原文。
type RolePromptPort interface {
	OptimizeRolePrompt(ctx context.Context, req dto.RolePromptOptimizeRequest) (dto.RolePromptOptimizeResult, error)
}

type Dependencies struct {
	Engine     ChatEngine
	Runtime    RuntimePort
	Plugins    PluginPort
	Skills     SkillPort
	Sessions   SessionPort
	Workspace  WorkspacePort
	Events     event.Hub
	Approval   ApprovalBroker
	RolePrompt RolePromptPort
	// EmployeePermissions 是"装配员工时分配员工权限"的写面：员工权限与用户权限
	// 走同一张权责表（主体 × 路由组 × 位），区别只是主体名（emp_<角色名>）。
	// 未装配（nil）时装配仍成功，只是不写员工权限条目（判定按档位默认派生）。
	EmployeePermissions EmployeePermissionPort
}

// EmployeePermissionPort 把"装配团队时写入的员工角色"翻译成**员工权限分配**。
//
// 为什么放在装配期：员工是在装配时进入团队的（这一刻才知道"谁在编、什么权责"），
// 权限分配必须与它同一个动作——否则就会出现"已经在编、权限还没分配"的窗口，
// 该窗口里员工回合按什么判都没有依据。实现方（seelebridge）把每个员工写成一个
// 主体条目，与用户权限同表同形。
type EmployeePermissionPort interface {
	AssignEmployeePermissions(roles []dto.RoleSpec) error
}
