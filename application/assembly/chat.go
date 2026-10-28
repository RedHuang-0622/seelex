package assembly

import (
	"context"
	"fmt"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	context_runtime "github.com/RedHuang-0622/seelex/application/conversation/context"
	"github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"log"
	"os"
	"time"
)

// runChatDebug 是 SEELEX_TEST_DEBUG=1 门控的临时诊断日志（复跑噪音点时
// 定位会话级执行路径；定位完成后清理）。
func runChatDebug(format string, args ...any) {
	if os.Getenv("SEELEX_TEST_DEBUG") == "1" {
		log.Printf("[runChat-debug] "+format, args...)
	}
}

// isActiveSessionLocked 判定指定会话是否为共享快照归属会话（锁内调用；
// 空会话 ID 视为活跃，兼容 draft 状态）。
func (service *Service) isActiveSessionLocked(sessionID string) bool {
	return sessionID == "" || sessionID == service.Core.Snapshot.Session.ID
}

// nextChatRequestIDLocked 生成跨会话唯一的聊天请求 ID（调用方持有
// Core.ViewMu）。时间戳 + 单调序号：仅时间戳在 Windows（UnixNano 分辨率约
// 0.5ms）下并行会话启动会碰撞。
func (service *Service) nextChatRequestIDLocked() string {
	service.ChatSeq++
	return fmt.Sprintf("chat-%d-%d", time.Now().UnixNano(), service.ChatSeq)
}

// startChat 在**当前视图会话**上启动 ReAct 对话。
//
// 会话 ID 必须持 Core.ViewMu 读取：视图指针由热挂载/冷加载在持锁段内整体改写
// （session_lifecycle.go 的 `service.Core.Snapshot.Session = SessionState{...}`），
// 无锁读会与「切换会话」并发构成数据竞争——CI 的 race-and-coverage 作业已在
// TestWorkspaceSwitchConcurrentWithBackgroundPersist 上复现（Write 在 hotAttachSession、
// 无锁 Read 就在本函数）。这里只取一次 ID；后续校验与状态机仍由 startChatFor
// 自己持锁完成，两段之间的切换不会让本函数写错状态，只是这一轮落在调用发生时的
// 那个会话上。
// startChatFor 在指定会话启动 ReAct 对话（多会话并行：后台会话不写活跃
// 快照，只维护会话级任务/plan 状态与按会话路由的事件）。
// runChat 在独立 goroutine 中执行一次会话提交：委托 Seele loop（9.2 边界，
// thin-wrapper-session-design.md §1.2/§6）——
//   - 提交：session.Engine.ChatStreamFor（显式 sid 路由，切换不串写）；
//   - 执行：Seele ReAct loop 在单次 ChatStream 内完成全部工具轮次，core
//     不自编循环（UC6 结构断言，见 chat_delegation_test.go）；
//   - 观察：Seele LoopHooks 链（seelebridge Chain：DiagnosticHook →
//     StageHook → SummaryHook）只做记录/透传，不改变 loop 控制流；
//     core 侧仅投影 ΔV/事件（appendDelta / appendVisibleDeltaBackground）。
//   - 收尾：context 恢复、task 终态、会话粒度 persist、ReleaseWorkingHistory。
//
// 事件指纹方法：相同输入序列 → 相同事件序列（kind + session/request/message
// ID 序数归一化），见 session_decoupling_test.go TestEventFingerprintStable。
// finalizeReActBudget 在工具预算耗尽后保留一次纯文本交付回合。常规循环在
// 此之前已停止；本回合让用户收到结果，而非裸预算错误。
// ensureViewPlanProjection 在进入终态工具前确保当前视图会话的协调器投影有
// 基线（迁移期：直设 Snapshot.Runtime.Plan 的路径/旧装配在 plan_load 前
// 没有种子时，从镜像补种；已有投影不覆盖）。
func (service *Service) ensureViewPlanProjection(ctx context.Context) {
	sessionID := task_context.SessionIDFromContext(ctx)
	if sessionID == "" {
		service.ViewMu.RLock()
		sessionID = service.Core.Snapshot.Session.ID
		service.ViewMu.RUnlock()
	}
	if sessionID == "" {
		return
	}
	service.ViewMu.RLock()
	plan := service.Core.Snapshot.Runtime.Plan
	service.ViewMu.RUnlock()
	service.components.tasks.EnsurePlanProjection(sessionID, plan)
}

// mirrorActivePlanAfterTerminal 在 check/complete 终态把协调器投影的最新
// plan 镜像进 Snapshot（这些工具以投影为权威变更点）。
func (service *Service) mirrorActivePlanAfterTerminal(ctx context.Context) {
	sessionID := task_context.SessionIDFromContext(ctx)
	if sessionID == "" {
		service.ViewMu.RLock()
		sessionID = service.Core.Snapshot.Session.ID
		service.ViewMu.RUnlock()
	}
	if sessionID == "" {
		return
	}
	plan := service.components.tasks.PlanProjectionCopy(sessionID)
	if plan == nil {
		return
	}
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	if sessionID == service.Core.Snapshot.Session.ID {
		service.Core.Snapshot.Runtime.Plan = plan
	}
}

