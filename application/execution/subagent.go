package execution

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// SubagentDeps supplies the Runtime subagent operations and read projection.
type SubagentDeps struct {
	Runtime contract.RuntimePort
	Detail  func(nodeID string) (*model.SubagentDetail, error)
	Refresh func()
}

// SubagentApplication owns Application-level subagent recovery and fork use
// cases. The Runtime and subagent view coordinator remain fact owners.
type SubagentApplication struct {
	deps SubagentDeps
}

// NewSubagent constructs the subagent application.
func NewSubagent(deps SubagentDeps) *SubagentApplication {
	return &SubagentApplication{deps: deps}
}

// ListRecovery lists interrupted subagent recovery entries.
func (application *SubagentApplication) ListRecovery(sessionID string) ([]dto.SubagentRecoveryView, error) {
	if application == nil || application.deps.Runtime == nil {
		return nil, fmt.Errorf("list subagent recovery: runtime is not assembled")
	}
	return application.deps.Runtime.ListSubagentRecovery(sessionID)
}

// ResumeInterrupted resumes all interrupted subagents for one session.
func (application *SubagentApplication) ResumeInterrupted(ctx context.Context, sessionID string) (dto.SubagentResumeReport, error) {
	if application == nil || application.deps.Runtime == nil {
		return dto.SubagentResumeReport{}, fmt.Errorf("resume subagents: runtime is not assembled")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return application.deps.Runtime.ResumeInterruptedSubagents(ctx, sessionID)
}

// Resume resumes one subagent.
func (application *SubagentApplication) Resume(ctx context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error) {
	if application == nil || application.deps.Runtime == nil {
		return dto.SubagentResumeResult{}, fmt.Errorf("resume subagent: runtime is not assembled")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return application.deps.Runtime.ResumeSubagent(ctx, sessionID, nodeID)
}

// Fork creates child subagents from fork specifications.
func (application *SubagentApplication) Fork(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error) {
	if application == nil || application.deps.Runtime == nil {
		return "", fmt.Errorf("fork subagents: runtime is not assembled")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return application.deps.Runtime.ForkSubagents(ctx, sessionID, specs)
}

// Detail reads one subagent detail projection.
func (application *SubagentApplication) Detail(nodeID string) (*model.SubagentDetail, error) {
	return application.deps.Detail(nodeID)
}

// ClearTree clears the Runtime subagent tree and refreshes the projection.
func (application *SubagentApplication) ClearTree() error {
	if err := application.deps.Runtime.ClearSubagentTree(); err != nil {
		return err
	}
	application.deps.Refresh()
	return nil
}
