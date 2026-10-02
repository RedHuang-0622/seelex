package task_context

import (
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/model"
)

// PlanNodeStatus 将字符串转换为 NodeStatus（queued/running/... 全量映射）。
func PlanNodeStatus(s string) model.NodeStatus {
	switch s {
	case "queued":
		return model.NodeQueued
	case "running", "started":
		return model.NodeRunning
	case "worktree_creating":
		return model.NodeWorktreeCreating
	case "rebasing":
		return model.NodeRebasing
	case "merging":
		return model.NodeMerging
	case "completed":
		return model.NodeCompleted
	case "failed":
		return model.NodeFailed
	case "aborted":
		return model.NodeAborted
	case "skipped":
		return model.NodeSkipped
	case "canceled":
		return model.NodeCanceled
	case "panicked":
		return model.NodePanicked
	default:
		return model.NodePending
	}
}

// ActivePlanProjection 返回 Plan 的只读投影（Completed/Failed/Pending 节点
// 分类 + current node 推导）。
func ActivePlanProjection(plan *model.PlanState, activePlanID string, planSequence uint64) *model.ActivePlanProjection {
	if plan == nil || activePlanID == "" {
		return nil
	}
	projection := &model.ActivePlanProjection{
		PlanID: activePlanID, Version: planSequence,
		CanonicalPlanRef: activePlanID, Status: string(plan.Status),
	}
	for _, node := range plan.Nodes {
		switch node.Status {
		case model.NodeCompleted, model.NodeSkipped:
			projection.CompletedNodes = append(projection.CompletedNodes, node.ID)
		case model.NodeFailed, model.NodeAborted, model.NodeCanceled, model.NodePanicked:
			projection.FailedNodes = append(projection.FailedNodes, node.ID)
		case model.NodeRunning:
			projection.CurrentNode = node.ID
		default:
			projection.PendingNodes = append(projection.PendingNodes, node.ID)
		}
	}
	if projection.CurrentNode == "" && len(projection.PendingNodes) > 0 {
		projection.CurrentNode = projection.PendingNodes[0]
	}
	return projection
}

// ActivePlanFrame 返回 plan 栈中的激活帧（未找到 → nil）。
func ActivePlanFrame(stack []model.SessionPlanFrame, activeID string) *model.SessionPlanFrame {
	for index := range stack {
		if stack[index].ID == activeID {
			return &stack[index]
		}
	}
	return nil
}

// ActivePlanFromStack 返回激活帧的 Plan 深拷贝（未找到 → nil）。
func ActivePlanFromStack(stack []model.SessionPlanFrame, activeID string) *model.PlanState {
	frame := ActivePlanFrame(stack, activeID)
	if frame == nil {
		return nil
	}
	return model.CloneRuntimeState(model.RuntimeState{Plan: frame.Plan}).Plan
}

// TranscriptTailHistory 把 transcript 尾部事件按协议单元收敛为 provider
// 历史（token 预算 + 单元上限）。maxUnits <= 0 表示全量累积（append-only
// 已定稿轮次，达峰前字节稳定）；maxUnits > 0 表示有界窗口（压缩后新鲜窗口）。
//
// 单元 = 可见轮次（见 transcriptProtocolUnits）：user 轮、assistant 文本轮、
// assistant 工具链轮。残缺（中断）工具链轮同样构成开放单元 —— 只在 UI 可见
// 就不得从冷加载 provider 上下文中消失；其协议合法性（缺失 tool 结果）由
// 装配层的 RepairInterruptedToolChains 在请求前补齐。
//
// 协议单元不可拆分：若最新完整单元单条就超出 tokenBudget，函数不返回空
// 历史，而是降级保留该最新单元（自 newest 起的最后一个可解析完整轮次）。
// 这样“最新上下文”不会因预算装不下而静默消失；该轮是否真的可发送（相对
// 真实 provider 窗口）由上层全量预算门禁决定，超限时应显式拒绝。
// TranscriptTailWindow 返回 TranscriptTailHistory 的窗口结果与窗口边界：
// 保留的引擎消息（按原序）与保留窗口在 events 中的起始下标。start 是保留段
// 第一个事件的索引——events[:start] 是窗口外前缀（尽数送进 compact_context），
// events[start:] 是保留的上下文前缀窗口。空 events / 预算非正时 start =
// len(events)（未保留任何事件）。
//
// 边界来自窗口决策本身（被选中的尾部单元的下标），下游只记录、不重算。
func TranscriptTailWindow(events []model.TranscriptEvent, tokenBudget, maxUnits int) ([]contract.EngineMessage, int) {
	return TranscriptTailWindowBy(events, tokenBudget, maxUnits, recordedUnitTokens)
}

