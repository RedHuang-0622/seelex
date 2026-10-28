package integration

import (
	"context"
	"strings"
	"testing"

	seelesession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/backend/engine/context/seelectx/tokens"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/react"
	"github.com/RedHuang-0622/seelex/kernel/task"
)

const prefixProbeRequestID = "prefix-probe-1"

// probeInputTrailerLen 是 serializeRequest 尾部「\n||\n + INPUT 空串」的长度
// （冒烟用例把当前输入与工具目录放在历史之后；LCP 度量只取历史字节流）。
var probeInputTrailerLen = len("\n||\n") + len(serText("INPUT", ""))

// prefixProbeConfig 是一次探针运行的装配变体（用于归因失效来源）。
type prefixProbeConfig struct {
	name string
	// engineKeepsToolContent = 引擎 assistant 工具调用消息是否保留正文。
	// 生产路径必须为 true；false 只用于守住旧缺陷的回归对照。
	// false = 生产实测（Seele loop.callLLM 在 tool_calls 时把 Content 置 nil，
	// 正文只经 onChunk 进视图）；true = 对照（正文进引擎历史与 transcript）。
	engineKeepsToolContent bool
}

// prefixProbeCall 是一次「会发给 provider 的请求」快照。
type prefixProbeCall struct {
	label    string
	turn     int
	iter     int
	history  int
	total    int
	bytes    string
	messages []contract.EngineMessage
}

// prefixProbeDivergence 是两次请求之间的前缀发散度量。
type prefixProbeDivergence struct {
	prevLabel     string
	curLabel      string
	prevBytes     int
	curBytes      int
	lcpBytes      int
	prefixIntact  bool
	firstDiffIdx  int
	firstDiffRole string
	sharedTok     int
	totalTok      int
	invalidated   int
	ratioRaw      float64
	ratio64       float64
	ratio1k       float64
	invalidShare  float64
}

// prefixProbeHarness 驱动生产装配路径并镜像 Seele 循环的工作历史。
type prefixProbeHarness struct {
	t       *testing.T
	service *legacyTestService
	engine  *fakeEngine
	runtime *fakeRuntime
	session string
	tools   []model.Tool
	history []contract.EngineMessage
	calls   []prefixProbeCall
	// lastSystem 是上一次 capture 时的 system prompt（用于检测回合内 system 漂移）。
	lastSystem string
}

func newPrefixProbeHarness(t *testing.T, window int) *prefixProbeHarness {
	t.Helper()
	engine := &fakeEngine{}
	runtime := &fakeRuntime{}
	service := newLegacyTestService(t, engine, withTestRuntime(runtimeWithContextLimits{
		fakeRuntime: runtime, window: window, output: 8192,
	}))
	sessionID := engine.SessionID()
	service.ViewMu.Lock()
	service.Core.Snapshot.Session.ID = sessionID
	service.Core.Snapshot.Chat = model.ChatState{Running: true, RequestID: prefixProbeRequestID}
	service.components.tasks.BeginTask(prefixProbeRequestID, "cross-turn prefix divergence probe", "high", nil, model.TaskCheckpoint{})
	service.ViewMu.Unlock()
	return &prefixProbeHarness{
		t: t, service: service, engine: engine, runtime: runtime, session: sessionID,
		tools: service.Deps.Runtime.VisibleTools(context.Background()),
	}
}

// startStream 绑定本会话的流式输出（生产 runChat 的 newBatchedDeltaSink 边界）。
func (h *prefixProbeHarness) startStream() {
	h.service.ViewMu.Lock()
	h.service.sessionUnitLocked(h.session).SetStream(chat.NewVisibleOutputStream(prefixProbeRequestID))
	h.service.ViewMu.Unlock()
}

