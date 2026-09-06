package core

// S1 靶场：工作表格（打点表）跨会话耦合/污染回归。
//
// 场景还原（对应 dev 区多会话运行的真实症状：会话历史/上下文里出现别的
// 会话、别的项目（如 big_result 调研子任务）的活动打点行）：
//   1. 会话 A 是当前任务会话（实时注册表 = A 的 task scope），运行中带
//      plan 打点；
//   2. 会话 B 在后台并行运行（切换 A→B 后 B 走自己的 scope 分区），B
//      自己的 plan 打点落在 B 分区；
//   3. B 准备自己的下一次请求（PrepareExecutionContextFor(B)）时，请求
//      尾部注入的“工作打点表”取自进程级 TaskSnapshot()（= A 的实时注册
//      表）→ B 的上下文被 A 的打点污染、且看不到自己的打点。
//
// 目标约束（会话化修复）：
//   - 打点表必须按“执行该请求的会话”取 TaskSnapshotFor(sessionID)；
//   - B 的上下文只含 B 分区打点，绝不含 A 的活动打点。

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

func TestS1BackgroundSessionContextMustNotCarryActiveSessionWorkTable(t *testing.T) {
	runtime := &fakeRuntime{}
	service := newTestService(t, &fakeEngine{sessionID: "session-a"}, withTestRuntime(runtime))
	defer service.Shutdown()

	// A = 当前任务会话：A 的活动 plan 打点住在实时注册表。
	runtime.currentTaskSession = "session-a"
	runtime.tasks = map[string]dto.TaskRecord{
		"plan:active": {ID: "plan:active", Phase: dto.TaskPhasePlan, Task: "A 前台打点", Status: dto.TaskRunning},
	}

	// B = 后台会话：B 自己的 plan 打点住在 B 的 scope 分区。
	runtime.sessionTaskSnapshots = map[string][]dto.TaskRecord{
		"session-b": {{
			ID: "plan:bg", Key: "plan:bg", Phase: dto.TaskPhasePlan,
			Task: "B 后台打点", Status: dto.TaskRunning,
		}},
	}

	// B 准备自己的下一次请求：请求尾部打点块必须来自 B 分区。
	out, err := service.components.context.PrepareExecutionContextFor("session-b", "req-bg", "继续")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, workTableTraceMarkerOpen) {
		t.Fatalf("background session request must carry its own worktable block: %q", out)
	}
	if !strings.Contains(out, "B 后台打点") {
		t.Fatalf("background session lost its own worktable rows: %q", out)
	}
	if strings.Contains(out, "A 前台打点") {
		t.Fatalf("cross-session worktable pollution: active session rows leaked into background context: %q", out)
	}
}

// TestWorkTableTraceBlockForScopesBySession 直接验证打点块构造按会话取数：
// 后台会话返回自身分区行，活跃会话（空/当前）返回实时注册表行。

func TestWorkTableTraceBlockForScopesBySession(t *testing.T) {
	runtime := &fakeRuntime{}
	service := newTestService(t, &fakeEngine{sessionID: "session-a"}, withTestRuntime(runtime))
	defer service.Shutdown()

	runtime.currentTaskSession = "session-a"
	runtime.tasks = map[string]dto.TaskRecord{
		"plan:active": {ID: "plan:active", Phase: dto.TaskPhasePlan, Task: "A 前台打点", Status: dto.TaskRunning},
	}
	runtime.sessionTaskSnapshots = map[string][]dto.TaskRecord{
		"session-b": {{ID: "plan:bg", Key: "plan:bg", Phase: dto.TaskPhasePlan, Task: "B 后台打点", Status: dto.TaskRunning}},
	}

	bBlock := service.workTableTraceBlockFor("session-b")
	if !strings.Contains(bBlock, "B 后台打点") || strings.Contains(bBlock, "A 前台打点") {
		t.Fatalf("session-scoped block wrong: %q", bBlock)
	}
	activeBlock := service.workTableTraceBlockFor("")
	if !strings.Contains(activeBlock, "A 前台打点") || strings.Contains(activeBlock, "B 后台打点") {
		t.Fatalf("active-session block wrong: %q", activeBlock)
	}
}
