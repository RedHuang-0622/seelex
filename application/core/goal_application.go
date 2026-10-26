package core

import (
	"context"

	applicationgoal "github.com/RedHuang-0622/seelex/application/goal"
)

func newServiceGoalApplication(service *Service, hostSessionForToolCall func(context.Context) string) *applicationgoal.Application {
	return applicationgoal.New(applicationgoal.Deps{
		Coordinator: func() applicationgoal.CoordinatorPort {
			if service == nil {
				return nil
			}
			return service.components.goal
		},
		Refresh:        service.refreshGoalRuntimeProjection,
		WriteSessionID: sessionIDFromContext,
		ReadSessionID: func(ctx context.Context) string {
			if sessionID := sessionIDFromContext(ctx); sessionID != "" {
				return sessionID
			}
			return hostSessionForToolCall(ctx)
		},
	})
}

func (service *Service) goalApplicationComponent() *applicationgoal.Application {
	if service == nil {
		return nil
	}
	return service.components.goalApplication
}
