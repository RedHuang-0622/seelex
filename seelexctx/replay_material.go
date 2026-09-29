// replay_material.go — 前缀重放素材的 wire 合法性规整。
//
// 为什么需要：前缀重放的素材是「最近一次真实请求的历史字节原样重放」，但素材的
// 来源快照**早于**请求出口的协议修复，两处都取不到修复后的字节：
//
//   - 装配层折叠：装配开始时读一次引擎历史（application/core/context_runtime
//     coordinator.go 的 `existing := c.foldHistory(sessionID)`），而残缺工具链的
//     补齐（PrepareProviderHistoryFor → RepairInterruptedToolChains）发生在同一
//     条装配路径的**替换之后**——被中断的回合（assistant 宣告了工具调用、结果
//     丢失）因此原样进重放请求；
//   - in-loop 折叠：直接拿活跃 ReAct 的工作历史（controller.go 的 ev.History），
//     那一刻它的尾巴可能正是一条还没有回执的 tool_call。
//
// 两者对 provider 都是硬违规：每条 assistant(tool_calls) 之后必须紧跟（相邻、
// 连续）覆盖它每个 tool_call_id 的 tool 消息，否则整条请求被拒收——
//
//	HTTP 400 An assistant message with 'tool_calls' must be followed by tool
//	messages responding to each 'tool_call_id'. (insufficient tool messages
//	following tool_calls message)
//
// 2026-09-29 现场：会话在工具轮被中断（结果未记录）后冷加载，装配层折叠触发，
// 前缀重放两次都在 provider 侧被 400 拒收，折叠每次静默降级本地折叠——素材本身
// 忠实复刻了历史，错在**没有经过请求出口的协议规整**。
//
// 两条规则，都不发明事实：
//
//  1. **未落定的尾单元整段丢掉**（尾巴上的 assistant 宣告仍有未回执调用，且其后
//     只有它自己的结果行）：这是"工具正在跑"或"上一轮被中断且无后续"的形态。它
//     不属于任何**已发出**的请求字节（上一个请求发出时它还没产生，它的回执要等
//     工具跑完才有），补占位等于对一个可能正在执行的调用宣布"结果丢了"。丢掉
//     既合法，又比补占位更贴近已发出的前缀字节。
//  2. **中段死链补回执占位**（声明之后还有别的消息）：这类链不可能是"正在跑"，
//     是中断/重启丢结果的记录残缺，装配层对真实请求补的占位与这里逐字相同
//     （见 interruptedToolResultContent），素材因此仍与已发出字节对齐。
//
// 规整之后仍不合法的素材不再发出去：ValidateReplayProtocol 指名违规位置，调用方
// 按"重放失败"降级本地折叠并把原因写进帧证据——可自答的失败，而不是一次 provider
// 400 之后再去猜。
package seelexctx

