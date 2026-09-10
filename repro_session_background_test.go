package main

// 阶段 0 后台会话收尾测试集（test-cases.md 第 2/3 节）：
// TC-A1-02 后台完成落盘后 record 全域只属 A；
// TC-A1-03 后台完成不影响活跃会话 B 的队列与运行态；
// TC-A2-01 收尾时再次切换 C，A 收尾不清 C 的引擎工作历史。

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// backgroundScenario 是"A 运行中切 B → B 完成 → A 后台完成"的标准场景产物。
type backgroundScenario struct {
	harness  fullChainHarness
	provider *requestCountingProvider
	sessionA string
	sessionB string
	store    *sessionstore.Router
}

// startBackgroundCompletionScenario 执行与 repro_session_disappear_test 相同
// 的场景编排，返回完成后（A 已落盘）的快照。bindProject 控制是否绑定项目根
// （跨工作区测试需要 false，另走 workspace 流程）。
func startBackgroundCompletionScenario(t *testing.T, bindProject bool) *backgroundScenario {
	t.Helper()
	provider := newRequestCountingProvider()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.serve(t, w, r)
	}))
	t.Cleanup(server.Close)

	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := "roles:\n  agent:\n    - model: test-model\n      base_url: " + server.URL +
		"\n      api_key: test-key-1\n    - model: test-model\n      base_url: " + server.URL +
		"\n      api_key: test-key-2\n"
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newFullChainHarnessWithProjectBinding(t, accountsPath, tempDir, 30*time.Second, bindProject)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)

	if err := harness.app.Submit(ctx, "first A"); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("first turn did not idle: %v", err)
	}
	sessionA := harness.app.Snapshot().Session.ID
	if sessionA == "" {
		t.Fatal("session A not materialized")
	}
	sessionB, err := harness.app.ForkSessionLatest(sessionA)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
	if err := harness.app.ResumeSession(sessionA); err != nil {
		t.Fatalf("resume A: %v", err)
	}
	if err := harness.app.Submit(ctx, "long task A"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-provider.secondSeen:
	case <-ctx.Done():
		t.Fatalf("provider did not block: %v", ctx.Err())
	}
	if err := harness.app.ResumeSession(sessionB); err != nil {
		t.Fatalf("resume B while A running: %v", err)
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
			t.Fatalf("B did not finish in time: %+v", harness.app.Snapshot().Chat)
		}
		time.Sleep(50 * time.Millisecond)
	}
	close(provider.releaseSecond)
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("wait idle after release: %v", err)
	}

	store, err := initStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &backgroundScenario{harness: harness, provider: provider, sessionA: sessionA, sessionB: sessionB, store: store}
}

// loadStoredRecord 读取 workspace 下会话的持久化 record。
func loadStoredRecord(t *testing.T, store *sessionstore.Router, workspaceID, sessionID string) (application.SessionRecord, bool) {
	t.Helper()
	// S20：record 通道退役，优先读派生态（message head.Meta + message 行 +
	// lifecycle）；非 v8 后端回退 state 通道。
	payload, handled, err := store.DerivedRecordWorkspace(workspaceID, sessionID)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return application.SessionRecord{}, false
		}
		t.Fatalf("load derived record %s/%s: %v", workspaceID, sessionID, err)
	}
	if handled {
		if len(payload) == 0 {
			return application.SessionRecord{}, false
		}
	} else {
		payload, err = store.LoadStateWorkspace(workspaceID, sessionID)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return application.SessionRecord{}, false
			}
			t.Fatalf("load stored record %s/%s: %v", workspaceID, sessionID, err)
		}
	}
	var record application.SessionRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		t.Fatalf("decode stored record %s/%s: %v", workspaceID, sessionID, err)
	}
	return record, true
}

func recordConversationTexts(record application.SessionRecord) []string {
	texts := make([]string, 0, len(record.Conversation.Messages))
	for _, message := range record.Conversation.Messages {
		texts = append(texts, message.Role+":"+message.Content)
	}
	return texts
}

