package core

import (
	"errors"
	"fmt"
)

// ErrReActBudgetExceeded marks a request that was stopped by its captured
// execution budget rather than by a provider or tool error.
var ErrReActBudgetExceeded = errors.New("ReAct execution budget exhausted")

type activeReActBudget struct {
	requestID         string
	budget            ReActBudget
	toolCalls         int
	lastProgressEpoch uint64
	noProgressRounds  int
	reason            string
}

func (service *Service) startReActBudgetLocked(requestID string, budget ReActBudget) {
	service.reactBudget = &activeReActBudget{requestID: requestID, budget: budget}
}

func (service *Service) clearReActBudget(requestID string) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.reactBudget != nil && service.reactBudget.requestID == requestID {
		service.reactBudget = nil
	}
}

func (service *Service) recordReActToolCall() {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.reactBudget != nil {
		service.reactBudget.toolCalls++
	}
}

// allowNextReActIteration is called after one complete tool-calling turn. It
// intentionally allows export/write tools while there is budget left; it only
// stops the next model iteration when the request-level budget is exhausted.
func (service *Service) allowNextReActIteration(turn int) bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	budget := service.reactBudget
	if budget == nil || budget.reason != "" {
		return budget == nil
	}
	if budget.budget.MaxToolCalls > 0 && budget.toolCalls >= budget.budget.MaxToolCalls {
		budget.reason = fmt.Sprintf("tool-call limit reached (%d)", budget.budget.MaxToolCalls)
		return false
	}
	if budget.budget.MaxToolRounds > 0 && turn+1 >= budget.budget.MaxToolRounds {
		budget.reason = fmt.Sprintf("tool-round limit reached (%d)", budget.budget.MaxToolRounds)
		return false
	}
	if budget.budget.MaxNoProgressRounds > 0 {
		state := service.taskExecution
		if state != nil && state.requestID == budget.requestID {
			if state.progressEpoch == budget.lastProgressEpoch {
				budget.noProgressRounds++
			} else {
				budget.lastProgressEpoch = state.progressEpoch
				budget.noProgressRounds = 0
			}
			if budget.noProgressRounds >= budget.budget.MaxNoProgressRounds {
				budget.reason = fmt.Sprintf("no observable progress for %d tool rounds", budget.noProgressRounds)
				return false
			}
		}
	}
	return true
}

func (service *Service) reactBudgetError(requestID string) error {
	service.mu.RLock()
	defer service.mu.RUnlock()
	if service.reactBudget == nil || service.reactBudget.requestID != requestID || service.reactBudget.reason == "" {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrReActBudgetExceeded, service.reactBudget.reason)
}
