package session_runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// ForkContext 是 fork 落盘所需的子会话数据（深拷贝结果）：Record 是截断后
// 的权威记录（血缘 meta 在内）；Events 是截断后的事件流；Context 是重写后
// 的 context 四栈；ToolResults 是父会话 tool-results 通道的全量物理复制
// （含 compressed:<segment_id> 压缩原文，删父安全的前提）。
type ForkContext struct {
	Record      model.SessionRecord
	Events      []model.TranscriptEvent
	Context     []byte
	ToolResults []model.StoredToolResult
}

// PrepareFork 基于父会话的已发布快照构建子会话深拷贝（一期决策契约）：
// 切断点锚定 EventSeq + 段落边界；SessionRecord/事件流截断到 fork 轮并
// 重写 CoversEventRange；压缩帧内 fork 走整帧继承 + 帧范围重写；四栈按
// fork 时刻过滤为子会话独立栈起点。tool-results 的物理复制不在这里做——
// 由 fork 流程调用 CloneSessionWorkspace 整通道复制（删父安全的前提）。
func (c *Coordinator) PrepareFork(location Location, childID, parentID string, request model.ForkRequest) (ForkContext, error) {
	childID = strings.TrimSpace(childID)
	parentID = strings.TrimSpace(parentID)
	if childID == "" || parentID == "" {
		return ForkContext{}, errors.New("session fork: parent and child session IDs are required")
	}
	record, has, err := c.LoadSessionRecord(location, parentID)
	if err != nil {
		return ForkContext{}, fmt.Errorf("session fork: load parent record: %w", err)
	}
	if !has {
		return ForkContext{}, fmt.Errorf("session fork: parent session %q has no durable record", parentID)
	}
	// 子会话 record 使用自己的会话身份（decodeSessionRecord 校验
	// record.ID == 存储键，父 ID 必须改写）。
	record.ID = childID
	forkPort, ok := c.Core.Deps.Sessions.(SessionForkPort)
	if !ok {
		return ForkContext{}, errors.New("session fork: durable fork storage is unavailable")
	}
	generation, err := forkPort.CurrentGenerationWorkspace(location.WorkspaceID, parentID)
	if err != nil {
		return ForkContext{}, fmt.Errorf("session fork: read parent generation: %w", err)
	}
	rawEvents, err := forkPort.LoadEventRangeWorkspace(location.WorkspaceID, parentID, 1, math.MaxUint64)
	if err != nil {
		return ForkContext{}, fmt.Errorf("session fork: load parent transcript: %w", err)
	}
	cut, cutEvent, err := resolveForkCut(rawEvents, request)
	if err != nil {
		return ForkContext{}, err
	}
	var cutTime time.Time
	var cutMessageID string
	if cutEvent != nil {
		cutTime = cutEvent.CreatedAt
		cutMessageID = cutEvent.MessageID
	}
	inherited := forkInheritedEvents(rawEvents, cut)

	contextPayload, inheritedFrames, err := c.forkContextRecord(location, parentID, cut, cutTime, cutMessageID)
	if err != nil {
		return ForkContext{}, err
	}
	record = truncateForkRecord(record, inherited, inheritedFrames, cutTime, c.displayUserInput)
	forkedAt := time.Now().UTC()
	record.ForkedFrom = &model.SessionForkRef{
		ParentSessionID:   parentID,
		ParentWorkspaceID: location.WorkspaceID,
		ParentGeneration:  generation,
		ForkPoint: model.ForkPoint{
			EventSeq:     cut,
			EventCount:   uint64(len(inherited)),
			Round:        uint64(len(sessionstore.EventParagraphs(inherited))),
			RequestID:    strings.TrimSpace(request.RequestID),
			MessageID:    cutMessageID,
			MessageCount: len(c.RecordConversation(record)),
		},
		ForkedAt: forkedAt,
	}
	rawResults, err := forkPort.LoadToolResultsWorkspace(location.WorkspaceID, parentID)
	if err != nil {
		return ForkContext{}, fmt.Errorf("session fork: enumerate parent tool results: %w", err)
	}
	return ForkContext{
		Record:      record,
		Events:      forkTranscriptEvents(inherited),
		Context:     contextPayload,
		ToolResults: forkStoredToolResults(rawResults),
	}, nil
}