// mirrorActiveTaskAfterTerminal 在终态工具落地后把活跃会话的权威任务状态
// 镜像进 Snapshot.Task（TaskService 不再直写快照；工具路径没有 runChat 收尾
// 的镜像段，须在此补齐，见 task_service.go lastTaskState 说明）。
func (service *Service) mirrorActiveTaskAfterTerminal(ctx context.Context) {
	sessionID := task_context.SessionIDFromContext(ctx)
	if sessionID == "" {
		service.ViewMu.RLock()
		sessionID = service.Core.Snapshot.Session.ID
		service.ViewMu.RUnlock()
	}
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	if sessionID == "" || sessionID != service.Core.Snapshot.Session.ID {
		return
	}
	if task := service.components.tasks.VisibleTaskStateFor(sessionID); task != nil {
		service.Core.Snapshot.Task = task
	}
}

// finalizeTaskExecution 把自然停止转换为可审计的完成/交接
// （TaskService.OnChatEnd 入口）。
func (service *Service) finalizeTaskExecution(requestID string) error {
	service.ensureViewPlanProjection(context.Background())
	err := service.components.tasks.FinalizeTask(context.Background(), task_context.ChatEndSummary{RequestID: requestID})
	if err == nil {
		service.mirrorActiveTaskAfterTerminal(context.Background())
	}
	return err
}

func (service *Service) removeReActBudgetFinalizationInput(sessionID string) error {
	history := service.engineHistoryFor(sessionID)
	filtered := make([]contract.EngineMessage, 0, len(history))
	removed := false
	for _, message := range history {
		if message.Role == "user" && message.Content == applicationconversation.ReactBudgetFinalizationInput {
			removed = true
			continue
		}
		filtered = append(filtered, message)
	}
	if !removed {
		return nil
	}
	if err := service.replaceEngineHistory(sessionID, filtered); err != nil {
		return fmt.Errorf("remove ReAct budget finalization input: %w", err)
	}
	return nil
}

func closedSignal() chan struct{} {
	idle := make(chan struct{})
	close(idle)
	return idle
}

func (service *Service) markBusyLocked() {
	select {
	case <-service.idle:
		service.idle = make(chan struct{})
	default:
	}
}

func (service *Service) markIdleLocked() {
	select {
	case <-service.idle:
	default:
		close(service.idle)
	}
}

func (service *Service) appendHistoryLocked(history []contract.EngineMessage) {
	service.appendHistoryLockedFor(service.Core.Snapshot.Session.ID, history)
}

// appendHistoryLockedFor 把引擎历史追加为指定会话的可见消息（冷加载无
// record 的旧格式会话恢复路径；阶段 1：写会话 view）。
func (service *Service) appendHistoryLockedFor(sessionID string, history []contract.EngineMessage) {
	for _, historyMessage := range history {
		if !service.components.sessionHistoryRuntime.IsVisibleHistoryMessage(historyMessage) {
			continue
		}
		// 引擎历史只有 provider role（这里的历史全部属于 EXEC 会话），按
		// task_context 的同一口径补默认归属，避免恢复后的旧会话又变成 AGENT。
		origin := model.MessageOrigin{RoleName: RoleNameMain, RoleSessionID: sessionID}
		if historyMessage.Role == "user" {
			origin.RoleName = RoleNameUser
		}
		if historyMessage.Role != "tool" && historyMessage.Content != "" && !context_runtime.IsProviderOnlyHistoryContent(historyMessage.Content) {
			content := historyMessage.Content
			if historyMessage.Role == "user" {
				content = DisplayUserInput(content)
			}
			appended := service.components.view.AppendMessageWithOriginLockedFor(sessionID, historyMessage.Role, content, nil, origin)
			if historyMessage.ReasoningContent != "" && appended != nil {
				appended.ReasoningContent = historyMessage.ReasoningContent
			}
		}
		for _, call := range historyMessage.ToolCalls {
			service.components.view.AppendMessageWithOriginLockedFor(sessionID, "tool", "", &model.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments, Status: dto.ToolEventSuccess}, origin)
		}
		if historyMessage.Role == "tool" {
			visible, ref, truncated, totalChars := service.boundToolResultForSnapshot(historyMessage.Name, historyMessage.Content)
			service.components.view.AppendMessageWithOriginLockedFor(sessionID, "tool_result", visible, &model.ToolCall{
				ID: historyMessage.ToolCallID, Name: historyMessage.Name,
				Result: visible, Status: dto.ToolEventSuccess,
				ResultRef: ref, Truncated: truncated, TotalChars: totalChars,
			}, origin)
		}
	}
}