// recordedUnitTokens 是 TranscriptTailWindow 的历史口径：单元内事件自带
// TokenCount（事件落盘时估算）之和。保留给冷读等只做装载、不做压缩判据的
// 调用方；判据/保留窗口必须走 TranscriptTailWindowBy 注入同一把 token 尺子，
// 否则“裁到预算”的裁剪量与“是否越线”的估算量会各说各话（一边按记录值裁、
// 一边按当前校准值判 → 每回合都判成越线）。
func recordedUnitTokens(events []model.TranscriptEvent) int {
	tokens := 0
	for _, event := range events {
		tokens += event.TokenCount
	}
	return tokens
}

// TranscriptTailWindowBy 与 TranscriptTailWindow 同语义，但按注入的单元
// token 估算（同一单元 messages 计数）选窗——预算/目标与判据共用同一估算器，
// 选出的窗口才不会在重新估算时“膨胀”回去。
func TranscriptTailWindowBy(
	events []model.TranscriptEvent,
	tokenBudget, maxUnits int,
	unitTokens func([]model.TranscriptEvent) int,
) ([]contract.EngineMessage, int) {
	if len(events) == 0 || tokenBudget <= 0 {
		return nil, len(events)
	}
	if unitTokens == nil {
		unitTokens = recordedUnitTokens
	}
	units := transcriptProtocolUnitList(events)
	if maxUnits <= 0 {
		maxUnits = len(units) // 全量累积（append-only 已定稿轮次）
	}
	selected := make([]transcriptProtocolUnit, 0, maxUnits)
	first := len(units) // 已选单元里最旧者的下标（len(units) = 未选任何单元）
	tokens := 0
	for index := len(units) - 1; index >= 0 && len(selected) < maxUnits; index-- {
		cost := unitTokens(units[index].events)
		if tokens+cost > tokenBudget {
			break
		}
		selected = append(selected, units[index])
		first = index
		tokens += cost
	}
	// 自 newest 向旧扫描一个单元都放不下（selected 为空）时，仍保留最新
	// 完整单元：静默丢弃最新轮会让模型“失忆”（继续请求看不到上一轮内容）。
	if len(selected) == 0 && len(units) > 0 {
		selected = append(selected, units[len(units)-1])
		first = len(units) - 1
	}
	// 轮次完整性：边界只能落在**轮次起点**，不能落在一轮中间。
	//
	// 现场（用户报告）：压缩之后模型「丢了目标」——它看得见自己刚才干到哪一步，
	// 却看不见用户到底在要求什么。原因是边界被判在一轮中间：该轮的用户提问被判进
	// 压缩区间（原文只在帧摘要里留一行），它的续写留在保留窗口里；压缩记录的区间
	// 终点因此越过了一条用户提问行，保留下来的续写失去了来由。
	//
	// 一轮被切成多段是常态而非异常：轮内的技能正文/内部材料注入（provider role
	// 映射为 system）会把一轮切开，半途中断的工具链同理，每段各自都是协议合法的
	// 单元 —— 所以单元级看一切正常，问题只在"轮次"这一层。被选中的最旧单元若不
	// 是轮次起点，就向前补齐到它所属轮次的起点；这些单元本来就在窗口边上（预算
	// 只差一段），补齐的代价远小于"留着续写、丢了提问"，方向与上面那条「单条超
	// 预算也保留最新单元」同源：宁可多留一点，不让模型失忆。
	for first > 0 && !units[first].opensRound() {
		first--
		selected = append(selected, units[first])
	}
	history := make([]contract.EngineMessage, 0)
	for index := len(selected) - 1; index >= 0; index-- {
		for _, event := range selected[index].events {
			history = append(history, transcriptEventMessage(event))
		}
	}
	if len(selected) == 0 {
		return history, len(events)
	}
	return history, units[first].start
}

