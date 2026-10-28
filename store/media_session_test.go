package store

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	storeartifact "github.com/RedHuang-0622/seelex/store/artifact"
	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

func openMediaSessionStore(t *testing.T, settings storeroot.Settings) *Stores {
	t.Helper()
	stores, err := OpenStores(t.TempDir(), settings)
	if err != nil {
		t.Fatalf("OpenStores: %v", err)
	}
	t.Cleanup(func() { _ = stores.Close() })
	return stores
}

func TestSessionMediaUsesExplicitScope(t *testing.T) {
	stores := openMediaSessionStore(t, storeroot.Settings{})
	ctx := context.Background()
	payload := []byte("png-bytes")
	item := storeartifact.MediaItem{
		Name: "screenshot-1.png", MimeType: "image/png", Kind: "screenshot",
		Data: payload, Width: 320, Height: 180, Scale: 0.5,
	}

	ref, err := stores.Media.WriteMedia(ctx, "proj-a", "sess-1", item)
	if err != nil {
		t.Fatalf("WriteMedia: %v", err)
	}
	if ref.Ref != storeartifact.MediaRefPrefix+ref.Hash || ref.SessionID != "sess-1" {
		t.Fatalf("media ref = %+v", ref)
	}
	if stores.Router.Workspace() != "" {
		t.Fatalf("explicit scope changed active workspace: %q", stores.Router.Workspace())
	}
	again, err := stores.Media.WriteMedia(ctx, "proj-a", "sess-1", item)
	if err != nil || again.Hash != ref.Hash {
		t.Fatalf("idempotent write = %+v, %v", again, err)
	}

	read, data, err := stores.Media.ReadMedia(ctx, "proj-a", "sess-1", ref.Ref)
	if err != nil {
		t.Fatalf("ReadMedia: %v", err)
	}
	if !bytes.Equal(data, payload) || read.Width != 320 || read.MimeType != "image/png" {
		t.Fatalf("read media = %+v data=%q", read, data)
	}
	if _, _, err := stores.Media.ReadMedia(ctx, "proj-a", "sess-2", ref.Ref); err == nil {
		t.Fatal("media must not cross session boundaries")
	}
	if _, err := stores.Media.WriteMedia(ctx, "proj-a", "  ", item); err == nil {
		t.Fatal("missing session id must fail")
	}
}

func TestMediaRefsRoundTripThroughToolResult(t *testing.T) {
	stores := openMediaSessionStore(t, storeroot.Settings{})
	key := Key{ProjectID: "p-media", SessionID: "s-media"}
	item, err := stores.Media.WriteMedia(context.Background(), key.ProjectID, key.SessionID, storeartifact.MediaItem{
		Name: "shot.png", MimeType: "image/png", Kind: "screenshot",
		Data: []byte("shot"), Width: 8, Height: 8,
	})
	if err != nil {
		t.Fatalf("WriteMedia: %v", err)
	}
	if err := stores.Router.SaveCommitWorkspace(key.ProjectID, key.SessionID, Commit{
		Events: []Event{{Seq: 1, Role: "user", Content: "看截图", MessageID: "m1"}},
		ToolResults: []ToolResult{{
			Ref: "blob:tool-1", Tool: "screenshot", Content: "captured 8x8",
			Digest: "d1", Size: 13, Multimodal: []storeartifact.MediaRef{item},
		}},
	}); err != nil {
		t.Fatalf("SaveCommitWorkspace: %v", err)
	}
	results, err := stores.Router.ListToolResultsWorkspace(key.ProjectID, key.SessionID)
	if err != nil || len(results) != 1 {
		t.Fatalf("ListToolResultsWorkspace = %+v err=%v", results, err)
	}
	if len(results[0].Multimodal) != 1 || results[0].Multimodal[0].Ref != item.Ref {
		t.Fatalf("multimodal refs lost: %+v", results[0].Multimodal)
	}
	raw, err := json.Marshal(results[0])
	if err != nil || !bytes.Contains(raw, []byte(`"multimodal"`)) {
		t.Fatalf("result json = %s err=%v", raw, err)
	}
}
