package core

import (
	"context"

	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	"github.com/RedHuang-0622/seelex/application/session/switchpolicy"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

const (
	asyncStateDone   = dto.AsyncStateDone
	asyncStateFailed = dto.AsyncStateFailed
)

func (service *Service) triggerAsyncCompletions(triggered map[string]struct{}) int {
	return service.asyncCompletionApplicationComponent().TriggerAsyncCompletions(triggered)
}

func (service *Service) triggerTeamworkJobCompletions(triggered map[string]struct{}) int {
	return service.asyncCompletionApplicationComponent().TriggerTeamworkJobCompletions(triggered)
}

// sessionIdleForAsyncTrigger reports whether a session can start a completion
// turn. A missing unit is deliberately treated as unavailable.
func (service *Service) sessionIdleForAsyncTrigger(sessionID string) bool {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	if service.closed || service.draining {
		return false
	}
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return false
	}
	return !unit.ChatState().Running
}

func (service *Service) asyncCompletionApplicationComponent() *applicationexecution.CompletionApplication {
	if service == nil {
		return nil
	}
	return service.components.asyncCompletionApplication
}

func newServiceAsyncCompletionApplication(service *Service) *applicationexecution.CompletionApplication {
	return applicationexecution.NewCompletion(applicationexecution.CompletionDeps{
		AsyncRuns: service.Deps.Runtime.AsyncRunsSnapshot,
		TeamworkRuns: func() []dto.TeamworkJobCompletionRecord {
			return switching.TeamworkJobCompletions(service.Deps.Runtime)
		},
		SessionIdle: service.sessionIdleForAsyncTrigger,
		SubmitConversation: func(ctx context.Context, sessionID, input string) error {
			return service.submitConversationFor(ctx, sessionID, input)
		},
		EvidenceChars: func() int { return Limits().EvidenceChars },
	})
}