// LatestForkCut 返回父会话最新完整段落边界（最后一个完整轮次的 EventSeq，
// 含端点；无完整段落时返回 0）。GUI/TUI 默认 fork 入口用它解析切断点。
func (c *Coordinator) LatestForkCut(location Location, parentID string) (uint64, error) {
	forkPort, ok := c.Core.Deps.Sessions.(SessionForkPort)
	if !ok {
		return 0, errors.New("session fork: durable fork storage is unavailable")
	}
	events, err := forkPort.LoadEventRangeWorkspace(location.WorkspaceID, parentID, 1, math.MaxUint64)
	if err != nil {
		return 0, fmt.Errorf("session fork: load parent transcript: %w", err)
	}
	paragraphs := sessionstore.EventParagraphs(events)
	if len(paragraphs) == 0 {
		return 0, nil
	}
	return paragraphs[len(paragraphs)-1].EventTo, nil
}

func forkTranscriptEvents(events []sessionstore.Event) []model.TranscriptEvent {
	adapted := make([]model.TranscriptEvent, len(events))
	for index, event := range events {
		calls := make([]model.TranscriptToolCall, len(event.ToolCalls))
		for callIndex, call := range event.ToolCalls {
			calls[callIndex] = model.TranscriptToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments}
		}
		adapted[index] = model.TranscriptEvent{
			Seq: event.Seq, TaskID: event.TaskID, MessageID: event.MessageID, Role: event.Role,
			ReasoningContent: event.ReasoningContent, Content: event.Content,
			ToolCallID: event.ToolCallID, Name: event.Name, ToolCalls: calls,
			ResultRef: event.ResultRef, TokenCount: event.TokenCount, CreatedAt: event.CreatedAt,
		}
	}
	return adapted
}

func forkStoredToolResults(results []sessionstore.ToolResult) []model.StoredToolResult {
	stored := make([]model.StoredToolResult, len(results))
	for index, result := range results {
		stored[index] = model.StoredToolResult{
			ToolResultRef: model.ToolResultRef{
				Ref: result.Ref, Tool: result.Tool, Digest: result.Digest, Size: result.Size,
				TokenCount: result.TokenCount, CreatedAt: result.CreatedAt,
			},
			Content: result.Content,
		}
	}
	return stored
}

// resolveForkCut 解析 fork 切断点（EventSeq 含端点，段落边界语义）。
// RequestID 解析为该轮完整单元的最后 EventSeq；显式 EventSeq 必须落在
// 段落边界，否则拒绝并提示最近边界。
func resolveForkCut(events []sessionstore.Event, request model.ForkRequest) (uint64, *sessionstore.Event, error) {
	requestID := strings.TrimSpace(request.RequestID)
	if requestID != "" && request.EventSeq != 0 {
		return 0, nil, errors.New("session fork: request_id and event_seq are mutually exclusive")
	}
	if requestID != "" {
		cut := uint64(0)
		found := false
		for _, paragraph := range sessionstore.EventParagraphs(events) {
			if paragraph.RequestID == requestID && paragraph.EventTo > cut {
				cut = paragraph.EventTo
				found = true
			}
		}
		if !found {
			return 0, nil, fmt.Errorf("session fork: request %q not found in parent transcript", requestID)
		}
		return cut, eventAt(events, cut), nil
	}
	if request.EventSeq == 0 {
		return 0, nil, nil // fork 起点：空继承
	}
	end, ok := sessionstore.ParagraphEnd(events, request.EventSeq)
	if !ok {
		return 0, nil, fmt.Errorf("session fork: event_seq %d is not inside a complete paragraph", request.EventSeq)
	}
	if end != request.EventSeq {
		return 0, nil, fmt.Errorf("session fork: event_seq %d is not a paragraph boundary (nearest boundary: %d)", request.EventSeq, end)
	}
	return request.EventSeq, eventAt(events, request.EventSeq), nil
}

func eventAt(events []sessionstore.Event, seq uint64) *sessionstore.Event {
	for index := range events {
		if events[index].Seq == seq {
			return &events[index]
		}
	}
	return nil
}

func forkInheritedEvents(events []sessionstore.Event, cut uint64) []sessionstore.Event {
	if cut == 0 {
		return nil
	}
	inherited := make([]sessionstore.Event, 0)
	for _, event := range events {
		if event.Seq <= cut {
			inherited = append(inherited, event)
		}
	}
	return inherited
}

