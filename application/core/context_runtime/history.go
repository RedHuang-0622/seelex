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
// 它与折叠同源取历史、同源写回（会话路由端口）：装配路径里它紧跟在折叠之后，
// 两边写的是同一份工作历史；写回在忙会话上由引擎排队到下一个检查点，因此不会
// 因为"这一轮正在跑"而丢掉。
func (h *HistoryCoordinator) PrepareProviderHistoryFor(sessionID string) error {
	history := h.foldHistory(sessionID)
	prepared, repaired := RepairInterruptedToolChains(history)
	if repaired {
		if err := h.replaceFoldHistory(sessionID, prepared); err != nil {
			return fmt.Errorf("repair interrupted tool chains: %w", err)
		}
	}
	final, repairedContent := RepairEmptyHistoryContent(prepared)
	if repairedContent {
		if err := h.replaceFoldHistory(sessionID, final); err != nil {
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
	history := h.foldHistory(sessionID)
	prepared, repaired := RepairEmptyHistoryContent(history)
	if !repaired {
		return nil
	}
	return h.replaceFoldHistory(sessionID, prepared)
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

// RepairInterruptedToolChains 修复中断（残缺）工具链：assistant 消息携带
// tool_calls，但其后只记录了部分（或没有）tool 结果 —— 会话中断/重启导致
// 结果丢失。对每个缺失调用 ID，若其后文再没有该 ID 的 tool 结果，就在链
// 断裂点（紧邻结果的最后一个之后、第一个非 tool 消息之前）插入合成 tool
// 占位消息，使发送给 provider 的序列保持 assistant/tool 配对合法。
//
// 同时把**乱序**的 tool 结果搬回它所属的 assistant 声明之后。provider 的校验
// 不是"历史里存在配对"而是"每条 tool 消息必须紧跟在携带其 tool_calls 的
// assistant 消息之后"：`assistant(tool_calls c1) → user → tool(c1)` 这种历史
// 会让整次请求 400——HTTP 400
// `{"error":{"message":"Messages with role 'tool' must be a response to a
// preceding message with 'tool_calls'", ...}}`（2026-09-17 实测）。旧实现只
// "补缺失、不动顺序"，这种历史会原样发出。没有 assistant 宣告的孤儿结果同样
// 无法满足该协议，投影时剔除（它没有任何可回应的前一条消息）。
//
// 幂等：已补齐的链再次执行不会重复插入；后文存在同 ID 结果时不插入（保守，
// 避免破坏既有配对）。合成正文带 InterruptedToolResultPrefix，属于
// provider-only，不渲染为真实工具输出。不修改入参。
func RepairInterruptedToolChains(history []contract.EngineMessage) ([]contract.EngineMessage, bool) {
	pairing := indexToolCallPairing(history)
	prepared := make([]contract.EngineMessage, 0, len(history)+2)
	repaired := false
	// pendingPlaceholders 缓存"本轮缺结果的合成占位"：provider 要求结果块紧跟
	// 声明，占位若插在真结果之前会把真结果挤到非相邻位置，等于把本来合法的请求
	// 变成 400，所以占位要等本单元的原地结果输出之后再补。
	var pendingPlaceholders []contract.EngineMessage
	pendingDeclarationRow := -1 // 占位属于哪个声明（-1 = 无待补占位）
	// delayedFlush 表示占位要等"该声明的原地结果行"输出后再补（该声明同时存在
	// 原地结果与被拉回的结果，直接补会挤掉原地结果）。
	delayedFlush := false
	for index, message := range history {
		if message.Role == "tool" {
			// tool 行原地输出的唯一情形：它是该 call_id 的首个结果，且本来就在
			// 声明的相邻区间内。其余一律跳过：乱序行由声明处带出（位置归位、
			// 内容不丢），重复结果与无宣告的孤儿则没有任何前一条消息可回应。
			if !pairing.emitInPlace(index, message.ToolCallID) {
				repaired = true
				continue
			}
			prepared = append(prepared, message)
			// 本行是某个声明里"以后出现的原地结果"：占位必须排在它之后，否则
			// 结果块会被占位隔开（provider 只认紧跟声明的结果）。
			if len(pendingPlaceholders) > 0 && delayedFlush &&
				index > pendingDeclarationRow && pairing.ownerOf[message.ToolCallID] == pendingDeclarationRow {
				prepared = append(prepared, pendingPlaceholders...)
				pendingPlaceholders = nil
				pendingDeclarationRow = -1
				delayedFlush = false
			}
			continue
		}
		// 新的声明行之前，把上一个声明的待补占位清掉（正常情况下已在下方补过，
		// 这里是防御性兜底，保证占位不会漂到别的声明后面）。
		if len(pendingPlaceholders) > 0 {
			prepared = append(prepared, pendingPlaceholders...)
			pendingPlaceholders = nil
			pendingDeclarationRow = -1
			delayedFlush = false
		}
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
		// 2) 既没有相邻结果、也没有后文结果的调用补合成占位。
		needPlaceholder := false
		for _, id := range wanted {
			if _, ok := pairing.resultAt[id]; ok {
				continue
			}
			pendingPlaceholders = append(pendingPlaceholders, contract.EngineMessage{
				Role: "tool", ToolCallID: id, Name: pairing.names[id],
				Content: interruptedToolResultContent(pairing.names[id]), ContentSet: true,
			})
			needPlaceholder = true
			repaired = true
		}
		if needPlaceholder {
			if pairing.declarationHasInPlaceResult(index) {
				// 该声明还有原地结果要输出：占位必须排在它之后，否则会把结果
				// 挤出声明区间。
				pendingDeclarationRow = index
				delayedFlush = true
			} else {
				// 本单元的结果（若有）都已被拉回声明之后 → 占位紧跟着补，不会
				// 挤掉任何行。
				prepared = append(prepared, pendingPlaceholders...)
				pendingPlaceholders = nil
			}
		}
	}
	prepared = append(prepared, pendingPlaceholders...)
	return prepared, repaired || pairing.reorder
}

// toolCallPairing 是 RepairInterruptedToolChains 的配对索引：ownerOf
// （call_id → 声明它的 assistant 行号）、resultAt（call_id → 该结果所在行号）、
// names（call_id → 工具名）、widths（assistant 行号 → 该行 tool_calls 数量）。
// 每个 call_id 只认**第一个**结果：provider 对同一 call_id 的重复结果同样报错，
// 后出现的重复行按孤儿丢弃。
type toolCallPairing struct {
	ownerOf  map[string]int
	resultAt map[string]int
	names    map[string]string
	widths   map[int]int
	reorder  bool
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

func indexToolCallPairing(history []contract.EngineMessage) toolCallPairing {
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
			pairing.names[call.ID] = call.Name
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
	// 乱序判定：结果必须落在声明行之后的连续区间内（区间内先后不限，provider
	// 只要求它们紧跟声明）；被别的消息隔开或落在区间外就必须重排。
	for index, message := range history {
		if message.Role != "assistant" {
			continue
		}
		for _, call := range message.ToolCalls {
			position, ok := pairing.resultAt[call.ID]
			if !ok {
				continue
			}
			if position < index+1 || position > index+len(message.ToolCalls) {
				pairing.reorder = true
			}
		}
	}
	return pairing
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
