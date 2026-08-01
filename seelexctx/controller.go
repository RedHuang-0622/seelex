// seelexContextController 是 seelectx.ContextController 实现
// （plan.md §3.7.4 / 架构文档 4.6(4)）：
//
//   - 软阈值：片段闭合（after_assistant / after_tool）时只压缩滑动窗口外的
//     轮次（窗口内永不压缩）；产物合并上一栈顶帧 → 综合摘要（栈顶自足）
//     后 push CompactStack，再返回 ContextDecision{ReplaceHistory, 投影历史}。
//   - 硬阈值：after_tool 超大工具输出先归档为 result_ref（processor 路径），
//     仍超限才收缩窗口（WindowPolicy 以硬阈值预算推导，不低于 MinRounds），
//     新移出窗口的轮次进入压缩。
//   - ReplaceHistory 前执行 history_safety 配对修复与 checkpoint 标记清理。
//
// 决策输入全部构造注入（WindowPolicy / TokenCounter / BudgetProvider /
// ToolResultArchiver / CompactStackStore），无魔法数字。
package seelexctx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// compactContextMarker 标记压缩帧块（窗口外压缩产物，进入 working history）。
const compactContextMarker = "<!-- seelex:compact-context:v1 -->"

// checkpointMarker 是旧应用侧任务检查点标记（替换历史时清理）。
const checkpointMarker = "<!-- seelex:context-checkpoint:v1 -->"

// ContextWindowPolicy 软/硬阈值（与 application contextBudget 同源决策：
// Budget 内 75% 软阈值 / 90% 硬阈值 / 压缩目标 60%，2026-07-29 文档保留）。
type ContextWindowPolicy struct {
	Window         int // provider 上下文窗口
	OutputReserve  int // 单次输出预留
	SafetyReserve  int // 安全保留区
	ReservedTokens int // system prompt + 栈块固定预留（0 → 用 SafetyReserve）
	ConfigRounds   int // 显式 window.rounds（0 = 未配置）
}

// NewContextWindowPolicy 按 window/outputReserve 构造阈值策略
// （与 newContextBudget 同款计算：safety = window/8）。
func NewContextWindowPolicy(window, outputReserve int) ContextWindowPolicy {
	safetyReserve := window / 8
	if safetyReserve < 0 {
		safetyReserve = 0
	}
	return ContextWindowPolicy{
		Window: window, OutputReserve: outputReserve, SafetyReserve: safetyReserve,
	}
}

// Budget 返回可用于请求的 token 预算。
func (p ContextWindowPolicy) Budget() int { return p.Window - p.OutputReserve - p.SafetyReserve }

// SoftThreshold 软阈值（75%）。
func (p ContextWindowPolicy) SoftThreshold() int { return p.Budget() * 75 / 100 }

// HardThreshold 硬阈值（90%）。
func (p ContextWindowPolicy) HardThreshold() int { return p.Budget() * 90 / 100 }

// TargetAfterCompaction 压缩目标（60%）。
func (p ContextWindowPolicy) TargetAfterCompaction() int { return p.Budget() * 60 / 100 }

// Reserved 固定预留（system prompt + 栈块）。
func (p ContextWindowPolicy) Reserved() int {
	if p.ReservedTokens > 0 {
		return p.ReservedTokens
	}
	return p.SafetyReserve
}

// TokenCounter 注入的 token 计数器（seelex token_counter 契约）。
type TokenCounter interface {
	Name() string
	CountText(string) int
	CountMessage(types.Message) int
	CountHistory([]types.Message) int
}

// ConservativeTokenCounter 保守估算（len/3，与 seelectx.EstimateTokens 同源）。
type ConservativeTokenCounter struct{}

// Name 实现 TokenCounter。
func (ConservativeTokenCounter) Name() string { return "conservative-v1" }

// CountText 实现 TokenCounter。
func (ConservativeTokenCounter) CountText(value string) int {
	if value == "" {
		return 0
	}
	return (len([]byte(value)) + 2) / 3
}

