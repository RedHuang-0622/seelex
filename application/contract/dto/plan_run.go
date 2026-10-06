package dto

// plan_run 结果状态词表（唯一一份）。
//
// `plan_run` 的工具结果是一段 JSON（见 seelebridge/plan 的 planRunResultJSON），它跨两层：
// **写方**在 seelebridge/plan，**读方**在 application/core（updatePlanFromRunResult 把结果
// status 折成计划状态、planRunFailure 判"这一批是不是失败了"）。这一格回答"这一批跑成什么样"，
// 三个词只在这里定义一次——两边都引它，不再各写一份字面量（③U6）。
//
// 边界：同一段 JSON 里的**节点** status（queued|running|completed|failed|skipped|canceled|
// aborted|panicked）来自框架 workplan 的 `NodeBase.Status`，**不是这一格的词**，不在这里定义。
const (
	// PlanRunStatusCompleted = 整批跑完且没有失败节点。
	PlanRunStatusCompleted = "completed"
	// PlanRunStatusFailed = 有失败节点，或执行本身回错（REQ-006：任一分支失败不得标成 completed）。
	PlanRunStatusFailed = "failed"
	// PlanRunStatusAborted = 批次被中止（Aborted），不是节点失败。
	PlanRunStatusAborted = "aborted"
)
