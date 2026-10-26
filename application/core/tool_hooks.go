package core

import (
	"context"
	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationtoolhooks "github.com/RedHuang-0622/seelex/application/execution/toolhooks"
	applicationtoolrecord "github.com/RedHuang-0622/seelex/application/execution/toolrecord"
	selexsession "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"time"
)

func (service *Service) handleToolStart(ctx context.Context, name, id, arguments string) {
	sessionID := task_context.SessionIDFromContext(ctx)
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
	service.components.tasks.EnsureToolCallTranscriptLocked(sessionID, name, id, arguments)
	// 工具轮正文既进入引擎历史，也经 onChunk 实时进入可见视图：在"本次迭代的
	// tool_call 事件刚就位 + 流式缓冲已 flush"的这一刻把它归位到该事件（按迭代
	// 增量）。回合收尾不再做事后填充——否则叙述会落到别的迭代/轮次（见
	// application/core/context_narration_attribution_test.go）。
	service.components.tasks.AttributeToolNarrationLocked(sessionID,
		model.TranscriptToolCall{ID: id, Name: name, Arguments: arguments},
		service.streamedAssistantTextLocked(sessionID))
	var message model.Message
	// 工具调起/结果归属调起方（EXEC）：与同轮的 assistant 行共享 role_name 与
	// round_id，前端把过程归到主持这一轮的 agent 名下（§2.1 表：tool → 调起方）。
	toolOrigin := model.MessageOrigin{
		RoleName: RoleNameMain, RoleSessionID: sessionID,
		RoundID: service.components.tasks.RoleRoundFor(sessionID),
	}
	if active {
		tool := &model.ToolCall{ID: id, Name: name, Arguments: arguments, Status: dto.ToolEventRunning}
		message = *service.appendMessageWithOriginLocked("tool", "", tool, toolOrigin)
	} else {
		// 阶段 1：后台会话工具消息写自身 view（hot_attach 回看可见）。
		message = *service.appendSessionMessageWithOriginLocked(sessionID, "tool", "", &model.ToolCall{ID: id, Name: name, Arguments: arguments, Status: dto.ToolEventRunning}, toolOrigin)
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
	service.publishSessionEvent(EventToolStarted, revision, requestID, sessionID, message)
	// 工具边界后被动同步 task → worktable 增量，再发最新 runtime.changed。
	service.refreshWorkTableFromSources()
	service.publishSessionEvent(EventRuntimeChanged, service.Snapshot().Revision, requestID, sessionID, service.Snapshot().Runtime)
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
	sessionID := task_context.SessionIDFromContext(ctx)
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
	status, errorText := dto.ToolEventSuccess, ""
	if toolErr != nil {
		status, errorText = dto.ToolEventError, service.components.errorPresentation.PresentToolError(name, toolErr)
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
	content := visibleContent
	if toolErr != nil {
		content = errorText
	}

	var message model.Message
	var assistant *model.Message
	message = *service.appendSessionMessageWithOriginLocked(sessionID, "tool_result", content, &model.ToolCall{
		ID: id, Name: name, Result: visibleContent, Error: errorText,
		Status: status, Duration: duration,
		ResultRef: resultRef, Truncated: truncated, TotalChars: totalChars,
	}, model.MessageOrigin{
		RoleName: RoleNameMain, RoleSessionID: sessionID,
		RoundID: service.components.tasks.RoleRoundFor(sessionID),
	})
	// tool_result 之后补空 assistant 占位（活跃与后台同一语义）：后台会话若
	// 缺占位，后续 appendVisibleDelta(Background) 会把下一段 LLM 正文并入
	// 工具之前的旧 assistant 空消息，工具看起来“后插入”到会话末尾
	// （热切回该会话时可见顺序错误；持久化重建不受影响）。
	if appended := service.appendAssistantPlaceholderAfterToolLocked(sessionID); appended != nil {
		assistant = appended
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
	// 工具记录（唯一判据 = toolrecord.Failed + toolrecord.Reason，读数落全局台账）：
	// 放在**发布之后**是刻意的——这里已经出了 ViewMu（台账要写文件，绝不在视图锁下做 IO）。
	//
	// 原因归并吃的是**原始错误**（toolErr），不是给模型看的 errorText：后者是呈现层的
	// 包装文案（"该工具未能完成本次操作…"），它刻意不含内部细节，拿它归因只会全落到
	// "其它"。成功也记（开关打开时）：没有分母的失败计数会把优化带偏。
	service.recordToolOutcome(sessionID, name, applicationtoolrecord.Failed(status), applicationtoolrecord.ErrorText(toolErr))
	if assistant != nil {
		service.publishSessionEvent(EventMessageAdded, revision, requestID, sessionID, *assistant)
	}
	// 工具完成：todo/taskadd 已写注册表，plan/subagent 状态已更新——
	// 统一走被动同步 + 增量发布，再发最新 runtime.changed。
	service.refreshWorkTableFromSources()
	// 项目看板投影（M3）：挂在**既有**同步点之后，**不新造工具完成回调**——
	// 触发面判定在订阅口（同步器）里，这里只做交接（判空 + 转发本次调用）。
	//
	// 名字/错误/会话都取完成段**当场已有**的变量（该段是按会话展开的）：后台会话的
	// 完成落在它自己的会话上，不退化成"当前视图会话"。取一份快照再调（不在锁下调
	// 外部实现，见 projectBoardSink）：生产嵌入的同步器只标脏 + 唤醒，不做 IO。
	if sink := service.projectBoardSink(); sink != nil {
		sink.OnToolComplete(name, toolErr, sessionID)
	}
	service.publishSessionEvent(EventRuntimeChanged, service.Snapshot().Revision, requestID, sessionID, service.Snapshot().Runtime)
	emit("toolhook.complete.event.done")
}

type ToolHookBridge struct {
	bridge *applicationtoolhooks.Bridge
}

// NewToolHookBridge constructs the Seele-to-Application hook adapter.
func NewToolHookBridge() *ToolHookBridge {
	return &ToolHookBridge{bridge: applicationtoolhooks.New()}
}

// Bind installs the current Service as the hook host.
func (bridge *ToolHookBridge) Bind(service *Service) {
	bridge.bridge.Bind(applicationtoolhooks.Deps{
		RecordLLMComplete: service.components.tasks.RecordLLMComplete,
		RecordReActToolCall: func(ctx context.Context) {
			service.components.tasks.RecordReActToolCall(ctx)
		},
		HandleToolStart: func(ctx context.Context, name, id, arguments string) {
			service.handleToolStart(ctx, name, id, arguments)
		},
		HandleToolComplete: func(ctx context.Context, info session.ToolCallInfo, id string, observe func(string)) {
			service.handleToolCompleteObserved(
				ctx, info.Name, id, info.Arguments, info.Result, info.Error, info.Duration, observe,
			)
		},
		AllowNextReActIteration: service.components.tasks.AllowNextReActIteration,
		SessionBacked: func() bool {
			reentrant, ok := service.Deps.Engine.(interface{ SessionBacked() bool })
			return ok && reentrant.SessionBacked()
		},
		QueuedInputs: func() bool {
			service.ViewMu.RLock()
			queued := len(service.activeQueuedChatRequestsLocked()) > 0
			service.ViewMu.RUnlock()
			return queued
		},
		ActiveRequestID: func() string {
			service.ViewMu.RLock()
			requestID := service.Core.Snapshot.Chat.RequestID
			service.ViewMu.RUnlock()
			return requestID
		},
		PrepareHistory: func(ctx context.Context) error {
			return service.components.history.PrepareNewHistoryContentFor(task_context.SessionIDFromContext(ctx))
		},
		RecordContextFailure: service.components.context.RecordContextControlFailure,
	})
}

// SetDiagnosticObserver installs optional metadata-only hook diagnostics.
func (bridge *ToolHookBridge) SetDiagnosticObserver(observer ToolHookDiagnosticObserver) {
	bridge.bridge.SetDiagnosticObserver(applicationtoolhooks.DiagnosticObserver(observer))
}

// Hooks returns Seele loop hooks.
func (bridge *ToolHookBridge) Hooks() *session.LoopHooks {
	return bridge.bridge.Hooks()
}

// ToolHookDiagnosticEvent is metadata-only hook instrumentation.
type ToolHookDiagnosticEvent = applicationtoolhooks.DiagnosticEvent

// ToolHookDiagnosticObserver receives best-effort hook stages.
type ToolHookDiagnosticObserver = applicationtoolhooks.DiagnosticObserver
