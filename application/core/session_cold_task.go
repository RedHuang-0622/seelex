package core

import (
	applicationsessioncold "github.com/RedHuang-0622/seelex/application/session/cold"
	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	task_context "github.com/RedHuang-0622/seelex/backend/engine/task"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func newServiceSessionColdTask(service *Service) *applicationsessioncold.TaskApplication {
	return applicationsessioncold.NewTask(applicationsessioncold.TaskDeps{
		LoadCompactions: func(location session_runtime.Location, sessionID string) ([]model.ContextCompaction, bool, error) {
			return service.components.sessions.LoadSessionCompactionRecords(location, sessionID)
		},
		LatestUser: func(messages []model.Message) string {
			return service.components.sessions.LatestUserContent(messages)
		},
		RetainedFrom: task_context.RetainedFromForCompactions,
		Restore: func(sessionID string, state task_context.RestoredTaskState) {
			service.components.tasks.RestoreSessionTaskLockedFor(sessionID, state)
		},
		Reset: func() {
			service.components.tasks.ResetForNewSessionLocked()
		},
	})
}
