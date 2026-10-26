package core

import (
	"time"

	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
)

func newServiceEditForkOperationRegistry(service *Service) *applicationedit.OperationRegistry {
	return applicationedit.NewOperationRegistry(editForkOperationTTL, time.Now)
}
