package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/session"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	seelplan "github.com/RedHuang-0622/seelex/seelebridge/plan"
	selexsession "github.com/RedHuang-0622/seelex/session"
)

func (service *Service) handleToolStart(ctx context.Context, name, id, arguments string) {
	sessionID := sessionIDFromContext(ctx)
	if sessionID == "" {
		service.ViewMu.RLock()
		sessionID = service.Core.Snapshot.Session.ID
		service.ViewMu.RUnlock()
	}
	// 先落地本会话未交付的流式文本（工具消息之前），与活跃视图无关。
	service.flushStreamBatcherFor(sessionID)
	service.ViewMu.Lock()
	active := service.isActiveSessionLocked(sessionID)
	if sessionID == "" {
		sessionID = service.Core.Snapshot.Session.ID
	}
	var planBinding *dto.PlanBranchBinding
	service.components.tasks.EnsureToolCallTranscriptLocked(sessionID, name, id, arguments)
	var message Message
	if active {
		tool := &ToolCall{ID: id, Name: name, Arguments: arguments, Status: "running"}
		message = *service.appendMessageLocked("tool", "", tool)
	} else {
		// 阶段 1：后台会话工具消息写自身 view（hot_attach 回看可见）。
		message = *service.appendSessionMessageLocked(sessionID, "tool", "", &ToolCall{ID: id, Name: name, Arguments: arguments, Status: "running"})
	}

	// plan_load 启动时：解析 DAG 并初始化 PlanState（Plan 状态属活跃会话；
	// 后台并行会话不修改共享 Plan 快照）。
	if active && name == "plan_load" {
		service.updatePlanFromLoad(arguments)
		if state := service.components.tasks.CurrentTaskExecution(); state != nil && state.RequestID == service.Core.Snapshot.Chat.RequestID {
			state.PlanArguments = arguments
		}
	}
	// plan_clear 启动时：清空 PlanState
	if active && name == "plan_clear" {
		service.Core.Snapshot.Runtime.Plan = nil
		service.components.tasks.SeedPlanProjection(service.Core.Snapshot.Session.ID, nil)
		if state := service.components.tasks.CurrentTaskExecution(); state != nil && state.RequestID == service.Core.Snapshot.Chat.RequestID {
			state.PlanArguments = ""
		}
	}
	if active && name == "plan_run" {
		binding := service.planBranchBindingLocked()
		planBinding = &binding
	}

	revision := uint64(0)
	if active {
		revision = service.bumpLocked()
	}
	requestID := service.Core.Snapshot.Chat.RequestID
	if !active {
		if task := service.components.tasks.CurrentTaskExecutionFor(sessionID); task != nil {
			requestID = task.RequestID
		}
	}
	service.ViewMu.Unlock()
	if planBinding != nil {
		service.Deps.Runtime.SetPlanBranchBinding(*planBinding)
	}
	service.publishSessionEvent(EventToolStarted, revision, requestID, sessionID, message)
	// plan_load/plan_clear/plan_run 已改 PlanState：被动同步 plan → task
	// 注册表并发布 worktable/task 增量，再发最新 runtime.changed。
	service.refreshWorkTableFromSources()
	service.publishSessionEvent(EventRuntimeChanged, service.Snapshot().Revision, requestID, sessionID, service.Snapshot().Runtime)
}

func (service *Service) planBranchBindingLocked() dto.PlanBranchBinding {
	binding := dto.PlanBranchBinding{
		SessionID: service.Core.Snapshot.Session.ID,
		AccountID: service.Core.Snapshot.Runtime.Account,
		TraceID:   service.Core.Snapshot.Chat.RequestID,
	}
	if workspace := service.Core.Snapshot.CurrentWorkspace; workspace != nil {
		binding.WorkspaceID = workspace.ID
	}
	if plan := service.Core.Snapshot.Runtime.Plan; plan != nil {
		binding.PlanID = plan.EntryNodeID
		binding.EntryNodeID = plan.EntryNodeID
	}
	return binding
}

func (service *Service) handleToolComplete(name, id, result string, toolErr error, duration time.Duration) {
	service.handleToolCompleteObserved(context.Background(), name, id, "", result, toolErr, duration, nil)
}