// beginTurn 复刻 runChat 开头 + Seele 循环开头的生产顺序：
// 可见消息占位（startChatFor）→ transcript user 事件 → PrepareExecutionContextFor
// 装配 → 循环把当前输入作为最后一条 user 消息追加进工作历史
// （Seele loop.go: `rl.history = append(rl.history, types.Message{Role:"user", ...})`）。
//
// 流按**每轮一个**重建：生产每次 runChat 都 `SetStream(NewVisibleOutputStream(
// requestID))`（chat.go:90/355/691/709），一个流只属于一次请求；本探针此前只建一次、
// 三代轮次共用（当时没有任何读路径读流内容，无差别），而工具轮说明正文的归位现在会在
// 回看态下读这份请求作用域的流缓冲（application/assembly/chat.go 的
// streamedAssistantTextLocked）——夹具必须与生产同形，否则第二轮读到的会是上一轮的
// 正文（跨轮漂移）。断言（每个工具轮事件只承载自己那次迭代的正文）不因此放宽。
func (h *prefixProbeHarness) beginTurn(input string) {
	h.t.Helper()
	h.startStream()
	h.service.ViewMu.Lock()
	h.service.components.view.AppendMessageLocked("user", input, nil)
	h.service.components.view.AppendMessageLocked("assistant", "", nil)
	h.service.components.tasks.AppendTranscriptEventLocked(model.TranscriptEvent{
		TaskID: prefixProbeRequestID, Role: "user", Content: input,
	})
	h.service.ViewMu.Unlock()
	currentInput, err := h.service.components.context.PrepareExecutionContextFor(h.session, prefixProbeRequestID, input)
	if err != nil {
		h.t.Fatalf("prepare execution context: %v", err)
	}
	h.history = append([]contract.EngineMessage(nil), h.engine.History()...)
	// 当前输入（可能含工作打点表块）由循环追加为工作历史最后一条 user 消息。
	h.history = append(h.history, contract.EngineMessage{Role: "user", Content: currentInput, ContentSet: true})
}

// streamText 走生产流式链路写可见正文（跨 ReAct 迭代追加到同一条 assistant
// 消息，即 appendVisibleDelta 语义）。
func (h *prefixProbeHarness) streamText(text string) {
	if text == "" {
		return
	}
	for _, chunk := range splitProbeChunks(text, 32) {
		h.service.components.conversationStreamRuntime.AppendDelta(prefixProbeRequestID, chunk)
	}
}

// llmIteration 复刻一次 LLM 调用之后的引擎/transcript 状态：
// 引擎工作历史追加 assistant 消息；OnLLMComplete 钩子落 transcript 事件。
func (h *prefixProbeHarness) llmIteration(engineContent, reasoning string, calls []types.ToolCall) {
	h.t.Helper()
	var content *string
	if engineContent != "" {
		value := engineContent
		content = &value
	}
	h.engine.AppendHistory(contract.EngineMessage{
		Role: "assistant", Content: engineContent, ContentSet: content != nil,
		ReasoningContent: reasoning, ToolCalls: probeEngineCalls(calls),
	})
	h.history = append(h.history, contract.EngineMessage{
		Role: "assistant", Content: engineContent, ContentSet: content != nil,
		ReasoningContent: reasoning, ToolCalls: probeEngineCalls(calls),
	})
	ctx := withSessionID(context.Background(), h.session)
	h.service.components.tasks.RecordLLMComplete(ctx, seelesession.LLMInfo{
		Turn: 0, Response: engineContent, ToolCalls: calls,
	})
}

// toolRound 复刻 ToolHookBridge 的 OnToolStart/OnToolComplete 生产投影。
func (h *prefixProbeHarness) toolRound(name, callID, arguments, result string) {
	h.t.Helper()
	h.service.ViewMu.Lock()
	h.service.components.tasks.EnsureToolCallTranscriptLocked(h.session, name, callID, arguments)
	// 生产 handleToolStart 的归位步骤：把本次迭代的说明正文（视图累积 − 本轮
	// 已归位前缀）挂到刚就位的 tool_call 事件上——调用同一个生产方法，不在
	// 探针里复制算法。传入的 ID 与生产同形（桥接层合成值，框架真实调用 ID 由
	// pendingProviderCalls 解析）。
	h.service.components.tasks.AttributeToolNarrationLocked(h.session,
		model.TranscriptToolCall{ID: "tool-" + callID, Name: name, Arguments: arguments},
		h.service.components.conversationStreamRuntime.StreamedAssistantTextLocked(h.session))
	h.service.components.view.AppendMessageLocked("tool", "", &model.ToolCall{ID: callID, Name: name, Arguments: arguments, Status: dto.ToolEventRunning})
	providerResult, _ := h.service.components.tasks.RecordToolTranscriptLocked(h.session, name, callID, arguments, result, nil)
	h.service.components.tasks.ObserveTool(task_context.ToolObservation{RequestID: prefixProbeRequestID, Name: name, Result: providerResult})
	h.service.ViewMu.Unlock()
	h.engine.AppendHistory(contract.EngineMessage{Role: "tool", ToolCallID: callID, Name: name, Content: result, ContentSet: true})
	h.history = append(h.history, contract.EngineMessage{Role: "tool", ToolCallID: callID, Name: name, Content: result, ContentSet: true})
	if providerResult != result {
		h.t.Logf("  note: transcript layer rewrote provider-safe tool result (%d -> %d chars)", len(result), len(providerResult))
	}
}

