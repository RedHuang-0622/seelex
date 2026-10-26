package context

import (
	"context"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/event"
	taskcontext "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	viewstate "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	kernelstate "github.com/RedHuang-0622/seelex/kernel/state"
)

const (
	ContextRecoveryPrefix           = "<!-- seelex:context-recovery:v1 -->"
	ProviderRecoveryPrefix          = "<!-- seelex:provider-recovery:v1 -->"
	ContextRecoveryRequestDelimiter = "\n## Original User Request\n"
	ContextRecoveryAgentInput       = "<!-- seelex:context-recovery-agent:v1 -->\n" +
		"The provider rejected the previous conversation record (context too large, or an invalid tool-call record). " +
		"The history now contains a bounded task checkpoint. Continue from that checkpoint without assuming omitted details. " +
		"If more detail is required, use a narrower, paginated, or filtered tool call; do not request a full large result. " +
		"Deliver the task if the checkpoint evidence is sufficient."
	contextRecoveryPrefix           = ContextRecoveryPrefix
	providerRecoveryPrefix          = ProviderRecoveryPrefix
	contextRecoveryRequestDelimiter = ContextRecoveryRequestDelimiter
	contextRecoveryAgentInput       = ContextRecoveryAgentInput
)

// RecoveryDeps supplies engine history, task state, view revision and stream
// callbacks. Recovery policy remains application-owned; provider calls remain
// behind the injected stream function.
type RecoveryDeps struct {
	Core                *kernelstate.Core
	Tasks               *taskcontext.Coordinator
	View                *viewstate.Coordinator
	Context             *EngineCoordinator
	EngineHistory       func(sessionID string) []contract.EngineMessage
	ReplaceHistory      func(sessionID string, history []contract.EngineMessage) error
	ChatStream          func(context.Context, string, string, func(string)) (string, error)
	PublishSessionEvent func(event.EventKind, uint64, string, string, any) event.Event
}

// RecoveryApplication owns provider-context recovery and cleanup.
type RecoveryApplication struct {
	deps RecoveryDeps
}

// NewRecovery constructs the provider recovery application.
func NewRecovery(deps RecoveryDeps) *RecoveryApplication {
	return &RecoveryApplication{deps: deps}
}

func (application *RecoveryApplication) sessionID(ctx context.Context) string {
	if sessionID := taskcontext.SessionIDFromContext(ctx); sessionID != "" {
		return sessionID
	}
	application.deps.Core.ViewMu.RLock()
	defer application.deps.Core.ViewMu.RUnlock()
	return application.deps.Core.Snapshot.Session.ID
}

// RecoverProviderContext recovers only provider context-exhaustion failures.
func (application *RecoveryApplication) RecoverProviderContext(err error, originalRequest string) error {
	if !contract.IsProviderContextExhaustion(err) {
		return nil
	}
	_, recoveryErr := application.RecoverProviderFailureFor(context.Background(), err, originalRequest)
	return recoveryErr
}

// RecoverProviderFailure is the active-session compatibility wrapper.
func (application *RecoveryApplication) RecoverProviderFailure(err error, originalRequest string) (bool, error) {
	return application.RecoverProviderFailureFor(context.Background(), err, originalRequest)
}

// RecoverProviderFailureFor replaces an unusable transcript with a bounded,
// private checkpoint for the target session.
func (application *RecoveryApplication) RecoverProviderFailureFor(ctx context.Context, err error, originalRequest string) (bool, error) {
	sessionID := application.sessionID(ctx)
	failureKind := contract.ClassifyProviderFailure(err)
	if failureKind == contract.ProviderFailureNone {
		return false, nil
	}
	prefix, heading, summary := providerRecoveryDetails(failureKind)

	application.deps.Core.ViewMu.Lock()
	checkpoint := ""
	if state := application.deps.Tasks.CurrentTaskExecutionFor(sessionID); state != nil {
		checkpoint = state.ContextSummary()
		state.Status = protocol.TurnInterrupted
	}
	requestID := application.deps.Core.Snapshot.Chat.RequestID
	application.deps.Tasks.SetTaskStateLocked(requestID, protocol.TurnInterrupted, summary)
	application.deps.Core.ViewMu.Unlock()

	recoveryNote := prefix + "\n## " + heading + `
The raw transcript was removed. Continue from the durable task checkpoint below;
use targeted tools to reacquire omitted detail, then deliver a result or state
what information is still needed. Do not assume a timed-out tool call can be
replayed safely.

` + checkpoint

	history := application.deps.EngineHistory(sessionID)
	recovered := RetainedSystemOnly(history)
	recovered = append(recovered,
		contract.EngineMessage{Role: "system", Content: recoveryNote, ContentSet: true},
		contract.EngineMessage{Role: "user", Content: NonEmptyProviderInput(originalRequest), ContentSet: true},
	)
	if err := application.deps.ReplaceHistory(sessionID, recovered); err != nil {
		return false, fmt.Errorf("recover provider context: %w", err)
	}
	return true, nil
}

