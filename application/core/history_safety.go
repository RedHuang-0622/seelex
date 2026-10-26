package core

import (
	"context"
	"fmt"
	context_runtime "github.com/RedHuang-0622/seelex/application/conversation/context"
	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	"github.com/RedHuang-0622/seelex/contract"
	"strings"
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
	if !contract.IsProviderContextExhaustion(err) {
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
	sessionID := task_context.SessionIDFromContext(ctx)
	if sessionID == "" {
		// 同上：回退读当前会话需持 Core.ViewMu（裸读与热切换写 Snapshot.Session 竞争）。
		service.ViewMu.RLock()
		sessionID = service.Core.Snapshot.Session.ID
		service.ViewMu.RUnlock()
	}
	failureKind := contract.ClassifyProviderFailure(err)
	if failureKind == contract.ProviderFailureNone {
		return false, nil
	}
	prefix, heading, summary := providerRecoveryDetails(failureKind)

	service.ViewMu.Lock()
	checkpoint := ""
	if state := service.components.tasks.CurrentTaskExecutionFor(sessionID); state != nil {
		checkpoint = state.ContextSummary()
		state.Status = TurnInterrupted
	}
	requestID := service.Core.Snapshot.Chat.RequestID
	service.components.tasks.SetTaskStateLocked(requestID, TurnInterrupted, summary)
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
		contract.EngineMessage{Role: "system", Content: recoveryNote, ContentSet: true},
		contract.EngineMessage{Role: "user", Content: nonEmptyProviderInput(originalRequest), ContentSet: true},
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
	sessionID := task_context.SessionIDFromContext(ctx)
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

// retryableAfterRecovery 判定"被 provider 拒绝的这次请求"能否安全地重放一次
// 有界恢复回合。恢复（recoverProviderFailureFor）已把 transcript 换成有界
// 检查点，重放不会把被拒的记录再发一遍；判据是"这次拒绝发生在工具执行之前"：
//   - 上下文耗尽：provider 在装配后、执行前拒绝，没有工具副作用；
//   - 记录不合法（tool 配对协议 400）：同样是请求校验就被拒。2026-09-28 现场
//     `session loop 0: seelex: stream with account "goalplan-1": ChatClient
//     stream: HTTP 400 ... insufficient tool messages following tool_calls
//     message` 把会话循环直接判死（恢复完却不重放，用户只看到"会话中断"）。
//
// 超时（504）与服务端故障刻意排除：工具副作用不确定，不得自动重放。
func retryableAfterRecovery(err error) bool {
	return contract.IsProviderContextExhaustion(err) || contract.ClassifyProviderFailure(err) == contract.ProviderFailureHistory
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

func (service *Service) removeProviderContextRecovery() error {
	// 会话 ID 在 Core.ViewMu 读锁下取值（与 startChat 同一收口口径）。
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	service.ViewMu.RUnlock()
	return service.removeProviderContextRecoveryFor(sessionID)
}

// removeProviderContextRecoveryFor 清理引擎私有的上下文控制信封：provider
// 恢复说明与自主压缩帧都不属于会话事实，回合结束（err == nil）后移除，
// 使下一轮装配从 transcript 重新构建、不在保留前缀里累积控制消息。
func (service *Service) removeProviderContextRecoveryFor(sessionID string) error {
	history := service.engineHistoryFor(sessionID)
	filtered := make([]contract.EngineMessage, 0, len(history))
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
