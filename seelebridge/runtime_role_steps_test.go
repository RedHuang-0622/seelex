package seelebridge

// runtime_role_steps_test.go — 钉住「角色回合的工具步骤接到 goal 过程观察面」
// 这条接线（b 回合的评审过程可见性的**唯一**来源）。
//
// 它测的是钩子本体（roleLoopHooks）而不是整轮：注入式假引擎会绕过 newRoleEngine
// 的钩子装配，因此整轮测不了这条；钩子是"ctx 取身份/sink → 上报"的纯搬运，单测
// 足够钉住契约，且不需要真实 LLM。
//
// 2026-10-03：钩子从包级函数变成 *Runtime 方法（它现在还负责**员工做工回合**的实时
// 工具活动，见 runtime_role_tool_test.go），因此这里的用例改成对着一个 Runtime 调。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/session"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

func TestRoleLoopHooksReportToolStepsToGoalSink(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	hooks := runtime.roleLoopHooks()
	if hooks == nil || hooks.OnToolStart == nil || hooks.OnToolComplete == nil {
		t.Fatal("角色回合必须挂工具钩子：没有它，ADVISOR 的评审过程完全不可见")
	}
	var got []goaldomain.TLStep
	ctx := goaldomain.WithTLStepSink(context.Background(), func(step goaldomain.TLStep) {
		got = append(got, step)
	})

	hooks.OnToolStart(ctx, session.ToolCallInfo{Turn: 1, Name: "read_file", Arguments: `{"path":"README.md"}`})
	hooks.OnToolComplete(ctx, session.ToolCallInfo{Turn: 1, Name: "read_file", Result: "命中 3 处"})

	if len(got) != 2 {
		t.Fatalf("一次工具调用应上报两个步骤（调用 + 返回），得 %+v", got)
	}
	if got[0].Kind != "tool" || got[0].Name != "read_file" || got[0].Args != `{"path":"README.md"}` || got[0].Turn != 1 {
		t.Fatalf("调用步骤形状不对: %+v", got[0])
	}
	if got[1].Kind != "tool_result" || got[1].Result != "命中 3 处" || got[1].Err != "" {
		t.Fatalf("返回步骤形状不对: %+v", got[1])
	}
}

func TestRoleLoopHooksCarryToolErrors(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	var got []goaldomain.TLStep
	ctx := goaldomain.WithTLStepSink(context.Background(), func(step goaldomain.TLStep) {
		got = append(got, step)
	})
	runtime.roleLoopHooks().OnToolComplete(ctx, session.ToolCallInfo{
		Turn: 2, Name: "bash", Error: errors.New("退出码 1"),
	})
	if len(got) != 1 || got[0].Err != "退出码 1" {
		t.Fatalf("工具错误必须随步骤上报（面板要高亮失败）: %+v", got)
	}
}

// TestRoleLoopHooksAreNoOpWithoutSink：既不是员工做工回合（ctx 上没有工作身份），
// 也没有挂 goal sink —— 钩子必须按"不观察"处理：既不 panic，也不产生任何副作用。
func TestRoleLoopHooksAreNoOpWithoutSink(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	reported := 0
	runtime.SetRoleToolCallback(func(dto.RoleToolActivity) { reported++ })
	hooks := runtime.roleLoopHooks()
	hooks.OnToolStart(context.Background(), session.ToolCallInfo{Name: "read_file", Arguments: "x"})
	hooks.OnToolComplete(context.Background(), session.ToolCallInfo{Name: "read_file", Error: errors.New("x")})
	if reported != 0 {
		t.Fatalf("没有工作身份的回合不该播报员工工具活动，得 %d 帧", reported)
	}
}

// TestRoleLoopHooksTruncateOversizedPayloads：参数/结果进面板前有界（面板是摘要，
// 不是正文），且截断后长度不超过各自上限。
func TestRoleLoopHooksTruncateOversizedPayloads(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	var got []goaldomain.TLStep
	ctx := goaldomain.WithTLStepSink(context.Background(), func(step goaldomain.TLStep) {
		got = append(got, step)
	})
	hooks := runtime.roleLoopHooks()
	hooks.OnToolStart(ctx, session.ToolCallInfo{
		Name: "grep_search", Arguments: strings.Repeat("a", goaldomain.StepArgsLimit*3),
	})
	hooks.OnToolComplete(ctx, session.ToolCallInfo{
		Name: "grep_search", Result: strings.Repeat("b", goaldomain.StepResultLimit*3),
	})
	if len(got) != 2 {
		t.Fatalf("应上报两个步骤，得 %d", len(got))
	}
	if runes := []rune(got[0].Args); len(runes) > goaldomain.StepArgsLimit {
		t.Fatalf("参数应截到 %d rune，得 %d", goaldomain.StepArgsLimit, len(runes))
	}
	if runes := []rune(got[1].Result); len(runes) > goaldomain.StepResultLimit {
		t.Fatalf("结果应截到 %d rune，得 %d", goaldomain.StepResultLimit, len(runes))
	}
}
