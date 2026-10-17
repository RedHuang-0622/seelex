package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"log"
	"strings"
	"testing"
	"time"
)

// failingSnapshotSessions 让快照持久化固定失败，用于断言"持久化失败不得声称进度已保存"。
type failingSnapshotSessions struct{ fakeSessions }

func (failingSnapshotSessions) SaveSessionSnapshot(string, []contract.EngineMessage, model.SessionRecord, []model.TranscriptEvent, []model.StoredToolResult) error {
	return errors.New("storage unavailable")
}

func (failingSnapshotSessions) SaveSessionSnapshotWorkspace(string, string, []contract.EngineMessage, model.SessionRecord, []model.TranscriptEvent, []model.StoredToolResult) error {
	return errors.New("storage unavailable")
}

func TestRunChatUsesRawErrorWhileToolProjectionUsesPresentedError(t *testing.T) {
	engine := &fakeEngine{chatErr: errors.New(`ChatClient stream: HTTP 500: server_error request_id=req-secret`)}
	service := newTestService(t, engine)
	defer service.Shutdown()
	subscription := service.Events.Subscribe(16)
	defer subscription.Close()

	if err := service.Submit(t.Context(), "检查项目"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	snapshot := service.Snapshot()
	// chat 报错 = 原始 err 正文（诊断优先）：provider 细节原样可见。
	if !strings.Contains(snapshot.Chat.Error, "HTTP 500") || !strings.Contains(snapshot.Chat.Error, "req-secret") {
		t.Fatalf("chat error should carry the raw error text: %q", snapshot.Chat.Error)
	}
	if strings.Contains(snapshot.Chat.Error, "模块：") {
		t.Fatalf("chat error must not be rewritten into presented text: %q", snapshot.Chat.Error)
	}
	if !conversationContains(snapshot.Conversation, "error", "server_error") {
		t.Fatalf("conversation did not contain the raw error: %#v", snapshot.Conversation)
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
	if !strings.Contains(eventMessage, "server_error") || strings.Contains(eventMessage, "模块：") {
		t.Fatalf("event error should carry the raw error text: %q", eventMessage)
	}

	service.handleToolStart(context.Background(), "read", "tool-read", `{}`)
	service.handleToolComplete("read", "tool-read", "", errors.New(`storage unavailable`), 0)
	tool := service.Snapshot().Conversation[len(service.Snapshot().Conversation)-2].Tool
	if tool == nil || strings.Contains(tool.Error, "storage unavailable") || !strings.Contains(tool.Error, "模块：") {
		t.Fatalf("tool error = %#v", tool)
	}
}

func TestRunChatLogsRawUnclassifiedError(t *testing.T) {
	engine := &fakeEngine{chatErr: errors.New("unrecognized internal failure request_id=req-diagnostic")}
	service := newTestService(t, engine)
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
	service := newTestService(t, &fakeEngine{}, withTestSessions(failingSnapshotSessions{}))

	if err := service.Submit(t.Context(), "inspect project"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	got := service.Snapshot().Chat.Error
	// chat 报错是原始正文：持久化失败必须自报风险，不得声称进度已保存。
	if !strings.Contains(got, "persistence failed and recovery is not guaranteed") {
		t.Fatalf("persistence error = %q", got)
	}
	for _, claim := range []string{"进度已保存", "已保留"} {
		if strings.Contains(got, claim) {
			t.Fatalf("persistence error claims progress was saved: %q", got)
		}
	}
}

func conversationContains(conversation []model.Message, role, fragment string) bool {
	for _, message := range conversation {
		if message.Role == role && strings.Contains(message.Content, fragment) {
			return true
		}
	}
	return false
}
