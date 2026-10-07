package fork

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/plan"
)

// TestForkPlanNodesHonourPerSubagentMaxLoops：**一个工作单元自己的执行参数**在
// subagent 层的入参落点——同批里逐条不同，而不是全批一个常量。
func TestForkPlanNodesHonourPerSubagentMaxLoops(t *testing.T) {
	tool := forkLoopHarness(plan.PlanPolicy{Effort: "high", MaxNodeLoops: 48}, 15)

	got := buildLoops(t, tool,
		SubagentSpec{ID: "read", Goal: "只读审查", MaxLoops: 8},
		SubagentSpec{ID: "default", Goal: "没带就回退配置默认"},
		SubagentSpec{ID: "migrate", Goal: "长链条迁移", MaxLoops: 48},
	)
	want := map[string]int{"read": 8, "default": 15, "migrate": 48}
	for id, loops := range want {
		if got[id] != loops {
			t.Fatalf("node %q max_loops = %d, want %d（全批 %v）", id, got[id], loops, got)
		}
	}
}

// TestValidateSubagentLoopsUsesProcessCeiling：循环声明的判据只有**进程级天花板**。
//
// 三条钉子：
//   - 越过天花板（9999）与负数在入口就被拒；
//   - 比 effort 档位节点上限（high=48）大的 60 **可以**——档位不是工作单元次数的
//     判据（2026-10-08 口径：设置 reasoning effort 不得污染 subagent/teammate 的
//     循环次数）；
//   - 0 = 没声明，留给本批默认，不算错。
func TestValidateSubagentLoopsUsesProcessCeiling(t *testing.T) {
	const ceiling = 9999
	if err := validateSubagentLoops(ceiling, SubagentSpec{ID: "s1", MaxLoops: ceiling + 1}); err == nil {
		t.Fatalf("越过进程级循环天花板（%d）的 max_loops 必须在入口被拒", ceiling)
	}
	if err := validateSubagentLoops(ceiling, SubagentSpec{ID: "s1", MaxLoops: -1}); err == nil {
		t.Fatal("负数 max_loops 必须在入口被拒")
	}
	if err := validateSubagentLoops(ceiling, SubagentSpec{ID: "s1", MaxLoops: 60}); err != nil {
		t.Fatalf("effort 档位的节点上限（high=48）不得再拦工作单元自己的声明：%v", err)
	}
	if err := validateSubagentLoops(ceiling, SubagentSpec{ID: "s1"}); err != nil {
		t.Fatalf("没声明（0）不是错误，留给本批默认：%v", err)
	}
	// 天花板未配置（0 = 不限）时不判上限：配置层没给数就不许自造一个。
	if err := validateSubagentLoops(0, SubagentSpec{ID: "s1", MaxLoops: 100000}); err != nil {
		t.Fatalf("未配置天花板时不应判上限：%v", err)
	}
}

// TestForkToolHandleRejectsSubagentLoopsAboveProcessCeiling：同一条判据在工具入口
// 上真的生效（不是只有单测里的自由函数被覆盖）。
func TestForkToolHandleRejectsSubagentLoopsAboveProcessCeiling(t *testing.T) {
	tool := NewTool(Deps{
		CurrentPlanPolicy: func() plan.PlanPolicy {
			return plan.PlanPolicy{Effort: "high", MaxNodeLoops: 48}
		},
		LoopLimitCeiling: 9999,
	})
	if _, err := tool.Handle(t.Context(), `{"subagents":[{"id":"s1","goal":"x","max_loops":10000}]}`); err == nil {
		t.Fatal("超过进程级循环天花板的 max_loops 必须在工具入口被拒")
	}
	if _, err := tool.Handle(t.Context(), `{"subagents":[{"id":"s1","goal":"x","max_loops":-1}]}`); err == nil {
		t.Fatal("负数 max_loops 必须在工具入口被拒")
	}
}

// TestSubagentsContractDescriptionMentionsPerSubagentLoops：工具描述是**给模型看的
// 文档面**——参数进了 schema 而描述没提，模型不会用；而且它必须讲清缺省来自配置，
// 不是来自 effort 档位。
func TestSubagentsContractDescriptionMentionsPerSubagentLoops(t *testing.T) {
	if !strings.Contains(SubagentsContractDescription, "max_loops") {
		t.Fatalf("fork_subagents 描述必须提到 per-subagent max_loops，实际：%s", SubagentsContractDescription)
	}
	if !strings.Contains(SubagentsContractDescription, "plan_node_max_loops") {
		t.Fatalf("描述里的缺省来源必须是配置 plan_node_max_loops，实际：%s", SubagentsContractDescription)
	}
	if strings.Contains(SubagentsContractDescription, "effort policy's node cap") {
		t.Fatalf("描述不得再宣称 effort 档位卡子代理的循环次数，实际：%s", SubagentsContractDescription)
	}
}
