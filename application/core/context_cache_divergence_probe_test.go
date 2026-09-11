package core

// 跨轮前缀发散探针（本地可复现，不发网络请求，不依赖真实 provider）。
//
// 目的：沿**生产装配路径**（task_context transcript → context_runtime
// PrepareExecutionContextFor → replaceEngineHistory → PrepareProviderHistoryFor）
// 驱动「第 1 轮含工具调用、第 2 轮继续」的两次会话回合，逐次 LLM 调用把
// 「实际会发给 provider 的请求」固化为字节流，度量相邻请求的字节 LCP、
// 首个差异消息（index/role）与失效后缀的 token 占比——这正是 provider 前缀
// 缓存命中率的上限来源。
//
// 与既有 context_cache_smoke_test.go 的差别：冒烟用例用 appendSettledRound
// 手工写入「已定稿轮次」（理想 append-only），本探针复刻**真实回合边界**：
//
//  1. Seele ReAct 循环 per-iteration 追加 assistant（tool_calls 时 Content=nil，
//     见 Seele@v0.1.3/session/loop.go callLLM 的 `msg := types.Message{Role:
//     "assistant", Content: nil, ToolCalls: toolCalls}`）与 tool 消息；
//  2. OnLLMComplete 钩子（ToolHookBridge.Hooks）落 transcript assistant 事件，
//     OnToolStart 钩子边界把本轮迭代的说明正文按迭代归位到该事件
//     （AttributeToolNarrationLocked）；
//  3. 回合结束的生产收尾两连：EnsureFinalAssistantTranscript →
//     BackfillAssistantReasoning（说明正文的归位已前移，回合收尾不再事后填充）；
//  4. 下一轮 PrepareExecutionContextFor 从 transcript 重建 + 空正文修复。
//
// 请求序列化沿用冒烟用例的 serializeRequest（system 在头部、工具目录在尾部，
// 两者跨轮常量），token 估算沿用生产同款 seelexctx/tokens。
//
// 运行：
//
//	go test ./application/core -run ContextCacheDivergenceProbe -v -count=1

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/RedHuang-0622/Seele/seelectx"
	seelesession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/chat"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/memory"
	"github.com/RedHuang-0622/seelex/seelexctx/tokens"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

const prefixProbeRequestID = "prefix-probe-1"

// probeInputTrailerLen 是 serializeRequest 尾部「\n||\n + INPUT 空串」的长度
// （冒烟用例把当前输入与工具目录放在历史之后；LCP 度量只取历史字节流）。
var probeInputTrailerLen = len("\n||\n") + len(serText("INPUT", ""))

// prefixProbeConfig 是一次探针运行的装配变体（用于归因失效来源）。
type prefixProbeConfig struct {
	name string
	// engineKeepsToolContent = 引擎 assistant 工具调用消息是否保留正文。
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
	messages []EngineMessage
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
	service *Service
	engine  *fakeEngine
	runtime *fakeRuntime
	session string
	tools   []Tool
	history []EngineMessage
	calls   []prefixProbeCall
	// lastSystem 是上一次 capture 时的 system prompt（用于检测回合内 system 漂移）。
	lastSystem string
}

func newPrefixProbeHarness(t *testing.T, window int) *prefixProbeHarness {
	t.Helper()
	engine := &fakeEngine{}
	runtime := &fakeRuntime{}
	service := newTestService(t, engine, withTestRuntime(runtimeWithContextLimits{
		fakeRuntime: runtime, window: window, output: 8192,
	}))
	sessionID := engine.SessionID()
	service.ViewMu.Lock()
	service.Core.Snapshot.Session.ID = sessionID
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: prefixProbeRequestID}
	service.components.tasks.BeginTask(prefixProbeRequestID, "cross-turn prefix divergence probe", "high", nil, TaskCheckpoint{})
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
func (h *prefixProbeHarness) beginTurn(input string) {
	h.t.Helper()
	h.service.ViewMu.Lock()
	h.service.appendMessageLocked("user", input, nil)
	h.service.appendMessageLocked("assistant", "", nil)
	h.service.components.tasks.AppendTranscriptEventLocked(TranscriptEvent{
		TaskID: prefixProbeRequestID, Role: "user", Content: input,
	})
	h.service.ViewMu.Unlock()
	currentInput, err := h.service.components.context.PrepareExecutionContextFor(h.session, prefixProbeRequestID, input)
	if err != nil {
		h.t.Fatalf("prepare execution context: %v", err)
	}
	h.history = append([]EngineMessage(nil), h.engine.History()...)
	// 当前输入（可能含工作打点表块）由循环追加为工作历史最后一条 user 消息。
	h.history = append(h.history, EngineMessage{Role: "user", Content: currentInput, ContentSet: true})
}

