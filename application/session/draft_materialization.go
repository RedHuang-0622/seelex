package session

import (
	"errors"
	"fmt"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// DraftMaterializationSource describes the draft selected for first input.
type DraftMaterializationSource struct {
	Draft     bool
	DraftID   string
	Workspace *model.WorkspaceInfo
}

// DraftMaterializationDeps supplies workspace, engine, title and publication
// capabilities.
type DraftMaterializationDeps struct {
	Snapshot                  func() DraftMaterializationSource
	BindWorkspace             func(workspace *model.WorkspaceInfo) error
	OpenEngine                func(draftID string, workspace *model.WorkspaceInfo) (string, error)
	Title                     func(firstQuestion string) model.SessionTitle
	Commit                    func(newID string, title model.SessionTitle) uint64
	PublishRuntimeProjections func()
	PublishSnapshot           func(revision uint64, sessionID string)
	RefreshCatalog            func()
	FlushToolRecord           func(sessionID string)
	TouchResident             func(sessionID string)
}

// DraftMaterializationApplication coordinates first-request materialization.
type DraftMaterializationApplication struct {
	deps DraftMaterializationDeps
}

// NewDraftMaterialization constructs the draft materialization application.
func NewDraftMaterialization(deps DraftMaterializationDeps) *DraftMaterializationApplication {
	return &DraftMaterializationApplication{deps: deps}
}

// Materialize turns the current draft into a real session on first input.
func (application *DraftMaterializationApplication) Materialize(firstQuestion string) error {
	source := application.deps.Snapshot()
	if !source.Draft {
		return nil
	}
	if source.DraftID == "" {
		return errors.New("draft session has no pre-assigned session ID")
	}
	if err := application.deps.BindWorkspace(source.Workspace); err != nil {
		return fmt.Errorf("bind project root for new session: %w", err)
	}
	newID, err := application.deps.OpenEngine(source.DraftID, source.Workspace)
	if err != nil {
		return err
	}
	title := application.deps.Title(firstQuestion)
	revision := application.deps.Commit(newID, title)
	application.deps.PublishRuntimeProjections()
	application.deps.PublishSnapshot(revision, newID)
	application.deps.RefreshCatalog()
	application.deps.FlushToolRecord(newID)
	application.deps.TouchResident(newID)
	return nil
}
