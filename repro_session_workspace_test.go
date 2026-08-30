package main

// 阶段 0 跨工作区键漂移测试集（test-cases.md 第 5/6 节）：
// TC-A4-01/02/03 后台会话落盘键 = 会话自身 workspace，且不改变当前写作用域、
// 不在他域产生幽灵会话；TC-A5-02 fork 不改变其它会话落盘键。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// startCrossWorkspaceScenario：A 绑定 workspace X；切到 workspace Y 建 B；
// A 运行中切 B 完成；A 后台完成落盘。返回两 workspace ID 与 store。
func startCrossWorkspaceScenario(t *testing.T) (harness fullChainHarness, sessionA, sessionB, wsX, wsY string, store *sessionstore.Router) {
	t.Helper()
	provider := newBlockingProvider(3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.serve(t, w, r)
	}))
	t.Cleanup(server.Close)
	tempDir := t.TempDir()
	xRoot := filepath.Join(tempDir, "x")
	yRoot := filepath.Join(tempDir, "y")
	if err := os.MkdirAll(xRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(yRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := "roles:\n  agent:\n    - model: test-model\n      base_url: " + server.URL +
		"\n      api_key: test-key-1\n    - model: test-model\n      base_url: " + server.URL +
		"\n      api_key: test-key-2\n"
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness = newUnboundFullChainHarness(t, accountsPath, tempDir, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)

	workspaceIDByName := func(name string) string {
		for _, workspace := range harness.app.Snapshot().Workspaces {
			// 显示名 = root 目录 basename（workspaceDisplayName），按目录名匹配。
			if workspace.Name == name || filepath.Base(workspace.RootPath) == name {
				return workspace.ID
			}
		}
		t.Fatalf("workspace %q not found: %#v", name, harness.app.Snapshot().Workspaces)
		return ""
	}
	if err := harness.app.CreateWorkspace("ws-x", xRoot, ""); err != nil {
		t.Fatal(err)
	}
	wsX = workspaceIDByName("x")
	if err := harness.app.Submit(ctx, "first A"); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	sessionA = harness.app.Snapshot().Session.ID
	// 离开 A 进入草稿：BeginNewSession 会把 A 按自身 workspace（X）落盘。
	if err := harness.app.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.CreateWorkspace("ws-y", yRoot, ""); err != nil {
		t.Fatal(err)
	}
	wsY = workspaceIDByName("y")
	// 草稿已绑定 Y（CreateWorkspace 的 draft 分支），提交 first B
	// （请求 #2 快速返回）→ B 物化绑定 Y。
	if err := harness.app.Submit(ctx, "first B"); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	sessionB = harness.app.Snapshot().Session.ID
	if sessionA == "" || sessionB == "" || sessionA == sessionB {
		t.Fatalf("sessions A=%q B=%q must be distinct", sessionA, sessionB)
	}
	// 回到 A 提交长任务（请求 #3 阻塞）。
	if err := harness.app.ResumeSession(sessionA); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.Submit(ctx, "long task A"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-provider.seen[3]:
	case <-ctx.Done():
		t.Fatal("A long task did not block")
	}
	// 切 B 完成一轮。
	if err := harness.app.ResumeSession(sessionB); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.Submit(ctx, "hello B"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		snapshot := harness.app.Snapshot()
		if snapshot.Session.ID == sessionB && !snapshot.Chat.Running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("B did not finish: %+v", harness.app.Snapshot().Chat)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// A 后台完成。
	close(provider.release[3])
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	store, err := initStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return harness, sessionA, sessionB, wsX, wsY, store
}

// TC-A4-01/02：A 的 record 落在自身 workspace X；Y 下无 A 键；Router 写作用域
// 仍为 Y（后台完成无副作用）。
func TestBackgroundPersistUsesSessionWorkspaceKey(t *testing.T) {
	_, sessionA, _, wsX, wsY, store := startCrossWorkspaceScenario(t)
	record, ok := loadStoredRecord(t, store, wsX, sessionA)
	if !ok {
		t.Fatalf("A record missing from workspace X after background completion")
	}
	joined := strings.Join(recordConversationTexts(record), "\n")
	if !strings.Contains(joined, "long task A") {
		t.Fatalf("A record in X lost in-flight message: %v", recordConversationTexts(record))
	}
	if _, ok := loadStoredRecord(t, store, wsY, sessionA); ok {
		t.Fatalf("A record leaked into workspace Y")
	}
}

// TC-A4-02：后台完成落盘不改变当前 Router 写作用域（Snapshot.CurrentWorkspace
// 仍是 Y）。
func TestBackgroundPersistDoesNotMutateWriteScope(t *testing.T) {
	harness, _, _, _, wsY, _ := startCrossWorkspaceScenario(t)
	snapshot := harness.app.Snapshot()
	if snapshot.CurrentWorkspace == nil || snapshot.CurrentWorkspace.ID != wsY {
		t.Fatalf("write scope after background persist = %#v, want %q", snapshot.CurrentWorkspace, wsY)
	}
}

// TC-A4-03：workspace Y 的 catalog 不含 A 会话条目（无幽灵会话）。
func TestNoPhantomSessionInForeignWorkspace(t *testing.T) {
	_, sessionA, _, _, wsY, store := startCrossWorkspaceScenario(t)
	for _, meta := range store.ListWorkspace(wsY) {
		if meta.SessionID == sessionA {
			t.Fatalf("session A appears in workspace Y catalog: %#v", meta)
		}
	}
}

// TC-A5-02：fork 不改变其它会话落盘键（fork 后 C 仍落自身 workspace）。
func TestForkDoesNotShiftOtherSessionPersistKey(t *testing.T) {
	harness, _, sessionB, wsX, wsY, store := startCrossWorkspaceScenario(t)
	child, err := harness.app.ForkSessionLatest(sessionB)
	if err != nil {
		t.Fatal(err)
	}
	if harness.app.Snapshot().CurrentWorkspace == nil || harness.app.Snapshot().CurrentWorkspace.ID != wsY {
		t.Fatalf("fork shifted write scope: %#v", harness.app.Snapshot().CurrentWorkspace)
	}
	// fork 子会话写自身 workspace（Y），不写 X。
	childRecord, ok := loadStoredRecord(t, store, wsY, child)
	if !ok {
		t.Fatalf("forked child missing from workspace Y")
	}
	_ = childRecord
	if _, ok := loadStoredRecord(t, store, wsX, child); ok {
		t.Fatalf("forked child leaked into workspace X")
	}
}