// CountMessage 实现 TokenCounter。
func (c ConservativeTokenCounter) CountMessage(message types.Message) int {
	tokens := 4 + c.CountText(message.Role) + c.CountText(messageContent(message)) + c.CountText(message.ReasoningContent)
	if message.ToolCallID != "" {
		tokens += 2 + c.CountText(message.ToolCallID)
	}
	if message.Name != "" {
		tokens += 2 + c.CountText(message.Name)
	}
	for _, call := range message.ToolCalls {
		tokens += 8 + c.CountText(call.ID) + c.CountText(call.Function.Name) + c.CountText(call.Function.Arguments)
	}
	return tokens
}

// CountHistory 实现 TokenCounter。
func (c ConservativeTokenCounter) CountHistory(history []types.Message) int {
	total := 0
	for _, message := range history {
		total += c.CountMessage(message)
	}
	return total
}

// messageContent 解引用可空消息正文。
func messageContent(message types.Message) string {
	if message.Content == nil {
		return ""
	}
	return *message.Content
}

// BudgetProvider 提供 provider 上下文窗口与最大输出（seelebridge Runtime 适配）。
type BudgetProvider interface {
	ContextTokens() int
	MaxOutputTokens() int
}

// CompactStackStore 会话级压缩栈读写（sessionstore.SessionContextStore 满足；
// nil → 控制器内存态，活跃会话内仍可审计）。
type CompactStackStore interface {
	Snapshot() sessionstore.SessionContextRecord
	PushCompact(frame sessionstore.CompactFrame) error
}

// memoryCompactStack 是 CompactStackStore 的内存态实现。
type memoryCompactStack struct {
	mu     sync.Mutex
	frames []sessionstore.CompactFrame
}

// Snapshot 实现 CompactStackStore。
func (m *memoryCompactStack) Snapshot() sessionstore.SessionContextRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return sessionstore.SessionContextRecord{
		CompactStack: append([]sessionstore.CompactFrame(nil), m.frames...),
	}
}

