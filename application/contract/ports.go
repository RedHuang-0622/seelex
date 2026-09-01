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
	// 仅在 OnIterationComplete 回调（ChatStream 同 goroutine）中调用，不加锁。
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
type RuntimePort interface {
	Model() string
	Provider() string
	Accounts() []model.AccountInfo
	SelectAccount(string) bool
	VisibleTools(context.Context) []model.Tool
	ActivePlugin() string
	FullAccess() bool
	SetFullAccess(bool)
	SetRuntimeVisibilityProjection(dto.RuntimeVisibilityProjection)
	SetParentEvidenceProjection(dto.ParentEvidenceProjection)
	DrainSubagentContexts() []string
	SetPlanPolicy(dto.PlanPolicy)
	PrepareReplan(context.Context, dto.ReplanRequest) (dto.PlanPreflight, error)
	ReplanMetrics() dto.ReplanMetrics
	SetPlanBranchBinding(dto.PlanBranchBinding)
	BindProjectRoot(rootPath string) error
	UnbindProjectRoot()
	// SetCurrentTaskBatch 设置会话级 task 注册表默认批次（startChat 写入
	// requestID；按会话保存，后台会话不覆盖活跃注册表默认批次——L3）。
	SetCurrentTaskBatch(sessionID, batchID string)
	// TodoSnapshot 返回当前 todolist 清单只读拷贝（GUI 待办面板数据源）。
	TodoSnapshot() []dto.TodoItem
	// SetTodoStatus 设置待办项三态（pending/doing/done；GUI 工作表格状态
	// 更新入口；越界/非法状态返回错误）。
	SetTodoStatus(index int, status dto.TodoItemStatus) error
	// TaskSnapshot 返回 task 注册表只读快照（worktable 投影数据源）。
	TaskSnapshot() []dto.TaskRecord
	// TaskSnapshotFor 返回指定会话的 task 注册表快照（会话持久化用；
	// 后台会话收尾不得读活跃注册表，对应 R6/P2）。
	TaskSnapshotFor(sessionID string) []dto.TaskRecord
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
	// SwitchSessionTasks 会话级 task 隔离：离开当前会话时保存其注册表
	// 快照，切换后整体替换为目标会话 task（复用 session stack 存储）。
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
	// SearchHistory 在会话压缩栈（语义索引）上检索历史聊天记录
	// （GUI 历史检索面板数据源；无压缩栈时尾部扫描兜底）。
	SearchHistory(context.Context, string, int) (seelexctxsearch.Result, error)
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
	SetObserver(observer func(*model.Interaction))
	SetPermissionAutoApproval(on bool)
	Request(ctx context.Context, request approval.ApprovalRequest) (approval.ApprovalDecision, error)
	Resolve(id string, decision approval.ApprovalDecision) error
	ResolveAll(decision approval.ApprovalDecision) int
	Shutdown()
}

type Dependencies struct {
	Engine    ChatEngine
	Runtime   RuntimePort
	Plugins   PluginPort
	Skills    SkillPort
	Sessions  SessionPort
	Workspace WorkspacePort
	Events    event.Hub
	Approval  ApprovalBroker
}
