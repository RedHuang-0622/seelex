// Package scheduler owns scheduled task application orchestration. Task
// definitions and execution remain in the Runtime scheduler.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/limit"
	effortpolicy "github.com/RedHuang-0622/seelex/kernel/limit/effort"
)

// SchedulerDeps supplies validation and Runtime scheduler callbacks.
type SchedulerDeps struct {
	ResolveWorkspace    func(workspaceID string) (*model.WorkspaceInfo, error)
	PluginCatalog       func() []model.PluginInfo
	ScheduleTask        func(context.Context, dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error)
	UpdateScheduledTask func(context.Context, string, dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error)
	CancelScheduledTask func(id string) error
	Refresh             func()
}

// SchedulerApplication owns create/update/cancel application ordering.
type SchedulerApplication struct {
	deps SchedulerDeps
}

// NewScheduler constructs the scheduled-task application.
func NewScheduler(deps SchedulerDeps) *SchedulerApplication {
	return &SchedulerApplication{deps: deps}
}

// WithRunDefaults fills undeclared run parameters with the unique default
// policy.
func WithRunDefaults(spec dto.ScheduledTaskSpec) dto.ScheduledTaskSpec {
	spec.ReasoningEffort, spec.LoopLimit = RunParams(spec.ReasoningEffort, spec.LoopLimit)
	return spec
}

// RunParams returns declared run parameters or their default values.
func RunParams(effort string, loopLimit int) (string, int) {
	return effortpolicy.DefaultRunParams(effort, loopLimit)
}

// ValidateRunParams validates effort and loop limit.
func ValidateRunParams(spec dto.ScheduledTaskSpec) error {
	if _, err := dto.NormalizeWireReasoningEffort(spec.ReasoningEffort); err != nil {
		return err
	}
	return limit.ValidateLoopLimit(spec.LoopLimit)
}

// ScheduleTask creates one scheduled task after defaulting and validation.
func (application *SchedulerApplication) ScheduleTask(ctx context.Context, spec dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error) {
	spec = WithRunDefaults(spec)
	if err := ValidateRunParams(spec); err != nil {
		return nil, err
	}
	if err := application.validateWorkspace(spec.WorkspaceID); err != nil {
		return nil, err
	}
	if err := application.validatePlugins(spec.Plugins); err != nil {
		return nil, err
	}
	created, err := application.deps.ScheduleTask(ctx, spec)
	if err != nil {
		return nil, err
	}
	application.deps.Refresh()
	return created, nil
}

// UpdateScheduledTask replaces one task after the same validation sequence as
// creation.
func (application *SchedulerApplication) UpdateScheduledTask(ctx context.Context, id string, spec dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("任务 ID 不能为空")
	}
	spec = WithRunDefaults(spec)
	if err := ValidateRunParams(spec); err != nil {
		return nil, err
	}
	if err := application.validateWorkspace(spec.WorkspaceID); err != nil {
		return nil, err
	}
	if err := application.validatePlugins(spec.Plugins); err != nil {
		return nil, err
	}
	updated, err := application.deps.UpdateScheduledTask(ctx, id, spec)
	if err != nil {
		return nil, err
	}
	application.deps.Refresh()
	return updated, nil
}

// CancelScheduledTask removes one scheduled task and refreshes projection.
func (application *SchedulerApplication) CancelScheduledTask(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("任务 ID 不能为空")
	}
	if err := application.deps.CancelScheduledTask(id); err != nil {
		return err
	}
	application.deps.Refresh()
	return nil
}

func (application *SchedulerApplication) validateWorkspace(workspaceID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil
	}
	if application.deps.ResolveWorkspace == nil {
		return errors.New("指定了工作区，但工作区能力未装配")
	}
	if _, err := application.deps.ResolveWorkspace(workspaceID); err != nil {
		return fmt.Errorf("定时任务工作区 %q 不可用: %w", workspaceID, err)
	}
	return nil
}

func (application *SchedulerApplication) validatePlugins(plugins []string) error {
	names, err := dto.NormalizePlugins(plugins, 0)
	if err != nil {
		return err
	}
	if len(names) == 0 || application.deps.PluginCatalog == nil {
		return nil
	}
	known := make(map[string]struct{})
	for _, info := range application.deps.PluginCatalog() {
		known[strings.TrimSpace(info.Name)] = struct{}{}
	}
	if len(known) == 0 {
		return nil
	}
	missing := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := known[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("定时任务装配的插件不存在：%s", strings.Join(missing, "、"))
	}
	return nil
}
