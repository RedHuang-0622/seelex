package seelebridge

import (
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
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

// TestRuntimeSessionBundlesArePerSession 验证会话槽化（决策契约 §1/§2）：
// 每个逻辑会话独立 bundle（Session/锁/绑定状态），切换会话不销毁其它
// 会话的驻留 Session 与 context store（互不串写）；只读面（Session()/
// MainSessionID()/PrepareMainSessionHistory）按当前激活会话路由。
func TestRuntimeSessionBundlesArePerSession(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	runtime.AttachHistoryRouter(router)

	first, err := runtime.NewMainSessionWithID("session-a", nil)
	if err != nil {
		t.Fatal(err)
	}
	// 为 session-a 装配独立 context store（Attach 路由到当前激活会话）。
	storeA := sessionstore.NewSessionContextStore(router, "session-a")
	if err := storeA.SetSystemPrompt("prompt-a"); err != nil {
		t.Fatal(err)
	}
	runtime.AttachSessionContextStore(storeA)

	second, err := runtime.NewMainSessionWithID("session-b", nil)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || second == nil {
		t.Fatalf("sessions missing: first=%v second=%v", first, second)
	}

	// 两个会话各自独立 bundle：session-a 的 Session 实例未被切换销毁，
	// context store 未串写进 session-b。
	runtime.bundlesMu.RLock()
	bundleA := runtime.bundles["session-a"]
	bundleB := runtime.bundles["session-b"]
	runtime.bundlesMu.RUnlock()
	if bundleA == nil || bundleB == nil {
		t.Fatalf("bundles missing: a=%v b=%v", bundleA, bundleB)
	}
	bundleA.mu.Lock()
	sessionA := bundleA.session
	bundleA.mu.Unlock()
	if sessionA != first {
		t.Fatal("session-a bundle session was replaced by session switch")
	}
	if bundleA.binding.contextStore() == nil || bundleA.binding.contextStore().SystemPrompt() != "prompt-a" {
		t.Fatal("session-a context store missing or lost after switch")
	}
	if bundleB.binding.contextStore() != nil {
		t.Fatal("session-b inherited session-a context store (跨会话串写)")
	}

	// 只读面按当前激活会话路由（向后兼容语义）。
	if runtime.MainSessionID() != "session-b" {
		t.Fatalf("MainSessionID = %q, want session-b", runtime.MainSessionID())
	}
	if runtime.Session() != second {
		t.Fatal("Session() does not return the active session")
	}

	// PrepareMainSessionHistory 按目标会话路由（session-a 的历史准备不
	// 影响 session-b）。
	content := "handoff-a"
	history := []types.Message{{Role: "user", Content: &content}}
	if !runtime.PrepareMainSessionHistory("session-a", history) {
		t.Fatal("PrepareMainSessionHistory for session-a returned false")
	}
}
