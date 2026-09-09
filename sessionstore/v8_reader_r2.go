// R2 装配读取器（v8.1 §5）：会话事实 → 下一次发给 LLM 的 wire 消息序列。
//
// 定位：
//   - 纯函数、只读、无副作用；只消费 guide/module head/message/compact +
//     运行期尝试缓存 + 请求参数，**不读取 EVENT**（R2-EVENT-1）；
//   - wire = compact 摘要正文 + 最新帧 message_to 之后的事件行 + 同一操作
//     最近 K 条尝试；预算超限停在最近完整协议单元边界并置 need_compact；
//   - 孤儿 tool 行跳过；残缺工具轮保留为 open 单元 + 修复占位，不跳后续
//     user（R2-OPEN-1 / R2-ORPHAN-1 / R2-FILTER-1）；
//   - 输出前缀稳定性：无新消息、同参数、同缓存状态下两次输出逐字节一致
//     （R2-STABLE-1）。
package sessionstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// v8WireRole 常量与 provider role 对齐（internal 材料以 user 形态进入 wire）。
const (
	v8WireRoleUser      = "user"
	v8WireRoleAssistant = "assistant"
	v8WireRoleTool      = "tool"
)

// v8WireMessage 是 R2 输出的一条 wire 消息。
type v8WireMessage struct {
	Role             string          `json:"role"`
	Content          string          `json:"content,omitempty"`
	ReasoningContent string          `json:"reasoning_content,omitempty"`
	ToolCalls        []EventToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string          `json:"tool_call_id,omitempty"`
	Name             string          `json:"name,omitempty"`
	ResultRef        string          `json:"result_ref,omitempty"`
	Seq              uint64          `json:"seq,omitempty"`
	// Internal 标记内部 user 材料（wire_material=true 的 internal/context 行）。
	Internal bool `json:"internal,omitempty"`
	// Repair 标记修复占位（缺失 tool 结果）。
	Repair bool `json:"repair,omitempty"`
	// Attempt 标记尝试缓存拼接行（不参与持久前缀稳定性承诺）。
	Attempt bool `json:"attempt,omitempty"`
}

// v8R2Params 是 R2 请求参数（budget 为估算字符预算；K 默认 3；Repair 开关
// 默认开）。
type v8R2Params struct {
	K      int  `json:"k,omitempty"`
	Budget int  `json:"budget,omitempty"`
	Repair bool `json:"repair,omitempty"`
}

func (params v8R2Params) normalized() v8R2Params {
	if params.K <= 0 {
		params.K = 3
	}
	if params.Budget <= 0 {
		params.Budget = 200_000
	}
	params.Repair = true
	return params
}

// v8R2Result 是 R2 输出。
type v8R2Result struct {
	Messages []v8WireMessage `json:"messages"`
	// NeedCompact 表示超预算且已停在完整单元边界（由压缩路径处理）。
	NeedCompact bool `json:"need_compact"`
	// PrefixDigest 是输出版本摘要（前缀稳定性比较用）。
	PrefixDigest string `json:"prefix_digest"`
	// TailStartSeq 是本轮实际装配的起始 seq（无 frame = 1）。
	TailStartSeq uint64 `json:"tail_start_seq"`
	// FrameApplied 表示是否存在 compact 摘要进入 wire。
	FrameApplied bool `json:"frame_applied,omitempty"`
	// Open 表示流尾存在未完成工具轮（open 单元 + 修复占位）。
	Open bool `json:"open,omitempty"`
}

// v8AssembleWire 构造 wire 消息序列（R2 主入口）。
func (store *v8Store) v8AssembleWire(key Key, cache *v8AttemptCache, params v8R2Params) (v8R2Result, error) {
	params = params.normalized()
	compactHead, err := store.v8ReadCompactHead(key)
	if err != nil {
		return v8R2Result{}, err
	}
	tailStart := uint64(1)
	frame := compactHead.LatestFrame
	if frame != nil {
		tailStart = frame.MessageToSeq + 1
	}
	rows, err := store.v8ReadRows(key, tailStart, 0)
	if err != nil {
		return v8R2Result{}, err
	}
	state := v8WireState{
		params:   params,
		declared: make(map[string]bool),
		pending:  make(map[string]bool),
	}
	if frame != nil {
		state.summary = frame.Summary
		if frame.Summary != "" {
			state.wire = append(state.wire, v8WireMessage{
				Role:    v8WireRoleAssistant,
				Content: frame.Summary,
			})
			state.costs += len(frame.Summary) / 4
		}
	}
	// 逐行装配（只依赖 message/compact；EVENT 不参与）。
	for _, row := range rows {
		state.consumeRow(row)
	}
	state.closeTail()
	state.attachAttempts(cache)
	wire := state.wire
	if state.needCompact {
		return v8R2Result{
			Messages:     wire,
			NeedCompact:  true,
			PrefixDigest: v8WireDigest(wire),
			TailStartSeq: tailStart,
			FrameApplied: frame != nil,
			Open:         state.open,
		}, nil
	}
	return v8R2Result{
		Messages:     wire,
		PrefixDigest: v8WireDigest(wire),
		TailStartSeq: tailStart,
		FrameApplied: frame != nil,
		Open:         state.open,
	}, nil
}

