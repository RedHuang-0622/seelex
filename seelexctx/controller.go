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
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/seelexctx/tokens"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// CompactContextMarker 标记压缩帧块（窗口外压缩产物，进入 working history；
// 跨包保留/清理协议用）。
const CompactContextMarker = "<!-- seelex:compact-context:v1 -->"

// compactContextMarker 兼容别名（包内引用）。
const compactContextMarker = CompactContextMarker

// checkpointMarker 是旧应用侧任务检查点标记（替换历史时清理）。
const checkpointMarker = "<!-- seelex:context-checkpoint:v1 -->"

// ActivePlanContextMarker 是应用侧 plan 尾部消息前缀（context-prefix-chain）：
// plan 消息贴近当前输入、不参与压缩、不作为记忆查询或轮次单元。
const ActivePlanContextMarker = "<!-- seelex:active-plan:v1 -->"

// ContextWindowPolicy 软/硬阈值与安全预留。比例**全部来自 limits 配置段**
// （context_soft_percent / context_hard_percent / context_target_percent /
// context_safety_reserve_divisor），与 application 层 newContextBudget 读同一份
// 配置：同一个"软阈值"在装配层与回合内控制器只能有一个来源，否则用户调低
// context_soft_percent 后，回合内仍按出厂比例压（报表说 60% 已压过、控制器还
// 在 75% 等）。默认 8/75/90/60 由 Limits.WithDefaults 给出（见 limits.go）。
type ContextWindowPolicy struct {
	Window         int // provider 上下文窗口
	OutputReserve  int // 单次输出预留
	SafetyReserve  int // 安全保留区
	ReservedTokens int // system prompt + 栈块固定预留（0 → 用 SafetyReserve）
	ConfigRounds   int // 显式 window.rounds（0 = 未配置）
	// 以下四个比例/除数是 limits 生效值（构造时经 WithDefaults 归一，恒 > 0）。
	SafetyReserveDivisor int // 安全预留除数（预算 = window − output − window/divisor）
	SoftPercent          int // 软压缩线（占预算 %）
	HardPercent          int // 硬阈值线（占预算 %）
	TargetPercent        int // 压缩后目标（占预算 %）
}

// NewContextWindowPolicy 按 window/outputReserve + limits 生效值构造阈值策略
// （与 newContextBudget 同款计算：safety = window/context_safety_reserve_divisor）。
// limits 传零值不构成"阈值 0"：先经 WithDefaults 归一，未配置字段回退默认比例。
func NewContextWindowPolicy(window, outputReserve int, limits Limits) ContextWindowPolicy {
	settings := limits.WithDefaults()
	safetyReserve := window / settings.ContextSafetyReserveDivisor
	if safetyReserve < 0 {
		safetyReserve = 0
	}
	return ContextWindowPolicy{
		Window: window, OutputReserve: outputReserve, SafetyReserve: safetyReserve,
		SafetyReserveDivisor: settings.ContextSafetyReserveDivisor,
		SoftPercent:          settings.ContextSoftPercent,
		HardPercent:          settings.ContextHardPercent,
		TargetPercent:        settings.ContextTargetPercent,
	}
}

// Budget 返回可用于请求的 token 预算。
func (p ContextWindowPolicy) Budget() int { return p.Window - p.OutputReserve - p.SafetyReserve }

// SoftThreshold 软阈值（limits.context_soft_percent，默认 75%）。
func (p ContextWindowPolicy) SoftThreshold() int { return p.Budget() * p.SoftPercent / 100 }

// HardThreshold 硬阈值（limits.context_hard_percent，默认 90%）。
func (p ContextWindowPolicy) HardThreshold() int { return p.Budget() * p.HardPercent / 100 }

// TargetAfterCompaction 压缩目标（limits.context_target_percent，默认 60%）。
func (p ContextWindowPolicy) TargetAfterCompaction() int { return p.Budget() * p.TargetPercent / 100 }

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

// ConservativeTokenCounter 脚本感知的保守估算（seelexctx/tokens），
// 替代旧 len/3 字节估算。
type ConservativeTokenCounter struct{}

// Name 实现 TokenCounter。
func (ConservativeTokenCounter) Name() string { return "conservative-v1" }

// CountText 实现 TokenCounter。
func (ConservativeTokenCounter) CountText(value string) int { return tokens.Count(value) }

// CountMessage 实现 TokenCounter。
func (ConservativeTokenCounter) CountMessage(message types.Message) int {
	return tokens.CountMessage(message)
}