// TranscriptEventMessages 把一组 transcript 事件映射为 provider 消息（与装配
// 出口同一映射）。调用方据此用自有的 token 估算器给单元计价，避免再写一份
// 事件→消息的转换。
func TranscriptEventMessages(events []model.TranscriptEvent) []contract.EngineMessage {
	messages := make([]contract.EngineMessage, 0, len(events))
	for _, event := range events {
		messages = append(messages, transcriptEventMessage(event))
	}
	return messages
}

// TranscriptTailHistoryBy 是 TranscriptTailWindowBy 的窗口消息视图。
func TranscriptTailHistoryBy(
	events []model.TranscriptEvent,
	tokenBudget, maxUnits int,
	unitTokens func([]model.TranscriptEvent) int,
) []contract.EngineMessage {
	history, _ := TranscriptTailWindowBy(events, tokenBudget, maxUnits, unitTokens)
	return history
}

// TranscriptTailHistory 是 TranscriptTailWindow 的窗口消息视图（多数调用方
// 只关心历史消息，边界由压缩记录方消费）。
func TranscriptTailHistory(events []model.TranscriptEvent, tokenBudget, maxUnits int) []contract.EngineMessage {
	history, _ := TranscriptTailWindow(events, tokenBudget, maxUnits)
	return history
}

// TranscriptEventRange 记录一段 transcript 区间的可定位边界（消息号 + 事件
// 序号）。压缩记录在压缩发生时用它记下“从哪到哪”，下游不再推算。
type TranscriptEventRange struct {
	EventFrom   uint64
	EventTo     uint64
	MessageFrom string
	MessageTo   string
}

// Empty 报告该区间没有任何可记录的边界。
func (r TranscriptEventRange) Empty() bool {
	return r.EventFrom == 0 && r.EventTo == 0 && r.MessageFrom == "" && r.MessageTo == ""
}

// TranscriptPrefixRange 记录 events[:end] 的区间边界：事件序号取首/末事件的
// Seq（Seq 为 0 的合成事件跳过），消息号取首个/末个非空 MessageID。
func TranscriptPrefixRange(events []model.TranscriptEvent, end int) TranscriptEventRange {
	if end > len(events) {
		end = len(events)
	}
	if end <= 0 {
		return TranscriptEventRange{}
	}
	var out TranscriptEventRange
	for _, event := range events[:end] {
		if event.Seq > 0 {
			if out.EventFrom == 0 {
				out.EventFrom = event.Seq
			}
			out.EventTo = event.Seq
		}
		if event.MessageID != "" {
			if out.MessageFrom == "" {
				out.MessageFrom = event.MessageID
			}
			out.MessageTo = event.MessageID
		}
	}
	return out
}

