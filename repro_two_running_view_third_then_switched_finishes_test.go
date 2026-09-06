package main

// 复现：A、B 两个会话同时在运行中，期间点击第三个会话 C 查看，然后让
// “被切换出去的会话”A 先跑完（B 仍阻塞在途），紧接着执行下一次切换。
//
// 用户现象：切换动作往往要等到被切走的会话跑完才开始（或跑完前后视图
// 内容/后续切换出现串写、卡住）。用 -race 运行本用例，定位锁与竞争。
//
// 覆盖两个变体：
//   - hot：C 已驻留（创建后保持 resident），点 C 走 hot_attach；
//   - cold：C 先卸载（UnloadSession 后未驻留），点 C 走 restoring 空壳 +
//     后台冷加载。

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

	"github.com/RedHuang-0622/seelex/application"
)

// perRequestGateProvider 前 quickCount 个请求快速返回；之后每个请求独立阻塞
// 到 release(n) 或 ctx 取消（可按请求逐个放行，模拟“被切走的 A 先跑完、
// B 仍在跑”）。
type perRequestGateProvider struct {
	mu       sync.Mutex
	requests int
	quick    int
	seen     chan int
	gates    map[int]chan struct{}
}

func newPerRequestGateProvider(quick int) *perRequestGateProvider {
	return &perRequestGateProvider{
		quick: quick,
		seen:  make(chan int, 32),
		gates: make(map[int]chan struct{}),
	}
}