// handleToolCompleteObserved 把生产完成投影保持在一处，同时允许 ToolHookBridge
// 在显式开启后端诊断时包围少数阻塞边界。
func (service *Service) handleToolCompleteObserved(ctx context.Context, name, id, arguments, result string, toolErr error, duration time.Duration, observe func(string)) {
	emit := func(stage string) {
		if observe != nil {
			observe(stage)
		}
	}
	sessionID := sessionIDFromContext(ctx)
	if sessionID == "" {
		service.ViewMu.RLock()
		sessionID = service.Core.Snapshot.Session.ID
		service.ViewMu.RUnlock()
	}
	emit("toolhook.complete.flush.start")
	service.flushStreamBatcherFor(sessionID)
	emit("toolhook.complete.flush.done")
	runtimeProjection := service.collectRuntimeProjectionFor(context.Background(), sessionID)
	emit("toolhook.complete.lock.start")
	service.ViewMu.Lock()
	// sessionID 回退必须在锁内解析：Snapshot.Session.ID 由
	// ApplyRuntimeProjectionLocked 等写者持 Core.ViewMu 修改，锁外读取与
	// handleToolComplete 并发时构成数据竞争（-race 可复现）。
	if sessionID == "" {
		sessionID = service.Core.Snapshot.Session.ID
	}
	active := service.isActiveSessionLocked(sessionID)
	emit("toolhook.complete.lock.done")
	toolArguments := arguments
	if toolArguments == "" {
		// G5 访问器化：经 View.mu 读会话可见投影（Read），不在 ViewMu 下
		// 裸字段穿越。
		service.components.view.SessionViewReadLocked(sessionID, func(view *selexsession.View) {
			for index := len(view.Conversation) - 1; index >= 0; index-- {
				tool := view.Conversation[index].Tool
				if tool != nil && tool.ID == id {
					toolArguments = tool.Arguments
					return
				}
			}
		})
	}
	emit("toolhook.complete.transcript.start")
	providerResult, _ := service.components.tasks.RecordToolTranscriptLocked(sessionID, name, id, toolArguments, result, toolErr)
	emit("toolhook.complete.transcript.done")
	emit("toolhook.complete.task.start")
	requestID := service.Core.Snapshot.Chat.RequestID
	if !active {
		if st := service.components.tasks.CurrentTaskExecutionFor(sessionID); st != nil {
			requestID = st.RequestID
		}
	}
	service.components.tasks.ObserveTool(task_context.ToolObservation{
		RequestID: requestID, Name: name, Result: providerResult, Err: toolErr,
	})
	emit("toolhook.complete.task.done")
	status, errorText := "success", ""
	if toolErr != nil {
		status, errorText = "error", presentToolError(name, toolErr)
	}
	// 快照截断（根因链治本一环）：可见会话只放预览
	// （≤ limits.snapshot_tool_output_chars），完整输出归档 result_ref，
	// 前端"加载完整输出"再取——60KB 级全文不再随快照/事件进渲染进程。
	visibleContent, resultRef, truncated, totalChars := service.boundToolResultForSnapshot(name, result)
	// G5 访问器化：工具状态写回经 View.mu（Mutate），与流式增量/镜像同锁
	// 序（View.mu 叶子锁），消除 ViewMu 下的裸字段穿越。
	service.components.view.SessionViewMutateLocked(sessionID, func(view *selexsession.View) {
		for index := len(view.Conversation) - 1; index >= 0; index-- {
			tool := view.Conversation[index].Tool
			if tool != nil && tool.ID == id {
				tool.Status, tool.Result, tool.Error, tool.Duration = status, visibleContent, errorText, duration
				tool.ResultRef, tool.Truncated, tool.TotalChars = resultRef, truncated, totalChars
				break
			}
		}
	})
	service.mirrorActiveViewLocked()
	if active && name == "read_file" && toolErr == nil {
		service.recordReadFileForSessionLocked(sessionID, toolArguments)
	}
	if active && name == "plan_load" && toolErr == nil {
		service.components.tasks.PushLoadedPlanLocked(toolArguments, time.Now())
	}
	if active && name == "plan_clear" && toolErr == nil {
		service.components.tasks.ClearActivePlanLocked()
	}
	content := visibleContent
	if toolErr != nil {
		content = errorText
	}

	var planFailure *Interaction
	if active && name == "plan_run" {
		switch {
		case toolErr != nil:
			planFailure = service.handlePlanRunFailureLocked(toolErr.Error(), result)
		default:
			if failure := planRunFailure(result); failure != "" {
				planFailure = service.handlePlanRunFailureLocked(failure, result)
			} else {
				service.updatePlanFromRunResult(result)
			}
		}
	}

	var message Message
	var assistant *Message
	if active {
		message = *service.appendMessageLocked("tool_result", content, &ToolCall{
			ID: id, Name: name, Result: visibleContent, Error: errorText,
			Status: status, Duration: duration,
			ResultRef: resultRef, Truncated: truncated, TotalChars: totalChars,
		})
		// Only append empty assistant if the last message isn't already an empty assistant
		if n := len(service.Core.Snapshot.Conversation); n == 0 || service.Core.Snapshot.Conversation[n-1].Role != "assistant" || service.Core.Snapshot.Conversation[n-1].Content != "" || service.Core.Snapshot.Conversation[n-1].Tool != nil {
			appended := *service.appendMessageLocked("assistant", "", nil)
			assistant = &appended
		}
	} else {
		message = *service.appendSessionMessageLocked(sessionID, "tool_result", content, &ToolCall{
			ID: id, Name: name, Result: visibleContent, Error: errorText,
			Status: status, Duration: duration,
			ResultRef: resultRef, Truncated: truncated, TotalChars: totalChars,
		})
	}
	emit("toolhook.complete.runtime.start")
	// 每会话投影写回本会话槽（G1）：后台会话的工具边界投影保留在自身槽，
	// 不再因非活跃被丢弃；活跃会话由协调器镜像 Snapshot.Runtime。
	service.applyRuntimeProjectionForLocked(sessionID, runtimeProjection)
	emit("toolhook.complete.runtime.done")
	revision := uint64(0)
	if active {
		revision = service.bumpLocked()
	}
	service.ViewMu.Unlock()
	emit("toolhook.complete.unlock.done")
	emit("toolhook.complete.event.start")
	service.publishSessionEvent(EventToolCompleted, revision, requestID, sessionID, message)
	if planFailure != nil {
		service.publishSessionEvent(EventInteractionOpened, revision, planFailure.ID, sessionID, planFailure)
	}
	if assistant != nil {
		service.publishSessionEvent(EventMessageAdded, revision, requestID, sessionID, *assistant)
	}
	// 工具完成：todo/taskadd 已写注册表，plan/subagent 状态已更新——
	// 统一走被动同步 + 增量发布，再发最新 runtime.changed。
	service.refreshWorkTableFromSources()
	service.publishSessionEvent(EventRuntimeChanged, service.Snapshot().Revision, requestID, sessionID, service.Snapshot().Runtime)
	emit("toolhook.complete.event.done")
}

