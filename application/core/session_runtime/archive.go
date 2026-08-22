package session_runtime

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

const (
	SessionRecordVersion       = 3
	SessionArchiveVersion      = 1
	SessionArchiveResumePrefix = "<!-- seelex:session-resume:v1 -->"
)

// PersistCurrentSession 把当前会话原子落盘：task 快照锁外收集（外部端口），
// 锁内构建 record + 拷贝事件，锁外合并/写入，最后锁内清理已提交 tool
// 结果引用。
func (c *Coordinator) PersistCurrentSession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	// task 快照随会话落盘：外部端口（actor/CSP）在锁外调用，避免持锁阻塞。
	tasks := c.Core.Deps.Runtime.TaskSnapshot()
	c.Core.Mu.Lock()
	record := c.sessionRecordLocked(sessionID, tasks)
	events := append([]model.TranscriptEvent(nil), c.tasks.Transcript()...)
	pendingResults := append([]model.StoredToolResult(nil), c.tasks.PendingToolResults()...)
	c.Core.Mu.Unlock()
	enrichTranscriptMessageIDs(events, record)

	if store, ok := c.Core.Deps.Sessions.(SessionRecordPort); ok {
		existing, err := store.LoadSessionRecord(sessionID)
		if err == nil {
			record.Conversation.Messages = c.mergeConversationMessages(existing.Conversation.Messages, record.Conversation.Messages)
		} else if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("load existing session record before merge: %w", err)
		}
	}

	if store, ok := c.Core.Deps.Sessions.(SessionSnapshotPort); ok {
		if err := store.SaveSessionSnapshot(sessionID, c.Core.Deps.Engine.History(), record, events, pendingResults); err != nil {
			return fmt.Errorf("save atomic session snapshot: %w", err)
		}
		c.Core.Mu.Lock()
		c.tasks.RemoveCommittedToolResultsLocked(pendingResults)
		c.Core.Mu.Unlock()
		return nil
	}

	if err := c.Core.Deps.Sessions.SaveCurrent(sessionID); err != nil {
		return err
	}
	store, ok := c.Core.Deps.Sessions.(SessionRecordPort)
	if !ok {
		return nil
	}
	if err := store.SaveSessionRecord(sessionID, record); err != nil {
		return fmt.Errorf("save session record: %w", err)
	}
	return nil
}

// enrichTranscriptMessageIDs 建立 event-to-message 关联（模块化方案 §3.2）：
// tool call/result 按 CallID 配对，user/assistant 按角色+内容配对；
// 不按数组位置临时推导。无法稳定配对的 event 保持 MessageID 为空，
// 由事件 shard 的 Seq 继续承担顺序语义。
func enrichTranscriptMessageIDs(events []model.TranscriptEvent, record model.SessionRecord) {
	if len(events) == 0 || len(record.Conversation.Messages) == 0 {
		return
	}
	// tool 角色消息承载 call 宣告，tool_result 消息承载结果；两类消息对同一
	// CallID 分别索引，避免互相串配。
	byCallMessage := make(map[string]string, len(record.Conversation.Messages))
	byResultMessage := make(map[string]string)
	byUserContent := make(map[string]string)
	byAssistantContent := make(map[string]string)
	for _, message := range record.Conversation.Messages {
		if message.Tool != nil && message.Tool.ID != "" {
			switch message.Role {
			case "tool":
				byCallMessage[message.Tool.ID] = message.ID
			case "tool_result":
				byResultMessage[message.Tool.ID] = message.ID
			}
		}
		switch message.Role {
		case "user":
			if message.Content != "" {
				byUserContent[message.Content] = message.ID
			}
		case "assistant":
			if message.Content != "" {
				byAssistantContent[message.Content] = message.ID
			}
		}
	}
	for index := range events {
		if events[index].MessageID != "" {
			continue
		}
		if events[index].Role == "tool" && events[index].ToolCallID != "" {
			events[index].MessageID = byResultMessage[events[index].ToolCallID]
			continue
		}
		if len(events[index].ToolCalls) == 1 {
			events[index].MessageID = byCallMessage[events[index].ToolCalls[0].ID]
			if events[index].MessageID != "" {
				continue
			}
		}
		switch events[index].Role {
		case "user":
			events[index].MessageID = byUserContent[events[index].Content]
		case "assistant":
			events[index].MessageID = byAssistantContent[events[index].Content]
		}
	}
}