func (p *perRequestGateProvider) serve(t *testing.T, writer http.ResponseWriter, request *http.Request) {
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
	number := p.requests
	quick := number <= p.quick
	var gate chan struct{}
	if !quick {
		gate = make(chan struct{})
		p.gates[number] = gate
	}
	p.mu.Unlock()

	writer.Header().Set("Content-Type", "text/event-stream")
	flusher, ok := writer.(http.Flusher)
	if !ok {
		http.Error(writer, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	if !quick {
		select {
		case p.seen <- number:
		default:
		}
		select {
		case <-gate:
		case <-request.Context().Done():
			return
		}
	}
	writeSSE(t, writer, flusher, map[string]any{
		"choices": []any{map[string]any{
			"index": 0, "delta": map[string]any{"content": "DONE"},
			"finish_reason": "stop",
		}},
	})
	fmt.Fprint(writer, "data: [DONE]\n\n")
	flusher.Flush()
}

func (p *perRequestGateProvider) release(number int) {
	p.mu.Lock()
	gate := p.gates[number]
	delete(p.gates, number)
	p.mu.Unlock()
	if gate != nil {
		close(gate)
	}
}

func (p *perRequestGateProvider) releaseAll() {
	p.mu.Lock()
	gates := p.gates
	p.gates = make(map[int]chan struct{})
	p.mu.Unlock()
	for _, gate := range gates {
		close(gate)
	}
}

func (p *perRequestGateProvider) debug() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	open := make([]int, 0, len(p.gates))
	for number := range p.gates {
		open = append(open, number)
	}
	return fmt.Sprintf("requests=%d openGates=%v", p.requests, open)
}

func (p *perRequestGateProvider) waitBlocked(t *testing.T, ctx context.Context, number int) {
	t.Helper()
	select {
	case got := <-p.seen:
		if got != number {
			t.Fatalf("blocked request %d arrived, want %d", got, number)
		}
	case <-ctx.Done():
		t.Fatalf("request %d never blocked: %v", number, ctx.Err())
	}
}

// runTwoRunningViewThirdScenario 驱动一轮完整时序；cold 变体把 C 卸载为冷。
func runTwoRunningViewThirdScenario(t *testing.T, cold bool) {
	provider := newPerRequestGateProvider(3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.serve(t, w, r)
	}))
	defer server.Close()
	t.Cleanup(provider.releaseAll)

	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := "roles:\n  agent:\n" +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-1\n", server.URL) +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-2\n", server.URL) +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-3\n", server.URL) +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-4\n", server.URL)
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newFullChainHarness(t, accountsPath, tempDir, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	submitAndIdle := func(ctx context.Context, text string) string {
		t.Helper()
		if err := harness.app.Submit(ctx, text); err != nil {
			t.Fatalf("submit %q: %v", text, err)
		}
		if err := harness.app.WaitForIdle(ctx); err != nil {
			t.Fatalf("idle after %q: %v\n%s", text, err, allGoroutineStacks())
		}
		return harness.app.Snapshot().Session.ID
	}

	// 三个会话各自完成一轮快速请求（request #1/#2/#3）→ A/B/C 均已驻留空闲。
	sessionA := submitAndIdle(ctx, "seed A")
	sessionB, err := harness.app.ForkSessionLatest(sessionA)
	if err != nil {
		t.Fatalf("fork B: %v", err)
	}
	if got := submitAndIdle(ctx, "seed B"); got != sessionB {
		t.Fatalf("seed B active = %q, want %q", got, sessionB)
	}
	sessionC, err := harness.app.ForkSessionLatest(sessionB)
	if err != nil {
		t.Fatalf("fork C: %v", err)
	}
	if got := submitAndIdle(ctx, "seed C"); got != sessionC {
		t.Fatalf("seed C active = %q, want %q", got, sessionC)
	}

	if cold {
		// 卸载 C：脱离驻留后点击走后台冷加载（运行中 + 未驻留 → restoring）。
		if err := harness.app.UnloadSession(sessionC); err != nil {
			t.Fatalf("unload C: %v", err)
		}
	}

	// A 运行中（request #4 阻塞），视图停在 A；随后切到 B 并让 B 也运行
	// （request #5 阻塞）→ 两个会话在途、被切走的 A 仍在后台跑。
	if err := harness.app.ResumeSession(sessionA); err != nil {
		t.Fatalf("resume A: %v", err)
	}
	if err := harness.app.Submit(ctx, "long A"); err != nil {
		t.Fatalf("submit long A: %v", err)
	}
	provider.waitBlocked(t, ctx, 4)
	if err := harness.app.ResumeSession(sessionB); err != nil {
		t.Fatalf("resume B: %v", err)
	}
	if err := harness.app.Submit(ctx, "long B"); err != nil {
		t.Fatalf("submit long B: %v", err)
	}
	provider.waitBlocked(t, ctx, 5)

	// 两个会话确认在途。
	for _, sid := range []string{sessionA, sessionB} {
		snap, err := harness.app.SnapshotOf(sid)
		if err != nil {
			t.Fatalf("SnapshotOf(%s): %v", sid, err)
		}
		if !snap.Chat.Running {
			t.Fatalf("session %s should be running (long task in-flight)", sid)
		}
	}

	// 关键步骤 1：两个会话运行中点击第三个会话 C 查看。ResumeSession 必须
	// 毫秒级返回（不得等被切走的 A 跑完才推进）；视图应立即是 C 且带 C 自己
	// 的内容/状态，而不是 A/B 残留或一直停留在“等待”。
	switchDone := make(chan error, 1)
	started := time.Now()
	go func() { switchDone <- harness.app.ResumeSession(sessionC) }()
	select {
	case err := <-switchDone:
		if err != nil {
			t.Fatalf("ResumeSession(C) = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("RED: ResumeSession(C) blocked >2s while A+B running（切换在等被切走的会话跑完）\n%s", allGoroutineStacks())
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("ResumeSession(C) took %v", elapsed)
	}
	snap := harness.app.Snapshot()
	if snap.Session.ID != sessionC {
		t.Fatalf("after clicking C view = %q, want %q", snap.Session.ID, sessionC)
	}
	var joinedC string
	if cold {
		if got := snap.Session.Status; got != "restoring" {
			t.Fatalf("cold C status = %q, want restoring", got)
		}
		// 后台冷加载应独立于 A/B 运行进度完成：等待 C 装载完成（内容落地）
		// 必须在“放行 A”之前发生——若 C 的装载结果要等某个运行中会话收尾
		// 才可见，即复现“切换动作在会话进行完后才开始”。
		deadline := time.Now().Add(5 * time.Second)
		for {
			snapC, err := harness.app.SnapshotOf(sessionC)
			if err != nil {
				t.Fatalf("SnapshotOf(C) while restoring: %v", err)
			}
			text := conversationText(snapC.Conversation)
			if snapC.Session.Status != "restoring" && strings.Contains(text, "seed C") {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("RED: cold C content did not land within 5s while A+B running (status=%q, text=%q)\nprovider: %s\n%s",
					snapC.Session.Status, text, provider.debug(), allGoroutineStacks())
			}
			select {
			case <-time.After(20 * time.Millisecond):
			case <-ctx.Done():
				t.Fatalf("wait cold C: %v", ctx.Err())
			}
		}
		snap = harness.app.Snapshot()
		if snap.Session.ID != sessionC {
			t.Fatalf("view changed while cold C loaded: %q, want %q", snap.Session.ID, sessionC)
		}
		joinedC = conversationText(snap.Conversation)
	} else {
		joinedC = conversationText(snap.Conversation)
	}
	if !strings.Contains(joinedC, "seed C") {
		t.Fatalf("view C missing own context (saw %q)", joinedC)
	}
	if strings.Contains(joinedC, "seed A") || strings.Contains(joinedC, "seed B") ||
		strings.Contains(joinedC, "long A") || strings.Contains(joinedC, "long B") {
		t.Fatalf("view C polluted by A/B content: %q", joinedC)
	}

	// 关键步骤 2：放行“被切换出去的会话”A（request #4）；B 仍阻塞。
	// A 的 runChat 收尾（持久化/事件/空闲对齐）在视图 = C、B 在途的背景下
	// 并发执行——这正是用户报告里“会话进行完了才开始下一步”的窗口。
	provider.release(4)
	waitSessionIdle(t, ctx, harness, sessionA)
	if !stillRunning(t, ctx, harness, sessionB) {
		t.Fatalf("B should still be running while A finished\nprovider: %s", provider.debug())
	}

	// 关键步骤 3：A 收尾完成后的“下一步切换动作”必须依旧即时、内容各自归位。
	for _, sid := range []string{sessionB, sessionC, sessionA} {
		started := time.Now()
		done := make(chan error, 1)
		go func(sid string) { done <- harness.app.ResumeSession(sid) }(sid)
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("ResumeSession(%s) after A finished = %v", sid, err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("RED: next switch to %s blocked after A finished\n%s", sid, allGoroutineStacks())
		}
		if elapsed := time.Since(started); elapsed > 3*time.Second {
			t.Fatalf("next switch to %s took %v", sid, elapsed)
		}
		if sid == sessionC {
			if snap := harness.app.Snapshot(); snap.Session.ID != sessionC {
				t.Fatalf("after switch active = %q, want %q", snap.Session.ID, sessionC)
			}
		}
	}

	// A 的最终回复必须已经落到 A 自己的可见上下文（收尾不与切换互相丢内容）。
	snapA, err := harness.app.SnapshotOf(sessionA)
	if err != nil {
		t.Fatalf("SnapshotOf(A): %v", err)
	}
	if text := conversationText(snapA.Conversation); !strings.Contains(text, "long A") || !strings.Contains(text, "DONE") {
		t.Fatalf("A lost own final context: %q", text)
	}

	// 收尾：放行 B，全部空闲。
	provider.releaseAll()
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

func conversationText(messages []application.Message) string {
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		parts = append(parts, message.Role+":"+message.Content)
	}
	return strings.Join(parts, "\n")
}

func waitSessionIdle(t *testing.T, ctx context.Context, harness fullChainHarness, sid string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		snap, err := harness.app.SnapshotOf(sid)
		if err != nil {
			t.Fatalf("SnapshotOf(%s): %v", sid, err)
		}
		if !snap.Chat.Running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("session %s did not become idle after release\n%s", sid, allGoroutineStacks())
		}
		select {
		case <-time.After(20 * time.Millisecond):
		case <-ctx.Done():
			t.Fatalf("wait idle(%s): %v", sid, ctx.Err())
		}
	}
}

