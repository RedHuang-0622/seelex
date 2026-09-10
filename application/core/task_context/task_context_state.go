package task_context

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/internal/limits"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/application/prompt"
)

const (
	activeSkillVersion = "installed-v1"
	// ActiveSkillMarker 是激活技能正文事件的内部标记。与
	// context_runtime.ActiveSkillPrefix 同源字符串（task_context 不反向依赖
	// context_runtime）：装配根以 IsActiveSkillContent 判定 internal → 事件不
	// 落盘、不进前端可见会话、import/压缩时跳过。正文作为一条 internal user
	// 事件 append-only 进入 transcript，随定稿轮次作为稳定前缀缓存；压缩窗口
	// 裁剪后技能随旧轮次自然消失（长历史由压缩帧/会话存档检索）。
	ActiveSkillMarker = "<!-- seelex:active-skill:v1 -->"
)

// ActivateTaskSkillsLocked 把请求级 skill 层投影进任务状态（调用方持有
// Core.ViewMu）。激活的技能正文以 internal 事件 append 进 transcript
// （append-only：同名同正文只落一次；后续轮次不再重建或改写）。
func (c *Coordinator) ActivateTaskSkillsLocked(state *TaskExecutionState, layers []prompt.PromptLayer) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._ActivateTaskSkillsLocked(state, layers)
}

func (c *Coordinator) _ActivateTaskSkillsLocked(state *TaskExecutionState, layers []prompt.PromptLayer) {
	if state == nil {
		return
	}
	st := c.sessionStateForTaskLocked(state)
	// append-only 去重基准 = 本任务既有激活记录（同名不再重复追加，避免同一
	// ReAct 循环内重复 skill_activate 产生重复指令正文）。不按 transcript 判重：
	// 上个任务遗留的同名技能事件属于历史轮次，新任务必须重新落一次自己的技能
	// 正文（跟随对话 append-only：正文出现在本任务激活时的 transcript 位置）。
	logged := make(map[string]struct{}, len(state.ActiveSkills))
	for _, active := range state.ActiveSkills {
		logged[active.SkillID] = struct{}{}
	}
	state.TrustedSkillLayers = append([]prompt.PromptLayer(nil), layers...)
	state.ActiveSkills = make([]model.ActiveSkill, 0, len(layers))
	for _, layer := range layers {
		digest := sha256.Sum256([]byte(layer.Text))
		state.ActiveSkills = append(state.ActiveSkills, model.ActiveSkill{
			SkillID: layer.Name, Version: activeSkillVersion,
			ContentHash: hex.EncodeToString(digest[:]), Scope: "task",
			ActivatedAt: time.Now(), SourceEvent: st.transcriptSeq + 1,
		})
	}
	c.ensureActiveSkillEventsLocked(st, state.TrustedSkillLayers, logged)
	c.syncGoalSkillActiveLocked()
}

// ensureActiveSkillEventsLocked 只追加本任务尚未落盘的技能正文事件（logged 里
// 已存在的层名跳过）。事件作为 internal user 轮次进入 transcript（append-only：
// 每份正文只写一次，随定稿轮次作为稳定前缀缓存；压缩窗口裁剪后技能随旧轮次
// 自然消失，长历史由压缩帧/会话存档检索）。
func (c *Coordinator) ensureActiveSkillEventsLocked(st *sessionTaskRuntime, layers []prompt.PromptLayer, logged map[string]struct{}) {
	for _, layer := range layers {
		text := strings.TrimSpace(layer.Text)
		if text == "" {
			continue
		}
		if logged != nil {
			if _, dup := logged[layer.Name]; dup {
				continue
			}
		}
		c.appendTranscriptEventLocked(st, model.TranscriptEvent{
			Role:    "user",
			Content: ActiveSkillMarker + "\n## Trusted Active Skill: " + layer.Name + "\n" + text,
		})
	}
}

// SyncGoalSkillActiveLocked 把任务级 skill 集投影到 lock-free 可见性值
// （Runtime.VisibleTools 消费；调用方持有 Core.ViewMu）。
func (c *Coordinator) SyncGoalSkillActiveLocked() {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._SyncGoalSkillActiveLocked()
}

func (c *Coordinator) _SyncGoalSkillActiveLocked() {
	c.syncGoalSkillActiveLocked()
}

func (c *Coordinator) syncGoalSkillActiveLocked() {
	c.goalSkillActive.Store(c.goalSkillActiveForLocked(c.activeSessionIDLocked()))
}

// AppendTranscriptEventLocked 追加一条 append-only transcript 事件（seq 自增；
// 调用方持有 Core.ViewMu）。事件归属会话由 event.TaskID 反查，缺省活跃会话。
func (c *Coordinator) AppendTranscriptEventLocked(event model.TranscriptEvent) model.TranscriptEvent {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._AppendTranscriptEventLocked(event)
}

func (c *Coordinator) _AppendTranscriptEventLocked(event model.TranscriptEvent) model.TranscriptEvent {
	st := c.activeSessionLocked()
	if event.TaskID != "" {
		if byRequest := c.sessionForRequestLocked(event.TaskID); byRequest != nil {
			st = byRequest
		}
	}
	// S19/D8：给模型看的内部材料（检查点渲染正文等）由生产方置
	// wire_material=true；否则重启后 internal 行进不了装配。
	if event.Kind == model.TranscriptEventKindInternal {
		event.WireMaterial = true
	}
	return c.appendTranscriptEventLocked(st, event)
}

// AppendTranscriptEventForLocked 追加一条指定会话的 transcript 事件（调用
// 方持有 Core.ViewMu；hook 等显式会话路径用）。
func (c *Coordinator) AppendTranscriptEventForLocked(sessionID string, event model.TranscriptEvent) model.TranscriptEvent {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._AppendTranscriptEventForLocked(sessionID, event)
}

func (c *Coordinator) _AppendTranscriptEventForLocked(sessionID string, event model.TranscriptEvent) model.TranscriptEvent {
	st := c.sessionStateLocked(sessionID)
	return c.appendTranscriptEventLocked(st, event)
}

// ImportEngineHistoryAsTranscriptLocked 把引擎既有历史导入活跃会话
// transcript（装配期/恢复路径；调用方持有 Core.ViewMu）。
func (c *Coordinator) ImportEngineHistoryAsTranscriptLocked(history []contract.EngineMessage) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._ImportEngineHistoryAsTranscriptLocked(history)
}

func (c *Coordinator) _ImportEngineHistoryAsTranscriptLocked(history []contract.EngineMessage) {
	st := c.activeSessionLocked()
	c.importEngineHistoryLocked(st, history)
}

