package context_runtime

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/core/internal/state"
)

// MissingHistoryContent 是空 content 修复文本（provider 非空要求）。
const MissingHistoryContent = "[Seelex recovery note: the previous message had no text after an interrupted request; its original content is unavailable.]"

// ToolCallHistoryContent 是**历史版本**里 assistant 工具调用缺正文的修复文本。
// 当前实现不再写入它：工具轮 assistant 正文在 provider 投影里归零
// （见 RepairEmptyHistoryContent）——wire 上本来就没有这段正文，补占位会让
// 重投影字节与已发出字节分叉。该常量保留用于识别旧会话记录里已落盘的占位文本
// （IsProviderOnlyHistoryContent），不得当作真实正文渲染或投影。
const ToolCallHistoryContent = "[Seelex recovery note: the assistant issued the recorded tool call(s); the original accompanying text is unavailable.]"

// InterruptedToolResultPrefix 是合成 tool 结果正文的前缀（见
// RepairInterruptedToolChains）：标识该结果不是真实工具输出，而是对
// 中断工具调用的协议占位（UI 不渲染、不得被当作成功证据）。
const InterruptedToolResultPrefix = "[Seelex recovery note: interrupted tool call"

// interruptedToolResultContent 生成缺失 tool 结果的协议占位正文：明示该
// 调用在记录到结果前被中断、是否已执行未知 —— 不谎称执行成功，模型据此
// 校验副作用或重新发起。
func interruptedToolResultContent(name string) string {
	return InterruptedToolResultPrefix + " \"" + name + "\" may not have executed; its result was lost before recording. Verify side effects or re-issue the call before continuing.]"
}

// HistoryCoordinator 拥有 provider 缓存归一化（空内容修复 + 中断工具链
// 配对修复）。
type HistoryCoordinator struct {
	*state.Core
}

// NewHistoryCoordinator 构造 history 域协调器。
func NewHistoryCoordinator(core *state.Core) *HistoryCoordinator {
	return &HistoryCoordinator{Core: core}
}

// PrepareProviderHistory 使每条持久化消息对拒绝空 content 的 provider 安全
// （工具调用保留，仅恢复缺失的说明文本；活跃会话兼容包装）。
func (h *HistoryCoordinator) PrepareProviderHistory() error {
	return h.PrepareProviderHistoryFor(h.Deps.Engine.SessionID())
}

// PrepareProviderHistoryFor 使每条持久化消息对拒绝空 content 的 provider
// 安全：先补齐中断（残缺）工具链缺失的 tool 结果（协议占位），再恢复空
// 正文。sessionID 指明目标会话。
//
// 它与压缩同源取历史、同源写回（会话路由端口）：装配路径里它紧跟在压缩之后，
// 两边写的是同一份工作历史；写回在忙会话上由引擎排队到下一个检查点，因此不会
// 因为"这一轮正在跑"而丢掉。
func (h *HistoryCoordinator) PrepareProviderHistoryFor(sessionID string) error {
	history := h.sessionHistory(sessionID)
	prepared, repaired := RepairInterruptedToolChains(history)
	if repaired {
		if err := h.replaceSessionHistory(sessionID, prepared); err != nil {
			return fmt.Errorf("repair interrupted tool chains: %w", err)
		}
	}
	final, repairedContent := RepairEmptyHistoryContent(prepared)
	if repairedContent {
		if err := h.replaceSessionHistory(sessionID, final); err != nil {
			return fmt.Errorf("repair empty provider history content: %w", err)
		}
	}
	return nil
}

// PrepareNewHistoryContentFor 仅修复引擎历史中新 append 的空正文消息
// （OnIterationComplete 中间态专用）：新 assistant/tool 记录的工具结果可能
// 尚未写入（活跃 ReAct 中），残缺链的配对修复留到装配/请求前由
// PrepareProviderHistoryFor 执行 —— 避免把"即将执行"的工具调用误判为
// 中断丢失而注入占位。
func (h *HistoryCoordinator) PrepareNewHistoryContentFor(sessionID string) error {
	history := h.sessionHistory(sessionID)
	prepared, repaired := RepairEmptyHistoryContent(history)
	if !repaired {
		return nil
	}
	return h.replaceSessionHistory(sessionID, prepared)
}

