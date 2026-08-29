package context_runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/internal/limits"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

const (
	TaskContextCheckpointPrefix = "<!-- seelex:context-checkpoint:v1 -->"
	planContextPrefix           = "<!-- seelex:active-plan:v1 -->"
	ToolResultOmittedPrefix     = "<seelex-tool-result-omitted>"
)

// ErrProviderContextBudgetExceeded 标记 provider 上下文超过安全 token 预算。
var ErrProviderContextBudgetExceeded = errors.New("provider context exceeds the safe token budget")

// Coordinator 拥有 provider 上下文装配与控制（token 预算/压缩/result-ref/
// 可恢复中断）。
type Coordinator struct {
	*state.Core
	tasks     TaskPort
	sessions  SessionPort
	prompts   PromptPort
	view      ViewPort
	history   HistoryPort
	workTable func() string
}

// NewCoordinator 构造 context 域协调器。
func NewCoordinator(deps Deps) *Coordinator {
	return &Coordinator{
		Core:      deps.Core,
		tasks:     deps.Tasks,
		sessions:  deps.Sessions,
		prompts:   deps.Prompts,
		view:      deps.View,
		history:   deps.History,
		workTable: deps.WorkTableTraceBlock,
	}
}

// Ports 是装配端口图的只读快照（组装校验/诊断用）。
func (c *Coordinator) Ports() Ports {
	return Ports{
		Tasks: c.tasks, Sessions: c.sessions, Prompts: c.prompts,
		View: c.view, History: c.history,
	}
}

// Ports 描述 context 域的协作端口。
type Ports struct {
	Tasks    TaskPort
	Sessions SessionPort
	Prompts  PromptPort
	View     ViewPort
	History  HistoryPort
}

// CompactTaskContext 把整个可变 transcript 替换为一个私有、有界的 checkpoint
// （引擎迭代 hook 调用，绝不持有 Core.Mu；活跃会话兼容包装）。
func (c *Coordinator) CompactTaskContext(requestID string) error {
	return c.CompactTaskContextFor(c.tasks.SessionIDForRequest(requestID), requestID)
}

// CompactTaskContextFor 把指定会话整个可变 transcript 替换为一个私有、有界
// 的 checkpoint（引擎迭代 hook 调用，绝不持有 Core.Mu）。
func (c *Coordinator) CompactTaskContextFor(sessionID, requestID string) error {
	_, err := c.PrepareExecutionContextFor(sessionID, requestID, "")
	if err != nil {
		return err
	}
	if err := c.sessions.PersistCurrentSession(sessionID); err != nil {
		return fmt.Errorf("persist context checkpoint: %w", err)
	}
	return nil
}

// PrepareExecutionContext 从 durable task 状态与完整 transcript 单元重建
// provider 缓存（活跃会话兼容包装）。返回可能被引用的当前输入；仍超安全
// 预算时拒绝发送。
func (c *Coordinator) PrepareExecutionContext(requestID, currentInput string) (string, error) {
	return c.PrepareExecutionContextFor(c.tasks.SessionIDForRequest(requestID), requestID, currentInput)
}

