package edit

import (
	"context"
	"fmt"
	"sync"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/store"
)

// ProcessDeps supplies target, validation and session callbacks.
type ProcessDeps struct {
	Resolve          func(parentID string, targetSeq uint64) (store.Event, uint64, uint64, error)
	Validate         func(parentID string, parentRevision, expectedRevision, targetSeq uint64) error
	TransitionForKey func(sessionID string) sync.Locker
	Fork             func(parentID string, request model.ForkRequest) (string, error)
	Resume           func(childID string) error
	Submit           func(ctx context.Context, childID, text string) error
	Rollback         func(parentID, childID string, cause error) error
}

// ProcessApplication owns atomic edit-and-fork child orchestration.
type ProcessApplication struct {
	deps ProcessDeps
}

// NewProcess constructs the edit/fork process.
func NewProcess(deps ProcessDeps) *ProcessApplication {
	return &ProcessApplication{deps: deps}
}

// Run resolves, validates, forks, resumes, submits and rolls back on failure.
func (application *ProcessApplication) Run(ctx context.Context, parentID string, request model.EditUserMessageForkRequest) (model.EditUserMessageForkResult, error) {
	target, cut, parentRevision, err := application.deps.Resolve(parentID, request.TargetEventSeq)
	if err != nil {
		return model.EditUserMessageForkResult{}, err
	}
	if err := application.deps.Validate(parentID, parentRevision, request.ExpectedRevision, request.TargetEventSeq); err != nil {
		return model.EditUserMessageForkResult{}, err
	}
	transition := application.deps.TransitionForKey(parentID)
	transition.Lock()
	childID, err := application.deps.Fork(parentID, model.ForkRequest{
		EventSeq: cut, SourceEventSeq: target.Seq, SourceMessageID: target.MessageID,
		ExpectedEventSeq: request.ExpectedRevision,
	})
	transition.Unlock()
	if err != nil {
		return model.EditUserMessageForkResult{}, err
	}
	if err := application.deps.Resume(childID); err != nil {
		return model.EditUserMessageForkResult{}, application.deps.Rollback(
			parentID, childID, fmt.Errorf("切换编辑分支失败: %w", err),
		)
	}
	if err := application.deps.Submit(ctx, childID, request.NewText); err != nil {
		return model.EditUserMessageForkResult{}, application.deps.Rollback(
			parentID, childID, fmt.Errorf("提交编辑后的输入失败: %w", err),
		)
	}
	return model.EditUserMessageForkResult{ChildSessionID: childID, ForkPoint: cut}, nil
}