import (
	"fmt"
	"strings"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// CompactReplayMaterialEvidenceRefPrefix 标记"重放素材被规整过"的帧证据 ref。
// 素材被动过是**事实**，必须可从帧读出来：否则读者只能看到一次没有模型摘要的
// 折叠，无从判断素材是不是早就坏了。
const CompactReplayMaterialEvidenceRefPrefix = "replay-material:"

// ReplayMaterialReport 是一次素材规整的事实（零值 = 逐字未动）。
type ReplayMaterialReport struct {
	// DroppedTailMessages 是丢掉的未落定尾单元消息数（0 = 没丢）。
	DroppedTailMessages int
	// DroppedTailCallIDs 是被丢弃尾单元里未回执的调用 ID（可读证据）。
	DroppedTailCallIDs []string
	// FilledPlaceholders 是补齐的回执占位条数（中段死链）。
	FilledPlaceholders int
	// DroppedOrphanResults 是剔除的孤儿结果行数（没有任何声明可回应）。
	DroppedOrphanResults int
	// ReorderedResults 表示把乱序结果搬回了声明的相邻块。
	ReorderedResults bool
	// NormalizedDeclarations 是被规整的声明行数（空 ID / 行内重复 ID）。
	NormalizedDeclarations int
}

// Repaired 报告本次素材是否被动过（false → 逐字原样重放）。
func (r ReplayMaterialReport) Repaired() bool {
	return r.DroppedTailMessages > 0 || r.FilledPlaceholders > 0 ||
		r.DroppedOrphanResults > 0 || r.ReorderedResults || r.NormalizedDeclarations > 0
}

// Terse 渲染一行事实（帧证据正文）。
func (r ReplayMaterialReport) Terse() string {
	if !r.Repaired() {
		return "replay material replayed byte-for-byte"
	}
	var builder strings.Builder
	builder.WriteString("replay material normalized to provider tool protocol:")
	if r.DroppedTailMessages > 0 {
		fmt.Fprintf(&builder, " dropped unsettled tail unit (%d messages, unanswered calls %s)",
			r.DroppedTailMessages, strings.Join(r.DroppedTailCallIDs, ","))
	}
	if r.FilledPlaceholders > 0 {
		fmt.Fprintf(&builder, " filled %d missing tool result(s) with recovery placeholder", r.FilledPlaceholders)
	}
	if r.DroppedOrphanResults > 0 {
		fmt.Fprintf(&builder, " dropped %d orphan tool result row(s)", r.DroppedOrphanResults)
	}
	if r.ReorderedResults {
		builder.WriteString(" moved misordered tool result(s) back behind their declaration")
	}
	if r.NormalizedDeclarations > 0 {
		fmt.Fprintf(&builder, " normalized %d declaration row(s) (empty/duplicate call id)", r.NormalizedDeclarations)
	}
	return builder.String()
}

// ReplayMaterialEvidence 把素材规整事实写成帧证据。与 ReplayEvidence / LocalFoldEvidence
// 同一条纪律：没有这件事（素材逐字未动）就不留痕。
func ReplayMaterialEvidence(report ReplayMaterialReport) []sessionstore.EvidenceRef {
	if !report.Repaired() {
		return nil
	}
	return []sessionstore.EvidenceRef{{
		Ref:     CompactReplayMaterialEvidenceRefPrefix + "normalized",
		Summary: report.Terse(),
	}}
}

// PrepareReplayMaterial 把素材规整成 provider 的 tool 配对协议下合法的字节序：
// 丢掉未落定的尾单元，再按请求出口口径补齐/搬回/剔除工具链（幂等，不修改入参）。
// 返回规整后的素材与事实报告；素材为空时返回 nil 素材与零报告。
func PrepareReplayMaterial(messages []types.Message) ([]types.Message, ReplayMaterialReport) {
	trimmed, report := trimUnsettledTail(messages)
	pairing := indexToolCallPairing(trimmed)
	for row, calls := range pairing.declares {
		for position := range calls {
			if pairing.resultsOf[row][position] < 0 {
				report.FilledPlaceholders++
			}
		}
	}
	for row := range pairing.resultFor {
		if pairing.resultFor[row] < 0 {
			report.DroppedOrphanResults++
		}
	}
	if pairing.reorder {
		report.ReorderedResults = true
	}
	report.NormalizedDeclarations = len(pairing.normalized)
	return repairToolPairing(trimmed, true), report
}

// trimUnsettledTail 丢掉"未落定的尾单元"：尾巴上那条 assistant 宣告仍有未回执的
// 调用，且其后只跟着它自己的结果行（可能一条也没有）。条件是必要的保守性——尾块里
// 出现不属于这条声明的 tool 行时**不动**（那可能是被搬错的真结果，交给配对修复）。
//
// 未落定的判定只按 call_id 是否在尾块里出现过：空 ID / 行内重复 ID 本身会被配对
// 修复剔除，不构成"未落定"。
func trimUnsettledTail(messages []types.Message) ([]types.Message, ReplayMaterialReport) {
	report := ReplayMaterialReport{}
	end := len(messages)
	start := end
	for start > 0 && messages[start-1].Role == "tool" {
		start--
	}
	if start == 0 {
		return messages, report
	}
	declaration := messages[start-1]
	if declaration.Role != "assistant" || len(declaration.ToolCalls) == 0 {
		return messages, report
	}
	declared := make(map[string]struct{}, len(declaration.ToolCalls))
	for _, call := range declaration.ToolCalls {
		if call.ID != "" {
			declared[call.ID] = struct{}{}
		}
	}
	answered := make(map[string]struct{}, end-start)
	for _, message := range messages[start:end] {
		if message.ToolCallID == "" {
			continue
		}
		if _, own := declared[message.ToolCallID]; !own {
			return messages, report
		}
		answered[message.ToolCallID] = struct{}{}
	}
	missing := make([]string, 0, len(declared))
	for _, call := range declaration.ToolCalls {
		if call.ID == "" {
			continue
		}
		if _, ok := answered[call.ID]; !ok {
			missing = append(missing, call.ID)
		}
	}
	if len(missing) == 0 {
		return messages, report
	}
	report.DroppedTailMessages = end - (start - 1)
	report.DroppedTailCallIDs = missing
	return messages[:start-1], report
}

// ValidateReplayProtocol 按 provider 数请求的那条规则校验一次消息序，违规时指名
// 第一条违规的位置：每条 assistant(tool_calls) 之后相邻连续的 tool 块必须覆盖它
// 宣告的每个 tool_call_id，且块内每条 tool 行都必须服务于它。
//
// 只做校验、不改消息——它是"这次重放能不能发"的判据，也是素材规整自身的回归哨兵。
func ValidateReplayProtocol(messages []types.Message) error {
	index := 0
	for index < len(messages) {
		message := messages[index]
		if message.Role != "assistant" || len(message.ToolCalls) == 0 {
			if message.Role == "tool" {
				return fmt.Errorf("msg#%d role=tool (tool_call_id=%q) 不在任何 assistant 声明的相邻结果块内",
					index, message.ToolCallID)
			}
			index++
			continue
		}
		declared := make(map[string]bool, len(message.ToolCalls))
		seen := make(map[string]struct{}, len(message.ToolCalls))
		for _, call := range message.ToolCalls {
			if call.ID == "" {
				return fmt.Errorf("msg#%d assistant tool_calls 含空 ID 调用（%q）", index, call.Function.Name)
			}
			if _, duplicate := seen[call.ID]; duplicate {
				return fmt.Errorf("msg#%d assistant tool_calls 行内重复 call id %q", index, call.ID)
			}
			seen[call.ID] = struct{}{}
			declared[call.ID] = false
		}
		next := index + 1
		orphanAt := -1
		orphanID := ""
		for next < len(messages) && messages[next].Role == "tool" {
			id := messages[next].ToolCallID
			if answered, ok := declared[id]; ok && !answered {
				declared[id] = true
			} else {
				orphanAt, orphanID = next, id
			}
			next++
		}
		for _, call := range message.ToolCalls {
			if !declared[call.ID] {
				return fmt.Errorf("msg#%d assistant 宣告的 tool_call %q 之后没有相邻的 tool 回执（insufficient tool messages following tool_calls message）",
					index, call.ID)
			}
		}
		if orphanAt >= 0 {
			return fmt.Errorf("msg#%d role=tool (tool_call_id=%q) 不服务于 msg#%d 的声明，也不是紧跟声明的回执",
				orphanAt, orphanID, index)
		}
		index = next
	}
	return nil
}
