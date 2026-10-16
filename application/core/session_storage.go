package core

import (
	"context"

	"github.com/RedHuang-0622/seelex/store"
)

func (service *Service) SessionStorageConfig() (store.Config, error) {
	return service.components.sessions.SessionStorageConfig()
}

func (service *Service) TestSessionStorage(ctx context.Context, config store.Config) error {
	return service.components.sessions.TestSessionStorage(ctx, config)
}

func (service *Service) ConfigureSessionStorage(ctx context.Context, config store.Config) error {
	return service.components.sessions.ConfigureSessionStorage(ctx, config)
}
