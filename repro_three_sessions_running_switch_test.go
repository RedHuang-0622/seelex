package main

// 复现：三个会话同时在运行（每个会话一次 LLM 请求被 provider 阻塞），期间在
// A/B/C 之间快速切换，验证：
//   1) 切换不等待当前运行完成（ResumeSession 应毫秒级返回）；
//   2) 切到的会话上下文不丢失（每个会话的可见对话只含自己的内容）；
//   3) 全部释放后各会话均能正常收尾 idle。
// 三个账号（各 MaxConcurrency=1）→ 允许 3 个在途请求同时阻塞，模拟
// "≥3 个会话都在运行中"。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// blockingFromRequestNProvider 前 quickCount 个请求快速返回，之后每个请求阻塞
// 到 release 或被取消（模拟 3 个会话各自的长任务同时 in-flight）。
type blockingFromRequestNProvider struct {
	mu          sync.Mutex
	requests    int
	quickCount  int
	blockedSeen chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	releaseAll  bool
}

func newBlockingFromRequestNProvider(quickCount int) *blockingFromRequestNProvider {
	return &blockingFromRequestNProvider{
		quickCount:  quickCount,
		blockedSeen: make(chan struct{}, 16),
		release:     make(chan struct{}),
	}
}

func (p *blockingFromRequestNProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request) {
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
	block := requestNumber > p.quickCount && !p.releaseAll
	p.mu.Unlock()

	writer.Header().Set("Content-Type", "text/event-stream")
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	if block {
		select {
		case p.blockedSeen <- struct{}{}:
		default:
		}
		select {
		case <-p.release:
		case <-request.Context().Done():
			return
		}
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"content": "DONE"},
				"finish_reason": "stop",
			}},
		})
		fmt.Fprint(writer, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}
	writeSSE(t, writer, flusher, map[string]any{
		"choices": []any{map[string]any{
			"index": 0, "delta": map[string]any{"content": "ok"},
		}},
	})
	writeSSE(t, writer, flusher, map[string]any{
		"choices": []any{map[string]any{
			"index": 0, "delta": map[string]any{}, "finish_reason": "stop",
		}},
	})
	fmt.Fprint(writer, "data: [DONE]\n\n")
	flusher.Flush()
}

func (p *blockingFromRequestNProvider) releaseAllNow() {
	p.mu.Lock()
	p.releaseAll = true
	p.mu.Unlock()
	p.releaseOnce.Do(func() { close(p.release) })
}

func TestThreeRunningSessionsSwitchDoesNotWaitAndKeepsContext(t *testing.T) {
	provider := newBlockingFromRequestNProvider(3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.serve(t, w, r)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := "roles:\n  agent:\n" +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-1\n", server.URL) +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-2\n", server.URL) +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-3\n", server.URL)
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newFullChainHarness(t, accountsPath, tempDir, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 创建三个空闲会话（首 3 个请求快速返回）。
	submitAndIdle := func(ctx context.Context, text string) string {
		if err := harness.app.Submit(ctx, text); err != nil {
			t.Fatalf("submit %q: %v", text, err)
		}
		if err := harness.app.WaitForIdle(ctx); err != nil {
			t.Fatalf("idle after %q: %v\n%s", text, err, allGoroutineStacks())
		}
		return harness.app.Snapshot().Session.ID
	}
	sessionA := submitAndIdle(ctx, "first A")
	sessionB, err := harness.app.ForkSessionLatest(sessionA)
	if err != nil {
		t.Fatalf("fork B: %v", err)
	}
	if got := submitAndIdle(ctx, "first B"); got != sessionB {
		t.Fatalf("first B active = %q, want %q", got, sessionB)
	}
	sessionC, err := harness.app.ForkSessionLatest(sessionB)
	if err != nil {
		t.Fatalf("fork C: %v", err)
	}
	if got := submitAndIdle(ctx, "first C"); got != sessionC {
		t.Fatalf("first C active = %q, want %q", got, sessionC)
	}

	// 三个会话各自开始长任务（请求 #4/#5/#6 同时阻塞）→ 三个会话都在运行中。
	startLong := func(sessionID, text string) {
		if err := harness.app.ResumeSession(sessionID); err != nil {
			t.Fatalf("resume %s: %v", sessionID, err)
		}
		if err := harness.app.Submit(ctx, text); err != nil {
			t.Fatalf("submit %q: %v", text, err)
		}
	}
	startLong(sessionA, "long A")
	startLong(sessionB, "long B")
	startLong(sessionC, "long C")

	for i := 0; i < 3; i++ {
		select {
		case <-provider.blockedSeen:
		case <-ctx.Done():
			t.Fatalf("only %d/3 session requests blocked: %v\n%s", i, ctx.Err(), allGoroutineStacks())
		}
	}

	// 三会话均确认在运行。
	for _, sid := range []string{sessionA, sessionB, sessionC} {
		if err := harness.app.ResumeSession(sid); err != nil {
			t.Fatalf("resume %s for state check: %v", sid, err)
		}
		snap := harness.app.Snapshot()
		if snap.Session.ID != sid {
			t.Fatalf("active = %q, want %q", snap.Session.ID, sid)
		}
		if !snap.Chat.Running {
			t.Fatalf("session %s should be running (long task in-flight)", sid)
		}
	}

	// 关键断言 1：三个会话同时在运行中，切换必须不等待当前运行完成。
	for i := 0; i < 2; i++ {
		for _, sid := range []string{sessionA, sessionB, sessionC, sessionA} {
			started := time.Now()
			done := make(chan error, 1)
			go func(sid string) { done <- harness.app.ResumeSession(sid) }(sid)
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("ResumeSession(%s) = %v", sid, err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("ResumeSession(%s) blocked >5s while 3 sessions running (wait repro!)\n%s", sid, allGoroutineStacks())
			}
			elapsed := time.Since(started)
			if elapsed > 5*time.Second {
				t.Fatalf("ResumeSession(%s) took %v while 3 sessions running", sid, elapsed)
			}
			snap := harness.app.Snapshot()
			if snap.Session.ID != sid {
				t.Fatalf("after switch active = %q, want %q", snap.Session.ID, sid)
			}
		}
	}

	// 关键断言 2：切到的会话上下文不丢失、不串台。
	assertOwnContext := func(sid, ownText string) {
		snap, err := harness.app.SnapshotOf(sid)
		if err != nil {
			t.Fatalf("SnapshotOf(%s): %v", sid, err)
		}
		joined := make([]string, 0, len(snap.Conversation))
		for _, message := range snap.Conversation {
			joined = append(joined, message.Role+":"+message.Content)
		}
		all := strings.Join(joined, "\n")
		if !strings.Contains(all, ownText) {
			t.Fatalf("session %s lost own context %q: %s", sid, ownText, all)
		}
	}
	assertOwnContext(sessionA, "first A")
	assertOwnContext(sessionA, "long A")
	assertOwnContext(sessionB, "first B")
	assertOwnContext(sessionB, "long B")
	assertOwnContext(sessionC, "first C")
	assertOwnContext(sessionC, "long C")

	// 释放全部阻塞请求，三会话应正常收尾 idle。
	provider.releaseAllNow()
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("wait idle after release: %v\n%s", err, allGoroutineStacks())
	}
	for _, sid := range []string{sessionA, sessionB, sessionC} {
		snap, err := harness.app.SnapshotOf(sid)
		if err != nil {
			t.Fatalf("SnapshotOf(%s) after idle: %v", sid, err)
		}
		if snap.Chat.Running {
			t.Fatalf("session %s still running after release", sid)
		}
	}
}