// RetainedFromForCompactions 从压缩记录的**区间事实**推出保留窗口起点
// （transcript 事件下标；0 = 推不出来），供冷恢复还原
// `TaskExecutionState.ContextRetainedFrom`。判据与口径：
//
//   - 取全部记录里最大的事件序号终点（EventTo）：压缩总是在既有前缀之上继续
//     （events[:compressedTo] 单调变长），最大值即最后一次压缩覆盖到的边界；
//   - 该终点必须能在给定事件流里**按事件序号定位**（存在 Seq == EventTo 的
//     事件），命中的下一条就是保留窗口的首个事件——这与
//     `TaskExecutionState.ContextRetainedFrom` 的语义同源：events[:n] 是已被
//     折出窗口的前缀，events[n:] 才是 provider 侧累积上下文；
//   - 定位不到（老记录没有区间字段、区间落在本次读回的窗口之外）就返回 0，
//     **不猜**：0 与"尚未压缩"同义，后果只是下一次装配可能把已经不在事件流里
//     的旧前缀再计入一次预算（那段前缀本身不可见时会重新生成一个帧，不会重复
//     进上下文），比推一个错边界（可能在会话中段切断历史）安全得多；
//   - 调用方只应在**存储事件流**上用这个函数：从可见会话重建的事件流会重新编码
//     事件序号（`RecordConversationTranscript` 的 Seq = len(events)+1），在那里
//     定位会把保留窗口错误地推到会话中段——丢历史比重折一次严重。
func RetainedFromForCompactions(events []model.TranscriptEvent, compactions []model.ContextCompaction) int {
	eventTo := uint64(0)
	for _, compaction := range compactions {
		if compaction.EventTo > eventTo {
			eventTo = compaction.EventTo
		}
	}
	if eventTo == 0 {
		return 0
	}
	for index, event := range events {
		if event.Seq == eventTo {
			return index + 1
		}
	}
	return 0
}

func transcriptEventMessage(event model.TranscriptEvent) contract.EngineMessage {
	message := contract.EngineMessage{
		Role: providerRoleForTranscriptEvent(event), ReasoningContent: event.ReasoningContent,
		Content:    providerContentForEvent(event),
		ContentSet: true, ToolCallID: event.ToolCallID, Name: event.Name,
	}
	for _, call := range event.ToolCalls {
		message.ToolCalls = append(message.ToolCalls, contract.EngineToolCall{ID: call.ID, Name: call.Name, Arguments: call.Arguments})
	}
	return message
}

// providerContentForEvent 返回事件在 provider wire 上的真实正文：ProviderContent
// 非空时以它为准（记录侧保留的「已发出字节」），否则正文即 Content。视图仍读
// Content（呈现文本）——工具失败/超限的呈现文本只属于视图，进 wire 会让下一轮
// 重投影改写该消息、provider 前缀缓存自该点起失效。
//
// 与 sessionstore.providerContentOrContent 是同一规则的两处实现（应用侧投影 /
// 存储侧投影），改动必须同步。
func providerContentForEvent(event model.TranscriptEvent) string {
	if event.ProviderContent != "" {
		return event.ProviderContent
	}
	return event.Content
}

// providerRoleForTranscriptEvent 把 transcript 事实映射为 provider 可见 role：
// 只有真实用户输入是 user；internal/context 状态材料统一为 system。存储行
// 本身仍保留原 Role/Kind（UI、审计、单元切分继续按事实读取）。
func providerRoleForTranscriptEvent(event model.TranscriptEvent) string {
	if event.Role == "system" {
		return "system"
	}
	if strings.Contains(event.Content, "<!-- seelex:active-skill:") {
		return event.Role
	}
	if event.Kind == model.TranscriptEventKindInternal ||
		event.Role == "internal_user" || event.Role == "context" {
		return "system"
	}
	return event.Role
}

// transcriptProtocolUnit 是一个协议单元及其在 events 中的起始下标。start 是
// 压缩窗口边界的唯一来源（记录用，不重算）。
type transcriptProtocolUnit struct {
	start  int
	events []model.TranscriptEvent
}

