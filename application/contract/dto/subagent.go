package dto

import "time"

// SubAgentNodeStatus 是子代理树节点的生命周期状态（树投影专用）。
type SubAgentNodeStatus string

const (
	SubAgentQueued      SubAgentNodeStatus = "queued" // fork 派工但会话尚未启动
	SubAgentRunning     SubAgentNodeStatus = "running"
	SubAgentDone        SubAgentNodeStatus = "done"
	SubAgentFailed      SubAgentNodeStatus = "failed"
	SubAgentInterrupted SubAgentNodeStatus = "interrupted" // 进程中断/崩溃遗留的未完成节点（重启后可见，供用户在父会话重跑）
)

// SubAgentTreeNode 是子代理树的只读投影节点（GUI 树视图数据源）。
//
// SessionID 是**子代理自己的**会话号（节点会话，详情/事件路由用）；MainSessionID
// 是**发起 fork 的那个主会话**（工作表格行的归属会话轴，见 WorkItem.SessionID）。
// 两者必须分开：树是进程级的一张（多个会话的 fork 共处一树），而工作表格行是
// 会话粒度的——前端按行的 session_id 做「仅本会话」与「实发」筛选，行拿不到
// 归属键就会被判成"每个会话本会话"。空 = 未标注（旧记录/无 ctx 会话归属的
// 注册路径），消费侧不得据空值猜会话。
type SubAgentTreeNode struct {
	ID            string               `json:"id"`
	ParentID      string               `json:"parent_id,omitempty"`
	Goal          string               `json:"goal,omitempty"`
	Status        SubAgentNodeStatus   `json:"status"`
	Summary       string               `json:"summary,omitempty"`
	Error         string               `json:"error,omitempty"`
	SessionID     string               `json:"session_id,omitempty"`
	MainSessionID string               `json:"main_session_id,omitempty"`
	StartedAt     time.Time            `json:"started_at,omitempty"`
	EndedAt       time.Time            `json:"ended_at,omitempty"`
	Context       *SubAgentNodeContext `json:"context,omitempty"`
	Children      []SubAgentTreeNode   `json:"children,omitempty"`
}

// SubAgentNodeContext 是树节点的紧凑上下文（ContextSnapshot 的有界投影）。
type SubAgentNodeContext struct {
	Goal          string   `json:"goal,omitempty"`
	Progress      string   `json:"progress,omitempty"`
	MessageCount  int      `json:"message_count"`
	TokenEstimate int      `json:"token_estimate,omitempty"`
	Findings      []string `json:"findings,omitempty"`
}