// replaceEngineHistory 会话内替换指定会话引擎历史（会话路由引擎用
// ReplaceHistoryFor，不切活跃；否则回退契约 ReplaceHistory）。
func (h *HistoryCoordinator) replaceEngineHistory(sessionID string, history []contract.EngineMessage) error {
	if routed, ok := h.Deps.Engine.(contract.SessionChatEngine); ok {
		return routed.ReplaceHistoryFor(sessionID, history)
	}
	return h.Deps.Engine.ReplaceHistory(sessionID, history)
}

// engineHistory 返回指定会话引擎历史（会话路由引擎用 HistoryFor，否则活跃
// 引擎）。
func (h *HistoryCoordinator) engineHistory(sessionID string) []contract.EngineMessage {
	if routed, ok := h.Deps.Engine.(contract.SessionChatEngine); ok {
		return routed.HistoryFor(sessionID)
	}
	return h.Deps.Engine.History()
}

// toolCallPairing 是 RepairInterruptedToolChains 的配对索引。
//
// 配对按 call_id 的**出现次序**做（第 k 次声明 ↔ 该 ID 的第 k 条结果），而不是
// "首个声明认领这个 ID 的全部结果"。现场（用户报告：session loop 0 直接
// HTTP 400 `An assistant message with 'tool_calls' must be followed by tool
// messages responding to each 'tool_call_id'. (insufficient tool messages
// following tool_calls message)`）的形状正是后者判错的：
//
//	assistant(tool_calls c1) → tool(c1) → assistant(文本) → user → assistant(tool_calls c1) → …
//
// 同一个 call_id 被两次声明各宣告一次（重试/中断后重发复用了 ID）。旧口径把第二条
// 同名结果当"重复行"丢掉、也不认为第二次声明缺回执 → 后一条声明在自己的相邻结果
// 块里一条结果都没有，provider 数下来就是 "insufficient tool messages following
// tool_calls message"。逐次配对后，第二条同名结果正好是后一次声明的回执，两条
// 声明各自拿到自己的结果块。
//
// 另一类被判成"无法配对、链保持原样"的形状同样会 400：声明里带**空 ID** 的调用
// （provider 按 tool_calls 条数数回执，空 ID 那条永远没有回执）与**行内重复 ID**
// 的调用（记录异常）。两者都在此处剔除，声明数与结果数才对得上。
type toolCallPairing struct {
	// declares 是声明行 → 规整后的调用列表（空 ID 与行内重复 ID 已剔除）。
	declares map[int][]contract.EngineToolCall
	// resultsOf 是声明行 → 该行每个调用对应的结果行号（-1 = 缺结果）。
	resultsOf map[int][]int
	// resultFor 是结果行号 → 它服务的声明行（-1 = 多余结果/孤儿 → 丢弃）。
	resultFor map[int]int
	// names 是 call_id → 工具名（取首个非空）。
	names map[string]string
	// normalized 记录被规整过的声明行（剔了空 ID / 行内重复 ID）。
	normalized map[int]bool
	reorder    bool
}

// toolCallSlot 是"第 k 次声明某个 call_id"的落点：声明行号 + 该行内第几个调用。
type toolCallSlot struct {
	row      int
	position int
}

// emitInPlace 报告某 tool 行能否原样输出：它是所服务声明的结果，且位置已经落在
// 该声明的相邻区间内。缺结果、多余结果、孤儿与乱序行都不满足。
func (p toolCallPairing) emitInPlace(index int) bool {
	row, ok := p.resultFor[index]
	if !ok || row < 0 {
		return false
	}
	return index > row && index <= row+len(p.declares[row])
}

// declarationHasInPlaceResult 报告声明行 index 的结果里是否存在"原地输出"的那
// 一条：决定缺失占位应当紧跟声明输出，还是要等原地结果之后再补。
func (p toolCallPairing) declarationHasInPlaceResult(index int) bool {
	for _, position := range p.resultsOf[index] {
		if position >= 0 && p.emitInPlace(position) {
			return true
		}
	}
	return false
}

// isInterruptedToolResult 报告一行 tool 结果是否为合成占位（不是真实工具输出）。
func isInterruptedToolResult(message contract.EngineMessage) bool {
	return strings.HasPrefix(message.Content, InterruptedToolResultPrefix)
}