// opensRound 报告该单元是否以**真实用户提问**开头（轮次起点）。
//
// 只有真实用户提问开启新轮次：轮内的技能正文与内部材料是 Role=user 的注入事件，
// 但 provider role 映射为 system（见 providerRoleForTranscriptEvent），它们把一轮
// 切成多段，本身不开启新轮。判据必须与 providerRoleForTranscriptEvent 同源——
// 两处对"什么算用户轮"的口径一分叉，压缩窗口就会在上面那种多段轮上判错边界。
//
// 现场（用户报告）：压缩之后模型「丢了目标」——界限的判断把**用户这一轮的提问**
// 判进了压缩区间。根因不是边界落在"一段的中间"，而是它把轮内的**内部材料行**当成了
// 新的轮次起点：材料行在提问**之后**，于是边界停在材料行上，这一轮真正的提问（在
// 材料行之前）被折走，保留窗口里只剩"续写"，模型从此不知道用户要什么。
func (unit transcriptProtocolUnit) opensRound() bool {
	if len(unit.events) == 0 {
		return false
	}
	return isUserQuestionEvent(unit.events[0])
}

// isUserQuestionEvent 判定一条 transcript 事件是否为**真实用户提问**（轮次起点）。
//
// 这是"什么算真实用户输入"的唯一口径，task_context 内三处读者共用：压缩窗口的
// 轮次边界（opensRound）、会话维护目标（sessionMaintenanceObjective，取"最后一条
// 真实用户输入"）、以及任何按"轮次起点"取数的路径。口径一分叉就会出上面那条现场：
// 内部材料行被当成轮次起点 → 压缩边界越过提问行 → 模型失去目标。
//
// 判据（任一不满足即不是提问）：
//   - Role 必须是 user，且不是激活技能正文（ActiveSkillMarker 注入）；
//   - 不能是内部材料：WireMaterial（检查点渲染正文等"给模型看的内部材料"，由生产
//     方置位；正文不带 `<!-- seelex:` 前缀时 Kind 会被归类成 user_input，只有这个
//     标志分得出材料与提问）；
//   - 不能是逻辑归属 system 的行（internal/context 行的角色归属，见
//     applyTranscriptRoleFieldsLocked）；
//   - Kind 必须是用户输入；空串 = 旧数据，回退 role 判定（见 classifyTranscriptEventKind）。
func isUserQuestionEvent(event model.TranscriptEvent) bool {
	if event.Role != "user" || event.WireMaterial || isActiveSkillEvent(event) {
		return false
	}
	if event.RoleName == "system" {
		return false
	}
	switch event.Kind {
	case "", model.TranscriptEventKindUserInput:
		return true
	default:
		return false
	}
}

// transcriptProtocolUnitList 划分协议单元并记录每段在 events 中的起始下标
// （TranscriptTailWindow 与 TranscriptTailHistory 共用同一划分）。
func transcriptProtocolUnitList(events []model.TranscriptEvent) []transcriptProtocolUnit {
	units := make([]transcriptProtocolUnit, 0, len(events))
	for index := 0; index < len(events); {
		event := events[index]
		switch {
		case event.Role == "user":
			if isActiveSkillEvent(event) {
				// 激活技能正文事件是独立 internal 指令轮次：不要求随附
				// assistant 回复（transcriptUserUnit 对孤立 user 的丢弃规则
				// 不适用），单独成单元输出 —— 保证技能正文在 wire 上每轮可见，
				// 且随定稿轮次稳定缓存。
				units = append(units, transcriptProtocolUnit{start: index, events: []model.TranscriptEvent{event}})
				index++
				continue
			}
			unit, next := transcriptUserUnit(events, index)
			if len(unit) > 0 {
				units = append(units, transcriptProtocolUnit{start: index, events: unit})
			}
			index = next
		case event.Role == "assistant" && len(event.ToolCalls) == 0:
			units = append(units, transcriptProtocolUnit{start: index, events: []model.TranscriptEvent{event}})
			index++
		case event.Role == "assistant" && len(event.ToolCalls) > 0:
			unit, next, complete := transcriptToolUnit(events, index)
			if complete {
				units = append(units, transcriptProtocolUnit{start: index, events: unit})
				index = next
			} else if len(unit) > 0 {
				// 残缺（中断）工具链：保留已记录部分为开放单元，从链断裂点
				// 续扫 —— 不整体作废、不连坐跳到下一个 user。缺失的 tool 结果
				// 由装配层 RepairInterruptedToolChains 补齐后再进 provider。
				units = append(units, transcriptProtocolUnit{start: index, events: unit})
				index = next
			} else {
				index = next
			}
		default:
			index++
		}
	}
	return units
}

