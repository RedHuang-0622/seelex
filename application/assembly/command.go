package assembly

import (
	"context"

	applicationinput "github.com/RedHuang-0622/seelex/application/input"
	applicationcommand "github.com/RedHuang-0622/seelex/application/input/command"
	applicationobservability "github.com/RedHuang-0622/seelex/application/observability"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// registerBuiltinCommands installs the extracted command application with the
// Service host callbacks.
func (service *Service) registerBuiltinCommands() error {
	deps := applicationcommand.Deps{
		Model: func() (string, string) {
			return service.Deps.Runtime.Model(), service.Deps.Runtime.Provider()
		},
		History:  service.Deps.Engine.History,
		Trace:    service.Deps.Engine.TraceText,
		Sessions: func() []model.SessionInfo { return service.components.frontendSnapshot.Snapshot().Sessions },
		Diag: func() string {
			service.ViewMu.RLock()
			snapshot := service.Core.Snapshot
			service.ViewMu.RUnlock()
			return applicationobservability.RenderDiag(snapshot)
		},
		Clear: func() {
			service.Deps.Engine.ClearHistory()
			service.components.view.ResetConversation("已清空")
		},
		SigilCommand: applicationinput.SigilCommand,
		SigilPlugin:  applicationinput.SigilPlugin,
		SigilSkill:   applicationinput.SigilSkill,
		Mutation: applicationcommand.MutationDeps{
			Compact: func(ctx context.Context) (string, error) {
				result, err := service.components.contextCompactionApplication.CompactContextNow(ctx)
				if err != nil {
					return "", err
				}
				if !result.Compacted || !result.Recorded {
					return result.Note, nil
				}
				return compactionRecordNote(result), nil
			},
			BeginNewSession: func() error {
				return service.components.sessionDrafts.entry.Begin()
			},
			Capabilities: func() (bool, string) {
				service.ViewMu.RLock()
				capabilities := service.Core.Snapshot.Capabilities
				service.ViewMu.RUnlock()
				return capabilities.SessionResume, capabilities.SessionResumeReason
			},
			SessionInteraction: service.sessionInteraction,
			Resume:             func(sessionID string) error { return service.components.sessionResume.Resume(sessionID) },
			CurrentSession: func() (string, bool) {
				service.ViewMu.RLock()
				currentID := service.Core.Snapshot.Session.ID
				draft := service.Core.Snapshot.Session.Draft
				service.ViewMu.RUnlock()
				return currentID, draft
			},
			Fork:               service.components.forkApplication.ForkLatest,
			AccountInteraction: service.accountInteraction,
			Plugins: func() ([]model.PluginInfo, model.PluginInfo, bool) {
				current, ok := service.Deps.Plugins.Current()
				return service.Deps.Plugins.All(), current, ok
			},
			SwitchPlugin: func(ctx context.Context, name string) error {
				return service.components.policyApplication.SwitchPlugin(ctx, name)
			},
			Effort: func() string { return service.effortManager.Current() },
			SwitchEffort: func(_ context.Context, level string) error {
				return service.components.policyApplication.SwitchEffort(level)
			},
			Permission:      func() string { return service.permissionTierForSession(service.currentViewSessionID()) },
			PermissionTiers: dto.PermissionTiers,
			SetPermission: func(tier string) (string, error) {
				return service.components.policyApplication.SetPermissionTier(tier)
			},
		},
	}
	application := applicationcommand.New(service.commands, deps)
	if err := application.Register(); err != nil {
		return err
	}
	return application.RegisterMutations()
}
