package view_state

import (
	"encoding/json"
	"testing"

	session "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/state"
)

func TestFrontendSnapshotEmptyConversationMarshalsAsArray(t *testing.T) {
	units := session.NewDomain()
	defer units.Close()
	core := state.New(contract.Dependencies{})
	core.Snapshot = protocol.Snapshot{
		ProtocolVersion: protocol.ProtocolVersion,
		Session:         protocol.SessionState{ID: "draft", Draft: true},
	}
	coordinator := NewCoordinator(Deps{Core: core, Units: units})
	snapshot := NewSnapshotApplication(coordinator, SnapshotDeps{
		// Production annotation copies through append([]Message(nil), ...), which
		// collapses an already-empty slice back to nil. The frontend snapshot must
		// restore the required wire shape after every enrichment stage.
		Annotate: func(_ string, messages []protocol.Message, _ bool) []protocol.Message {
			return append([]protocol.Message(nil), messages...)
		},
	}).Snapshot()

	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal frontend snapshot: %v", err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("decode frontend snapshot: %v", err)
	}
	if got := string(wire["conversation"]); got != "[]" {
		t.Fatalf("empty conversation wire = %s, want [] (GUI snapshot contract requires an array)", got)
	}
}

func TestFrontendSnapshotProjectsDraftRowsAndHostEnrichment(t *testing.T) {
	units := session.NewDomain()
	defer units.Close()
	core := state.New(contract.Dependencies{})
	core.Snapshot = protocol.Snapshot{
		Revision: 7,
		Session:  protocol.SessionState{ID: "view"},
		Chat:     protocol.ChatState{Running: true},
		Sessions: []protocol.SessionInfo{{ID: "loaded", Name: "loaded"}},
		Conversation: []protocol.Message{{
			ID: "m1", Role: "user", Content: "raw",
		}},
	}
	coordinator := NewCoordinator(Deps{Core: core, Units: units})
	snapshot := NewSnapshotApplication(coordinator, SnapshotDeps{
		Status: func(string) protocol.SessionStatus { return protocol.SessionStatusRunning },
		DraftRow: func() *protocol.SessionInfo {
			return &protocol.SessionInfo{ID: "draft", Name: "draft", Status: protocol.SessionStatusDraft}
		},
		Annotate: func(_ string, messages []protocol.Message, running bool) []protocol.Message {
			if !running {
				t.Fatal("annotation did not receive running state")
			}
			messages[0].Content = "annotated"
			return messages
		},
		LatestTranscriptSeq: func(string) uint64 { return 11 },
		ProjectDraftWorkspace: func(snapshot *protocol.Snapshot) {
			snapshot.SessionWorkspaces = map[string]string{"draft": "ws"}
		},
		WithLiveRows: func(rows []protocol.SessionInfo) []protocol.SessionInfo {
			return append(rows, protocol.SessionInfo{ID: "live"})
		},
		EnrichRows: func(rows []protocol.SessionInfo) []protocol.SessionInfo {
			for index := range rows {
				rows[index].Name += ":enriched"
			}
			return rows
		},
	}).Snapshot()

	if snapshot.Revision != 7 || snapshot.Session.Status != protocol.SessionStatusRunning {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if len(snapshot.Conversation) != 1 || snapshot.Conversation[0].Content != "annotated" {
		t.Fatalf("conversation = %#v", snapshot.Conversation)
	}
	if snapshot.ConversationRevision != 11 || snapshot.SessionWorkspaces["draft"] != "ws" {
		t.Fatalf("snapshot enrichment = %#v", snapshot)
	}
	if len(snapshot.Sessions) != 3 || snapshot.Sessions[0].ID != "draft" || snapshot.Sessions[2].ID != "live" {
		t.Fatalf("sessions = %#v", snapshot.Sessions)
	}
}
