package execution

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

type subagentRuntimeStub struct {
	contract.RuntimePort
	cleared bool
}

func (runtime *subagentRuntimeStub) ListSubagentRecovery(string) ([]dto.SubagentRecoveryView, error) {
	return []dto.SubagentRecoveryView{{NodeID: "node-a"}}, nil
}
func (runtime *subagentRuntimeStub) ResumeInterruptedSubagents(context.Context, string) (dto.SubagentResumeReport, error) {
	return dto.SubagentResumeReport{Resumed: []string{"node-a"}}, nil
}
func (runtime *subagentRuntimeStub) ResumeSubagent(context.Context, string, string) (dto.SubagentResumeResult, error) {
	return dto.SubagentResumeResult{NodeID: "node-a"}, nil
}
func (runtime *subagentRuntimeStub) ForkSubagents(context.Context, string, []dto.SubagentForkSpec) (string, error) {
	return "node-fork", nil
}
func (runtime *subagentRuntimeStub) ClearSubagentTree() error {
	runtime.cleared = true
	return nil
}

func TestSubagentApplicationDelegatesRuntimeUseCases(t *testing.T) {
	runtime := &subagentRuntimeStub{}
	refreshed := false
	application := NewSubagent(SubagentDeps{
		Runtime: runtime,
		Detail: func(nodeID string) (*model.SubagentDetail, error) {
			return &model.SubagentDetail{Goal: nodeID}, nil
		},
		Refresh: func() { refreshed = true },
	})
	list, err := application.ListRecovery("session-a")
	if err != nil || len(list) != 1 {
		t.Fatalf("ListRecovery = %+v, %v", list, err)
	}
	report, err := application.ResumeInterrupted(context.Background(), "session-a")
	if err != nil || len(report.Resumed) != 1 {
		t.Fatalf("ResumeInterrupted = %+v, %v", report, err)
	}
	resumed, err := application.Resume(context.Background(), "session-a", "node-a")
	if err != nil || resumed.NodeID != "node-a" {
		t.Fatalf("Resume = %+v, %v", resumed, err)
	}
	forked, err := application.Fork(context.Background(), "session-a", nil)
	if err != nil || forked != "node-fork" {
		t.Fatalf("Fork = %q, %v", forked, err)
	}
	detail, err := application.Detail("node-a")
	if err != nil || detail.Goal != "node-a" {
		t.Fatalf("Detail = %+v, %v", detail, err)
	}
	if err := application.ClearTree(); err != nil || !runtime.cleared || !refreshed {
		t.Fatalf("ClearTree = %v, cleared=%v refreshed=%v", err, runtime.cleared, refreshed)
	}
}

func TestSubagentApplicationPreservesRuntimeUnavailable(t *testing.T) {
	application := NewSubagent(SubagentDeps{})
	if _, err := application.ListRecovery("session-a"); err == nil ||
		!strings.Contains(err.Error(), "runtime is not assembled") {
		t.Fatalf("ListRecovery error = %v", err)
	}
}
