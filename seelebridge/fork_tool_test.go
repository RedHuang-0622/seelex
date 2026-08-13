package seelebridge

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/workplan/codec"
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/fork"
	"github.com/RedHuang-0622/seelex/seelebridge/plan"
	"github.com/RedHuang-0622/seelex/seelebridge/task"
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

// TestForkSubagentsReuseStoredOutputSavesTokens 验证结果复用短路：既有 task
// 已完成且子代理树保留完整输出（结果返回失败需要 retry 的典型场景）→
// fork 直接返回已保存输出，不重新执行（省 token）；task 状态经 retry
// 计数后回到 completed。
func TestForkSubagentsReuseStoredOutputSavesTokens(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	// 预置：两个 goal 命中既有已完成 task + 子代理树已保存输出。
	seed := []struct {
		id, goal, summary string
	}{
		{"s1", "audit module A", "SEEDED-OUTPUT-A: module A verified"},
		{"s2", "audit module B", "SEEDED-OUTPUT-B: module B verified"},
	}
	for _, item := range seed {
		task, created, err := runtime.TaskAdd(dto.TaskSpec{
			ID: "subagent:" + item.id, Key: task.TaskKeyForGoal(item.goal),
			Phase: dto.TaskPhaseSubagent, Task: item.goal, Kind: "subagent",
		})
		if err != nil || !created {
			t.Fatalf("seed task %s: created=%v err=%v", item.id, created, err)
		}
		if _, err := runtime.TaskSetStatus(task.ID, dto.TaskCompleted, "previous done"); err != nil {
			t.Fatal(err)
		}
	}
	runtime.subagentTree.RegisterFork(mainAgentNodeID, []fork.SubagentSpec{
		{ID: "s1", Goal: "audit module A"},
		{ID: "s2", Goal: "audit module B"},
	})
	runtime.subagentTree.CompleteSubagentNode("s1", seed[0].summary, nil)
	runtime.subagentTree.CompleteSubagentNode("s2", seed[1].summary, nil)

	// 若误执行，scripted completer 会返回与 SEEDED 不同的输出——断言结果
	// 只含已保存摘要即可证明未重跑。
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{
		"s1": newScriptedNodeCompleter("FRESH-RUN-A"),
		"s2": newScriptedNodeCompleter("FRESH-RUN-B"),
	})

	result, err := runtime.Agent().DirectDispatch(context.Background(), "fork_subagents",
		`{"subagents":[{"id":"s1","goal":"audit module A"},{"id":"s2","goal":"audit module B"}]}`)
	if err != nil {
		t.Fatalf("fork_subagents failed: %v", err)
	}
	for _, want := range []string{`"status":"completed"`, `"reused":true`, seed[0].summary, seed[1].summary, "复用上次已保存输出"} {
		if !strings.Contains(result, want) {
			t.Errorf("reuse result missing %q:\n%s", want, result)
		}
	}
	for _, fresh := range []string{"FRESH-RUN-A", "FRESH-RUN-B"} {
		if strings.Contains(result, fresh) {
			t.Errorf("reuse result must not re-run subagent (leaked %q):\n%s", fresh, result)
		}
	}
	// task 状态：completed（复用成功），retry_count=1（重试计数保留）。
	for _, item := range seed {
		record, found, err := runtime.ResolveTaskByKey(task.TaskKeyForGoal(item.goal))
		if err != nil || !found {
			t.Fatalf("resolve %s: found=%v err=%v", item.goal, found, err)
		}
		if record.Status != dto.TaskCompleted || record.RetryCount != 1 {
			t.Errorf("task %s after reuse = status=%s retry=%d, want completed/1", item.goal, record.Status, record.RetryCount)
		}
	}
}