// truncateForkRecord 把父 SessionRecord 截断到 fork 时刻：Conversation/
// Tasks/PlanStack/Checkpoints/Projection/Execution 一律截断，CoversEventRange
// 重写为切断点；ToolResults 注册表只保留子会话可达的 ref。
func truncateForkRecord(record model.SessionRecord, events []sessionstore.Event, frames []sessionstore.CompactFrame, cutTime time.Time, displayUserInput func(string) string) model.SessionRecord {
	now := time.Now().UTC()
	cut := forkCutSeq(events)
	truncated := model.SessionRecord{
		Version:   SessionRecordVersion,
		ID:        record.ID,
		Title:     inheritedForkTitle(record.Title, record.Conversation.Messages, now, displayUserInput),
		PlanStack: forkPlanFramesByTime(record.PlanStack, cutTime),
		Tasks:     forkTaskRecordsByTime(record.Tasks, cutTime),
		Conversation: model.ConversationRecord{
			Messages:  forkConversationMessages(record.Conversation.Messages, events, cutTime),
			UpdatedAt: now,
		},
		Checkpoints: forkCheckpoints(record.Checkpoints, cut),
		Projection:  forkProjection(record.Projection, cut, record.ID),
		ToolResults: forkToolResultRegistry(record.ToolResults, reachableToolResultRefs(events, record, frames)),
		UpdatedAt:   now,
	}
	truncated.ActivePlanID = record.ActivePlanID
	if !hasPlanFrame(truncated.PlanStack, truncated.ActivePlanID) {
		truncated.ActivePlanID = ""
	}
	truncated.Execution.ReadFiles = forkReadFiles(record.Execution.ReadFiles, cutTime)
	if record.Execution.Task != nil {
		task := *record.Execution.Task
		task.ContextCompactions = append([]model.ContextCompaction(nil), record.Execution.Task.ContextCompactions...)
		if !task.UpdatedAt.IsZero() && task.UpdatedAt.After(cutTime) {
			// 结论晚于切断点：只保留压缩历史证据，不继承 fork 点后的结论。
			task = model.TaskState{ContextCompactions: task.ContextCompactions}
		}
		truncated.Execution.Task = &task
	}
	// Continuation 是最近一次请求的恢复提示，可能晚于切断点：子会话 resume
	// 由 transcript/record 重建，不继承父的 continuation。
	truncated.Execution.Continuation = ""
	return truncated
}

func forkCutSeq(events []sessionstore.Event) uint64 {
	if len(events) == 0 {
		return 0
	}
	return events[len(events)-1].Seq
}

func inheritedForkTitle(title model.SessionTitle, messages []model.Message, now time.Time, displayUserInput func(string) string) model.SessionTitle {
	if title.Value != "" {
		return title
	}
	for _, message := range messages {
		if message.Role != "user" {
			continue
		}
		if value := SessionTitle(displayUserInput(message.Content)); value != "" {
			return model.SessionTitle{Value: value, Source: "fork", FinalizedAt: now}
		}
	}
	return model.SessionTitle{Value: "fork", Source: "fork", FinalizedAt: now}
}

func forkConversationMessages(messages []model.Message, events []sessionstore.Event, cutTime time.Time) []model.Message {
	inheritedIDs := make(map[string]struct{}, len(events))
	for _, event := range events {
		if event.MessageID != "" {
			inheritedIDs[event.MessageID] = struct{}{}
		}
	}
	result := make([]model.Message, 0, len(messages))
	for _, message := range messages {
		keep := false
		if message.ID != "" {
			_, keep = inheritedIDs[message.ID]
		}
		if !keep && (message.CreatedAt.IsZero() || !message.CreatedAt.After(cutTime)) {
			keep = true
		}
		if !keep {
			continue
		}
		copyMessage := message
		if message.Tool != nil {
			tool := *message.Tool
			copyMessage.Tool = &tool
		}
		result = append(result, copyMessage)
	}
	return result
}

func forkPlanFramesByTime(frames []model.SessionPlanFrame, cutTime time.Time) []model.SessionPlanFrame {
	result := make([]model.SessionPlanFrame, 0, len(frames))
	for _, frame := range frames {
		if frame.LoadedAt.IsZero() || !frame.LoadedAt.After(cutTime) {
			result = append(result, frame)
		}
	}
	return result
}

