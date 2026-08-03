package seelebridge

import (
	"context"
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
	// 两个子代理输出都在结果里（nodes 数组 + summary 拼接）。
	for _, want := range []string{"fork-left: audit module A done", "fork-right: audit module B done", "## s1", "## s2"} {
		if !strings.Contains(result, want) {
			t.Errorf("fork result missing %q:\n%s", want, result)
		}
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
	wc.SetResultRaw("z", "z-output")
	wc.SetResultRaw("a", "a-output")

	out, err := n.Run(context.Background(), wc)
	if err != nil {
		t.Fatal(err)
	}
	// 排序：a 在 z 前。
	ai, zi := strings.Index(out, "## a"), strings.Index(out, "## z")
	if ai < 0 || zi < 0 || ai > zi {
		t.Fatalf("summary must be sorted by node id, got:\n%s", out)
	}
	if !strings.Contains(out, "a-output") || !strings.Contains(out, "z-output") {
		t.Fatalf("summary must carry all predecessor outputs:\n%s", out)
	}
}