type ToolHookBridge struct {
	mu         sync.Mutex
	service    *Service
	toolSeq    uint64
	pending    map[string][]string
	diagnostic ToolHookDiagnosticObserver
}

// ToolHookDiagnosticEvent is metadata-only instrumentation for the boundary
// between the framework session loop and application event projection.
// Arguments and tool output are intentionally excluded.
type ToolHookDiagnosticEvent struct {
	Stage string
	Name  string
	Err   error
}

// ToolHookDiagnosticObserver receives best-effort ToolHookBridge stages.
type ToolHookDiagnosticObserver func(ToolHookDiagnosticEvent)

func NewToolHookBridge() *ToolHookBridge { return &ToolHookBridge{} }
func (bridge *ToolHookBridge) Bind(service *Service) {
	bridge.mu.Lock()
	bridge.service = service
	bridge.mu.Unlock()
}

// SetDiagnosticObserver 安装可选、尽力而为的生命周期诊断。传入 nil 关闭。
func (bridge *ToolHookBridge) SetDiagnosticObserver(observer ToolHookDiagnosticObserver) {
	bridge.mu.Lock()
	bridge.diagnostic = observer
	bridge.mu.Unlock()
}

func (bridge *ToolHookBridge) observeDiagnostic(event ToolHookDiagnosticEvent) {
	bridge.mu.Lock()
	observer := bridge.diagnostic
	bridge.mu.Unlock()
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer(event)
}
func (bridge *ToolHookBridge) Hooks() *session.LoopHooks {
	return &session.LoopHooks{
		OnLLMComplete: func(ctx context.Context, info session.LLMInfo) {
			bridge.mu.Lock()
			svc := bridge.service
			bridge.mu.Unlock()
			if svc != nil {
				svc.components.tasks.RecordLLMComplete(ctx, info)
			}
		},
		OnToolStart: func(ctx context.Context, info session.ToolCallInfo) {
			info = normalizePlanToolCallInfo(info)
			bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: "toolhook.start.enter", Name: info.Name})
			service, id := bridge.beginTool(info)
			bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: "toolhook.start.matched", Name: info.Name})
			if service != nil {
				bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: "toolhook.start.project.start", Name: info.Name})
				service.components.tasks.RecordReActToolCall(ctx)
				service.handleToolStart(ctx, info.Name, id, info.Arguments)
				bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: "toolhook.start.project.done", Name: info.Name})
			}
		},
		OnToolComplete: func(ctx context.Context, info session.ToolCallInfo) {
			info = normalizePlanToolCallInfo(info)
			bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: "toolhook.complete.enter", Name: info.Name, Err: info.Error})
			service, id := bridge.completeTool(info)
			bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: "toolhook.complete.matched", Name: info.Name, Err: info.Error})
			if service != nil {
				bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: "toolhook.complete.project.start", Name: info.Name, Err: info.Error})
				service.handleToolCompleteObserved(ctx, info.Name, id, info.Arguments, info.Result, info.Error, info.Duration, func(stage string) {
					bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: stage, Name: info.Name, Err: info.Error})
				})
				bridge.observeDiagnostic(ToolHookDiagnosticEvent{Stage: "toolhook.complete.project.done", Name: info.Name, Err: info.Error})
			}
		},
		OnIterationComplete: func(ctx context.Context, turn int) bool {
			bridge.mu.Lock()
			svc := bridge.service
			bridge.mu.Unlock()
			if svc == nil {
				return true
			}
			if !svc.components.tasks.AllowNextReActIteration(ctx, turn) {
				return false
			}
			// P1 goal 接线：回合边界登记 exec 账本水位并把 b→a TL 指令注入
			// 引擎历史（Session 锁内仅 AppendHistory 安全；可见记录在
			// runChat 结束点回放）。
			svc.GoalIterationCompleted(ctx)
			// 新 Session 装配（session.NewSession + Session.ChatStream）下，
			// OnIterationComplete 在 Session 锁内同步执行：回调不得重入 Session
			// 的历史操作（History/ReplaceHistory/AppendHistory），否则死锁。
			// 压缩决策移交 ContextController（seelectx.ContextController，
			// plan.md §3.5：OnIterationComplete 不再触发 compactTaskContext）；
			// 配对修复由 chat 边界（prepareProviderHistory / 批处理路径）
			// 承担，进度回调与事件流保持不变。
			if reentrant, ok := svc.Deps.Engine.(interface{ SessionBacked() bool }); ok && reentrant.SessionBacked() {
				// Session 锁内不可重入 AppendHistory（死锁）；每轮 ReAct
				// 迭代结束检查输入队列：非空 → 返回 false 中断本轮（本轮
				// 工具已全部完成，是安全边界），由 runChat 结尾的队列提升
				// 自动开启下一轮并清空队列——一轮一消费，无需等整条 loop。
				svc.ViewMu.RLock()
				queued := len(svc.activeQueuedChatRequestsLocked()) > 0
				svc.ViewMu.RUnlock()
				return !queued
			}
			svc.ViewMu.RLock()
			activeRequestID := svc.Core.Snapshot.Chat.RequestID
			svc.ViewMu.RUnlock()
			// The engine adds assistant/tool records after the initial preflight.
			// Repair empty content before its next provider request, not only
			// before loop 0. 残缺链配对修复（注入缺失 tool 结果）不在活跃
			// ReAct 中间态执行 —— 新 append 的 assistant 工具记录可能仍在
			// 执行中，误判会与即将到达的真结果重复；定稿链由装配/请求前
			// 的 PrepareProviderHistoryFor 补齐。
			if err := svc.components.history.PrepareNewHistoryContentFor(sessionIDFromContext(ctx)); err != nil {
				svc.components.context.RecordContextControlFailure(activeRequestID, err)
				return false
			}
			// 非 Session-backed 引擎（仅测试）：队列输入统一由 runChat 结尾
			// 单点消费并开启下一轮，不在此处注入引擎历史。
			return true
		},
	}
}

