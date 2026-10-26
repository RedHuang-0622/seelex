package query

import (
	"errors"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// SnapshotReadState is the host-owned hot/cold decision input.
type SnapshotReadState struct {
	Resident        bool
	ViewSessionID   string
	ContentUnloaded bool
}

// SnapshotReadDeps supplies state sampling and both snapshot sources.
type SnapshotReadDeps struct {
	State            func(sessionID string) SnapshotReadState
	ColdSnapshot     func(sessionID string) (model.SessionSnapshot, error)
	ResidentSnapshot func(sessionID string) (model.SessionSnapshot, error)
}

// ReadSnapshot chooses the cold persisted baseline or the resident assembly
// without taking locks itself; the State callback owns the host lock boundary.
func ReadSnapshot(sessionID string, deps SnapshotReadDeps) (model.SessionSnapshot, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return model.SessionSnapshot{}, errors.New("session ID is required")
	}
	state := deps.State(sessionID)
	if !state.Resident {
		return deps.ColdSnapshot(sessionID)
	}
	if sessionID != state.ViewSessionID && state.ContentUnloaded {
		if snapshot, err := deps.ColdSnapshot(sessionID); err == nil {
			return snapshot, nil
		}
	}
	return deps.ResidentSnapshot(sessionID)
}
