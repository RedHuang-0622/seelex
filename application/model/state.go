// Package model defines the versioned application DTOs shared with clients.
package model

import (
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge"
	seelplan "github.com/RedHuang-0622/seelex/seelebridge/plan"
)

// ProtocolVersion identifies the Snapshot/Event contract consumed by frontends.
const ProtocolVersion = 1

type Snapshot struct {
	ProtocolVersion    int               `json:"protocol_version"`
	Revision           uint64            `json:"revision"`
	Session            SessionState      `json:"session"`
	Sessions           []SessionInfo     `json:"sessions"`
	Conversation       []Message         `json:"conversation"`
	Chat               ChatState         `json:"chat"`
	Task               *TaskState        `json:"task,omitempty"`
	Runtime            RuntimeState      `json:"runtime"`
	Interaction        *Interaction      `json:"interaction,omitempty"`
	Capabilities       Capabilities      `json:"capabilities"`
	HistoryOffset      int               `json:"history_offset"`
	TotalMessages      int               `json:"total_messages"`
	HasMoreHistory     bool              `json:"has_more_history"`
	ConversationWindow int               `json:"conversation_window"`
	ReadFiles          []ReadFileRef     `json:"read_files,omitempty"`
	Workspaces         []WorkspaceInfo   `json:"workspaces,omitempty"`
	CurrentWorkspace   *WorkspaceInfo    `json:"current_workspace,omitempty"`
	SessionWorkspaces  map[string]string `json:"session_workspaces,omitempty"`
}

// TaskState is the user-visible, evidence-oriented outcome of the latest
// request. It intentionally describes execution state rather than model
// internals or prompt content.
type TaskState struct {
	RequestID          string              `json:"request_id,omitempty"`
	Status             TaskStatus          `json:"status"`
	Summary            string              `json:"summary,omitempty"`
	ContextCompactions []ContextCompaction `json:"context_compactions,omitempty"`
	UpdatedAt          time.Time           `json:"updated_at,omitempty"`
}

// ContextCompaction is a user-visible record that the active provider context
// was condensed. It intentionally contains no prompt text, checkpoint body,
// tool argument, tool result, or conversation content.
type ContextCompaction struct {
	Version         uint64    `json:"version"`
	Reason          string    `json:"reason"`
	MessagesBefore  int       `json:"messages_before"`
	EstimatedTokens int       `json:"estimated_tokens"`
	CompactedAt     time.Time `json:"compacted_at"`
}

type TaskStatus string

const (
	TaskProgressing       TaskStatus = "progressing"
	TaskCompleted         TaskStatus = "completed"
	TaskNeedsUserDecision TaskStatus = "needs_user_decision"
	TaskBlocked           TaskStatus = "blocked"
	TaskInterrupted       TaskStatus = "interrupted"
	TaskFailed            TaskStatus = "failed"
)

type SessionState struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Draft bool   `json:"draft,omitempty"`
}
type Message struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content,omitempty"`
	Tool      *ToolCall `json:"tool,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
type ToolCall struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Arguments string        `json:"arguments,omitempty"`
	Result    string        `json:"result,omitempty"`
	Error     string        `json:"error,omitempty"`
	Status    string        `json:"status"`
	Duration  time.Duration `json:"duration,omitempty"`
}
type ChatState struct {
	Running     bool      `json:"running"`
	RequestID   string    `json:"request_id,omitempty"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	Error       string    `json:"error,omitempty"`
	QueuedCount int       `json:"queued_count"`          // 排队中的输入数
	InputQueue  []string  `json:"input_queue,omitempty"` // 排队消息内容（TUI 显示用）
}
type RuntimeState struct {
	Model             string                             `json:"model"`
	Provider          string                             `json:"provider"`
	Account           string                             `json:"account,omitempty"`
	Plugin            string                             `json:"plugin,omitempty"`
	Effort            string                             `json:"effort"`
	FullAccess        bool                               `json:"full_access"`
	VisibleTools      []Tool                             `json:"visible_tools"`
	Skills            []SkillInfo                        `json:"skills"`
	Tokens            string                             `json:"tokens"`
	Replan            ReplanMonitor                      `json:"replan"`
	Plan              *PlanState                         `json:"plan,omitempty"`
	Plugins           []PluginInfo                       `json:"plugins,omitempty"`            // 完整插件列表（含描述）
	Accounts          []AccountInfo                      `json:"accounts,omitempty"`           // 账号池
	TodoItems         []dto.TodoItem                     `json:"todo_items,omitempty"`         // todolist 清单（GUI 待办面板）
	ScheduledTasks    []seelebridge.ScheduledTaskStatus  `json:"scheduled_tasks,omitempty"`    // 定时周期任务（GUI 定时任务面板）
	ScheduledCommands []seelebridge.ScheduledCommandInfo `json:"scheduled_commands,omitempty"` // 白名单命令（新建弹窗下拉）
	// SubAgentTree 是 fork 子代理树的权威投影（内存态，不落盘；GUI 树视图
	// 数据源）。节点状态由 fork 子代理会话生命周期投影，随节点事件增量刷新。
	SubAgentTree []dto.SubAgentTreeNode `json:"subagent_tree,omitempty"`
	// WorkTable 是工作台统一工作表格的权威投影（plan 节点 / todolist 项 /
	// fork 子代理 → 扁平 WorkItem 行，含任务打点 trace）。有界（行数上限
	// limits.work_table_rows，trace 上限 limits.plan_node_events）。
	WorkTable []WorkItem `json:"work_table,omitempty"`
}

