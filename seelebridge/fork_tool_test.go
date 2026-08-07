package seelebridge

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/workplan/codec"
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"
)

// ── fork_subagents（切片 5，docs/2026-08-03-subagent-fork-architecture/plan.md §4）──

// TestForkSubagentsEndToEnd 验证 fork 全链路：两个并行子代理（确定性
// completer）→ summary 拼接 → plan_run 结果含全部输出。
func TestForkSubagentsEndToEnd(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	// 两个子代理路由到不同账号（并行）。
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{
		"sub-1": newScriptedNodeCompleter("fork-left: audit module A done"),
		"sub-2": newScriptedNodeCompleter("fork-right: audit module B done"),
	})

	result, err := runtime.Agent().DirectDispatch(context.Background(), "fork_subagents",
		`{"subagents":[{"id":"s1","goal":"audit module A"},{"id":"s2","goal":"audit module B"}]}`)
	if err != nil {
		t.Fatalf("fork_subagents failed: %v", err)
	}
	if !strings.Contains(result, `"status":"completed"`) {
		t.Fatalf("fork result must be completed, got: %s", result)
	}
	// 两个子代理输出都在结果里（nodes 数组 + summary 紧凑行；T1：对话区
	// 只带单行摘要，完整输出在工作区子代理树/详情弹窗）。
	for _, want := range []string{"fork-left: audit module A done", "fork-right: audit module B done", "- s1:", "- s2:"} {
		if !strings.Contains(result, want) {
			t.Errorf("fork result missing %q:\n%s", want, result)
		}
	}
	if strings.Contains(result, `"## `) {
		t.Errorf("fork summary must not use old full-output format (##):\n%s", result)
	}
	// 结束后详情数据面：结构化上下文快照（Goal/MessageCount；只读子代理 actor）。
	snap, ok := runtime.NodeContextSnapshot("s1")
	if !ok || snap == nil {
		t.Fatalf("node context snapshot missing after fork")
	}
	if snap.Goal != "audit module A" || snap.MessageCount == 0 {
		t.Fatalf("node context snapshot = %+v", snap)
	}
	if _, ok := runtime.NodeContextSnapshot("missing-node"); ok {
		t.Fatal("unknown node must not have a context snapshot")
	}
}

// TestForkSubagentsValidation 验证护栏：空列表 / 重复 id / 缺 goal / 超数量上限。
func TestForkSubagentsValidation(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	runtime.SetPlanPolicy(PlanPolicy{Effort: "test", MaxNodes: 2})

	cases := []struct {
		name string
		args string
		want string
	}{
		{"empty", `{"subagents":[]}`, "at least one subagent"},
		{"duplicate", `{"subagents":[{"id":"a","goal":"x"},{"id":"a","goal":"y"}]}`, "duplicate subagent id"},
		{"missing goal", `{"subagents":[{"id":"a","goal":""}]}`, "must have id and goal"},
		{"too many", `{"subagents":[{"id":"a","goal":"x"},{"id":"b","goal":"y"},{"id":"c","goal":"z"}]}`, "exceeds policy limit"},
	}
	for _, tc := range cases {
		if _, err := runtime.Agent().DirectDispatch(context.Background(), "fork_subagents", tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want contains %q", tc.name, err, tc.want)
		}
	}
}

