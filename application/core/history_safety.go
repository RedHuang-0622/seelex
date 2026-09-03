package core

import (
	"context"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
)

const contextRecoveryPrefix = "<!-- seelex:context-recovery:v1 -->"
const providerRecoveryPrefix = "<!-- seelex:provider-recovery:v1 -->"
const contextRecoveryRequestDelimiter = "\n## Original User Request\n"

const contextRecoveryAgentInput = "<!-- seelex:context-recovery-agent:v1 -->\n" +
	"The provider rejected the previous context as too large. The history now contains a bounded task checkpoint. " +
	"Continue from that checkpoint without assuming omitted details. If more detail is required, use a narrower, paginated, or filtered tool call; do not request a full large result. " +
	"Deliver the task if the checkpoint evidence is sufficient."

func nonEmptyProviderInput(input string) string {
	if strings.TrimSpace(input) != "" {
		return input
	}
	return "[Seelex recovery note: the submitted request was empty. Ask the user to provide the missing request details.]"
}

// recoverProviderContext 在 provider 因超出上下文窗口拒绝累积 transcript 后，
// 保留最小、证据优先的续接记录。刻意不使用猜测的 token/字符上限：provider
// 已给出"全量历史不可用"的权威信号。记录仅留在引擎私有区，下一轮成功后
// 恢复原始用户请求。
func (service *Service) recoverProviderContext(err error, originalRequest string) error {
	if !isProviderContextExhaustion(err) {
		return nil
	}
	_, recoveryErr := service.recoverProviderFailure(err, originalRequest)
	return recoveryErr
}

// recoverProviderFailure 仅在 provider 拒绝请求后，把不可用 transcript 替换
// 为有界、私有的续接记录（活跃会话兼容包装）。它从不自动重放超时工具轮：
// 504 意味着工具侧效果不确定，用户必须从 checkpoint 显式继续。
func (service *Service) recoverProviderFailure(err error, originalRequest string) (bool, error) {
	return service.recoverProviderFailureFor(context.Background(), err, originalRequest)
}

// recoverProviderFailureFor 与 recoverProviderFailure 相同，但 ctx 携带会话
// ID 时按目标会话路由（多会话并行执行路径）。
func (service *Service) recoverProviderFailureFor(ctx context.Context, err error, originalRequest string) (bool, error) {
	sessionID := sessionIDFromContext(ctx)
	if sessionID == "" {
		sessionID = service.Core.Snapshot.Session.ID
	}
	failureKind := classifyProviderFailure(err)
	if failureKind == providerFailureNone {
		return false, nil
	}
	prefix, heading, summary := providerRecoveryDetails(failureKind)

	service.ViewMu.Lock()
	checkpoint := ""
	if state := service.components.tasks.CurrentTaskExecutionFor(sessionID); state != nil {
		checkpoint = state.ContextSummary()
		state.Status = task_context.StatusInterrupted
	}
	requestID := service.Core.Snapshot.Chat.RequestID
	service.components.tasks.SetTaskStateLocked(requestID, TaskInterrupted, summary)
	service.ViewMu.Unlock()

	recovery := prefix + "\n## " + heading + `
The raw transcript was removed. Continue from the durable task checkpoint below;
use targeted tools to reacquire omitted detail, then deliver a result or state
what information is still needed. Do not assume a timed-out tool call can be
replayed safely.

` + checkpoint + contextRecoveryRequestDelimiter + nonEmptyProviderInput(originalRequest)

	history := service.engineHistoryFor(sessionID)
	// 恢复路径只保留 system 指令（RetainedSystemOnly）：provider 已拒绝过大
	// 上下文，不得把已定稿轮次带进恢复信封。
	recovered := context_runtime.RetainedSystemOnly(history)
	recovered = append(recovered, EngineMessage{Role: "user", Content: recovery, ContentSet: true})
	if err := service.replaceEngineHistory(sessionID, recovered); err != nil {
		return false, fmt.Errorf("recover provider context: %w", err)
	}
	return true, nil
}

