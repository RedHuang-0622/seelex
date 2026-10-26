package core

import (
	"context"

	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
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
		WriteSessionID: task_context.SessionIDFromContext,
		ReadSessionID: func(ctx context.Context) string {
			if sessionID := task_context.SessionIDFromContext(ctx); sessionID != "" {
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