func (c *Coordinator) mergeConversationMessages(existing, projected []model.Message) []model.Message {
	if len(existing) == 0 {
		return c.RecordConversation(model.SessionRecord{Conversation: model.ConversationRecord{Messages: projected}})
	}
	merged := c.RecordConversation(model.SessionRecord{Conversation: model.ConversationRecord{Messages: existing}})
	indices := make(map[string]int, len(merged))
	for index := range merged {
		if merged[index].ID != "" {
			indices[merged[index].ID] = index
		}
	}
	for _, message := range projected {
		copy := c.RecordConversation(model.SessionRecord{Conversation: model.ConversationRecord{Messages: []model.Message{message}}})[0]
		if copy.ID != "" {
			if index, ok := indices[copy.ID]; ok {
				merged[index] = copy
				continue
			}
			indices[copy.ID] = len(merged)
		}
		merged = append(merged, copy)
	}
	return merged
}

func (c *Coordinator) sessionRecordLocked(sessionID string, tasks []dto.TaskRecord) model.SessionRecord {
	now := time.Now()
	c.tasks.SyncActivePlanFrameLocked(now)
	title := c.sessionTitle
	if title.Value == "" {
		title = model.SessionTitle{Value: c.Core.Snapshot.Session.Name, Source: "first_request", FinalizedAt: now}
	}
	record := model.SessionRecord{
		Version: SessionRecordVersion, ID: sessionID, Title: title,
		ActivePlanID: c.tasks.ActivePlanID(),
		PlanStack:    CloneSessionPlanStack(c.tasks.PlanStack()),
		// task 注册表快照随会话落盘（复用 stack 存储通道；锁外收集）。
		Tasks:        append([]dto.TaskRecord(nil), tasks...),
		Conversation: model.ConversationRecord{UpdatedAt: now},
		Execution:    model.SessionExecutionRecord{ReadFiles: append([]model.ReadFileRef(nil), c.Core.Snapshot.ReadFiles...)},
		Projection:   c.tasks.TaskProjectionLocked(sessionID),
		Checkpoints:  append([]model.TaskCheckpoint(nil), c.tasks.TaskCheckpoints()...),
		ToolResults:  append([]model.ToolResultRef(nil), c.tasks.ToolResultRefs()...),
		UpdatedAt:    now,
	}
	for _, message := range c.Core.Snapshot.Conversation {
		if message.Role == "system" {
			continue
		}
		copy := c.archivedConversationMessageLocked(message)
		record.Conversation.Messages = append(record.Conversation.Messages, copy)
	}
	if task := c.Core.Snapshot.Task; task != nil {
		copy := *task
		copy.ContextCompactions = append([]model.ContextCompaction(nil), task.ContextCompactions...)
		record.Execution.Task = &copy
	}
	if continuation := c.tasks.ContinuationSummary(c.Core.Snapshot.Chat.RequestID); continuation != "" {
		record.Execution.Continuation = continuation
	}
	return record
}

// SessionRecordLocked 构建当前会话的归档 record（调用方持有 Core.Mu；
// 测试/恢复路径直接构造用）。
func (c *Coordinator) SessionRecordLocked(sessionID string, tasks []dto.TaskRecord) model.SessionRecord {
	return c.sessionRecordLocked(sessionID, tasks)
}