// ReplanMonitor exposes bounded recovery-planning usage without exposing
// request content or provider credentials.
// ReadFileRef is the compact, persistent cache of files successfully read by
// the agent in this session. It records provenance, not file content.
type ReadFileRef struct {
	Path   string    `json:"path"`
	ReadAt time.Time `json:"read_at,omitempty"`
}

// SessionTitle is stable session metadata. It is stored under the session's
// unique persistence key and is never reconstructed from mutable chat history.
type SessionTitle struct {
	Value        string    `json:"value"`
	Source       string    `json:"source,omitempty"`
	FinalizedAt  time.Time `json:"finalized_at,omitempty"`
	UserEditedAt time.Time `json:"user_edited_at,omitempty"`
}

// ConversationRecord is the application-visible, append-only conversation
// projection. It is intentionally distinct from provider history, which is an
// execution cache and may be compacted or replaced.
type ConversationRecord struct {
	Messages  []Message `json:"messages,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// SessionPlanFrame preserves one loaded Plan revision. ActivePlanID identifies
// the frame whose Plan is currently executable; older frames remain evidence
// for recovery and workbench history.
type SessionPlanFrame struct {
	ID        string     `json:"id"`
	Plan      *PlanState `json:"plan,omitempty"`
	Arguments string     `json:"arguments,omitempty"`
	LoadedAt  time.Time  `json:"loaded_at,omitempty"`
	UpdatedAt time.Time  `json:"updated_at,omitempty"`
}

// SessionExecutionRecord groups non-conversation execution data that survives
// provider-history compaction.
type SessionExecutionRecord struct {
	Task         *TaskState    `json:"task,omitempty"`
	ReadFiles    []ReadFileRef `json:"read_files,omitempty"`
	Continuation string        `json:"continuation,omitempty"`
}

// SessionRecord is the durable backend record addressed by
// (workspace_id, session_id). It is the source of truth for stable metadata,
// Plan revisions, and visible conversation; framework history is not.
type SessionRecord struct {
	Version      int                `json:"version"`
	ID           string             `json:"id"`
	Title        SessionTitle       `json:"title"`
	ActivePlanID string             `json:"active_plan_id,omitempty"`
	PlanStack    []SessionPlanFrame `json:"plan_stack,omitempty"`
	// Tasks 是 task 注册表快照（worktable 条目；复用 session stack 持久化，
	// 与 PlanStack 同一 immutable 存储通道，T4）。
	Tasks        []dto.TaskRecord       `json:"tasks,omitempty"`
	Conversation ConversationRecord     `json:"conversation"`
	Execution    SessionExecutionRecord `json:"execution"`
	Projection   *TaskContextProjection `json:"projection,omitempty"`
	Checkpoints  []TaskCheckpoint       `json:"checkpoints,omitempty"`
	ToolResults  []ToolResultRef        `json:"tool_results,omitempty"`
	UpdatedAt    time.Time              `json:"updated_at,omitempty"`
}

// SessionArchive is the v1 sidecar shape retained only for migration. New
// writes use SessionRecord, whose title, Plan stack, and conversation record
// are explicit independent components.
type SessionArchive struct {
	Version       int           `json:"version"`
	Name          string        `json:"name,omitempty"`
	Conversation  []Message     `json:"conversation,omitempty"`
	Task          *TaskState    `json:"task,omitempty"`
	Plan          *PlanState    `json:"plan,omitempty"`
	PlanArguments string        `json:"plan_arguments,omitempty"`
	ReadFiles     []ReadFileRef `json:"read_files,omitempty"`
	Continuation  string        `json:"continuation,omitempty"`
	UpdatedAt     time.Time     `json:"updated_at,omitempty"`
}

type ReplanMonitor struct {
	InFlight               int       `json:"in_flight"`
	ConcurrentLimit        int       `json:"concurrent_limit"`
	WindowAttempts         int       `json:"window_attempts"`
	WindowLimit            int       `json:"window_limit"`
	WindowStartedAt        time.Time `json:"window_started_at,omitempty"`
	Accepted               uint64    `json:"accepted"`
	Succeeded              uint64    `json:"succeeded"`
	Failed                 uint64    `json:"failed"`
	Rejected               uint64    `json:"rejected"`
	DuplicateRejected      uint64    `json:"duplicate_rejected"`
	ProviderRequests       uint64    `json:"provider_requests"`
	ProviderWindowRequests int       `json:"provider_window_requests"`
	ProviderWindowLimit    int       `json:"provider_window_limit"`
}

// ── Plan 可视化 ────────────────────────────────────────────

// PlanState 描述当前 WorkPlan 的执行状态（nil = 无活跃 Plan）。
type PlanState struct {
	Name        string              `json:"name"`
	EntryNodeID string              `json:"entry_node_id"`
	Status      PlanStatus          `json:"status"`
	Nodes       []PlanNode          `json:"nodes,omitempty"`
	Edges       []seelplan.PlanEdge `json:"edges,omitempty"`
	Progress    float64             `json:"progress"`
	Elapsed     string              `json:"elapsed,omitempty"`
	ReplanCount int                 `json:"replan_count,omitempty"`
}

type PlanStatus string

const (
	PlanPending   PlanStatus = "pending"
	PlanRunning   PlanStatus = "running"
	PlanCompleted PlanStatus = "completed"
	PlanFailed    PlanStatus = "failed"
	PlanAborted   PlanStatus = "aborted"
)

type PlanNode struct {
	ID         string              `json:"id"`
	Label      string              `json:"label"`
	Kind       string              `json:"kind"`
	Status     NodeStatus          `json:"status"`
	Depth      int                 `json:"depth,omitempty"`  // 缩进层级（0 = 根）
	Output     string              `json:"output,omitempty"` // 节点输出内容
	Elapsed    string              `json:"elapsed,omitempty"`
	Events     []PlanNodeEventInfo `json:"events,omitempty"`      // 节点事件时间线（详情页）
	ToolEvents []SubagentToolEvent `json:"tool_events,omitempty"` // 子代理工具活动（有界）
	Children   []PlanNode          `json:"children,omitempty"`    // Fork 子节点
}

// PlanNodeEventInfo 是节点事件时间线的一条记录（子代理详情页数据源）：
// queued → running（可含心跳刷新）→ 终态，含时间戳与输出快照。
type PlanNodeEventInfo struct {
	Status NodeStatus `json:"status"`
	At     time.Time  `json:"at"`
	Output string     `json:"output,omitempty"`
}

// SubagentDetail 是子代理详情弹窗的数据载荷（会话记录 + 状态/耗时/输出）。
// Conversation 经应用层适配截断（单条 ≤ evidence_chars、总 ≤ 50 条）。
type SubagentDetail struct {
	Conversation []Message           `json:"conversation,omitempty"`
	ToolEvents   []SubagentToolEvent `json:"tool_events,omitempty"`
	Context      *SubagentContext    `json:"context,omitempty"`
	// Worktree 是节点 worktree 现场（失败/合并被拒时保留，Path 即人工恢复
	// 入口；成功路径已清理 → nil）。
	Worktree *SubagentWorktreeInfo `json:"worktree,omitempty"`
	Running  bool                  `json:"running"`
	Status   NodeStatus            `json:"status"`
	Elapsed  string                `json:"elapsed,omitempty"`
	Output   string                `json:"output,omitempty"`
}

// SubagentWorktreeInfo 是节点 worktree 现场的只读摘要（详情弹窗"工作区"
// 数据源）。节点失败/被拒时文件保留在 Path，分支改动仍可 git merge 恢复。
type SubagentWorktreeInfo struct {
	Path       string `json:"path,omitempty"`
	Branch     string `json:"branch,omitempty"`
	MainBranch string `json:"main_branch,omitempty"`
}

// SubagentContext 是子代理运行过程的结构化上下文快照（详情弹窗"上下文"
// 标签数据源）。只暴露可公开的证据面：目标/进度/发现/决策/约束/待办/
// 消息数/token 估算；不含 prompt 原文、工具参数或秘密。
type SubagentContext struct {
	Goal          string                    `json:"goal,omitempty"`
	Progress      string                    `json:"progress,omitempty"`
	MessageCount  int                       `json:"message_count"`
	TokenEstimate int                       `json:"token_estimate,omitempty"`
	Findings      []string                  `json:"findings,omitempty"`
	Decisions     []SubagentContextDecision `json:"decisions,omitempty"`
	Constraints   []string                  `json:"constraints,omitempty"`
	PendingWork   []string                  `json:"pending_work,omitempty"`
}

// ── 工作表格（Work Table）─────────────────────────────────

// WorkItem 是工作台工作表格的统一只读投影行：把 plan 节点、todolist 项与
// fork 子代理归一为同一张多维表格；Trace 是任务打点（有界，按时间倒序）。
type WorkItem struct {
	ID           string           `json:"id"`                     // 稳定键：plan:<id> | todo:<index> | subagent:<id>
	Phase        string           `json:"phase"`                  // plan | tasklist | subagent
	Task         string           `json:"task"`                   // 任务名/节点 label/goal
	Description  string           `json:"description,omitempty"`  // 描述/output 摘要
	Status       string           `json:"status"`                 // 权威状态（来源状态机）
	RetryCount   int              `json:"retry_count,omitempty"`  // 重试数字（RETRY n）
	Assignee     string           `json:"assignee,omitempty"`     // main | 子代理 id | 执行节点
	Dependencies []string         `json:"dependencies,omitempty"` // 前置任务（WorkItem ID 引用）
	Attachments  []string         `json:"attachments,omitempty"`  // 可选：worktree/read_file 路径
	Kind         string           `json:"kind"`                   // plan | todo | subagent
	SourceID     string           `json:"source_id,omitempty"`    // 原数据面 ID（详情溯源）
	Participants []string         `json:"participants,omitempty"` // 同一 task 的多个子代理（幂等去重后合并）
	StartedAt    time.Time        `json:"started_at,omitempty"`
	EndedAt      time.Time        `json:"ended_at,omitempty"`
	Elapsed      string           `json:"elapsed,omitempty"`
	Trace        []WorkTracePoint `json:"trace,omitempty"`
}

// WorkTracePoint 是任务打点（操作/状态/时间/证据；evidence 已截断）。
type WorkTracePoint struct {
	At        time.Time `json:"at,omitempty"`
	Status    string    `json:"status"`
	Operation string    `json:"operation,omitempty"` // node.lifecycle | task_check_node | tool 名 | subagent.lifecycle
	Evidence  string    `json:"evidence,omitempty"`
	Duration  string    `json:"duration,omitempty"`
}

// WorkTableEvent 是 worktable.changed 增量的 payload（只含表格，不整份 runtime）。
type WorkTableEvent struct {
	Items []WorkItem `json:"items"`
}

// TaskChangedEvent 是 task.changed 增量的 payload：单个 task 的内部变更
// （状态/打点/retry），Task 是 WorkItem 同构快照。
type TaskChangedEvent struct {
	TaskID string   `json:"task_id"`
	Task   WorkItem `json:"task"`
}

// CloneWorkItems 返回工作表格行的深拷贝（并发读者安全；供快照克隆与事件发布）。
func CloneWorkItems(items []WorkItem) []WorkItem {
	if len(items) == 0 {
		return nil
	}
	cloned := append([]WorkItem(nil), items...)
	for index := range cloned {
		cloned[index].Dependencies = append([]string(nil), items[index].Dependencies...)
		cloned[index].Attachments = append([]string(nil), items[index].Attachments...)
		cloned[index].Participants = append([]string(nil), items[index].Participants...)
		cloned[index].Trace = append([]WorkTracePoint(nil), items[index].Trace...)
	}
	return cloned
}

// SubagentContextDecision 是子代理关键决策（What/Why）。
type SubagentContextDecision struct {
	What string `json:"what"`
	Why  string `json:"why,omitempty"`
}

// SubagentEvent 是节点生命周期的前端增量载荷。Node 是权威 Snapshot
// 中该节点的完整有界投影；PlanStatus/Progress 让 reducer 无需整份重载。
type SubagentEvent struct {
	PlanID     string     `json:"plan_id,omitempty"`
	RunID      string     `json:"run_id,omitempty"`
	NodeID     string     `json:"node_id"`
	Node       PlanNode   `json:"node"`
	PlanStatus PlanStatus `json:"plan_status"`
	Progress   float64    `json:"progress"`
}

// SubagentToolEvent 是子代理内部工具调用的有界活动投影。
type SubagentToolEvent struct {
	ID        string        `json:"id"`
	NodeID    string        `json:"node_id"`
	Name      string        `json:"name"`
	Arguments string        `json:"arguments,omitempty"`
	Result    string        `json:"result,omitempty"`
	Error     string        `json:"error,omitempty"`
	Status    string        `json:"status"`
	StartedAt time.Time     `json:"started_at,omitempty"`
	Duration  time.Duration `json:"duration,omitempty"`
}

type NodeStatus string

const (
	NodePending          NodeStatus = "pending"
	NodeQueued           NodeStatus = "queued"
	NodeRunning          NodeStatus = "running"
	NodeWorktreeCreating NodeStatus = "worktree_creating"
	NodeRebasing         NodeStatus = "rebasing"
	NodeMerging          NodeStatus = "merging"
	NodeCompleted        NodeStatus = "completed"
	NodeFailed           NodeStatus = "failed"
	NodeAborted          NodeStatus = "aborted"
	NodeSkipped          NodeStatus = "skipped"
	NodeCanceled         NodeStatus = "canceled"
	NodePanicked         NodeStatus = "panicked"
)

type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}
type SkillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"-"`
}
type PluginInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Prompt      string `json:"-"`
}
type AccountInfo struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Disabled bool   `json:"disabled"`
}

