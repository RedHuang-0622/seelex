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
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/application/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
)

const (
	TaskContextCheckpointPrefix = "<!-- seelex:context-checkpoint:v1 -->"
	planContextPrefix           = "<!-- seelex:active-plan:v1 -->"
	// ActiveSkillPrefix 标记激活技能正文 internal 事件：作为 append-only user
	// 轮次进入 transcript（与 task_context.ActiveSkillMarker 同源字符串），
	// 装配/存档/import 用 IsActiveSkillContent 把它挡在可见会话之外；它不是
	// 每轮重建的动态尾部消息，保留段照常携带（定稿轮次，字节稳定）。
	ActiveSkillPrefix       = "<!-- seelex:active-skill:v1 -->"
	ToolResultOmittedPrefix = "<seelex-tool-result-omitted>"
	// 恢复/预算终局前缀：与根包 history_safety.go / chat.go 同源协议字符串
	// （context_runtime 不反向依赖 core 根包，字符串字面量在此保留）。
	contextRecoveryPrefix         = "<!-- seelex:context-recovery:v1 -->"
	providerRecoveryPrefix        = "<!-- seelex:provider-recovery:v1 -->"
	reactBudgetFinalizationPrefix = "<!-- seelex:react-budget-finalize:v1 -->"
)

// IsActiveSkillContent 判定内容是否为激活技能 internal 事件（Append-only
// transcript 技能轮次；前端可见性/存档/import 据此跳过）。
func IsActiveSkillContent(content string) bool {
	return strings.HasPrefix(content, ActiveSkillPrefix)
}

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
	workTable func(sessionID string) string
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
// （引擎迭代 hook 调用，绝不持有 Core.ViewMu；活跃会话兼容包装）。
func (c *Coordinator) CompactTaskContext(requestID string) error {
	return c.CompactTaskContextFor(c.tasks.SessionIDForRequest(requestID), requestID)
}

// CompactTaskContextFor 把指定会话整个可变 transcript 替换为一个私有、有界
// 的 checkpoint（引擎迭代 hook 调用，绝不持有 Core.ViewMu）。
func (c *Coordinator) CompactTaskContextFor(sessionID, requestID string) error {
	_, err := c.PrepareExecutionContextFor(sessionID, requestID, "")
	if err != nil {
		return err
	}
	if err := c.sessions.PersistCurrentSession(c.sessionLocationLocked(sessionID), sessionID); err != nil {
		return fmt.Errorf("persist context checkpoint: %w", err)
	}
	return nil
}

