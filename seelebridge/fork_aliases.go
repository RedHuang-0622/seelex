package seelebridge

import (
	"github.com/RedHuang-0622/Seele/workplan/codec"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	"github.com/RedHuang-0622/seelex/seelebridge/fork"
)

// ── fork 域 API 兼容别名 ───────────────────────────────────────────────
// fork_subagents 的纯类型与 summary 节点已迁入 seelebridge/fork；执行编排
// （Runtime 门面）留在 fork_tool.go。本文件在根包重导出符号并保留旧名，
// 保证既有调用面（buildNode、fork_tool.go、fork 测试）不因拆包而破坏。

// forkSubagentsInput 是 fork_subagents 的参数契约。
type forkSubagentsInput = fork.Input

// forkSubagentSpec 是单个子代理的派工规格。
type forkSubagentSpec = fork.SubagentSpec

// forkSubagentsContractDescription 是 fork_subagents 工具的契约描述。
const forkSubagentsContractDescription = fork.SubagentsContractDescription

// forkSummaryLineLimit / forkSummaryMaxLines 是 summary 节点的截断参数
// （兼容既有测试/调用面）。
const (
	forkSummaryLineLimit = fork.SummaryLineLimit
	forkSummaryMaxLines  = fork.SummaryMaxLines
)

// forkPlanCanonical 生成 fork DAG 的规范 JSON（审计/展示；非模型输入）。
func forkPlanCanonical(input forkSubagentsInput) string { return fork.PlanCanonical(input) }

// forkResultSummaryLines 提取子代理输出的有界摘要（rune 计数）。
func forkResultSummaryLines(output string) (summary string, fullRunes int, truncated bool) {
	return fork.ResultSummaryLines(output)
}

// forkSummaryNode 是 fork 的汇总节点。
type forkSummaryNode = fork.SummaryNode

// newForkSummaryNode 构造 fork summary 节点（根包薄壳，buildNode 使用）。
func newForkSummaryNode(spec codec.NodeSpec[SeelexNodeInput]) *forkSummaryNode {
	return fork.NewSummaryNode(spec)
}

// 编译期断言：fork.SummaryNode 实现 node.Node（buildNode summary 分支返回）。
var _ node.Node = (*fork.SummaryNode)(nil)
