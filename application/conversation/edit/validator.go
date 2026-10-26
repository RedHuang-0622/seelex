package edit

import (
	"errors"
	"fmt"
)

// ParentState is the locked parent admission snapshot.
type ParentState struct {
	CurrentSessionID string
	Running          bool
	Queued           bool
	Floor            uint64
	HasWindow        bool
}

// ValidatorDeps supplies the parent snapshot and running error identity.
type ValidatorDeps struct {
	Snapshot         func(parentID string) ParentState
	ChatRunningError error
}

// Validator owns edit admission rules.
type Validator struct {
	deps ValidatorDeps
}

// NewValidator constructs the edit validator.
func NewValidator(deps ValidatorDeps) *Validator {
	return &Validator{deps: deps}
}

// Validate checks current-session, running, queue, revision, window and floor.
func (validator *Validator) Validate(parentID string, parentRevision, expectedRevision, targetSeq uint64) error {
	state := validator.deps.Snapshot(parentID)
	if state.CurrentSessionID != parentID {
		return errors.New("只能编辑当前会话中的用户消息")
	}
	if state.Running {
		return validator.deps.ChatRunningError
	}
	if state.Queued {
		return errors.New("父会话还有排队输入；先等待或处理完队列再编辑")
	}
	if expectedRevision != 0 && expectedRevision != parentRevision {
		return fmt.Errorf(
			"父会话已推进（期望事件 revision %d，当前 %d），请重新打开编辑器",
			expectedRevision,
			parentRevision,
		)
	}
	if !state.HasWindow {
		return errors.New(NoWindowReason)
	}
	if state.Floor > 0 && targetSeq < state.Floor {
		return errors.New(CompressedReason)
	}
	return nil
}