// retryContextRecovery 在 provider 因上下文长度在执行前拒绝请求时，给同一
// Agent 一次安全的恢复回合。刻意限定于上下文耗尽：超时与服务器故障可能留下
// 不确定的工具副作用，不得重放。
func (service *Service) retryContextRecovery(ctx context.Context, requestID string, onChunk func(string)) error {
	sessionID := sessionIDFromContext(ctx)
	if sessionID == "" {
		sessionID = service.Core.Snapshot.Session.ID
	}
	service.ViewMu.Lock()
	state := service.components.tasks.CurrentTaskExecutionFor(sessionID)
	if state == nil || state.RequestID != requestID {
		service.ViewMu.Unlock()
		return fmt.Errorf("resume context recovery: task state is unavailable")
	}
	service.components.tasks.ResumeTaskLocked(requestID, "Context was reset to a bounded checkpoint; the Agent is continuing with targeted reads.")
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, requestID, sessionID, nil)

	recoveryInput, prepareErr := service.components.context.PrepareExecutionContextFor(sessionID, requestID, contextRecoveryAgentInput)
	if prepareErr != nil {
		return prepareErr
	}
	_, err := service.chatStream(ctx, sessionID, recoveryInput, onChunk)
	if contextErr := service.components.context.TakeContextControlFailure(requestID); contextErr != nil {
		return contextErr
	}
	return err
}

type providerFailureKind string

const (
	providerFailureNone    providerFailureKind = ""
	providerFailureContext providerFailureKind = "context_exhausted"
	providerFailureHistory providerFailureKind = "invalid_history"
	providerFailureTimeout providerFailureKind = "timeout"
	providerFailureServer  providerFailureKind = "server_unavailable"
)

func classifyProviderFailure(err error) providerFailureKind {
	if isProviderContextExhaustion(err) {
		return providerFailureContext
	}
	if err == nil {
		return providerFailureNone
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "chat content is empty") {
		return providerFailureHistory
	}
	if strings.Contains(message, "http 504") || strings.Contains(message, "timeout_error") ||
		strings.Contains(message, "upstream timeout") || strings.Contains(message, "request timeout") {
		return providerFailureTimeout
	}
	if strings.Contains(message, "http 500") || strings.Contains(message, "http 502") ||
		strings.Contains(message, "http 503") || strings.Contains(message, "http 529") ||
		strings.Contains(message, "server_error") || strings.Contains(message, "internal server error") {
		return providerFailureServer
	}
	return providerFailureNone
}

func providerRecoveryDetails(kind providerFailureKind) (prefix, heading, summary string) {
	switch kind {
	case providerFailureContext:
		return contextRecoveryPrefix, "Context Recovery", "The provider context window was exceeded; a bounded checkpoint was prepared."
	case providerFailureHistory:
		return providerRecoveryPrefix, "History Recovery", "The provider rejected an invalid conversation record; a bounded checkpoint was prepared."
	case providerFailureTimeout:
		return providerRecoveryPrefix, "Recoverable Provider Interruption", "The model service timed out; the task was marked interrupted and will be persisted before the UI reports completion."
	case providerFailureServer:
		return providerRecoveryPrefix, "Recoverable Provider Interruption", "The model service was temporarily unavailable; the task was marked interrupted and will be persisted before the UI reports completion."
	default:
		return "", "", ""
	}
}

func isProviderContextExhaustion(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "context window exceeds") ||
		strings.Contains(message, "context length") ||
		strings.Contains(message, "maximum context") ||
		strings.Contains(message, "上下文窗口")
}

func (service *Service) removeProviderContextRecovery() error {
	return service.removeProviderContextRecoveryFor(service.Core.Snapshot.Session.ID)
}

func (service *Service) removeProviderContextRecoveryFor(sessionID string) error {
	history := service.engineHistoryFor(sessionID)
	filtered := make([]EngineMessage, 0, len(history))
	removed := false
	for _, message := range history {
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
	if err := service.replaceEngineHistory(sessionID, filtered); err != nil {
		return fmt.Errorf("remove provider context recovery: %w", err)
	}
	return nil
}
