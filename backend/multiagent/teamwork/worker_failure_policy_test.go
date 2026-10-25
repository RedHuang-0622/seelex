package teamwork

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/RedHuang-0622/Seele/jobs"
)

type failurePolicyRunner struct{}

func (failurePolicyRunner) RunWorker(context.Context, WorkerRequest, jobs.Sink) error {
	return &WorkItemToolFailureError{
		WorkItemID: "wi-preserve", Threshold: 3, Tool: "bash",
		Cause: context.DeadlineExceeded,
	}
}

type recordingSettler struct {
	calls int
}

func (s *recordingSettler) SettleWorkItem(context.Context, WorkerRequest, error) error {
	s.calls++
	return nil
}

type recordingSink struct {
	state jobs.State
}

func (s *recordingSink) Note(string)                         {}
func (s *recordingSink) SignalBytes()                        {}
func (s *recordingSink) Exit(int)                            {}
func (s *recordingSink) Complete(state jobs.State, _ string) { s.state = state }

// TestWorkItemToolFailureBypassesSettlePreservingArtifacts 钉住失败返回策略的
// 唯一回滚定义：只把控制权交还 leader，不进入自动合并/验收/现场释放链。
func TestWorkItemToolFailureBypassesSettlePreservingArtifacts(t *testing.T) {
	payload, err := json.Marshal(WorkerRequest{
		Role: "exec", RoleSessionID: "wi-session", WorkItemID: "wi-preserve",
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	settler := &recordingSettler{}
	sink := &recordingSink{}
	executor := &workerExecutor{
		runner:   failurePolicyRunner{},
		settler:  settler,
		maxTurns: 4,
	}
	if err := executor.Start(context.Background(), jobs.Spec{Payload: payload}, sink); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if settler.calls != 0 {
		t.Fatalf("tool-failure return must not settle/merge/release, calls=%d", settler.calls)
	}
	if sink.state != jobs.StateFailed {
		t.Fatalf("worker terminal state = %q, want failed", sink.state)
	}
}