// v8WireState 是 R2 装配的增量状态（纯函数内部结构）。
type v8WireState struct {
	params  v8R2Params
	summary string
	wire    []v8WireMessage
	// declared 是扫描窗口内全部宣告过的 tool_call_id（孤儿判定）。
	declared map[string]bool
	// pending 是等待结果、尚未关闭的 tool_call_id。
	pending map[string]bool
	// costs 是已装配估算字符。
	costs int
	// stopped 表示预算截断已发生（后续行不再装配）。
	stopped     bool
	needCompact bool
	open        bool
	// unitOpen 表示当前装配位置处于未完成工具轮。
	unitOpen bool
}

func (state *v8WireState) emit(message v8WireMessage, cost int) {
	if state.stopped {
		return
	}
	state.wire = append(state.wire, message)
	state.costs += cost
}

// unitCost 估算一条 wire 消息的字符成本（TokenCount 缺失时 len/4 近似）。
func unitCost(row Event) int {
	if row.TokenCount > 0 {
		return row.TokenCount
	}
	cost := len(row.Content)/4 + len(row.ReasoningContent)/4
	for _, call := range row.ToolCalls {
		cost += len(call.Name) + len(call.Arguments)/4
	}
	if cost == 0 {
		cost = 1
	}
	return cost
}

func (state *v8WireState) consumeRow(row Event) {
	if state.stopped {
		return
	}
	kind := EventKindOf(row)
	role := row.Role
	switch kind {
	case EventKindUserInput:
		state.closePendingBeforeUser()
		cost := unitCost(row)
		if state.budgetWouldExceed(cost, true) {
			state.needCompact = true
			state.stopped = true
			return
		}
		state.emit(v8WireMessage{Role: v8WireRoleUser, Content: row.Content, Seq: row.Seq}, cost)
		state.unitOpen = false
	case EventKindLLM, EventKindToolCall:
		message := v8WireMessage{
			Role:             v8WireRoleAssistant,
			Content:          row.Content,
			ReasoningContent: row.ReasoningContent,
			ToolCalls:        row.ToolCalls,
			Seq:              row.Seq,
		}
		cost := unitCost(row)
		if state.budgetWouldExceed(cost, len(state.pending) == 0) {
			state.needCompact = true
			state.stopped = true
			return
		}
		state.emit(message, cost)
		if len(row.ToolCalls) > 0 {
			state.unitOpen = true
			for _, call := range row.ToolCalls {
				state.declared[call.ID] = true
				state.pending[call.ID] = true
			}
		} else {
			state.unitOpen = false
		}
	case EventKindToolOutput:
		if row.ToolCallID != "" && !state.declared[row.ToolCallID] {
			return // 孤儿 tool 行：扫描窗口无匹配宣告，跳过（R2-ORPHAN-1）
		}
		cost := unitCost(row)
		if state.budgetWouldExceed(cost, len(state.pending) == 0) {
			state.needCompact = true
			state.stopped = true
			return
		}
		state.emit(v8WireMessage{
			Role:       v8WireRoleTool,
			Content:    row.Content,
			ToolCallID: row.ToolCallID,
			Name:       row.Name,
			ResultRef:  row.ResultRef,
			Seq:        row.Seq,
		}, cost)
		delete(state.pending, row.ToolCallID)
		if len(state.pending) == 0 {
			state.unitOpen = false
		}
	default:
		// internal_user/context：仅 wire_material=true 进 wire（内部 user 材料），
		// 其余跳过（只服务前端/历史，R2-FILTER-1）。
		if row.WireMaterial && (role == "internal_user" || role == "context" || kind == EventKindInternal) {
			cost := unitCost(row)
			if state.budgetWouldExceed(cost, len(state.pending) == 0) {
				state.needCompact = true
				state.stopped = true
				return
			}
			state.emit(v8WireMessage{Role: v8WireRoleUser, Content: row.Content, Seq: row.Seq, Internal: true}, cost)
		}
	}
}