// PrepareExecutionContextFor 从 durable task 状态与完整 transcript 单元重建
// 指定会话 provider 缓存。返回可能被引用的当前输入；仍超安全预算时拒绝
// 发送。sessionID 指明执行会话（多会话并行时目标会话）。
func (c *Coordinator) PrepareExecutionContextFor(sessionID, requestID, currentInput string) (string, error) {
	if _, err := c.rejectOversizedToolResults(sessionID, task_context.DefaultToolResultLimit()); err != nil {
		return "", err
	}
	// 工作打点表：请求尾部的只读标记块（system 前缀保持不变 → 缓存友好；
	// 无活动任务时块为空 → 自动删除；不落历史 → 不参与压缩）。
	if block := c.workTable(); block != "" {
		if currentInput == "" {
			currentInput = block
		} else {
			currentInput = block + "\n\n" + currentInput
		}
	}
	budget := task_context.ContextBudgetFor(c.Deps.Runtime)
	tools := c.Deps.Runtime.VisibleTools(context.Background())
	existing := c.engineHistory(sessionID)
	c.Mu.RLock()
	systemPrompt := c.prompts.SystemPromptForActiveTaskLockedFor(sessionID)
	c.Mu.RUnlock()
	c.setEngineSystemPrompt(sessionID, systemPrompt)
	rawTokens := c.tasks.CountRequestTokens(systemPrompt, existing, currentInput, tools)

	runtimeModel := c.Deps.Runtime.Model()
	c.Mu.Lock()
	state := c.tasks.CurrentTaskExecutionFor(sessionID)
	if state == nil || state.RequestID != requestID {
		c.Mu.Unlock()
		return currentInput, nil
	}
	currentInput = c.protectOversizedCurrentInputLocked(sessionID, requestID, currentInput, budget)
	newCheckpoint := rawTokens >= budget.SoftThreshold && state.CompactedEpoch != state.ProgressEpoch
	if newCheckpoint {
		state.ContextVersion++
		state.CompactedEpoch = state.ProgressEpoch
	}
	checkpoint := c.tasks.BuildTaskCheckpointLocked(state)
	checkpoint.Version = state.ContextVersion
	planMessage := c.planContextMessageLocked(sessionID)
	checkpointMessage := checkpointContextMessage(checkpoint, rawTokens >= budget.HardThreshold)
	events := append([]model.TranscriptEvent(nil), c.tasks.TranscriptFor(sessionID)...)
	events = excludeCurrentInputEvent(events, requestID, currentInput)
	c.Mu.Unlock()

	systems := RetainedSystemHistory(c.engineHistory(sessionID))
	target := budget.Budget
	if rawTokens >= budget.SoftThreshold {
		target = budget.TargetAfterCompaction
	}
	assembled, estimated := c.fitExecutionHistory(systemPrompt, systems, planMessage, checkpointMessage, events, currentInput, tools, target)
	if estimated > budget.Budget {
		checkpointMessage = checkpointContextMessage(checkpoint, true)
		assembled, estimated = c.fitExecutionHistory(systemPrompt, systems, planMessage, checkpointMessage, nil, currentInput, tools, budget.Budget)
	}
	if estimated > budget.Budget {
		return "", fmt.Errorf("%w: estimated=%d budget=%d", ErrProviderContextBudgetExceeded, estimated, budget.Budget)
	}
	if err := c.replaceEngineHistory(sessionID, assembled); err != nil {
		return "", fmt.Errorf("assemble provider context: %w", err)
	}
	if err := c.history.PrepareProviderHistoryFor(sessionID); err != nil {
		return "", err
	}

	c.Mu.Lock()
	state = c.tasks.CurrentTaskExecutionFor(sessionID)
	recorded := false
	var revision uint64
	if state != nil && state.RequestID == requestID {
		state.TokenAudit = model.TokenAudit{
			Model: runtimeModel, Counter: c.tasks.TokenCounterName(),
			Budget: budget.Budget, SoftThreshold: budget.SoftThreshold, HardThreshold: budget.HardThreshold,
			TargetAfterCompaction: budget.TargetAfterCompaction, EstimatedPromptTokens: estimated,
			ActualPromptTokens: state.TokenAudit.ActualPromptTokens, UpdatedAt: time.Now(),
		}
		if newCheckpoint {
			c.tasks.RememberCheckpointLocked(checkpoint)
			recorded = c.tasks.RecordContextCompactionLocked(requestID, model.ContextCompaction{
				Version: checkpoint.Version, Reason: "context_budget", MessagesBefore: len(existing),
				EstimatedTokens: rawTokens, CompactedAt: time.Now(),
			})
			if recorded {
				revision = c.view.BumpLocked()
			}
		}
	}
	c.Mu.Unlock()
	if recorded {
		c.Events.Publish(event.EventSnapshotChanged, revision, requestID, nil)
	}
	return currentInput, nil
}

func (c *Coordinator) fitExecutionHistory(
	systemPrompt string,
	systems []contract.EngineMessage,
	planMessage, checkpointMessage string,
	events []model.TranscriptEvent,
	currentInput string,
	tools []model.Tool,
	target int,
) ([]contract.EngineMessage, int) {
	for maxUnits := limits.Get().ContextMaxUnits; maxUnits >= 0; maxUnits-- { // limits.context_max_units（默认 4）
		history := append([]contract.EngineMessage(nil), systems...)
		if planMessage != "" {
			history = append(history, contract.EngineMessage{Role: "user", Content: planMessage, ContentSet: true})
		}
		if checkpointMessage != "" {
			history = append(history, contract.EngineMessage{Role: "user", Content: checkpointMessage, ContentSet: true})
		}
		if maxUnits > 0 {
			history = append(history, task_context.TranscriptTailHistory(events, target, maxUnits)...)
		}
		estimated := c.tasks.CountRequestTokens(systemPrompt, history, currentInput, tools)
		if estimated <= target {
			return history, estimated
		}
	}
	history := append([]contract.EngineMessage(nil), systems...)
	if planMessage != "" {
		history = append(history, contract.EngineMessage{Role: "user", Content: planMessage, ContentSet: true})
	}
	if checkpointMessage != "" {
		history = append(history, contract.EngineMessage{Role: "user", Content: checkpointMessage, ContentSet: true})
	}
	return history, c.tasks.CountRequestTokens(systemPrompt, history, currentInput, tools)
}

