package assembly

import (
	"context"
	"errors"

	applicationscheduler "github.com/RedHuang-0622/seelex/application/admin/scheduler"
	applicationlifecycle "github.com/RedHuang-0622/seelex/application/lifecycle"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	switchworkspace "github.com/RedHuang-0622/seelex/application/workspace/switch"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func (service *Service) assembleSchedulerApplications() {
	service.components.schedulerApplication = applicationscheduler.NewScheduler(applicationscheduler.SchedulerDeps{
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
			if component := service.components.runtimeApplication; component != nil {
				component.Refresh()
			}
		},
	})

	service.components.scheduledRunApplication = applicationscheduler.NewRun(applicationscheduler.RunDeps{
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

	service.components.scheduledSessionApplication = applicationscheduler.NewSession(applicationscheduler.SessionDeps{
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
		BumpLocked: func() uint64 {
			return service.components.view.BumpLocked()
		},
		UnlockView: service.ViewMu.Unlock,
		PublishSnapshotChanged: func(revision uint64, sessionID string) {
			service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		},
		TouchResident: func(sessionID string) {
			service.components.residencyRuntime.TouchResident(sessionID)
		},
		RequestCatalogRefresh: func() {
			service.components.sessions.RequestCatalogRefresh()
		},
		AssembleRun: func(ctx context.Context, sessionID string, task dto.ScheduledTaskSpec) (context.Context, error) {
			return service.components.scheduledRunApplication.AssembleScheduledRun(ctx, sessionID, task)
		},
		SubmitConversation: service.submitConversationFor,
		DrainingError:      ErrApplicationDraining,
	})
}

func (service *Service) assembleKnobs() {
	service.knobs = applicationpolicy.NewKnobs(serviceKnobsHost{service: service})
}

func (service *Service) assembleLifecycleApplication() {
	service.components.lifecycleApplication = applicationlifecycle.New(applicationlifecycle.Deps{
		SetDraining: func() {
			service.ViewMu.Lock()
			service.draining = true
			service.ViewMu.Unlock()
		},
		Idle: func() <-chan struct{} {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.idle
		},
		AnyRunning: func() bool {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.anyChatRunningLocked()
		},
		ReadCancels: func() []context.CancelFunc {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			cancels := make([]context.CancelFunc, 0, 4)
			for _, sid := range service.sessions.UnitIDs() {
				if unit := service.sessions.Unit(sid); unit != nil {
					if cancel := unit.CancelFunc(); cancel != nil {
						cancels = append(cancels, cancel)
					}
				}
			}
			return cancels
		},
		CancelChat: func(requestID string) bool {
			service.ViewMu.Lock()
			defer service.ViewMu.Unlock()
			sessionID := service.Core.Snapshot.Session.ID
			runtime := service.sessionUnitLocked(sessionID)
			chat := runtime.ChatState()
			if !chat.Running || runtime.CancelFunc() == nil {
				return false
			}
			if requestID != "" && requestID != chat.RequestID {
				runChatDebug("cancel request id %q is stale; cancelling current request %q of session %s", requestID, chat.RequestID, sessionID)
			}
			runtime.CancelFunc()()
			return true
		},
		BeginShutdown: func() bool {
			service.ViewMu.Lock()
			if service.closed {
				service.ViewMu.Unlock()
				return false
			}
			service.closed = true
			for _, sid := range service.sessions.UnitIDs() {
				if unit := service.sessions.Unit(sid); unit != nil {
					if cancel := unit.CancelFunc(); cancel != nil {
						cancel()
					}
				}
			}
			service.ViewMu.Unlock()
			return true
		},
		FlushToolRecords: func() {
			service.toolRecords.FlushAll()
		},
		StopCatalogRefresh: func() {
			service.components.sessions.StopCatalogRefresh()
		},
		CloseSessions: service.sessions.Close,
		StopLifecycleConsumers: func() {
			if service.components.workTableRuntime != nil {
				service.components.workTableRuntime.StopLifecycleConsumers()
			}
		},
		CloseWorkTable: func() {
			if service.components.workTableRuntime != nil {
				service.components.workTableRuntime.Close()
			}
		},
		ShutdownApproval: service.Approval.Shutdown,
	})
}