// finishTurn 复刻 runChat 的回合收尾（chat.go:175/180）：补终态 assistant 事件 +
// 回填推理草稿。工具轮说明正文的归位已前移到工具钩子边界（toolRound），回合收尾
// 不再做“事后填充”——否则叙述会落到别的轮次/会话。
func (h *prefixProbeHarness) finishTurn(reply string) {
	h.t.Helper()
	h.service.components.tasks.EnsureFinalAssistantTranscript(prefixProbeRequestID, reply)
	h.service.components.tasks.BackfillAssistantReasoning(h.session, h.engine.History())
	after := h.toolCallEventContents()
	attributed := 0
	for index := range after {
		if after[index] != "" {
			attributed++
		}
	}
	h.t.Logf("  end-of-turn: transcript assistant(tool) events=%d narration-attributed=%d view-assistant-text=%q",
		len(after), attributed, summarizeProbeText(h.visibleAssistantText()))
	for index := range after {
		h.t.Logf("    transcript assistant(tool) #%d content=%q", index, summarizeProbeText(after[index]))
	}
}

// capture 固化当前工作历史会产生的 provider 请求（serializeRequest 与冒烟
// 用例同款；当前输入已作为工作历史最后一条 user 消息，故 input 参数留空）。
//
// 字节 LCP 用 serializeRequest(system, history, "", nil) 度量：冒烟用例把工具
// 目录放在序列化**尾部**（工具跨轮常量），若把工具目录算进 LCP，回合内
// 「追加消息」会被尾部常量块打断而误判为前缀不完整。工具/schema 部分因此
// 单列为常量（token 数见日志），不计入 LCP。
func (h *prefixProbeHarness) capture(turn, iter int, label string) {
	h.t.Helper()
	system := systemPromptFor(h.service)
	if h.lastSystem != "" && h.lastSystem != system {
		offset := lcpBytes(h.lastSystem, system)
		h.t.Logf("  SYSTEM PROMPT CHANGED at byte %d/%d: before=%q after=%q",
			offset, len(system), probeWindow(h.lastSystem, offset), probeWindow(system, offset))
	}
	h.lastSystem = system
	serialized := serializeRequest(system, h.history, "", nil)
	// serializeRequest 把 INPUT/tools 放在序列化尾部（跨轮常量）。LCP 度量只看
	// 「system + 历史消息」字节流：否则回合内追加的消息会被尾部的 INPUT 占位
	// 打断，误判为前缀不完整。
	serialized = serialized[:len(serialized)-probeInputTrailerLen]
	total := h.service.components.tasks.CountRequestTokens(system, h.history, "", h.tools)
	h.calls = append(h.calls, prefixProbeCall{
		label: label, turn: turn, iter: iter, history: len(h.history),
		total: total, bytes: serialized, messages: append([]contract.EngineMessage(nil), h.history...),
	})
	h.t.Logf("  call=%-10s turn=%d iter=%d msgs=%3d total=%6d tok", label, turn, iter, len(h.history), total)
}

// toolCallEventContents 返回 transcript 中 assistant 工具调用事件的正文。
func (h *prefixProbeHarness) toolCallEventContents() []string {
	events := h.service.components.tasks.TranscriptFor(h.session)
	contents := make([]string, 0, len(events))
	for _, event := range events {
		if event.Role == "assistant" && len(event.ToolCalls) > 0 {
			contents = append(contents, event.Content)
		}
	}
	return contents
}

// visibleAssistantText 返回可见投影中最后一条 assistant 正文（生产视图里
// 一个回合的流式正文跨迭代累积到同一条消息）。
func (h *prefixProbeHarness) visibleAssistantText() string {
	conversation := h.service.components.frontendSnapshot.Snapshot().Conversation
	for index := len(conversation) - 1; index >= 0; index-- {
		if conversation[index].Role == "assistant" && conversation[index].Tool == nil {
			return conversation[index].Content
		}
	}
	return ""
}

// ── 度量 ────────────────────────────────────────────────────────────────

