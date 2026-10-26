// Package query owns read-only session projections for the Application layer.
// It does not own command execution, session lifecycle or persistent stores.
package query

import (
	"context"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	switching "github.com/RedHuang-0622/seelex/middle/switch"
)

// Deps supplies the authoritative session directory, metadata and durable read
// capabilities. Callbacks retain the host's existing lock and I/O boundaries.
type Deps struct {
	ViewMu       *sync.RWMutex
	CatalogRows  func() []model.SessionInfo
	WithLiveRows func([]model.SessionInfo) []model.SessionInfo
	EnrichRows   func([]model.SessionInfo) []model.SessionInfo
	Meta         func() switching.MetaDeps
	Workspace    contract.WorkspacePort
	WaitCatalog  func(context.Context) error
	SnapshotOf   func(string) (model.SessionSnapshot, error)
	Transcript   func(string, uint64, uint64) ([]model.TranscriptEvent, error)
	InputIndex   func(string) (model.SessionInputIndex, error)
}

// Application exposes the session read surface without holding a Service.
type Application struct {
	deps Deps
}

// New constructs the read-only session query application.
func New(deps Deps) *Application {
	return &Application{deps: deps}
}

// ListSessions returns the authoritative session directory. The shared read
// lock covers only in-memory catalog composition.
func (application *Application) ListSessions() []model.SessionInfo {
	if application == nil || application.deps.ViewMu == nil {
		return nil
	}
	application.deps.ViewMu.RLock()
	defer application.deps.ViewMu.RUnlock()
	rows := append([]model.SessionInfo(nil), application.deps.CatalogRows()...)
	if application.deps.WithLiveRows != nil {
		rows = application.deps.WithLiveRows(rows)
	}
	if application.deps.EnrichRows != nil {
		rows = application.deps.EnrichRows(rows)
	}
	return rows
}

// ProjectRootFor reads a session's workspace binding root without falling back
// to the current view's workspace.
func (application *Application) ProjectRootFor(sessionID string) string {
	if application == nil || application.deps.Workspace == nil {
		return ""
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	if workspace, ok := application.deps.Workspace.SessionWorkspace(sessionID); ok {
		return strings.TrimSpace(workspace.RootPath)
	}
	return ""
}

// SessionMeta reads authoritative session display metadata.
func (application *Application) SessionMeta(sessionID string) (model.SessionMeta, error) {
	return switching.ReadMeta(application.deps.Meta(), sessionID)
}

// WaitCatalogRefresh waits for the catalog worker without taking ViewMu.
func (application *Application) WaitCatalogRefresh(ctx context.Context) error {
	if application == nil || application.deps.WaitCatalog == nil {
		return nil
	}
	return application.deps.WaitCatalog(ctx)
}

// SnapshotOf reads one authoritative session snapshot.
func (application *Application) SnapshotOf(sessionID string) (model.SessionSnapshot, error) {
	return application.deps.SnapshotOf(sessionID)
}

// GetSessionTranscript reads an authoritative transcript range.
func (application *Application) GetSessionTranscript(sessionID string, fromSeq, toSeq uint64) ([]model.TranscriptEvent, error) {
	return application.deps.Transcript(sessionID, fromSeq, toSeq)
}

// SessionInputIndex reads the bounded full user-input index.
func (application *Application) SessionInputIndex(sessionID string) (model.SessionInputIndex, error) {
	return application.deps.InputIndex(sessionID)
}