func (c *Coordinator) importEngineHistoryLocked(st *sessionTaskRuntime, history []contract.EngineMessage) {
	for _, message := range history {
		if message.Role == "system" || c.isInternalContent(message.Content) {
			continue
		}
		event := model.TranscriptEvent{
			Role: message.Role, ReasoningContent: message.ReasoningContent, Content: message.Content,
			ToolCallID: message.ToolCallID, Name: message.Name,
		}
		if event.Role == "tool" && c.isOversized(event.Content, defaultToolResultLimit()) {
			stored := c.storeToolResultLocked(st, event.Name, event.Content)
			event.Content = c.oversizedWarning(event.Name, stored.Ref)
			event.ResultRef = stored.Ref
			st.resultRefsByToolCallID[event.ToolCallID] = stored.Ref
		}
		for _, call := range message.ToolCalls {
			event.ToolCalls = append(event.ToolCalls, model.TranscriptToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
		}
		c.appendTranscriptEventLocked(st, event)
	}
}

// applyTranscriptRoleFieldsLocked 给消息行盖群聊角色归属与排序键（R4）：
// user 行开启新 round；assistant/tool 行归 main；role_session_id 默认取本
// 会话 ID。显式已填字段不覆盖，给未来 agent-team 生产者留入口。
func (c *Coordinator) applyTranscriptRoleFieldsLocked(st *sessionTaskRuntime, event *model.TranscriptEvent) {
	if event.RoleName == "" {
		switch {
		case event.Role == "system":
			event.RoleName = "system"
		case event.Kind == model.TranscriptEventKindInternal:
			// internal/context 行是任务/plan/goal/subagent 状态材料，不是
			// 用户输入；provider role 仍走标准 role，逻辑归属登记为 system。
			event.RoleName = "system"
		case event.Role == "internal_user" || event.Role == "context":
			event.RoleName = "system"
		case event.Role == "user":
			event.RoleName = "user"
		case event.Role == "assistant" || event.Role == "tool":
			event.RoleName = "main"
		}
	}
	if event.RoleName == "" {
		return
	}
	if event.RoleSessionID == "" {
		event.RoleSessionID = st.sessionID
	}
	if event.RoundID == 0 {
		if event.RoleName == "user" {
			st.roleRoundID++
			st.roleUnitSeq = 0
		}
		event.RoundID = st.roleRoundID
	}
	st.roleUnitSeq++
	if event.UnitSeq == 0 {
		event.UnitSeq = st.roleUnitSeq
	}
}

func (c *Coordinator) appendTranscriptEventLocked(st *sessionTaskRuntime, event model.TranscriptEvent) model.TranscriptEvent {
	event.Kind = classifyTranscriptEventKind(event)
	c.applyTranscriptRoleFieldsLocked(st, &event)
	st.transcriptSeq++
	event.Seq = st.transcriptSeq
	if event.TaskID == "" && st.taskExecution != nil {
		event.TaskID = st.taskExecution.RequestID
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	event.TokenCount = c._CountTranscriptEvent(event)
	st.transcript = append(st.transcript, event)
	return event
}

// classifyTranscriptEventKind 返回事件的显式类别；调用方已标注 Kind 时原样保留，
// 否则按 Role/ToolCalls/内部标记回退，保证轨迹多线谱在旧事件上也有稳定分类。
func classifyTranscriptEventKind(event model.TranscriptEvent) string {
	if event.Kind != "" {
		return event.Kind
	}
	switch event.Role {
	case "user":
		if strings.HasPrefix(event.Content, "<!-- seelex:") {
			return model.TranscriptEventKindInternal
		}
		return model.TranscriptEventKindUserInput
	case "assistant":
		if len(event.ToolCalls) > 0 {
			return model.TranscriptEventKindToolCall
		}
		return model.TranscriptEventKindLLM
	case "tool":
		return model.TranscriptEventKindToolOutput
	case "system":
		return model.TranscriptEventKindSystem
	case "error":
		return model.TranscriptEventKindError
	default:
		return model.TranscriptEventKindNotice
	}
}

// CountTranscriptEvent 估算一条 transcript 事件的 token 数。
func (c *Coordinator) CountTranscriptEvent(event model.TranscriptEvent) int {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._CountTranscriptEvent(event)
}

func (c *Coordinator) _CountTranscriptEvent(event model.TranscriptEvent) int {
	message := contract.EngineMessage{
		Role: event.Role, ReasoningContent: event.ReasoningContent, Content: event.Content,
		ContentSet: true, ToolCallID: event.ToolCallID, Name: event.Name,
	}
	for _, call := range event.ToolCalls {
		message.ToolCalls = append(message.ToolCalls, contract.EngineToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
	}
	return c.tokenCounter.CountMessage(message)
}

// RecordLLMComplete 记录一次 LLM 完成（真实 usage 校准 + assistant 事件；
// 自行加锁）。ctx 携带会话 ID 时路由到对应会话。
func (c *Coordinator) RecordLLMComplete(ctx context.Context, info session.LLMInfo) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._RecordLLMComplete(ctx, info)
}

func (c *Coordinator) _RecordLLMComplete(ctx context.Context, info session.LLMInfo) {
	if info.Response == "" && len(info.ToolCalls) == 0 && info.Usage == nil {
		return
	}
	st := c.runtimeForContextLocked(ctx)
	state := st.taskExecution
	if state == nil {
		return
	}
	if info.Usage != nil {
		state.TokenAudit.ActualPromptTokens = info.Usage.PromptTokens
		state.TokenAudit.UpdatedAt = time.Now()
		// 用真实 usage 反馈校准估算因子（同一请求的估算与实际配对）。
		if counter, ok := c.tokenCounter.(*CalibratedTokenCounter); ok &&
			state.TokenAudit.EstimatedPromptTokens > 0 && info.Usage.PromptTokens > 0 {
			counter.Observe(state.TokenAudit.EstimatedPromptTokens, info.Usage.PromptTokens)
		}
	}
	if info.Response == "" && len(info.ToolCalls) == 0 {
		return
	}
	event := model.TranscriptEvent{Role: "assistant", Content: info.Response}
	for _, call := range info.ToolCalls {
		item := model.TranscriptToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments}
		event.ToolCalls = append(event.ToolCalls, item)
		st.pendingProviderCalls = append(st.pendingProviderCalls, item)
	}
	c.appendTranscriptEventLocked(st, event)
}

// BackfillAssistantReasoning 在聊天回合结束后，用引擎历史中的 assistant
// 消息补齐 transcript 事件的推理草稿。原因：LoopHooks 的 LLMInfo 不携带
// reasoning（Seele 只回传 Response/ToolCalls/Usage），推理只在引擎历史
// assistant 消息上可见；若不回填，record/transcript 落盘后草稿丢失，重启
// 恢复的轨迹只剩工具痕迹（2026-09-08 实测：live reasoning=555/194 →
// store=0）。匹配以归一化 content + tool call ID 集合为准。
func (c *Coordinator) BackfillAssistantReasoning(sessionID string, history []contract.EngineMessage) int {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._BackfillAssistantReasoning(sessionID, history)
}

type reasoningCandidate struct {
	content   string
	reasoning string
	callIDs   map[string]bool
}

func (c *Coordinator) _BackfillAssistantReasoning(sessionID string, history []contract.EngineMessage) int {
	st := c.sessionStateLocked(sessionID)
	var candidates []reasoningCandidate
	for _, message := range history {
		if message.Role != "assistant" || strings.TrimSpace(message.ReasoningContent) == "" {
			continue
		}
		candidate := reasoningCandidate{
			content:   normalizeReasoningContent(message.Content),
			reasoning: message.ReasoningContent,
			callIDs:   make(map[string]bool, len(message.ToolCalls)),
		}
		for _, call := range message.ToolCalls {
			candidate.callIDs[call.ID] = true
		}
		candidates = append(candidates, candidate)
	}
	if len(candidates) == 0 {
		return 0
	}
	backfilled := 0
	for index := range st.transcript {
		event := &st.transcript[index]
		if event.Role != "assistant" || strings.TrimSpace(event.ReasoningContent) != "" {
			continue
		}
		candidate := findReasoningCandidate(candidates, event)
		if candidate == nil {
			continue
		}
		event.ReasoningContent = candidate.reasoning
		event.TokenCount = c._CountTranscriptEvent(*event)
		backfilled++
		candidates = removeReasoningCandidate(candidates, candidate)
	}
	return backfilled
}

func findReasoningCandidate(candidates []reasoningCandidate, event *model.TranscriptEvent) *reasoningCandidate {
	eventIDs := make(map[string]bool, len(event.ToolCalls))
	for _, call := range event.ToolCalls {
		eventIDs[call.ID] = true
	}
	for index := range candidates {
		candidate := &candidates[index]
		if len(candidate.callIDs) != len(eventIDs) {
			continue
		}
		idsMatch := true
		for id := range eventIDs {
			if !candidate.callIDs[id] {
				idsMatch = false
				break
			}
		}
		if !idsMatch {
			continue
		}
		if normalizeReasoningContent(event.Content) != candidate.content {
			continue
		}
		return candidate
	}
	return nil
}

func removeReasoningCandidate(candidates []reasoningCandidate, target *reasoningCandidate) []reasoningCandidate {
	for index := range candidates {
		if &candidates[index] == target {
			return append(candidates[:index], candidates[index+1:]...)
		}
	}
	return candidates
}

func normalizeReasoningContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" || strings.HasPrefix(content, "<!-- seelex:") ||
		strings.HasPrefix(content, "[Seelex recovery note:") {
		return ""
	}
	return content
}