func stillRunning(t *testing.T, ctx context.Context, harness fullChainHarness, sid string) bool {
	t.Helper()
	snap, err := harness.app.SnapshotOf(sid)
	if err != nil {
		t.Fatalf("SnapshotOf(%s): %v", sid, err)
	}
	return snap.Chat.Running
}

func TestTwoRunningViewThirdThenSwitchedFinishesHot(t *testing.T) {
	runTwoRunningViewThirdScenario(t, false)
}

func TestTwoRunningViewThirdThenSwitchedFinishesCold(t *testing.T) {
	runTwoRunningViewThirdScenario(t, true)
}

// TestTwoRunningViewThirdSwitchStress 把同一时序高频重复，配合 -race 放大
// 竞争窗口（点击 C ↔ A 收尾 ↔ 下一步切换）。
func TestTwoRunningViewThirdSwitchStress(t *testing.T) {
	if testing.Short() {
		t.Skip("stress only with -race")
	}
	provider := newPerRequestGateProvider(3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.serve(t, w, r)
	}))
	defer server.Close()
	defer provider.releaseAll()

	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := "roles:\n  agent:\n" +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-1\n", server.URL) +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-2\n", server.URL) +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-3\n", server.URL) +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-4\n", server.URL)
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newFullChainHarness(t, accountsPath, tempDir, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()

	submitAndIdle := func(ctx context.Context, text string) string {
		t.Helper()
		if err := harness.app.Submit(ctx, text); err != nil {
			t.Fatalf("submit %q: %v", text, err)
		}
		if err := harness.app.WaitForIdle(ctx); err != nil {
			t.Fatalf("idle after %q: %v\n%s", text, err, allGoroutineStacks())
		}
		return harness.app.Snapshot().Session.ID
	}
	sessionA := submitAndIdle(ctx, "seed A")
	sessionB, err := harness.app.ForkSessionLatest(sessionA)
	if err != nil {
		t.Fatalf("fork B: %v", err)
	}
	if got := submitAndIdle(ctx, "seed B"); got != sessionB {
		t.Fatalf("seed B active = %q, want %q", got, sessionB)
	}
	sessionC, err := harness.app.ForkSessionLatest(sessionB)
	if err != nil {
		t.Fatalf("fork C: %v", err)
	}
	if got := submitAndIdle(ctx, "seed C"); got != sessionC {
		t.Fatalf("seed C active = %q, want %q", got, sessionC)
	}

	if err := harness.app.ResumeSession(sessionA); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.Submit(ctx, "long A"); err != nil {
		t.Fatal(err)
	}
	provider.waitBlocked(t, ctx, 4)
	if err := harness.app.ResumeSession(sessionB); err != nil {
		t.Fatal(err)
	}
	if err := harness.app.Submit(ctx, "long B"); err != nil {
		t.Fatal(err)
	}
	provider.waitBlocked(t, ctx, 5)

	// 反复：A 收尾的同时高频切换 C/B/A/C（每次切换都校验返回快与视图归位）。
	releaseA := make(chan struct{})
	go func() {
		select {
		case <-releaseA:
		case <-time.After(3 * time.Second):
		}
		provider.release(4)
	}()

	switcherCtx, switcherCancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer switcherCancel()
	switcherErr := make(chan error, 1)
	go func() {
		for i := 0; i < 80; i++ {
			select {
			case <-switcherCtx.Done():
				switcherErr <- fmt.Errorf("stress switcher deadline: %w", switcherCtx.Err())
				return
			default:
			}
			for _, sid := range []string{sessionC, sessionB, sessionA, sessionC} {
				done := make(chan error, 1)
				go func(sid string) { done <- harness.app.ResumeSession(sid) }(sid)
				select {
				case err := <-done:
					if err != nil {
						switcherErr <- fmt.Errorf("ResumeSession(%s): %w", sid, err)
						return
					}
				case <-time.After(3 * time.Second):
					switcherErr <- fmt.Errorf("ResumeSession(%s) blocked in stress\n%s", sid, allGoroutineStacks())
					return
				}
				if snap := harness.app.Snapshot(); snap.Session.ID != sid {
					switcherErr <- fmt.Errorf("stress view = %q, want %q", snap.Session.ID, sid)
					return
				}
			}
		}
		switcherErr <- nil
	}()

	close(releaseA)
	waitSessionIdle(t, ctx, harness, sessionA)
	select {
	case err := <-switcherErr:
		if err != nil {
			t.Fatal(err)
		}
	case <-switcherCtx.Done():
		t.Fatalf("switcher did not finish: %v", switcherCtx.Err())
	}

	provider.releaseAll()
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("wait idle: %v\n%s", err, allGoroutineStacks())
	}
}