func (c *Coordinator) planContextMessageLocked(sessionID string) string {
	projection := c.tasks.ActivePlanProjectionLockedFor(sessionID)
	if projection == nil || projection.Status == string(model.PlanCompleted) {
		return ""
	}
	payload := map[string]any{
		"plan_ref": projection.CanonicalPlanRef, "plan_id": projection.PlanID,
		"status": projection.Status, "current": projection.CurrentNode,
		"completed": projection.CompletedNodes, "failed": projection.FailedNodes,
		"pending": projection.PendingNodes,
	}
	if frame := task_context.ActivePlanFrame(c.tasks.PlanStackFor(sessionID), c.tasks.ActivePlanIDFor(sessionID)); frame != nil {
		payload["current_slice"] = currentPlanSlice(frame.Arguments, projection.CurrentNode)
	}
	encoded, _ := json.Marshal(payload)
	return planContextPrefix + "\n" + string(encoded)
}

func currentPlanSlice(arguments, currentNode string) any {
	var plan struct {
		Nodes map[string]json.RawMessage `json:"nodes"`
		Edges map[string][]string        `json:"edges"`
	}
	if json.Unmarshal([]byte(arguments), &plan) != nil {
		return nil
	}
	nodes := make(map[string]json.RawMessage)
	edges := make(map[string][]string)
	if node, ok := plan.Nodes[currentNode]; ok {
		nodes[currentNode] = node
	}
	for source, targets := range plan.Edges {
		if source == currentNode {
			edges[source] = append([]string(nil), targets...)
			for _, target := range targets {
				if node, ok := plan.Nodes[target]; ok {
					nodes[target] = node
				}
			}
		}
		for _, target := range targets {
			if target == currentNode {
				edges[source] = task_context.AppendUniqueStrings(edges[source], target)
				if node, ok := plan.Nodes[source]; ok {
					nodes[source] = node
				}
			}
		}
	}
	return map[string]any{"nodes": nodes, "edges": edges}
}

func checkpointContextMessage(checkpoint model.TaskCheckpoint, minimal bool) string {
	if !task_context.HasSubstantiveCheckpoint(checkpoint) {
		return ""
	}
	if minimal {
		checkpoint.Decisions = nil
		checkpoint.ChangedFiles = nil
		checkpoint.Artifacts = nil
		if len(checkpoint.CompletedWork) > 1 {
			checkpoint.CompletedWork = checkpoint.CompletedWork[len(checkpoint.CompletedWork)-1:]
		}
	}
	encoded, _ := json.Marshal(checkpoint)
	return TaskContextCheckpointPrefix + "\n" + string(encoded)
}

func excludeCurrentInputEvent(events []model.TranscriptEvent, requestID, currentInput string) []model.TranscriptEvent {
	if currentInput == "" || len(events) == 0 {
		return events
	}
	last := events[len(events)-1]
	if last.TaskID == requestID && last.Role == "user" {
		return events[:len(events)-1]
	}
	return events
}

func (c *Coordinator) protectOversizedCurrentInputLocked(sessionID, requestID, currentInput string, budget task_context.ContextBudget) string {
	if currentInput == "" || c.tasks.CountTextTokens(currentInput) <= budget.TargetAfterCompaction/2 {
		return currentInput
	}
	stored := c.tasks.StoreToolResultForLocked(sessionID, "user_input", currentInput)
	warning := ContentReferenceWarning(stored.Ref)
	transcript := c.tasks.TranscriptFor(sessionID)
	for index := len(transcript) - 1; index >= 0; index-- {
		event := &transcript[index]
		if event.TaskID == requestID && event.Role == "user" {
			event.Content = warning
			event.ResultRef = stored.Ref
			event.TokenCount = c.tasks.CountTranscriptEvent(*event)
			break
		}
	}
	return warning
}