// MergeToolNarration 把“流式正文只进视图、引擎/转录事件只含 tool_calls”
// 的说明文本并入对应 transcript tool_call 事件（2026-09-08：框架在
// CompleteStream 返回 toolCalls 时丢弃 content，正文只经 onChunk 展示，
// 不落盘则重启恢复只剩工具痕迹）。candidates 是视图里按顺序出现的
// assistant 正文（含普通答复与工具轮说明）；只消费“在 transcript 里找不到
// 对应正文”的候选，并按序赋给 content 为空的 tool_call 事件。
func (c *Coordinator) MergeToolNarration(sessionID string, candidates []string) int {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._MergeToolNarration(sessionID, candidates)
}

func (c *Coordinator) _MergeToolNarration(sessionID string, candidates []string) int {
	st := c.sessionStateLocked(sessionID)
	pending := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		content := normalizeReasoningContent(candidate)
		if content == "" {
			continue
		}
		if assistantContentExists(st.transcript, content) {
			continue
		}
		pending = append(pending, content)
	}
	if len(pending) == 0 {
		return 0
	}
	merged := 0
	next := 0
	for index := range st.transcript {
		event := &st.transcript[index]
		if event.Role != "assistant" || len(event.ToolCalls) == 0 {
			continue
		}
		if strings.TrimSpace(event.Content) != "" {
			continue
		}
		if next >= len(pending) {
			break
		}
		event.Content = pending[next]
		event.TokenCount = c._CountTranscriptEvent(*event)
		next++
		merged++
	}
	return merged
}

func assistantContentExists(events []model.TranscriptEvent, content string) bool {
	for _, event := range events {
		if event.Role == "assistant" && len(event.ToolCalls) == 0 &&
			normalizeReasoningContent(event.Content) == content {
			return true
		}
	}
	return false
}

// EnsureToolCallTranscriptLocked 保证工具调用宣告已入指定会话 transcript
// （缺失时补一条 assistant 事件；调用方持有 Core.ViewMu）。
func (c *Coordinator) EnsureToolCallTranscriptLocked(sessionID, name, fallbackID, arguments string) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._EnsureToolCallTranscriptLocked(sessionID, name, fallbackID, arguments)
}

func (c *Coordinator) _EnsureToolCallTranscriptLocked(sessionID, name, fallbackID, arguments string) {
	st := c.sessionStateLocked(sessionID)
	for _, call := range st.pendingProviderCalls {
		if call.Name == name && call.Arguments == arguments {
			return
		}
	}
	call := model.TranscriptToolCall{ID: fallbackID, Name: name, Arguments: arguments}
	st.pendingProviderCalls = append(st.pendingProviderCalls, call)
	c.appendTranscriptEventLocked(st, model.TranscriptEvent{Role: "assistant", ToolCalls: []model.TranscriptToolCall{call}})
}

// RecordToolTranscriptLocked 记录指定会话工具结果事件（错误呈现/超限引用；
// 返回可见内容与结果引用；调用方持有 Core.ViewMu）。
func (c *Coordinator) RecordToolTranscriptLocked(sessionID, name, fallbackID, arguments, result string, toolErr error) (string, string) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._RecordToolTranscriptLocked(sessionID, name, fallbackID, arguments, result, toolErr)
}

func (c *Coordinator) _RecordToolTranscriptLocked(sessionID, name, fallbackID, arguments, result string, toolErr error) (string, string) {
	st := c.sessionStateLocked(sessionID)
	callID := fallbackID
	for index, call := range st.pendingProviderCalls {
		if call.Name != name || (arguments != "" && call.Arguments != arguments) {
			continue
		}
		callID = call.ID
		st.pendingProviderCalls = append(st.pendingProviderCalls[:index], st.pendingProviderCalls[index+1:]...)
		break
	}
	content := result
	resultRef := ""
	if toolErr != nil {
		content = c.presentToolError(name, toolErr)
	} else if c.isOversized(result, defaultToolResultLimit()) {
		stored := c.storeToolResultLocked(st, name, result)
		resultRef = stored.Ref
		content = c.oversizedWarning(name, resultRef)
		st.resultRefsByToolCallID[callID] = resultRef
	}
	c.appendTranscriptEventLocked(st, model.TranscriptEvent{
		Role: "tool", Content: content, ToolCallID: callID, Name: name, ResultRef: resultRef,
	})
	return content, resultRef
}

// defaultToolResultLimit 返回工具结果字符预算（seelex.yaml limits 段
// max_tool_result_chars，默认 20000）。
func defaultToolResultLimit() int {
	return limits.Get().MaxToolResultChars
}

// DefaultToolResultLimit 返回工具结果字符预算（导出面；根包兼容包装用）。
func DefaultToolResultLimit() int {
	return defaultToolResultLimit()
}

// StoreToolResultLocked 把超限工具结果以引用形式存储（活跃会话；内容 +
// 摘要元数据）。
func (c *Coordinator) StoreToolResultLocked(name, content string) model.StoredToolResult {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._StoreToolResultLocked(name, content)
}

func (c *Coordinator) _StoreToolResultLocked(name, content string) model.StoredToolResult {
	st := c.activeSessionLocked()
	return c.storeToolResultLocked(st, name, content)
}

// StoreToolResultForLocked 把超限工具结果以引用形式存储到指定会话。
func (c *Coordinator) StoreToolResultForLocked(sessionID, name, content string) model.StoredToolResult {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._StoreToolResultForLocked(sessionID, name, content)
}

func (c *Coordinator) _StoreToolResultForLocked(sessionID, name, content string) model.StoredToolResult {
	return c.storeToolResultLocked(c.sessionStateLocked(sessionID), name, content)
}

func (c *Coordinator) storeToolResultLocked(st *sessionTaskRuntime, name, content string) model.StoredToolResult {
	digest := sha256.Sum256([]byte(name + "\x00" + content))
	digestText := hex.EncodeToString(digest[:])
	result := model.StoredToolResult{
		ToolResultRef: model.ToolResultRef{
			Ref: "tr-" + digestText[:24], Tool: name, Digest: "sha256:" + digestText,
			Size: len([]byte(content)), TokenCount: c.tokenCounter.CountText(content), CreatedAt: time.Now(),
		},
		Content: content,
	}
	for _, existing := range st.toolResultRefs {
		if existing.Ref == result.Ref {
			return result
		}
	}
	st.toolResultRefs = append(st.toolResultRefs, result.ToolResultRef)
	st.pendingToolResults = append(st.pendingToolResults, result)
	return result
}

