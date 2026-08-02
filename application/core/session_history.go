package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// resumeSession replaces the active engine history and restores the session's
// workspace binding before publishing one coherent snapshot.
func (service *Service) resumeSession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}

	service.sessionTransitionMu.Lock()
	defer service.sessionTransitionMu.Unlock()

	service.mu.RLock()
	running := service.snapshot.Chat.Running
	service.mu.RUnlock()
	if running {
		return ErrChatRunning
	}

	location := service.components.sessions.locateSession(sessionID)
	// 会话恢复三读（record/history/transcript）相互独立，并行加载：
	// 大会话（数 MB）下全量解析总耗时从串行求和变为三路取最大值。
	// history 路径为尾部窗口读：先 (0,0) 取总数（只读 manifest，不解析 shard），
	// 再 (total-window, window) 只解析覆盖尾部窗口的 1-2 个 shard——
	// 8.4MB/6 shard 的会话从全量解析降为单 shard 解析。
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
		record, hasRecord, recordErr = service.components.sessions.loadSessionRecord(location, sessionID)
	}()
	go func() {
		defer loadGroup.Done()
		history, historyTotal, historyErr = service.components.sessions.loadHistoryTailWindow(location)
	}()
	go func() {
		defer loadGroup.Done()
		transcript, transcriptErr = service.components.sessions.loadSessionTranscript(location, sessionID)
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
		budget := contextBudgetFor(service.deps.Runtime)
		engineHistory = transcriptTailHistory(transcript, budget.TargetAfterCompaction, 4)
		if len(engineHistory) == 0 {
			engineHistory = recordResumeHistory(record)
		}
	}
	if err := service.deps.Engine.ReplaceHistory(sessionID, engineHistory); err != nil {
		return fmt.Errorf("replace engine history: %w", err)
	}
	service.deps.Engine.SetSystemPrompt(service.promptStack.Render())

	total := historyTotal // 尾部窗口读返回的真实总数（无 record 的旧格式会话）
	if hasRecord {
		total = len(record.Conversation.Messages)
	}
	offset := total - Limits().HistoryWindow
	if offset < 0 {
		offset = 0
	}
	visibleHistory := history
	currentWorkspace := location.workspace
	if service.deps.Workspace != nil {
		if currentWorkspace != nil {
			if err := service.deps.Runtime.BindProjectRoot(currentWorkspace.RootPath); err != nil {
				return fmt.Errorf("bind project root: %w", err)
			}
			service.deps.Sessions.SetWorkspace(currentWorkspace.ID)
			service.deps.Workspace.BindSession(sessionID, currentWorkspace.ID)
		} else {
			service.deps.Runtime.UnbindProjectRoot()
			service.deps.Sessions.SetWorkspace("")
			service.deps.Workspace.UnbindSession(sessionID)
		}
	}

	activePlan := activePlanFrame(record.PlanStack, record.ActivePlanID)
	var planRestoreErr error
	if hasRecord && activePlan != nil && activePlan.Arguments != "" {
		if restorer, ok := service.deps.Runtime.(persistedPlanRestorer); ok {
			planRestoreErr = restorer.RestorePlan(context.Background(), activePlan.Arguments)
		}
	}

	service.mu.Lock()
	name := sessionTitleFromHistory(history)
	if hasRecord && record.Title.Value != "" {
		name = record.Title.Value
	}
	service.snapshot.Session = SessionState{ID: sessionID, Name: name}
	service.sessionTitle = SessionTitle{Value: name, Source: "legacy_history"}
	if hasRecord {
		service.sessionTitle = record.Title
		service.planStack = cloneSessionPlanStack(record.PlanStack)
		service.activePlanID = record.ActivePlanID
		service.planSequence = uint64(len(service.planStack))
		service.transcript = append([]TranscriptEvent(nil), transcript...)
		if len(transcript) > 0 {
			service.transcriptSeq = transcript[len(transcript)-1].Seq
		}
		if record.Projection != nil && record.Projection.Checkpoint.CoversEventRange.End > service.transcriptSeq {
			service.transcriptSeq = record.Projection.Checkpoint.CoversEventRange.End
		}
		service.taskCheckpoints = append([]TaskCheckpoint(nil), record.Checkpoints...)
		service.toolResultRefs = append([]ToolResultRef(nil), record.ToolResults...)
		service.pendingProviderCalls = nil
		service.pendingToolResults = nil
		service.resultRefsByToolCallID = make(map[string]string)
		service.components.tasks.restoreTaskProjectionLocked(record.Projection, latestUserContent(record.Conversation.Messages))
	} else {
		service.planStack = nil
		service.activePlanID = ""
		service.planSequence = 0
		service.transcript = nil
		service.transcriptSeq = 0
		service.taskExecution = nil
		service.taskService = nil
		service.taskCheckpoints = nil
		service.toolResultRefs = nil
		service.pendingToolResults = nil
		service.pendingProviderCalls = nil
		service.resultRefsByToolCallID = make(map[string]string)
	}
	service.snapshot.Conversation = nil
	service.snapshot.Runtime.Plan = nil
	service.snapshot.ReadFiles = nil
	service.snapshot.Task = nil
	service.snapshot.Interaction = nil
	service.appendMessageLocked("system", "已恢复会话: "+sessionID, nil)
	if hasRecord {
		service.snapshot.Conversation = append(service.snapshot.Conversation, recordConversation(record)...)
		service.snapshot.Runtime.Plan = activePlanFromStack(record.PlanStack, record.ActivePlanID)
		service.snapshot.ReadFiles = append([]ReadFileRef(nil), record.Execution.ReadFiles...)
		if record.Execution.Task != nil {
			task := *record.Execution.Task
			task.ContextCompactions = append([]ContextCompaction(nil), record.Execution.Task.ContextCompactions...)
			service.snapshot.Task = &task
		}
		if planRestoreErr != nil {
			service.appendMessageLocked("system", "The stored Plan is visible for review but could not be reloaded for execution with the current settings.", nil)
		}
	} else {
		service.appendHistoryLocked(visibleHistory)
	}
	service.deps.Engine.SetSystemPrompt(service.components.prompts.systemPromptForActiveTaskLocked())
	service.snapshot.HistoryOffset = offset
	service.snapshot.TotalMessages = total
	service.snapshot.HasMoreHistory = offset > 0
	if service.deps.Workspace != nil {
		service.snapshot.CurrentWorkspace = currentWorkspace
		service.refreshWorkspaceLocked()
	}
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
	return nil
}

