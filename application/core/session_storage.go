package core

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

type sessionStoragePort interface {
	StorageConfig() (sessionstore.Config, error)
	TestStorage(context.Context, sessionstore.Config) error
	ConfigureStorage(context.Context, sessionstore.Config) error
}

func (service *sessionCoordinator) sessionStorageConfig() (sessionstore.Config, error) {
	storage, ok := service.deps.Sessions.(sessionStoragePort)
	if !ok {
		return sessionstore.Config{}, fmt.Errorf("session storage settings are unavailable")
	}
	return storage.StorageConfig()
}

func (service *sessionCoordinator) testSessionStorage(ctx context.Context, config sessionstore.Config) error {
	storage, ok := service.deps.Sessions.(sessionStoragePort)
	if !ok {
		return fmt.Errorf("session storage settings are unavailable")
	}
	return storage.TestStorage(ctx, config)
}

func (service *sessionCoordinator) configureSessionStorage(ctx context.Context, config sessionstore.Config) error {
	storage, ok := service.deps.Sessions.(sessionStoragePort)
	if !ok {
		return fmt.Errorf("session storage settings are unavailable")
	}
	if err := storage.ConfigureStorage(ctx, config); err != nil {
		return err
	}
	service.clearSessionNames()
	return nil
}

func (service *Service) SessionStorageConfig() (sessionstore.Config, error) {
	return service.components.sessions.sessionStorageConfig()
}

func (service *Service) TestSessionStorage(ctx context.Context, config sessionstore.Config) error {
	return service.components.sessions.testSessionStorage(ctx, config)
}

func (service *Service) ConfigureSessionStorage(ctx context.Context, config sessionstore.Config) error {
	return service.components.sessions.configureSessionStorage(ctx, config)
}