// EnsureFinalAssistantTranscript 在请求结束时补一条可见 assistant 终态事件
// （去重；自行加锁；requestID 反查会话）。
func (c *Coordinator) EnsureFinalAssistantTranscript(requestID, content string) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._EnsureFinalAssistantTranscript(requestID, content)
}

func (c *Coordinator) _EnsureFinalAssistantTranscript(requestID, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	st := c.sessionForRequestLocked(requestID)
	if st == nil {
		st = c.activeSessionLocked()
	}
	if state := st.taskExecution; state == nil || state.RequestID != requestID {
		return
	}
	if len(st.transcript) > 0 {
		last := st.transcript[len(st.transcript)-1]
		if last.TaskID == requestID && last.Role == "assistant" && last.Content == content && len(last.ToolCalls) == 0 {
			return
		}
	}
	c.appendTranscriptEventLocked(st, model.TranscriptEvent{TaskID: requestID, Role: "assistant", Content: content})
}

// sessionStateForTaskLocked 返回任务状态所属会话的运行时（调用方持有
// Core.ViewMu）。
func (c *Coordinator) sessionStateForTaskLocked(state *TaskExecutionState) *sessionTaskRuntime {
	if state != nil {
		if st := c.sessionForRequestLocked(state.RequestID); st != nil {
			return st
		}
	}
	return c.activeSessionLocked()
}

// TaskProjectionLocked 构建指定会话任务的权威投影（会话归档用；调用方持有
// Core.ViewMu）。
func (c *Coordinator) TaskProjectionLocked(sessionID string) *model.TaskContextProjection {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._TaskProjectionLocked(sessionID)
}

func (c *Coordinator) _TaskProjectionLocked(sessionID string) *model.TaskContextProjection {
	st := c.sessionStateLocked(sessionID)
	state := st.taskExecution
	if state == nil {
		return nil
	}
	projectID := ""
	if c.Snapshot.CurrentWorkspace != nil {
		projectID = c.Snapshot.CurrentWorkspace.ID
	}
	checkpoint := c.buildTaskCheckpointLocked(st, state)
	objectiveRef := ""
	if checkpoint.CoversEventRange.Start != 0 {
		objectiveRef = fmt.Sprintf("event:%d", checkpoint.CoversEventRange.Start)
	}
	return &model.TaskContextProjection{
		SchemaVersion: 1, ProjectID: projectID, SessionID: sessionID, TaskID: state.RequestID,
		Status: state.Status, ObjectiveRef: objectiveRef,
		ActiveSkills: append([]model.ActiveSkill(nil), state.ActiveSkills...), ActivePlan: c._ActivePlanProjectionLocked(),
		Checkpoint: checkpoint, TokenAudit: state.TokenAudit, UpdatedAt: time.Now(),
	}
}

// BuildTaskCheckpointLocked 构建任务 checkpoint（调用方持有 Core.ViewMu）。
func (c *Coordinator) BuildTaskCheckpointLocked(state *TaskExecutionState) model.TaskCheckpoint {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._BuildTaskCheckpointLocked(state)
}

func (c *Coordinator) _BuildTaskCheckpointLocked(state *TaskExecutionState) model.TaskCheckpoint {
	st := c.sessionStateForTaskLocked(state)
	return c.buildTaskCheckpointLocked(st, state)
}

func (c *Coordinator) buildTaskCheckpointLocked(st *sessionTaskRuntime, state *TaskExecutionState) model.TaskCheckpoint {
	checkpoint := model.TaskCheckpoint{}
	if state.InheritedCheckpoint != nil {
		checkpoint = CloneTaskCheckpoint(*state.InheritedCheckpoint)
	}
	checkpoint.Version = state.ContextVersion
	checkpoint.CoversEventRange = extendEventRange(checkpoint.CoversEventRange, eventRangeForTask(st.transcript, state.RequestID))
	checkpoint.UpdatedAt = time.Now()
	keys := make([]string, 0, len(state.checkpoints))
	for key := range state.checkpoints {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		node := state.checkpoints[key]
		record := fmt.Sprintf("node=%s status=%s", node.NodeKey, node.Status)
		switch node.Status {
		case string(model.NodeCompleted):
			checkpoint.CompletedWork = AppendUniqueStrings(checkpoint.CompletedWork, record)
		case string(model.NodeFailed), string(model.NodeAborted), string(model.NodeCanceled), string(model.NodePanicked):
			checkpoint.Failures = AppendUniqueStrings(checkpoint.Failures, record+boundedFailure(node.Failure))
		default:
			checkpoint.PendingWork = AppendUniqueStrings(checkpoint.PendingWork, record)
		}
		checkpoint.ChangedFiles = AppendUniqueStrings(checkpoint.ChangedFiles, node.ChangedFiles...)
		checkpoint.Artifacts = AppendUniqueStrings(checkpoint.Artifacts, node.Artifacts...)
	}
	if state.Terminal != nil {
		checkpoint.Artifacts = AppendUniqueStrings(checkpoint.Artifacts, state.Terminal.Artifacts...)
		checkpoint.Decisions = AppendUniqueStrings(checkpoint.Decisions, state.Terminal.DecisionQuestion)
	}
	for _, event := range st.transcript {
		if event.TaskID == state.RequestID && event.ResultRef != "" {
			checkpoint.ToolResultRefs = AppendUniqueStrings(checkpoint.ToolResultRefs, event.ResultRef)
		}
	}
	return checkpoint
}

func extendEventRange(existing, current model.EventRange) model.EventRange {
	if existing.Start == 0 {
		return current
	}
	if current.Start == 0 {
		return existing
	}
	if current.Start < existing.Start {
		existing.Start = current.Start
	}
	if current.End > existing.End {
		existing.End = current.End
	}
	return existing
}

func boundedFailure(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return " failure=" + BoundedEvidence(value)
}

func eventRangeForTask(events []model.TranscriptEvent, taskID string) model.EventRange {
	rangeForTask := model.EventRange{}
	for _, event := range events {
		if event.TaskID != taskID {
			continue
		}
		if rangeForTask.Start == 0 {
			rangeForTask.Start = event.Seq
		}
		rangeForTask.End = event.Seq
	}
	return rangeForTask
}

// AppendUniqueStrings 追加去重后的非空字符串。
func AppendUniqueStrings(values []string, incoming ...string) []string {
	for _, value := range incoming {
		value = strings.TrimSpace(value)
		if value != "" && !ContainsString(values, value) {
			values = append(values, value)
		}
	}
	return values
}

// ActivePlanProjectionLocked 返回活跃会话当前激活 Plan 的只读投影（调用方
// 持有 Core.ViewMu）。
func (c *Coordinator) ActivePlanProjectionLocked() *model.ActivePlanProjection {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._ActivePlanProjectionLocked()
}

func (c *Coordinator) _ActivePlanProjectionLocked() *model.ActivePlanProjection {
	st := c.activeSessionLocked()
	return ActivePlanProjection(c.Snapshot.Runtime.Plan, st.activePlanID, st.planSequence)
}

