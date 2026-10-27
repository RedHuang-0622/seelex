package context

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// WireRow is the context projection of one persisted message row.
type WireRow struct {
	Seq              uint64
	Kind             string
	Role             string
	ReasoningContent string
	Content          string
	ProviderContent  string
	ToolCallID       string
	Name             string
	ToolCalls        []WireToolCall
	ResultRef        string
	TokenCount       int
	WireMaterial     bool
}

// WireAttempt is one retry attempt attached to a persisted message anchor.
type WireAttempt struct {
	Role    string
	Content string
}

// WireAttemptProvider supplies the recent attempts for one message anchor.
type WireAttemptProvider interface {
	RecentForAnchor(anchorSeq uint64, k int) []WireAttempt
}

// WireFrame is the compact summary and published tail boundary applied to a wire.
type WireFrame struct {
	Summary      string
	MessageToSeq uint64
}

func providerContent(row WireRow) string {
	if row.ProviderContent != "" {
		return row.ProviderContent
	}
	return row.Content
}

// AssembleWireRows builds a deterministic provider wire without performing I/O.
func AssembleWireRows(frame *WireFrame, rows []WireRow, attempts WireAttemptProvider, params WireParams) WireResult {
	params = NormalizeWireParams(params)
	tailStart := uint64(1)
	if frame != nil {
		tailStart = frame.MessageToSeq + 1
	}
	state := &wireState{
		params: params, declared: make(map[string]bool), pending: make(map[string]bool),
	}
	if frame != nil {
		if frame.Summary != "" {
			state.wire = append(state.wire, WireMessage{Role: WireRoleAssistant, Content: frame.Summary})
			state.costs += len(frame.Summary) / 4
		}
	}
	for _, row := range rows {
		state.consumeRow(row)
	}
	state.closeTail()
	state.attachAttempts(attempts)
	result := WireResult{
		Messages: state.wire, PrefixDigest: WireDigest(state.wire),
		TailStartSeq: tailStart, FrameApplied: frame != nil, Open: state.open,
	}
	result.NeedCompact = state.needCompact
	return result
}

type wireState struct {
	params      WireParams
	wire        []WireMessage
	declared    map[string]bool
	pending     map[string]bool
	costs       int
	stopped     bool
	needCompact bool
	open        bool
}

func (state *wireState) emit(message WireMessage, cost int) {
	if state.stopped {
		return
	}
	state.wire = append(state.wire, message)
	state.costs += cost
}

func unitCost(row WireRow) int {
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

func (state *wireState) consumeRow(row WireRow) {
	if state.stopped {
		return
	}
	switch row.Kind {
	case "user_input":
		state.closePendingBeforeUser()
		cost := unitCost(row)
		if state.budgetWouldExceed(cost, true) {
			state.needCompact = true
			state.stopped = true
			return
		}
		state.emit(WireMessage{Role: WireRoleUser, Content: row.Content, Seq: row.Seq}, cost)
	case "llm", "tool_call":
		message := WireMessage{
			Role: WireRoleAssistant, Content: row.Content,
			ReasoningContent: row.ReasoningContent, ToolCalls: row.ToolCalls, Seq: row.Seq,
		}
		cost := unitCost(row)
		if state.budgetWouldExceed(cost, len(state.pending) == 0) {
			state.needCompact = true
			state.stopped = true
			return
		}
		state.emit(message, cost)
		if len(row.ToolCalls) > 0 {
			for _, call := range row.ToolCalls {
				state.declared[call.ID] = true
				state.pending[call.ID] = true
			}
		}
	case "tool_output":
		if row.ToolCallID != "" && !state.declared[row.ToolCallID] {
			return
		}
		cost := unitCost(row)
		if state.budgetWouldExceed(cost, len(state.pending) == 0) {
			state.needCompact = true
			state.stopped = true
			return
		}
		state.emit(WireMessage{
			Role: WireRoleTool, Content: providerContent(row), ToolCallID: row.ToolCallID,
			Name: row.Name, ResultRef: row.ResultRef, Seq: row.Seq,
		}, cost)
		delete(state.pending, row.ToolCallID)
	default:
		if row.WireMaterial && (row.Role == "internal_user" || row.Role == "context" || row.Kind == "internal") {
			cost := unitCost(row)
			if state.budgetWouldExceed(cost, len(state.pending) == 0) {
				state.needCompact = true
				state.stopped = true
				return
			}
			role := WireRoleSystem
			if strings.Contains(row.Content, "<!-- seelex:active-skill:") {
				role = WireRoleUser
			}
			state.emit(WireMessage{Role: role, Content: row.Content, Seq: row.Seq, Internal: true}, cost)
		}
	}
}

func (state *wireState) budgetWouldExceed(cost int, boundaryComplete bool) bool {
	if state.costs+cost <= state.params.Budget {
		return false
	}
	if !boundaryComplete {
		state.needCompact = true
		return false
	}
	return true
}

func (state *wireState) closePendingBeforeUser() {
	for callID := range state.pending {
		state.emit(WireMessage{
			Role: WireRoleTool, ToolCallID: callID,
			Content: "【缺失工具结果 · 修复占位】该工具调用未在流中完成，装配层已按安全修复补齐。",
			Repair:  true,
		}, 8)
		delete(state.pending, callID)
	}
}

func (state *wireState) closeTail() {
	if len(state.pending) == 0 {
		return
	}
	state.open = true
	for callID := range state.pending {
		state.emit(WireMessage{
			Role: WireRoleTool, ToolCallID: callID,
			Content: "【缺失工具结果 · 修复占位】工具轮在流尾保持 open，结果缺失。",
			Repair:  true,
		}, 8)
		delete(state.pending, callID)
	}
}

func (state *wireState) attachAttempts(provider WireAttemptProvider) {
	if provider == nil {
		return
	}
	anchors := make(map[uint64]bool)
	for _, message := range state.wire {
		if message.Seq != 0 {
			anchors[message.Seq] = true
		}
	}
	out := make([]WireMessage, 0, len(state.wire)+8)
	for _, message := range state.wire {
		out = append(out, message)
		if !anchors[message.Seq] {
			continue
		}
		for _, attempt := range provider.RecentForAnchor(message.Seq, state.params.K) {
			out = append(out, WireMessage{
				Role: roleForAttempt(attempt.Role), Content: attempt.Content,
				Seq: message.Seq, Attempt: true,
			})
		}
	}
	state.wire = out
}

func roleForAttempt(role string) string {
	if role == "" {
		return WireRoleAssistant
	}
	if role == WireRoleTool || role == WireRoleUser || role == WireRoleAssistant {
		return role
	}
	return WireRoleAssistant
}

// WireDigest returns the stable digest of one assembled wire.
func WireDigest(messages []WireMessage) string {
	sum := sha256.New()
	for _, message := range messages {
		data, _ := json.Marshal(message)
		sum.Write(data)
		sum.Write([]byte{'\n'})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// WirePrefixEqual compares persistent wire messages while ignoring retry attempts.
func WirePrefixEqual(left, right []WireMessage) bool {
	strip := func(messages []WireMessage) []WireMessage {
		out := make([]WireMessage, 0, len(messages))
		for _, message := range messages {
			if !message.Attempt {
				out = append(out, message)
			}
		}
		return out
	}
	left, right = strip(left), strip(right)
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