// TestForkSummaryNodeConcatenatesPredecessors 验证 summary 节点拼接
// WorkflowContext.PrevResults（含排序确定性）。
func TestForkSummaryNodeConcatenatesPredecessors(t *testing.T) {
	spec := codec.NodeSpec[SeelexNodeInput]{ID: "summary", Kind: "summary",
		Input: SeelexNodeInput{ID: "summary", Input: "summarize"}}
	n := newForkSummaryNode(spec)
	wc := workplanTypes.NewWorkflowContext()
	// 多行输出：紧凑摘要保留前 N 行（*N 语义），超出部分截断。
	// 30 行上限（forkSummaryMaxLines）可容纳一批 20+ 实例的逐条汇报。
	var zLines []string
	for i := 1; i <= forkSummaryMaxLines+3; i++ {
		zLines = append(zLines, fmt.Sprintf("z-line-%d", i))
	}
	wc.SetResultRaw("z", strings.Join(zLines, "\n"))
	wc.SetResultRaw("a", "a-line-1\na-line-2")

	out, err := n.Run(context.Background(), wc)
	if err != nil {
		t.Fatal(err)
	}
	// 排序：a 在 z 前（T1 紧凑摘要仍保序）。
	ai, zi := strings.Index(out, "- a:"), strings.Index(out, "- z:")
	if ai < 0 || zi < 0 || ai > zi {
		t.Fatalf("summary must be sorted by node id, got:\n%s", out)
	}
	// *N 语义：前 N 行都在（N = forkSummaryMaxLines），超出 N 的行截断。
	if !strings.Contains(out, "- a: a-line-1\n  a-line-2") || !strings.Contains(out, "- z: z-line-1\n  z-line-2") {
		t.Fatalf("summary must carry first %d lines, got:\n%s", forkSummaryMaxLines, out)
	}
	lastKept := fmt.Sprintf("z-line-%d", forkSummaryMaxLines)
	if !strings.Contains(out, lastKept) {
		t.Fatalf("summary must keep line %d, got:\n%s", forkSummaryMaxLines, out)
	}
	dropped := fmt.Sprintf("z-line-%d", forkSummaryMaxLines+1)
	if strings.Contains(out, dropped) {
		t.Fatalf("summary must drop lines beyond %d, got:\n%s", forkSummaryMaxLines, out)
	}
	if !strings.Contains(out, "子代理完成情况") {
		t.Fatalf("summary must carry the compact header:\n%s", out)
	}
}

// TestForkPlanNodesCarryLenientLoopBudget 验证 fork 子代理节点预算使用
// 独立的宽松默认（limits.fork_node_max_loops，默认 60）而非通用 15 轮——
// 长任务（多实例串行）不会被循环预算掐死。
func TestForkPlanNodesCarryLenientLoopBudget(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	loaded, err := runtime.buildForkPlan(forkSubagentsInput{
		Subagents: []forkSubagentSpec{{ID: "s1", Goal: "fix"}, {ID: "s2", Goal: "fix"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, id := range loaded.Plan.AllNodes() {
		agentNode, ok := loaded.Plan.GetNode(id).(*SeelexAgentNode)
		if !ok {
			continue
		}
		input := agentNode.input
		if input.Budget == nil || input.Budget.MaxLoops != runtime.limits.ForkNodeMaxLoops {
			t.Fatalf("fork node %q budget = %+v, want max_loops=%d",
				input.ID, input.Budget, runtime.limits.ForkNodeMaxLoops)
		}
		found++
	}
	if found != 2 {
		t.Fatalf("fork agent nodes = %d, want 2", found)
	}
}

func TestForkSummaryLinesCompact(t *testing.T) {
	cases := []struct {
		output string
		want   string
	}{
		{"", ""},
		{"   \n", ""},
		{"第一行\n第二行\n", "第一行\n第二行"},
		{"a\n\nb\nc\nd\ne\nf\ng\n", "a\nb\nc\nd\ne\nf\ng"}, // 空行跳过；30 行上限内全保留
		{strings.Repeat("x", 200), strings.Repeat("x", forkSummaryLineLimit) + "…"},
		{multiLine(forkSummaryMaxLines + 3), multiLine(forkSummaryMaxLines)}, // 超出 30 行截断
	}
	for _, tc := range cases {
		got := forkResultSummaryLines(tc.output)
		if got != tc.want {
			t.Fatalf("forkResultSummaryLines(%q) = %q, want %q", tc.output, got, tc.want)
		}
	}
}

// multiLine 生成 n 行 l1..ln 文本。
func multiLine(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("l%d", i+1)
	}
	return strings.Join(lines, "\n")
}
