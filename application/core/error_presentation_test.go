package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"testing"
	"time"
)

type failingSnapshotSessions struct{ fakeSessions }

func (failingSnapshotSessions) SaveSessionSnapshot(string, []EngineMessage, SessionRecord, []TranscriptEvent, []StoredToolResult) error {
	return errors.New("storage unavailable")
}

func TestPresentUserErrorHidesProviderDetailsAndIdentifiesSource(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		module string
		method string
		forbid []string
	}{
		{
			name:   "context exhaustion",
			err:    errors.New(`engine loop 15: ChatClient stream: HTTP 400: invalid params, context window exceeds limit (2013), request_id=req-secret`),
			module: "上下文恢复", method: "recoverProviderFailure",
			forbid: []string{"HTTP", "2013", "req-secret", "400"},
		},
		{
			name:   "empty provider content",
			err:    errors.New(`engine loop 0: ChatClient stream: HTTP 400: invalid params, chat content is empty (2013), request_id=req-secret`),
			module: "会话安全", method: "prepareProviderHistory",
			forbid: []string{"HTTP", "2013", "req-secret", "400"},
		},
		{
			name:   "provider timeout",
			err:    errors.New(`engine loop 16: ChatClient stream: HTTP 504: timeout_error, request_id=req-secret`),
			module: "模型传输", method: "ChatStream",
			forbid: []string{"HTTP", "504", "req-secret", "timeout_error"},
		},
		{
			name:   "provider server error",
			err:    errors.New(`ChatClient stream: HTTP 500: server_error, request_id=req-secret`),
			module: "模型传输", method: "ChatStream",
			forbid: []string{"HTTP", "500", "req-secret", "server_error"},
		},
		{
			name:   "invalid plan",
			err:    errors.New(`plan preflight: no valid plan_load: normalize DAG input: edges is required`),
			module: "计划预检", method: "PreparePlan",
			forbid: []string{"edges is required", "normalize DAG"},
		},
		{
			name:   "unknown failure",
			err:    errors.New(`unrecognized internal failure request_id=req-secret`),
			module: "代理运行时", method: "runChat",
			forbid: []string{"req-secret", "unrecognized internal"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := presentUserError(test.err)
			for _, expected := range []string{"模块：" + test.module, "方法：" + test.method} {
				if !strings.Contains(got, expected) {
					t.Fatalf("presentation %q does not contain %q", got, expected)
				}
			}
			for _, forbidden := range test.forbid {
				if strings.Contains(got, forbidden) {
					t.Fatalf("presentation leaks %q: %q", forbidden, got)
				}
			}
		})
	}
}

func TestRunChatAndToolProjectionUsePresentedErrors(t *testing.T) {
	engine := &fakeEngine{chatErr: errors.New(`ChatClient stream: HTTP 500: server_error request_id=req-secret`)}
	service := newTestService(engine)
	defer service.Shutdown()
	subscription := service.events.Subscribe(16)
	defer subscription.Close()

	if err := service.Submit(t.Context(), "检查项目"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	snapshot := service.Snapshot()
	if strings.Contains(snapshot.Chat.Error, "HTTP") || strings.Contains(snapshot.Chat.Error, "req-secret") {
		t.Fatalf("chat error leaks raw provider details: %q", snapshot.Chat.Error)
	}
	if !strings.Contains(snapshot.Chat.Error, "模块：模型传输") || !strings.Contains(snapshot.Chat.Error, "方法：ChatStream") {
		t.Fatalf("chat error = %q", snapshot.Chat.Error)
	}
	if !conversationContains(snapshot.Conversation, "error", "模块：模型传输") {
		t.Fatalf("conversation did not contain presented error: %#v", snapshot.Conversation)
	}

	var eventMessage string
	deadline := time.After(time.Second)
	for eventMessage == "" {
		select {
		case event := <-subscription.Events:
			if event.Kind != EventError {
				continue
			}
			var payload map[string]string
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			eventMessage = payload["message"]
		case <-deadline:
			t.Fatal("did not receive EventError")
		}
	}
	if strings.Contains(eventMessage, "HTTP") || strings.Contains(eventMessage, "req-secret") ||
		!strings.Contains(eventMessage, "模块：模型传输") {
		t.Fatalf("event error leaks or lacks source: %q", eventMessage)
	}

	service.handleToolStart("plan_load", "tool-plan", `{}`)
	service.handleToolComplete("plan_load", "tool-plan", "", errors.New(`plan_load: normalize DAG input: edges is required`), 0)
	tool := service.Snapshot().Conversation[len(service.Snapshot().Conversation)-2].Tool
	if tool == nil || strings.Contains(tool.Error, "edges is required") || !strings.Contains(tool.Error, "模块：计划预检") {
		t.Fatalf("tool error = %#v", tool)
	}
}

func TestRunChatLogsRawUnclassifiedError(t *testing.T) {
	engine := &fakeEngine{chatErr: errors.New("unrecognized internal failure request_id=req-diagnostic")}
	service := newTestService(engine)
	defer service.Shutdown()

	var logs bytes.Buffer
	previousOutput, previousFlags, previousPrefix := log.Writer(), log.Flags(), log.Prefix()
	log.SetOutput(&logs)
	log.SetFlags(0)
	log.SetPrefix("")
	defer func() {
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	}()

	if err := service.Submit(t.Context(), "inspect project"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	if got := logs.String(); !strings.Contains(got, "request_id=chat-") || !strings.Contains(got, "request_id=req-diagnostic") {
		t.Fatalf("diagnostic log = %q", got)
	}
}

func TestPersistenceFailureDoesNotClaimProgressWasSaved(t *testing.T) {
	service := newTestService(&fakeEngine{})
	defer service.Shutdown()
	service.deps.Sessions = failingSnapshotSessions{}

	if err := service.Submit(t.Context(), "inspect project"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	got := service.Snapshot().Chat.Error
	want := presentUserError(errors.New("persistence failed and recovery is not guaranteed"))
	if got != want || !strings.Contains(got, "persistCurrentSession") {
		t.Fatalf("persistence error = %q, want %q", got, want)
	}
}

func conversationContains(conversation []Message, role, fragment string) bool {
	for _, message := range conversation {
		if message.Role == role && strings.Contains(message.Content, fragment) {
			return true
		}
	}
	return false
}
