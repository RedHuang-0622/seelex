package assemble

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// TestRoleSessionHonoursDeclaredMaxLoops：一个 teammate **自己声明的**回合上限真的
// 落到角色会话的引擎上——这是成员 `max_turns` 那条链的最后一跳
// （成员条目 → WorkerRequest.MaxTurns → roleRoundSpec.MaxLoops →
// session.SessionConfig.MaxLoops）。
//
// 与 fork 的 per-subagent max_loops 对称，也是 2026-10-08 口径的一半：工作单元跑多少
// 轮由**单元自己（或装配层常量 roleTurnMaxLoops）**决定，effort 档位（reasoning
// effort 的预设）不参与——档位切换不许连带改变 teammate 的次数。
func TestRoleSessionHonoursDeclaredMaxLoops(t *testing.T) {
	runtime, engine, _ := newRoleTurnRuntime(t)

	if _, err := runtime.roleSessionFor(roleRoundSpec{
		MainSessionID: "sess-main",
		RoleName:      "pm",
		RoleSessionID: "sess-role-pm",
		ToolsPolicy:   dto.ToolPolicyReadWrite,
		MaxLoops:      20,
	}); err != nil {
		t.Fatalf("roleSessionFor: %v", err)
	}

	if _, maxLoops, _, _ := engine.snapshot(); maxLoops != 20 {
		t.Fatalf("声明的回合上限未落到角色会话引擎：maxLoops = %d, want 20", maxLoops)
	}
}

// TestWorkerTurnLimitPrefersDeclaredAndFallsBackToConstant：没声明回合上限的 teammate
// 作业回退**装配层常量** roleTurnMaxLoops——不是"零轮"，也不是 effort 档位派生的值
// （2026-10-08 口径：档位切换不许连带改变工作单元的次数）。
func TestWorkerTurnLimitPrefersDeclaredAndFallsBackToConstant(t *testing.T) {
	if got := workerTurnLimit(20); got != 20 {
		t.Fatalf("载荷声明了 20 轮就必须用 20：得到 %d", got)
	}
	if got := workerTurnLimit(0); got != roleTurnMaxLoops {
		t.Fatalf("没声明时回退装配层常量：得到 %d, want %d", got, roleTurnMaxLoops)
	}
}
