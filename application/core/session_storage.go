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

func (service *Service) SessionStorageConfig() (sessionstore.Config, error) {
	storage, ok := service.deps.Sessions.(sessionStoragePort)
	if !ok {
		return sessionstore.Config{}, fmt.Errorf("session storage settings are unavailable")
	}
	return storage.StorageConfig()
}

func (service *Service) TestSessionStorage(ctx context.Context, config sessionstore.Config) error {
	storage, ok := service.deps.Sessions.(sessionStoragePort)
	if !ok {
		return fmt.Errorf("session storage settings are unavailable")
	}
	return storage.TestStorage(ctx, config)
}

func (service *Service) ConfigureSessionStorage(ctx context.Context, config sessionstore.Config) error {
	storage, ok := service.deps.Sessions.(sessionStoragePort)
	if !ok {
		return fmt.Errorf("session storage settings are unavailable")
	}
	return storage.ConfigureStorage(ctx, config)
}
