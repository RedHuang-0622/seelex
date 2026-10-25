package assemble

import (
	"context"
	"errors"
	"strings"
	"testing"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	teamdomain "github.com/RedHuang-0622/seelex/backend/multiagent/team"
	"github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
)

func TestRoleToolHookCancelsAtWorkItemFailureThreshold(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	ctx := teamdomain.WithWorkScope(context.Background(), teamdomain.WorkScope{
		MainSessionID: "sess-main", RoleName: "exec", RoleSessionID: "sess-main-team-exec-wi-wi-1",
	})
	canceled := false
	run := &roleToolFailureRun{
		tracker: teamwork.NewWorkItemToolFailureTracker("wi-1", teamwork.ToolFailurePolicy{
			Enabled: true, Threshold: 3,
		}),
		cancel: func() { canceled = true },
	}
	ctx = withRoleToolFailureRun(ctx, run)
	for index := 0; index < 2; index++ {
		runtime.reportRoleToolComplete(ctx, frameworkSession.ToolCallInfo{
			Name: "bash", Error: errors.New("exit status 1"),
		})
	}
	if canceled || run.tracker.Stopped() {
		t.Fatal("阈值之前不得停止回合")
	}
	runtime.reportRoleToolComplete(ctx, frameworkSession.ToolCallInfo{
		Name: "bash", Error: errors.New("exit status 2"),
	})
	if !canceled || !run.tracker.Stopped() {
		t.Fatal("第三次连续显式失败必须取消当前回合")
	}
	if err := run.tracker.Error(); err == nil {
		t.Fatal("停止后必须把结构化原因交回 leader")
	}
}

func TestWorkerRoundSpecKeepsSystemPromptAndWorkItemScope(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	spec := runtime.workerRoleRoundSpec(teamwork.WorkerRequest{
		MainSessionID: "sess-main", TeamID: "team-1", Role: "exec",
		RoleSessionID: "sess-main-team-1-exec-wi-wi-1", WorkItemID: "wi-1",
		Goal: "实现", MaxTurns: 4,
	}, "sess-main", 4)
	if strings.TrimSpace(spec.SystemPrompt) == "" {
		t.Fatal("worker system prompt 不得为空")
	}
	if spec.WorkScope.WorkItemID != "wi-1" || spec.WorkItemID != "wi-1" {
		t.Fatalf("WorkItem scope 丢失: %+v spec=%+v", spec.WorkScope, spec)
	}
}
