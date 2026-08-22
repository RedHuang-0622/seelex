package session_runtime

import (
	"context"
	"fmt"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// SessionStorageConfig 返回会话存储设置（可选能力：无 storage 端口报错）。
func (c *Coordinator) SessionStorageConfig() (sessionstore.Config, error) {
	storage, ok := c.Core.Deps.Sessions.(SessionStoragePort)
	if !ok {
		return sessionstore.Config{}, fmt.Errorf("session storage settings are unavailable")
	}
	return storage.StorageConfig()
}

// TestSessionStorage 验证会话存储配置可用性（可选能力）。
func (c *Coordinator) TestSessionStorage(ctx context.Context, config sessionstore.Config) error {
	storage, ok := c.Core.Deps.Sessions.(SessionStoragePort)
	if !ok {
		return fmt.Errorf("session storage settings are unavailable")
	}
	return storage.TestStorage(ctx, config)
}

// ConfigureSessionStorage 应用会话存储配置并清空标题缓存（可选能力）。
func (c *Coordinator) ConfigureSessionStorage(ctx context.Context, config sessionstore.Config) error {
	storage, ok := c.Core.Deps.Sessions.(SessionStoragePort)
	if !ok {
		return fmt.Errorf("session storage settings are unavailable")
	}
	if err := storage.ConfigureStorage(ctx, config); err != nil {
		return err
	}
	c.clearSessionNames()
	return nil
}