func (c *Coordinator) restoreTaskProjectionLocked(st *sessionTaskRuntime, projection *model.TaskContextProjection, fallbackObjective string) {
	c.prompt.ClearSkillLayers()
	if projection == nil {
		st.taskExecution = nil
		st.taskService = nil
		c.syncGoalSkillActiveLocked()
		return
	}
	objective := c.resolveObjectiveRefLocked(st, projection.ObjectiveRef)
	if objective == "" {
		objective = strings.TrimSpace(fallbackObjective)
	}
	state := NewTaskExecutionState(projection.TaskID, objective, c.prompt.CurrentEffort())
	state.Status = projection.Status
	state.ContextVersion = projection.Checkpoint.Version
	if state.ContextVersion == 0 {
		state.ContextVersion = 1
	}
	checkpoint := CloneTaskCheckpoint(projection.Checkpoint)
	if HasSubstantiveCheckpoint(checkpoint) {
		state.InheritedCheckpoint = &checkpoint
	}
	state.ActiveSkills = append([]model.ActiveSkill(nil), projection.ActiveSkills...)
	state.TokenAudit = projection.TokenAudit
	if frame := ActivePlanFrame(st.planStack, st.activePlanID); frame != nil {
		state.PlanArguments = frame.Arguments
	}
	for _, active := range projection.ActiveSkills {
		skill, ok := c.Deps.Skills.Get(active.SkillID)
		if !ok {
			continue
		}
		digest := sha256.Sum256([]byte(skill.Prompt))
		if hex.EncodeToString(digest[:]) != active.ContentHash {
			continue
		}
		layer := prompt.PromptLayer{Kind: "skill", Name: skill.Name, Text: skill.Prompt}
		state.TrustedSkillLayers = append(state.TrustedSkillLayers, layer)
		c.prompt.PushSkillLayer(layer.Kind, layer.Name, layer.Text)
	}
	st.taskExecution = state
	// 恢复补讲：会话存档的 transcript 不含 internal 技能事件（存档层按
	// isInternalContent 过滤），恢复后把仍激活的技能正文补 append 一次到
	// transcript 尾部 —— 恢复会话的下一次装配即携带技能，且仍是 append-only
	// 单次写入（只追加，不改写既有定稿轮次）。logged=nil 强制补落（恢复的
	// transcript 已被整键重建，无同任务事件可判重）。
	c.ensureActiveSkillEventsLocked(st, state.TrustedSkillLayers, nil)
	st.taskService = newTaskService(c.activeSessionIDLocked(), c, state, c.queuedInputRefs)
	c.syncGoalSkillActiveLocked()
}

func (c *Coordinator) resolveObjectiveRefLocked(st *sessionTaskRuntime, objectiveRef string) string {
	const eventPrefix = "event:"
	if !strings.HasPrefix(objectiveRef, eventPrefix) {
		return strings.TrimSpace(objectiveRef)
	}
	sequence, err := strconv.ParseUint(strings.TrimPrefix(objectiveRef, eventPrefix), 10, 64)
	if err != nil {
		return ""
	}
	for _, event := range st.transcript {
		if event.Seq == sequence && event.Role == "user" {
			return event.Content
		}
	}
	return ""
}

// RecordContextCompactionLocked 记录一次上下文压缩（仅运行中任务；调用方
// 持有 Core.ViewMu；requestID 反查会话）。
func (c *Coordinator) RecordContextCompactionLocked(requestID string, compaction model.ContextCompaction) bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._RecordContextCompactionLocked(requestID, compaction)
}

func (c *Coordinator) _RecordContextCompactionLocked(requestID string, compaction model.ContextCompaction) bool {
	st := c.sessionForRequestLocked(requestID)
	if st == nil {
		st = c.activeSessionLocked()
	}
	state := st.taskExecution
	if state == nil || state.RequestID != requestID || state.Status != StatusRunning {
		return false
	}
	state.ContextCompactions = append(state.ContextCompactions, compaction)
	if task := c.Snapshot.Task; task != nil && task.RequestID == requestID {
		task.ContextCompactions = append([]model.ContextCompaction(nil), state.ContextCompactions...)
		task.UpdatedAt = compaction.CompactedAt
	}
	return true
}

// SetTaskStateLocked 把任务可见状态写入快照（调用方持有 Core.ViewMu；requestID
// 反查会话）。非活跃会话（后台并行执行）跳过共享快照写入，避免污染活跃
// 会话投影；任务内部状态由调用方独立维护。
func (c *Coordinator) SetTaskStateLocked(requestID string, status model.TaskStatus, summary string) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._SetTaskStateLocked(requestID, status, summary)
}

func (c *Coordinator) _SetTaskStateLocked(requestID string, status model.TaskStatus, summary string) {
	sessionID := c._SessionIDForRequest(requestID)
	if !c.isActiveSessionLocked(sessionID) {
		return
	}
	st := c.sessionStateLocked(sessionID)
	var compactions []model.ContextCompaction
	if taskState := st.taskExecution; taskState != nil && taskState.RequestID == requestID {
		compactions = append([]model.ContextCompaction(nil), taskState.ContextCompactions...)
	}
	c.Snapshot.Task = &model.TaskState{
		RequestID:          requestID,
		Status:             status,
		Summary:            strings.TrimSpace(summary),
		ContextCompactions: compactions,
		UpdatedAt:          time.Now(),
	}
}

// isActiveSessionLocked 判定会话是否为共享快照归属会话（调用方持有
// Core.ViewMu）。
func (c *Coordinator) isActiveSessionLocked(sessionID string) bool {
	return sessionID == c.Snapshot.Session.ID
}

// InterruptTaskLocked 把任务置为中断（快照 + 内部状态；调用方持有 Core.ViewMu）。
func (c *Coordinator) InterruptTaskLocked(requestID, summary string) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._InterruptTaskLocked(requestID, summary)
}

func (c *Coordinator) _InterruptTaskLocked(requestID, summary string) {
	c._SetTaskStateLocked(requestID, model.TaskInterrupted, summary)
	st := c.sessionForRequestLocked(requestID)
	if st == nil {
		st = c.activeSessionLocked()
	}
	if state := st.taskExecution; state != nil && state.RequestID == requestID {
		state.Status = StatusInterrupted
	}
}

// FailTaskLocked 把任务置为失败（快照 + 内部状态；调用方持有 Core.ViewMu）。
func (c *Coordinator) FailTaskLocked(requestID, summary string) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._FailTaskLocked(requestID, summary)
}

func (c *Coordinator) _FailTaskLocked(requestID, summary string) {
	c._SetTaskStateLocked(requestID, model.TaskFailed, summary)
	st := c.sessionForRequestLocked(requestID)
	if st == nil {
		st = c.activeSessionLocked()
	}
	if state := st.taskExecution; state != nil && state.RequestID == requestID {
		state.Status = StatusFailed
	}
}

// ResumeTaskLocked 恢复被压缩/中断的任务（快照 + 内部状态 + epoch 推进；
// 调用方持有 Core.ViewMu）。
func (c *Coordinator) ResumeTaskLocked(requestID, summary string) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._ResumeTaskLocked(requestID, summary)
}

func (c *Coordinator) _ResumeTaskLocked(requestID, summary string) {
	st := c.sessionForRequestLocked(requestID)
	if st == nil {
		st = c.activeSessionLocked()
	}
	state := st.taskExecution
	if state == nil || state.RequestID != requestID {
		return
	}
	state.Status = StatusRunning
	state.ProgressEpoch++
	c._SetTaskStateLocked(requestID, model.TaskProgressing, summary)
}

// RememberCheckpointLocked 按 version 替换或追加活跃会话 checkpoint（调用
// 方持有 Core.ViewMu）。
func (c *Coordinator) RememberCheckpointLocked(checkpoint model.TaskCheckpoint) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._RememberCheckpointLocked(checkpoint)
}

