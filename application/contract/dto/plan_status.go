package dto

// PlanStatus 是**一张 Plan 的生命周期状态**（`Snapshot.Runtime.Plan.Status`、
// `SubagentEvent.PlanStatus` 的同一格）。
//
// 与 `PlanRunStatus`（**一次 plan_run 调用的结果**）的分工必须写清，否则两格会互相污染：
//
//   - `PlanRunStatus`：`plan_run` 工具结果 JSON 里的那一个 status，回答"这一批跑成什么样"，
//     取值面只有终态（completed | failed | aborted），而且**必须拒绝节点状态词**
//     （`ParsePlanRunStatus("running")` 判否——见那一格的用例）。
//   - `PlanStatus`（本格）：Plan 投影现在的样子，含在途（pending | running）。它的词与
//     **节点状态**的词（queued | running | …）大量重合，但那是**另一格**：节点状态由
//     框架 workplan 的 `NodeBase.Status` 驱动（见 `NodeStatus`）。
//
// 两格由同一条判据算出来（都是"看节点跑成什么样"）——这份重复是**有意的**：一处是工具
// 一次的结算单，一处是投影的当前态，读方与写方都不重叠，合并会让"批次结果只可能落终态"
// 这条不变式消失（合并后 `ParsePlanRunStatus("running")` 会认下在途词）。
type PlanStatus uint8

const (
	// PlanStatusUnknown 是零值："没有 Plan"或读不懂的词落到它上面。它**不是终态**。
	PlanStatusUnknown PlanStatus = iota
	// PlanPending = 计划已装载、还没有节点在跑。
	PlanPending
	// PlanRunning = 有计划在执行中。
	PlanRunning
	// PlanCompleted = 全部节点收敛为完成（Progress = 1）。
	PlanCompleted
	// PlanFailed = 有节点失败/恐慌（panicked）。
	PlanFailed
	// PlanAborted = 批次被取消/中止（canceled | aborted）。
	PlanAborted
)

// planStatusWords 是"枚举 ↔ 对外词"的对照表（本格唯一一份）。
var planStatusWords = [...]string{
	PlanStatusUnknown: "unknown",
	PlanPending:       "pending",
	PlanRunning:       "running",
	PlanCompleted:     "completed",
	PlanFailed:        "failed",
	PlanAborted:       "aborted",
}

var planStatusCodec = stateCodec{name: "计划状态", words: planStatusWords[:]}

// String 给出对外词。注意 "running" 在这一格里是**计划的**在途态；节点状态的
// "running" 是另一格（同一个词面，两台机器——取值永远走各自那一格的常量）。
func (s PlanStatus) String() string { return planStatusCodec.word(uint8(s)) }

// ParsePlanStatus 把对外词读回枚举；第二个返回值报告认不认得。
func ParsePlanStatus(text string) (PlanStatus, bool) {
	ordinal, ok := planStatusCodec.ordinal(text)
	return PlanStatus(ordinal), ok
}

// MarshalJSON 保住 wire 形状：JSON 里仍是 "running" 这样的词。
func (s PlanStatus) MarshalJSON() ([]byte, error) { return planStatusCodec.marshal(uint8(s)) }

// UnmarshalJSON 读回对外词；认不得的词报错，不动原值。
func (s *PlanStatus) UnmarshalJSON(data []byte) error {
	return planStatusCodec.unmarshal(data, (*uint8)(s))
}
