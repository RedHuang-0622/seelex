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
	// 多行输出：紧凑摘要只保留首行（内核 RawString 会 JSON 编码，单行内容
	// 会被整行保留，用多行才能验证"不灌完整输出"）。
	wc.SetResultRaw("z", "z-line-1\nz-line-2\nz-line-3")
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
	// 紧凑：只带首行摘要，不带完整输出（完整内容在工作区子代理树/详情）。
	if !strings.Contains(out, "- a: a-line-1") || !strings.Contains(out, "- z: z-line-1") {
		t.Fatalf("summary must carry first-line summaries, got:\n%s", out)
	}
	if strings.Contains(out, "a-line-2") || strings.Contains(out, "z-line-2") || strings.Contains(out, "z-line-3") {
		t.Fatalf("compact summary must not embed full outputs:\n%s", out)
	}
	if !strings.Contains(out, "子代理完成情况") {
		t.Fatalf("summary must carry the compact header:\n%s", out)
	}
}

func TestForkSummaryLineCompact(t *testing.T) {
	cases := []struct {
		output string
		want   string
	}{
		{"", "(无输出)"},
		{"   \n", "(无输出)"},
		{"第一行\n第二行\n", "第一行"},
		{strings.Repeat("x", 200), strings.Repeat("x", forkSummaryLineLimit) + "…"},
	}
	for _, tc := range cases {
		got := forkResultSummaryLine(tc.output)
		if got != tc.want {
			t.Fatalf("forkResultSummaryLine(%q) = %q, want %q", tc.output, got, tc.want)
		}
	}
}
