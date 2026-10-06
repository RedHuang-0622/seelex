package dto

import "time"

// SubAgentNodeStatus 是子代理树节点的生命周期状态（树投影专用，**也是落盘记录的同一格**）。
//
// 枚举（不是"契约里的几个字符串"）：取值只能从下面这组常量来，比较只能发生在枚举之间，
// 写错词是**编译错误**。对外词只在 subAgentNodeStatusWords 里出现一次——JSON、GUI、以及
// 落盘记录（sessionstore 在契约之下，存的就是这个词）都走它。
type SubAgentNodeStatus uint8

const (
	// SubAgentUnknown 是零值：只出现在"落盘记录里的词我们认不得"这条路径上
	// （见 seelebridge 的 nodeStateOfRecord）。它**不是终态**，消费方不得据它判完成。
	SubAgentUnknown SubAgentNodeStatus = iota
	// SubAgentQueued = fork 派工但会话尚未启动。
	SubAgentQueued
	// SubAgentRunning = 会话已启动、还没落结论。
	SubAgentRunning
	// SubAgentDone = 落了成功结论。
	SubAgentDone
	// SubAgentFailed = 落了失败结论。
	SubAgentFailed
	// SubAgentInterrupted = 进程中断/崩溃遗留的未完成节点（重启后可见，供用户在父会话重跑）。
	SubAgentInterrupted
)

// subAgentNodeStatusWords 是"枚举 ↔ 对外词"的对照表（本格唯一一份）。
var subAgentNodeStatusWords = [...]string{
	SubAgentUnknown:     "unknown",
	SubAgentQueued:      "queued",
	SubAgentRunning:     "running",
	SubAgentDone:        "done",
	SubAgentFailed:      "failed",
	SubAgentInterrupted: "interrupted",
}

var subAgentNodeStatusCodec = stateCodec{name: "记录状态", words: subAgentNodeStatusWords[:]}

// String 给出对外词（JSON / GUI / 落盘记录都用它）。
func (s SubAgentNodeStatus) String() string { return subAgentNodeStatusCodec.word(uint8(s)) }

// ParseSubAgentNodeStatus 把对外词读回枚举；第二个返回值报告认不认得。
func ParseSubAgentNodeStatus(text string) (SubAgentNodeStatus, bool) {
	ordinal, ok := subAgentNodeStatusCodec.ordinal(text)
	return SubAgentNodeStatus(ordinal), ok
}

// MarshalJSON 保住 wire 形状：JSON 里仍是 "running" 这样的词。
func (s SubAgentNodeStatus) MarshalJSON() ([]byte, error) {
	return subAgentNodeStatusCodec.marshal(uint8(s))
}

// UnmarshalJSON 读回对外词；认不得的词报错，不动原值。
func (s *SubAgentNodeStatus) UnmarshalJSON(data []byte) error {
	return subAgentNodeStatusCodec.unmarshal(data, (*uint8)(s))
}

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
