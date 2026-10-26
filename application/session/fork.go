package session

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

// ForkDeps supplies the per-session transition lock, the core-owned fork
// transaction and the session switch operation.
type ForkDeps struct {
	TransitionForKey func(string) sync.Locker
	ForkLocked       func(parentID string, request protocol.ForkRequest) (string, error)
	LatestForkCut    func(parentID string) (uint64, error)
	Resume           func(sessionID string) error
}

// ForkApplication owns fork validation, transition locking and post-commit
// switching. Snapshot copying and persistence remain in the injected core host.
type ForkApplication struct {
	deps ForkDeps
}

func NewFork(deps ForkDeps) *ForkApplication {
	return &ForkApplication{deps: deps}
}

func (application *ForkApplication) Fork(parentID string, request protocol.ForkRequest) (string, error) {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return "", errors.New("session ID is required")
	}
	transition := application.deps.TransitionForKey(parentID)
	transition.Lock()
	childID, err := application.deps.ForkLocked(parentID, request)
	transition.Unlock()
	if err != nil {
		return "", err
	}
	if err := application.deps.Resume(childID); err != nil {
		return childID, fmt.Errorf("fork created session %q but switching failed: %w", childID, err)
	}
	return childID, nil
}

func (application *ForkApplication) ForkLatest(parentID string) (string, error) {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return "", errors.New("session ID is required")
	}
	transition := application.deps.TransitionForKey(parentID)
	transition.Lock()
	cut, err := application.deps.LatestForkCut(parentID)
	var childID string
	if err == nil {
		childID, err = application.deps.ForkLocked(parentID, protocol.ForkRequest{EventSeq: cut})
	}
	transition.Unlock()
	if err != nil {
		return "", err
	}
	if err := application.deps.Resume(childID); err != nil {
		return childID, fmt.Errorf("fork created session %q but switching failed: %w", childID, err)
	}
	return childID, nil
}