// budgetWouldExceed 判断添加 cost 是否会超软预算；boundaryComplete 表示
// 当前处于完整单元边界（可安全截断）。
func (state *v8WireState) budgetWouldExceed(cost int, boundaryComplete bool) bool {
	if state.costs+cost <= state.params.Budget {
		return false
	}
	if !boundaryComplete {
		// 残缺工具轮内：允许完成当前单元（open 修复占位），截断标记交给
		// closeTail。
		state.needCompact = true
		return false
	}
	return true
}

// closePendingBeforeUser 在 user 边界修复仍未完成的工具轮（不跳后续 user）。
func (state *v8WireState) closePendingBeforeUser() {
	for callID := range state.pending {
		state.emit(v8WireMessage{
			Role:       v8WireRoleTool,
			ToolCallID: callID,
			Content:    "【缺失工具结果 · 修复占位】该工具调用未在流中完成，装配层已按安全修复补齐。",
			Repair:     true,
		}, 8)
		delete(state.pending, callID)
	}
	state.unitOpen = false
}

// closeTail 处理流尾：未完成工具轮保留 open + 修复占位。
func (state *v8WireState) closeTail() {
	if len(state.pending) > 0 {
		state.open = true
		for callID := range state.pending {
			state.emit(v8WireMessage{
				Role:       v8WireRoleTool,
				ToolCallID: callID,
				Content:    "【缺失工具结果 · 修复占位】工具轮在流尾保持 open，结果缺失。",
				Repair:     true,
			}, 8)
			delete(state.pending, callID)
		}
		state.unitOpen = false
	}
}

// attachAttempts 在尝试缓存锚点行后拼接最近 K 条尝试（锚点行必须在 wire 中）。
func (state *v8WireState) attachAttempts(cache *v8AttemptCache) {
	if cache == nil {
		return
	}
	anchors := make(map[uint64]bool)
	for _, message := range state.wire {
		if message.Seq != 0 {
			anchors[message.Seq] = true
		}
	}
	if len(anchors) == 0 {
		return
	}
	out := make([]v8WireMessage, 0, len(state.wire)+8)
	for _, message := range state.wire {
		out = append(out, message)
		if !anchors[message.Seq] {
			continue
		}
		for _, attempt := range cache.recentForAllAnchors(message.Seq, state.params.K) {
			out = append(out, v8WireMessage{
				Role:    roleForAttempt(attempt.Role),
				Content: attempt.Content,
				Seq:     message.Seq,
				Attempt: true,
			})
		}
	}
	state.wire = out
}

func roleForAttempt(role string) string {
	if role == "" {
		return v8WireRoleAssistant
	}
	if role == v8WireRoleTool || role == v8WireRoleUser || role == v8WireRoleAssistant {
		return role
	}
	return v8WireRoleAssistant
}

// v8WireDigest 计算 wire 序列版本摘要（前缀稳定性比较基础）。
func v8WireDigest(messages []v8WireMessage) string {
	sum := sha256.New()
	for _, message := range messages {
		data, _ := json.Marshal(message)
		sum.Write(data)
		sum.Write([]byte{'\n'})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// v8WirePrefixEqual 判断两次 wire 输出的持久前缀逐字节一致：比较去掉
// Attempt 行后的完整序列。
func v8WirePrefixEqual(left, right []v8WireMessage) bool {
	strip := func(messages []v8WireMessage) []v8WireMessage {
		out := make([]v8WireMessage, 0, len(messages))
		for _, message := range messages {
			if !message.Attempt {
				out = append(out, message)
			}
		}
		return out
	}
	left = strip(left)
	right = strip(right)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		lm, _ := json.Marshal(left[index])
		rm, _ := json.Marshal(right[index])
		if string(lm) != string(rm) {
			return false
		}
	}
	return true
}

// recentForAllAnchors 归并 AttemptCache.RecentFor 的辅助（按 op 归组取最近
// K；当前单 op 场景与测试一致）。
func (cache *v8AttemptCache) recentForAllAnchors(anchorSeq uint64, k int) []v8Attempt {
	seen := make(map[string]bool)
	var out []v8Attempt
	items := cache.snapshot()
	for _, item := range items {
		if item.AnchorSeq != anchorSeq || seen[item.OperationKey] {
			continue
		}
		seen[item.OperationKey] = true
		out = append(out, cache.RecentFor(anchorSeq, item.OperationKey, k)...)
	}
	return out
}

func (cache *v8AttemptCache) snapshot() []v8Attempt {
	if cache == nil {
		return nil
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return append([]v8Attempt(nil), cache.items...)
}
