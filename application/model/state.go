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
	Runtime           RuntimeState      `json:"runtime"`
	Interaction       *Interaction      `json:"interaction,omitempty"`
	Capabilities      Capabilities      `json:"capabilities"`
	HistoryOffset     int               `json:"history_offset"`
	TotalMessages     int               `json:"total_messages"`
	HasMoreHistory    bool              `json:"has_more_history"`
	Workspaces        []WorkspaceInfo   `json:"workspaces,omitempty"`
	CurrentWorkspace  *WorkspaceInfo    `json:"current_workspace,omitempty"`
	SessionWorkspaces map[string]string `json:"session_workspaces,omitempty"`
}

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
