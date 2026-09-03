package core

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// TestTaskStatusForSubagentInterrupted（G4 stale）：崩溃遗留节点经树恢复为
// interrupted 后，工作表条目映射为 interrupted（不再是 running 的假象）；
// 未知状态按保守口径同样标 interrupted。
func TestTaskStatusForSubagentInterrupted(t *testing.T) {
	cases := []struct {
		sub  dto.SubAgentNodeStatus
		want dto.TaskStatus
	}{
		{sub: dto.SubAgentQueued, want: dto.TaskQueued},
		{sub: dto.SubAgentRunning, want: dto.TaskRunning},
		{sub: dto.SubAgentDone, want: dto.TaskCompleted},
		{sub: dto.SubAgentFailed, want: dto.TaskFailed},
		{sub: dto.SubAgentInterrupted, want: dto.TaskInterrupted},
		{sub: "", want: dto.TaskInterrupted},
	}
	for _, tc := range cases {
		if got := taskStatusForSubagent(tc.sub); got != tc.want {
			t.Fatalf("taskStatusForSubagent(%q) = %q, want %q", tc.sub, got, tc.want)
		}
	}
}
