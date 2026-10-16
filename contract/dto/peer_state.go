package dto

// PeerState 是评审者（b / ADVISOR）会话的生命周期状态（a2a 协议 §9：
// `AdvisorSession.State`，并随治理投影下发前端 `peer_state`）。
//
// 与"目标状态"（GoalStatus）是两格：这一格描述的是**评审者这一侧**的会话在不在、
// 在干什么——detached（没绑）→ bound（绑上）→ evaluating（评估中）→
// advisory_pending（有裁决待取）→ reaped（已回收，reason=done|evicted|killed）。
type PeerState uint8

const (
	// PeerStateUnknown 是零值：读回来的词认不得，或这一侧还没有状态。
	PeerStateUnknown PeerState = iota
	// PeerDetached = 未绑定（会话没起来）。
	PeerDetached
	// PeerBound = 已绑定（会话可用）。
	PeerBound
	// PeerEvaluating = 正在评估（有在飞 TL 回合）。
	PeerEvaluating
	// PeerAdvisoryPending = 已有裁决待取。
	PeerAdvisoryPending
	// PeerReaped = 已回收（unbind 后）。
	PeerReaped
)

// peerStateWords 是"枚举 ↔ 对外词"的对照表（本格唯一一份）。
var peerStateWords = [...]string{
	PeerStateUnknown:    "unknown",
	PeerDetached:        "detached",
	PeerBound:           "bound",
	PeerEvaluating:      "evaluating",
	PeerAdvisoryPending: "advisory_pending",
	PeerReaped:          "reaped",
}

var peerStateCodec = stateCodec{name: "评审者状态", words: peerStateWords[:]}

// String 给出对外词（治理投影的 peer_state）。
func (s PeerState) String() string { return peerStateCodec.word(uint8(s)) }

// ParsePeerState 把对外词读回枚举；第二个返回值报告认不认得。
func ParsePeerState(text string) (PeerState, bool) {
	ordinal, ok := peerStateCodec.ordinal(text)
	return PeerState(ordinal), ok
}

// MarshalJSON 保住 wire 形状：JSON 里仍是 "detached" 这样的词。
func (s PeerState) MarshalJSON() ([]byte, error) { return peerStateCodec.marshal(uint8(s)) }

// UnmarshalJSON 读回对外词；认不得的词报错，不动原值。
func (s *PeerState) UnmarshalJSON(data []byte) error {
	return peerStateCodec.unmarshal(data, (*uint8)(s))
}