// normalizePlanToolCallInfo 保持应用快照与 Seele 执行的同一规范 DAG 表示。
// 非法适配器输入原样保留，使工具错误对用户可见。
func normalizePlanToolCallInfo(info session.ToolCallInfo) session.ToolCallInfo {
	if info.Name != "plan_load" {
		return info
	}
	canonical, err := seelplan.NormalizePlanLoadArguments(info.Arguments)
	if err == nil {
		info.Arguments = canonical
	}
	return info
}

func (bridge *ToolHookBridge) beginTool(info session.ToolCallInfo) (*Service, string) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	id := bridge.nextToolIDLocked()
	if bridge.pending == nil {
		bridge.pending = make(map[string][]string)
	}
	key := toolHookKey(info)
	bridge.pending[key] = append(bridge.pending[key], id)
	return bridge.service, id
}

func (bridge *ToolHookBridge) completeTool(info session.ToolCallInfo) (*Service, string) {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	key := toolHookKey(info)
	ids := bridge.pending[key]
	if len(ids) == 0 {
		return bridge.service, bridge.nextToolIDLocked()
	}
	id := ids[0]
	if len(ids) == 1 {
		delete(bridge.pending, key)
	} else {
		bridge.pending[key] = ids[1:]
	}
	return bridge.service, id
}

func (bridge *ToolHookBridge) nextToolIDLocked() string {
	bridge.toolSeq++
	return fmt.Sprintf("tool-%d", bridge.toolSeq)
}

func toolHookKey(info session.ToolCallInfo) string {
	return fmt.Sprintf("%d\x00%s\x00%s", info.Turn, info.Name, info.Arguments)
}
