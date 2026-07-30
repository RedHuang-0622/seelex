// Package model defines the versioned application DTOs shared with clients.
package model

import (
	"time"

	"github.com/RedHuang-0622/seelex/seelebridge"
)

// ProtocolVersion identifies the Snapshot/Event contract consumed by frontends.
const ProtocolVersion = 1

type Snapshot struct {
	ProtocolVersion   int               `json:"protocol_version"`
	Revision          uint64            `json:"revision"`
	Session           SessionState      `json:"session"`
	Sessions          []SessionInfo     `json:"sessions"`
	Conversation      []Message         `json:"conversation"`
	Chat              ChatState         `json:"chat"`
	Task              *TaskState        `json:"task,omitempty"`
	Runtime           RuntimeState      `json:"runtime"`
	Interaction       *Interaction      `json:"interaction,omitempty"`
	Capabilities      Capabilities      `json:"capabilities"`
	HistoryOffset     int               `json:"history_offset"`
	TotalMessages     int               `json:"total_messages"`
	HasMoreHistory    bool              `json:"has_more_history"`
	ReadFiles         []ReadFileRef     `json:"read_files,omitempty"`
	Workspaces        []WorkspaceInfo   `json:"workspaces,omitempty"`
	CurrentWorkspace  *WorkspaceInfo    `json:"current_workspace,omitempty"`
	SessionWorkspaces map[string]string `json:"session_workspaces,omitempty"`
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
	Model        string        `json:"model"`
	Provider     string        `json:"provider"`
	Account      string        `json:"account,omitempty"`
	Plugin       string        `json:"plugin,omitempty"`
	Effort       string        `json:"effort"`
	VisibleTools []Tool        `json:"visible_tools"`
	Skills       []SkillInfo   `json:"skills"`
	Tokens       string        `json:"tokens"`
	Replan       ReplanMonitor `json:"replan"`
	Plan         *PlanState    `json:"plan,omitempty"`
	Plugins      []PluginInfo  `json:"plugins,omitempty"`  // 完整插件列表（含描述）
	Accounts     []AccountInfo `json:"accounts,omitempty"` // 账号池
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
	Version      int                    `json:"version"`
	ID           string                 `json:"id"`
	Title        SessionTitle           `json:"title"`
	ActivePlanID string                 `json:"active_plan_id,omitempty"`
	PlanStack    []SessionPlanFrame     `json:"plan_stack,omitempty"`
	Conversation ConversationRecord     `json:"conversation"`
	Execution    SessionExecutionRecord `json:"execution"`
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
	Name        string                 `json:"name"`
	EntryNodeID string                 `json:"entry_node_id"`
	Status      PlanStatus             `json:"status"`
	Nodes       []PlanNode             `json:"nodes,omitempty"`
	Edges       []seelebridge.PlanEdge `json:"edges,omitempty"`
	Progress    float64                `json:"progress"`
	Elapsed     string                 `json:"elapsed,omitempty"`
	ReplanCount int                    `json:"replan_count,omitempty"`
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
	ID       string     `json:"id"`
	Label    string     `json:"label"`
	Kind     string     `json:"kind"`
	Status   NodeStatus `json:"status"`
	Depth    int        `json:"depth,omitempty"`  // 缩进层级（0 = 根）
	Output   string     `json:"output,omitempty"` // 节点输出内容
	Elapsed  string     `json:"elapsed,omitempty"`
	Children []PlanNode `json:"children,omitempty"` // Fork 子节点
}

type NodeStatus string

const (
	NodePending   NodeStatus = "pending"
	NodeQueued    NodeStatus = "queued"
	NodeRunning   NodeStatus = "running"
	NodeCompleted NodeStatus = "completed"
	NodeFailed    NodeStatus = "failed"
	NodeAborted   NodeStatus = "aborted"
	NodeSkipped   NodeStatus = "skipped"
	NodeCanceled  NodeStatus = "canceled"
	NodePanicked  NodeStatus = "panicked"
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
	if runtime.Plan != nil {
		planCopy := *runtime.Plan
		planCopy.Nodes = clonePlanNodes(runtime.Plan.Nodes)
		planCopy.Edges = append([]seelebridge.PlanEdge(nil), runtime.Plan.Edges...)
		copyRuntime.Plan = &planCopy
	}
	return copyRuntime
}

func clonePlanNodes(nodes []PlanNode) []PlanNode {
	if len(nodes) == 0 {
		return nil
	}
	cloned := append([]PlanNode(nil), nodes...)
	for index := range cloned {
		cloned[index].Children = clonePlanNodes(nodes[index].Children)
	}
	return cloned
}