// PushCompact 实现 CompactStackStore。
func (m *memoryCompactStack) PushCompact(frame sessionstore.CompactFrame) error {
	if m == nil {
		return fmt.Errorf("seelexctx: compact stack is unavailable")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.frames = append(m.frames, frame)
	return nil
}

// NewMemoryCompactStack 创建内存态压缩栈（无持久后端时的兜底，活跃会话
// 内仍可审计 From/To 不变量）。
func NewMemoryCompactStack() CompactStackStore {
	return &memoryCompactStack{}
}

// ControllerOptions 控制器的全部注入依赖。
type ControllerOptions struct {
	Policy ContextWindowPolicy
	Window WindowPolicy

	// Tokens token 计数（nil → ConservativeTokenCounter）。
	Tokens TokenCounter

	// Budget provider 上下文窗口/最大输出（nil → 用 Policy.Window 且
	// OutputReserve 归零，阈值仍可用）。
	Budget BudgetProvider

	// Archive 超大工具结果归档（nil → 内存归档）。
	Archive ToolResultArchiver

	// Stacks 会话级压缩栈（nil → 内存态）。
	Stacks CompactStackStore

	// SessionID 用于压缩帧 SegmentID 溯源。
	SessionID string

	// MaxToolResultChars 超大工具结果判定（≤0 → 框架默认）。
	MaxToolResultChars int
}

// seelexContextController 实现 seelectx.ContextController。
type seelexContextController struct {
	opts ControllerOptions
	mu   sync.Mutex
	// lastCompactedLen 上次压缩时窗口外单元数：相对上次无新溢出时不重复压缩。
	lastCompactedLen int
}

// NewContextController 构造 seelex 上下文控制器。
func NewContextController(opts ControllerOptions) seelectx.ContextController {
	if opts.Tokens == nil {
		opts.Tokens = ConservativeTokenCounter{}
	}
	if opts.Archive == nil {
		opts.Archive = NewInMemoryToolResultArchiver()
	}
	if opts.Stacks == nil {
		opts.Stacks = &memoryCompactStack{}
	}
	return &seelexContextController{opts: opts}
}

// Handle 实现 seelectx.ContextController。
func (c *seelexContextController) Handle(ctx context.Context, ev seelectx.ContextEvent) (seelectx.ContextDecision, error) {
	switch ev.Kind {
	case seelectx.ContextAfterTool:
		if c.oversizedTool(ev.Tool) {
			return c.hardThresholdPath(ctx, ev)
		}
		if c.softThresholdHit(ev) {
			return c.compressWindowOutside(ctx, ev)
		}
	case seelectx.ContextAfterAssistant:
		// 片段闭合（完整协议单元结束）→ 软阈值触发窗口外压缩。
		if c.softThresholdHit(ev) {
			return c.compressWindowOutside(ctx, ev)
		}
	}
	return seelectx.ContextDecision{}, nil
}

// ── 阈值与窗口 ────────────────────────────────────────────────────

// softThresholdHit 按注入 token 计数估算当前请求 token，跨过软阈值即触发。
func (c *seelexContextController) softThresholdHit(ev seelectx.ContextEvent) bool {
	tokens := c.opts.Tokens.CountHistory(ev.History) + c.opts.Tokens.CountText(ev.Query)
	return tokens >= c.policy().SoftThreshold()
}

// oversizedTool 判断事件携带的工具结果是否超大（带截断标记或超字符预算）。
func (c *seelexContextController) oversizedTool(result *seelectx.ToolResult) bool {
	if result == nil {
		return false
	}
	return IsOversizedToolResult(result.Raw, c.maxToolResultChars())
}

func (c *seelexContextController) maxToolResultChars() int {
	if c.opts.MaxToolResultChars > 0 {
		return c.opts.MaxToolResultChars
	}
	return DefaultMaxToolResultChars
}

// policy 返回生效的阈值策略（Budget 提供时用其窗口/输出推导）。
func (c *seelexContextController) policy() ContextWindowPolicy {
	policy := c.opts.Policy
	if policy.Window <= 0 {
		policy.Window = DefaultMaxTokens
	}
	if c.opts.Budget != nil {
		if contextTokens := c.opts.Budget.ContextTokens(); contextTokens > 0 {
			output := c.opts.Budget.MaxOutputTokens()
			policy = NewContextWindowPolicy(contextTokens, output)
		}
	}
	if policy.OutputReserve <= 0 {
		policy.OutputReserve = policy.Window / 8
	}
	return policy
}

// windowRounds 经 WindowPolicy 推导当前窗口 N；输入缺失时保守回退
// MinRounds（WindowRounds 返回的 n 已是回退值，错误供审计）。
func (c *seelexContextController) windowRounds(ctx context.Context, history []types.Message) int {
	if c.opts.Window == nil {
		return defaultMinRounds
	}
	units := c.chatUnits(history)
	info := c.windowInfo(units)
	n, err := c.opts.Window.WindowRounds(ctx, info)
	if err != nil {
		return n // 策略已保守回退 MinRounds
	}
	return n
}

// shrinkWindowRounds 硬阈值路径的窗口收缩：以硬阈值预算为 ContextTokens
// 重推导 N（WindowPolicy clamp 保证不低于 MinRounds）。
func (c *seelexContextController) shrinkWindowRounds(ctx context.Context, history []types.Message) int {
	if c.opts.Window == nil {
		return defaultMinRounds
	}
	units := c.chatUnits(history)
	info := c.windowInfo(units)
	info.ContextTokens = c.policy().HardThreshold()
	n, err := c.opts.Window.WindowRounds(ctx, info)
	if err != nil {
		return n
	}
	return n
}

// defaultMinRounds 是 WindowPolicy 缺省时的保守回退（与 DefaultWindowConfig
// 的 min_rounds 一致；配置策略注入后由策略决定）。
const defaultMinRounds = 4

func (c *seelexContextController) windowInfo(units []historyUnit) ProviderContextInfo {
	policy := c.policy()
	return ProviderContextInfo{
		ContextTokens:  policy.Window,
		AvgRoundTokens: c.avgRoundTokens(units),
		ReservedTokens: policy.Reserved(),
		ConfigRounds:   policy.ConfigRounds,
	}
}

// avgRoundTokens 按最近完整单元估算每轮 token（双限：非零且有界）。
func (c *seelexContextController) avgRoundTokens(units []historyUnit) int {
	for index := len(units) - 1; index >= 0; index-- {
		unitTokens := c.opts.Tokens.CountHistory(units[index].messages)
		if unitTokens > 0 {
			return unitTokens
		}
	}
	return 1
}

// ── 硬阈值路径 ────────────────────────────────────────────────────

// hardThresholdPath：先归档超大工具输出为 result_ref（processor 路径之外
// 兜底；归档器按调用 ID 幂等），仍超限才收缩窗口（不低于 MinRounds），
// 新移出窗口的轮次进入压缩。
func (c *seelexContextController) hardThresholdPath(ctx context.Context, ev seelectx.ContextEvent) (seelectx.ContextDecision, error) {
	if ev.Tool != nil && c.oversizedTool(ev.Tool) {
		if _, err := c.opts.Archive.Store(ctx, ev.Tool.CallID, ev.Tool.Name, ev.Tool.Raw); err != nil {
			return seelectx.ContextDecision{}, fmt.Errorf("seelexctx: archive oversized tool result %q: %w", ev.Tool.Name, err)
		}
	}
	n := c.shrinkWindowRounds(ctx, ev.History)
	return c.compressWindowOutsideWith(ctx, ev, n)
}

// ── 窗口外压缩（plan.md §3.7.4）────────────────────────────────────

// compressWindowOutside 以当前窗口 N 压缩窗口外轮次。
func (c *seelexContextController) compressWindowOutside(ctx context.Context, ev seelectx.ContextEvent) (seelectx.ContextDecision, error) {
	n := c.windowRounds(ctx, ev.History)
	return c.compressWindowOutsideWith(ctx, ev, n)
}

// compressWindowOutsideWith 只压缩窗口外轮次（窗口内原样保留）；新溢出帧
// 合并上一栈顶帧（栈顶自足）后 push CompactStack。
func (c *seelexContextController) compressWindowOutsideWith(ctx context.Context, ev seelectx.ContextEvent, n int) (seelectx.ContextDecision, error) {
	if n <= 0 {
		return seelectx.ContextDecision{}, nil
	}
	units := c.chatUnits(ev.History)
	if len(units) <= n {
		return seelectx.ContextDecision{}, nil
	}
	overflow := units[:len(units)-n]
	c.mu.Lock()
	lastCompacted := c.lastCompactedLen
	c.mu.Unlock()
	if len(overflow) == lastCompacted {
		return seelectx.ContextDecision{}, nil // 相对上次压缩无新溢出
	}

	frame, err := c.buildCompactFrame(overflow)
	if err != nil {
		return seelectx.ContextDecision{}, fmt.Errorf("seelexctx: build compact frame: %w", err)
	}
	if err := c.opts.Stacks.PushCompact(frame); err != nil {
		return seelectx.ContextDecision{}, fmt.Errorf("seelexctx: push compact frame: %w", err)
	}

	projected := projectHistory(ev.History, units, n)
	// ReplaceHistory 前：history_safety 配对修复 + checkpoint/旧压缩帧清理
	//（只作用于保留的窗口消息；新压缩帧在修复后前置，不受清理影响）。
	projected = PrepareReplaceHistory(projected)
	projected = append([]types.Message{compactFrameMessage(frame)}, projected...)

	c.mu.Lock()
	c.lastCompactedLen = len(overflow)
	c.mu.Unlock()
	return seelectx.ContextDecision{ReplaceHistory: true, History: projected}, nil
}

// buildCompactFrame 构造压缩帧：Summary 合并上一栈顶帧与当前溢出内容
// （栈顶自足 = 该时刻窗口外全部轮次的综合摘要）；From/To 为单元索引
// （可审计"窗口外才被压缩"不变量）。
func (c *seelexContextController) buildCompactFrame(overflow []historyUnit) (sessionstore.CompactFrame, error) {
	record := c.opts.Stacks.Snapshot()
	var prevTop *sessionstore.CompactFrame
	if len(record.CompactStack) > 0 {
		top := record.CompactStack[len(record.CompactStack)-1]
		prevTop = &top
	}
	summary := c.summarizeOverflow(overflow, prevTop, record)
	segmentID := fmt.Sprintf("compact-%d", time.Now().UnixMilli())
	if c.opts.SessionID != "" {
		segmentID = fmt.Sprintf("compact-%s-%d", c.opts.SessionID, time.Now().UnixMilli())
	}
	frame := sessionstore.CompactFrame{
		SegmentID:    segmentID,
		From:         0,
		To:           len(overflow) - 1,
		Summary:      summary,
		Evidence:     overflowEvidence(overflow, record),
		CompressedAt: time.Now(),
	}
	if prevTop != nil {
		// 合并上一栈顶：帧范围接续上一帧起点（窗口外全部轮次的连续段）。
		frame.From = prevTop.From
	}
	return frame, nil
}

// summarizeOverflow 生成综合摘要：栈帧的 goal/plan/evidence（片段闭合压缩
// 保留目标/计划/证据）+ 上一栈顶摘要 + 溢出轮次代表性内容。
func (c *seelexContextController) summarizeOverflow(overflow []historyUnit, prevTop *sessionstore.CompactFrame, record sessionstore.SessionContextRecord) string {
	var builder strings.Builder
	if len(record.TaskStack) > 0 {
		top := record.TaskStack[len(record.TaskStack)-1]
		builder.WriteString("任务目标: ")
		builder.WriteString(top.Objective)
		builder.WriteByte('\n')
	}
	if len(record.PlanStack) > 0 {
		top := record.PlanStack[len(record.PlanStack)-1]
		builder.WriteString("计划: ")
		builder.WriteString(top.Title)
		builder.WriteString(" (")
		builder.WriteString(top.Status)
		builder.WriteString(")\n")
	}
	if prevTop != nil && strings.TrimSpace(prevTop.Summary) != "" {
		builder.WriteString("先前压缩摘要: ")
		builder.WriteString(prevTop.Summary)
		builder.WriteByte('\n')
	}
	builder.WriteString("本轮溢出轮次: ")
	builder.WriteString(fmt.Sprintf("%d 个完整协议单元", len(overflow)))
	builder.WriteByte('\n')
	for _, unit := range overflow {
		builder.WriteString(renderUnitLine(unit.messages))
	}
	return strings.TrimSpace(builder.String())
}

// renderUnitLine 渲染一个单元的单行摘要（用户输入前 80 字符 + 工具名）。
func renderUnitLine(unit []types.Message) string {
	var builder strings.Builder
	for _, message := range unit {
		switch {
		case message.Role == "user" && message.Content != nil:
			content := *message.Content
			if len(content) > 80 {
				content = content[:80] + "..."
			}
			builder.WriteString("- 用户: ")
			builder.WriteString(content)
			builder.WriteByte('\n')
		case message.Role == "assistant":
			for _, call := range message.ToolCalls {
				builder.WriteString("- 工具调用: ")
				builder.WriteString(call.Function.Name)
				builder.WriteByte('\n')
			}
		case message.Role == "tool" && message.Name != "":
			builder.WriteString("- 工具结果: ")
			builder.WriteString(message.Name)
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

// overflowEvidence 从溢出轮次中的工具结果与任务栈证据提取证据引用。
func overflowEvidence(overflow []historyUnit, record sessionstore.SessionContextRecord) []sessionstore.EvidenceRef {
	var evidence []sessionstore.EvidenceRef
	for _, unit := range overflow {
		for _, message := range unit.messages {
			if message.Role == "tool" && message.ToolCallID != "" {
				evidence = append(evidence, sessionstore.EvidenceRef{Ref: "result:" + message.ToolCallID})
			}
		}
	}
	if len(record.TaskStack) > 0 {
		top := record.TaskStack[len(record.TaskStack)-1]
		evidence = append(evidence, top.Evidence...)
	}
	return evidence
}

// projectHistory 投影历史：保留窗口内完整单元及其后的未闭合尾部
// （按原始消息顺序保留，当前输入不因压缩丢失）。压缩帧块由调用方前置。
func projectHistory(history []types.Message, units []historyUnit, n int) []types.Message {
	projected := make([]types.Message, 0, len(history))
	if len(units) > n {
		windowStart := units[len(units)-n].start
		projected = append(projected, history[windowStart:]...)
	}
	return projected
}

// compactFrameMessage 把压缩帧渲染为 working history 中的块消息。
func compactFrameMessage(frame sessionstore.CompactFrame) types.Message {
	encoded, err := json.Marshal(frame)
	if err != nil {
		encoded = []byte("{}")
	}
	content := compactContextMarker + "\n" + string(encoded)
	return types.Message{Role: "user", Content: &content}
}

// ── 轮次单元切分（对齐 sessionstore completeEventUnits 语义）────────

// historyUnit 是历史中的一个完整协议单元（轮），start/end 为原始历史
// 的半开消息索引（用于投影时保留窗口内消息）。
type historyUnit struct {
	messages []types.Message
	start    int
	end      int
}

// chatUnits 把 working history 切分为完整协议单元（轮）：user 轮、
// assistant 文本轮、assistant 工具链轮（按调用 ID 配对 tool 结果）。
// 未闭合的工具链、孤儿 tool 消息与上下文控制块不构成单元。
func (c *seelexContextController) chatUnits(history []types.Message) []historyUnit {
	var units []historyUnit
	for index := 0; index < len(history); {
		message := history[index]
		switch {
		case message.Role == "user" && !isStackContextMarker(message):
			unit, next, complete := userMessageUnit(history, index)
			if complete {
				units = append(units, unit)
			}
			index = next
		case message.Role == "assistant" && len(message.ToolCalls) == 0:
			units = append(units, historyUnit{messages: []types.Message{message}, start: index, end: index + 1})
			index++
		case message.Role == "assistant" && len(message.ToolCalls) > 0:
			unit, next, complete := toolChainUnit(history, index)
			if complete {
				units = append(units, unit)
			}
			index = next
		default:
			index++ // 孤儿 tool / 控制块：不构成单元
		}
	}
	return units
}

// userMessageUnit 用户轮：user + 直到下一个 user 或 assistant 文本回复；
// 工具链在其中闭合时完整。
func userMessageUnit(history []types.Message, start int) (historyUnit, int, bool) {
	unit := historyUnit{messages: []types.Message{history[start]}, start: start, end: start + 1}
	index := start + 1
	for index < len(history) && history[index].Role != "user" {
		message := history[index]
		if message.Role != "assistant" {
			// 非用户/非 assistant（孤儿 tool）→ 该轮不完整，跳到下一个 user。
			return historyUnit{}, nextUserIndex(history, index+1), false
		}
		if len(message.ToolCalls) == 0 {
			unit.messages = append(unit.messages, message)
			unit.end = index + 1
			return unit, index + 1, true
		}
		chain, next, complete := toolChainUnit(history, index)
		if !complete {
			return historyUnit{}, nextUserIndex(history, next), false
		}
		unit.messages = append(unit.messages, chain.messages...)
		unit.end = chain.end
		index = next
	}
	return unit, index, false
}

// toolChainUnit 工具链轮：assistant + 全部调用 ID 配对成功的 tool 结果。
func toolChainUnit(history []types.Message, start int) (historyUnit, int, bool) {
	assistant := history[start]
	wanted := make(map[string]struct{}, len(assistant.ToolCalls))
	for _, call := range assistant.ToolCalls {
		if call.ID == "" {
			return historyUnit{}, start + 1, false
		}
		if _, duplicate := wanted[call.ID]; duplicate {
			return historyUnit{}, start + 1, false
		}
		wanted[call.ID] = struct{}{}
	}
	unit := historyUnit{messages: []types.Message{assistant}, start: start, end: start + 1}
	seen := make(map[string]struct{}, len(wanted))
	index := start + 1
	for index < len(history) && len(seen) < len(wanted) {
		message := history[index]
		if message.Role != "tool" {
			break
		}
		if _, ok := wanted[message.ToolCallID]; !ok {
			break
		}
		if _, duplicate := seen[message.ToolCallID]; duplicate {
			break
		}
		seen[message.ToolCallID] = struct{}{}
		unit.messages = append(unit.messages, message)
		unit.end = index + 1
		index++
	}
	return unit, index, len(seen) == len(wanted)
}

func nextUserIndex(history []types.Message, start int) int {
	for start < len(history) && history[start].Role != "user" {
		start++
	}
	return start
}
