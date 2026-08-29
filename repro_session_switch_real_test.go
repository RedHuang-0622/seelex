package main

// 临时复现测试：真实 runtime + 真实 application 装配下，会话运行中切换/新建是否
// 死锁或长时间阻塞。假 provider 首个请求快速返回，后续请求阻塞（模拟长响应）。
// 诊断用；通过后由维护者决定保留或删除。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
)

// blockingAfterFirstProvider 首个请求立即回复，后续请求阻塞到 release（模拟 LLM 长响应）。
type blockingAfterFirstProvider struct {
	mu           sync.Mutex
	requests     int
	blockingSeen chan struct{}
	release      chan struct{}
}

func newBlockingAfterFirstProvider() *blockingAfterFirstProvider {
	return &blockingAfterFirstProvider{
		blockingSeen: make(chan struct{}),
		release:      make(chan struct{}),
	}
}

func (p *blockingAfterFirstProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request) {
	t.Helper()
	defer request.Body.Close()
	var payload struct {
		Stream bool `json:"stream"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	if !payload.Stream {
		http.Error(writer, "expected streaming request", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	p.requests++
	requestNumber := p.requests
	p.mu.Unlock()

	writer.Header().Set("Content-Type", "text/event-stream")
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	if requestNumber == 1 {
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index": 0,
				"delta": map[string]any{"content": "ok"},
			}},
		})
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": "stop",
			}},
		})
		return
	}

	closeOnce := sync.Once{}
	closeOnce.Do(func() { close(p.blockingSeen) })
	select {
	case <-p.release:
	case <-request.Context().Done():
		return
	}
	writeSSE(t, writer, flusher, map[string]any{
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{"content": "done"},
			"finish_reason": "stop",
		}},
	})
}

func waitReal(t *testing.T, done <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatalf("REAL DEADLOCK REPRO: %s blocked >10s\n===== GOROUTINE DUMP =====\n%s\n===== END DUMP =====", what, allGoroutineStacks())
		return nil
	}
}

// TestRealSwitchWhileChattingRepro 场景 1：会话 A 运行中（provider 阻塞）恢复会话 B。
func TestRealSwitchWhileChattingRepro(t *testing.T) {
	provider := newBlockingAfterFirstProvider()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.serve(t, w, r)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := fmt.Sprintf("roles:\n  agent:\n    - model: test-model\n      base_url: %s\n      api_key: test-key\n", server.URL)
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newFullChainHarness(t, accountsPath, tempDir, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 创建会话 A（首个请求快速返回）。
	if err := harness.app.Submit(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("first turn did not idle: %v\n%s", err, allGoroutineStacks())
	}
	sessionA := harness.app.Snapshot().Session.ID
	if sessionA == "" {
		t.Fatal("session A not materialized")
	}

	// fork 出会话 B（持久化子会话）。
	sessionB, err := harness.app.ForkSessionLatest(sessionA)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}

	// 回到 A 并触发长任务（provider 阻塞 → A 运行中）。
	if err := harness.app.ResumeSession(sessionA); err != nil {
		t.Fatalf("resume A: %v", err)
	}
	if err := harness.app.Submit(ctx, "long task"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-provider.blockingSeen:
	case <-ctx.Done():
		t.Fatalf("provider did not block: %v\n%s", ctx.Err(), allGoroutineStacks())
	}

	// A 运行中恢复 B。
	resumeDone := make(chan error, 1)
	go func() { resumeDone <- harness.app.ResumeSession(sessionB) }()
	if err := waitReal(t, resumeDone, "ResumeSession(B) while A running (real runtime)"); err != nil {
		t.Fatalf("ResumeSession(B) = %v", err)
	}
	if active := harness.app.Snapshot().Session.ID; active != sessionB {
		t.Fatalf("active = %q, want %q", active, sessionB)
	}

	close(provider.release)
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("wait idle after release: %v\n%s", err, allGoroutineStacks())
	}
}

// TestRealBeginNewWhileChattingRepro 场景 2：会话 A 运行中新建会话。
func TestRealBeginNewWhileChattingRepro(t *testing.T) {
	provider := newBlockingAfterFirstProvider()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.serve(t, w, r)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := fmt.Sprintf("roles:\n  agent:\n    - model: test-model\n      base_url: %s\n      api_key: test-key\n", server.URL)
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newFullChainHarness(t, accountsPath, tempDir, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := harness.app.Submit(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.Submit(ctx, "long task"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-provider.blockingSeen:
	case <-ctx.Done():
		t.Fatalf("provider did not block: %v\n%s", ctx.Err(), allGoroutineStacks())
	}

	newDone := make(chan error, 1)
	go func() { newDone <- harness.app.BeginNewSession() }()
	if err := waitReal(t, newDone, "BeginNewSession while A running (real runtime)"); err != nil {
		// ErrChatRunning 是 M1 设计行为：立即返回，不阻塞、不死锁。
		if !errors.Is(err, application.ErrChatRunning) {
			t.Fatalf("BeginNewSession = %v (want nil or ErrChatRunning, no deadlock)", err)
		}
	}

	close(provider.release)
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("wait idle after release: %v\n%s", err, allGoroutineStacks())
	}
}

// TestRealNewSessionDraftLostOnSwitchRepro 记录“新建会话（草稿）→ 切换会话”
// 的现状行为：草稿无 ID、不落盘，切换后从快照与目录中消失。
func TestRealNewSessionDraftLostOnSwitchRepro(t *testing.T) {
	provider := newBlockingAfterFirstProvider()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.serve(t, w, r)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := fmt.Sprintf("roles:\n  agent:\n    - model: test-model\n      base_url: %s\n      api_key: test-key\n", server.URL)
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newFullChainHarness(t, accountsPath, tempDir, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := harness.app.Submit(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	sessionA := harness.app.Snapshot().Session.ID
	sessionB, err := harness.app.ForkSessionLatest(sessionA)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}

	// 新建会话 → 草稿：无 ID、无对话、未落盘。
	if err := harness.app.ResumeSession(sessionA); err != nil {
		t.Fatalf("resume A: %v", err)
	}
	if err := harness.app.BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	draft := harness.app.Snapshot()
	if !draft.Session.Draft || draft.Session.ID != "" {
		t.Fatalf("expected draft session, got %+v", draft.Session)
	}
	if len(draft.Conversation) != 0 {
		t.Fatalf("draft conversation = %d messages, want 0", len(draft.Conversation))
	}

	// 切换会话 B → 草稿被覆盖，快照与目录中都不再有草稿。
	if err := harness.app.ResumeSession(sessionB); err != nil {
		t.Fatalf("resume B: %v", err)
	}
	after := harness.app.Snapshot()
	if after.Session.ID != sessionB || after.Session.Draft {
		t.Fatalf("after switch: session = %+v, want B (draft gone)", after.Session)
	}
	foundDraft := false
	for _, session := range after.Sessions {
		if session.ID == "" || session.ID == draft.Session.ID {
			foundDraft = true
			break
		}
	}
	if foundDraft {
		t.Fatalf("draft unexpectedly appears in catalog: %+v", after.Sessions)
	}
	t.Logf("confirmed: draft (ID=%q) vanished after switching to %q; catalog has %d sessions", draft.Session.ID, sessionB, len(after.Sessions))
}
