package node

// SeelexNodeInput 是子代理节点的输入负载。
//
// 它原先定义在 Plan 包（产品 DSL 的节点负载），但真正执行它的 AgentNode
// 属于 node 域；Plan 执行器退役后，fork_subagents 仍按这份输入构造单条
// 子代理。放在 node 域可以避免 node 反向依赖 Plan。
type SeelexNodeInput struct {
	ID    string `json:"id"`
	Input string `json:"input"`
	Kind  string `json:"kind,omitempty"` // agent | auto | function | approve | verify | deliver
	// Budget 是节点级执行预算：可选覆盖 limits 默认值；
	// 缺省（nil/0）回退 seele.yaml limits。
	Budget *NodeBudgetInput `json:"budget,omitempty"`
	// TaskID 是装配件模式绑定的现成 task：只绑 task_id，不注入 task
	// 内容到 prompt；空 = 子代理自行开 task。
	TaskID string `json:"task_id,omitempty"`
}

// NodeBudgetInput 是节点子代理的预算参数（JSON 契约：max_loops /
// max_output_tokens 均可选，0 = 缺省回退 limits）。
type NodeBudgetInput struct {
	MaxLoops        int `json:"max_loops,omitempty"`
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`
}
