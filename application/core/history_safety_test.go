package core

import (
	"context"
	"errors"
	"github.com/RedHuang-0622/seelex/application/core/context_runtime"
	"github.com/RedHuang-0622/seelex/application/core/task_context"
	"strings"
	"testing"
)

func TestNonEmptyProviderInputExplainsEmptySubmission(t *testing.T) {
	if got := nonEmptyProviderInput("  \n"); got == "" {
		t.Fatal("empty input must be converted to a recovery explanation")
	}
	if got := nonEmptyProviderInput("hello"); got != "hello" {
		t.Fatalf("non-empty input changed to %q", got)
	}
}

func TestPrepareProviderHistoryRepairsBeforeChat(t *testing.T) {
	engine := &fakeEngine{history: []EngineMessage{{Role: "assistant", Content: ""}}}
	service := newTestService(t, engine)
	defer service.Shutdown()
	if err := service.components.history.PrepareProviderHistory(); err != nil {
		t.Fatal(err)
	}
	history := engine.History()
	if len(history) != 1 || history[0].Content != context_runtime.MissingHistoryContent || !history[0].ContentSet {
		t.Fatalf("prepared history = %+v", history)
	}
}

func TestRecoverProviderContextReplacesRejectedTranscriptWithPrivateCheckpoint(t *testing.T) {
	engine := &fakeEngine{history: []EngineMessage{
		{Role: "system", Content: "private system instruction", ContentSet: true},
		{Role: "user", Content: "original request", ContentSet: true},
		{Role: "assistant", ToolCalls: []EngineToolCall{{ID: "call-1", Name: "bash", Arguments: `{"command":"Get-ChildItem"}`}}},
		{Role: "tool", ToolCallID: "call-1", Name: "bash", Content: "raw command output that must not survive", ContentSet: true},
	}}
	service := newTestService(t, engine)
	defer service.Shutdown()
	service.ViewMu.Lock()
	service.components.tasks.BeginTask("task-1", "audit the repository", "high", nil, TaskCheckpoint{})
	service.components.tasks.CurrentTaskExecution().Checkpoint("inspect", "inspect source", "completed", "found the call path", "")
	service.ViewMu.Unlock()

	err := errors.New("engine loop 15: invalid params, context window exceeds limit (2013)")
	if err := service.recoverProviderContext(err, "audit the repository"); err != nil {
		t.Fatal(err)
	}
	history := engine.History()
	if len(history) != 3 || history[0].Role != "system" || history[1].Role != "system" || history[2].Role != "user" {
		t.Fatalf("recovered history = %#v", history)
	}
	if !strings.HasPrefix(history[1].Content, contextRecoveryPrefix) || !strings.Contains(history[1].Content, "node=inspect status=completed") {
		t.Fatalf("missing recovery checkpoint: %q", history[1].Content)
	}
	if strings.Contains(history[1].Content, "raw command output") || len(history[1].ToolCalls) != 0 {
		t.Fatalf("raw transcript survived recovery: %#v", history[1])
	}
	if err := service.removeProviderContextRecovery(); err != nil {
		t.Fatal(err)
	}
	history = engine.History()
	if len(history) != 2 || history[1].Content != "audit the repository" || strings.HasPrefix(history[1].Content, contextRecoveryPrefix) {
		t.Fatalf("recovery was not restored before persistence: %#v", history)
	}
}

func TestRecoverProviderContextIgnoresOtherProviderErrors(t *testing.T) {
	engine := &fakeEngine{history: []EngineMessage{{Role: "user", Content: "keep me", ContentSet: true}}}
	service := newTestService(t, engine)
	defer service.Shutdown()
	if err := service.recoverProviderContext(errors.New("HTTP 504 upstream timeout"), "new request"); err != nil {
		t.Fatal(err)
	}
	if history := engine.History(); len(history) != 1 || history[0].Content != "keep me" {
		t.Fatalf("non-context error rewrote history: %#v", history)
	}
}

