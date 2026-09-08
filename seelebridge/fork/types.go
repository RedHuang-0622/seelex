package fork

import (
	"encoding/json"
)

// SubagentsContractDescription 是 fork_subagents 工具的契约描述（追加在
// 工具 Description 后，指导模型使用）。
const SubagentsContractDescription = `
- Fork N isolated subagents in parallel (worktree-isolated) and return their structured outputs.
- max_concurrency: optional cap on parallel subagents (default: policy limit).
- Returns a summary JSON with each subagent's output.
`

// Input 是 fork_subagents 的参数契约。
type Input struct {
	Subagents      []SubagentSpec `json:"subagents"`
	MaxConcurrency int            `json:"max_concurrency,omitempty"`
	// TimeoutSec 是本批 fork 的总超时覆盖（秒）：长任务可不填（走
	// limits.fork_timeout，默认 2h）；简单审查/只读任务按需给 1200（20 分钟）
	// 等更紧的上限，避免排队或异常时挂太久。
	TimeoutSec int `json:"timeout_sec,omitempty"`
}

// SubagentSpec 是单个子代理的派工规格。
type SubagentSpec struct {
	ID   string `json:"id"`
	Goal string `json:"goal"`
}

// PlanCanonical 生成 fork DAG 的规范 JSON（审计/展示；非模型输入）。
func PlanCanonical(input Input) string {
	encoded, _ := json.Marshal(input)
	return string(encoded)
}