func (c *Coordinator) archivedConversationMessageLocked(message model.Message) model.Message {
	copy := message
	if message.Tool != nil {
		tool := *message.Tool
		copy.Tool = &tool
		if resultRef := c.tasks.ToolResultRefByCallID(tool.ID); resultRef != "" {
			warning := c.oversizedWarning(tool.Name, resultRef)
			copy.Tool.Result = warning
			if copy.Role == "tool_result" {
				copy.Content = warning
			}
		}
	}
	if copy.Role == "user" {
		if resultRef := c.userInputResultRefLocked(copy.Content); resultRef != "" {
			copy.Content = c.contentWarning(resultRef)
		}
	}
	return copy
}

func (c *Coordinator) userInputResultRefLocked(content string) string {
	digest := "sha256:" + fmt.Sprintf("%x", sha256.Sum256([]byte("user_input\x00"+content)))
	for _, result := range c.tasks.ToolResultRefs() {
		if result.Tool == "user_input" && result.Digest == digest {
			return result.Ref
		}
	}
	return ""
}

// LoadSessionRecord 读取会话归档 record（可选能力：无 record 端口或版本/
// ID 不匹配时返回 (零值, false, nil)）。
func (c *Coordinator) LoadSessionRecord(location Location, sessionID string) (model.SessionRecord, bool, error) {
	store, ok := c.Core.Deps.Sessions.(SessionRecordPort)
	if !ok {
		return model.SessionRecord{}, false, nil
	}
	record, err := store.LoadSessionRecordWorkspace(location.WorkspaceID, sessionID)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, sql.ErrNoRows) {
		return model.SessionRecord{}, false, nil
	}
	if err != nil {
		return model.SessionRecord{}, false, err
	}
	if record.Version == 2 && record.ID == sessionID {
		record.Version = SessionRecordVersion
		return record, true, nil
	}
	if record.Version != SessionRecordVersion || record.ID != sessionID {
		return model.SessionRecord{}, false, nil
	}
	return record, true, nil
}

