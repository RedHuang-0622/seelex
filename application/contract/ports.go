// Package contract defines the application-owned interfaces for external systems.
package contract

import (
	"context"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/approval"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelebridge"
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
type ChatEngine interface {
	ChatStream(context.Context, string, func(string)) (string, error)
	History() []EngineMessage
	ClearHistory()
	ReplaceHistory(string, []EngineMessage) error
	SessionID() string
	StartSession() string
	SetSystemPrompt(string)
	SetMaxLoops(int)
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
	NodeWorktreeInfoFor(nodeID string) (seelebridge.NodeWorktreeInfo, bool)
	// SubAgentTree 返回 fork 子代理树的只读投影（内存态，不落盘；
	// GUI 树视图数据源，经权威 Snapshot 增量携带）。
	SubAgentTree() []seelebridge.SubAgentTreeNode
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
	SetRuntimeVisibilityProjection(seelebridge.RuntimeVisibilityProjection)
	SetParentEvidenceProjection(seelebridge.ParentEvidenceProjection)
	DrainSubagentContexts() []string
	SetPlanPolicy(seelebridge.PlanPolicy)
	PrepareReplan(context.Context, seelebridge.ReplanRequest) (seelebridge.PlanPreflight, error)
	ReplanMetrics() seelebridge.ReplanMetrics
	SetPlanBranchBinding(seelebridge.PlanBranchBinding)
	BindProjectRoot(rootPath string) error
	UnbindProjectRoot()
	// TodoSnapshot 返回当前 todolist 清单只读拷贝（GUI 待办面板数据源）。
	TodoSnapshot() []seelebridge.TodoItem
	// SetTodoStatus 设置待办项三态（pending/doing/done；GUI 工作表格状态
	// 更新入口；越界/非法状态返回错误）。
	SetTodoStatus(index int, status seelebridge.TodoItemStatus) error
	// TaskSnapshot 返回 task 注册表只读快照（worktable 投影数据源）。
	TaskSnapshot() []seelebridge.TaskRecord
	// TaskAdd 主动登记 task（幂等：Key 命中返回既有记录）。
	TaskAdd(spec seelebridge.TaskSpec) (seelebridge.TaskRecord, bool, error)
	// ResolveTaskByKey 按幂等键查 task（子代理装配现成 task_id 用）。
	ResolveTaskByKey(key string) (seelebridge.TaskRecord, bool, error)
	// TaskSetStatus 更新 task 状态（retry 自增计数）。
	TaskSetStatus(id string, status seelebridge.TaskStatus, evidence string) (seelebridge.TaskRecord, error)
	// TaskAttachParticipant 把子代理挂为 task 参与者（幂等）。
	TaskAttachParticipant(id, participant string) (seelebridge.TaskRecord, error)
	// TaskChangedChannel 返回 task.changed 输出 channel（CSP：变更即投递，
	// application 消费者直发增量，不拉脏）。
	TaskChangedChannel() <-chan seelebridge.TaskRecord
	// SubagentTreeEvents 返回子代理树生命周期信号 channel（CSP 消费者刷新
	// 工作表格；取代同步回调 observer）。
	SubagentTreeEvents() <-chan struct{}
	// PlanNodeEventChannel 返回 plan 节点事件 channel（CSP 消费者串行处理；
	// 取代同步回调）。
	PlanNodeEventChannel() <-chan seelebridge.PlanNodeEvent
	// SwitchSessionTasks 会话级 task 隔离：切换会话时整体替换注册表
	// （清空当前会话 task，恢复目标会话 task；复用 session stack 存储）。
	SwitchSessionTasks(records []seelebridge.TaskRecord)
	// ScheduledCommands 返回定时周期任务白名单命令展示信息（GUI 新建弹窗数据源）。
	ScheduledCommands() []seelebridge.ScheduledCommandInfo
	// ScheduledTasksSnapshot 返回周期任务只读快照（GUI 定时任务面板数据源）。
	ScheduledTasksSnapshot() []seelebridge.ScheduledTaskStatus
	// ScheduleTask 创建并启动一个周期任务（变更入口；Runtime 调度器执行）。
	ScheduleTask(context.Context, seelebridge.ScheduledTaskSpec) (*seelebridge.ScheduledTaskStatus, error)
	// CancelScheduledTask 取消并移除周期任务。
	CancelScheduledTask(string) error
	// ClearSubagentTree 清空子代理树（GUI「清空」入口；失败节点显式清走，
	// 详情数据面不受影响）。
	ClearSubagentTree() error
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

type Dependencies struct {
	Engine    ChatEngine
	Runtime   RuntimePort
	Plugins   PluginPort
	Skills    SkillPort
	Sessions  SessionPort
	Workspace WorkspacePort
	Events    *event.EventHub
	Approval  *approval.ApprovalBroker
}
