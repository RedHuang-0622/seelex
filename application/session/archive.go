package session

import (
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// ArchiveDeps supplies session record, engine and release callbacks.
type ArchiveDeps struct {
	Residency      ResidencyPort
	Transition     func(sessionID string) sync.Locker
	Gate           func(sessionID string) ArchiveGate
	Load           func(sessionID string) (model.SessionRecord, bool, error)
	Flush          func(sessionID string) error
	MarkArchived   func(sessionID string) error
	RefreshProject func(sessionID string)
	EngineResident func(sessionID string) bool
	EngineLoaded   func(sessionID string) bool
	EngineUnload   func(sessionID string) error
	ForgetResident func(sessionID string)
	ReleaseSession func(sessionID string)
}

// ArchiveApplication adapts the application host to residency.Archive.
type ArchiveApplication struct {
	deps ArchiveDeps
}

// NewArchive constructs the archive application.
func NewArchive(deps ArchiveDeps) *ArchiveApplication {
	return &ArchiveApplication{deps: deps}
}

// Archive archives a session while preserving the middle-layer decision path.
func (application *ArchiveApplication) Archive(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	transition := application.deps.Transition(sessionID)
	transition.Lock()
	defer transition.Unlock()
	if application.deps.Residency == nil {
		return ErrResidencyUnavailable
	}
	return application.deps.Residency.Archive(ArchiveUseCaseDeps{
		Gate: func() ArchiveGate { return application.deps.Gate(sessionID) },
		Store: ArchiveStore{
			Load:           application.deps.Load,
			Flush:          application.deps.Flush,
			MarkArchived:   application.deps.MarkArchived,
			RefreshProject: application.deps.RefreshProject,
		},
		Engine: ArchiveEngine{
			Resident:       application.deps.EngineResident,
			Loaded:         application.deps.EngineLoaded,
			Unload:         application.deps.EngineUnload,
			ForgetResident: application.deps.ForgetResident,
		},
		ReleaseAsync: application.deps.ReleaseSession,
	}, sessionID)
}
