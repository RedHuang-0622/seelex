package core

import (
	"context"
	"errors"

	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	"github.com/RedHuang-0622/seelex/application/prompt"
	switching "github.com/RedHuang-0622/seelex/application/session/switchpolicy"
	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func newServicePolicyApplication(service *Service) *applicationpolicy.SwitchApplication {
	return applicationpolicy.NewSwitch(applicationpolicy.SwitchDeps{
		ActivateSkill: func(name string) (model.SkillInfo, error) {
			if service == nil {
				return model.SkillInfo{}, errors.New("service is not initialized")
			}
			return service.components.skillActivation.Activate(
				name,
				service.Deps.Skills,
				service.applySkill,
			)
		},
		SetReasoningEffort: service.knobs.SetReasoningEffort,
		SetLoopLimit:       service.knobs.SetLoopLimit,
		SetReleaseWindow: func(window int) (int, error) {
			return switching.SetReleaseWindow(service.releaseWindowDeps(), window)
		},
		ReleaseWindow: func() int {
			return switching.ReleaseWindow(service.releaseWindowDeps())
		},
		SetPermissionTier: func(tier string) (string, error) {
			return service.components.policyApplication.SetPermissionTier(tier)
		},
		CurrentViewSessionID: service.currentViewSessionID,
		PersistTier:          service.persistPermissionTier,
		SetUnitTier: func(sessionID, tier string) {
			if unit := service.sessions.Unit(sessionID); unit != nil {
				unit.SetPermissionTier(tier)
			}
		},
		SetApprovalAuto: func(sessionID string, full bool) {
			if service.Approval != nil {
				service.Approval.SetPermissionAutoApprovalFor(sessionID, full)
			}
		},
		SetRuntimeTier: func(sessionID, tier string) error {
			if service.Deps.Runtime == nil {
				return nil
			}
			return service.Deps.Runtime.SetPermissionTierFor(sessionID, tier)
		},
		EffectiveTier: service.permissionTierForSession,
		ResolveApprovals: func(sessionID string) {
			if service.Approval != nil {
				service.Approval.ResolveAllFor(sessionID, contract.ApprovalDecision{OptionID: "allow"})
			}
		},
		PublishTier: func(sessionID, effective string) {
			service.ViewMu.Lock()
			service.Core.Snapshot.Runtime.PermissionTier = effective
			service.Core.Snapshot.Runtime.FullAccess = dto.PermissionTierIsFullAccess(effective)
			revision := service.components.view.BumpLocked()
			runtime := model.CloneRuntimeState(service.Core.Snapshot.Runtime)
			service.ViewMu.Unlock()
			service.publishSessionEvent(EventRuntimeChanged, revision, "", sessionID, runtime)
		},
		EnsureViewIdle: service.knobs.EnsureViewIdle,
		CycleEffort: func() (string, error) {
			return service.effortManager.Cycle()
		},
		ApplyEffort: func(level string) error {
			return service.effortManager.Apply(level)
		},
		SubagentPolicy: func() dto.SubagentPolicy {
			return service.effortManager.SubagentPolicy()
		},
		SetSubagentPolicy: func(policy dto.SubagentPolicy) {
			service.Deps.Runtime.SetSubagentPolicy(policy)
		},
		SyncReasoningEffort: service.syncSessionReasoningEffort,
		CurrentEffort: func() string {
			return service.effortManager.Current()
		},
		RenderPrompt: func() string { return service.promptStack.Render() },
		SetSystemPrompt: func(sessionID, prompt string) {
			if routed, ok := service.Deps.Engine.(interface{ SetSystemPromptFor(string, string) }); ok {
				routed.SetSystemPromptFor(sessionID, prompt)
			} else {
				service.Deps.Engine.SetSystemPrompt(prompt)
			}
		},
		CommitEffort: func(sessionID, effort, _ string, _ int) {
			service.ViewMu.Lock()
			if unit := service.sessions.Unit(sessionID); unit != nil {
				unit.SetEffortLevel(effort)
			}
			service.Core.Snapshot.Runtime.Effort = effort
			service.knobs.SeedReadings(prompt.ReasoningEffortFor(effort), prompt.MaxLoops(effort))
			reasoning := service.knobs.ReasoningEfforts()
			loop := service.knobs.LoopLimit()
			service.Core.Snapshot.Runtime.ReasoningEffort = reasoning
			service.Core.Snapshot.Runtime.LoopLimit = loop
			revision := service.components.view.BumpLocked()
			runtime := model.CloneRuntimeState(service.Core.Snapshot.Runtime)
			service.ViewMu.Unlock()
			service.publishSessionEvent(EventRuntimeChanged, revision, "", service.currentViewSessionID(), runtime)
		},
		AnyChatRunning: func() bool {
			service.ViewMu.RLock()
			running := service.anyChatRunningLocked()
			service.ViewMu.RUnlock()
			return running
		},
		ActivatePlugin:   service.Deps.Plugins.Activate,
		DeactivatePlugin: service.Deps.Plugins.Deactivate,
		ClearHistory:     service.Deps.Engine.ClearHistory,
		ResetPrompt:      service.promptStack.Reset,
		CurrentPlugin:    service.Deps.Plugins.Current,
		ReapplyEffort:    service.reapplyEffortAfterPluginSwitch,
		SetPluginSystemPrompt: func(prompt string) {
			service.Deps.Engine.SetSystemPrompt(prompt)
		},
		ResetConversation: func(message string) {
			service.components.view.ResetConversation(message)
		},
		CollectProjection: func(ctx context.Context) view_state.RuntimeStateProjection {
			return service.components.view.CollectRuntimeProjection(ctx)
		},
		CommitProjection: func(_ context.Context, projection view_state.RuntimeStateProjection) {
			service.ViewMu.Lock()
			service.components.view.ApplyRuntimeProjectionLocked(projection)
			revision := service.components.view.BumpLocked()
			runtime := model.CloneRuntimeState(service.Core.Snapshot.Runtime)
			service.ViewMu.Unlock()
			service.publishSessionEvent(EventRuntimeChanged, revision, "", service.currentViewSessionID(), runtime)
			service.publishRuntimeProjections()
		},
	})
}
