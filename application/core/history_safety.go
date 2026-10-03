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
	"The provider rejected the previous conversation record (context too large, or an invalid tool-call record). The history now contains a bounded task checkpoint. " +
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

	recoveryNote := prefix + "\n## " + heading + `
The raw transcript was removed. Continue from the durable task checkpoint below;
use targeted tools to reacquire omitted detail, then deliver a result or state
what information is still needed. Do not assume a timed-out tool call can be
replayed safely.

` + checkpoint

	history := service.engineHistoryFor(sessionID)
	// 恢复路径只保留 system 指令（RetainedSystemOnly）：provider 已拒绝过大
	// 上下文，不得把已定稿轮次带进恢复信封。
	recovered := context_runtime.RetainedSystemOnly(history)
	// 恢复说明是 Seelex 编排事实，走 provider system；原始用户请求保持 user。
	// 两条分开，成功恢复后才能只删说明、保留真实 user 输入。
	recovered = append(recovered,
		EngineMessage{Role: "system", Content: recoveryNote, ContentSet: true},
		EngineMessage{Role: "user", Content: nonEmptyProviderInput(originalRequest), ContentSet: true},
	)
	if err := service.replaceEngineHistory(sessionID, recovered); err != nil {
		return false, fmt.Errorf("recover provider context: %w", err)
	}
	return true, nil
}

// retryContextRecovery 在 provider 于执行前拒绝请求（上下文长度 / 记录不合法）
// 时，给同一 Agent 一次安全的恢复回合。是否重放由调用方经
// retryableAfterRecovery 判定：超时与服务器故障可能留下不确定的工具副作用，
// 不得重放。
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
	// tool 配对协议的两种措辞都算"记录不合法"：孤儿/乱序 tool 结果（2026-09-20
	// 现场）与 assistant 宣告的调用没拿到足够回执（"insufficient tool messages
	// following tool_calls message"，2026-09-24 探针 P4 实测、现场复现）。
	if strings.Contains(message, "chat content is empty") ||
		strings.Contains(message, "must be a response to a preceding message with") ||
		strings.Contains(message, "must be followed by tool messages") {
		// 后两者 = provider 拒绝 tool 配对协议（孤儿/乱序 tool 结果，2026-09-20
		// 现场）：同属"请求里的记录不合法"，走有界检查点的历史恢复，而不是把
		// 会话循环判死。wire 出口已按协议剔除这类形状（seelexctx 装配器），
		// 这里保留兜底——provider 的措辞与严格度不由我们决定。
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

// retryableAfterRecovery 判定"被 provider 拒绝的这次请求"能否安全地重放一次
// 有界恢复回合。恢复（recoverProviderFailureFor）已把 transcript 换成有界
// 检查点，重放不会把被拒的记录再发一遍；判据是"这次拒绝发生在工具执行之前"：
//   - 上下文耗尽：provider 在装配后、执行前拒绝，没有工具副作用；
//   - 记录不合法（tool 配对协议 400）：同样是请求校验就被拒。2026-09-28 现场
//     `session loop 0: seelebridge: stream with account "goalplan-1": ChatClient
//     stream: HTTP 400 ... insufficient tool messages following tool_calls
//     message` 把会话循环直接判死（恢复完却不重放，用户只看到"会话中断"）。
//
// 超时（504）与服务端故障刻意排除：工具副作用不确定，不得自动重放。
func retryableAfterRecovery(err error) bool {
	return isProviderContextExhaustion(err) || classifyProviderFailure(err) == providerFailureHistory
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

// removeProviderContextRecoveryFor 清理引擎私有的上下文控制信封：provider
// 恢复说明与自主压缩帧都不属于会话事实，回合结束（err == nil）后移除，
// 使下一轮装配从 transcript 重新构建、不在保留前缀里累积控制消息。
func (service *Service) removeProviderContextRecoveryFor(sessionID string) error {
	history := service.engineHistoryFor(sessionID)
	filtered := make([]EngineMessage, 0, len(history))
	removed := false
	for _, message := range history {
		if message.Role == "system" && (strings.HasPrefix(message.Content, contextRecoveryPrefix) ||
			strings.HasPrefix(message.Content, providerRecoveryPrefix) ||
			strings.HasPrefix(message.Content, context_runtime.AutonomousCompactionPrefix)) {
			removed = true
			continue
		}
		// 兼容旧的单条 user 恢复信封：切出原请求后保留为 user 行。
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
