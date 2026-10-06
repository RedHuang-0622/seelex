package dto

// GoalStatus 是**一个 goal** 的生命周期状态（goal 栈每层一态；看板/headless 视图与
// goal 存档同一格）。
//
// 两个面：
//   - 可见面：`GoalRecord.Status`（goal_status 视图 / 事件投影）；
//   - 存档面：`sessionstore.GoalFrame.Status`（第五栈帧的落盘 wire，store 在契约之下）
//     ——读回由 `application/core/goal` 的 `goalStatusOfRecord` 折算（认不得的词说认不得）。
type GoalStatus uint8

const (
	// GoalStatusUnknown 是零值：存档里认不得的词落到它上面。它**不是**某个已知状态——
	// 谁把它当成 active/paused，谁就是在替存档编事实。
	GoalStatusUnknown GoalStatus = iota
	// GoalActive = 栈顶当前目标。
	GoalActive
	// GoalPaused = 栈下层被挂起（嵌套时自动）。
	GoalPaused
	// GoalReviewing = 终态校验中（TL gate 在跑）。
	GoalReviewing
	// GoalCompleted = finish 收口。
	GoalCompleted
	// GoalFailed = 判不可达成。
	GoalFailed
	// GoalAborted = 显式放弃。
	GoalAborted
	// GoalWaitingHuman = 预算耗尽/越权，等人工。
	GoalWaitingHuman
)

// goalStatusWords 是"枚举 ↔ 对外词"的对照表（本格唯一一份）。
var goalStatusWords = [...]string{
	GoalStatusUnknown: "unknown",
	GoalActive:        "active",
	GoalPaused:        "paused",
	GoalReviewing:     "reviewing",
	GoalCompleted:     "completed",
	GoalFailed:        "failed",
	GoalAborted:       "aborted",
	GoalWaitingHuman:  "waiting_human",
}

var goalStatusCodec = stateCodec{name: "goal 状态", words: goalStatusWords[:]}

// String 给出对外词（视图 JSON 与存档 wire 都用它）。
func (s GoalStatus) String() string { return goalStatusCodec.word(uint8(s)) }

// ParseGoalStatus 把对外词读回枚举；第二个返回值报告认不认得。
func ParseGoalStatus(text string) (GoalStatus, bool) {
	ordinal, ok := goalStatusCodec.ordinal(text)
	return GoalStatus(ordinal), ok
}

// Terminal 报告状态是否终态（不再停留在 goal 栈上）。
func (s GoalStatus) Terminal() bool {
	switch s {
	case GoalCompleted, GoalFailed, GoalAborted:
		return true
	default:
		return false
	}
}

// MarshalJSON 保住 wire 形状：JSON 里仍是 "active" 这样的词。
func (s GoalStatus) MarshalJSON() ([]byte, error) { return goalStatusCodec.marshal(uint8(s)) }

// UnmarshalJSON 读回对外词；认不得的词报错，不动原值。
func (s *GoalStatus) UnmarshalJSON(data []byte) error {
	return goalStatusCodec.unmarshal(data, (*uint8)(s))
}
