package dto

// SessionStatus 是**一个会话**的可见状态（会话树 / 当前会话徽标 / 目录行的数据源）。
//
// 这一格的两半：一半是**运行期叠加**的状态（`restoring`：切到未驻留会话时的后台冷加载；
// 以及 `running`/`queued`/`awaiting_approval` 这些只在进程活着的期间成立的态），
// 另一半是**持久**的态（`draft`/`idle`/`archived`）。落盘那一侧由 sessionstore 的
// `sessionstore.Status`（字符串 wire，在契约之下）承载，它的词集是这一格的**持久子集**；
// 边界转换点见 `internal/adapters` 的 `sessionStatusOfRecord`。
//
// 历史：`model.SessionStatus` 曾经是 `type SessionStatus string` 的第二份词表，
// `internal/adapters` 里那句 `model.SessionStatus(item.Status)` 是**无类型转换**——
// store 里出现一个不在表里的词，它照样流进可见面。合并后取值只能从下面这组常量来，
// 越界的词在边界处显式折成 `SessionStatusUnknown`（不是任何已知态）。
type SessionStatus uint8

const (
	// SessionStatusUnknown 是零值：store 里的词我们认不得时落到它上面。
	SessionStatusUnknown SessionStatus = iota
	// SessionStatusDraft = 尚未生成 ID、不得持久化的待发送会话。
	SessionStatusDraft
	// SessionStatusIdle = 会话存在且当前没在跑。
	SessionStatusIdle
	// SessionStatusRunning = 有在飞回合。
	SessionStatusRunning
	// SessionStatusQueued = 有排队输入在等当前回合。
	SessionStatusQueued
	// SessionStatusAwaitingApproval = 引擎阻塞在审批上（覆盖运行态）。
	SessionStatusAwaitingApproval
	// SessionStatusArchived = 已归档（可见但不再运行）。
	SessionStatusArchived
	// SessionStatusRestoring = 运行中切换到未驻留会话时"后台冷加载中"的权威状态
	// （视图已切到目标空壳，内容基线由装载完成事件发布）。**不落盘**：它只由运行期叠加。
	SessionStatusRestoring
)

// sessionStatusWords 是"枚举 ↔ 对外词"的对照表（本格唯一一份）。
var sessionStatusWords = [...]string{
	SessionStatusUnknown:          "unknown",
	SessionStatusDraft:            "draft",
	SessionStatusIdle:             "idle",
	SessionStatusRunning:          "running",
	SessionStatusQueued:           "queued",
	SessionStatusAwaitingApproval: "awaiting_approval",
	SessionStatusArchived:         "archived",
	SessionStatusRestoring:        "restoring",
}

var sessionStatusCodec = stateCodec{name: "会话可见状态", words: sessionStatusWords[:]}

// String 给出对外词（快照 JSON / 目录行都用它）。
func (s SessionStatus) String() string { return sessionStatusCodec.word(uint8(s)) }

// ParseSessionStatus 把对外词读回枚举；第二个返回值报告认不认得。
func ParseSessionStatus(text string) (SessionStatus, bool) {
	ordinal, ok := sessionStatusCodec.ordinal(text)
	return SessionStatus(ordinal), ok
}

// MarshalJSON 保住 wire 形状：JSON 里仍是 "idle" 这样的词。
func (s SessionStatus) MarshalJSON() ([]byte, error) { return sessionStatusCodec.marshal(uint8(s)) }

// UnmarshalJSON 读回对外词；认不得的词报错，不动原值。
func (s *SessionStatus) UnmarshalJSON(data []byte) error {
	return sessionStatusCodec.unmarshal(data, (*uint8)(s))
}
