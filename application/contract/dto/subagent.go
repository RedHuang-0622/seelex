package dto

import "time"

// SubAgentNodeStatus 是子代理树节点的生命周期状态（树投影专用）。
type SubAgentNodeStatus string

const (
	SubAgentQueued  SubAgentNodeStatus = "queued" // fork 派工但会话尚未启动
	SubAgentRunning SubAgentNodeStatus = "running"
	SubAgentDone    SubAgentNodeStatus = "done"
	SubAgentFailed  SubAgentNodeStatus = "failed"
)

// SubAgentTreeNode 是子代理树的只读投影节点（GUI 树视图数据源）。
type SubAgentTreeNode struct {
	ID        string               `json:"id"`
	ParentID  string               `json:"parent_id,omitempty"`
	Goal      string               `json:"goal,omitempty"`
	Status    SubAgentNodeStatus   `json:"status"`
	Summary   string               `json:"summary,omitempty"`
	Error     string               `json:"error,omitempty"`
	SessionID string               `json:"session_id,omitempty"`
	StartedAt time.Time            `json:"started_at,omitempty"`
	EndedAt   time.Time            `json:"ended_at,omitempty"`
	Context   *SubAgentNodeContext `json:"context,omitempty"`
	Children  []SubAgentTreeNode   `json:"children,omitempty"`
}

// SubAgentNodeContext 是树节点的紧凑上下文（ContextSnapshot 的有界投影）。
type SubAgentNodeContext struct {
	Goal          string   `json:"goal,omitempty"`
	Progress      string   `json:"progress,omitempty"`
	MessageCount  int      `json:"message_count"`
	TokenEstimate int      `json:"token_estimate,omitempty"`
	Findings      []string `json:"findings,omitempty"`
}