// sessionLocationLocked 返回指定会话的持久化定位（workspace 绑定优先；
// 回退当前活跃工作区）。压缩 checkpoint 落盘的目标会话可能不是活跃会话，
// 必须按会话键落盘（对应 R3 键漂移修复）。
func (c *Coordinator) sessionLocationLocked(sessionID string) session_runtime.Location {
	c.ViewMu.RLock()
	defer c.ViewMu.RUnlock()
	location := session_runtime.Location{Meta: model.SessionInfo{ID: sessionID}}
	if workspaceID, ok := c.Snapshot.SessionWorkspaces[sessionID]; ok && workspaceID != "" {
		location.WorkspaceID = workspaceID
		return location
	}
	if c.Snapshot.CurrentWorkspace != nil {
		location.WorkspaceID = c.Snapshot.CurrentWorkspace.ID
		location.Workspace = c.Snapshot.CurrentWorkspace
	}
	return location
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
	// 无活动任务时块为空 → 自动删除；不落历史 → 不参与压缩）。打点按正在
	// 组装上下文的会话取数：后台会话只见自己的 scope 分区，不串活跃会话。
	if block := c.workTable(sessionID); block != "" {
		if currentInput == "" {
			currentInput = block
		} else {
			currentInput = block + "\n\n" + currentInput
		}
	}
	budget := task_context.ContextBudgetFor(c.Deps.Runtime)
	tools := c.Deps.Runtime.VisibleTools(context.Background())
	existing := c.engineHistory(sessionID)
	c.ViewMu.RLock()
	systemPrompt := c.prompts.SystemPromptForActiveTaskLockedFor(sessionID)
	c.ViewMu.RUnlock()
	c.setEngineSystemPrompt(sessionID, systemPrompt)

	runtimeModel := c.Deps.Runtime.Model()
	c.ViewMu.Lock()
	state := c.tasks.CurrentTaskExecutionFor(sessionID)
	if state == nil || state.RequestID != requestID {
		c.ViewMu.Unlock()
		return currentInput, nil
	}
	events := append([]model.TranscriptEvent(nil), c.tasks.TranscriptFor(sessionID)...)
	events = excludeCurrentInputEvent(events, requestID, currentInput)
	// 达峰判定以全量累积 context 为准（而非可能已被框架压缩的引擎历史）：
	// 与引擎缓存估算取峰值，压缩是唯一使累积前缀失效的事件。
	fullContext := task_context.TranscriptTailHistory(events, budget.Budget, 0)
	rawTokens := c.tasks.CountRequestTokens(systemPrompt, fullContext, currentInput, tools)
	if cacheTokens := c.tasks.CountRequestTokens(systemPrompt, existing, currentInput, tools); cacheTokens > rawTokens {
		rawTokens = cacheTokens
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
	c.ViewMu.Unlock()

	systems := RetainedSystemHistory(c.engineHistory(sessionID))
	target := budget.Budget
	contextMaxUnits := 0 // 达峰前：全量累积（append-only 已定稿轮次）
	if rawTokens >= budget.SoftThreshold {
		target = budget.TargetAfterCompaction
		contextMaxUnits = limits.Get().ContextMaxUnits // 压缩后：有界新鲜窗口
	}
	if contextMaxUnits == 0 {
		// 累积模式：保留段（稳定前缀 + 已定稿轮次）已覆盖 transcript 前缀，
		// 只追加保留段之后的新事件（append-only，字节稳定）。
		if covered := retainedContextEventCount(systems); covered > 0 {
			if covered < len(events) && !retainedMatchesTranscriptPrefix(systems, events) {
				// 冷恢复只装载了尾部窗口：保留段是 transcript 的**后缀**而
				// 非前缀（retainedMatchesTranscriptPrefix=false）。此时按
				// “已覆盖 covered 条事件”跳过会得到 [tail]+[middle] 的
				// 重排与重复（恢复后首个请求上下文顺序 != 会话顺序，前缀
				// 字节不稳定 → 缓存无法命中）。回退为从完整 transcript
				// 按原序重建：保留段只留 system，事件不裁剪。
				systems = RetainedSystemOnly(systems)
			} else if covered < len(events) {
				events = events[covered:]
			} else {
				events = nil
			}
		}
	}
	assembled, estimated := c.fitExecutionHistory(systemPrompt, systems, planMessage, events, currentInput, tools, target, contextMaxUnits)
	if estimated > budget.Budget {
		return "", fmt.Errorf("%w: estimated=%d budget=%d", ErrProviderContextBudgetExceeded, estimated, budget.Budget)
	}
	if err := c.replaceEngineHistory(sessionID, assembled); err != nil {
		return "", fmt.Errorf("assemble provider context: %w", err)
	}
	if err := c.history.PrepareProviderHistoryFor(sessionID); err != nil {
		return "", err
	}

	c.ViewMu.Lock()
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
	c.ViewMu.Unlock()
	if recorded {
		if hub, ok := c.Events.(event.SessionAwareHub); ok {
			hub.PublishSession(event.EventSnapshotChanged, revision, requestID, sessionID, nil)
		} else {
			c.Events.Publish(event.EventSnapshotChanged, revision, requestID, nil)
		}
	}
	return currentInput, nil
}

// fitExecutionHistory 按目标预算装配 provider 历史：稳定前缀（system）→
// 累积 context（已定稿轮次，含 append-only 的激活技能事件）→ plan 尾部。
// contextMaxUnits <= 0 = 全量累积（达峰前 append-only，字节稳定）；>0 =
// 有界窗口（压缩后新鲜窗口）。
func (c *Coordinator) fitExecutionHistory(
	systemPrompt string,
	systems []contract.EngineMessage,
	planMessage string,
	events []model.TranscriptEvent,
	currentInput string,
	tools []model.Tool,
	target int,
	contextMaxUnits int,
) ([]contract.EngineMessage, int) {
	// 压缩窗口模式：丢弃保留的累积段，从事件重建新鲜窗口（保留段只供全量
	// 累积模式复用，避免与窗口内容重复）。
	base := systems
	if contextMaxUnits > 0 {
		base = RetainedSystemOnly(systems)
	}
	if history, estimated := c.tryFitExecutionHistory(systemPrompt, base, planMessage, events, currentInput, tools, target, contextMaxUnits); estimated <= target {
		return history, estimated
	}
	// 达峰回退：全量累积超预算 → 折为有界窗口；窗口仍超 → 逐级收缩。
	// 最终兜底不“静默清空”：TranscriptTailHistory 保证至少返回最新 1 个
	// 完整单元（即使估算超过 target），不再走 events=nil 的 system+plan
	// 空历史分支；估算仍超出全量预算时由调用方以 ErrProviderContextBudgetExceeded
	// 拒绝发送（拒绝优于“模型失忆”，正常路径不用 checkpoint 兜底）。
	for maxUnits := limits.Get().ContextMaxUnits; maxUnits > 0; maxUnits-- {
		if history, estimated := c.tryFitExecutionHistory(systemPrompt, base, planMessage, events, currentInput, tools, target, maxUnits); estimated <= target {
			return history, estimated
		}
	}
	return c.tryFitExecutionHistory(systemPrompt, RetainedSystemOnly(systems), planMessage, events, currentInput, tools, target, 1)
}

// tryFitExecutionHistory 装配一次 system → context → plan 历史并估算 token。
func (c *Coordinator) tryFitExecutionHistory(
	systemPrompt string,
	systems []contract.EngineMessage,
	planMessage string,
	events []model.TranscriptEvent,
	currentInput string,
	tools []model.Tool,
	target int,
	contextMaxUnits int,
) ([]contract.EngineMessage, int) {
	history := append([]contract.EngineMessage(nil), systems...)
	if contextMaxUnits <= 0 {
		// 全量累积（append-only 已定稿轮次）。
		history = append(history, task_context.TranscriptTailHistory(events, target, 0)...)
	} else {
		history = append(history, task_context.TranscriptTailHistory(events, target, contextMaxUnits)...)
	}
	// plan 后置贴近当前输入（LLM 循环会把当前输入追加到历史尾部）。
	if planMessage != "" {
		history = append(history, contract.EngineMessage{Role: "system", Content: planMessage, ContentSet: true})
	}
	return history, c.tasks.CountRequestTokens(systemPrompt, history, currentInput, tools)
}

// retainedMatchesTranscriptPrefix 判定引擎保留段（非 system 的已定稿轮次）
// 是否与 transcript 事件流的前缀一一对应。正常续跑时引擎历史就是 transcript
// 前缀，covered 计数可直接用于跳过；冷恢复（resumeSessionCold 只装载尾部
// 窗口）时保留段对应 transcript 后缀，若仍按 covered 跳过会把中段事件
// 追加到尾部之后，造成上下文重排/重复。比较以 Role+Content 为准（工具轮
// 的 ToolCalls 只影响 wire 展示，不改变覆盖判断）。
func retainedMatchesTranscriptPrefix(systems []contract.EngineMessage, events []model.TranscriptEvent) bool {
	retained := make([]contract.EngineMessage, 0, len(systems))
	for _, message := range systems {
		if message.Role != "system" {
			retained = append(retained, message)
		}
	}
	if len(retained) == 0 || len(retained) > len(events) {
		return false
	}
	for index := range retained {
		if retained[index].Role != events[index].Role ||
			retained[index].Content != events[index].Content {
			return false
		}
	}
	return true
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
	// plan 执行指令并入尾部（原本在 system 的 Active Plan Execution Policy 段，
	// 会随 plan 加载/完成改写 system 头部 → 一次前缀悬崖）。尾部消息每轮随节点
	// 状态重建，本来就在缓存未命中区，指令放这里零额外失效成本。
	return planContextPrefix + "\n" + string(encoded) + "\n\n## Active Plan Execution Policy\n" +
		"The Plan is validated and authoritative for this task. Do not silently replace or reorder it. " +
		"Execute the current node and its declared dependencies in stable order. Use read_plan for omitted node detail. " +
		"plan_ref=" + projection.CanonicalPlanRef
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
	c.ViewMu.RLock()
	refs := c.tasks.ResultRefsByCallIDFor(sessionID)
	c.ViewMu.RUnlock()
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
	compacted := RetainedSystemOnly(history)
	return append(compacted, contract.EngineMessage{Role: "system", Content: checkpoint, ContentSet: true})
}

// RetainedSystemHistory 保留稳定前缀 + 已定稿轮次的 append-only 累积段：
// 从既有引擎历史中剔除动态尾部（plan 上下文 / checkpoint / 压缩帧标记 /
// 恢复信封），使下一轮装配只追加保留段之后的新事件，前缀字节稳定。
// 恢复路径（provider 504 / history-safety）请用 RetainedSystemOnly。
func RetainedSystemHistory(history []contract.EngineMessage) []contract.EngineMessage {
	retained := make([]contract.EngineMessage, 0, len(history))
	for _, message := range history {
		if !isDynamicTailMessage(message) {
			retained = append(retained, message)
		}
	}
	return retained
}

// RetainedSystemOnly 保留一条产品指令（框架侧摘要也是 system 消息，全保留
// 会让错误/重复的历史替换成倍放大 prompt）。恢复路径专用：不得携带已定稿
// 轮次（provider 已拒绝过大上下文，重放 transcript 会再次失败）。
func RetainedSystemOnly(history []contract.EngineMessage) []contract.EngineMessage {
	for _, message := range history {
		if message.Role == "system" {
			return []contract.EngineMessage{message}
		}
	}
	return nil
}

// isDynamicTailMessage 判定消息是否为动态尾部/控制消息（plan 上下文、
// checkpoint、压缩帧标记、恢复信封、预算终局输入）：这类消息每轮重建或
// 由恢复路径单独管理，不进入保留的稳定前缀 + 已定稿累积段。激活技能事件
// 不在其列——它是 append-only 的定稿轮次，由保留段照常携带并计数。
func isDynamicTailMessage(message contract.EngineMessage) bool {
	content := message.Content
	return strings.HasPrefix(content, planContextPrefix) ||
		strings.HasPrefix(content, TaskContextCheckpointPrefix) ||
		strings.HasPrefix(content, seelexctx.CompactContextMarker) ||
		strings.HasPrefix(content, contextRecoveryPrefix) ||
		strings.HasPrefix(content, providerRecoveryPrefix) ||
		strings.HasPrefix(content, reactBudgetFinalizationPrefix)
}

// retainedContextEventCount 返回保留段中已定稿轮次的 message 数（与
// transcript 事件 1:1，供累积模式跳过已保留前缀）。
func retainedContextEventCount(retained []contract.EngineMessage) int {
	count := 0
	for _, message := range retained {
		if message.Role != "system" {
			count++
		}
	}
	return count
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
