package core

import (
	"context"
	"errors"
	"strings"

	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	conversationcontext "github.com/RedHuang-0622/seelex/application/conversation/context"
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationinput "github.com/RedHuang-0622/seelex/application/input"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	applicationreference "github.com/RedHuang-0622/seelex/application/reference"
	session_runtime "github.com/RedHuang-0622/seelex/application/session/runtime"
	switching "github.com/RedHuang-0622/seelex/application/session/switchpolicy"
	applicationworkspace "github.com/RedHuang-0622/seelex/application/workspace"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func (service *Service) assembleRuntimeApplication() {
	service.components.runtimeApplication = applicationadmin.NewRuntime(applicationadmin.RuntimeDeps{
		Projection: serviceRuntimeProjectionPort{service: service},
	})
}

func (service *Service) assembleAdminApplication() {
	service.components.adminApplication = applicationadmin.NewAccountApplication(applicationadmin.AccountDeps{
		Admin: func() contract.AccountAdminPort {
			if service == nil {
				return nil
			}
			return service.Deps.AccountAdmin
		},
		Balance: func() contract.AccountBalancePort {
			if service == nil || service.Deps.Runtime == nil {
				return nil
			}
			port, _ := service.Deps.Runtime.(contract.AccountBalancePort)
			return port
		},
		Select: func(name string) bool {
			return service.Deps.Runtime.SelectAccount(name)
		},
		Window: func(name string, window int) (int, error) {
			runtimePort, _ := service.Deps.Runtime.(contract.AccountWindowPort)
			return service.components.accountWindowPolicy.Set(
				runtimePort,
				service.Deps.AccountWindowStore,
				name,
				window,
			)
		},
		Projection: serviceAdminProjectionPort{service: service},
	})
}

func (service *Service) assembleWorkspaceApplication() {
	service.components.workspaceApplication = applicationworkspace.New(applicationworkspace.Deps{
		Environment: func() contract.WorkspacePort {
			if service == nil {
				return nil
			}
			return service.Deps.Workspace
		},
		WorkspaceRoot: func() (contract.WorkspacePort, string, error) {
			if service == nil || service.components.workspace == nil {
				return nil, "", errors.New("worktree: workspace root port is not assembled")
			}
			return service.components.workspace.WorkspaceRootPort()
		},
		CreateWorkspaces: func(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error) {
			return service.components.workspace.CreateWorkspaces(requests)
		},
		Projection: serviceWorkspaceProjectionPort{service: service},
	})
}

func (service *Service) assembleAsyncCompletionApplication() {
	service.components.asyncCompletionApplication = applicationexecution.NewCompletion(applicationexecution.CompletionDeps{
		AsyncRuns: service.Deps.Runtime.AsyncRunsSnapshot,
		TeamworkRuns: func() []dto.TeamworkJobCompletionRecord {
			return switching.TeamworkJobCompletions(service.Deps.Runtime)
		},
		SessionIdle: service.sessionIdleForAsyncTrigger,
		SubmitConversation: func(ctx context.Context, sessionID, input string) error {
			return service.submitConversationFor(ctx, sessionID, input)
		},
		EvidenceChars: func() int { return applicationpolicy.Get().EvidenceChars },
	})
}

func (service *Service) assembleCompletionApplication() {
	service.components.completionApplication = applicationinput.New(applicationinput.Deps{
		CommandSuggestions: func() []applicationinput.Suggestion {
			suggestions := make([]applicationinput.Suggestion, 0)
			for _, command := range service.commands.All() {
				suggestions = append(suggestions, applicationinput.Suggestion{
					Text: command.Name(), Description: command.Description(),
					Kind: applicationinput.SuggestionKindCommand,
				})
			}
			return suggestions
		},
		SkillSuggestions: func() []applicationinput.Suggestion {
			suggestions := make([]applicationinput.Suggestion, 0)
			for _, skill := range service.Deps.Skills.All() {
				suggestions = append(suggestions, applicationinput.Suggestion{
					Text: skill.Name, Description: skill.Description,
					Kind: applicationinput.SuggestionKindSkill,
				})
			}
			return suggestions
		},
		PluginSuggestions: func() []applicationinput.Suggestion {
			suggestions := make([]applicationinput.Suggestion, 0)
			for _, plugin := range service.Deps.Plugins.All() {
				suggestions = append(suggestions, applicationinput.Suggestion{
					Text: plugin.Name, Description: plugin.Description,
					Kind: applicationinput.SuggestionKindPlugin,
				})
			}
			return append(suggestions, applicationinput.Suggestion{
				Text: "off", Description: "停用所有插件",
				Kind: applicationinput.SuggestionKindPlugin,
			})
		},
		HasPlugin: func(name string) bool {
			for _, plugin := range service.Deps.Plugins.All() {
				if strings.EqualFold(plugin.Name, name) {
					return true
				}
			}
			return false
		},
		HasSkill: func(name string) bool {
			_, ok := service.Deps.Skills.Get(name)
			return ok
		},
		HasVisibleTool: func(name string) bool {
			if service.Deps.Runtime == nil || strings.TrimSpace(name) == "" {
				return false
			}
			for _, tool := range service.Deps.Runtime.VisibleTools(context.Background()) {
				if strings.EqualFold(tool.Name, strings.TrimSpace(name)) {
					return true
				}
			}
			return false
		},
	})
}

func (service *Service) assembleContextCompactionApplication() {
	service.components.contextCompactionApplication = conversationcontext.New(conversationcontext.Deps{
		CurrentSessionID: func(ctx context.Context) string {
			sessionID := task_context.SessionIDFromContext(ctx)
			if sessionID == "" {
				service.ViewMu.RLock()
				sessionID = service.Core.Snapshot.Session.ID
				service.ViewMu.RUnlock()
			}
			return sessionID
		},
		Acquire: service.acquireCompactionRound,
		Release: service.releaseCompactionRound,
		Compact: func(ctx context.Context, sessionID string) (conversationcontext.EngineCompactResult, error) {
			return service.components.context.CompactContextNow(ctx, sessionID)
		},
	})
}

func (service *Service) assembleReferenceApplication() {
	service.components.referenceApplication = applicationreference.New(applicationreference.Deps{
		ReferencePageSize: func() int {
			return applicationpolicy.Get().ReferencePageSize
		},
		MaxReferencePageSize: func() int {
			return applicationpolicy.Get().MaxReferencePageSize
		},
		NodeResult: func(nodeID, ref string) (string, bool) {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.nodeToolResult(nodeID, ref)
		},
		ResolveAlias: service.resolveToolResultRefAlias,
		ResolveResult: func(ref string) (model.StoredToolResult, bool, error) {
			service.ViewMu.RLock()
			if !service.hasToolResultRefLocked(ref) {
				service.ViewMu.RUnlock()
				return model.StoredToolResult{}, false, nil
			}
			for _, pending := range service.components.tasks.PendingToolResults() {
				if pending.Ref == ref {
					service.ViewMu.RUnlock()
					return pending, true, nil
				}
			}
			sessionID := service.Core.Snapshot.Session.ID
			workspaceID := session_runtime.WorkspaceID(service.Core.Snapshot.CurrentWorkspace)
			service.ViewMu.RUnlock()

			store, ok := service.Deps.Sessions.(session_runtime.SessionTranscriptPort)
			if !ok {
				return model.StoredToolResult{}, false, errors.New("durable result storage is unavailable")
			}
			result, err := store.LoadToolResultWorkspace(workspaceID, sessionID, ref)
			if err != nil {
				return model.StoredToolResult{}, false, err
			}
			return result, true, nil
		},
		ActivePlanID: func() string {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.components.tasks.ActivePlanID()
		},
		PlanFrame: func(planRef string) (string, model.RuntimeState, bool) {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			frame := task_context.ActivePlanFrame(service.components.tasks.PlanStack(), planRef)
			if frame == nil {
				return "", model.RuntimeState{}, false
			}
			planState := model.CloneRuntimeState(model.RuntimeState{Plan: frame.Plan})
			return frame.Arguments, planState, true
		},
		CompressedTurnView: func() (string, string) {
			service.ViewMu.RLock()
			defer service.ViewMu.RUnlock()
			return service.Core.Snapshot.Session.ID,
				session_runtime.WorkspaceID(service.Core.Snapshot.CurrentWorkspace)
		},
		LoadCompressedTurn: func(workspaceID, sessionID, ref string) (string, error) {
			store, ok := service.Deps.Sessions.(session_runtime.SessionTranscriptPort)
			if !ok {
				return "", errors.New("durable storage is unavailable")
			}
			result, err := store.LoadToolResultWorkspace(workspaceID, sessionID, ref)
			if err != nil {
				return "", err
			}
			return result.Content, nil
		},
		CompressedTurnPrefix: applicationreference.CompressedTurnRefPrefix,
	})
}
