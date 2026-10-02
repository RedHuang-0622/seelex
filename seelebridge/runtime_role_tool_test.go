package seelebridge

// runtime_role_tool_test.go — 钉住「员工做工回合的工具活动有实时出口」（2026-10-03）。
//
// 修前事实（Confirmed）：角色回合的 ReAct 钩子只把工具步骤送进 goal 域的 TLStep sink
// （评审过程），而那个 sink 只有 ADVISOR 回合挂得上——**员工（teammate）在做工时前端
// 一无所知**，只能等这一轮跑完（leader 写里程碑那一刻）才看得到结果。子代理侧不是这样：
// `session.ToolEventState` 把子代理工具调用实时投影成 started/completed，前端详情逐帧跟。
//
// 本组用例钉三件事：
//  1. 员工做工回合（ctx 上有 roleWorkScope）→ 每次工具调用发两份活动投影（started/completed），
//     身份三栏（主会话 / 角色名 / 角色会话）齐全，两帧同 ID；
//  2. 非员工回合（ADVISOR 评审）→ **一条员工活动都不发**，且 TLStep sink 那条老路照旧（别把这条改动读成"评审过程没了"）；
//  3. worker 作业回合把身份**塞进本轮的 ctx**——钩子挂在引擎上（活得比一轮长），身份
//     只能按轮从 ctx 读；漏掉这一步，前端又回到"跑完才看得到"。

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/session"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
	"github.com/RedHuang-0622/seelex/seelebridge/teamwork"
)

func TestRoleLoopHooksReportTeammateToolActivity(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	var got []dto.RoleToolActivity
	runtime.SetRoleToolCallback(func(event dto.RoleToolActivity) { got = append(got, event) })

	scope := roleWorkScope{MainSessionID: "s-main", RoleName: "impl_ui", RoleSessionID: "role-impl-ui"}
	hooks := runtime.roleLoopHooks()
	ctx := withRoleWorkScope(context.Background(), scope)
	arguments := `{"path":"gui/app.js"}`
	hooks.OnToolStart(ctx, session.ToolCallInfo{Turn: 2, Name: "read_file", Arguments: arguments})
	hooks.OnToolComplete(ctx, session.ToolCallInfo{
		Turn: 2, Name: "read_file", Arguments: arguments, Result: "…正文…", Duration: 12 * time.Millisecond,
	})

	if len(got) != 2 {
		t.Fatalf("一次工具调用应发 started + completed 两帧，得到 %d 帧：%+v", len(got), got)
	}
	started, completed := got[0], got[1]
	if started.Status != "running" || completed.Status != "success" {
		t.Fatalf("状态词表不对：started=%q completed=%q（want running / success）", started.Status, completed.Status)
	}
	if started.ID == "" || started.ID != completed.ID {
		t.Fatalf("两帧必须同 ID（前端据此 upsert，而不是把一次调用读成两条）：%q vs %q",
			started.ID, completed.ID)
	}
	for _, frame := range got {
		if frame.MainSessionID != "s-main" || frame.RoleName != "impl_ui" || frame.RoleSessionID != "role-impl-ui" {
			t.Fatalf("身份三栏必须齐全（事件按主会话路由、按角色会话归位）：%+v", frame)
		}
		if frame.Name != "read_file" || frame.Turn != 2 {
			t.Fatalf("工具名/轮次要原样带上：%+v", frame)
		}
	}
	if !strings.Contains(completed.Result, "正文") {
		t.Fatalf("completed 帧要带结果：%+v", completed)
	}
	if completed.Duration != 12*time.Millisecond {
		t.Fatalf("completed 帧要带耗时：%+v", completed)
	}
}

func TestRoleLoopHooksTruncateAndReportErrors(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	var got []dto.RoleToolActivity
	runtime.SetRoleToolCallback(func(event dto.RoleToolActivity) { got = append(got, event) })
	ctx := withRoleWorkScope(context.Background(),
		roleWorkScope{MainSessionID: "s-main", RoleName: "impl", RoleSessionID: "role-impl"})

	long := strings.Repeat("证", roleToolActivityLimit+40)
	runtime.roleLoopHooks().OnToolComplete(ctx, session.ToolCallInfo{
		Turn: 1, Name: "bash", Arguments: long, Result: long,
		Error: errProbe{message: long},
	})

	if len(got) != 1 {
		t.Fatalf("应只有一帧 completed，得到 %+v", got)
	}
	frame := got[0]
	if frame.Status != "error" {
		t.Fatalf("工具报错必须落 error 状态：%+v", frame)
	}
	if runes := []rune(frame.Arguments); len(runes) > roleToolActivityLimit+1 {
		t.Fatalf("入参必须按 rune 截断到 %d，得到 %d", roleToolActivityLimit, len(runes))
	}
	if runes := []rune(frame.Result); len(runes) > roleToolActivityLimit+1 {
		t.Fatalf("结果必须按 rune 截断到 %d，得到 %d", roleToolActivityLimit, len(runes))
	}
	if frame.Error == "" {
		t.Fatal("error 帧必须带错误正文")
	}
}