// TC-A1-02：A 后台完成落盘后，record 的 Title/Conversation/transcript/Tasks
// 全域只属 A（无 B 内容）。
func TestBackgroundCompletionPreservesARecordDomains(t *testing.T) {
	scenario := startBackgroundCompletionScenario(t, true)
	record, ok := loadStoredRecord(t, scenario.store, "", scenario.sessionA)
	if !ok {
		t.Fatalf("A record missing from workspace after background completion")
	}
	if strings.Contains(record.Title.Value, "hello B") {
		t.Fatalf("A record title = %q, must not inherit B title", record.Title.Value)
	}
	texts := recordConversationTexts(record)
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, "long task A") {
		t.Fatalf("A record lost in-flight message: %v", texts)
	}
	if strings.Contains(joined, "hello B") {
		t.Fatalf("A record polluted with B conversation: %v", texts)
	}
	for _, task := range record.Tasks {
		if strings.Contains(task.Task, "hello B") || strings.Contains(task.Description, "hello B") {
			t.Fatalf("A record Tasks contains B task: %#v", task)
		}
	}
	// transcript 事件通道也不含 B 内容。
	events, err := scenario.store.LoadEventTailWorkspace("", scenario.sessionA, 1<<20, 10000)
	if err != nil {
		t.Fatalf("load A event tail: %v", err)
	}
	for _, event := range events {
		if strings.Contains(event.Content, "hello B") {
			t.Fatalf("A transcript contains B event: %#v", event)
		}
	}
}

// blockingProvider 与 requestCountingProvider 相同，但指定请求序号的请求
// 各自阻塞（TC-A1-03 需要 B 首轮保持运行使 q1/q2 排队；TC-A2-01 需要 C 在
// A 收尾期间运行）。
type blockingProvider struct {
	mu       sync.Mutex
	requests int
	seen     map[int]chan struct{}
	release  map[int]chan struct{}
}

func newBlockingProvider(blockedNumbers ...int) *blockingProvider {
	provider := &blockingProvider{
		seen:    make(map[int]chan struct{}, len(blockedNumbers)),
		release: make(map[int]chan struct{}, len(blockedNumbers)),
	}
	for _, number := range blockedNumbers {
		provider.seen[number] = make(chan struct{})
		provider.release[number] = make(chan struct{})
	}
	return provider
}

