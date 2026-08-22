package core

import (
	"context"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

func (service *Service) SessionStorageConfig() (sessionstore.Config, error) {
	return service.components.sessions.SessionStorageConfig()
}

func (service *Service) TestSessionStorage(ctx context.Context, config sessionstore.Config) error {
	return service.components.sessions.TestSessionStorage(ctx, config)
}

func (service *Service) ConfigureSessionStorage(ctx context.Context, config sessionstore.Config) error {
	return service.components.sessions.ConfigureSessionStorage(ctx, config)
}
