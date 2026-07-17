package seelebridge

import "testing"

func TestSessionStoreRoundTrip(t *testing.T) {
	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := "hello"
	messages := []Message{{Role: "user", Content: &content}}
	if err := store.Save("session-1", messages); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load("session-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 1 || loaded[0].Content == nil || *loaded[0].Content != content {
		t.Fatalf("unexpected messages: %#v", loaded)
	}
	if len(store.List()) != 1 {
		t.Fatal("session metadata missing")
	}
	if err := store.Delete("session-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("session-1"); err == nil {
		t.Fatal("deleted session should not load")
	}
}