// errProbe 是带可变正文的错误（截断判据需要长正文）。
type errProbe struct{ message string }

func (e errProbe) Error() string { return e.message }

func TestRoleLoopHooksStaySilentWithoutWorkScope(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	reported := 0
	runtime.SetRoleToolCallback(func(dto.RoleToolActivity) { reported++ })

	// ADVISOR 评审回合：ctx 上没有员工做工身份（它挂的是 goal 域的 TLStep sink）。
	var steps []goaldomain.TLStep
	ctx := goaldomain.WithTLStepSink(context.Background(), func(step goaldomain.TLStep) {
		steps = append(steps, step)
	})
	hooks := runtime.roleLoopHooks()
	hooks.OnToolStart(ctx, session.ToolCallInfo{Turn: 1, Name: "read_file", Arguments: "{}"})
	hooks.OnToolComplete(ctx, session.ToolCallInfo{Turn: 1, Name: "read_file", Result: "ok"})

	if reported != 0 {
		t.Fatalf("评审回合不该播报员工工具活动（别把两件事读成一件）：%d 帧", reported)
	}
	if len(steps) != 2 {
		t.Fatalf("评审回合的 TLStep sink 必须照旧收到步骤（这条改动不动那条路）：%+v", steps)
	}
}

func TestWorkerRoundCarriesWorkScopeIntoTheRound(t *testing.T) {
	runtime, engine, _ := newRoleTurnRuntime(t)
	defer runtime.Shutdown()

	if _, err := runtime.runRoleRound(context.Background(), runtime.workerRoleRoundSpec(teamwork.WorkerRequest{
		MainSessionID: "s-main", Role: "impl_ui", RoleSessionID: "role-impl-ui",
		Goal: "把横排记录改成竖排",
	}, "s-main", 3)); err != nil {
		t.Fatalf("runRoleRound(worker): %v", err)
	}

	_, _, calls, ctxs := engine.snapshot()
	if calls != 1 || len(ctxs) != 1 {
		t.Fatalf("假引擎应恰好跑了一轮，得到 calls=%d ctxs=%d", calls, len(ctxs))
	}
	scope, ok := roleWorkScopeFrom(ctxs[0])
	if !ok {
		t.Fatal("员工做工回合必须把身份塞进本轮 ctx：钩子挂在引擎上（活得比一轮长），只能按轮从 ctx 读")
	}
	want := roleWorkScope{MainSessionID: "s-main", RoleName: "impl_ui", RoleSessionID: "role-impl-ui"}
	if scope != want {
		t.Fatalf("本轮身份不符：%+v，want %+v", scope, want)
	}
}

// TestRoleWorkScopeIsPerRound 钉住"身份是按轮的事实"：同一条钩子（同一个 Runtime）在
// 一轮是员工做工、下一轮是评审时，只对员工那一轮出声。
func TestRoleWorkScopeIsPerRound(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	reported := 0
	runtime.SetRoleToolCallback(func(dto.RoleToolActivity) { reported++ })
	hooks := runtime.roleLoopHooks()

	work := withRoleWorkScope(context.Background(),
		roleWorkScope{MainSessionID: "s-main", RoleName: "impl", RoleSessionID: "role-impl"})
	hooks.OnToolStart(work, session.ToolCallInfo{Turn: 1, Name: "read_file", Arguments: "{}"})
	hooks.OnToolStart(context.Background(), session.ToolCallInfo{Turn: 1, Name: "read_file", Arguments: "{}"})
	hooks.OnToolStart(work, session.ToolCallInfo{Turn: 1, Name: "read_file", Arguments: "{}"})

	if reported != 2 {
		t.Fatalf("只有员工做工那两轮该出声，实际 %d 帧", reported)
	}
}