// TestForkSubagentsRetryRunsWhenNoStoredOutput 验证无已保存输出时的重试路径：
// 既有 completed task 被重新 fork（结果返回失败后重试）→ bindSubagentTask
// 置 retry（计数自增），子代理树无输出 → 正常重跑 → 完成后状态回到
// completed、retry_count 保留。
func TestForkSubagentsRetryRunsWhenNoStoredOutput(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	injectScriptedCompleters(t, runtime, map[string]agent.Completer{
		"s1": newScriptedNodeCompleter("retry-run: audit module A done"),
	})

	// 第一次执行：正常完成，task 落 completed。
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "fork_subagents",
		`{"subagents":[{"id":"s1","goal":"audit module A"}]}`); err != nil {
		t.Fatal(err)
	}
	before, found, err := runtime.ResolveTaskByKey(task.TaskKeyForGoal("audit module A"))
	if err != nil || !found || before.Status != dto.TaskCompleted || before.RetryCount != 0 {
		t.Fatalf("after first run = %+v found=%v", before, found)
	}

	// 清除子代理树（模拟结果返回失败且无已保存文档）→ 重试必须真正重跑。
	if err := runtime.ClearSubagentTree(); err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Agent().DirectDispatch(context.Background(), "fork_subagents",
		`{"subagents":[{"id":"s1","goal":"audit module A"}]}`)
	if err != nil {
		t.Fatalf("fork retry failed: %v", err)
	}
	if !strings.Contains(result, "retry-run: audit module A done") {
		t.Fatalf("retry must actually re-run subagent, got:\n%s", result)
	}
	after, found, err := runtime.ResolveTaskByKey(task.TaskKeyForGoal("audit module A"))
	if err != nil || !found {
		t.Fatalf("resolve after retry: found=%v err=%v", found, err)
	}
	if after.Status != dto.TaskCompleted || after.RetryCount != 1 {
		t.Errorf("task after retry = status=%s retry=%d, want completed/1", after.Status, after.RetryCount)
	}
}

// TestForkSubagentsValidation 验证护栏：空列表 / 重复 id / 缺 goal / 超数量上限。
func TestForkSubagentsValidation(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	runtime.SetPlanPolicy(dto.PlanPolicy{Effort: "test", MaxNodes: 2})

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
	spec := codec.NodeSpec[plan.SeelexNodeInput]{ID: "summary", Kind: "summary",
		Input: plan.SeelexNodeInput{ID: "summary", Input: "summarize"}}
	n := fork.NewSummaryNode(spec)
	wc := workplanTypes.NewWorkflowContext()
	// 多行输出：紧凑摘要保留前 N 行（*N 语义），超出部分截断。
	// 30 行上限（fork.SummaryMaxLines）可容纳一批 20+ 实例的逐条汇报。
	var zLines []string
	for i := 1; i <= fork.SummaryMaxLines+3; i++ {
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
	// *N 语义：前 N 行都在（N = fork.SummaryMaxLines），超出 N 的行截断。
	if !strings.Contains(out, "- a: a-line-1\n  a-line-2") || !strings.Contains(out, "- z: z-line-1\n  z-line-2") {
		t.Fatalf("summary must carry first %d lines, got:\n%s", fork.SummaryMaxLines, out)
	}
	lastKept := fmt.Sprintf("z-line-%d", fork.SummaryMaxLines)
	if !strings.Contains(out, lastKept) {
		t.Fatalf("summary must keep line %d, got:\n%s", fork.SummaryMaxLines, out)
	}
	dropped := fmt.Sprintf("z-line-%d", fork.SummaryMaxLines+1)
	if strings.Contains(out, dropped) {
		t.Fatalf("summary must drop lines beyond %d, got:\n%s", fork.SummaryMaxLines, out)
	}
	if !strings.Contains(out, "子代理完成情况") {
		t.Fatalf("summary must carry the compact header:\n%s", out)
	}
}

// TestForkPlanNodesCarryEffortLoopBudget 验证 fork 子代理节点循环预算复用
// effort 调节值（dto.PlanPolicy.MaxNodeLoops）：high=48 → 节点 48 轮；未设置
// （lite/medium）→ 回退通用 PlanNodeMaxLoops。
func TestForkSummaryLinesCompact(t *testing.T) {
	cases := []struct {
		output    string
		want      string
		wantTrunc bool
	}{
		{"", "", false},
		{"   \n", "", false},
		{"第一行\n第二行\n", "第一行\n第二行", false},
		{"a\n\nb\nc\nd\ne\nf\ng\n", "a\nb\nc\nd\ne\nf\ng", false}, // 空行跳过；30 行上限内全保留
		{strings.Repeat("x", 200), strings.Repeat("x", fork.SummaryLineLimit) + "…", true},
		{multiLine(fork.SummaryMaxLines + 3), multiLine(fork.SummaryMaxLines), true}, // 超出 30 行截断
	}
	for _, tc := range cases {
		got, full, truncated := fork.ResultSummaryLines(tc.output)
		wantFull := len([]rune(strings.TrimSpace(tc.output)))
		if got != tc.want || full != wantFull || truncated != tc.wantTrunc {
			t.Fatalf("fork.ResultSummaryLines(%q) = (%q,%d,%v), want (%q,%d,%v)",
				tc.output, got, full, truncated, tc.want, wantFull, tc.wantTrunc)
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