// ContentReferenceWarning 是超限用户输入归档引用警告文本。
func ContentReferenceWarning(resultRef string) string {
	return "<seelex-content-reference>\nresult_ref=" + resultRef + "\n" +
		"The user input is stored out of band because it exceeds the single-item context budget. " +
		"Use read_tool_result with pagination or filtering.\n</seelex-content-reference>"
}

// FrameworkToolOutputTruncatedMarker 是框架截断工具输出标记。
const FrameworkToolOutputTruncatedMarker = "\n...[truncated]"

// rejectOversizedToolResults 把超限输出替换为显式重试指令（不给头部/尾部
// 预览，避免基于误导片段的推理；目标会话显式传入）。
func (c *Coordinator) rejectOversizedToolResults(sessionID string, maxChars int) (bool, error) {
	history := c.engineHistory(sessionID)
	c.Mu.RLock()
	refs := c.tasks.ResultRefsByCallIDFor(sessionID)
	c.Mu.RUnlock()
	filtered, changed := rejectToolResultsWithRefs(history, maxChars, refs)
	if !changed {
		return false, nil
	}
	if err := c.replaceEngineHistory(sessionID, filtered); err != nil {
		return false, fmt.Errorf("reject oversized tool results: %w", err)
	}
	return true, nil
}

// RejectToolResults 替换超限工具结果为显式引用警告（纯函数面）。
func RejectToolResults(history []contract.EngineMessage, maxChars int) ([]contract.EngineMessage, bool) {
	return rejectToolResultsWithRefs(history, maxChars, nil)
}

func rejectToolResultsWithRefs(history []contract.EngineMessage, maxChars int, refs map[string]string) ([]contract.EngineMessage, bool) {
	filtered := append([]contract.EngineMessage(nil), history...)
	changed := false
	for index := range filtered {
		message := &filtered[index]
		if message.Role != "tool" || !IsOversizedToolResult(message.Content, maxChars) {
			continue
		}
		message.Content = OversizedToolResultWarning(message.Name, refs[message.ToolCallID])
		message.ContentSet = true
		changed = true
	}
	return filtered, changed
}

// IsOversizedToolResult 判定工具结果是否超限（或带框架截断标记）。
func IsOversizedToolResult(content string, maxChars int) bool {
	return len(content) > maxChars || strings.HasSuffix(content, FrameworkToolOutputTruncatedMarker)
}

// OversizedToolResultWarning 是超限工具结果归档警告文本。
func OversizedToolResultWarning(name, resultRef string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "tool"
	}
	var builder strings.Builder
	builder.WriteString(ToolResultOmittedPrefix + "\n")
	builder.WriteString("tool=")
	builder.WriteString(name)
	builder.WriteByte('\n')
	if resultRef != "" {
		builder.WriteString("result_ref=")
		builder.WriteString(resultRef)
		builder.WriteByte('\n')
	}
	builder.WriteString("The result exceeded the provider-context item budget; raw content was not included.\n")
	builder.WriteString("Do not infer facts from omitted content. Use read_tool_result with pagination or filtering, or issue a narrower read-only query.\n")
	builder.WriteString("</seelex-tool-result-omitted>")
	return builder.String()
}

// ProviderSafeToolResult 把超限工具结果替换为警告（provider 路径）。
func ProviderSafeToolResult(name, result string, toolErr error) string {
	if toolErr != nil || !IsOversizedToolResult(result, limits.Get().MaxToolResultChars) {
		return result
	}
	return OversizedToolResultWarning(name, "")
}

// EstimateEngineHistoryTokens 估算引擎历史 token 数（纯函数面）。
func EstimateEngineHistoryTokens(history []contract.EngineMessage) int {
	tokens := 0
	for _, message := range history {
		tokens += seelexctx.EstimateTokens(message.Content)
		tokens += seelexctx.EstimateTokens(message.ReasoningContent)
		for _, call := range message.ToolCalls {
			tokens += seelexctx.EstimateTokens(call.Name) + seelexctx.EstimateTokens(call.Arguments)
		}
	}
	return tokens
}

// TaskContextRecoveryHistory 保留 system 指令并把可变协议记录替换为 checkpoint。
func TaskContextRecoveryHistory(history []contract.EngineMessage, checkpoint string) []contract.EngineMessage {
	compacted := RetainedSystemHistory(history)
	return append(compacted, contract.EngineMessage{Role: "user", Content: checkpoint, ContentSet: true})
}