// RetryContextRecovery gives one rejected request a bounded recovery turn.
func (application *RecoveryApplication) RetryContextRecovery(ctx context.Context, requestID string, onChunk func(string)) error {
	sessionID := application.sessionID(ctx)
	application.deps.Core.ViewMu.Lock()
	state := application.deps.Tasks.CurrentTaskExecutionFor(sessionID)
	if state == nil || state.RequestID != requestID {
		application.deps.Core.ViewMu.Unlock()
		return fmt.Errorf("resume context recovery: task state is unavailable")
	}
	application.deps.Tasks.ResumeTaskLocked(requestID, "Context was reset to a bounded checkpoint; the Agent is continuing with targeted reads.")
	revision := application.deps.View.BumpLocked()
	application.deps.Core.ViewMu.Unlock()
	application.deps.PublishSessionEvent(event.EventSnapshotChanged, revision, requestID, sessionID, nil)

	recoveryInput, err := application.deps.Context.PrepareExecutionContextFor(sessionID, requestID, contextRecoveryAgentInput)
	if err != nil {
		return err
	}
	_, err = application.deps.ChatStream(ctx, sessionID, recoveryInput, onChunk)
	if contextErr := application.deps.Context.TakeContextControlFailure(requestID); contextErr != nil {
		return contextErr
	}
	return err
}

// RetryableAfterRecovery reports whether a failed request is safe to replay.
func RetryableAfterRecovery(err error) bool {
	return contract.IsProviderContextExhaustion(err) || contract.ClassifyProviderFailure(err) == contract.ProviderFailureHistory
}

// NonEmptyProviderInput supplies the provider-safe fallback for an empty user
// request during recovery.
func NonEmptyProviderInput(input string) string {
	if strings.TrimSpace(input) != "" {
		return input
	}
	return "[Seelex recovery note: the submitted request was empty. Ask the user to provide the missing request details.]"
}

func providerRecoveryDetails(kind contract.ProviderFailureKind) (prefix, heading, summary string) {
	switch kind {
	case contract.ProviderFailureContext:
		return contextRecoveryPrefix, "Context Recovery", "The provider context window was exceeded; a bounded checkpoint was prepared."
	case contract.ProviderFailureHistory:
		return providerRecoveryPrefix, "History Recovery", "The provider rejected an invalid conversation record; a bounded checkpoint was prepared."
	case contract.ProviderFailureTimeout:
		return providerRecoveryPrefix, "Recoverable Provider Interruption", "The model service timed out; the task was marked interrupted and will be persisted before the UI reports completion."
	case contract.ProviderFailureServer:
		return providerRecoveryPrefix, "Recoverable Provider Interruption", "The model service was temporarily unavailable; the task was marked interrupted and will be persisted before the UI reports completion."
	default:
		return "", "", ""
	}
}

// RemoveProviderContextRecovery removes recovery/control envelopes from the
// active session after a completed turn.
func (application *RecoveryApplication) RemoveProviderContextRecovery() error {
	application.deps.Core.ViewMu.RLock()
	sessionID := application.deps.Core.Snapshot.Session.ID
	application.deps.Core.ViewMu.RUnlock()
	return application.RemoveProviderContextRecoveryFor(sessionID)
}

// RemoveProviderContextRecoveryFor removes recovery/control envelopes from one
// session's engine history.
func (application *RecoveryApplication) RemoveProviderContextRecoveryFor(sessionID string) error {
	history := application.deps.EngineHistory(sessionID)
	filtered := make([]contract.EngineMessage, 0, len(history))
	removed := false
	for _, message := range history {
		if message.Role == "system" && (strings.HasPrefix(message.Content, contextRecoveryPrefix) ||
			strings.HasPrefix(message.Content, providerRecoveryPrefix) ||
			strings.HasPrefix(message.Content, AutonomousCompactionPrefix)) {
			removed = true
			continue
		}
		if message.Role == "user" && (strings.HasPrefix(message.Content, contextRecoveryPrefix) || strings.HasPrefix(message.Content, providerRecoveryPrefix)) {
			_, original, found := strings.Cut(message.Content, contextRecoveryRequestDelimiter)
			if found {
				message.Content = original
				message.ContentSet = true
				filtered = append(filtered, message)
			}
			removed = true
			continue
		}
		filtered = append(filtered, message)
	}
	if !removed {
		return nil
	}
	if err := application.deps.ReplaceHistory(sessionID, filtered); err != nil {
		return fmt.Errorf("remove provider context recovery: %w", err)
	}
	return nil
}