func (p *blockingProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request) {
	t.Helper()
	defer request.Body.Close()
	var payload struct {
		Stream bool `json:"stream"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
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
	if release, blocked := p.release[requestNumber]; blocked {
		closeOnce := sync.Once{}
		closeOnce.Do(func() { close(p.seen[requestNumber]) })
		select {
		case <-release:
		case <-request.Context().Done():
			return
		}
	}
	writeSSE(t, writer, flusher, map[string]any{
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         map[string]any{"content": "ok"},
			"finish_reason": "stop",
		}},
	})
}

// TC-A1-03：A 后台完成收尾不得影响活跃会话 B 的队列与运行态。
func TestBackgroundCompletionKeepsActiveSessionQueue(t *testing.T) {
	provider := newBlockingProvider(2, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.serve(t, w, r)
	}))
	defer server.Close()
	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := "roles:\n  agent:\n    - model: test-model\n      base_url: " + server.URL +
		"\n      api_key: test-key-1\n    - model: test-model\n      base_url: " + server.URL +
		"\n      api_key: test-key-2\n"
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newFullChainHarness(t, accountsPath, tempDir, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := harness.app.Submit(ctx, "first A"); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	sessionA := harness.app.Snapshot().Session.ID
	sessionB, err := harness.app.ForkSessionLatest(sessionA)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.app.ResumeSession(sessionA); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.Submit(ctx, "long task A"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-provider.seen[2]:
	case <-ctx.Done():
		t.Fatal("A long task did not block")
	}
	if err := harness.app.ResumeSession(sessionB); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.Submit(ctx, "hello B"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-provider.seen[3]:
	case <-ctx.Done():
		t.Fatal("B first turn did not block")
	}
	// B 运行中排队 q1/q2。
	if err := harness.app.Submit(ctx, "q1"); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.Submit(ctx, "q2"); err != nil {
		t.Fatal(err)
	}
	// A 后台完成。
	close(provider.release[2])
	store, err := initStore()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	deadlineA := time.Now().Add(20 * time.Second)
	for {
		record, ok := loadStoredRecord(t, store, "", sessionA)
		if ok {
			joined := strings.Join(recordConversationTexts(record), "\n")
			if strings.Contains(joined, "long task A") {
				break
			}
		}
		if time.Now().After(deadlineA) {
			t.Fatalf("A did not persist after release: %v", ctx.Err())
		}
		time.Sleep(50 * time.Millisecond)
	}
	// B 仍运行中（请求 #3 未释放），队列应保留 q1/q2，且不被 A 收尾清掉。
	snapshot := harness.app.Snapshot()
	if snapshot.Session.ID != sessionB {
		t.Fatalf("active session = %q, want %q", snapshot.Session.ID, sessionB)
	}
	if !snapshot.Chat.Running {
		t.Fatalf("B chat stopped while q1/q2 queued: %+v", snapshot.Chat)
	}
	queue := snapshot.Chat.InputQueue
	if len(queue) != 2 || queue[0] != "q1" || queue[1] != "q2" {
		t.Fatalf("B input queue = %v, want [q1 q2]", queue)
	}
	close(provider.release[3])
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("B did not finish after release: %v", err)
	}
}

// TC-A2-01：A 收尾与 C 运行并发（真实交错）；A 的 ReleaseWorkingHistoryFor
// 不得清 C 的引擎工作历史或串写 C。
func TestBackgroundCompletionWhileSwitchingToC(t *testing.T) {
	provider := newBlockingProvider(2, 5)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.serve(t, w, r)
	}))
	defer server.Close()
	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := "roles:\n  agent:\n    - model: test-model\n      base_url: " + server.URL +
		"\n      api_key: test-key-1\n    - model: test-model\n      base_url: " + server.URL +
		"\n      api_key: test-key-2\n"
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newFullChainHarness(t, accountsPath, tempDir, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := harness.app.Submit(ctx, "first A"); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	sessionA := harness.app.Snapshot().Session.ID
	sessionB, err := harness.app.ForkSessionLatest(sessionA)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.app.ResumeSession(sessionA); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.Submit(ctx, "long task A"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-provider.seen[2]:
	case <-ctx.Done():
		t.Fatal("A long task did not block")
	}
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
	// C：fork B 后完成一轮（请求 #4 快速返回）。
	sessionC, err := harness.app.ForkSessionLatest(sessionB)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.app.ResumeSession(sessionC); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.Submit(ctx, "hello C"); err != nil {
		t.Fatal(err)
	}
	for {
		snapshot := harness.app.Snapshot()
		if snapshot.Session.ID == sessionC && !snapshot.Chat.Running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("C first turn did not finish: %+v", harness.app.Snapshot().Chat)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// C 运行中（请求 #5 阻塞）：此刻释放 A，使 A 收尾与 C 运行并发。
	if err := harness.app.Submit(ctx, "hello C2"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-provider.seen[5]:
	case <-ctx.Done():
		t.Fatal("C second turn did not block")
	}
	close(provider.release[2])
	store, err := initStore()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	deadlineA := time.Now().Add(20 * time.Second)
	for {
		record, ok := loadStoredRecord(t, store, "", sessionA)
		if ok {
			joined := strings.Join(recordConversationTexts(record), "\n")
			if strings.Contains(joined, "long task A") {
				break
			}
		}
		if time.Now().After(deadlineA) {
			t.Fatalf("A did not persist while C running")
		}
		time.Sleep(50 * time.Millisecond)
	}
	// A 收尾完成：C 仍运行中（工作历史未被 A 清掉）。
	snapshot := harness.app.Snapshot()
	if snapshot.Session.ID != sessionC || !snapshot.Chat.Running {
		t.Fatalf("C execution affected by A completion: %+v", snapshot.Chat)
	}
	record, ok := loadStoredRecord(t, store, "", sessionA)
	if !ok {
		t.Fatalf("A record missing")
	}
	for _, text := range recordConversationTexts(record) {
		if strings.Contains(text, "hello C") {
			t.Fatalf("A record polluted with C content: %v", recordConversationTexts(record))
		}
	}
	// 释放 C：C 完整收尾，可见内容含 hello C 与 hello C2。
	close(provider.release[5])
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("C did not finish: %v", err)
	}
	if err := harness.app.ResumeSession(sessionC); err != nil {
		t.Fatal(err)
	}
	if !containsUserText(harness.app.Snapshot().Conversation, "hello C") ||
		!containsUserText(harness.app.Snapshot().Conversation, "hello C2") {
		t.Fatalf("C conversation incomplete after A completion: %v", conversationTexts(harness.app.Snapshot().Conversation))
	}
}
