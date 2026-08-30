package session_runtime

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
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

// PersistCurrentSession 把指定会话原子落盘（阶段 0：显式 location 键 +
// 全 For 会话读源，后台会话收尾不得读全局活跃槽）：
// task 快照锁外收集（外部端口），锁内构建 record + 拷贝事件，锁外写入，
// 最后锁内清理已提交 tool 结果引用。
func (c *Coordinator) PersistCurrentSession(location Location, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	// task 快照随会话落盘：外部端口（actor/CSP）在锁外调用，避免持锁阻塞；
	// 按会话分片取（后台会话不读活跃注册表，对应 R6/P2）。
	tasks := c.Core.Deps.Runtime.TaskSnapshotFor(sessionID)
	c.Core.Mu.Lock()
	record := c.sessionRecordLocked(sessionID, tasks)
	memoryEvents := append([]model.TranscriptEvent(nil), c.tasks.TranscriptFor(sessionID)...)
	pendingResults := append([]model.StoredToolResult(nil), c.tasks.PendingToolResultsFor(sessionID)...)
	c.Core.Mu.Unlock()
	// L1：resume 后内存 transcript 只含尾部窗口；可见对话与事件必须从
	// 磁盘事件全量重建（旧消息保留），否则 >4 轮会话落盘会截断 record。
	events := c.allTranscriptEventsForSession(location, sessionID, memoryEvents)
	if len(events) > 0 {
		// 事件非空才覆盖：transcript 为空（单会话测试桩/异常路径）时保留
		// sessionRecordLocked 的 Snapshot 回退，避免 record 变空。
		record.Conversation.Messages = c.conversationFromTranscriptLocked(events)
	}
	// L2：内存标题缺失（纯后台启动从未激活）时回退磁盘 record 标题，
	// 不读全局活跃 Snapshot 标题。
	if record.Title.Value == "" {
		if store, ok := c.Core.Deps.Sessions.(SessionRecordPort); ok {
			if existing, err := store.LoadSessionRecordWorkspace(location.WorkspaceID, sessionID); err == nil && existing.Title.Value != "" {
				record.Title = existing.Title
			}
		}
	}
	enrichTranscriptMessageIDs(events, record)

	if store, ok := c.Core.Deps.Sessions.(SessionSnapshotPort); ok {
		// 原子快照：transcript 是全量权威事件源，record 由其全量重建，
		// 整键替换即可，无需增量合并。
		if err := store.SaveSessionSnapshotWorkspace(location.WorkspaceID, sessionID, c.engineHistoryFor(sessionID), record, events, pendingResults); err != nil {
			return fmt.Errorf("save atomic session snapshot: %w", err)
		}
		c.Core.Mu.Lock()
		c.tasks.RemoveCommittedToolResultsForLocked(sessionID, pendingResults)
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
	// 非原子路径防御：transcript 为空（异常/旧数据）时保留磁盘既有可见
	// 对话，避免整键替换丢内容。
	if len(record.Conversation.Messages) == 0 {
		if existing, err := store.LoadSessionRecordWorkspace(location.WorkspaceID, sessionID); err == nil {
			record.Conversation.Messages = c.RecordConversation(model.SessionRecord{Conversation: existing.Conversation})
		}
	}
	if err := store.SaveSessionRecordWorkspace(location.WorkspaceID, sessionID, record); err != nil {
		return fmt.Errorf("save session record: %w", err)
	}
	return nil
}

// allTranscriptEventsForSession 返回会话全量事件：磁盘持久化事件（全量读）
// + 内存新增（按 Seq 去重合并）。恢复后内存 transcript 只有尾部窗口，
// 必须以此保证 record/事件通道整键替换不丢旧消息。
func (c *Coordinator) allTranscriptEventsForSession(location Location, sessionID string, memory []model.TranscriptEvent) []model.TranscriptEvent {
	store, ok := c.Core.Deps.Sessions.(SessionTranscriptPort)
	if !ok {
		return memory
	}
	persisted, err := store.LoadTranscriptTailWorkspace(location.WorkspaceID, sessionID, 1<<30, 1<<30)
	if err != nil {
		return memory
	}
	return mergeTranscriptEventsBySeq(persisted, memory)
}

func mergeTranscriptEventsBySeq(persisted, incoming []model.TranscriptEvent) []model.TranscriptEvent {
	bySeq := make(map[uint64]model.TranscriptEvent, len(persisted)+len(incoming))
	for _, event := range persisted {
		if event.Seq != 0 {
			bySeq[event.Seq] = event
		}
	}
	var tail []model.TranscriptEvent
	for _, event := range incoming {
		if event.Seq == 0 {
			tail = append(tail, event)
			continue
		}
		bySeq[event.Seq] = event
	}
	seqs := make([]uint64, 0, len(bySeq))
	for seq := range bySeq {
		seqs = append(seqs, seq)
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	merged := make([]model.TranscriptEvent, 0, len(bySeq)+len(tail))
	for _, seq := range seqs {
		merged = append(merged, bySeq[seq])
	}
	return append(merged, tail...)
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

func (c *Coordinator) sessionRecordLocked(sessionID string, tasks []dto.TaskRecord) model.SessionRecord {
	now := time.Now()
	c.tasks.SyncActivePlanFrameLockedFor(sessionID, now)
	title := c.sessionTitles[sessionID]
	requestID := c.tasks.CurrentRequestIDFor(sessionID)
	record := model.SessionRecord{
		Version: SessionRecordVersion, ID: sessionID, Title: title,
		ActivePlanID: c.tasks.ActivePlanIDFor(sessionID),
		PlanStack:    CloneSessionPlanStack(c.tasks.PlanStackFor(sessionID)),
		// task 注册表快照随会话落盘（复用 stack 存储通道；锁外收集）。
		Tasks:        append([]dto.TaskRecord(nil), tasks...),
		Conversation: model.ConversationRecord{UpdatedAt: now},
		// ReadFiles 仍读全局 Snapshot（阶段 1 SessionScope 收口前遗留，
		// 对应 P2 剩余面；阶段 0 目标是消除 record 级对话/plan/title 串写）。
		Execution:   model.SessionExecutionRecord{ReadFiles: append([]model.ReadFileRef(nil), c.Core.Snapshot.ReadFiles...)},
		Projection:  c.tasks.TaskProjectionLocked(sessionID),
		Checkpoints: append([]model.TaskCheckpoint(nil), c.tasks.TaskCheckpointsFor(sessionID)...),
		ToolResults: append([]model.ToolResultRef(nil), c.tasks.ToolResultRefsFor(sessionID)...),
		UpdatedAt:   now,
	}
	// 可见对话由本会话 transcript 全量重建（后台会话没有独立可见缓冲，
	// transcript 是会话级权威事件源；不再读全局 Snapshot.Conversation）。
	record.Conversation.Messages = c.conversationFromTranscriptLocked(c.tasks.TranscriptFor(sessionID))
	if len(record.Conversation.Messages) == 0 {
		// transcript 为空（异常/单会话测试桩路径）时回退全局可见对话，
		// 保留超限替换语义；阶段 1 SessionScope 收口后删除该回退。
		for _, message := range c.Core.Snapshot.Conversation {
			if message.Role == "system" {
				continue
			}
			record.Conversation.Messages = append(record.Conversation.Messages, c.archivedConversationMessageLocked(sessionID, message))
		}
	}
	// Execution.Task 按会话取（后台会话收尾不得读全局 Snapshot.Task，
	// 对应 L5/Execution.Task 串写修复）。
	if task := c.tasks.TaskStateFor(sessionID); task != nil {
		copy := *task
		copy.ContextCompactions = append([]model.ContextCompaction(nil), task.ContextCompactions...)
		record.Execution.Task = &copy
	}
	if continuation := c.tasks.ContinuationSummaryFor(sessionID, requestID); continuation != "" {
		record.Execution.Continuation = continuation
	}
	return record
}

// archivedConversationMessageLocked 归档单条可见消息：超限工具结果/用户
// 输入替换为引用警告（transcript 为空回退路径用；调用方持有 Core.Mu）。
func (c *Coordinator) archivedConversationMessageLocked(sessionID string, message model.Message) model.Message {
	copy := message
	if message.Tool != nil {
		tool := *message.Tool
		copy.Tool = &tool
		if resultRef := c.tasks.ToolResultRefByCallIDFor(sessionID, tool.ID); resultRef != "" {
			warning := c.oversizedWarning(tool.Name, resultRef)
			copy.Tool.Result = warning
			if copy.Role == "tool_result" {
				copy.Content = warning
			}
		}
	}
	if copy.Role == "user" {
		if resultRef := c.userInputResultRefLocked(sessionID, copy.Content); resultRef != "" {
			copy.Content = c.contentWarning(resultRef)
		}
	}
	return copy
}

func (c *Coordinator) userInputResultRefLocked(sessionID, content string) string {
	digest := "sha256:" + fmt.Sprintf("%x", sha256.Sum256([]byte("user_input\x00"+content)))
	for _, result := range c.tasks.ToolResultRefsFor(sessionID) {
		if result.Tool == "user_input" && result.Digest == digest {
			return result.Ref
		}
	}
	return ""
}

// conversationFromTranscriptLocked 从指定会话 transcript 事件重建可见对话
// 消息（阶段 0：后台会话无独立可见缓冲，transcript 是权威事件源；不读
// 全局 Snapshot.Conversation）。消息 ID 由事件 Seq 派生（message-%d），
// 同一会话内唯一，resume 后新消息由 advanceMessageSeq 继续递增不冲突。
func (c *Coordinator) conversationFromTranscriptLocked(events []model.TranscriptEvent) []model.Message {
	messages := make([]model.Message, 0, len(events))
	for _, event := range events {
		if event.Role == "system" || c.isInternalContent(event.Content) {
			continue
		}
		message := model.Message{
			ID: fmt.Sprintf("message-%d", event.Seq), Role: event.Role,
			Content: event.Content, CreatedAt: event.CreatedAt,
		}
		switch event.Role {
		case "assistant":
			if len(event.ToolCalls) > 0 {
				for callIndex, call := range event.ToolCalls {
					messages = append(messages, model.Message{
						// L4：同一 assistant 事件的多个 tool call 必须有独立
						// 消息 ID，避免前端按 ID 增量路由时串更新。
						ID: fmt.Sprintf("message-%d-%d", event.Seq, callIndex), Role: "tool", CreatedAt: event.CreatedAt,
						Tool: &model.ToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments, Status: "success"},
					})
				}
				continue
			}
		case "tool":
			message.Role = "tool_result"
			message.Tool = &model.ToolCall{ID: event.ToolCallID, Name: event.Name, Result: event.Content, Status: "success"}
		}
		messages = append(messages, message)
	}
	return messages
}

// engineHistoryFor 返回指定会话引擎历史（会话路由引擎用 HistoryFor；无会话
// 路由能力的单会话桩路径回退活跃引擎——单会话下不存在跨会话污染面）。
func (c *Coordinator) engineHistoryFor(sessionID string) []contract.EngineMessage {
	if routed, ok := c.Core.Deps.Engine.(contract.SessionChatEngine); ok {
		return routed.HistoryFor(sessionID)
	}
	return c.Core.Deps.Engine.History()
}

// SessionRecordLocked 构建当前会话的归档 record（调用方持有 Core.Mu；
// 测试/恢复路径直接构造用）。
func (c *Coordinator) SessionRecordLocked(sessionID string, tasks []dto.TaskRecord) model.SessionRecord {
	return c.sessionRecordLocked(sessionID, tasks)
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
