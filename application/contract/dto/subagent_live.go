package dto

import "time"

// SubagentLiveEvent 是 node 第一视角的实时推送事件（即时输出面）：
// stage = 阶段日志（spawn/turn/tool/result），tool = 工具调用与结果，
// assistant = 子代理 assistant 正文增量（G7：ChatStream 的流式正文，经
// node AgentNode 边界投递；驱动 GUI 详情去轮询后仍保持新鲜）。
type SubagentLiveEvent struct {
	NodeID string        `json:"node_id"`
	At     time.Time     `json:"at"`
	Kind   string        `json:"kind"` // "stage" | "tool" | "assistant"
	Stage  *NodeStageLog `json:"stage,omitempty"`
	Tool   *SubagentTool `json:"tool,omitempty"`
	// Assistant 是子代理 assistant 正文增量（一次 LLM 轮次的流式文本分片；
	// 打开详情的客户端把它追加进会话记录底部，直到下次权威快照/详情到达）。
	Assistant *SubagentAssistant `json:"assistant,omitempty"`
}

// SubagentAssistant 是子代理 assistant 正文增量的事件载荷。
type SubagentAssistant struct {
	Turn int    `json:"turn,omitempty"`
	Text string `json:"text,omitempty"`
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

// ToolEventStatus 是子代理工具调用事件的状态（工具链活动面的同一格）。
//
// 枚举（int + iota）：写方在 seelebridge/session/tool_events.go 与 application/core/tool_hooks.go，
// 读方在 application/core/subagent_view 与 tui——全都只认这一组常量，比较写错词是编译错误。
type ToolEventStatus uint8

const (
	// ToolEventUnknown 是零值：事件没有状态时落到它上面（"没有值"不是"成功"）。
	ToolEventUnknown ToolEventStatus = iota
	// ToolEventRunning = 调用已发起、结果未回。
	ToolEventRunning
	// ToolEventSuccess = 结果回来了且不是错误。
	ToolEventSuccess
	// ToolEventError = 结果回来了且是错误（含权限拒绝）。
	ToolEventError
)

// toolEventStatusWords 是"枚举 ↔ 对外词"的对照表（本格唯一一份）。
var toolEventStatusWords = [...]string{
	ToolEventUnknown: "unknown",
	ToolEventRunning: "running",
	ToolEventSuccess: "success",
	ToolEventError:   "error",
}

var toolEventStatusCodec = stateCodec{name: "工具事件状态", words: toolEventStatusWords[:]}

// String 给出对外词（事件 JSON 与 GUI 都用它）。
func (s ToolEventStatus) String() string { return toolEventStatusCodec.word(uint8(s)) }

// ParseToolEventStatus 把对外词读回枚举；第二个返回值报告认不认得。
func ParseToolEventStatus(text string) (ToolEventStatus, bool) {
	ordinal, ok := toolEventStatusCodec.ordinal(text)
	return ToolEventStatus(ordinal), ok
}

// MarshalJSON 保住 wire 形状：JSON 里仍是 "success" 这样的词。
func (s ToolEventStatus) MarshalJSON() ([]byte, error) {
	return toolEventStatusCodec.marshal(uint8(s))
}

// UnmarshalJSON 读回对外词；认不得的词报错，不动原值。
func (s *ToolEventStatus) UnmarshalJSON(data []byte) error {
	return toolEventStatusCodec.unmarshal(data, (*uint8)(s))
}

// SubagentTool 是子代理工具调用的实时投影（含结果预览）。
type SubagentTool struct {
	ID         string          `json:"id"`
	NodeID     string          `json:"node_id"`
	Name       string          `json:"name"`
	Arguments  string          `json:"arguments,omitempty"`
	Result     string          `json:"result,omitempty"`
	Error      string          `json:"error,omitempty"`
	Status     ToolEventStatus `json:"status"` // running | success | error
	StartedAt  time.Time       `json:"started_at,omitempty"`
	DurationMS int64           `json:"duration_ms,omitempty"`
}

// SubagentToolEvent 是子代理工具调用的有界活动投影（详情数据面；runtime 事件
// 与 application 快照共用同一形状，单源在本包）。与 SubagentTool 的区别：
// 本结构保留 Duration（time.Duration），供 PlanNode.ToolEvents 有界投影使用；
// 实时流推送（SubagentTool.DurationMS）在边界转换。
type SubagentToolEvent struct {
	ID        string          `json:"id"`
	NodeID    string          `json:"node_id"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments,omitempty"`
	Result    string          `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	Status    ToolEventStatus `json:"status"`
	StartedAt time.Time       `json:"started_at,omitempty"`
	Duration  time.Duration   `json:"duration,omitempty"`
}