// prefixProbeCompare 计算 prev → cur 的字节 LCP 与失效后缀（block 粒度按
// provider 整块缓存语义对齐）。
func prefixProbeCompare(prev, cur prefixProbeCall) prefixProbeDivergence {
	divergence := prefixProbeDivergence{
		prevLabel: prev.label, curLabel: cur.label,
		prevBytes: len(prev.bytes), curBytes: len(cur.bytes), totalTok: cur.total,
	}
	sharedBytes := lcpBytes(prev.bytes, cur.bytes)
	divergence.lcpBytes = sharedBytes
	divergence.prefixIntact = sharedBytes == len(prev.bytes)
	divergence.firstDiffIdx, divergence.firstDiffRole = prefixProbeFirstDiff(prev.messages, cur.messages)
	divergence.sharedTok = tokens.Count(cur.bytes[:sharedBytes])
	divergence.invalidated = cur.total - divergence.sharedTok
	if cur.total > 0 {
		divergence.ratioRaw = float64(divergence.sharedTok) / float64(cur.total)
		divergence.ratio64 = float64(blockFloor(divergence.sharedTok, 64)) / float64(cur.total)
		divergence.ratio1k = float64(blockFloor(divergence.sharedTok, 1024)) / float64(cur.total)
		divergence.invalidShare = 1 - divergence.ratioRaw
	}
	return divergence
}

func prefixProbeFirstDiff(prev, cur []contract.EngineMessage) (int, string) {
	limit := len(prev)
	if len(cur) < limit {
		limit = len(cur)
	}
	for index := 0; index < limit; index++ {
		if probeMessagesEqual(prev[index], cur[index]) {
			continue
		}
		return index, prev[index].Role
	}
	if len(cur) > len(prev) {
		return len(prev), "appended-tail"
	}
	return -1, "prefix"
}

func probeMessagesEqual(left, right contract.EngineMessage) bool {
	if left.Role != right.Role || left.Content != right.Content ||
		left.ReasoningContent != right.ReasoningContent ||
		left.ToolCallID != right.ToolCallID || left.Name != right.Name {
		return false
	}
	if len(left.ToolCalls) != len(right.ToolCalls) {
		return false
	}
	for index := range left.ToolCalls {
		if left.ToolCalls[index].ID != right.ToolCalls[index].ID ||
			left.ToolCalls[index].Name != right.ToolCalls[index].Name ||
			left.ToolCalls[index].Arguments != right.ToolCalls[index].Arguments {
			return false
		}
	}
	return true
}

func probeEngineCalls(calls []types.ToolCall) []contract.EngineToolCall {
	out := make([]contract.EngineToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, contract.EngineToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
	}
	return out
}

func probeCallIDs(calls []contract.EngineToolCall) string {
	ids := make([]string, 0, len(calls))
	for _, call := range calls {
		ids = append(ids, call.ID)
	}
	return strings.Join(ids, ",")
}

// probeStreamedContent 返回对照臂下引擎 assistant 工具调用消息的正文。
func probeStreamedContent(cfg prefixProbeConfig) string {
	if cfg.engineKeepsToolContent {
		return "narration (model output preserved)"
	}
	return "narration (dropped by regression arm)"
}

func splitProbeChunks(text string, size int) []string {
	if len(text) <= size {
		return []string{text}
	}
	chunks := make([]string, 0, len(text)/size+1)
	for start := 0; start < len(text); start += size {
		end := start + size
		if end > len(text) {
			end = len(text)
		}
		chunks = append(chunks, text[start:end])
	}
	return chunks
}

func summarizeProbeText(text string) string {
	const limit = 72
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}

// probeWindow 返回 text 中 offset 附近的可读窗口（定位 system 漂移点）。
func probeWindow(text string, offset int) string {
	const span = 60
	start := offset - span
	if start < 0 {
		start = 0
	}
	end := offset + span
	if end > len(text) {
		end = len(text)
	}
	return text[start:end]
}

func logProbeDivergence(t *testing.T, kind string, divergence prefixProbeDivergence) {
	t.Helper()
	t.Logf("  %-11s %-10s -> %-10s prefix_intact=%-5v first_diff=msg#%d(%s) shared=%6d total=%6d invalidated=%6d tok raw=%5.1f%% @64=%5.1f%% @1024=%5.1f%% invalid_share=%5.1f%%",
		kind, divergence.prevLabel, divergence.curLabel, divergence.prefixIntact,
		divergence.firstDiffIdx, divergence.firstDiffRole,
		divergence.sharedTok, divergence.totalTok, divergence.invalidated,
		divergence.ratioRaw*100, divergence.ratio64*100, divergence.ratio1k*100, divergence.invalidShare*100)
}
