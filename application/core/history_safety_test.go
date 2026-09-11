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

func TestEmptyProviderContentLeavesNextTurnWithRecoverableHistory(t *testing.T) {
	engine := &fakeEngine{
		appendChatHistory: true,
		chatErr:           errors.New("engine loop 0: ChatClient stream: invalid params, chat content is empty (2013)"),
	}
	service := newTestService(t, engine)
	defer service.Shutdown()
	if err := service.Submit(context.Background(), "continue the audit"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)

	history := engine.History()
	recoveryFound := false
	for _, message := range history {
		if strings.HasPrefix(message.Content, providerRecoveryPrefix) {
			recoveryFound = true
			break
		}
	}
	if !recoveryFound {
		t.Fatalf("empty-content failure left no recovery history: %#v", history)
	}
	if state := service.Snapshot().Task; state == nil || state.Status != TaskInterrupted {
		t.Fatalf("task state = %#v, want interrupted", state)
	}
	if visible := service.Snapshot().Chat.Error; !strings.Contains(visible, "模块：会话安全") || strings.Contains(visible, "2013") {
		t.Fatalf("visible error = %q", visible)
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
