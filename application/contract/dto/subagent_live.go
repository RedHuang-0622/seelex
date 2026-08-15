package dto

import "time"

// SubagentLiveEvent 是 node 第一视角的实时推送事件（即时输出面）：
// stage = 阶段日志（spawn/turn/tool/result），tool = 工具调用与结果。
type SubagentLiveEvent struct {
	NodeID string        `json:"node_id"`
	At     time.Time     `json:"at"`
	Kind   string        `json:"kind"` // "stage" | "tool"
	Stage  *NodeStageLog `json:"stage,omitempty"`
	Tool   *SubagentTool `json:"tool,omitempty"`
}

// NodeStageLog 是 node 第一视角阶段日志的对外投影。
type NodeStageLog struct {
	Stage         string    `json:"stage"`
	NodeID        string    `json:"node_id"`
	SessionID     string    `json:"session_id"`
	Turn          int       `json:"turn,omitempty"`
	At            time.Time `json:"at"`
	Preview       string    `json:"preview,omitempty"`
	TokenEstimate int       `json:"token_estimate,omitempty"`
}

// SubagentTool 是子代理工具调用的实时投影（含结果预览）。
type SubagentTool struct {
	ID         string    `json:"id"`
	NodeID     string    `json:"node_id"`
	Name       string    `json:"name"`
	Arguments  string    `json:"arguments,omitempty"`
	Result     string    `json:"result,omitempty"`
	Error      string    `json:"error,omitempty"`
	Status     string    `json:"status"` // running | success | error
	StartedAt  time.Time `json:"started_at,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
}

// SubagentToolEvent 是子代理工具调用的有界活动投影（详情数据面；runtime 事件
// 与 application 快照共用同一形状，单源在本包）。与 SubagentTool 的区别：
// 本结构保留 Duration（time.Duration），供 PlanNode.ToolEvents 有界投影使用；
// 实时流推送（SubagentTool.DurationMS）在边界转换。
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