// TestSwitchToIdleThirdBlocksWhileAliasEngineSessionRuns 复现用户报告的
// “切换动作要等被切走的会话跑完才开始”：EnginePort 的进程级活跃别名
// （port.engine）可能指向一个正在运行的会话引擎；core 热挂载空闲目标时在
// 收尾无条件调用全局 SetSystemPrompt（EnginePort.SetSystemPrompt → 别名
// 引擎.SetSystemPrompt），而 framework Session.ChatStream 全程持锁 →
// 切换 RPC 阻塞到该运行中会话结束。
//
// 时序：先把 A 卸载并冷加载（让活跃别名 = A 的引擎）→ B 长任务在跑 →
// A 长任务在跑（别名引擎 A 被 ChatStream 持锁）→ 点击空闲热会话 C 必须
// 毫秒级完成，当前实现会等到 A 放行才返回（RED）。
func TestSwitchToIdleThirdBlocksWhileAliasEngineSessionRuns(t *testing.T) {
	provider := newPerRequestGateProvider(3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provider.serve(t, w, r)
	}))
	defer server.Close()
	t.Cleanup(provider.releaseAll)

	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	accounts := "roles:\n  agent:\n" +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-1\n", server.URL) +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-2\n", server.URL) +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-3\n", server.URL) +
		fmt.Sprintf("    - model: test-model\n      base_url: %s\n      api_key: test-key-4\n", server.URL)
	if err := os.WriteFile(accountsPath, []byte(accounts), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := newFullChainHarness(t, accountsPath, tempDir, 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	submitAndIdle := func(ctx context.Context, text string) string {
		t.Helper()
		if err := harness.app.Submit(ctx, text); err != nil {
			t.Fatalf("submit %q: %v", text, err)
		}
		if err := harness.app.WaitForIdle(ctx); err != nil {
			t.Fatalf("idle after %q: %v\n%s", text, err, allGoroutineStacks())
		}
		return harness.app.Snapshot().Session.ID
	}

	sessionA := submitAndIdle(ctx, "seed A")
	sessionB, err := harness.app.ForkSessionLatest(sessionA)
	if err != nil {
		t.Fatalf("fork B: %v", err)
	}
	if got := submitAndIdle(ctx, "seed B"); got != sessionB {
		t.Fatalf("seed B active = %q, want %q", got, sessionB)
	}
	sessionC, err := harness.app.ForkSessionLatest(sessionB)
	if err != nil {
		t.Fatalf("fork C: %v", err)
	}
	if got := submitAndIdle(ctx, "seed C"); got != sessionC {
		t.Fatalf("seed C active = %q, want %q", got, sessionC)
	}

	// 让 EnginePort 的活跃别名指向 A：卸载 A 再冷加载（无会话运行中，同步
	// 冷加载会把 port.engine/port.sessionID 切到 A 的新引擎）。
	if err := harness.app.UnloadSession(sessionA); err != nil {
		t.Fatalf("unload A: %v", err)
	}
	if err := harness.app.ResumeSession(sessionA); err != nil {
		t.Fatalf("cold resume A: %v", err)
	}
	if got := harness.app.Snapshot().Session.ID; got != sessionA {
		t.Fatalf("after cold resume view = %q, want %q", got, sessionA)
	}

	// B 长任务在跑（request #4 阻塞）；活跃别名仍是 A（A 尚未运行）。
	if err := harness.app.ResumeSession(sessionB); err != nil {
		t.Fatalf("resume B: %v", err)
	}
	if err := harness.app.Submit(ctx, "long B"); err != nil {
		t.Fatalf("submit long B: %v", err)
	}
	provider.waitBlocked(t, ctx, 4)

	// A 长任务在跑（request #5 阻塞）：此刻进程活跃别名 = A 的引擎且被
	// A 的 ChatStream 全程持锁。
	if err := harness.app.ResumeSession(sessionA); err != nil {
		t.Fatalf("resume A: %v", err)
	}
	if err := harness.app.Submit(ctx, "long A"); err != nil {
		t.Fatalf("submit long A: %v", err)
	}
	provider.waitBlocked(t, ctx, 5)
	for _, sid := range []string{sessionA, sessionB} {
		snap, err := harness.app.SnapshotOf(sid)
		if err != nil {
			t.Fatalf("SnapshotOf(%s): %v", sid, err)
		}
		if !snap.Chat.Running {
			t.Fatalf("session %s should be running", sid)
		}
	}

	// 点击第三个（空闲热）会话 C：不得等待被切走的 A 收尾。
	switched := make(chan error, 1)
	started := time.Now()
	go func() { switched <- harness.app.ResumeSession(sessionC) }()
	select {
	case err := <-switched:
		if err != nil {
			t.Fatalf("ResumeSession(C) = %v", err)
		}
		if elapsed := time.Since(started); elapsed > 2*time.Second {
			t.Fatalf("ResumeSession(C) took %v while alias-engine session A running", elapsed)
		}
	case <-time.After(2 * time.Second):
		// RED：切换卡在别名引擎锁上，等 A 放行后才返回。
		provider.release(5)
		select {
		case err := <-switched:
			if err != nil {
				t.Fatalf("ResumeSession(C) after releasing A = %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("ResumeSession(C) still blocked after A released\n%s", allGoroutineStacks())
		}
		t.Fatalf("RED REPRO: ResumeSession(C) blocked >2s; it only completed after the switched-away session A finished\n%s", allGoroutineStacks())
	}

	if snap := harness.app.Snapshot(); snap.Session.ID != sessionC {
		t.Fatalf("after click C view = %q, want %q", snap.Session.ID, sessionC)
	}
	provider.releaseAll()
	if err := harness.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("wait idle: %v\n%s", err, allGoroutineStacks())
	}
}
