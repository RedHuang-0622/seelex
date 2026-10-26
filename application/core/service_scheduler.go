package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

	applicationscheduler "github.com/RedHuang-0622/seelex/application/admin/scheduler"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	switchworkspace "github.com/RedHuang-0622/seelex/middle/switch/workspace"
)

func withScheduledRunDefaults(spec dto.ScheduledTaskSpec) dto.ScheduledTaskSpec {
	return applicationscheduler.WithRunDefaults(spec)
}

func scheduledRunParams(effort string, loopLimit int) (string, int) {
	return applicationscheduler.RunParams(effort, loopLimit)
}

func (service *Service) validateScheduledRunParams(spec dto.ScheduledTaskSpec) error {
	return applicationscheduler.ValidateRunParams(spec)
}

func (service *Service) applyScheduledRunParams(sessionID string, task dto.ScheduledTaskSpec) error {
	effort, loops := scheduledRunParams(task.ReasoningEffort, task.LoopLimit)
	pinner, ok := service.Deps.Engine.(contract.SessionLoopLimitPort)
	if !ok {
		return errors.New("宿主引擎不支持按会话设置循环上限（SetSessionMaxLoopsFor 未实现）")
	}
	pinner.SetSessionMaxLoopsFor(sessionID, loops)
	service.knobs.BorrowSessionReasoningEffort(sessionID, effort)
	return nil
}

func (service *Service) validateScheduledWorkspace(workspaceID string) error {
	_, err := service.resolveScheduledWorkspace(workspaceID)
	return err
}

func (service *Service) resolveScheduledWorkspace(workspaceID string) (*model.WorkspaceInfo, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		var unbound *model.WorkspaceInfo
		return unbound, nil
	}
	if service.Deps.Workspace == nil {
		return nil, errors.New("指定了工作区，但工作区能力未装配")
	}
	info, err := service.Deps.Workspace.Get(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("定时任务工作区 %q 不可用: %w", workspaceID, err)
	}
	return &info, nil
}

func (service *Service) validateScheduledPlugins(plugins []string) error {
	names, err := dto.NormalizePlugins(plugins, 0)
	if err != nil {
		return err
	}
	if len(names) == 0 || service.Deps.Plugins == nil {
		return nil
	}
	known := make(map[string]struct{})
	for _, info := range service.Deps.Plugins.All() {
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

func (service *Service) schedulerApplicationComponent() *applicationscheduler.SchedulerApplication {
	if service == nil {
		return nil
	}
	return service.components.schedulerApplication
}

func (service *Service) scheduledRunApplicationComponent() *applicationscheduler.RunApplication {
	if service == nil {
		return nil
	}
	return service.components.scheduledRunApplication
}

func (service *Service) scheduledSessionApplicationComponent() *applicationscheduler.SessionApplication {
	if service == nil {
		return nil
	}
	return service.components.scheduledSessionApplication
}

func newServiceSchedulerApplication(service *Service) *applicationscheduler.SchedulerApplication {
	return applicationscheduler.NewScheduler(applicationscheduler.SchedulerDeps{
		ValidateWorkspace: service.validateScheduledWorkspace,
		ValidatePlugins:   service.validateScheduledPlugins,
		ScheduleTask: func(ctx context.Context, spec dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error) {
			return service.Deps.Runtime.ScheduleTask(ctx, spec)
		},
		UpdateScheduledTask: func(ctx context.Context, id string, spec dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error) {
			return service.Deps.Runtime.UpdateScheduledTask(ctx, id, spec)
		},
		CancelScheduledTask: service.Deps.Runtime.CancelScheduledTask,
		Refresh: func() {
			if component := service.runtimeApplicationComponent(); component != nil {
				component.Refresh()
			}
		},
	})
}

func newServiceScheduledRunApplication(service *Service) *applicationscheduler.RunApplication {
	return applicationscheduler.NewRun(applicationscheduler.RunDeps{
		EnsureSessionUnit: func(sessionID string) {
			service.ViewMu.Lock()
			service.sessionUnitLocked(sessionID)
			service.ViewMu.Unlock()
		},
		PersistTier:     service.persistPermissionTier,
		ApplyStoredTier: service.applyStoredPermissionTier,
		ApplyRunParams:  service.applyScheduledRunParams,
		WithPluginAssembly: func(ctx context.Context, plugins []string) (context.Context, error) {
			carrier, ok := service.Deps.Runtime.(interface {
				WithPluginAssembly(context.Context, []string) context.Context
			})
			if !ok {
				return ctx, errors.New("宿主不支持插件装配（WithPluginAssembly 未实现）")
			}
			return carrier.WithPluginAssembly(ctx, plugins), nil
		},
	})
}

func newServiceScheduledSessionApplication(service *Service) *applicationscheduler.SessionApplication {
	return applicationscheduler.NewSession(applicationscheduler.SessionDeps{
		LifecycleState: func() (bool, bool) {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.closed, service.draining
		},
		ResolveWorkspace: service.resolveScheduledWorkspace,
		NewSessionID: func(prefix string) string {
			return service.newGeneratedSessionID(prefix)
		},
		OpenSession: func(sessionID string, workspace *model.WorkspaceInfo) (string, error) {
			transition := service.transitionForSession(sessionID)
			transition.Lock()
			defer transition.Unlock()
			return service.openSessionEngine(sessionID, workspace)
		},
		Projection: func() switchworkspace.Projection {
			return service.components.workspace.Projection()
		},
		LockView:                       service.ViewMu.Lock,
		ApplyWorkspaceProjectionLocked: service.applyWorkspaceProjectionLocked,
		BumpLocked:                     service.bumpLocked,
		UnlockView:                     service.ViewMu.Unlock,
		PublishSnapshotChanged: func(revision uint64, sessionID string) {
			service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		},
		TouchResident: service.touchResident,
		RequestCatalogRefresh: func() {
			service.components.sessions.RequestCatalogRefresh()
		},
		AssembleRun: func(ctx context.Context, sessionID string, task dto.ScheduledTaskSpec) (context.Context, error) {
			return service.scheduledRunApplicationComponent().AssembleScheduledRun(ctx, sessionID, task)
		},
		SubmitConversation: service.submitConversationFor,
		DrainingError:      ErrApplicationDraining,
	})
}