// WorkspaceInfo is the minimal workspace summary carried by application state.
type WorkspaceInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	RootPath  string `json:"root_path"`
	GitRemote string `json:"git_remote,omitempty"`
}

type SessionInfo struct {
	ID         string    `json:"id"`
	Name       string    `json:"name,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
	TokenCount int       `json:"token_count"`
}
type Interaction struct {
	ID       string              `json:"id"`
	Kind     string              `json:"kind"`
	Title    string              `json:"title"`
	Question string              `json:"question,omitempty"`
	Risk     string              `json:"risk,omitempty"`
	ToolName string              `json:"tool_name,omitempty"`
	Preview  string              `json:"preview,omitempty"`
	Options  []InteractionOption `json:"options"`
	OpenedAt time.Time           `json:"opened_at"`
	Timeout  time.Duration       `json:"timeout,omitempty"`
}
type InteractionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Style       string `json:"style,omitempty"`
}
type Capabilities struct {
	SessionResume       bool   `json:"session_resume"`
	SessionResumeReason string `json:"session_resume_reason,omitempty"`
}

// CloneSnapshot returns an independent copy suitable for concurrent readers.
func CloneSnapshot(snapshot Snapshot) Snapshot {
	copySnapshot := snapshot
	copySnapshot.Sessions = append([]SessionInfo(nil), snapshot.Sessions...)
	if snapshot.SessionWorkspaces != nil {
		copySnapshot.SessionWorkspaces = make(map[string]string, len(snapshot.SessionWorkspaces))
		for sessionID, workspaceID := range snapshot.SessionWorkspaces {
			copySnapshot.SessionWorkspaces[sessionID] = workspaceID
		}
	}
	if snapshot.CurrentWorkspace != nil {
		workspace := *snapshot.CurrentWorkspace
		copySnapshot.CurrentWorkspace = &workspace
	}
	copySnapshot.Conversation = append([]Message(nil), snapshot.Conversation...)
	copySnapshot.ReadFiles = append([]ReadFileRef(nil), snapshot.ReadFiles...)
	if snapshot.Task != nil {
		task := *snapshot.Task
		task.ContextCompactions = append([]ContextCompaction(nil), snapshot.Task.ContextCompactions...)
		copySnapshot.Task = &task
	}
	if copySnapshot.Conversation == nil {
		copySnapshot.Conversation = []Message{} // 确保 JSON 序列化为 [] 而非 null
	}
	// 标量字段 (HistoryOffset, TotalMessages, HasMoreHistory) 已值拷贝
	for index := range copySnapshot.Conversation {
		if copySnapshot.Conversation[index].Tool != nil {
			tool := *copySnapshot.Conversation[index].Tool
			copySnapshot.Conversation[index].Tool = &tool
		}
	}
	copySnapshot.Runtime = CloneRuntimeState(snapshot.Runtime)
	if snapshot.Interaction != nil {
		interaction := *snapshot.Interaction
		interaction.Options = append([]InteractionOption(nil), snapshot.Interaction.Options...)
		copySnapshot.Interaction = &interaction
	}
	return copySnapshot
}

// CloneRuntimeState returns an independent copy of mutable runtime slices.
func CloneRuntimeState(runtime RuntimeState) RuntimeState {
	copyRuntime := runtime
	copyRuntime.VisibleTools = append([]Tool(nil), runtime.VisibleTools...)
	copyRuntime.Skills = append([]SkillInfo(nil), runtime.Skills...)
	copyRuntime.Plugins = append([]PluginInfo(nil), runtime.Plugins...)
	copyRuntime.Accounts = append([]AccountInfo(nil), runtime.Accounts...)
	copyRuntime.TodoItems = append([]dto.TodoItem(nil), runtime.TodoItems...)
	copyRuntime.ScheduledTasks = append([]seelebridge.ScheduledTaskStatus(nil), runtime.ScheduledTasks...)
	copyRuntime.ScheduledCommands = append([]seelebridge.ScheduledCommandInfo(nil), runtime.ScheduledCommands...)
	if runtime.Plan != nil {
		planCopy := *runtime.Plan
		planCopy.Nodes = clonePlanNodes(runtime.Plan.Nodes)
		planCopy.Edges = append([]seelplan.PlanEdge(nil), runtime.Plan.Edges...)
		copyRuntime.Plan = &planCopy
	}
	copyRuntime.SubAgentTree = cloneSubAgentTree(runtime.SubAgentTree)
	copyRuntime.WorkTable = CloneWorkItems(runtime.WorkTable)
	return copyRuntime
}

// cloneSubAgentTree 深拷贝子代理树投影（快照克隆不改旧快照的契约）。
func cloneSubAgentTree(nodes []dto.SubAgentTreeNode) []dto.SubAgentTreeNode {
	if len(nodes) == 0 {
		return nil
	}
	cloned := append([]dto.SubAgentTreeNode(nil), nodes...)
	for index := range cloned {
		cloned[index].Children = cloneSubAgentTree(nodes[index].Children)
	}
	return cloned
}

func clonePlanNodes(nodes []PlanNode) []PlanNode {
	if len(nodes) == 0 {
		return nil
	}
	cloned := append([]PlanNode(nil), nodes...)
	for index := range cloned {
		cloned[index].Children = clonePlanNodes(nodes[index].Children)
		if nodes[index].Events != nil {
			cloned[index].Events = append([]PlanNodeEventInfo(nil), nodes[index].Events...)
		}
		if nodes[index].ToolEvents != nil {
			cloned[index].ToolEvents = append([]SubagentToolEvent(nil), nodes[index].ToolEvents...)
		}
	}
	return cloned
}
