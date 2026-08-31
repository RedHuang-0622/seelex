package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/chat"
	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/core/view_state"
)

// persistedPlanRestorer 是 Runtime 的可选能力：resume 时按 plan 参数恢复
// 可执行 Plan（不可用时保留可见投影）。
type persistedPlanRestorer interface {
	RestorePlan(context.Context, string) error
}

// resumeSession 替换活跃引擎历史并恢复会话的 workspace 绑定，然后发布一份
// 一致的快照。
func (service *Service) resumeSession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}

	transition := service.components.sessions.TransitionLock()
	transition.Lock()
	defer transition.Unlock()

	service.Mu.RLock()
	hot := service.sessionLoaded(sessionID)
	service.Mu.RUnlock()
	if hot {
		// 阶段 2：目标会话已驻留（含运行中）→ 热加载，只换视图指针 +
		// 投影会话 scope，不重建历史、不触碰 X/M/R（不变量 Ⅱ）。
		return service.hotAttachSession(sessionID)
	}

	location := service.components.sessions.LocateSession(sessionID)
	// 会话恢复三读（record/history/transcript）相互独立，并行加载：
	// 大会话（数 MB）下全量解析总耗时从串行求和变为三路取最大值。
	// history 路径为尾部窗口读：先 (0,0) 取总数（只读 manifest，不解析 shard），
	// 再 (total-window, window) 只解析覆盖尾部窗口的 1-2 个 shard。
	var (
		record        SessionRecord
		hasRecord     bool
		history       []EngineMessage
		historyTotal  int
		historyErr    error
		transcript    []TranscriptEvent
		transcriptErr error
		recordErr     error
	)
	var loadGroup sync.WaitGroup
	loadGroup.Add(3)
	go func() {
		defer loadGroup.Done()
		record, hasRecord, recordErr = service.components.sessions.LoadSessionRecord(location, sessionID)
	}()
	go func() {
		defer loadGroup.Done()
		history, historyTotal, historyErr = service.components.sessions.LoadHistoryTailWindow(location)
	}()
	go func() {
		defer loadGroup.Done()
		transcript, transcriptErr = service.components.sessions.LoadSessionTranscript(location, sessionID)
	}()
	loadGroup.Wait()
	if recordErr != nil {
		return fmt.Errorf("load session record %q: %w", sessionID, recordErr)
	}
	if historyErr != nil && !hasRecord {
		return fmt.Errorf("load session %q: %w", sessionID, historyErr)
	}
	if historyErr != nil {
		// A v2 SessionRecord is authoritative for the visible transcript and
		// recovery checkpoint. Framework history is only a provider cache, so a
		// lost legacy cache must not make the saved session impossible to open.
		history = nil
	}
	if transcriptErr != nil && !hasRecord {
		return fmt.Errorf("load session transcript %q: %w", sessionID, transcriptErr)
	}
	engineHistory := history
	if hasRecord {
		budget := task_context.ContextBudgetFor(service.Deps.Runtime)
		latestUser := service.components.sessions.LatestUserContent(record.Conversation.Messages)
		if len(transcript) == 0 || (latestUser != "" && !service.components.sessions.TranscriptContainsUser(transcript, latestUser)) {
			// The durable Conversation is the source of truth. Rehydrate it into
			// the application transcript when the append-only tail is empty or
			// stale, otherwise the next prepareExecutionContext call would drop
			// the fallback history again.
			transcript = service.components.sessions.RecordConversationTranscript(record)
		}
		engineHistory = task_context.TranscriptTailHistory(transcript, budget.TargetAfterCompaction, 4)
		recordHistory := service.components.sessions.RecordConversationResumeHistory(record, budget.TargetAfterCompaction, 4)
		if len(engineHistory) == 0 || (latestUser != "" && !service.components.sessions.HistoryContainsUser(engineHistory, latestUser)) {
			engineHistory = recordHistory
		}
		if len(engineHistory) == 0 {
			engineHistory = session_runtime.RecordResumeHistory(record)
		}
	}
	// framework DurableHistory 按会话 workspace 显式键落盘（R3 键漂移收敛）：
	// 必须在引擎创建/恢复前登记绑定，后台会话 ChatStream 结束时不串写他域。
	service.Deps.Runtime.SetSessionWorkspace(sessionID, location.WorkspaceID)
	if enginePort, ok := service.Deps.Engine.(interface {
		ResumeSession(string, []EngineMessage) error
	}); ok {
		// M1：恢复目标会话自己的引擎实例（会话注册表内存缓存，切走不再销毁）。
		if err := enginePort.ResumeSession(sessionID, engineHistory); err != nil {
			return fmt.Errorf("resume engine session: %w", err)
		}
	} else if err := service.Deps.Engine.ReplaceHistory(sessionID, engineHistory); err != nil {
		return fmt.Errorf("replace engine history: %w", err)
	}
	// 会话级 system prompt：切换路径必须按目标会话路由，禁止触碰全局活跃
	// 引擎（运行中会话的 Session 锁可能被 ChatStream 全程持有，误触会阻塞
	// 到该会话 LLM 返回 —— 用户视角的死锁/长时间无响应）。
	if promptPort, ok := service.Deps.Engine.(interface {
		SetSystemPromptFor(string, string)
	}); ok {
		promptPort.SetSystemPromptFor(sessionID, service.promptStack.Render())
	} else {
		service.Deps.Engine.SetSystemPrompt(service.promptStack.Render())
	}

	total := historyTotal // 尾部窗口读返回的真实总数（无 record 的旧格式会话）
	if hasRecord {
		total = len(service.components.sessions.RecordConversation(record))
	}
	offset := total - Limits().HistoryWindow
	if offset < 0 {
		offset = 0
	}
	// 标题说明（审查 #5）：hasRecord 时标题取 record.Title（权威）；无 record
	// 的旧格式会话标题由 SessionTitleFromHistory 取窗口内首条 user 消息。
	visibleHistory := history
	currentWorkspace := location.Workspace
	if service.Deps.Workspace != nil {
		if currentWorkspace != nil {
			if err := service.Deps.Runtime.BindProjectRoot(currentWorkspace.RootPath); err != nil {
				return fmt.Errorf("bind project root: %w", err)
			}
			service.Deps.Sessions.SetWorkspace(currentWorkspace.ID)
			service.Deps.Workspace.BindSession(sessionID, currentWorkspace.ID)
		} else {
			service.Deps.Runtime.UnbindProjectRoot()
			service.Deps.Sessions.SetWorkspace("")
			service.Deps.Workspace.UnbindSession(sessionID)
		}
	}

	activePlan := task_context.ActivePlanFrame(record.PlanStack, record.ActivePlanID)
	var planRestoreErr error
	if hasRecord && activePlan != nil && activePlan.Arguments != "" {
		if restorer, ok := service.Deps.Runtime.(persistedPlanRestorer); ok {
			planRestoreErr = restorer.RestorePlan(context.Background(), activePlan.Arguments)
		}
	}
	// 会话级 task 隔离：切换会话时整体替换注册表（清空旧会话、恢复目标
	// 会话 task）并清空子代理树，避免旧数据污染新会话工作台。
	service.Deps.Runtime.SwitchSessionTasks(sessionID, record.Tasks)
	_ = service.Deps.Runtime.ClearSubagentTree()
	// 恢复锚点：从主会话事件库/子会话记录重建目标会话的 fork 树与认领
	// （Assignee → subagent:<节点会话ID>；重启/切页后不再停留 main）。
	_ = service.Deps.Runtime.RestoreSubagentAnchors(sessionID)
	workspaceProjection := service.collectWorkspaceProjection()

	service.Mu.Lock()
	name := session_runtime.SessionTitleFromHistory(history, displayUserInput)
	if hasRecord && record.Title.Value != "" {
		name = record.Title.Value
	}
	service.Core.Snapshot.Session = SessionState{ID: sessionID, Name: name}
	resumedRuntime := service.sessionChatLocked(sessionID)
	resumedRuntime.cancel = nil
	service.inputQueue = resumedRuntime.inputQueue
	service.components.sessions.SetSessionTitleLocked(sessionID, SessionTitle{Value: name, Source: "legacy_history"})
	if hasRecord {
		service.components.sessions.SetSessionTitleLocked(sessionID, record.Title)
		transcriptSeq := uint64(0)
		if len(transcript) > 0 {
			transcriptSeq = transcript[len(transcript)-1].Seq
		}
		if record.Projection != nil && record.Projection.Checkpoint.CoversEventRange.End > transcriptSeq {
			transcriptSeq = record.Projection.Checkpoint.CoversEventRange.End
		}
		service.components.tasks.RestoreSessionTaskLocked(task_context.RestoredTaskState{
			PlanStack:         session_runtime.CloneSessionPlanStack(record.PlanStack),
			ActivePlanID:      record.ActivePlanID,
			Transcript:        transcript,
			TranscriptSeq:     transcriptSeq,
			Checkpoints:       record.Checkpoints,
			ToolResults:       record.ToolResults,
			Projection:        record.Projection,
			FallbackObjective: service.components.sessions.LatestUserContent(record.Conversation.Messages),
		})
	} else {
		service.components.tasks.ResetForNewSessionLocked()
	}
	// 阶段 1：冷加载重建写会话 view（Snapshot 是活跃会话的只读镜像）。
	view := &state.SessionView{
		TotalMessages:      total,
		HistoryOffset:      offset,
		HasMoreHistory:     offset > 0,
		ConversationWindow: Limits().HistoryWindow,
	}
	if hasRecord {
		service.advanceMessageSeqLocked(record.Conversation.Messages)
		view.Conversation = append(view.Conversation, Message{Role: "system", Content: "已恢复会话: " + sessionID, CreatedAt: time.Now()})
		view.Conversation = append(view.Conversation, service.components.sessions.RecordConversationTail(record, Limits().HistoryWindow)...)
		view.ReadFiles = append([]ReadFileRef(nil), record.Execution.ReadFiles...)
		service.components.view.SetSessionViewLocked(sessionID, view)
		if record.Execution.Task != nil {
			task := *record.Execution.Task
			task.ContextCompactions = append([]ContextCompaction(nil), record.Execution.Task.ContextCompactions...)
			service.Core.Snapshot.Task = &task
		} else {
			service.Core.Snapshot.Task = nil
		}
		if planRestoreErr != nil {
			service.appendSessionMessageLocked(sessionID, "system", "The stored Plan is visible for review but could not be reloaded for execution with the current settings.", nil)
		}
	} else {
		service.appendHistoryLockedFor(sessionID, visibleHistory)
	}
	service.setSessionChatLockedFor(sessionID, resumedRuntime.chat)
	service.Core.Snapshot.Runtime.Plan = task_context.ActivePlanFromStack(record.PlanStack, record.ActivePlanID)
	service.Core.Snapshot.Interaction = nil
	systemPrompt := service.components.prompts.SystemPromptForActiveTaskLocked()
	if service.Deps.Workspace != nil {
		service.Core.Snapshot.CurrentWorkspace = currentWorkspace
		service.applyWorkspaceProjectionLocked(workspaceProjection)
	}
	revision := service.bumpLocked()
	service.Mu.Unlock()
	service.Deps.Engine.SetSystemPrompt(systemPrompt)
	// context 模块挂接：resume 恢复后加载会话四栈到 Runtime（下一轮 prompt
	// 组装前就绪）。损坏的 context 显式失败，不静默降级成内存栈。
	if store, ok := service.Deps.Sessions.(session_runtime.SessionContextPort); ok {
		if err := store.AttachSessionContext(location.WorkspaceID, sessionID); err != nil {
			return fmt.Errorf("attach session context %q: %w", sessionID, err)
		}
	}
	service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
	service.publishRuntimeProjections()
	service.components.sessions.RequestCatalogRefresh()
	return nil
}

