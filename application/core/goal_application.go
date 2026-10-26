package core

import applicationgoal "github.com/RedHuang-0622/seelex/application/goal"

func newServiceGoalApplication(service *Service) *applicationgoal.Application {
	return applicationgoal.New(applicationgoal.Deps{
		Coordinator: func() applicationgoal.CoordinatorPort {
			if service == nil {
				return nil
			}
			return service.components.goal
		},
		Refresh: service.refreshGoalRuntimeProjection,
	})
}

func (service *Service) goalApplicationComponent() *applicationgoal.Application {
	if service == nil {
		return nil
	}
	return service.components.goalApplication
}
