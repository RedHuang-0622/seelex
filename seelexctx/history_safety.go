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

// toolCallPairing 是 repairToolPairing 的配对索引：ownerOf（call_id → 声明它
// 的 assistant 行号）、resultAt（call_id → 该结果所在行号）、names（call_id →
// 工具名）、widths（assistant 行号 → 该行 tool_calls 数量）。每个 call_id 只认
// **第一个**结果：provider 对同一 call_id 的重复结果同样报错，后出现的重复行
// 按孤儿丢弃。
type toolCallPairing struct {
	ownerOf  map[string]int
	resultAt map[string]int
	names    map[string]string
	widths   map[int]int
}

// emitInPlace 报告某 tool 行能否原样输出：它是该 call_id 的首个结果，且位置已经
// 落在声明的相邻区间内。重复结果、孤儿与乱序行都不满足。
func (p toolCallPairing) emitInPlace(index int, callID string) bool {
	if callID == "" {
		return false
	}
	position, ok := p.resultAt[callID]
	if !ok || position != index {
		return false
	}
	owner, ok := p.ownerOf[callID]
	if !ok {
		return false
	}
	return position >= owner+1 && position <= owner+p.widths[owner]
}

// declarationHasInPlaceResult 报告声明行 index 的结果里是否存在"原地输出"的那
// 一条：决定缺失占位应当紧跟声明输出，还是要等原地结果之后再补。
func (p toolCallPairing) declarationHasInPlaceResult(index int) bool {
	for callID, owner := range p.ownerOf {
		if owner != index {
			continue
		}
		if position, ok := p.resultAt[callID]; ok && p.emitInPlace(position, callID) {
			return true
		}
	}
	return false
}

// indexToolCallPairing 为一次配对修复建立索引（每个 call_id 只认首个声明与
// 首个结果）。
func indexToolCallPairing(history []types.Message) toolCallPairing {
	pairing := toolCallPairing{
		ownerOf:  make(map[string]int),
		resultAt: make(map[string]int),
		names:    make(map[string]string),
		widths:   make(map[int]int),
	}
	for index, message := range history {
		if message.Role != "assistant" {
			continue
		}
		pairing.widths[index] = len(message.ToolCalls)
		for _, call := range message.ToolCalls {
			if call.ID == "" {
				continue
			}
			if _, duplicate := pairing.ownerOf[call.ID]; duplicate {
				continue
			}
			pairing.ownerOf[call.ID] = index
			pairing.names[call.ID] = call.Function.Name
		}
	}
	for index, message := range history {
		if message.Role != "tool" || message.ToolCallID == "" {
			continue
		}
		if _, declared := pairing.ownerOf[message.ToolCallID]; !declared {
			continue
		}
		if _, duplicate := pairing.resultAt[message.ToolCallID]; duplicate {
			continue
		}
		pairing.resultAt[message.ToolCallID] = index
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
//   - 重复结果（同一 call_id 两条结果）：后出现的按孤儿丢弃。
//
// fabricate=true 时，再为"声明了但全篇没有结果"的调用在链断裂点补合成占位
// （会话中断/重启丢结果的恢复口径，provider-only，见
// interruptedToolResultContent）。fabricate=false 是 wire 出口（装配器）的保守
// 口径：请求可能发生在工具尚未执行完的活跃 ReAct 中间态，给"即将执行"的调用
// 补占位等于污染历史（与应用侧 PrepareNewHistoryContentFor 同一条边界），
// 因此出口只剔除与搬运、不合成。
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
			// tool 行原地输出的唯一情形：它是该 call_id 的首个结果，且本来就在
			// 声明的相邻区间内。其余一律跳过：乱序行由声明处带出（位置归位、
			// 内容不丢），重复结果与无宣告的孤儿则没有任何前一条消息可回应。
			if !pairing.emitInPlace(index, message.ToolCallID) {
				continue
			}
			prepared = append(prepared, message)
			// 本行是某个声明里"以后出现的原地结果"：占位必须排在它之后，否则
			// 结果块会被占位隔开（provider 只认紧跟声明的结果）。
			if delayedFlush && index > pendingDeclarationRow &&
				pairing.ownerOf[message.ToolCallID] == pendingDeclarationRow {
				flushPending()
			}
			continue
		}
		// 新的声明行之前，把上一个声明的待补占位清掉（正常情况下已在下方补过，
		// 这里是防御性兜底，保证占位不会漂到别的声明后面）。
		flushPending()
		prepared = append(prepared, message)
		if message.Role != "assistant" || len(message.ToolCalls) == 0 {
			continue
		}
		wanted := make([]string, 0, len(message.ToolCalls))
		valid := true
		for _, call := range message.ToolCalls {
			if call.ID == "" {
				valid = false // 空 ID 无法配对；链保持原样
				break
			}
			wanted = append(wanted, call.ID)
		}
		if !valid {
			continue
		}
		// 1) 把散落在别处的同 ID 结果搬回声明之后（乱序 → 相邻）。已经在相邻
		// 区间内、会原地输出的结果不重复带出（否则同一结果出现两次）。
		for _, id := range wanted {
			if position, ok := pairing.resultAt[id]; ok && !pairing.emitInPlace(position, id) {
				prepared = append(prepared, history[position])
			}
		}
		// 2) 既没有相邻结果、也没有后文结果的调用补合成占位（仅 ReplaceHistory
		// 路径；wire 出口不合成，见函数注释）。
		if !fabricate {
			continue
		}
		needPlaceholder := false
		for _, id := range wanted {
			if _, ok := pairing.resultAt[id]; ok {
				continue
			}
			content := interruptedToolResultContent(pairing.names[id])
			pendingPlaceholders = append(pendingPlaceholders, types.Message{
				Role: "tool", ToolCallID: id, Name: pairing.names[id], Content: &content,
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
		// 本单元的结果（若有）都已被拉回声明之后 → 占位紧跟着补，不会挤掉任何行。
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