func (c *Coordinator) _RememberCheckpointLocked(checkpoint model.TaskCheckpoint) {
	st := c.activeSessionLocked()
	for index := range st.taskCheckpoints {
		if st.taskCheckpoints[index].Version == checkpoint.Version && checkpoint.Version != 0 {
			st.taskCheckpoints[index] = checkpoint
			return
		}
	}
	st.taskCheckpoints = append(st.taskCheckpoints, checkpoint)
}

// BeginTask 为活跃会话当前请求创建任务执行状态与 TaskService（调用方持有
// Core.ViewMu）。
func (c *Coordinator) BeginTask(requestID, objective, effort string, previous *TaskExecutionState, checkpoint model.TaskCheckpoint) *TaskExecutionState {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._BeginTask(requestID, objective, effort, previous, checkpoint)
}

func (c *Coordinator) _BeginTask(requestID, objective, effort string, previous *TaskExecutionState, checkpoint model.TaskCheckpoint) *TaskExecutionState {
	return c._BeginTaskFor(c.activeSessionIDLocked(), requestID, objective, effort, previous, checkpoint)
}

// BeginTaskFor 为指定会话当前请求创建任务执行状态与 TaskService（调用方
// 持有 Core.ViewMu）。
func (c *Coordinator) BeginTaskFor(sessionID, requestID, objective, effort string, previous *TaskExecutionState, checkpoint model.TaskCheckpoint) *TaskExecutionState {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._BeginTaskFor(sessionID, requestID, objective, effort, previous, checkpoint)
}

func (c *Coordinator) _BeginTaskFor(sessionID, requestID, objective, effort string, previous *TaskExecutionState, checkpoint model.TaskCheckpoint) *TaskExecutionState {
	st := c.sessionStateLocked(sessionID)
	state := continuationTaskExecutionState(requestID, objective, effort, previous, checkpoint)
	st.taskExecution = state
	st.taskService = newTaskService(sessionID, c, state, c.queuedInputRefs)
	c.bindRequestLocked(requestID, sessionID)
	return state
}

// ContinuationSummary 返回活跃会话当前任务的恢复摘要（requestID 不匹配 →
// ""）。
func (c *Coordinator) ContinuationSummary(requestID string) string {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._ContinuationSummary(requestID)
}

func (c *Coordinator) _ContinuationSummary(requestID string) string {
	st := c.activeSessionLocked()
	state := st.taskExecution
	if state == nil || state.RequestID != requestID {
		return ""
	}
	return state.ContextSummary()
}

// ContinuationSummaryFor 返回指定会话当前任务的恢复摘要（requestID 不匹配
// → ""）。会话归档/恢复路径用（后台会话收尾不得读活跃会话摘要）。
func (c *Coordinator) ContinuationSummaryFor(sessionID, requestID string) string {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._ContinuationSummaryFor(sessionID, requestID)
}

func (c *Coordinator) _ContinuationSummaryFor(sessionID, requestID string) string {
	st := c.sessionStateLocked(sessionID)
	state := st.taskExecution
	if state == nil || state.RequestID != requestID {
		return ""
	}
	return state.ContextSummary()
}

// Transcript 返回活跃会话 append-only 事件。
func (c *Coordinator) Transcript() []model.TranscriptEvent {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._Transcript(

	// TranscriptFor 返回指定会话 append-only 事件。
	)
}

func (c *Coordinator) _Transcript() []model.TranscriptEvent {
	return c.activeSessionLocked().transcript
}

func (c *Coordinator) TranscriptFor(sessionID string) []model.TranscriptEvent {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._TranscriptFor(

		// PendingToolResults 返回活跃会话尚未随会话原子提交的工具结果。
		sessionID)
}

func (c *Coordinator) _TranscriptFor(sessionID string) []model.TranscriptEvent {
	return c.sessionStateLocked(sessionID).transcript
}

func (c *Coordinator) PendingToolResults() []model.StoredToolResult {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._PendingToolResults(

	// PendingToolResultsFor 返回指定会话尚未随会话原子提交的工具结果。
	)
}

func (c *Coordinator) _PendingToolResults() []model.StoredToolResult {
	return c.activeSessionLocked().pendingToolResults
}

func (c *Coordinator) PendingToolResultsFor(sessionID string) []model.StoredToolResult {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._PendingToolResultsFor(

		// TaskCheckpoints 返回活跃会话任务 checkpoint 序列。
		sessionID)
}

func (c *Coordinator) _PendingToolResultsFor(sessionID string) []model.StoredToolResult {
	return c.sessionStateLocked(sessionID).pendingToolResults
}

func (c *Coordinator) TaskCheckpoints() []model.TaskCheckpoint {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._TaskCheckpoints(

	// TaskCheckpointsFor 返回指定会话任务 checkpoint 序列。
	)
}

func (c *Coordinator) _TaskCheckpoints() []model.TaskCheckpoint {
	return c.activeSessionLocked().taskCheckpoints
}

func (c *Coordinator) TaskCheckpointsFor(sessionID string) []model.TaskCheckpoint {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._TaskCheckpointsFor(

		// ToolResultRefs 返回活跃会话工具结果引用表。
		sessionID)
}

func (c *Coordinator) _TaskCheckpointsFor(sessionID string) []model.TaskCheckpoint {
	return c.sessionStateLocked(sessionID).taskCheckpoints
}

func (c *Coordinator) ToolResultRefs() []model.ToolResultRef {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._ToolResultRefs(

	// ToolResultRefsFor 返回指定会话工具结果引用表。
	)
}

func (c *Coordinator) _ToolResultRefs() []model.ToolResultRef {
	return c.activeSessionLocked().toolResultRefs
}

func (c *Coordinator) ToolResultRefsFor(sessionID string) []model.ToolResultRef {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._ToolResultRefsFor(

		// ToolResultRefByCallID 按工具调用 ID 查活跃会话结果引用（未找到 → ""）。
		sessionID)
}

func (c *Coordinator) _ToolResultRefsFor(sessionID string) []model.ToolResultRef {
	return c.sessionStateLocked(sessionID).toolResultRefs
}

func (c *Coordinator) ToolResultRefByCallID(callID string) string {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._ToolResultRefByCallID(

		// ToolResultRefByCallIDFor 按工具调用 ID 查指定会话结果引用（未找到 → ""）。
		callID)
}

func (c *Coordinator) _ToolResultRefByCallID(callID string) string {
	return c.activeSessionLocked().resultRefsByToolCallID[callID]
}

func (c *Coordinator) ToolResultRefByCallIDFor(sessionID, callID string) string {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._ToolResultRefByCallIDFor(sessionID,

		// CurrentRequestIDFor 返回指定会话当前任务的请求 ID（无任务 → ""）。
		callID)
}

func (c *Coordinator) _ToolResultRefByCallIDFor(sessionID, callID string) string {
	return c.sessionStateLocked(sessionID).resultRefsByToolCallID[callID]
}

func (c *Coordinator) CurrentRequestIDFor(sessionID string) string {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._CurrentRequestIDFor(sessionID)
}

func (c *Coordinator) _CurrentRequestIDFor(sessionID string) string {
	st := c.sessionStateLocked(sessionID)
	if st.taskExecution == nil {
		return ""
	}
	return st.taskExecution.RequestID
}

// TaskStateFor 返回指定会话当前任务的可见状态（会话归档用；后台会话收尾
// 不得读全局 Snapshot.Task——对应 L5/Execution.Task 串写修复）。
func (c *Coordinator) TaskStateFor(sessionID string) *model.TaskState {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._TaskStateFor(sessionID)
}

