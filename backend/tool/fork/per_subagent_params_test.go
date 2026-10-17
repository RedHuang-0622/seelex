package fork

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// 判据本身（进程级天花板、0 = 未声明、effort 节点上限不参与）已随 M2-B 归一，
// 用例搬到唯一实现处：`backend/limit/loop/loop_test.go` 的
// TestValidateSubagentMaxLoopsUsesProcessCeiling 与 …MessagesUnchanged。
// 本文件只保留"这条判据在工具入口真的生效"的用例（下面那条）。

// TestForkToolHandleRejectsSubagentLoopsAboveProcessCeiling：同一条判据在工具入口
// 上真的生效（不是只有单测里的自由函数被覆盖）。
func TestForkToolHandleRejectsSubagentLoopsAboveProcessCeiling(t *testing.T) {
	tool := NewTool(Deps{
		CurrentSubagentPolicy: func(context.Context) dto.SubagentPolicy {
			return dto.SubagentPolicy{Effort: "high", MaxNodeLoops: 48}
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