func indexToolCallPairing(history []contract.EngineMessage) toolCallPairing {
	pairing := toolCallPairing{
		declares:   make(map[int][]contract.EngineToolCall),
		resultsOf:  make(map[int][]int),
		resultFor:  make(map[int]int),
		names:      make(map[string]string),
		normalized: make(map[int]bool),
	}
	slots := make(map[string][]toolCallSlot)
	for index, message := range history {
		if message.Role != "assistant" || len(message.ToolCalls) == 0 {
			continue
		}
		seen := make(map[string]bool, len(message.ToolCalls))
		calls := make([]contract.EngineToolCall, 0, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			if call.ID == "" || seen[call.ID] {
				// 空 ID 在 provider 的回执计数里是一条永远配不上的宣告；行内
				// 重复 ID 是记录异常（一条调用只有一个结果位置）。两者都剔除，
				// 声明条数与结果条数才对得上。
				pairing.normalized[index] = true
				continue
			}
			seen[call.ID] = true
			calls = append(calls, call)
			slots[call.ID] = append(slots[call.ID], toolCallSlot{row: index, position: len(calls) - 1})
			if _, known := pairing.names[call.ID]; !known {
				pairing.names[call.ID] = call.Name
			}
		}
		if len(calls) == 0 {
			// 整行没有可配对的宣告：退回普通 assistant 文本行。
			pairing.normalized[index] = true
		}
		pairing.declares[index] = calls
		pairing.resultsOf[index] = make([]int, len(calls))
		for position := range pairing.resultsOf[index] {
			pairing.resultsOf[index][position] = -1
		}
	}
	// 结果行按 call_id 分组（保序），再逐次配给同名声明。合成占位被同 ID 的
	// 真结果顶掉：占位只是协议垫片（"该调用结果没记下来"），真结果后到时必须
	// 接替它 —— 否则下一轮"重复结果"规则会把真结果当多余行丢弃，模型永远
	// 只能看到"调用可能没执行"。
	rows := make(map[string][]int)
	placeholders := make(map[string]bool)
	for index, message := range history {
		if message.Role != "tool" {
			continue
		}
		pairing.resultFor[index] = -1
		if message.ToolCallID == "" || len(slots[message.ToolCallID]) == 0 {
			continue // 孤儿结果：没有任何可回应的声明
		}
		rows[message.ToolCallID] = append(rows[message.ToolCallID], index)
		if isInterruptedToolResult(message) {
			placeholders[message.ToolCallID] = true
		}
	}
	for id, positions := range rows {
		serving := positions
		if placeholders[id] {
			// 同 ID 的真结果足够覆盖全部声明时，占位是多余的协议垫片（真结果
			// 后到时必须接替它，否则"重复结果"规则会把真结果当多余行丢掉，
			// 模型永远只看到"调用可能没执行"）。真结果不够覆盖声明时占位要
			// 留下补足槽位——否则每次修复都"丢占位→再补占位"，历史永不定稿。
			real := make([]int, 0, len(positions))
			for _, position := range positions {
				if !isInterruptedToolResult(history[position]) {
					real = append(real, position)
				}
			}
			if len(real) >= len(slots[id]) {
				serving = real
			}
		}
		for occurrence, slot := range slots[id] {
			if occurrence >= len(serving) {
				break
			}
			position := serving[occurrence]
			pairing.resultFor[position] = slot.row
			pairing.resultsOf[slot.row][slot.position] = position
		}
	}
	// 乱序判定：结果必须落在声明行之后的相邻区间内（区间内先后不限，provider
	// 只要求它们紧跟声明）；被别的消息隔开或落在区间外就必须重排。
	for _, results := range pairing.resultsOf {
		for _, position := range results {
			if position >= 0 && !pairing.emitInPlace(position) {
				pairing.reorder = true
			}
		}
	}
	return pairing
}