func TestRecoverProviderTimeoutCreatesPrivateResumeCheckpoint(t *testing.T) {
	engine := &fakeEngine{history: []EngineMessage{
		{Role: "system", Content: "private system instruction", ContentSet: true},
		{Role: "user", Content: "audit source", ContentSet: true},
		{Role: "tool", Name: "bash", Content: "raw output that must not survive", ContentSet: true},
	}}
	service := newTestService(t, engine)
	defer service.Shutdown()
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.components.tasks.BeginTask("task-1", "audit source", "high", nil, TaskCheckpoint{})
	service.components.tasks.CurrentTaskExecution().Checkpoint("inspect", "source", "completed", "found a call path", "")
	service.ViewMu.Unlock()

	recovered, err := service.recoverProviderFailure(errors.New("engine loop 16: ChatClient stream: HTTP 504: timeout_error"), "audit source")
	if err != nil || !recovered {
		t.Fatalf("recover timeout = %v, %v", recovered, err)
	}
	history := engine.History()
	if len(history) != 3 || history[1].Role != "system" || !strings.HasPrefix(history[1].Content, providerRecoveryPrefix) || history[2].Role != "user" {
		t.Fatalf("timeout recovery history = %#v", history)
	}
	if strings.Contains(history[1].Content, "raw output that must not survive") || !strings.Contains(history[1].Content, "node=inspect status=completed") {
		t.Fatalf("timeout recovery checkpoint = %q", history[1].Content)
	}
	if state := service.Snapshot().Task; state == nil || state.Status != TaskInterrupted {
		t.Fatalf("task state = %#v, want interrupted", state)
	}
}

func TestContextExhaustionPersistsInterruptedProjectionAfterBoundedRetryFails(t *testing.T) {
	engine := &fakeEngine{
		appendChatHistory: true,
		chatErr:           errors.New("engine loop 15: context window exceeds limit"),
	}
	service := newTestService(t, engine)
	defer service.Shutdown()
	if err := service.Submit(context.Background(), "finish the repository audit"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)

	if service.Snapshot().Chat.Error == "" {
		t.Fatal("provider failure must remain visible for the failed turn")
	}
	if task := service.Snapshot().Task; task == nil || task.Status != TaskInterrupted {
		t.Fatalf("task state = %#v, want interrupted", task)
	}
	service.ViewMu.RLock()
	projection := service.components.tasks.TaskProjectionLocked(service.Core.Snapshot.Session.ID)
	service.ViewMu.RUnlock()
	if projection == nil || projection.Status != task_context.StatusInterrupted || projection.Checkpoint.CoversEventRange.End == 0 {
		t.Fatalf("projection = %#v", projection)
	}
}

func containsRecoveryHistory(history []EngineMessage, prefix string) bool {
	for _, message := range history {
		if strings.HasPrefix(message.Content, prefix) {
			return true
		}
	}
	return false
}

func TestContextExhaustionReturnsBoundedRecoveryInstructionToAgent(t *testing.T) {
	engine := &fakeEngine{
		appendChatHistory: true,
		chatErrors:        []error{errors.New("engine loop 15: context window exceeds limit"), nil},
	}
	service := newTestService(t, engine)
	defer service.Shutdown()
	if err := service.Submit(context.Background(), "finish the repository audit"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)

	engine.mu.Lock()
	inputs := append([]string(nil), engine.chatInputs...)
	engine.mu.Unlock()
	if len(inputs) != 2 || inputs[1] != contextRecoveryAgentInput {
		t.Fatalf("recovery inputs = %#v", inputs)
	}
	if got := service.Snapshot().Chat.Error; got != "" {
		t.Fatalf("successful bounded recovery left an error: %q", got)
	}
}

// TestEmptyProviderContentRejectionResumesFromBoundedCheckpoint 覆盖“记录不合法”
// 的另一半措辞（invalid params, chat content is empty）：它与 tool 配对 400 同类
// （classifyProviderFailure 都判 providerFailureHistory），恢复之后同样重放一次
// 有界恢复回合，而不是把回合判死。被拒的记录不进入重放的请求；恢复说明只走私有
// system 区，回合成功后由 removeProviderContextRecovery 摘掉。
func TestEmptyProviderContentRejectionResumesFromBoundedCheckpoint(t *testing.T) {
	engine := &fakeEngine{
		appendChatHistory: true,
		chatErrors:        []error{errors.New("engine loop 0: ChatClient stream: invalid params, chat content is empty (2013)")},
	}
	service := newTestService(t, engine)
	defer service.Shutdown()
	if err := service.Submit(context.Background(), "continue the audit"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)

	engine.mu.Lock()
	inputs := append([]string(nil), engine.chatInputs...)
	resent := append([]EngineMessage(nil), engine.historyBeforeChat...)
	engine.mu.Unlock()
	if len(inputs) != 2 {
		t.Fatalf("provider calls = %d, want 2 (original + bounded recovery turn)", len(inputs))
	}
	if !strings.Contains(inputs[1], "seelex:context-recovery-agent:v1") {
		t.Fatalf("second request is not the bounded recovery turn: %q", inputs[1])
	}
	for _, message := range resent {
		if strings.HasPrefix(message.Content, providerRecoveryPrefix) {
			t.Fatalf("replayed request still carries the rejected transcript: %#v", resent)
		}
	}
	if visible := service.Snapshot().Chat.Error; visible != "" {
		t.Fatalf("recovered empty-content rejection surfaced as a dead turn: %q", visible)
	}
}

