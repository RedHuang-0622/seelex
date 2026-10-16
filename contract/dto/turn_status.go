package dto

// TurnStatus 是一次**回合**（用户这一次请求）的执行状态与结论。
//
// 为什么是 Turn 而不是 Task：仓库里另有一格叫 TaskStatus，那是**工作表条目**的生命周期
// （条目可以排队、重试、被打点）。这一格回答的是"用户这次请求现在处在哪一步、结论是什么"。
// 两格必须取两个分得开的名字——历史上它们同名（`model.TaskStatus` 与 `dto.TaskStatus`）
// 却是两台不同的机器，这正是"状态机没有枚举统一"的核心症状之一，见
// docs/arch/state-machine-inventory.md §3。
//
// 这一格有**两个面**，词表只有下面一份：
//   - 可见面：`model.TaskState.Status`（`Snapshot.Task`，GUI/TUI 渲染）；
//   - 存档面：`model.TaskContextProjection.Status`（回合存档里的 wire 词）。
//
// 两个面曾经各有一套词表（可见面 progressing、存档面 running），中间靠一处手写映射
// 接起来；合并后同一个状态在两面说同一个词。老存档里的 "running" 由边界函数
// `task_context.turnStatusOfRecord` 读回（见那个函数上的口径）。
type TurnStatus uint8

const (
	// TurnUnknown 是零值：存档里认不得的词落到它上面。它**不是终态**，
	// 消费方不得据它判"这次请求结束了"。
	TurnUnknown TurnStatus = iota
	// TurnIdle = 会话持有上下文状态，但没有在飞回合（冷恢复 / 刚清空会话的维护身份）。
	TurnIdle
	// TurnProgressing = 回合进行中。
	TurnProgressing
	// TurnCompleted = 回合正常收尾（task_complete）。
	TurnCompleted
	// TurnNeedsUserDecision = 回合停在"必须由用户选择"的有效分歧上。
	TurnNeedsUserDecision
	// TurnBlocked = 回合被外部条件挡住（可续接）。
	TurnBlocked
	// TurnInterrupted = 进程中断/崩溃/上下文超限遗留的未收尾回合（可续接）。
	TurnInterrupted
	// TurnFailed = 回合以有界失败收尾（task_failed）。
	TurnFailed
)

// turnStatusWords 是"枚举 ↔ 对外词"的对照表（本格唯一一份）。
var turnStatusWords = [...]string{
	TurnUnknown:           "unknown",
	TurnIdle:              "idle",
	TurnProgressing:       "progressing",
	TurnCompleted:         "completed",
	TurnNeedsUserDecision: "needs_user_decision",
	TurnBlocked:           "blocked",
	TurnInterrupted:       "interrupted",
	TurnFailed:            "failed",
}

var turnStatusCodec = stateCodec{name: "回合状态", words: turnStatusWords[:]}

// String 给出对外词（Snapshot 的 JSON 与回合存档都用它）。
func (s TurnStatus) String() string { return turnStatusCodec.word(uint8(s)) }

// ParseTurnStatus 把对外词读回枚举；第二个返回值报告认不认得。
func ParseTurnStatus(text string) (TurnStatus, bool) {
	ordinal, ok := turnStatusCodec.ordinal(text)
	return TurnStatus(ordinal), ok
}

// MarshalJSON 保住 wire 形状：JSON 里仍是 "progressing" 这样的词。
func (s TurnStatus) MarshalJSON() ([]byte, error) { return turnStatusCodec.marshal(uint8(s)) }

// UnmarshalJSON 读回对外词；认不得的词报错，不动原值。
func (s *TurnStatus) UnmarshalJSON(data []byte) error {
	return turnStatusCodec.unmarshal(data, (*uint8)(s))
}
