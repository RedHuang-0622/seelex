package main

// 复现测试：会话 A 运行中（provider 阻塞模拟长任务）时切到会话 B，
// B 完成一轮对话后，A 在后台完成。验证 A 的持久化内容是否完好：
// 期望 A 保留自己的消息（"long task A" 与回复），不被 B 的内容污染。

import (
	"context"
	"encoding/json"
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

// requestCountingProvider 按请求序号控制行为：
//   - 请求 #1（会话 A 首轮）快速返回
//   - 请求 #2（会话 A 的长任务）阻塞直到 release
//   - 请求 #3+（会话 B 等）快速返回
type requestCountingProvider struct {
	mu            sync.Mutex
	requests      int
	secondSeen    chan struct{}
	releaseSecond chan struct{}
}

func newRequestCountingProvider() *requestCountingProvider {
	return &requestCountingProvider{
		secondSeen:    make(chan struct{}),
		releaseSecond: make(chan struct{}),
	}
}

func (p *requestCountingProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request) {
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

	if requestNumber == 2 {
		closeOnce := sync.Once{}
		closeOnce.Do(func() { close(p.secondSeen) })
		select {
		case <-p.releaseSecond:
		case <-request.Context().Done():
			return
		}
		writeSSE(t, writer, flusher, map[string]any{
			"choices": []any{map[string]any{
				"index":         0,
				"delta":         map[string]any{"content": "DONE_FROM_A"},
				"finish_reason": "stop",
			}},
		})
		return
	}

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
}

func conversationTexts(conversation []application.Message) []string {
	texts := make([]string, 0, len(conversation))
	for _, message := range conversation {
		texts = append(texts, message.Role+":"+message.Content)
	}
	return texts
}

func containsUserText(conversation []application.Message, needle string) bool {
	for _, message := range conversation {
		if message.Role == "user" && message.Content == needle {
			return true
		}
	}
	return false
}

// TestBackgroundSessionCompletionMustNotPolluteOwner 复现：A 运行中切到 B，
// B 完成后 A 在后台完成 → 断言 A 内容不被 B 污染、A 自己的新消息不丢失。
func TestBackgroundSessionCompletionMustNotPolluteOwner(t *testing.T) {
	provider := newRequestCountingProvider()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.serve(t, w, r)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := fmt.Sprintf("roles:\n  agent:\n    - model: test-model\n      base_url: %s\n      api_key: test-key-1\n    - model: test-model\n      base_url: %s\n      api_key: test-key-2\n", server.URL, server.URL)
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newFullChainHarness(t, accountsPath, tempDir, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 1) 会话 A 首轮（请求 #1 快速返回）。
	if err := harness.app.Submit(ctx, "first A"); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("first turn did not idle: %v\n%s", err, allGoroutineStacks())
	}
	sessionA := harness.app.Snapshot().Session.ID
	if sessionA == "" {
		t.Fatal("session A not materialized")
	}

	// 2) fork 出会话 B（持久化子会话）。
	sessionB, err := harness.app.ForkSessionLatest(sessionA)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}

	// 3) 回到 A 触发长任务（请求 #2 阻塞 → A 运行中）。
	if err := harness.app.ResumeSession(sessionA); err != nil {
		t.Fatalf("resume A: %v", err)
	}
	if err := harness.app.Submit(ctx, "long task A"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-provider.secondSeen:
	case <-ctx.Done():
		t.Fatalf("provider did not block: %v\n%s", ctx.Err(), allGoroutineStacks())
	}

	// 4) A 运行中切换到 B，并让 B 完成一轮（请求 #3 快速返回）。
	if err := harness.app.ResumeSession(sessionB); err != nil {
		t.Fatalf("resume B while A running: %v", err)
	}
	if err := harness.app.Submit(ctx, "hello B"); err != nil {
		t.Fatal(err)
	}
	// B 是活跃会话：等 B 的 Chat 结束（Running=false）。
	deadline := time.Now().Add(15 * time.Second)
	for {
		snapshot := harness.app.Snapshot()
		if snapshot.Session.ID == sessionB && !snapshot.Chat.Running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("B did not finish in time: %+v\n%s", harness.app.Snapshot().Chat, allGoroutineStacks())
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 5) 释放 A：A 在后台（B 为活跃会话）完成 → 触发 PersistCurrentSession(A)。
	close(provider.releaseSecond)
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("wait idle after release: %v\n%s", err, allGoroutineStacks())
	}

	// 6) 切回 A：验证 A 的内容完好（A 自己的消息在、B 的消息不混入）。
	if err := harness.app.ResumeSession(sessionA); err != nil {
		t.Fatalf("resume A after background completion: %v", err)
	}
	snapshot := harness.app.Snapshot()
	if snapshot.Session.ID != sessionA {
		t.Fatalf("active session = %q, want %q", snapshot.Session.ID, sessionA)
	}
	texts := conversationTexts(snapshot.Conversation)
	t.Logf("A conversation after background completion: %v", texts)
	if !containsUserText(snapshot.Conversation, "long task A") {
		t.Fatalf("BUG REPRO: session A lost its own in-flight user message %q after background completion; conversation = %v",
			"long task A", texts)
	}
	if containsUserText(snapshot.Conversation, "hello B") {
		t.Fatalf("BUG REPRO: session A is polluted with session B's message %q; conversation = %v",
			"hello B", texts)
	}
}
