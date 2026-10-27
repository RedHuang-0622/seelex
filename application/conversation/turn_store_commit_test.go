package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

type turnStoreCommitFake struct {
	order []string
	fail  string
}

func (fake *turnStoreCommitFake) WriteArtifacts(context.Context, string, string, []protocol.StoredToolResult) error {
	fake.order = append(fake.order, "artifact")
	if fake.fail == "artifact" {
		return errors.New("artifact failed")
	}
	return nil
}

func (fake *turnStoreCommitFake) WriteMessages(context.Context, string, string, []protocol.TranscriptEvent) error {
	fake.order = append(fake.order, "message")
	if fake.fail == "message" {
		return errors.New("message failed")
	}
	return nil
}

func (fake *turnStoreCommitFake) WriteCompactions(context.Context, string, string, []json.RawMessage) error {
	fake.order = append(fake.order, "compaction")
	if fake.fail == "compaction" {
		return errors.New("compaction failed")
	}
	return nil
}

func TestTurnStoreCommitPreservesM0OrderAndStopsAtFailure(t *testing.T) {
	batch := TurnStoreCommitBatch{
		ProjectID: "project", SessionID: "session",
		ToolResults: []protocol.StoredToolResult{{ToolResultRef: protocol.ToolResultRef{Ref: "result:1"}}},
		Events:      []protocol.TranscriptEvent{{Seq: 1, Role: "user", Content: "hi"}},
		Compactions: []json.RawMessage{json.RawMessage(`{}`)},
	}
	fake := &turnStoreCommitFake{}
	if err := NewTurnStoreCommit(fake).Commit(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fake.order, []string{"artifact", "message", "compaction"}) {
		t.Fatalf("order=%v", fake.order)
	}

	fake = &turnStoreCommitFake{fail: "message"}
	if err := NewTurnStoreCommit(fake).Commit(context.Background(), batch); err == nil {
		t.Fatal("message failure must stop the turn commit")
	}
	if !reflect.DeepEqual(fake.order, []string{"artifact", "message"}) {
		t.Fatalf("failure order=%v: compaction must not run after message failure", fake.order)
	}
}
