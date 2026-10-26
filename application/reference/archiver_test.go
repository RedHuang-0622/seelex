package reference

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
)

type recordingCompressedTurnStore struct {
	projectID string
	sessionID string
	ref       string
	content   string
	calls     int
}

func (store *recordingCompressedTurnStore) SaveCompressedTurn(projectID, sessionID, ref, content string) error {
	store.projectID = projectID
	store.sessionID = sessionID
	store.ref = ref
	store.content = content
	store.calls++
	return nil
}

func TestCompressedTurnArchiverUsesContextSessionBeforeProvider(t *testing.T) {
	store := &recordingCompressedTurnStore{}
	providerCalls := 0
	archiver := &CompressedTurnArchiver{
		Store: store,
		SessionIDProvider: func() string {
			providerCalls++
			return "session-view"
		},
		ProjectIDProvider: func(sessionID string) string { return "project-" + sessionID },
	}
	content := "compressed body"
	ctx := task_context.WithSessionID(context.Background(), "session-background")
	ref, err := archiver.StoreTurn(ctx, "segment-1", []types.Message{{
		Role: "user", Content: &content,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if ref != CompressedTurnRefPrefix+"segment-1" {
		t.Fatalf("ref = %q", ref)
	}
	if store.projectID != "project-session-background" || store.sessionID != "session-background" {
		t.Fatalf("scope = %s/%s", store.projectID, store.sessionID)
	}
	if providerCalls != 0 {
		t.Fatalf("provider called %d time(s) although context carried a session", providerCalls)
	}
}

func TestCompressedTurnArchiverFallsBackToProviderAndRejectsNoOwner(t *testing.T) {
	store := &recordingCompressedTurnStore{}
	archiver := &CompressedTurnArchiver{
		Store:             store,
		SessionIDProvider: func() string { return "session-view" },
		WorkspaceIDProvider: func() string {
			return "project-view"
		},
	}
	content := "warm-up body"
	if _, err := archiver.StoreTurn(context.Background(), "segment-2", []types.Message{{
		Role: "user", Content: &content,
	}}); err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 || store.projectID != "project-view" || store.sessionID != "session-view" {
		t.Fatalf("fallback scope = %s/%s calls=%d", store.projectID, store.sessionID, store.calls)
	}

	orphan := &CompressedTurnArchiver{Store: &recordingCompressedTurnStore{}}
	if _, err := orphan.StoreTurn(context.Background(), "segment-3", nil); err == nil {
		t.Fatal("compression without a routable session must fail")
	}
}
