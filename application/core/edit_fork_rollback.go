package core

import applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"

func newServiceEditForkRollback(service *Service) *applicationedit.RollbackApplication {
	return applicationedit.NewRollback(applicationedit.RollbackDeps{
		Delete: service.DeleteSession,
		Resume: service.resumeSession,
	})
}