func hasPlanFrame(frames []model.SessionPlanFrame, planID string) bool {
	for _, frame := range frames {
		if frame.ID == planID {
			return true
		}
	}
	return false
}

func forkTaskRecordsByTime(tasks []dto.TaskRecord, cutTime time.Time) []dto.TaskRecord {
	result := make([]dto.TaskRecord, 0, len(tasks))
	for _, task := range tasks {
		if task.CreatedAt.IsZero() || !task.CreatedAt.After(cutTime) {
			result = append(result, task)
		}
	}
	return result
}

func forkCheckpoints(checkpoints []model.TaskCheckpoint, cut uint64) []model.TaskCheckpoint {
	result := make([]model.TaskCheckpoint, 0, len(checkpoints))
	for _, checkpoint := range checkpoints {
		if checkpoint.CoversEventRange.Start > cut {
			continue
		}
		if checkpoint.CoversEventRange.End > cut {
			checkpoint.CoversEventRange.End = cut
		}
		checkpoint.CompletedWork = append([]string(nil), checkpoint.CompletedWork...)
		checkpoint.PendingWork = append([]string(nil), checkpoint.PendingWork...)
		checkpoint.Decisions = append([]string(nil), checkpoint.Decisions...)
		checkpoint.Failures = append([]string(nil), checkpoint.Failures...)
		checkpoint.ChangedFiles = append([]string(nil), checkpoint.ChangedFiles...)
		checkpoint.Artifacts = append([]string(nil), checkpoint.Artifacts...)
		checkpoint.ToolResultRefs = append([]string(nil), checkpoint.ToolResultRefs...)
		result = append(result, checkpoint)
	}
	return result
}

func forkProjection(projection *model.TaskContextProjection, cut uint64, sessionID string) *model.TaskContextProjection {
	if projection == nil {
		return nil
	}
	copyProjection := *projection
	copyProjection.SessionID = sessionID
	copyProjection.ActiveSkills = append([]model.ActiveSkill(nil), projection.ActiveSkills...)
	if projection.ActivePlan != nil {
		activePlan := *projection.ActivePlan
		activePlan.CompletedNodes = append([]string(nil), projection.ActivePlan.CompletedNodes...)
		activePlan.FailedNodes = append([]string(nil), projection.ActivePlan.FailedNodes...)
		activePlan.PendingNodes = append([]string(nil), projection.ActivePlan.PendingNodes...)
		copyProjection.ActivePlan = &activePlan
	}
	copyProjection.Checkpoint.CoversEventRange.End = cut
	copyProjection.Checkpoint.ToolResultRefs = append([]string(nil), projection.Checkpoint.ToolResultRefs...)
	return &copyProjection
}

func forkReadFiles(files []model.ReadFileRef, cutTime time.Time) []model.ReadFileRef {
	result := make([]model.ReadFileRef, 0, len(files))
	for _, file := range files {
		if file.ReadAt.IsZero() || !file.ReadAt.After(cutTime) {
			result = append(result, file)
		}
	}
	return result
}

// reachableToolResultRefs 汇总子会话可达的 tool-result ref：继承事件流的
// ResultRef、截断后 checkpoint/Projection 的 ToolResultRefs、继承压缩帧的
// Evidence 与 compressed:<segment_id> 原文归档。注册表只保留可达 ref，
// 防止子会话通过 read_tool_result 提示触达 fork 点之后的内容。
func reachableToolResultRefs(events []sessionstore.Event, record model.SessionRecord, frames []sessionstore.CompactFrame) map[string]struct{} {
	reachable := make(map[string]struct{})
	for _, event := range events {
		if event.ResultRef != "" {
			reachable[event.ResultRef] = struct{}{}
		}
	}
	for _, checkpoint := range record.Checkpoints {
		for _, ref := range checkpoint.ToolResultRefs {
			reachable[ref] = struct{}{}
		}
	}
	if record.Projection != nil {
		for _, ref := range record.Projection.Checkpoint.ToolResultRefs {
			reachable[ref] = struct{}{}
		}
	}
	for _, frame := range frames {
		if frame.SegmentID != "" {
			reachable[sessionstore.CompressedTurnRefPrefix+frame.SegmentID] = struct{}{}
		}
		for _, evidence := range frame.Evidence {
			if evidence.Ref != "" {
				reachable[evidence.Ref] = struct{}{}
			}
		}
	}
	return reachable
}