// LoadSessionTranscript 读取会话 transcript 尾部窗口（预算 + 单元上限由
// 装配端口提供），过滤内部标记事件。
func (c *Coordinator) LoadSessionTranscript(location Location, sessionID string) ([]model.TranscriptEvent, error) {
	store, ok := c.Core.Deps.Sessions.(SessionTranscriptPort)
	if !ok {
		return []model.TranscriptEvent{}, nil
	}
	budget := c.transcriptTailBudget(c.Core.Deps.Runtime)
	events, err := store.LoadTranscriptTailWorkspace(location.WorkspaceID, sessionID, budget, 4)
	if err != nil {
		return nil, err
	}
	filtered := make([]model.TranscriptEvent, 0, len(events))
	for _, event := range events {
		if event.Role == "system" || c.isInternalContent(event.Content) {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered, nil
}

func recordResumeHistory(record model.SessionRecord) []contract.EngineMessage {
	continuation := strings.TrimSpace(record.Execution.Continuation)
	if continuation == "" {
		continuation = "A prior session is available in the user-visible transcript. Treat its details as historical context and ask targeted questions or reread files when more detail is needed."
	}
	return []contract.EngineMessage{{
		Role: "user", ContentSet: true,
		Content: SessionArchiveResumePrefix + "\n" + continuation,
	}}
}

// RecordResumeHistory 是 durable-record 冷加载兜底（仅当 transcript 与可见
// 历史均不可用时，注入 resume 前缀 + 恢复摘要）。
func RecordResumeHistory(record model.SessionRecord) []contract.EngineMessage {
	return recordResumeHistory(record)
}

// RecordConversation 返回去除内部消息后的可见会话消息（深拷贝 tool 引用）。
func (c *Coordinator) RecordConversation(record model.SessionRecord) []model.Message {
	conversation := make([]model.Message, 0, len(record.Conversation.Messages))
	for _, message := range record.Conversation.Messages {
		if isInternalConversationMessage(message, c.isInternalContent) {
			continue
		}
		copy := message
		if message.Tool != nil {
			tool := *message.Tool
			copy.Tool = &tool
		}
		conversation = append(conversation, copy)
	}
	return conversation
}

func isInternalConversationMessage(message model.Message, isInternalContent func(string) bool) bool {
	if message.Role == "system" {
		return true
	}
	if message.Role != "user" {
		return false
	}
	return isInternalContent(message.Content) ||
		strings.HasPrefix(message.Content, SessionArchiveResumePrefix)
}

// RecordConversationResumeHistory 是 durable-record 冷加载回退历史（transcript
// 尾部缺失/过期/仅内部标记时使用），与 append-only transcript 路径使用同一
// 有界协议单元选择器。
func (c *Coordinator) RecordConversationResumeHistory(record model.SessionRecord, tokenBudget, maxUnits int) []contract.EngineMessage {
	return c.tailHistory(c.RecordConversationTranscript(record), tokenBudget, maxUnits)
}

func (c *Coordinator) RecordConversationTranscript(record model.SessionRecord) []model.TranscriptEvent {
	events := make([]model.TranscriptEvent, 0, len(record.Conversation.Messages))
	for _, message := range record.Conversation.Messages {
		if isInternalConversationMessage(message, c.isInternalContent) {
			continue
		}
		event := model.TranscriptEvent{
			Seq:        uint64(len(events) + 1),
			Role:       message.Role,
			Content:    message.Content,
			MessageID:  message.ID,
			TokenCount: seelexctx.EstimateTokens(message.Content),
		}
		switch message.Role {
		case "tool":
			if message.Tool != nil && message.Tool.ID != "" {
				event.Role = "assistant"
				event.ToolCalls = []model.TranscriptToolCall{{ID: message.Tool.ID, Name: message.Tool.Name, Arguments: message.Tool.Arguments}}
			}
		case "tool_result":
			event.Role = "tool"
			if message.Tool != nil {
				event.ToolCallID = message.Tool.ID
				event.Name = message.Tool.Name
			}
		}
		for _, call := range event.ToolCalls {
			event.TokenCount += seelexctx.EstimateTokens(call.Name) + seelexctx.EstimateTokens(call.Arguments)
		}
		events = append(events, event)
	}
	return events
}

// RecordConversationTail 返回可见会话的尾部窗口消息（含 window 上限）。
func (c *Coordinator) RecordConversationTail(record model.SessionRecord, window int) []model.Message {
	messages := c.RecordConversation(record)
	if window > 0 && len(messages) > window {
		messages = messages[len(messages)-window:]
	}
	return c.RecordConversation(model.SessionRecord{Conversation: model.ConversationRecord{Messages: messages}})
}

// CloneSessionPlanStack 深拷贝会话 plan 栈（frame 内 Plan 单独克隆）。
func CloneSessionPlanStack(stack []model.SessionPlanFrame) []model.SessionPlanFrame {
	cloned := make([]model.SessionPlanFrame, len(stack))
	for index := range stack {
		cloned[index] = stack[index]
		cloned[index].Plan = model.CloneRuntimeState(model.RuntimeState{Plan: stack[index].Plan}).Plan
	}
	return cloned
}

// RecordReadFileLocked 记录一次 read 工具的文件引用（会话归档 ReadFiles）。
func (c *Coordinator) RecordReadFileLocked(arguments string) {
	var input struct {
		Path string `json:"path"`
	}
	if json.Unmarshal([]byte(arguments), &input) != nil {
		return
	}
	input.Path = strings.TrimSpace(input.Path)
	if input.Path == "" {
		return
	}
	now := time.Now()
	for index := range c.Core.Snapshot.ReadFiles {
		if c.Core.Snapshot.ReadFiles[index].Path == input.Path {
			c.Core.Snapshot.ReadFiles[index].ReadAt = now
			return
		}
	}
	c.Core.Snapshot.ReadFiles = append(c.Core.Snapshot.ReadFiles, model.ReadFileRef{Path: input.Path, ReadAt: now})
}