func TestNonRecoverableProviderFailureMarksTaskFailed(t *testing.T) {
	engine := &fakeEngine{chatErr: errors.New("HTTP 401 provider rejected request")}
	service := newTestService(t, engine)
	defer service.Shutdown()
	if err := service.Submit(context.Background(), "inspect repository"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	if task := service.Snapshot().Task; task == nil || task.Status != TaskFailed {
		t.Fatalf("task state = %#v, want failed", task)
	}
}

func TestIterationRepairsNewlyAddedEmptyToolHistory(t *testing.T) {
	engine := &fakeEngine{history: []EngineMessage{{
		Role: "assistant", ToolCalls: []EngineToolCall{{ID: "call-1", Name: "plan_load", Arguments: `{}`}},
	}}}
	service := newTestService(t, engine)
	defer service.Shutdown()
	service.ViewMu.Lock()
	service.Core.Snapshot.Chat = ChatState{Running: true, RequestID: "task-1"}
	service.components.tasks.BeginTask("task-1", "load a plan", "high", nil, TaskCheckpoint{})
	service.ViewMu.Unlock()

	bridge := NewToolHookBridge()
	bridge.Bind(service)
	if !bridge.Hooks().OnIterationComplete(context.Background(), 0) {
		t.Fatal("iteration should remain available")
	}
	// 工具调用必须保留（配对修复的输入），正文保持为空：工具轮 assistant 正文
	// 在 wire 上恒为空（框架 tool_calls 分支 Content=nil），投影补占位会让下一轮
	// 重投影字节与已发出字节分叉（provider 前缀缓存自该消息起失效）。
	history := engine.History()
	if len(history) != 1 || len(history[0].ToolCalls) != 1 {
		t.Fatalf("tool round must be retained for pairing repair: %#v", history)
	}
	if history[0].Content != "" || history[0].ContentSet {
		t.Fatalf("tool-call assistant content = %q (set=%v), want empty: wire carries no text with tool calls",
			history[0].Content, history[0].ContentSet)
	}
}

func TestServerFailuresAreRecoverableWithoutAutomaticReplay(t *testing.T) {
	if got := classifyProviderFailure(errors.New("engine loop 16: HTTP 500: server_error")); got != providerFailureServer {
		t.Fatalf("provider failure = %q, want %q", got, providerFailureServer)
	}
}

// TestToolProtocolRejectionsAreHistoryFailures 覆盖 2026-09-20 现场那条 400：
// provider 拒绝"孤儿 tool 消息"（没有前一条 assistant 宣告）属于**无效历史**
// ——请求里的记录不合法，应当走有界检查点的历史恢复，而不是把会话循环判死
// （现场中断在 session loop 15 / 22）。wire 出口已按协议剔除这种形状
// （seelexctx 装配器的 tool 配对规整 + 本包投影修复），这里是措辞兜底。
func TestToolProtocolRejectionsAreHistoryFailures(t *testing.T) {
	err := errors.New(`engine loop 15: seelebridge: stream with account "agent-1": ` +
		`ChatClient stream: HTTP 400: {"error":{"message":"Messages with role 'tool' must be a ` +
		`response to a preceding message with 'tool_calls'","type":"invalid_request_error",` +
		`"param":null,"code":"invalid_request_error"}}`)
	if got := classifyProviderFailure(err); got != providerFailureHistory {
		t.Fatalf("provider failure = %q, want %q", got, providerFailureHistory)
	}
}

// TestInsufficientToolMessagesIsAHistoryFailure 覆盖 tool 配对协议的另一半措辞：
// assistant 宣告了 tool_calls，但紧跟其后（相邻区间内）的 tool 回执不足以回应
// 每个 tool_call_id——provider 报 "An assistant message with 'tool_calls' must be
// followed by tool messages responding to each 'tool_call_id'. (insufficient tool
// messages following tool_calls message)"。这与 TestToolProtocolRejectionsAreHistoryFailures
// 是同一类"请求里的记录不合法"，也必须走有界检查点的历史恢复，而不是把会话循环
// 判死（同上一条用例的现场形状：一轮在第 N 次 LLM 往返上被 400 打断）。
// 措辞实测来源：docs/2026-09-24-async-tool-deferred-ack/README.md §0 探针 P4。
func TestInsufficientToolMessagesIsAHistoryFailure(t *testing.T) {
	err := errors.New(`session loop 0: seelebridge: stream with account "agent-1": ` +
		`ChatClient stream: HTTP 400: {"error":{"message":"An assistant message with 'tool_calls' ` +
		`must be followed by tool messages responding to each 'tool_call_id'. ` +
		`(insufficient tool messages following tool_calls message)","type":"invalid_request_error",` +
		`"param":null,"code":"invalid_request_error"}}`)
	if got := classifyProviderFailure(err); got != providerFailureHistory {
		t.Fatalf("provider failure = %q, want %q", got, providerFailureHistory)
	}
}

// TestRetryableAfterRecoveryOnlyReplaysPreExecutionRejections 钉住"什么时候可以
// 重放恢复回合"：provider 在工具执行前拒绝（上下文耗尽 / 记录不合法）可安全重放；
// 超时与服务端故障副作用不确定，不得重放。
func TestRetryableAfterRecoveryOnlyReplaysPreExecutionRejections(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"context exhaustion", errors.New("engine loop 15: context window exceeds limit (2013)"), true},
		{"tool pairing 400 (live)", liveSessionLoopToolPairingFailure(), true},
		{"orphan tool result 400", errors.New(`HTTP 400: Messages with role 'tool' must be a response to a preceding message with 'tool_calls'`), true},
		{"upstream timeout", errors.New("engine loop 16: ChatClient stream: HTTP 504: timeout_error"), false},
		{"server unavailable", errors.New("engine loop 16: HTTP 503: server_error"), false},
		{"not a provider failure", nil, false},
	}
	for _, testCase := range cases {
		if got := retryableAfterRecovery(testCase.err); got != testCase.want {
			t.Fatalf("%s: retryableAfterRecovery = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

// liveSessionLoopToolPairingFailure 是 2026-09-28 现场原文：会话循环第一次请求
// 就带着一条没配齐回执的 assistant(tool_calls)，provider 直接 400，循环被判死。
func liveSessionLoopToolPairingFailure() error {
	return errors.New(`session loop 0: seelebridge: stream with account "goalplan-1": ` +
		`ChatClient stream: HTTP 400: {"error":{"message":"An assistant message with 'tool_calls' ` +
		`must be followed by tool messages responding to each 'tool_call_id'. ` +
		`(insufficient tool messages following tool_calls message)","type":"invalid_request_error",` +
		`"param":null,"code":"invalid_request_error"}}`)
}

// TestHistoryProtocolFailureResumesInsteadOfKillingTheSession 覆盖现场后果：恢复
// 做了，但没有重放恢复回合（重放条件只认上下文耗尽），于是第一次请求就 400 的
// 会话被判死——用户看到的是"会话中断"，而不是"Agent 从检查点继续"。
// 期望：恢复回合重放一次；被拒的记录不再出现在重放的请求里；会话照常结束。
func TestHistoryProtocolFailureResumesInsteadOfKillingTheSession(t *testing.T) {
	engine := &fakeEngine{
		appendChatHistory: true,
		chatErrors:        []error{liveSessionLoopToolPairingFailure()},
	}
	service := newTestService(t, engine)
	defer service.Shutdown()
	if err := service.Submit(context.Background(), "finish the repository audit"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)

	if visible := service.Snapshot().Chat.Error; visible != "" {
		t.Fatalf("history-protocol 400 surfaced as a dead turn: %q", visible)
	}
	engine.mu.Lock()
	inputs := append([]string(nil), engine.chatInputs...)
	resent := append([]EngineMessage(nil), engine.historyBeforeChat...)
	engine.mu.Unlock()
	if len(inputs) != 2 {
		t.Fatalf("provider calls = %d, want 2 (original + bounded recovery turn)", len(inputs))
	}
	if !strings.Contains(inputs[1], "seelex:context-recovery-agent:v1") {
		t.Fatalf("second request is not the bounded recovery turn: %q", inputs[1])
	}
	for _, message := range resent {
		if message.Role == "tool" || len(message.ToolCalls) > 0 {
			t.Fatalf("replayed request still carries the rejected tool record: %#v", resent)
		}
	}
}
