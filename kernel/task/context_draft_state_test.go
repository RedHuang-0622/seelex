package task_context

import (
	"context"
	"reflect"
	"testing"

	model "github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/state"
)

func TestContextDraftStateIsCopiedAtPersistenceBoundary(t *testing.T) {
	coordinator := NewCoordinator(Deps{Core: &state.Core{}})
	input := model.ContextDraftState{
		Items:       []model.ContextDraftItem{{Seq: 1, Bytes: "a"}, {Seq: 2, Bytes: "b", Final: true}},
		Watermark:   1,
		Uncommitted: true,
	}
	coordinator.ReplaceContextDraftStateFor("session-a", input)
	input.Items[0].Bytes = "mutated"

	got := coordinator.ContextDraftStateFor("session-a")
	if !reflect.DeepEqual(got.Items, []model.ContextDraftItem{{Seq: 1, Bytes: "a"}, {Seq: 2, Bytes: "b", Final: true}}) {
		t.Fatalf("draft state not copied: %+v", got.Items)
	}
	if got.Watermark != 1 || !got.Uncommitted {
		t.Fatalf("draft metadata lost: %+v", got)
	}
	got.Items[0].Bytes = "reader-mutated"
	if coordinator.ContextDraftStateFor("session-a").Items[0].Bytes != "a" {
		t.Fatal("reader mutated authoritative draft state")
	}
}

func TestContextDraftActorCommitsUserWireAndAdvancesWatermark(t *testing.T) {
	coordinator := NewCoordinator(Deps{
		Core: &state.Core{},
		CurrentSessionID: func() string {
			return "session-a"
		},
	})
	event := coordinator.AppendTranscriptEventForLocked("session-a", model.TranscriptEvent{
		TaskID: "req-a", Role: "user", Content: "raw",
	})
	if !coordinator.RecordUserWireContentLocked("session-a", "req-a", "raw + env") {
		t.Fatal("user wire was not recorded")
	}
	state := coordinator.ContextDraftStateFor("session-a")
	if len(state.Items) != 1 || state.Items[0].Seq != event.Seq || state.Items[0].Bytes != "raw + env" {
		t.Fatalf("wire was not staged in draft actor: %+v", state)
	}
	if err := coordinator.CommitSessionContextDraft(context.Background(), "session-a"); err != nil {
		t.Fatalf("commit draft: %v", err)
	}
	state = coordinator.ContextDraftStateFor("session-a")
	if state.Watermark != event.Seq || len(state.Items) != 0 || state.Uncommitted {
		t.Fatalf("draft ack did not advance watermark: %+v", state)
	}
}
