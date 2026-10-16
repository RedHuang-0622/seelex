// history_safety.go — ReplaceHistory 前的历史安全规则 + provider 的 tool 配对
// 协议（迁移自 application/core/history_safety.go 与
// application/core/context_runtime/history.go 的合法配对规则，语义保持一致）：
//
//  1. checkpoint/压缩帧标记清理：应用侧控制块（任务检查点、旧压缩帧）
//     不进入替换后的历史；
//  2. tool 配对协议：剔除没有 assistant 宣告的孤儿结果与重复结果、把乱序
//     结果搬回声明的相邻结果块、给中断（残缺）链补缺失占位——provider 校验
//     的是"每条 role=tool 消息必须紧跟那条携带其 tool_call_id 的 assistant"，
//     不是"历史里存在配对"（见 repairToolPairing）；
//  3. 空正文配对修复：assistant 带 tool_calls 保留协议、assistant 仅
//     推理正文保留协议，其余空正文消息补缺失标记（provider 拒绝空
//     content 字段）。
//
// 应用侧与框架侧是同一规则的两处实现（包依赖方向不允许共用一份：
// application → seelexctx）；改动必须同步，否则两侧漂移——2026-09-20 的现场
// 就是框架侧只补缺失占位、不剔孤儿不搬乱序，应用侧早已按协议修复，于是这类
// 历史一路发到 provider：HTTP 400 "Messages with role 'tool' must be a
// response to a preceding message with 'tool_calls'"（会话循环第 15/22 轮
// 中断）。
package seelexctx

import (
	"strings"

	"github.com/RedHuang-0622/Seele/types"
)

// missingHistoryContent 是空正文中断恢复的配对修复文本。
const missingHistoryContent = "[Seelex recovery note: the previous message had no text after an interrupted request; its original content is unavailable.]"

// toolCallHistoryContent 是 assistant+tool_calls 缺正文的配对修复文本。
const toolCallHistoryContent = "[Seelex recovery note: the assistant issued the recorded tool call(s); the original accompanying text is unavailable.]"

// interruptedToolResultPrefix 是合成 tool 占位结果正文的前缀（见
// repairToolPairing）：标识非真实工具输出、provider-only。
const interruptedToolResultPrefix = "[Seelex recovery note: interrupted tool call"

func interruptedToolResultContent(name string) string {
	return interruptedToolResultPrefix + " \"" + name + "\" may not have executed; its result was lost before recording. Verify side effects or re-issue the call before continuing.]"
}

// SanitizeProviderToolProtocol 是 wire 出口的 tool 配对协议规整：剔除没有
// assistant 宣告的孤儿结果与重复结果、把乱序结果搬回声明的相邻结果块，
// **不合成占位**（见 repairToolPairing 的 fabricate=false 口径）。
//
// 自定义装配器（不经 seelexctx.NewAssembler 的出口：节点子代理的
// ScopeAssembler 直接委托 DefaultRequestAssembler）必须在出口调用它，
// 否则控制器投影保留的孤儿 tool 行会原样发到 provider（2026-09-20 现场：
// goalplan-1 节点会话 HTTP 400 "Messages with role 'tool' must be a response
// to a preceding message with 'tool_calls'"）。合法历史逐字不变。
func SanitizeProviderToolProtocol(history []types.Message) []types.Message {
	return repairToolPairing(history, false)
}

// PrepareReplaceHistory 在 ContextDecision.ReplaceHistory 生效前执行：
// 清理上下文控制块（checkpoint/旧压缩帧），再按 provider 的 tool 配对协议
// 规整工具链（剔孤儿/重复、搬回乱序、补中断占位），最后修复空正文。
// 返回修复后的历史（不修改入参）。
func PrepareReplaceHistory(history []types.Message) []types.Message {
	filtered := removeContextMarkers(history)
	return repairEmptyHistoryContent(repairToolPairing(filtered, true))
}