func forkToolResultRegistry(refs []model.ToolResultRef, reachable map[string]struct{}) []model.ToolResultRef {
	result := make([]model.ToolResultRef, 0, len(refs))
	for _, ref := range refs {
		if _, ok := reachable[ref.Ref]; ok {
			result = append(result, ref)
		}
	}
	return result
}

// forkContextRecord 重写父 context 四栈为子会话独立栈起点：Plan/Task/Skill
// 按进入时间过滤到 fork 时刻；CompactStack 整帧继承 + 帧范围重写。
func (c *Coordinator) forkContextRecord(location Location, parentID string, cut uint64, cutTime time.Time, cutMessageID string) ([]byte, []sessionstore.CompactFrame, error) {
	forkPort, ok := c.Core.Deps.Sessions.(SessionForkPort)
	if !ok {
		return nil, nil, errors.New("session fork: durable fork storage is unavailable")
	}
	payload, err := forkPort.LoadContextStateWorkspace(location.WorkspaceID, parentID)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			data, _ := json.Marshal(sessionstore.SessionContextRecord{SchemaVersion: sessionstore.SessionContextSchemaVersion})
			return data, nil, nil
		}
		return nil, nil, fmt.Errorf("session fork: load parent context: %w", err)
	}
	var record sessionstore.SessionContextRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return nil, nil, fmt.Errorf("session fork: decode parent context: %w", err)
	}
	if record.SchemaVersion != sessionstore.SessionContextSchemaVersion {
		return nil, nil, fmt.Errorf("session fork: parent context has unsupported schema version %d (want %d)", record.SchemaVersion, sessionstore.SessionContextSchemaVersion)
	}
	record.PlanStack = forkContextPlanFrames(record.PlanStack, cutTime)
	record.TaskStack = forkContextTaskFrames(record.TaskStack, cutTime)
	record.SkillStack = forkContextSkillFrames(record.SkillStack, cutTime)
	frames := rewriteForkCompactStack(record.CompactStack, cut, cutMessageID)
	record.CompactStack = frames
	data, err := json.Marshal(record)
	if err != nil {
		return nil, nil, fmt.Errorf("session fork: encode child context: %w", err)
	}
	return data, frames, nil
}

func forkContextPlanFrames(frames []sessionstore.PlanFrame, cutTime time.Time) []sessionstore.PlanFrame {
	result := make([]sessionstore.PlanFrame, 0, len(frames))
	for _, frame := range frames {
		if frame.EnteredAt.IsZero() || !frame.EnteredAt.After(cutTime) {
			result = append(result, frame)
		}
	}
	return result
}

func forkContextTaskFrames(frames []sessionstore.TaskFrame, cutTime time.Time) []sessionstore.TaskFrame {
	result := make([]sessionstore.TaskFrame, 0, len(frames))
	for _, frame := range frames {
		if frame.EnteredAt.IsZero() || !frame.EnteredAt.After(cutTime) {
			result = append(result, frame)
		}
	}
	return result
}

func forkContextSkillFrames(frames []sessionstore.SkillFrame, cutTime time.Time) []sessionstore.SkillFrame {
	result := make([]sessionstore.SkillFrame, 0, len(frames))
	for _, frame := range frames {
		if frame.ActivatedAt.IsZero() || !frame.ActivatedAt.After(cutTime) {
			result = append(result, frame)
		}
	}
	return result
}

// rewriteForkCompactStack 处理压缩帧内的 fork 切断：整帧继承 + 范围重写
// （EventTo/MessageTo 到切断点）。EventRevision 保留父修订（标识继承事件
// 的来源提交，正是"切断点所属版本"）；完全晚于切断点的帧丢弃。
func rewriteForkCompactStack(frames []sessionstore.CompactFrame, cut uint64, cutMessageID string) []sessionstore.CompactFrame {
	result := make([]sessionstore.CompactFrame, 0, len(frames))
	for _, frame := range frames {
		if frame.EventFrom == 0 && frame.EventTo == 0 {
			// 旧帧无事件坐标：无可截断依据，保守保留。
			result = append(result, frame)
			continue
		}
		if frame.EventTo <= cut {
			result = append(result, frame)
			continue
		}
		if frame.EventFrom > cut {
			continue
		}
		frame.EventTo = cut
		if cutMessageID != "" {
			frame.MessageTo = cutMessageID
		}
		result = append(result, frame)
	}
	return result
}
