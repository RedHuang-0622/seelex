package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// RunDeps supplies session unit, tier, run-param and plugin assembly callbacks.
type RunDeps struct {
	EnsureSessionUnit  func(sessionID string)
	PersistTier        func(sessionID, tier string) error
	ApplyStoredTier    func(sessionID, tier string)
	ApplyRunParams     func(sessionID string, task dto.ScheduledTaskSpec) error
	WithPluginAssembly func(context.Context, []string) (context.Context, error)
}

// RunApplication owns pre-submit session assembly.
type RunApplication struct {
	deps RunDeps
}

// NewRun constructs the scheduled-run application.
func NewRun(deps RunDeps) *RunApplication {
	return &RunApplication{deps: deps}
}

// AssembleScheduledRun applies permission tier, run parameters and plugins to a
// target session context.
func (application *RunApplication) AssembleScheduledRun(ctx context.Context, sessionID string, task dto.ScheduledTaskSpec) (context.Context, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ctx, errors.New("会话 ID 不能为空")
	}
	tier := strings.TrimSpace(task.PermissionTier)
	if tier == "" {
		tier = dto.PermissionTierFull
	}
	normalized, err := dto.NormalizePermissionTier(tier)
	if err != nil {
		return ctx, err
	}
	application.deps.EnsureSessionUnit(sessionID)
	if err := application.deps.PersistTier(sessionID, normalized); err != nil {
		return ctx, fmt.Errorf("定时任务装配权限档位失败: %w", err)
	}
	application.deps.ApplyStoredTier(sessionID, normalized)
	if err := application.deps.ApplyRunParams(sessionID, task); err != nil {
		return ctx, err
	}
	plugins, err := dto.NormalizePlugins(task.Plugins, 0)
	if err != nil {
		return ctx, err
	}
	if len(plugins) == 0 {
		return ctx, nil
	}
	return application.deps.WithPluginAssembly(ctx, plugins)
}