// RepairInterruptedToolChains 修复历史里的工具链，使它在 provider 的 tool 配对
// 协议下合法：每条 assistant 宣告的 tool_call 都必须在自己的相邻结果块里拿到
// 一条结果，且每条 role=tool 消息都必须有可回应的前一条声明。
//
// 修四类形状：
//
//   - 缺结果（宣告了但全篇没有结果）：在声明处（后缀文本之前）补合成占位；
//   - 乱序结果（assistant(tool_calls c1) → user → tool(c1)）：搬回声明的相邻
//     结果块；
//   - 重复宣告（同一 call_id 被两次声明）：逐次配对（第 k 次声明 ↔ 第 k 条
//     结果），不再把后一条同名结果当"多余行"丢掉 —— 丢了它就是
//     "insufficient tool messages following tool_calls message"；
//   - 无法配对的宣告（空 ID、行内重复 ID、没有任何声明的孤儿结果）：从声明里
//     剔除 / 整行丢弃。
//
// 这是**请求前**的应用侧口径（PrepareProviderHistoryFor，落盘/恢复语义）：
// 缺结果一律补占位。wire 出口（框架侧装配器）另有一套保守口径——它可能发生在
// 工具尚未执行完的活跃 ReAct 中间态，给"即将执行"的调用补占位会让模型重发同一
// 调用（见 seelexctx/repairToolPairing 的 fabricate 参数）。
//
// 同时把**乱序**的结果搬回它所属的 assistant 声明之后。provider 的校验不是
// "历史里存在配对"而是"每条 tool 消息必须紧跟在携带其 tool_calls 的 assistant
// 消息之后"：`assistant(tool_calls c1) → user → tool(c1)` 这种历史会让整次请求
// 400——HTTP 400 `{"error":{"message":"Messages with role 'tool' must be a
// response to a preceding message with 'tool_calls'", ...}}`（2026-09-17 实测）。
// 没有 assistant 宣告的孤儿结果同样无法满足该协议，投影时剔除。合法历史逐字不变
// （前缀缓存口径）。
//
// 幂等：已补齐的链再次执行不会重复插入；后文存在同 ID 结果时不插入（保守，
// 避免破坏既有配对）。合成正文带 InterruptedToolResultPrefix，属于
// provider-only，不渲染为真实工具输出。不修改入参。
func RepairInterruptedToolChains(history []contract.EngineMessage) ([]contract.EngineMessage, bool) {
	pairing := indexToolCallPairing(history)
	prepared := make([]contract.EngineMessage, 0, len(history)+2)
	repaired := len(pairing.normalized) > 0 || pairing.reorder
	// pendingPlaceholders 缓存"本轮缺结果的合成占位"：provider 要求结果块紧跟
	// 声明，占位若插在真结果之前会把真结果挤到非相邻位置，等于把本来合法的请求
	// 变成 400，所以占位要等本单元的原地结果输出之后再补。
	var pendingPlaceholders []contract.EngineMessage
	pendingDeclarationRow := -1 // 占位属于哪个声明（-1 = 无待补占位）
	// delayedFlush 表示占位要等"该声明的原地结果行"输出后再补（该声明同时存在
	// 原地结果与被拉回的结果，直接补会挤掉原地结果）。
	delayedFlush := false
	flushPending := func() {
		if len(pendingPlaceholders) == 0 {
			return
		}
		prepared = append(prepared, pendingPlaceholders...)
		pendingPlaceholders = nil
		pendingDeclarationRow = -1
		delayedFlush = false
	}
	for index, message := range history {
		if message.Role == "tool" {
			// tool 行原地输出的唯一情形：它是所服务声明的结果，且本来就在
			// 声明的相邻区间内。其余一律跳过：乱序行由声明处带出（位置归位、
			// 内容不丢），重复结果与无宣告的孤儿则没有任何前一条消息可回应。
			if !pairing.emitInPlace(index) {
				repaired = true
				continue
			}
			prepared = append(prepared, message)
			// 本行是某个声明里"以后出现的原地结果"：占位必须排在它之后，否则
			// 结果块会被占位隔开（provider 只认紧跟声明的结果）。
			if delayedFlush && index > pendingDeclarationRow &&
				pairing.resultFor[index] == pendingDeclarationRow {
				flushPending()
			}
			continue
		}
		// 新的声明行之前，把上一个声明的待补占位清掉（正常情况下已在下方补过，
		// 这里是防御性兜底，保证占位不会漂到别的声明后面）。
		flushPending()
		if message.Role != "assistant" || len(message.ToolCalls) == 0 {
			prepared = append(prepared, message)
			continue
		}
		// 规整过的声明行按规整结果输出（剔除空 ID / 行内重复 ID）。合法的
		// 声明行原样输出，保证已发出字节的前缀缓存不被改写。
		if pairing.normalized[index] {
			normalized := message
			normalized.ToolCalls = append([]contract.EngineToolCall(nil), pairing.declares[index]...)
			prepared = append(prepared, normalized)
		} else {
			prepared = append(prepared, message)
		}
		wanted := pairing.declares[index]
		if len(wanted) == 0 {
			continue
		}
		// 1) 把散落在别处的同 ID 结果搬回声明之后（乱序 → 相邻）。已经在相邻
		// 区间内、会原地输出的结果不重复带出（否则同一结果出现两次）。
		for _, position := range pairing.resultsOf[index] {
			if position >= 0 && !pairing.emitInPlace(position) {
				prepared = append(prepared, history[position])
			}
		}
		// 2) 没拿到回执的调用补合成占位。
		needPlaceholder := false
		for position, call := range wanted {
			if pairing.resultsOf[index][position] >= 0 {
				continue
			}
			pendingPlaceholders = append(pendingPlaceholders, contract.EngineMessage{
				Role: "tool", ToolCallID: call.ID, Name: pairing.names[call.ID],
				Content: interruptedToolResultContent(pairing.names[call.ID]), ContentSet: true,
			})
			needPlaceholder = true
			repaired = true
		}
		if !needPlaceholder {
			continue
		}
		if pairing.declarationHasInPlaceResult(index) {
			// 该声明还有原地结果要输出：占位必须排在它之后，否则会把结果
			// 挤出声明区间。
			pendingDeclarationRow = index
			delayedFlush = true
			continue
		}
		// 本单元的结果（若有）都已被拉回声明之后 → 占位紧跟着补，不会挤掉
		// 任何行。
		flushPending()
	}
	flushPending()
	return prepared, repaired
}