// CountHistory 实现 TokenCounter。
func (ConservativeTokenCounter) CountHistory(history []types.Message) int {
	return tokens.CountHistory(history)
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

	// Turns 压缩轮次原文归档（nil → 不持久化原文，压缩不可读回）。
	// 归档后帧 Evidence 携带读回句柄（ref），模型可经 read_compressed_turn
	// 工具读回原文——压缩丢失可逆。
	Turns TurnArchiver

	// Stacks 会话级压缩栈（nil → 内存态）。
	Stacks CompactStackStore

	// SessionIDProvider 提供当前会话 ID 用于压缩帧 SegmentID 溯源
	// （每次压缩时动态取值，会话切换后仍溯源到正确会话；nil → 无前缀）。
	SessionIDProvider func() string

	// Compaction 压缩 DAG 执行器（docs/2026-09-06-compaction-dag/design.md
	// §4；nil → 旧 buildCompactFrame 本地折叠路径）。注入后压缩走
	// select_range → chapter1/chapter2 → merge 的 workplan 图，成功取帧；
	// Chapter 2 失败由 DAG 内部回退本地折叠（详设 §4.5）。
	Compaction *CompactionDAG

	// MaxToolResultChars 超大工具结果判定（≤0 → seelex 生效默认
	// DefaultToolResultLimit()，与 processor / application.core 同源）。
	MaxToolResultChars int

	// FrameCarryTokens 是帧摘要传递上限（limits.context_frame_carry_tokens；
	// ≤0 → DefaultFrameCarryTokens）：本地折叠把上一栈顶帧的 Chapter 2 正文并入
	// 新帧时的并入量上限，超出退化为锚点（见 CarryPreviousChapter2）。
	FrameCarryTokens int
}

// seelexContextController 实现 seelectx.ContextController。
type seelexContextController struct {
	opts ControllerOptions
	mu   sync.Mutex
	// lastCompactedTo 上次压缩帧的累计 To（ChatQueue 单元索引）：
	// 本次帧 To 不大于它时说明无新溢出（去重基准是"是否有新溢出内容"，
	// 而非溢出批次尺寸——同尺寸连续批次会跳过真实新溢出，见审计 R2）。
	lastCompactedTo int
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
	// lastCompactedTo 初值 -1：首帧 To 可能为 0（溢出 1 单元），
	// 不能与"从未压缩"的 0 初值混淆而被去重误杀。
	return &seelexContextController{opts: opts, lastCompactedTo: -1}
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
	return DefaultToolResultLimit()
}

// frameCarryTokens 返回帧摘要传递上限（≤0 → DefaultFrameCarryTokens）。
func (c *seelexContextController) frameCarryTokens() int {
	if c.opts.FrameCarryTokens > 0 {
		return c.opts.FrameCarryTokens
	}
	return DefaultFrameCarryTokens
}

