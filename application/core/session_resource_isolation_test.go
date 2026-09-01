package core

// 会话资源隔离单元测试（test-cases.md 第 7 节：不变量 Ⅰ–Ⅲ，fake 端口）。
// 目标：后台会话收尾/视图切换不得读全局活跃槽；A/B 域不相交。

import (
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
)

// TestSessionDomainsDisjoint（TC-INV-01）：A 与 B 的 M/X/R 状态域无共享，
// TranscriptFor(A) 读不到 B 任何内容。
func TestSessionDomainsDisjoint(t *testing.T) {
	service := newTestService(t, &fakeEngine{sessionID: "session-a"})
	defer service.Shutdown()

	service.Mu.Lock()
	service.Core.Snapshot.Session = SessionState{ID: "session-a"}
	service.components.tasks.BeginTaskFor("session-a", "req-a", "first A", "high", nil, TaskCheckpoint{})
	service.components.tasks.BeginTaskFor("session-b", "req-b", "hello B", "high", nil, TaskCheckpoint{})
	service.components.tasks.AppendTranscriptEventForLocked("session-a", TranscriptEvent{Role: "user", Content: "long task A"})
	service.components.tasks.AppendTranscriptEventForLocked("session-b", TranscriptEvent{Role: "user", Content: "hello B"})
	service.components.tasks.AppendTranscriptEventForLocked("session-b", TranscriptEvent{Role: "assistant", Content: "reply B"})
	service.Mu.Unlock()

	service.Mu.RLock()
	aTranscript := service.components.tasks.TranscriptFor("session-a")
	bTranscript := service.components.tasks.TranscriptFor("session-b")
	service.Mu.RUnlock()
	if len(aTranscript) != 1 || aTranscript[0].Content != "long task A" {
		t.Fatalf("A transcript = %#v, want only long task A", aTranscript)
	}
	if len(bTranscript) != 2 {
		t.Fatalf("B transcript = %#v, want hello B + reply B", bTranscript)
	}
	for _, event := range aTranscript {
		if strings.Contains(event.Content, "B") && event.Content != "long task A" {
			t.Fatalf("A domain leaks B content: %#v", event)
		}
	}
}

// TestViewSwitchDoesNotMutateExecution（TC-INV-02）：切到 B 只换视图指针，
// A 的聊天运行态与 transcript 逐字节不变（不变量 Ⅱ）。
func TestViewSwitchDoesNotMutateExecution(t *testing.T) {
	sessions := &archiveSessions{
		record: SessionRecord{
			Version: session_runtime.SessionRecordVersion,
			ID:      "session-b",
			Title:   SessionTitle{Value: "session B", Source: "user"},
			Conversation: ConversationRecord{Messages: []Message{
				{ID: "b-1", Role: "user", Content: "hello B"},
			}},
		},
		transcript: []TranscriptEvent{
			{Seq: 1, TaskID: "req-b", Role: "user", Content: "hello B", TokenCount: 2},
		},
	}
	service := newTestService(t, &fakeEngine{sessionID: "session-a"}, withTestSessions(sessions))
	defer service.Shutdown()

	service.Mu.Lock()
	service.Core.Snapshot.Session = SessionState{ID: "session-a"}
	service.components.tasks.BeginTaskFor("session-a", "req-a", "first A", "high", nil, TaskCheckpoint{})
	service.components.tasks.AppendTranscriptEventForLocked("session-a", TranscriptEvent{Role: "user", Content: "long task A"})
	aRuntime := service.sessionUnitLocked("session-a")
	aRuntime.SetChatState(ChatState{Running: true, RequestID: "req-a", StartedAt: time.Now()}, nil)
	service.Mu.Unlock()

	if err := service.ResumeSession("session-b"); err != nil {
		t.Fatal(err)
	}

	service.Mu.RLock()
	defer service.Mu.RUnlock()
	unitA := service.sessions.Unit("session-a")
	if unitA == nil {
		t.Fatal("A unit missing after view switch")
	}
	chatA := unitA.ChatState()
	if !chatA.Running || chatA.RequestID != "req-a" {
		t.Fatalf("A execution state mutated by view switch: %#v", chatA)
	}
	aTranscript := service.components.tasks.TranscriptFor("session-a")
	if len(aTranscript) != 1 || aTranscript[0].Content != "long task A" {
		t.Fatalf("A transcript mutated by view switch: %#v", aTranscript)
	}
}

// TestPersistReadsOnlyOwnDomain（TC-INV-03）：快照/活跃槽全是 B 时，
// PersistCurrentSession(A) 的 record 全域仍只属 A（不变量 Ⅲ）。
func TestPersistReadsOnlyOwnDomain(t *testing.T) {
	sessions := &archiveSessions{history: []EngineMessage{{Role: "system", Content: "system", ContentSet: true}}}
	service := newTestService(t, &fakeEngine{sessionID: "session-b"}, withTestSessions(sessions))
	defer service.Shutdown()

	service.Mu.Lock()
	// 全局快照归属会话 = B（模拟 A 后台完成时活跃会话是 B）。
	service.Core.Snapshot.Session = SessionState{ID: "session-b", Name: "B title"}
	service.Core.Snapshot.Conversation = []Message{{ID: "b-1", Role: "user", Content: "hello B"}}
	// A 的会话域。
	service.components.sessions.SetSessionTitleLocked("session-a", SessionTitle{Value: "first A", Source: "first_request", FinalizedAt: time.Now()})
	service.components.tasks.BeginTaskFor("session-a", "req-a", "first A", "high", nil, TaskCheckpoint{})
	service.components.tasks.AppendTranscriptEventForLocked("session-a", TranscriptEvent{Role: "user", Content: "long task A"})
	service.components.tasks.AppendTranscriptEventForLocked("session-a", TranscriptEvent{Role: "assistant", Content: "DONE_FROM_A"})
	// B 的会话域。
	service.components.sessions.SetSessionTitleLocked("session-b", SessionTitle{Value: "hello B", Source: "first_request", FinalizedAt: time.Now()})
	service.components.tasks.BeginTaskFor("session-b", "req-b", "hello B", "high", nil, TaskCheckpoint{})
	service.components.tasks.AppendTranscriptEventForLocked("session-b", TranscriptEvent{Role: "user", Content: "hello B"})
	service.Mu.Unlock()

	location := session_runtime.Location{WorkspaceID: "ws-x", Meta: SessionInfo{ID: "session-a"}}
	if err := service.components.sessions.PersistCurrentSession(location, "session-a"); err != nil {
		t.Fatal(err)
	}
	record := sessions.record
	if record.Title.Value != "first A" {
		t.Fatalf("record title = %q, want first A", record.Title.Value)
	}
	texts := make([]string, 0, len(record.Conversation.Messages))
	for _, message := range record.Conversation.Messages {
		texts = append(texts, message.Role+":"+message.Content)
	}
	joined := strings.Join(texts, " ")
	if !strings.Contains(joined, "long task A") || !strings.Contains(joined, "DONE_FROM_A") {
		t.Fatalf("A record lost own in-flight messages: %v", texts)
	}
	if strings.Contains(joined, "hello B") {
		t.Fatalf("A record polluted with B conversation: %v", texts)
	}
}
