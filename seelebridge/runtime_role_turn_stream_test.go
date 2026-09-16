package seelebridge

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// runtime_role_turn_stream_test.go — 钉住「b 回合的进行中输出不再被丢掉」。
//
// 病灶（用户报的"渲染不及时"）：执行面本来就是流式的（引擎 ChatStream 带 onChunk），
// 但 roleRoundSpec 没有把它接出来，调用方只能等回合结束拿完整回答——评审"正在写
// 什么"在上层完全不可见。
//
// 这里钉的是转发本身：spec.OnDelta 必须逐段收到引擎分片；不挂 OnDelta 时回合照旧
// 跑完（观察面是可选的，不影响任何回合语义）。

// TestRunRoleRoundForwardsOnDelta：装上 OnDelta 时逐段转发，返回值仍是完整回答。
func TestRunRoleRoundForwardsOnDelta(t *testing.T) {
	runtime, engine, _ := newRoleTurnRuntime(t)
	engine.output = "完整回答"
	engine.deltas = []string{"第一段", "第二段"}

	var got []string
	output, err := runtime.runRoleRound(context.Background(), roleRoundSpec{
		MainSessionID: "sess-main",
		RoleName:      "tl",
		RoleSessionID: "advisor:sess-main",
		ToolsPolicy:   dto.ToolPolicyReadonly,
		SystemPrompt:  "你是评审者",
		Input:         "评审这段上下文",
		MaxLoops:      1,
		FreshContext:  true,
		OnDelta:       func(delta string) { got = append(got, delta) },
	})
	if err != nil {
		t.Fatalf("runRoleRound: %v", err)
	}
	if output != "完整回答" {
		t.Fatalf("回合结果应仍是完整回答，得 %q", output)
	}
	if strings.Join(got, ",") != "第一段,第二段" {
		t.Fatalf("OnDelta 应逐段收到引擎分片，得 %v", got)
	}
}

// TestRunRoleRoundWithoutOnDelta：没有观察面时（员工回合/未装配 GUI）回合照旧。
func TestRunRoleRoundWithoutOnDelta(t *testing.T) {
	runtime, engine, _ := newRoleTurnRuntime(t)
	engine.output = "员工回合产出"
	engine.deltas = []string{"分片一"}

	output, err := runtime.runRoleRound(context.Background(), roleRoundSpec{
		MainSessionID: "sess-main",
		RoleName:      "pm",
		RoleSessionID: "goal-a2a-pm",
		ToolsPolicy:   dto.ToolPolicyReadonly,
		SystemPrompt:  "你是 pm",
		Input:         "拆任务",
		MaxLoops:      1,
	})
	if err != nil {
		t.Fatalf("runRoleRound: %v", err)
	}
	if output != "员工回合产出" {
		t.Fatalf("output = %q", output)
	}
}