// streamText 走生产流式链路写可见正文（跨 ReAct 迭代追加到同一条 assistant
// 消息，即 appendVisibleDelta 语义）。
func (h *prefixProbeHarness) streamText(text string) {
	if text == "" {
		return
	}
	for _, chunk := range splitProbeChunks(text, 32) {
		h.service.appendDelta(prefixProbeRequestID, chunk)
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
	h.engine.AppendHistory(types.Message{Role: "assistant", Content: content, ReasoningContent: reasoning, ToolCalls: calls})
	h.history = append(h.history, EngineMessage{
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
		TranscriptToolCall{ID: "tool-" + callID, Name: name, Arguments: arguments},
		h.service.streamedAssistantTextLocked(h.session))
	h.service.appendMessageLocked("tool", "", &ToolCall{ID: callID, Name: name, Arguments: arguments, Status: "running"})
	providerResult, _ := h.service.components.tasks.RecordToolTranscriptLocked(h.session, name, callID, arguments, result, nil)
	h.service.components.tasks.ObserveTool(task_context.ToolObservation{RequestID: prefixProbeRequestID, Name: name, Result: providerResult})
	h.service.ViewMu.Unlock()
	h.engine.AppendHistory(types.Message{Role: "tool", ToolCallID: callID, Name: name, Content: &result})
	h.history = append(h.history, EngineMessage{Role: "tool", ToolCallID: callID, Name: name, Content: result, ContentSet: true})
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
		total: total, bytes: serialized, messages: append([]EngineMessage(nil), h.history...),
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
	conversation := h.service.Snapshot().Conversation
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

func prefixProbeFirstDiff(prev, cur []EngineMessage) (int, string) {
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

func probeMessagesEqual(left, right EngineMessage) bool {
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

func probeEngineCalls(calls []types.ToolCall) []EngineToolCall {
	out := make([]EngineToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, EngineToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments})
	}
	return out
}

func probeCallIDs(calls []EngineToolCall) string {
	ids := make([]string, 0, len(calls))
	for _, call := range calls {
		ids = append(ids, call.ID)
	}
	return strings.Join(ids, ",")
}

// probeStreamedContent 返回生产路径下引擎 assistant 工具调用消息的流式正文
// （tool_calls 时框架置 nil，即空串）。
func probeStreamedContent(cfg prefixProbeConfig) string {
	if cfg.engineKeepsToolContent {
		return "narration (control: engine keeps content)"
	}
	return ""
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

// ── H1/H2/H3：两回合（第 1 轮含工具调用）前缀发散 ────────────────────────

// TestContextCacheDivergenceProbe_TwoTurnToolCliff 驱动 1 个含工具调用的回合
// + 1 个纯文本回合并度量：
//   - H1：回合内多 ReAct 迭代是否保持 append-only（prev 字节是 cur 的前缀）；
//   - H2：跨回合重建是否在「尾之前」发生字节改变（失效上一轮整段后缀）；
//   - H3：工作打点表块（请求尾部的动态块）改变是否只影响尾部。
func TestContextCacheDivergenceProbe_TwoTurnToolCliff(t *testing.T) {
	configs := []prefixProbeConfig{
		{name: "A-production(drop+attribute)", engineKeepsToolContent: false},
		{name: "C-control(keeps content)", engineKeepsToolContent: true},
	}
	for _, cfg := range configs {
		t.Run(cfg.name, func(t *testing.T) { runPrefixProbeTwoTurns(t, cfg) })
	}
}

func runPrefixProbeTwoTurns(t *testing.T, cfg prefixProbeConfig) {
	harness := newPrefixProbeHarness(t, 1_000_000)
	harness.startStream()

	turnOneInput := "第 1 轮：请核对 context_runtime 装配路径的前缀稳定性，并给出证据。"
	turnOneNarration := "我先读取装配入口与回合收尾代码，核对工具轮的前缀语义。"
	turnOneReply := "第 1 轮结论：装配把可复用段拼为保留前缀，工具轮正文需要单独核对。"
	toolResultOne := strings.Repeat("coordinator.go: PrepareExecutionContextFor 保留段重建路径证据行；", 120)
	toolResultTwo := strings.Repeat("chat.go: mergeStreamedToolNarration 视图正文合并证据行；", 80)

	t.Logf("== config %s ==", cfg.name)
	t.Logf("  constants excluded from byte LCP: system prompt %d tok, tools %d schemas",
		tokens.Count(systemPromptFor(harness.service)), len(harness.tools))
	harness.beginTurn(turnOneInput)
	harness.capture(1, 1, "t1.iter1")

	// 迭代 1：模型先输出说明文本（流式进视图），随后发起工具调用。
	harness.streamText(turnOneNarration)
	callOne := []types.ToolCall{{
		ID: "call-read-1", Type: "function",
		Function: types.ToolCallFunction{Name: "read_file", Arguments: `{"path":"application/core/context_runtime/coordinator.go"}`},
	}}
	engineContent := ""
	if cfg.engineKeepsToolContent {
		engineContent = turnOneNarration
	}
	harness.llmIteration(engineContent, "需要先看清装配顺序。", callOne)
	harness.toolRound("read_file", "call-read-1", `{"path":"application/core/context_runtime/coordinator.go"}`, toolResultOne)
	harness.capture(1, 2, "t1.iter2")

	// 迭代 2：第二个工具调用（该轮无说明文本）。
	callTwo := []types.ToolCall{{
		ID: "call-grep-2", Type: "function",
		Function: types.ToolCallFunction{Name: "grep_search", Arguments: `{"pattern":"mergeStreamedToolNarration"}`},
	}}
	harness.llmIteration("", "核对收尾三连。", callTwo)
	harness.toolRound("grep_search", "call-grep-2", `{"pattern":"mergeStreamedToolNarration"}`, toolResultTwo)
	harness.capture(1, 3, "t1.iter3")

	// 迭代 3：最终答复（同样流式进视图：视图正文 = 说明 + 答复的拼接）。
	harness.streamText(turnOneReply)
	harness.llmIteration(turnOneReply, "整理结论。", nil)
	harness.finishTurn(turnOneReply)

	// 回合间：工作打点表出现新条目（尾部动态块变化 → H3 观察点）。
	harness.runtime.tasks = map[string]dto.TaskRecord{
		"todo:1": {ID: "todo:1", Phase: dto.TaskPhaseTasklist, Task: "核对前缀失效区间", Status: dto.TaskRunning, Kind: "todo"},
	}

	// 第 2 轮：纯文本回合。
	harness.beginTurn("第 2 轮：继续，并说明上一轮工具结果的失效影响。")
	harness.capture(2, 1, "t2.iter1")
	// H2 直接证据：turn 2 装配出的「上一轮工具调用 assistant 消息」字节。
	for index, message := range harness.engine.History() {
		if message.Role == "assistant" && len(message.ToolCalls) > 0 {
			t.Logf("  H2 rebuilt msg#%d(assistant, tool_calls=%s) content=%q | streamed content=%q",
				index, probeCallIDs(message.ToolCalls), summarizeProbeText(message.Content), probeStreamedContent(cfg))
			break
		}
	}

	for index := 1; index < len(harness.calls); index++ {
		kind := "cross-turn"
		if harness.calls[index-1].turn == harness.calls[index].turn {
			kind = "within-turn"
		}
		logProbeDivergence(t, kind, prefixProbeCompare(harness.calls[index-1], harness.calls[index]))
	}

	// 跨回合：与「之前所有已发请求」的最长公共前缀（provider 可命中的上界）。
	reference := harness.calls[len(harness.calls)-2]
	best := prefixProbeCompare(reference, harness.calls[len(harness.calls)-1])
	for index := 0; index < len(harness.calls)-1; index++ {
		candidate := prefixProbeCompare(harness.calls[index], harness.calls[len(harness.calls)-1])
		if candidate.sharedTok > best.sharedTok {
			best = candidate
			reference = harness.calls[index]
		}
	}
	logProbeDivergence(t, "cross-max", best)

	// H3：尾部动态块（工作打点表）位置——块只在第 2 轮出现，位于请求尾部。
	last := harness.calls[len(harness.calls)-1]
	tailIndex := len(last.messages) - 1
	tailTokens := tokens.Count(last.messages[tailIndex].Content)
	t.Logf("  H3 tail: dynamic work-table block sits inside last message #%d(role=%s, %d chars / %d tok → %.2f%% of prompt)",
		tailIndex, last.messages[tailIndex].Role, len(last.messages[tailIndex].Content), tailTokens,
		100*float64(tailTokens)/float64(last.total))

	t.Logf("== summary %s: cross-turn reference=%s raw=%.1f%% @64=%.1f%% @1024=%.1f%% first_diff=msg#%d(%s) invalidated=%d tok (%.1f%% of %d) ==",
		cfg.name, reference.label, best.ratioRaw*100, best.ratio64*100, best.ratio1k*100,
		best.firstDiffIdx, best.firstDiffRole, best.invalidated, best.invalidShare*100, best.totalTok)
}

// ── H4：记忆块/稳定前缀栈块的位置与随查询变化 ────────────────────────────

// TestContextCacheDivergenceProbe_MemoryBlockPosition 用生产装配器
// （seelexctx.NewAssembler，与 seelebridge.seelexAssembler 同款选项接线）验证
// 「记忆块 + 稳定前缀栈块」投影顺序，并度量记忆块随当前查询变化时被失效的
// 字节/token 量。
func TestContextCacheDivergenceProbe_MemoryBlockPosition(t *testing.T) {
	harness := newPrefixProbeHarness(t, 1_000_000)
	candidates := []memory.Candidate{
		{SegmentID: "compact-s-1", From: 1, To: 6, Summary: "压缩段一：append-only prefix stable conclusion provider cache baseline。"},
		{SegmentID: "compact-s-2", From: 7, To: 14, Summary: "压缩段二：provider cache hit rate versus tool result size baseline。"},
		{SegmentID: "compact-s-3", From: 15, To: 22, Summary: "压缩段三：memory select lexical score with CJK bigram behavior。"},
		{SegmentID: "compact-s-4", From: 23, To: 30, Summary: "压缩段四：plan tail message and worktable trace block position。"},
	}
	options := memory.DefaultOptions()
	queryOne := "第 1 轮：provider cache hit rate versus tool result size。"
	queryTwo := "第 2 轮：memory select lexical score with CJK bigram and worktable position。"

	record := sessionstore.SessionContextRecord{
		SkillStack: []sessionstore.SkillFrame{{SkillID: "skill-review", Name: "review"}},
		CompactStack: []sessionstore.CompactFrame{{
			SegmentID: "compact-s-4", From: 23, To: 30, Summary: candidates[3].Summary,
		}},
	}
	assembler := seelexctx.NewAssembler(seelexctx.AssemblerOptions{
		SystemPrompt: func() string { return systemPromptFor(harness.service) },
		PrefixStacks: func() []seelectx.PromptBlock { return seelexctx.RenderStablePrefixBlocks(record) },
		Memories: func(_ context.Context, query string) []seelectx.PromptBlock {
			selected := memory.Select(query, candidates, options)
			if block := memory.RenderMemoryBlock(selected, options.MaxTokens); block != nil {
				return []seelectx.PromptBlock{*block}
			}
			return nil
		},
	})

	accumulated := probeAccumulatedHistory(40, 2400)
	requestOne := probeAssembleRequest(t, assembler, queryOne, accumulated)
	requestTwo := probeAssembleRequest(t, assembler, queryTwo, accumulated)

	selectedOne := memory.Select(queryOne, candidates, options)
	selectedTwo := memory.Select(queryTwo, candidates, options)
	blockOne := memory.RenderMemoryBlock(selectedOne, options.MaxTokens)
	blockTwo := memory.RenderMemoryBlock(selectedTwo, options.MaxTokens)
	t.Logf("== memory block (seelebridge/runtime_context.go relatedMemoryBlocks 同款接线) ==")
	t.Logf("  query1 selected=%v memory_block=%d tok", probeSegmentIDs(selectedOne), probeBlockTokens(blockOne))
	t.Logf("  query2 selected=%v memory_block=%d tok", probeSegmentIDs(selectedTwo), probeBlockTokens(blockTwo))
	t.Logf("  selection_changed=%v block_changed=%v", !probeSameSegments(selectedOne, selectedTwo), probeBlockText(blockOne) != probeBlockText(blockTwo))

	bytesOne, bytesTwo := probeSerializeAssembly(requestOne), probeSerializeAssembly(requestTwo)
	shared := lcpBytes(bytesOne, bytesTwo)
	t.Logf("  assembled order: memory block at msg#%d(%s), skill/compact prefix block at msg#%d(%s), accumulated context follows",
		probeMessageIndex(requestOne, "## 相关记忆"), probeRole(requestOne, "## 相关记忆"),
		probeMessageIndex(requestOne, "## 当前技能"), probeRole(requestOne, "## 当前技能"))
	if shared < len(bytesTwo) {
		t.Logf("  divergence starts inside msg#%d(%s) — memory block sits BEFORE the accumulated context, so a selection change invalidates everything after it",
			probeMessageIndexByBytes(requestOne, shared), probeRoleByBytes(requestOne, shared))
	}
	sharedTokens := tokens.Count(bytesTwo[:shared])
	totalTokens := tokens.Count(bytesTwo)
	t.Logf("  memory-block-change cost: lcp=%d/%d bytes (%.1f%% into prompt) shared=%d tok total=%d tok invalidated=%d tok (%.1f%%)",
		shared, len(bytesTwo), 100*float64(shared)/float64(len(bytesTwo)),
		sharedTokens, totalTokens, totalTokens-sharedTokens, 100*float64(totalTokens-sharedTokens)/float64(totalTokens))
}

func probeAccumulatedHistory(messages, charsPerMessage int) []types.Message {
	history := make([]types.Message, 0, messages+1)
	for index := 0; index < messages; index++ {
		content := fmt.Sprintf("第 %d 段已定稿上下文：%s", index, strings.Repeat("前缀保持稳定；", charsPerMessage/8+1))
		history = append(history, types.Message{Role: "assistant", Content: &content})
	}
	return history
}

func probeAssembleRequest(t *testing.T, assembler seelectx.RequestAssembler, query string, accumulated []types.Message) seelectx.AssembledRequest {
	t.Helper()
	working := append([]types.Message(nil), accumulated...)
	working = append(working, types.Message{Role: "user", Content: &query})
	assembled, err := assembler.Assemble(context.Background(), seelectx.AssemblyRequest{WorkingHistory: working})
	if err != nil {
		t.Fatalf("assemble: %v", err)
	}
	return assembled
}

func probeSerializeAssembly(request seelectx.AssembledRequest) string {
	var builder strings.Builder
	for _, message := range request.Messages {
		builder.WriteString(serText("ROLE", message.Role))
		content := ""
		if message.Content != nil {
			content = *message.Content
		}
		builder.WriteString(serText("CONTENT", content))
	}
	return builder.String()
}

func probeMessageIndex(request seelectx.AssembledRequest, marker string) int {
	for index, message := range request.Messages {
		if message.Content != nil && strings.Contains(*message.Content, marker) {
			return index
		}
	}
	return -1
}

func probeRole(request seelectx.AssembledRequest, marker string) string {
	index := probeMessageIndex(request, marker)
	if index < 0 {
		return "absent"
	}
	return request.Messages[index].Role
}

func probeMessageIndexByBytes(request seelectx.AssembledRequest, offset int) int {
	position := 0
	for index, message := range request.Messages {
		content := ""
		if message.Content != nil {
			content = *message.Content
		}
		position += len(serText("ROLE", message.Role)) + len(serText("CONTENT", content))
		if offset < position {
			return index
		}
	}
	return len(request.Messages) - 1
}

func probeRoleByBytes(request seelectx.AssembledRequest, offset int) string {
	index := probeMessageIndexByBytes(request, offset)
	if index < 0 || index >= len(request.Messages) {
		return "absent"
	}
	return request.Messages[index].Role
}

func probeBlockTokens(block *seelectx.PromptBlock) int {
	if block == nil {
		return 0
	}
	return tokens.Count(probeBlockText(block))
}

func probeBlockText(block *seelectx.PromptBlock) string {
	if block == nil {
		return ""
	}
	var builder strings.Builder
	for _, message := range block.Messages {
		if message.Content != nil {
			builder.WriteString(*message.Content)
		}
	}
	return builder.String()
}

func probeSegmentIDs(selected []memory.Candidate) []string {
	ids := make([]string, 0, len(selected))
	for _, candidate := range selected {
		ids = append(ids, candidate.SegmentID)
	}
	return ids
}

func probeSameSegments(left, right []memory.Candidate) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].SegmentID != right[index].SegmentID {
			return false
		}
	}
	return true
}
