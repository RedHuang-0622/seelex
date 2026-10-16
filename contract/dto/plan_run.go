package dto

// plan_run 结果状态：枚举（唯一一份定义）。
//
// `plan_run` 的工具结果是一段 JSON（写方见 seelebridge/plan 的 planRunResultJSON），它跨两层：
// **写方**在 seelebridge/plan，**读方**在 application/core（把结果 status 折成计划状态、
// 判"这一批是不是失败了"）。这一格回答"这一批跑成什么样"。
//
// 枚举化（不是"契约里的一处无类型字符串"）：取值只能从下面这一组来，比较只能发生在枚举之间；
// 对外词只在 `planRunStatusWords` 里出现一次，边界处 `String()` 转出去。
//
// 边界：同一段 JSON 里的**节点** status（queued|running|completed|failed|skipped|canceled|
// aborted|panicked）来自框架 workplan 的 `NodeBase.Status`，**不是这一格的词**——所以
// `ParsePlanRunStatus("panicked")` 必须判否（有用例钉住）。
type PlanRunStatus uint8

const (
	// PlanRunStatusUnknown 是零值：结果 JSON 没带 status 时落到它上面。
	PlanRunStatusUnknown PlanRunStatus = iota
	// PlanRunStatusCompleted = 整批跑完且没有失败节点。
	PlanRunStatusCompleted
	// PlanRunStatusFailed = 有失败节点，或执行本身回错（REQ-006：任一分支失败不得标成 completed）。
	PlanRunStatusFailed
	// PlanRunStatusAborted = 批次被中止（Aborted），不是节点失败。
	PlanRunStatusAborted
)

// planRunStatusWords 是"枚举 ↔ 对外词"的唯一对照表。
var planRunStatusWords = [...]string{
	PlanRunStatusUnknown:   "unknown",
	PlanRunStatusCompleted: "completed",
	PlanRunStatusFailed:    "failed",
	PlanRunStatusAborted:   "aborted",
}

// planRunStatusCodec 把这张表接到**唯一一份编码口径**上（见 state_codec.go）。
var planRunStatusCodec = stateCodec{name: "plan_run 结果状态", words: planRunStatusWords[:]}

// String 给出对外词（plan_run 结果 JSON 里的 status）。
func (s PlanRunStatus) String() string { return planRunStatusCodec.word(uint8(s)) }

// ParsePlanRunStatus 把对外词读回枚举；第二个返回值报告它是不是这一格的词。
func ParsePlanRunStatus(text string) (PlanRunStatus, bool) {
	ordinal, ok := planRunStatusCodec.ordinal(text)
	return PlanRunStatus(ordinal), ok
}

// MarshalJSON 保住 wire 形状：JSON 里仍是 "completed" 这样的词。
func (s PlanRunStatus) MarshalJSON() ([]byte, error) { return planRunStatusCodec.marshal(uint8(s)) }

// UnmarshalJSON 读回对外词；认不得的词报错（含框架的节点状态词——它不属于这一格）。
func (s *PlanRunStatus) UnmarshalJSON(data []byte) error {
	return planRunStatusCodec.unmarshal(data, (*uint8)(s))
}
