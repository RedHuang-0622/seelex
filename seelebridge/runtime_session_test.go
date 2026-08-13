package seelebridge

import (
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestRuntimeMainSessionIDTracksCurrentSession 验证 MainSessionID 跟踪
// 当前主会话（新建/恢复都会重建 Session），会话切换后返回新 ID——
// 压缩帧 SegmentID 溯源随会话切换更新。
func TestRuntimeMainSessionIDTracksCurrentSession(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	if got := runtime.MainSessionID(); got != "" {
		t.Fatalf("initial MainSessionID = %q, want empty", got)
	}
	first, err := runtime.NewMainSessionWithID("session-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = first
	if got := runtime.MainSessionID(); got != "session-a" {
		t.Fatalf("MainSessionID after create = %q, want session-a", got)
	}
	second, err := runtime.NewMainSessionWithID("session-b", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = second
	if got := runtime.MainSessionID(); got != "session-b" {
		t.Fatalf("MainSessionID after switch = %q, want session-b", got)
	}
}
func TestRuntimeNewMainSessionWithIDKeepsDurableResumeIdentity(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	runtime.AttachHistoryRouter(router)

	session, err := runtime.NewMainSessionWithID("resume-session-42", nil)
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID() != "resume-session-42" {
		t.Fatalf("framework session ID = %q, want durable resume key", session.SessionID())
	}
}
