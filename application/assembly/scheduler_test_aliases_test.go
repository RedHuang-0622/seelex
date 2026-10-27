package assembly

import applicationscheduler "github.com/RedHuang-0622/seelex/application/admin/scheduler"

type schedulerApplication = applicationscheduler.SchedulerApplication
type schedulerApplicationDeps = applicationscheduler.SchedulerDeps

func newSchedulerApplication(deps schedulerApplicationDeps) *schedulerApplication {
	return applicationscheduler.NewScheduler(deps)
}

type scheduledRunApplication = applicationscheduler.RunApplication
type scheduledRunApplicationDeps = applicationscheduler.RunDeps

func newScheduledRunApplication(deps scheduledRunApplicationDeps) *scheduledRunApplication {
	return applicationscheduler.NewRun(deps)
}

type scheduledSessionApplication = applicationscheduler.SessionApplication
type scheduledSessionApplicationDeps = applicationscheduler.SessionDeps

func newScheduledSessionApplication(deps scheduledSessionApplicationDeps) *scheduledSessionApplication {
	return applicationscheduler.NewSession(deps)
}
