package assembly

import (
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// taskRecordsForSession selects the live task registry or the session scope.
// WorkTable reads and trace reads share this single predicate.
func (state *serviceState) taskRecordsForSession(sessionID, viewSessionID string) []dto.TaskRecord {
	if state == nil || state.Deps.Runtime == nil {
		return nil
	}
	forTasks, ok := state.Deps.Runtime.(interface{ TaskSnapshotFor(string) []dto.TaskRecord })
	if !ok {
		return state.Deps.Runtime.TaskSnapshot()
	}
	scope := sessionID
	if scope == "" || scope == viewSessionID {
		scope = ""
	}
	return forTasks.TaskSnapshotFor(scope)
}