// policy 返回生效的阈值策略（Budget 提供时用账号窗口/输出预留覆盖输入）。
//
// 覆盖只换 Window/OutputReserve 这两个账号输入，比例与除数沿用构造时注入的
// limits 生效值：在这里重建一份策略会把配置丢掉、退回出厂默认，配置就只对着
// 一个触发层生效（装配层跟着改、回合内控制器仍按 75% 等）。
func (c *seelexContextController) policy() ContextWindowPolicy {
	policy := c.opts.Policy
	if policy.Window <= 0 {
		policy.Window = DefaultMaxTokens
	}
	if c.opts.Budget != nil {
		if contextTokens := c.opts.Budget.ContextTokens(); contextTokens > 0 {
			policy.Window = contextTokens
			policy.OutputReserve = c.opts.Budget.MaxOutputTokens()
			policy.SafetyReserve = contextTokens / policy.SafetyReserveDivisor
			if policy.SafetyReserve < 0 {
				policy.SafetyReserve = 0
			}
		}
	}
	if policy.OutputReserve <= 0 {
		policy.OutputReserve = policy.Window / policy.SafetyReserveDivisor
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
	lastCompactedTo := c.lastCompactedTo
	c.mu.Unlock()
	// 去重基准 = 累计边界（帧 To 单调递增的 ChatQueue 单元索引）：
	// 本次帧没有覆盖到上次压缩点之后的任何新单元 → 无新溢出。
	// 提前检查（用同一累计公式预测 To）：无新溢出时不执行压缩 DAG，
	// 避免无谓的前缀重放模型调用（详设 §4.6 去重语义不变）。
	predictedTo := c.predictedFrameTo(overflow)
	if predictedTo <= lastCompactedTo {
		return seelectx.ContextDecision{}, nil
	}
	frame, err := c.buildCompactionFrame(ctx, overflow, ev)
	if err != nil {
		return seelectx.ContextDecision{}, fmt.Errorf("seelexctx: build compact frame: %w", err)
	}
	// 后置去重（并发/快照漂移兜底）：build 后上次压缩点可能已前进，帧没有
	// 覆盖任何新溢出内容 → 跳过，保持旧路径的原子语义。
	c.mu.Lock()
	lastCompactedTo = c.lastCompactedTo
	c.mu.Unlock()
	if frame.To <= lastCompactedTo {
		return seelectx.ContextDecision{}, nil
	}
	// 原文归档（可选注入）：溢出轮次原文持久化，帧 Evidence 携带读回
	// 句柄，Summary 提示 read_compressed_turn —— 压缩丢失可逆。
	if c.opts.Turns != nil {
		ref, err := c.opts.Turns.StoreTurn(ctx, frame.SegmentID, overflowMessages(overflow))
		if err != nil {
			return seelectx.ContextDecision{}, fmt.Errorf("seelexctx: archive compressed turns: %w", err)
		}
		frame.Evidence = append(frame.Evidence, sessionstore.EvidenceRef{
			Ref:     ref,
			Summary: "compressed turns original (read_compressed_turn)",
		})
		frame.Summary += fmt.Sprintf("\n已压缩轮次原文可经 read_compressed_turn(segment_id=%s) 读回", frame.SegmentID)
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
	c.lastCompactedTo = frame.To
	c.mu.Unlock()
	return seelectx.ContextDecision{ReplaceHistory: true, History: projected}, nil
}

// predictedFrameTo 预测新帧 To = 末个被压单元的**已记录区号**（单元自带
// ordinal，见 chatUnits/compactedUnitBase）；不再用 len(overflow) 推算终点。
func (c *seelexContextController) predictedFrameTo(overflow []historyUnit) int {
	if len(overflow) == 0 {
		return -1
	}
	return overflow[len(overflow)-1].ordinal
}

// buildCompactionFrame 生成压缩帧：注入 CompactionDAG 时走 workplan 图
// （成功取帧，失败逐级兜底），否则用本地 buildCompactFrame（兼容旧调用方
// 与测试）。两者都产出带链锚字段/request 索引的契约帧。
func (c *seelexContextController) buildCompactionFrame(
	ctx context.Context,
	overflow []historyUnit,
	ev seelectx.ContextEvent,
) (sessionstore.CompactFrame, error) {
	if c.opts.Compaction != nil {
		input := CompactionInput{
			Record:   c.opts.Stacks.Snapshot(),
			Messages: overflowMessages(overflow),
			History:  ev.History,
			Kind:     CompactFoldOverflow,
		}
		return c.opts.Compaction.Execute(ctx, input)
	}
	return c.buildCompactFrame(overflow)
}

// buildCompactFrame 构造压缩帧：Summary 合并上一栈顶帧与当前溢出内容
// （栈顶自足 = 该时刻窗口外全部轮次的综合摘要）。
//
// From/To 语义：区间**记录**自被压单元自身的区号（historyUnit.ordinal，
// 由 chatUnits 以已记录帧边界为基准编号），不再用 len(overflow) /
// prevTop.To+len(overflow) 推算终点——窗口外单元不保证从 0 连续
// （投影、覆盖缺口、冷恢复），推算值与事实会漂移。合并帧的 From 沿用
// 已记录的前帧起点（综合摘要覆盖从 From 到 To 的连续段），To 取末个被压
// 单元的区号。消费方（覆盖账簿/UI/fork）因此可把帧映射回持久化 ChatQueue。
func (c *seelexContextController) buildCompactFrame(overflow []historyUnit) (sessionstore.CompactFrame, error) {
	record := c.opts.Stacks.Snapshot()
	var prevTop *sessionstore.CompactFrame
	if len(record.CompactStack) > 0 {
		top := record.CompactStack[len(record.CompactStack)-1]
		prevTop = &top
	}
	segmentID := fmt.Sprintf("compact-%d", time.Now().UnixMilli())
	if c.opts.SessionIDProvider != nil {
		if sessionID := c.opts.SessionIDProvider(); sessionID != "" {
			segmentID = fmt.Sprintf("compact-%s-%d", sessionID, time.Now().UnixMilli())
		}
	}
	to := -1
	from := 0
	if len(overflow) > 0 {
		from = overflow[0].ordinal
		to = overflow[len(overflow)-1].ordinal
	}
	if prevTop != nil {
		from = prevTop.From
	}
	requestFrom, requestTo := ChatQueueRequestLabels(from, to)
	summary, carry := c.summarizeOverflow(overflow, prevTop, record)
	frame := sessionstore.CompactFrame{
		SegmentID:     segmentID,
		From:          from,
		To:            to,
		RequestFrom:   requestFrom,
		RequestTo:     requestTo,
		Summary:       RenderFrameSummary(RenderAnchorChapter(prevTop), summary),
		SummarySource: CompactSummarySourceLocal,
		AnchorSource:  AnchorSourceWithCarry(FrameAnchorSource(prevTop), carry),
		Evidence:      append(overflowEvidence(overflow, record), CarryEvidence(carry)...),
		CompressedAt:  time.Now(),
	}
	if prevTop != nil {
		// 链锚点：只指向前驱（SegmentID/request/一句话），不复制前驱全文。
		frame.PrevSegmentID = prevTop.SegmentID
		frame.PrevRequestFrom = prevTop.RequestFrom
		frame.PrevRequestTo = prevTop.RequestTo
		frame.PrevSummaryOneLine = OneLineSummary(*prevTop)
	}
	return frame, nil
}

// summarizeOverflow 生成综合摘要：栈帧的 goal/plan/evidence（片段闭合压缩
// 保留目标/计划/证据）+ 上一栈顶摘要 + 溢出轮次代表性内容。第二个返回值是
// 「上一帧正文并入」的决策事实（帧摘要传递上限，见 CarryPreviousChapter2）。
func (c *seelexContextController) summarizeOverflow(overflow []historyUnit, prevTop *sessionstore.CompactFrame, record sessionstore.SessionContextRecord) (string, CarryDiagnostics) {
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
	previous, carry := CarryPreviousChapter2(prevTop, c.frameCarryTokens())
	if previous != "" {
		builder.WriteString("先前压缩摘要: ")
		builder.WriteString(previous)
		builder.WriteByte('\n')
	}
	builder.WriteString("本轮溢出轮次: ")
	builder.WriteString(fmt.Sprintf("%d 个完整协议单元", len(overflow)))
	builder.WriteByte('\n')
	for _, unit := range overflow {
		builder.WriteString(renderUnitLine(unit.messages))
	}
	return strings.TrimSpace(builder.String()), carry
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

// projectHistory 投影历史：从最后一个溢出单元的结束处起保留
// （= 溢出区与窗口之间的非单元消息（不完整工具链/孤儿）随窗口保留，
// 不再静默丢弃，见审计 R3）+ 窗口内完整单元及其后的未闭合尾部。
// 压缩帧块由调用方前置。
func projectHistory(history []types.Message, units []historyUnit, n int) []types.Message {
	projected := make([]types.Message, 0, len(history))
	if len(units) > n {
		overflowLastEnd := units[len(units)-n-1].end
		projected = append(projected, history[overflowLastEnd:]...)
	}
	return projected
}

// compactFrameMessage 把压缩帧渲染为 working history 中的块消息。
// 只携带 marker + 帧定位（segment/From/To）——摘要正文由 Assembler 的
// 栈顶 compact 块渲染（RenderStackBlocks），避免同一摘要双重投喂
// （审计 R5）；帧块消息本身可被下次压缩的 removeContextMarkers 清理。
func compactFrameMessage(frame sessionstore.CompactFrame) types.Message {
	location := fmt.Sprintf("segment=%s from=%d to=%d", frame.SegmentID, frame.From, frame.To)
	content := compactContextMarker + " " + location
	return types.Message{Role: "user", Content: &content}
}

// ── 轮次单元切分（对齐 sessionstore completeEventUnits 语义）────────

// historyUnit 是历史中的一个完整协议单元（轮），start/end 为原始历史
// 的半开消息索引（用于投影时保留窗口内消息）。
type historyUnit struct {
	messages []types.Message
	start    int
	end      int
	// ordinal 是该单元在累计 ChatQueue 单元序列里的区号（记录值）。压缩帧的
	// From/To 直接取被压单元的区号记录，而不是用单元条数推算终点——窗口外的
	// 单元不一定从 0 连续（投影/覆盖缺口/冷恢复），推算值会与事实漂移。
	ordinal int
}

// chatUnits 把 working history 切分为可见协议轮次单元：user 轮、assistant
// 文本轮、assistant 工具链轮（按调用 ID 配对 tool 结果）。孤儿 tool 消息与
// 上下文控制块不构成单元。中断（残缺）工具链轮与未回复的 user 请求仍构成
// 开放单元 —— UI 可见的轮次不得因窗口/溢出统计而消失；缺失 tool 结果由
// 装配层请求前补齐。
// chatUnits 把 working history 切分为可见协议单元（按 user 轮 / assistant
// 文本轮 / assistant 工具链轮）；baseOrdinal 是首个单元的累计区号（= 已有
// 压缩帧覆盖的单元数，冷启动 0），单元按顺序记录自己的区号。
//
// 单元编号只是"记录"：压缩帧的 From/To 取被压单元的区号，不再由单元条数推算。
func chatUnits(history []types.Message, baseOrdinal int) []historyUnit {
	var units []historyUnit
	for index := 0; index < len(history); {
		message := history[index]
		switch {
		case message.Role == "user" && !isStackContextMarker(message):
			unit, next, _ := userMessageUnit(history, index)
			if len(unit.messages) > 0 {
				units = append(units, unit)
			}
			index = next
		case message.Role == "assistant" && len(message.ToolCalls) == 0:
			units = append(units, historyUnit{messages: []types.Message{message}, start: index, end: index + 1})
			index++
		case message.Role == "assistant" && len(message.ToolCalls) > 0:
			unit, next, _ := toolChainUnit(history, index)
			// 完整链与残缺（中断）链都保留为单元（残缺部分等待装配修复），
			// index 落在链断裂点续扫 —— 不整体作废、不连坐跳到下一个 user。
			if len(unit.messages) > 0 {
				units = append(units, unit)
			}
			index = next
		default:
			index++ // 孤儿 tool / 控制块：不构成单元
		}
	}
	// 单元按顺序记录自己的累计区号（基准 + 顺序下标）：压缩帧的 From/To 用它。
	for index := range units {
		units[index].ordinal = baseOrdinal + index
	}
	return units
}

// chatUnits 方法版：区号基准取已记录帧边界的下一个区号（见 compactedUnitBase）。
func (c *seelexContextController) chatUnits(history []types.Message) []historyUnit {
	return chatUnits(history, c.compactedUnitBase())
}

// compactedUnitBase 返回累计单元区号基准 = 已被压缩帧覆盖的单元数（栈顶
// To+1），冷启动 0。基准取自**已记录**的帧边界，不由当前对话轮数推算。
func (c *seelexContextController) compactedUnitBase() int {
	if c.opts.Stacks == nil {
		return 0
	}
	record := c.opts.Stacks.Snapshot()
	if len(record.CompactStack) == 0 {
		return 0
	}
	return record.CompactStack[len(record.CompactStack)-1].To + 1
}

// userMessageUnit 用户轮：user + 直到下一个 user 或 assistant 文本收尾；
// 工具链（完整或中断）并入该轮，中断链的缺失结果由装配层补齐。
func userMessageUnit(history []types.Message, start int) (historyUnit, int, bool) {
	unit := historyUnit{messages: []types.Message{history[start]}, start: start, end: start + 1}
	index := start + 1
	for index < len(history) && history[index].Role != "user" {
		message := history[index]
		if message.Role != "assistant" {
			// 孤儿 tool / 异常角色：轮在此终止，产出已保留内容；孤儿消息由
			// 外层 default 跳过。
			return unit, index, true
		}
		if len(message.ToolCalls) == 0 {
			unit.messages = append(unit.messages, message)
			unit.end = index + 1
			return unit, index + 1, true
		}
		chain, next, _ := toolChainUnit(history, index)
		// 工具链完整或残缺（中断）都并入该轮；next 落在链断裂点，后续同轮
		// 文本/下一 user 不会被跳转丢弃。
		unit.messages = append(unit.messages, chain.messages...)
		if chain.end > unit.end {
			unit.end = chain.end
		}
		index = next
	}
	// 到达下一个 user 或流末：产出开放单元（残缺工具链收尾 / 无回复的
	// user 请求等可见轮次 —— 不因单元切分从窗口/溢出统计中消失）。
	return unit, index, true
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

// overflowMessages 展平溢出单元的消息（保留原始顺序；单元内消息不重复）。
func overflowMessages(overflow []historyUnit) []types.Message {
	total := 0
	for _, unit := range overflow {
		total += len(unit.messages)
	}
	messages := make([]types.Message, 0, total)
	for _, unit := range overflow {
		messages = append(messages, unit.messages...)
	}
	return messages
}