func (c *Coordinator) _TaskStateFor(sessionID string) *model.TaskState {
	st := c.sessionStateLocked(sessionID)
	state := st.taskExecution
	if state == nil {
		return nil
	}
	status := model.TaskStatus(state.Status)
	if status == model.TaskStatus("running") {
		status = model.TaskProgressing
	}
	return &model.TaskState{
		RequestID:          state.RequestID,
		Status:             status,
		ContextCompactions: append([]model.ContextCompaction(nil), state.ContextCompactions...),
		UpdatedAt:          time.Now(),
	}
}

// VisibleTaskStateFor 返回指定会话当前任务的**可见**状态（镜像收口用）：
// 优先取 TaskService 最近一次落地值（含 summary/decision 等展示字段），
// 任务已推进到新请求时回退 TaskStateFor 的构造值。调用方持有 Core.ViewMu。
func (c *Coordinator) VisibleTaskStateFor(sessionID string) *model.TaskState {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._VisibleTaskStateFor(sessionID)
}

func (c *Coordinator) _VisibleTaskStateFor(sessionID string) *model.TaskState {
	st := c.sessionStateLocked(sessionID)
	state := st.taskExecution
	if state == nil {
		return nil
	}
	if ts := st.taskService; ts != nil && ts.lastTaskState != nil &&
		ts.lastTaskState.RequestID == state.RequestID && ts.state == state {
		visible := *ts.lastTaskState
		return &visible
	}
	return c._TaskStateFor(sessionID)
}

// ResultRefsByCallID 返回活跃会话 callID → resultRef 全量拷贝（上下文拒绝
// 路径）。
func (c *Coordinator) ResultRefsByCallID() map[string]string {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._ResultRefsByCallID()
}

func (c *Coordinator) _ResultRefsByCallID() map[string]string {
	refs := make(map[string]string, len(c.activeSessionLocked().resultRefsByToolCallID))
	for callID, resultRef := range c.activeSessionLocked().resultRefsByToolCallID {
		refs[callID] = resultRef
	}
	return refs
}

// ResultRefsByCallIDFor 返回指定会话 callID → resultRef 全量拷贝。
func (c *Coordinator) ResultRefsByCallIDFor(sessionID string) map[string]string {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._ResultRefsByCallIDFor(sessionID)
}

func (c *Coordinator) _ResultRefsByCallIDFor(sessionID string) map[string]string {
	refs := make(map[string]string, len(c.sessionStateLocked(sessionID).resultRefsByToolCallID))
	for callID, resultRef := range c.sessionStateLocked(sessionID).resultRefsByToolCallID {
		refs[callID] = resultRef
	}
	return refs
}

// PlanStackFor 返回指定会话 plan 帧栈。
func (c *Coordinator) PlanStackFor(sessionID string) []model.SessionPlanFrame {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._PlanStackFor(

		// SessionIDForRequest 按 requestID 反查会话 ID（未绑定 → 活跃会话）。
		sessionID)
}

func (c *Coordinator) _PlanStackFor(sessionID string) []model.SessionPlanFrame {
	return c.sessionStateLocked(sessionID).planStack
}

func (c *Coordinator) SessionIDForRequest(requestID string) string {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._SessionIDForRequest(requestID)
}

func (c *Coordinator) _SessionIDForRequest(requestID string) string {
	if requestID == "" {
		if c.currentSessionID != nil {
			return c.currentSessionID()
		}
		return c.activeSessionIDLocked()
	}
	c.requestMu.RLock()
	if sessionID, ok := c.requestToSession[requestID]; ok {
		c.requestMu.RUnlock()
		return sessionID
	}
	c.requestMu.RUnlock()
	if c.currentSessionID != nil {
		return c.currentSessionID()
	}
	return c.activeSessionIDLocked()
}

// ActivePlanProjectionLockedFor 返回指定会话激活 Plan 的只读投影（调用方
// 持有 Core.ViewMu）。
func (c *Coordinator) ActivePlanProjectionLockedFor(sessionID string) *model.ActivePlanProjection {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._ActivePlanProjectionLockedFor(sessionID)
}

func (c *Coordinator) _ActivePlanProjectionLockedFor(sessionID string) *model.ActivePlanProjection {
	st := c.sessionStateLocked(sessionID)
	return ActivePlanProjection(c.Snapshot.Runtime.Plan, st.activePlanID, st.planSequence)
}

// SyncActivePlanFrameLocked 把当前快照 Plan 收敛进活跃会话激活帧（调用方
// 持有 Core.ViewMu）。
func (c *Coordinator) SyncActivePlanFrameLocked(now time.Time) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._SyncActivePlanFrameLocked(now)
}

func (c *Coordinator) _SyncActivePlanFrameLocked(now time.Time) {
	st := c.activeSessionLocked()
	if st.activePlanID == "" || len(st.planStack) == 0 {
		return
	}
	c.syncActivePlanFrameLocked(st, now)
}

// SyncActivePlanFrameLockedFor 把当前快照 Plan 收敛进指定会话激活帧（调用
// 方持有 Core.ViewMu）。会话归档/恢复路径用（后台会话收尾不得清活跃帧）。
// 遗留风险（P6）：Plan 投影仍来自全局 Snapshot.Runtime.Plan——阶段 1
// SessionScope 收口前，plan 投影尚未按会话隔离。
func (c *Coordinator) SyncActivePlanFrameLockedFor(sessionID string, now time.Time) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._SyncActivePlanFrameLockedFor(sessionID, now)
}

func (c *Coordinator) _SyncActivePlanFrameLockedFor(sessionID string, now time.Time) {
	st := c.sessionStateLocked(sessionID)
	if st.activePlanID == "" || len(st.planStack) == 0 {
		return
	}
	c.syncActivePlanFrameLocked(st, now)
}

func (c *Coordinator) syncActivePlanFrameLocked(st *sessionTaskRuntime, now time.Time) {
	if st.activePlanID == "" || len(st.planStack) == 0 {
		return
	}
	for index := range st.planStack {
		frame := &st.planStack[index]
		if frame.ID != st.activePlanID {
			continue
		}
		frame.Plan = model.CloneRuntimeState(model.RuntimeState{Plan: c.Snapshot.Runtime.Plan}).Plan
		frame.UpdatedAt = now
		return
	}
}

// PushLoadedPlanLocked 把 plan_load 参数追加为活跃会话新的激活帧（调用方
// 持有 Core.ViewMu）。
func (c *Coordinator) PushLoadedPlanLocked(arguments string, now time.Time) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._PushLoadedPlanLocked(arguments, now)
}

func (c *Coordinator) _PushLoadedPlanLocked(arguments string, now time.Time) {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" || c.Snapshot.Runtime.Plan == nil {
		return
	}
	st := c.activeSessionLocked()
	st.planSequence++
	planID := fmt.Sprintf("plan-%d", st.planSequence)
	st.planStack = append(st.planStack, model.SessionPlanFrame{
		ID:        planID,
		Plan:      model.CloneRuntimeState(model.RuntimeState{Plan: c.Snapshot.Runtime.Plan}).Plan,
		Arguments: arguments,
		LoadedAt:  now,
		UpdatedAt: now,
	})
	st.activePlanID = planID
}

// RemoveCommittedToolResultsLocked 清理活跃会话已随会话快照提交的待定工具
// 结果。
func (c *Coordinator) RemoveCommittedToolResultsLocked(committed []model.StoredToolResult) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._RemoveCommittedToolResultsLocked(committed)
}