// RetainedSystemHistory 保留一条产品指令（框架侧摘要也是 system 消息，
// 全保留会让错误/重复的历史替换成倍放大 prompt）。
func RetainedSystemHistory(history []contract.EngineMessage) []contract.EngineMessage {
	for _, message := range history {
		if message.Role == "system" {
			return []contract.EngineMessage{message}
		}
	}
	return nil
}

// RemoveTaskContextCheckpoints 阻止 Application 控制消息被持久化/重建为
// 前端会话占位（活跃会话兼容包装）。
func (c *Coordinator) RemoveTaskContextCheckpoints() error {
	return c.RemoveTaskContextCheckpointsFor(c.tasks.SessionIDForRequest(""))
}

// RemoveTaskContextCheckpointsFor 阻止 Application 控制消息被持久化/重建为
// 前端会话占位（目标会话由调用方显式传入）。
func (c *Coordinator) RemoveTaskContextCheckpointsFor(sessionID string) error {
	history := c.engineHistory(sessionID)
	filtered := make([]contract.EngineMessage, 0, len(history))
	removed := false
	for _, message := range history {
		if message.Role == "user" && strings.HasPrefix(message.Content, TaskContextCheckpointPrefix) {
			removed = true
			continue
		}
		filtered = append(filtered, message)
	}
	if !removed {
		return nil
	}
	if err := c.replaceEngineHistory(sessionID, filtered); err != nil {
		return fmt.Errorf("remove task context checkpoint: %w", err)
	}
	return nil
}

// IsTaskContextCheckpoint 判定内容是否为上下文 checkpoint 标记。
func IsTaskContextCheckpoint(content string) bool {
	return strings.HasPrefix(content, TaskContextCheckpointPrefix)
}

// RecordContextControlFailure 把 hook 失败转移给 runChat（委托 task 域）。
func (c *Coordinator) RecordContextControlFailure(requestID string, err error) {
	c.tasks.RecordContextControlFailure(requestID, err)
}

// TakeContextControlFailure 取走当前请求的 context 控制失败（委托 task 域）。
func (c *Coordinator) TakeContextControlFailure(requestID string) error {
	return c.tasks.TakeContextControlFailure(requestID)
}

// engineHistory 返回指定会话引擎历史（会话路由引擎用 HistoryFor，否则活跃
// 引擎）。
func (c *Coordinator) engineHistory(sessionID string) []contract.EngineMessage {
	if routed, ok := c.Deps.Engine.(contract.SessionChatEngine); ok {
		return routed.HistoryFor(sessionID)
	}
	return c.Deps.Engine.History()
}

// setEngineSystemPrompt 设置指定会话引擎 system prompt（会话路由引擎用
// SetSystemPromptFor，否则活跃引擎）。
func (c *Coordinator) setEngineSystemPrompt(sessionID, prompt string) {
	if routed, ok := c.Deps.Engine.(contract.SessionChatEngine); ok {
		routed.SetSystemPromptFor(sessionID, prompt)
		return
	}
	c.Deps.Engine.SetSystemPrompt(prompt)
}

// replaceEngineHistory 会话内替换指定会话引擎历史（会话路由引擎用
// ReplaceHistoryFor，不切活跃；否则回退契约 ReplaceHistory）。
func (c *Coordinator) replaceEngineHistory(sessionID string, history []contract.EngineMessage) error {
	if routed, ok := c.Deps.Engine.(contract.SessionChatEngine); ok {
		return routed.ReplaceHistoryFor(sessionID, history)
	}
	return c.Deps.Engine.ReplaceHistory(sessionID, history)
}

// clearEngineHistory 清空指定会话引擎历史（会话路由引擎用 ClearHistoryFor，
// 否则活跃引擎）。
func (c *Coordinator) clearEngineHistory(sessionID string) {
	if routed, ok := c.Deps.Engine.(contract.SessionChatEngine); ok {
		routed.ClearHistoryFor(sessionID)
		return
	}
	c.Deps.Engine.ClearHistory()
}

// appendEngineHistory 追加消息到指定会话引擎历史（会话路由引擎用
// AppendHistoryFor，否则活跃引擎）。
func (c *Coordinator) appendEngineHistory(sessionID string, msg types.Message) {
	if routed, ok := c.Deps.Engine.(contract.SessionChatEngine); ok {
		routed.AppendHistoryFor(sessionID, msg)
		return
	}
	c.Deps.Engine.AppendHistory(msg)
}
