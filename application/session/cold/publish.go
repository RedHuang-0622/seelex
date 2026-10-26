package cold

import (
	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

// PublishDeps supplies context, task, runtime and LRU publication callbacks.
type PublishDeps struct {
	SetGlobalPrompt   func(prompt string)
	AttachContext     func(location session_runtime.Location, sessionID string) error
	SwitchTasks       func(sessionID string, tasks []dto.TaskRecord)
	ClearSubagentTree func()
	RestoreAnchors    func(sessionID string)
	RefreshRuntime    func(sessionID string)
	PublishSnapshot   func(revision uint64, sessionID string)
	PublishRuntime    func()
	RefreshCatalog    func()
	TouchResident     func(sessionID string)
	TouchContent      func(sessionID string)
}

// PublishApplication owns post-unlock cold-load publication.
type PublishApplication struct {
	deps PublishDeps
}

// NewPublish constructs the cold-publish application.
func NewPublish(deps PublishDeps) *PublishApplication {
	return &PublishApplication{deps: deps}
}

// Publish attaches context and publishes runtime/snapshot state.
func (application *PublishApplication) Publish(
	sessionID string,
	location session_runtime.Location,
	record model.SessionRecord,
	mayActivate bool,
	revision uint64,
	systemPrompt string,
) error {
	if mayActivate {
		application.deps.SetGlobalPrompt(systemPrompt)
	}
	if err := application.deps.AttachContext(location, sessionID); err != nil {
		return err
	}
	if mayActivate {
		application.deps.SwitchTasks(sessionID, record.Tasks)
		application.deps.ClearSubagentTree()
		application.deps.RestoreAnchors(sessionID)
		application.deps.RefreshRuntime(sessionID)
		application.deps.PublishSnapshot(revision, sessionID)
		application.deps.PublishRuntime()
	}
	application.deps.RefreshCatalog()
	application.deps.TouchResident(sessionID)
	application.deps.TouchContent(sessionID)
	return nil
}
