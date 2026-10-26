package core

import (
	"math"

	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/store"
)

func newServiceEditForkTargetResolver(service *Service) *applicationedit.TargetResolver {
	return applicationedit.NewTargetResolver(applicationedit.TargetResolverDeps{
		LoadEvents: func(parentID string) ([]store.Event, bool, error) {
			location := service.components.sessions.LocateSession(parentID)
			port, ok := service.Deps.Sessions.(session_runtime.SessionForkPort)
			if !ok {
				return nil, false, nil
			}
			events, err := port.LoadEventRangeWorkspace(location.WorkspaceID, parentID, 1, math.MaxUint64)
			return events, true, err
		},
		IsUserText: func(message model.Message) bool {
			return inputIndexUserText(message) != ""
		},
	})
}
