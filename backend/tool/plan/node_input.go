package plan

import seenode "github.com/RedHuang-0622/seelex/backend/tool/node"

// SeelexNodeInput / NodeBudgetInput 的唯一实现已下沉到 node 域（执行者
// AgentNode 所在处）。这里保留同身份别名，供仍在使用 Plan DSL 的历史用例
// 与兼容面引用；Plan 执行器退役后这些别名随包一起删除。
type (
	SeelexNodeInput = seenode.SeelexNodeInput
	NodeBudgetInput = seenode.NodeBudgetInput
)
