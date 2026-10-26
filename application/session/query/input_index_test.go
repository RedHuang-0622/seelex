package query

import (
	"errors"
	"sync"
	"testing"

	session_runtime "github.com/RedHuang-0622/seelex/application/session/runtime"
	sessionstate "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestInputIndexApplicationComposesFullConversationAndLoadedWindow(t *testing.T) {
	var viewMu sync.RWMutex
	messages := []model.Message{
		{ID: "m1", Role: "user", Content: "first"},
		{ID: "m2", Role: "assistant", Content: "answer"},
		{ID: "m3", Role: "user", Content: "second"},
	}
	application := NewInputIndex(InputIndexDeps{
		ViewMu: &viewMu,
		Locate: func(sessionID string) session_runtime.Location {
			if sessionID != "session-a" {
				t.Fatalf("workspace session = %q", sessionID)
			}
			return session_runtime.Location{WorkspaceID: "workspace-a"}
		},
		LoadRange: func(workspaceID, sessionID string) ([]model.Message, int, bool, error) {
			if workspaceID != "workspace-a" || sessionID != "session-a" {
				t.Fatalf("range args = %q/%q", workspaceID, sessionID)
			}
			return messages, len(messages), true, nil
		},
		ReadUnit: func(sessionID string, read func(*sessionstate.View)) {
			read(&sessionstate.View{
				Conversation:       messages[2:],
				HistoryOffset:      2,
				TotalMessages:      len(messages),
				ConversationWindow: 1,
			})
		},
		BuildItems: func(conversation []model.Message, loaded []string, limit int, userText func(model.Message) string) []model.SessionInputIndexRow {
			if len(loaded) != 1 || loaded[0] != "second" || limit != 7 || len(conversation) != len(messages) {
				t.Fatalf("build args loaded=%v limit=%d conversation=%d", loaded, limit, len(conversation))
			}
			return []model.SessionInputIndexRow{
				{Offset: 0, MessageID: "m1", Summary: userText(messages[0])},
				{Offset: 2, MessageID: "m3", Summary: userText(messages[2]), Loaded: true},
			}
		},
		SummaryLimit:  func() int { return 7 },
		HistoryWindow: func() int { return 20 },
		DurableCount:  func(items []model.Message) int { return len(items) },
		UserText:      func(message model.Message) string { return message.Content },
	})

	index, err := application.Read(" session-a ")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if index.SessionID != "session-a" || index.Total != 3 || index.InputCount != 2 || len(index.Items) != 2 {
		t.Fatalf("index = %+v", index)
	}
	if index.Window.Offset != 2 || index.Window.Count != 1 || index.Window.Total != 3 {
		t.Fatalf("window = %+v", index.Window)
	}
}

func TestInputIndexApplicationFallsBackToRecord(t *testing.T) {
	wantRange := errors.New("range unavailable")
	application := NewInputIndex(InputIndexDeps{
		ViewMu: &sync.RWMutex{},
		Locate: func(string) session_runtime.Location {
			return session_runtime.Location{WorkspaceID: "workspace-a"}
		},
		LoadRange: func(string, string) ([]model.Message, int, bool, error) {
			return nil, 0, true, wantRange
		},
		LoadRecord: func(_ session_runtime.Location, sessionID string) (model.SessionRecord, bool, error) {
			return model.SessionRecord{ID: sessionID}, true, nil
		},
		Record: func(model.SessionRecord) []model.Message {
			return []model.Message{{ID: "record-user", Role: "user", Content: "record content"}}
		},
		BuildItems: func(conversation []model.Message, _ []string, _ int, userText func(model.Message) string) []model.SessionInputIndexRow {
			return []model.SessionInputIndexRow{{MessageID: conversation[0].ID, Summary: userText(conversation[0])}}
		},
		SummaryLimit:  func() int { return 20 },
		HistoryWindow: func() int { return 10 },
		DurableCount:  func(items []model.Message) int { return len(items) },
		UserText:      func(message model.Message) string { return message.Content },
	})

	index, err := application.Read("session-a")
	if err != nil {
		t.Fatalf("Read fallback: %v", err)
	}
	if index.Total != 1 || len(index.Items) != 1 || index.Items[0].MessageID != "record-user" {
		t.Fatalf("fallback index = %+v", index)
	}
}

func TestInputIndexApplicationRequiresSessionID(t *testing.T) {
	application := NewInputIndex(InputIndexDeps{ViewMu: &sync.RWMutex{}})
	if _, err := application.Read("   "); err == nil {
		t.Fatal("empty session ID must fail")
	}
}