// toolCallPairing 是 repairToolPairing 的配对索引。
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
// 块里一条结果都没有，provider 按宣告条数数下来就是 "insufficient tool messages
// following tool_calls message"。逐次配对后，两条声明各自拿到自己的结果块。
//
// 另一类被判成"无法配对、链保持原样"的形状同样会 400：声明里带**空 ID** 的调用
// （provider 按 tool_calls 条数数回执，空 ID 那条永远没有回执）与**行内重复 ID**
// 的调用（记录异常）。两者都在此处剔除，声明数与结果数才对得上。
type toolCallPairing struct {
	// declares 是声明行 → 规整后的调用列表（空 ID 与行内重复 ID 已剔除）。
	declares map[int][]types.ToolCall
	// resultsOf 是声明行 → 该行每个调用对应的结果行号（-1 = 缺结果）。
	resultsOf map[int][]int
	// occurrences 是声明行 → 该行每个调用是第几次声明这个 call_id（0 起）。
	occurrences map[int][]int
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
func isInterruptedToolResult(message types.Message) bool {
	return message.Content != nil && strings.HasPrefix(*message.Content, interruptedToolResultPrefix)
}

// indexToolCallPairing 为一次配对修复建立索引（每个 call_id 逐次配对；占位被同
// ID 的真结果顶掉）。
func indexToolCallPairing(history []types.Message) toolCallPairing {
	pairing := toolCallPairing{
		declares:    make(map[int][]types.ToolCall),
		resultsOf:   make(map[int][]int),
		occurrences: make(map[int][]int),
		resultFor:   make(map[int]int),
		names:       make(map[string]string),
		normalized:  make(map[int]bool),
	}
	slots := make(map[string][]toolCallSlot)
	for index, message := range history {
		if message.Role != "assistant" || len(message.ToolCalls) == 0 {
			continue
		}
		seen := make(map[string]bool, len(message.ToolCalls))
		calls := make([]types.ToolCall, 0, len(message.ToolCalls))
		occurrences := make([]int, 0, len(message.ToolCalls))
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
			occurrences = append(occurrences, len(slots[call.ID]))
			slots[call.ID] = append(slots[call.ID], toolCallSlot{row: index, position: len(calls) - 1})
			if _, known := pairing.names[call.ID]; !known {
				pairing.names[call.ID] = call.Function.Name
			}
		}
		if len(calls) == 0 {
			// 整行没有可配对的宣告：退回普通 assistant 文本行。
			pairing.normalized[index] = true
		}
		pairing.declares[index] = calls
		pairing.occurrences[index] = occurrences
		pairing.resultsOf[index] = make([]int, len(calls))
		for position := range pairing.resultsOf[index] {
			pairing.resultsOf[index][position] = -1
		}
	}
	// 结果行按 call_id 分组（保序），再逐次配给同名声明。合成占位在真结果足够
	// 覆盖全部声明时被顶掉：占位只是协议垫片（"该调用结果没记下来"），真结果后
	// 到时必须接替它 —— 否则"重复结果"规则会把真结果当多余行丢弃，模型永远只能
	// 看到"调用可能没执行"。
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
			// 真结果不够覆盖声明时占位要留下补足槽位，否则每次修复都"丢占位→
			// 再补占位"，历史永不定稿。
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

// repairToolPairing 按 provider 的 tool 配对协议规整历史。现场（2026-09-20
// 实测 HTTP 400 "Messages with role 'tool' must be a response to a preceding
// message with 'tool_calls'"）的三种违规形状与处置：
//
//   - 孤儿结果（没有任何 assistant 宣告过它的 tool 行）：剔除——它没有任何
//     可回应的前一条消息。窗口投影刻意保留窗口内的非单元消息（见
//     projectHistory 的审计 R3 口径），孤儿因此可能落在投影后历史的最前面；
//   - 乱序结果（assistant(tool_calls c1) → user → tool(c1)）：把结果搬回声明
//     之后（位置归位，内容不丢）；
//   - 重复宣告（同一 call_id 被两次声明）：逐次配对——第 k 次声明配该 ID 的第 k
//     条结果。旧口径"每个 ID 只认首个结果"会把后一条同名结果当重复行丢掉，于是
//     后一条声明在自己的相邻块里缺回执，provider 直接 400
//     "insufficient tool messages following tool_calls message"（2026-09-28
//     现场：session loop 0）；
//   - 无法配对的宣告（空 ID、行内重复 ID）：从声明里剔除（provider 按宣告条数
//     数回执，这类调用永远配不上）。
//
// fabricate=true 时，再为"声明了但全篇没有结果"的调用在链断裂点补合成占位
// （会话中断/重启丢结果的恢复口径，provider-only，见
// interruptedToolResultContent）。fabricate=false 是 wire 出口（装配器）的保守
// 口径：请求可能发生在工具尚未执行完的活跃 ReAct 中间态，给"即将执行"的调用
// 补占位等于污染历史（与应用侧 PrepareNewHistoryContentFor 同一条边界）。
//
// 唯一不受 fabricate 影响的是**重复宣告**（同一 call_id 的第二次及以后）：它是
// 一条已经拿到过回执的调用的重发/重放，缺回执一定是记录残缺，补占位不会误导
// "还没执行"；不补则请求必 400，比补占位更糟。
//
// 幂等：修好的历史再跑一次逐字不变（原地结果继续原地输出、占位不再重复补）；
// 不修改入参。
func repairToolPairing(history []types.Message, fabricate bool) []types.Message {
	pairing := indexToolCallPairing(history)
	prepared := make([]types.Message, 0, len(history)+2)
	// pendingPlaceholders 缓存"本轮缺结果的合成占位"：provider 要求结果块紧跟
	// 声明，占位若插在真结果之前会把真结果挤到非相邻位置，等于把本来合法的请求
	// 变成 400，所以占位要等本单元的原地结果输出之后再补。
	var pendingPlaceholders []types.Message
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
			normalized := cloneMessageSlice([]types.Message{message})[0]
			normalized.ToolCalls = append([]types.ToolCall(nil), pairing.declares[index]...)
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
		// 2) 没拿到回执的调用补合成占位（wire 出口只补"重复宣告"这一类，见函数
		// 注释）。
		needPlaceholder := false
		for position, call := range wanted {
			if pairing.resultsOf[index][position] >= 0 {
				continue
			}
			if !fabricate && pairing.occurrences[index][position] == 0 {
				continue
			}
			content := interruptedToolResultContent(pairing.names[call.ID])
			pendingPlaceholders = append(pendingPlaceholders, types.Message{
				Role: "tool", ToolCallID: call.ID, Name: pairing.names[call.ID], Content: &content,
			})
			needPlaceholder = true
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
	return prepared
}

// removeContextMarkers 移除 user 角色且以 checkpoint 或压缩帧标记开头的
// 控制消息（应用侧专用，不得作为对话内容渲染/持久化）。
func removeContextMarkers(history []types.Message) []types.Message {
	filtered := make([]types.Message, 0, len(history))
	for _, message := range history {
		if message.Role == "user" && message.Content != nil &&
			(strings.HasPrefix(*message.Content, checkpointMarker) ||
				strings.HasPrefix(*message.Content, compactContextMarker)) {
			continue
		}
		filtered = append(filtered, message)
	}
	return filtered
}

// repairEmptyHistoryContent 按配对规则修复空正文消息。
func repairEmptyHistoryContent(history []types.Message) []types.Message {
	prepared := cloneMessageSlice(history)
	for index := range prepared {
		message := &prepared[index]
		if message.Content != nil && strings.TrimSpace(*message.Content) != "" {
			continue
		}
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			content := toolCallHistoryContent
			message.Content = &content
			continue
		}
		if message.Role == "assistant" && message.ReasoningContent != "" {
			content := missingHistoryContent
			message.Content = &content
			continue
		}
		if message.Role == "system" || message.Role == "user" || message.Role == "assistant" || message.Role == "tool" {
			content := missingHistoryContent
			message.Content = &content
		}
	}
	return prepared
}

// IsProviderOnlyHistoryContent 识别仅用于满足 provider 非空正文的修复文本，
// 不得渲染为助手回复（与 application 侧语义一致）。
func IsProviderOnlyHistoryContent(content string) bool {
	return content == missingHistoryContent || content == toolCallHistoryContent ||
		strings.HasPrefix(content, interruptedToolResultPrefix)
}

func cloneMessageSlice(messages []types.Message) []types.Message {
	out := make([]types.Message, len(messages))
	copy(out, messages)
	for index := range out {
		if messages[index].Content != nil {
			value := *messages[index].Content
			out[index].Content = &value
		}
		if messages[index].ToolCalls != nil {
			out[index].ToolCalls = append([]types.ToolCall(nil), messages[index].ToolCalls...)
		}
	}
	return out
}