// RepairEmptyHistoryContent 使历史对拒绝空 content 的 provider 安全
// （纯 reasoning / 其余角色补占位文本），并**归零工具轮 assistant 正文**：
//
// 携带工具调用的 assistant 消息，其正文在 wire 上恒为空 —— 框架构造该消息时
// 直接置 nil（Seele `session/loop.go:564`
// `types.Message{Role:"assistant", Content:nil, ToolCalls: toolCalls}`），
// provider 从未收到这段正文。而 durable 转写里可能带着视图流式正文（回合收尾
// `mergeStreamedToolNarration` 补写，视图/轨迹需要）或空正文：两者都不属于
// **已发出字节**。若让它们进入 provider 投影（补占位 / 保留转写正文），下一轮
// 请求的字节就会与上一轮已发出的字节分叉，provider 前缀缓存自该消息起全部
// 失效（跨轮命中 65.9% → 98.3%，见
// docs/research/2026-09-11-seelex-vs-codex-context-strategy-control-group.md）。
// 因此投影时统一归零，保持「重投影 == 已发出」。
//
// 与框架 wire 行为耦合：本规则成立的前提是"框架在 tool_calls 时丢弃正文"。
// 若 Seele 改为在 wire 上保留工具轮正文，必须同步停止归零，否则分叉方向反转
// ——对照探针里 S-fix / C 两臂（模型假设 wire 保留正文）在本次修复后由
// 98.6% / 98.3% 掉到 74.4% / 46.8%，就是这条耦合的报警器
// （application/core/context_strategy_ab_probe_test.go）。
func RepairEmptyHistoryContent(history []contract.EngineMessage) ([]contract.EngineMessage, bool) {
	prepared := make([]contract.EngineMessage, len(history))
	copy(prepared, history)
	repaired := false
	for index := range prepared {
		message := &prepared[index]
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			// wire 上工具轮 assistant 正文恒为空（框架 Content=nil）：占位与
			// 转写补写的叙述都不属于已发出字节，一律归零（ContentSet=false
			// 与框架的 nil 同义）。工具调用本身原样保留，配对不被破坏。
			if message.Content != "" || message.ContentSet {
				message.Content = ""
				message.ContentSet = false
				repaired = true
			}
			continue
		}
		if strings.TrimSpace(message.Content) != "" {
			continue
		}
		if !message.ContentSet && message.Role == "assistant" && message.ReasoningContent != "" {
			message.Content = MissingHistoryContent
			message.ContentSet = true
			repaired = true
			continue
		}
		if message.Role == "tool" {
			// 工具结果正文就是 wire 上的字节（框架把工具返回值原样作为 tool
			// 消息正文发出）：**空结果在 wire 上也是空**，补占位会让下一轮
			// 重投影 ≠ 已发出 —— 与工具轮 assistant 归零同一条规则。
			// provider 对空 tool 正文的接受已由同回合后续请求实证（生产里
			// 无输出的只读工具是常态）。中断链占位不走这里
			// （RepairInterruptedToolChains 生成非空正文）。
			continue
		}
		if message.Role == "system" || message.Role == "user" || message.Role == "assistant" || message.Role == "tool" {
			message.Content = MissingHistoryContent
			message.ContentSet = true
			repaired = true
		}
	}
	return prepared, repaired
}

// IsProviderOnlyHistoryContent 识别仅用于满足 provider 非空 content 要求的
// 修复文本（非用户创作，恢复后不得渲染为 assistant 回复）。
func IsProviderOnlyHistoryContent(content string) bool {
	return content == MissingHistoryContent || content == ToolCallHistoryContent ||
		strings.HasPrefix(content, InterruptedToolResultPrefix)
}