// loadHistoryTailWindow 尾部窗口读：先探总数（limit=0 只读 manifest），
// 再读尾部 window 条（只解析覆盖窗口的 shard）。返回窗口消息与真实总数，
// resumeSession 的 visibleHistory/TotalMessages 直接消费。
func (service *sessionCoordinator) loadHistoryTailWindow(location sessionLocation) ([]EngineMessage, int, error) {
	window := Limits().HistoryWindow
	_, total, err := service.loadSessionHistoryRange(location.workspaceID, location.meta.ID, 0, 0)
	if err != nil {
		return nil, 0, err
	}
	offset := total - window
	if offset < 0 {
		offset = 0
	}
	history, _, err := service.loadSessionHistoryRange(location.workspaceID, location.meta.ID, offset, window)
	return history, total, err
}

func latestUserContent(messages []Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == "user" {
			return messages[index].Content
		}
	}
	return ""
}

// ResumeSession is the direct application boundary for GUI/TUI session
// selection. It deliberately bypasses command text parsing so a click has one
// synchronous outcome: a restored snapshot or a returned error.
func (service *Service) ResumeSession(sessionID string) error {
	return service.resumeSession(sessionID)
}

func activePlanFrame(stack []SessionPlanFrame, activeID string) *SessionPlanFrame {
	for index := range stack {
		if stack[index].ID == activeID {
			return &stack[index]
		}
	}
	return nil
}

func activePlanFromStack(stack []SessionPlanFrame, activeID string) *PlanState {
	frame := activePlanFrame(stack, activeID)
	if frame == nil {
		return nil
	}
	return cloneRuntimeState(RuntimeState{Plan: frame.Plan}).Plan
}

// LoadMoreHistory prepends an older history page to the visible conversation.
func (service *Service) LoadMoreHistory(limit int) error {
	if limit <= 0 {
		limit = Limits().HistoryWindow
	}

	service.mu.RLock()
	offset := service.snapshot.HistoryOffset
	sessionID := service.snapshot.Session.ID
	service.mu.RUnlock()
	if offset <= 0 {
		return nil
	}

	loadOffset := offset - limit
	if loadOffset < 0 {
		loadOffset = 0
	}
	loadLimit := offset - loadOffset

	workspaceID := ""
	service.mu.RLock()
	if service.snapshot.CurrentWorkspace != nil {
		workspaceID = service.snapshot.CurrentWorkspace.ID
	}
	service.mu.RUnlock()
	history, total, err := service.components.sessions.loadSessionHistoryRange(workspaceID, sessionID, loadOffset, loadLimit)
	if err != nil {
		return fmt.Errorf("load history range: %w", err)
	}

	adapted := make([]Message, 0, len(history))
	for _, msg := range history {
		if !isVisibleHistoryMessage(msg) {
			continue
		}
		adapted = append(adapted, adaptEngineMessage(msg))
	}

	service.mu.Lock()
	for index := range adapted {
		service.messageSeq++
		adapted[index].ID = fmt.Sprintf("message-%d", service.messageSeq)
	}
	service.snapshot.Conversation = append(adapted, service.snapshot.Conversation...)
	service.snapshot.HistoryOffset = loadOffset
	service.snapshot.TotalMessages = total
	service.snapshot.HasMoreHistory = loadOffset > 0
	revision := service.bumpLocked()
	service.mu.Unlock()
	service.events.Publish(EventSnapshotChanged, revision, "", nil)
	return nil
}

func adaptEngineMessage(msg EngineMessage) Message {
	content := msg.Content
	if msg.Role == "user" {
		content = displayUserInput(content)
	} else if msg.Role == "assistant" || msg.Role == "tool" {
		content = stripThoughtBlocks(content)
	}
	if isProviderOnlyHistoryContent(content) {
		content = ""
	}
	message := Message{Role: msg.Role, Content: content}
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