// ResumeSession 是 GUI/TUI 会话选择的直接应用边界。它刻意绕过命令文本解析，
// 让一次点击有同步结果：恢复的快照或返回的错误。
func (service *Service) ResumeSession(sessionID string) error {
	return service.resumeSession(sessionID)
}

// LoadMoreHistory 把更早的历史页前置到可见会话。
func (service *Service) LoadMoreHistory(limit int) error {
	if limit <= 0 {
		limit = Limits().HistoryWindow
	}

	service.Mu.RLock()
	offset := service.Core.Snapshot.HistoryOffset
	sessionID := service.Core.Snapshot.Session.ID
	service.Mu.RUnlock()
	if offset <= 0 {
		return nil
	}

	loadOffset := offset - limit
	if loadOffset < 0 {
		loadOffset = 0
	}
	loadLimit := offset - loadOffset

	workspaceID := ""
	service.Mu.RLock()
	if service.Core.Snapshot.CurrentWorkspace != nil {
		workspaceID = service.Core.Snapshot.CurrentWorkspace.ID
	}
	service.Mu.RUnlock()
	var adapted []Message
	total := 0
	if store, ok := service.Deps.Sessions.(session_runtime.SessionConversationRangePort); ok {
		messages, count, err := store.LoadConversationRangeWorkspace(workspaceID, sessionID, loadOffset, loadLimit)
		if err != nil {
			return fmt.Errorf("load conversation range: %w", err)
		}
		adapted = service.components.sessions.RecordConversation(SessionRecord{Conversation: ConversationRecord{Messages: messages}})
		total = count
	} else {
		history, count, err := service.components.sessions.LoadSessionHistoryRange(workspaceID, sessionID, loadOffset, loadLimit)
		if err != nil {
			return fmt.Errorf("load history range: %w", err)
		}
		total = count
		adapted = make([]Message, 0, len(history))
		for _, msg := range history {
			if !isVisibleHistoryMessage(msg) {
				continue
			}
			adapted = append(adapted, adaptEngineMessage(msg))
		}
	}

	service.Mu.Lock()
	for index := range adapted {
		if adapted[index].ID == "" {
			adapted[index].ID = fmt.Sprintf("message-%d", service.components.view.NextMessageSeqLocked())
		}
	}
	service.Core.Snapshot.Conversation = append(adapted, service.Core.Snapshot.Conversation...)
	service.Core.Snapshot.Conversation = view_state.BoundConversationHead(service.Core.Snapshot.Conversation, Limits().HistoryWindow)
	service.Core.Snapshot.HistoryOffset = loadOffset
	service.Core.Snapshot.TotalMessages = total
	service.Core.Snapshot.HasMoreHistory = loadOffset > 0
	service.Core.Snapshot.ConversationWindow = Limits().HistoryWindow
	revision := service.bumpLocked()
	service.Mu.Unlock()
	service.Events.Publish(EventSnapshotChanged, revision, "", nil)
	return nil
}

func adaptEngineMessage(msg EngineMessage) Message {
	content := msg.Content
	if msg.Role == "user" {
		content = displayUserInput(content)
	} else if msg.Role == "assistant" || msg.Role == "tool" {
		content = chat.StripThoughtBlocks(content)
	}
	if context_runtime.IsProviderOnlyHistoryContent(content) {
		content = ""
	}
	message := Message{Role: msg.Role, Content: content, ReasoningContent: msg.ReasoningContent}
	for _, toolCall := range msg.ToolCalls {
		message.Tool = &ToolCall{
			ID: toolCall.ID, Name: toolCall.Name, Arguments: toolCall.Arguments, Status: "success",
		}
	}
	return message
}

func isVisibleHistoryMessage(message EngineMessage) bool {
	if message.Role == "system" {
		return false
	}
	if message.Role != "user" {
		return true
	}
	return displayUserInput(message.Content) != ""
}
