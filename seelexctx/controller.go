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
// 在 95% 等）。默认 8/95/98/80 由 Limits.WithDefaults 给出（见 limits.go）。
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

// SoftThreshold 软阈值（limits.context_soft_percent，默认 95%）。
func (p ContextWindowPolicy) SoftThreshold() int { return p.Budget() * p.SoftPercent / 100 }

// HardThreshold 硬阈值（limits.context_hard_percent，默认 98%）。
func (p ContextWindowPolicy) HardThreshold() int { return p.Budget() * p.HardPercent / 100 }

// TargetAfterCompaction 压缩目标（limits.context_target_percent，默认 80%）。
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
//
// 压缩编排（窗口推导 / 压缩帧 / ReplaceHistory）已于 2026-09-30 整体移出控制器：
// 压缩是上下文压缩流程里**产出元数据**的一步，折完必须由模型按章节写出读后感，
// 只有装配层（application/core/context_runtime，下一轮开始前那次）做得到。
// 控制器因此只剩一件事——超大工具结果的兜底归档，依赖也就是归档器与它的字符预算；
// 原先的 Policy/Window/Tokens/Budget/Stacks/Turns/SessionIDProvider/Compaction/
// FrameCarryTokens 九个注入项随压缩编排一并摘除，不留"留着但没人读"的配置面。
type ControllerOptions struct {
	// Archive 超大工具结果归档（nil → 内存归档）。控制器只做兜底归档：
	// 归档器按调用 ID 幂等，与 processor 共用同一份实现时不会重复入库。
	Archive ToolResultArchiver

	// MaxToolResultChars 超大工具结果判定（≤0 → seelex 生效默认
	// DefaultToolResultLimit()，与 processor / application.core 同源）。
	MaxToolResultChars int

	// Stacks 会话级压缩栈（nil → 内存态）。控制器自己不再折帧，但栈顶帧的
	// From 是"溢出起点"的去重基准（见 chatUnits / compactedUnitBase）：
	// 窗口外轮次的划分要接着上一次压缩的落点算，否则同一段内容会被反复计入
	// 溢出区间。
	Stacks CompactStackStore
}

// seelexContextController 实现 seelectx.ContextController。
type seelexContextController struct {
	opts ControllerOptions
}

// NewContextController 构造 seelex 上下文控制器。
func NewContextController(opts ControllerOptions) seelectx.ContextController {
	if opts.Archive == nil {
		opts.Archive = NewInMemoryToolResultArchiver()
	}
	if opts.Stacks == nil {
		opts.Stacks = &memoryCompactStack{}
	}
	return &seelexContextController{opts: opts}
}

// Handle 实现 seelectx.ContextController。
//
// 循环内不再压缩对话（2026-09-30 起，见包文档）：压缩是上下文压缩流程里**产出
// 元数据**的一步，折完必须由模型按章节写出读后感；而这一步只有装配层做得到
// ——它手里握着上一次真实请求的原件，能接着发起摘要调用。回合内控制器拿到的只有
// 引擎工作历史（system/项目/记忆/前缀栈/工具面都还没进去），叫不动模型写读后感，
// 折出来只能是"没有正文的薄记录"：模型看不到被折内容、要 search_history 回读，
// 而且请求开头被改写，provider 的前缀缓存整段作废。窗口越线交给装配层在
// 下一轮开始前处理（application/core/context_runtime）。
//
// 这里保留的动作只有一个：超大工具结果的兜底归档（processor 路径之外的保险；
// 归档器按调用 ID 幂等）。归档不改变历史，只把原文外置成引用。
func (c *seelexContextController) Handle(ctx context.Context, ev seelectx.ContextEvent) (seelectx.ContextDecision, error) {
	if ev.Kind != seelectx.ContextAfterTool || !c.oversizedTool(ev.Tool) {
		return seelectx.ContextDecision{}, nil
	}
	if _, err := c.opts.Archive.Store(ctx, ev.Tool.CallID, ev.Tool.Name, ev.Tool.Raw); err != nil {
		return seelectx.ContextDecision{}, fmt.Errorf("seelexctx: archive oversized tool result %q: %w", ev.Tool.Name, err)
	}
	return seelectx.ContextDecision{}, nil
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

// renderUnitLine 渲染一个单元的单行摘要（用户输入前 80 字符 + 工具名）。
// maxUnitPreviewRunes 是轮次行预览的 rune 上限：本地压缩是**索引**，每轮只留
// 一眼可辨的首段正文（用户问题与助手答复各一行），完整原文经 read_compressed_turn
// / search_history 回读。
//
// 按 rune 而不是 byte 截断：被折正文以中文为主，按 byte 切会在多字节字符中间断开
// （预览里出现半个汉字），截断点还随内容语言漂移——同一段内容换个措辞就变长短。
// 80 与旧口径一致（原 user 行就是"前 80 字符"）。
const maxUnitPreviewRunes = 80

// renderUnitLine 渲染一个单元的可读索引行：用户输入与助手答复各取首段预览
// （rune 上限 maxUnitPreviewRunes），工具只出调用名/工具名。
//
// 助手侧正文此前**完全不进帧**（只出工具调用名），于是被折轮次在模型可见面上
// 只剩"某人问了什么"，回答了什么一个字都没有——本地压缩的 Chapter 2 是模型唯一
// 能看到的被折内容（assembler 只渲染栈顶帧 Chapter 2），压缩因此变成"压缩掉上下文"
// 而不是"总结上下文"（2026-09-30 重启恢复现场：重启后模型对早先对话只剩索引）。
// 把助手答复的首段一并留下，压缩产物才既有定位（索引）又有内容（可读首段）。
//
// 这条渲染路径现在只剩装配层的降级分支在用（摘要器不可用时 chapter2Node 落本地
// 压缩）：回合内控制器已不折帧（2026-09-30），但"折出来的东西必须让人和模型看得见
// 内容"这条要求不变。
func renderUnitLine(unit []types.Message) string {
	var builder strings.Builder
	appendPreview := func(label, content string) {
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		builder.WriteString(label)
		builder.WriteString(truncateRunes(content, maxUnitPreviewRunes))
		builder.WriteByte('\n')
	}
	for _, message := range unit {
		switch message.Role {
		case "user":
			if message.Content != nil {
				appendPreview("- 用户: ", *message.Content)
			}
		case "assistant":
			if message.Content != nil {
				appendPreview("- 助手: ", *message.Content)
			}
			for _, call := range message.ToolCalls {
				builder.WriteString("- 工具调用: ")
				builder.WriteString(call.Function.Name)
				builder.WriteByte('\n')
			}
		case "tool":
			if message.Name != "" {
				builder.WriteString("- 工具结果: ")
				builder.WriteString(message.Name)
				builder.WriteByte('\n')
			}
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