// transcriptProtocolUnits 只要单元内容（不关心边界）的视图。
func transcriptProtocolUnits(events []model.TranscriptEvent) [][]model.TranscriptEvent {
	listed := transcriptProtocolUnitList(events)
	units := make([][]model.TranscriptEvent, 0, len(listed))
	for _, unit := range listed {
		units = append(units, unit.events)
	}
	return units
}

// isActiveSkillEvent 判定事件是否为激活技能正文 internal 轮次（ActiveSkillMarker
// 开头；与 context_runtime.IsActiveSkillContent 同源字符串判定，task_context
// 不反向依赖）。
func isActiveSkillEvent(event model.TranscriptEvent) bool {
	return event.Role == "user" && strings.HasPrefix(event.Content, ActiveSkillMarker)
}

func transcriptUserUnit(events []model.TranscriptEvent, start int) ([]model.TranscriptEvent, int) {
	unit := []model.TranscriptEvent{events[start]}
	index := start + 1
	for index < len(events) && events[index].Role != "user" {
		event := events[index]
		if event.Role != "assistant" {
			// 孤儿 tool / 异常角色：轮在此终止，产出已保留内容（含 user 与
			// 之前的链部分）；孤儿消息本身由外层 default 跳过，不并入任何轮。
			return unit, index
		}
		if len(event.ToolCalls) == 0 {
			unit = append(unit, event)
			return unit, index + 1
		}
		toolUnit, next, _ := transcriptToolUnit(events, index)
		// 工具链完整或残缺（中断）都并入该轮；残缺链的缺失结果由装配层
		// 补齐。next 落在链断裂点，后续同轮文本/下一 user 不会被跳转丢弃。
		unit = append(unit, toolUnit...)
		index = next
	}
	// 到达下一个 user 或流末：产出开放单元。覆盖三类可见轮次 —— 残缺工具
	// 链收尾（无文本终止点）、无回复的 user 请求（会话关闭/取消/进程在首个
	// 模型响应前退出）、以及异常截断。丢弃它们会让持久化会话在 UI 可见但
	// 冷加载 provider 上下文缺失，重启后 continue 无从继续。
	return unit, index
}

func transcriptToolUnit(events []model.TranscriptEvent, start int) ([]model.TranscriptEvent, int, bool) {
	assistant := events[start]
	wanted := make(map[string]struct{}, len(assistant.ToolCalls))
	for _, call := range assistant.ToolCalls {
		if call.ID == "" {
			return nil, start + 1, false
		}
		if _, duplicate := wanted[call.ID]; duplicate {
			return nil, start + 1, false
		}
		wanted[call.ID] = struct{}{}
	}
	unit := []model.TranscriptEvent{assistant}
	seen := make(map[string]struct{}, len(wanted))
	index := start + 1
	for index < len(events) && len(seen) < len(wanted) {
		event := events[index]
		if event.Role != "tool" {
			break
		}
		if _, ok := wanted[event.ToolCallID]; !ok {
			break
		}
		if _, duplicate := seen[event.ToolCallID]; duplicate {
			break
		}
		seen[event.ToolCallID] = struct{}{}
		unit = append(unit, event)
		index++
	}
	return unit, index, len(seen) == len(wanted)
}