func (c *Coordinator) _RemoveCommittedToolResultsLocked(committed []model.StoredToolResult) {
	st := c.activeSessionLocked()
	c.removeCommittedToolResultsLocked(st, committed)
}

// RemoveCommittedToolResultsForLocked 清理指定会话已随会话快照提交的待定
// 工具结果。
func (c *Coordinator) RemoveCommittedToolResultsForLocked(sessionID string, committed []model.StoredToolResult) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._RemoveCommittedToolResultsForLocked(sessionID, committed)
}

func (c *Coordinator) _RemoveCommittedToolResultsForLocked(sessionID string, committed []model.StoredToolResult) {
	st := c.sessionStateLocked(sessionID)
	c.removeCommittedToolResultsLocked(st, committed)
}

func (c *Coordinator) removeCommittedToolResultsLocked(st *sessionTaskRuntime, committed []model.StoredToolResult) {
	if len(committed) == 0 || len(st.pendingToolResults) == 0 {
		return
	}
	refs := make(map[string]struct{}, len(committed))
	for _, result := range committed {
		refs[result.Ref] = struct{}{}
	}
	pending := st.pendingToolResults[:0]
	for _, result := range st.pendingToolResults {
		if _, ok := refs[result.Ref]; !ok {
			pending = append(pending, result)
		}
	}
	st.pendingToolResults = pending
}

// UnloadSessionState 释放指定会话的任务/plan 运行时状态（阶段 2 生命周期；
// 调用方持有 Core.ViewMu）。unload 后重开走 cold_load。
func (c *Coordinator) UnloadSessionState(sessionID string) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._UnloadSessionState(sessionID)
}

func (c *Coordinator) _UnloadSessionState(sessionID string) {
	delete(c.sessionStates, sessionID)
	for requestID, sid := range c.requestToSession {
		if sid == sessionID {
			delete(c.requestToSession, requestID)
		}
	}
}

// TokenCounterName 返回当前 token 计数器标识。
func (c *Coordinator) TokenCounterName() string {
	c.stateMu.Lock()
	defer c.stateMu.Unlock(

	// CountRequestTokens 估算一次完整请求 token 数。
	)
	return c._TokenCounterName()
}

func (c *Coordinator) _TokenCounterName() string {
	return c.tokenCounter.Name()
}

func (c *Coordinator) CountRequestTokens(systemPrompt string, history []contract.EngineMessage, currentInput string, tools []model.Tool) int {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._CountRequestTokens(systemPrompt, history,

		// CountTextTokens 估算文本 token 数。
		currentInput, tools)
}

func (c *Coordinator) _CountRequestTokens(systemPrompt string, history []contract.EngineMessage, currentInput string, tools []model.Tool) int {
	return c.tokenCounter.CountRequest(systemPrompt, history, currentInput, tools)
}

func (c *Coordinator) CountTextTokens(value string) int {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.

		// VerifyAndApply 是终态/打点工具的入口（Registry handler 面）。ctx 携带
		// 会话 ID 时路由到对应会话，否则按活跃会话。
		_CountTextTokens(value)
}

func (c *Coordinator) _CountTextTokens(value string) int {
	return c.tokenCounter.CountText(value)
}

func (c *Coordinator) VerifyAndApply(ctx context.Context, kind, argsJSON string) (string, error) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._VerifyAndApply(ctx, kind, argsJSON)
}

func (c *Coordinator) _VerifyAndApply(ctx context.Context, kind, argsJSON string) (string, error) {
	ts := c.taskServiceForContextLocked(ctx)
	return ts.VerifyAndApply(ctx, kind, argsJSON)
}

// FinalizeTask 把自然停止转换为可审计完成/交接（OnChatEnd 入口；summary
// requestID 反查会话）。
func (c *Coordinator) FinalizeTask(ctx context.Context, summary ChatEndSummary) error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._FinalizeTask(ctx, summary)
}

func (c *Coordinator) _FinalizeTask(ctx context.Context, summary ChatEndSummary) error {
	ts := c.taskServiceForRequestLocked(summary.RequestID)
	_, err := ts.OnChatEnd(ctx, summary)
	return err
}

// OnChatEnd 把自然停止转换为可审计完成/交接，返回可见任务状态。
func (c *Coordinator) OnChatEnd(ctx context.Context, summary ChatEndSummary) (model.TaskState, error) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._OnChatEnd(ctx, summary)
}

func (c *Coordinator) _OnChatEnd(ctx context.Context, summary ChatEndSummary) (model.TaskState, error) {
	ts := c.taskServiceForRequestLocked(summary.RequestID)
	return ts.OnChatEnd(ctx, summary)
}

// CurrentTaskResumeRecord 返回活跃会话当前任务的终态恢复记录。
func (c *Coordinator) CurrentTaskResumeRecord() TaskResumeRecord {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._CurrentTaskResumeRecord(

	// SetTaskProjectionFlushLocked 注入活跃会话 TaskService 的 Plan 投影 flush
	// 钩子（测试模拟延迟投影；调用方持有 Core.ViewMu）。
	)
}

func (c *Coordinator) _CurrentTaskResumeRecord() TaskResumeRecord {
	return c.currentTaskService().ResumeRecord()
}

func (c *Coordinator) SetTaskProjectionFlushLocked(flush func(context.Context) error) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._SetTaskProjectionFlushLocked(flush)
}

func (c *Coordinator) _SetTaskProjectionFlushLocked(flush func(context.Context) error) {
	if ts := c.activeSessionLocked().taskService; ts != nil {
		ts.projection = &planProjectionReader{c: c, flush: flush}
	}
}

// ObserveTool 记录工具执行观测（调用方持有 Core.ViewMu；observation requestID
// 反查会话）。
func (c *Coordinator) ObserveTool(observation ToolObservation) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._ObserveTool(observation)
}

func (c *Coordinator) _ObserveTool(observation ToolObservation) {
	if st := c.sessionForRequestLocked(observation.RequestID); st != nil {
		ts := st.taskService
		state := st.taskExecution
		if ts == nil || ts.state != state {
			ts = newTaskService(c._SessionIDForRequest(observation.RequestID), c, state, c.queuedInputRefs)
		}
		ts.ObserveTool(observation)
		return
	}
	c.currentTaskServiceLocked().ObserveTool(observation)
}

// ObservePlanEvent 记录 plan 事件投影观测（调用方持有 Core.ViewMu；活跃会话）。
func (c *Coordinator) ObservePlanEvent(event PlanEvent) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c._ObservePlanEvent(event)

	// ObserveModelOutput 记录模型回复观测（自行加锁；output requestID 反查
	// 会话）。
}

func (c *Coordinator) _ObservePlanEvent(event PlanEvent) {
	c.currentTaskServiceLocked().ObservePlanEvent(event)
}

func (c *Coordinator) ObserveModelOutput(ctx context.Context, output ModelOutput) error {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c._ObserveModelOutput(ctx, output)
}

func (c *Coordinator) _ObserveModelOutput(ctx context.Context, output ModelOutput) error {
	ts := c.taskServiceForRequestLocked(output.RequestID)
	return ts.ObserveModelOutput(ctx, output)
}

// taskServiceForContextLocked 按 ctx 会话 ID 返回任务的 TaskService（未注入
// → 活跃会话；调用方持有 Core.ViewMu）。
func (c *Coordinator) taskServiceForContextLocked(ctx context.Context) *TaskService {
	if sessionID := SessionIDFromContext(ctx); sessionID != "" {
		return c.currentTaskServiceForLocked(sessionID)
	}
	return c.currentTaskServiceForLocked(c.activeSessionIDLocked())
}
