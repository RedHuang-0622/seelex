package main

// headless 多会话 / 冷热切换 pprof 探查（2026-09-09）。
//
// 场景 1（应用层）：A→B→C fork 链 + 独立会话 D，各完成 2 轮；随后多次
// 热切换（驻留会话互切）与冷切换（UnloadSession → ResumeSession 后台冷
// 加载），最后进程重启冷恢复 C；每次切换校验目标会话只含自身（或 fork 血
// 缘）内容，不出现其它会话的 seed（跨会话污染/幻读探针）。
//
// 场景 2（存储层）：4 个会话并发写提交 + 并发读（AssembleWire/ReadRange），
// 供 mutex/block pprof 观察仓库级/模块级锁竞争与读写热点。
//
// pprof 运行示例：
//   go test . -run 'TestSessionSwitchHotColdProfile|TestStorageConcurrentSessionLockProfile' \
//     -count=1 -cpuprofile %TEMP%\switch.cpu -memprofile %TEMP%\switch.mem \
//     -mutexprofile %TEMP%\switch.mutex -blockprofile %TEMP%\switch.block -timeout 600s

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

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestSessionSwitchHotColdProfile 应用层多会话冷热切换 + 重启恢复探查。
func TestSessionSwitchHotColdProfile(t *testing.T) {
	server := newEchoProvider(t)
	tempDir := t.TempDir()
	accountsPath := filepath.Join(tempDir, "accounts.yaml")
	if err := os.WriteFile(accountsPath, []byte(fmt.Sprintf(
		"roles:\n  agent:\n    - model: test-model\n      base_url: %s\n      api_key: test-key\n",
		server.URL,
	)), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	harness := newFullChainHarness(t, accountsPath, tempDir, 10*time.Second)

	submitAndIdle := func(text string) string {
		t.Helper()
		if err := harness.app.Submit(ctx, text); err != nil {
			t.Fatalf("submit %q: %v", text, err)
		}
		if err := harness.app.WaitForIdle(ctx); err != nil {
			t.Fatalf("idle %q: %v", text, err)
		}
		return harness.app.Snapshot().Session.ID
	}
	waitSessionReady := func(sid, seed string) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for {
			snap := harness.app.Snapshot()
			if snap.Session.ID == sid && snap.Session.Status != "restoring" &&
				strings.Contains(conversationText(snap.Conversation), seed) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("session %s not ready (status=%s)", sid, snap.Session.Status)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	assertView := func(sid, own, forbidden string) {
		t.Helper()
		snap, err := harness.app.SnapshotOf(sid)
		if err != nil {
			t.Fatalf("SnapshotOf(%s): %v", sid, err)
		}
		text := conversationText(snap.Conversation)
		if !strings.Contains(text, own) {
			t.Fatalf("session %s missing own content %q", sid, own)
		}
		if strings.Contains(text, forbidden) {
			t.Fatalf("session %s polluted by %q: %s", sid, forbidden, text)
		}
	}

	sessionA := submitAndIdle("seed A")
	sessionB, err := harness.app.ForkSessionLatest(sessionA)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.app.ResumeSession(sessionB); err != nil {
		t.Fatal(err)
	}
	submitAndIdle("seed B")
	sessionC, err := harness.app.ForkSessionLatest(sessionB)
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.app.ResumeSession(sessionC); err != nil {
		t.Fatal(err)
	}
	submitAndIdle("seed C")
	if err := harness.app.BeginNewSession(); err != nil {
		t.Fatal(err)
	}
	sessionD := submitAndIdle("seed D")
	seeds := map[string]string{
		sessionA: "seed A",
		sessionB: "seed B",
		sessionC: "seed C",
		sessionD: "seed D",
	}

	assertView(sessionC, "seed C", "seed D")
	assertView(sessionD, "seed D", "seed A")

	// 热切换（驻留互切）6 轮。
	order := []string{sessionA, sessionB, sessionC, sessionD, sessionA, sessionC, sessionB, sessionD}
	for _, sid := range order {
		if err := harness.app.ResumeSession(sid); err != nil {
			t.Fatalf("hot resume %s: %v", sid, err)
		}
		waitSessionReady(sid, seeds[sid])
	}

	// 冷切换：逐个卸载再恢复（后台冷加载），重复两轮。
	for cycle := 0; cycle < 2; cycle++ {
		for _, sid := range []string{sessionD, sessionB, sessionA, sessionC} {
			if err := harness.app.UnloadSession(sid); err != nil {
				t.Fatalf("unload %s: %v", sid, err)
			}
			start := time.Now()
			if err := harness.app.ResumeSession(sid); err != nil {
				t.Fatalf("cold resume %s: %v", sid, err)
			}
			waitSessionReady(sid, seeds[sid])
			t.Logf("cold_resume session=%s %s", sid, time.Since(start))
		}
	}

	// 进程重启 → 冷恢复 C，验证跨会话数据仍在各自键下。
	sessionID := sessionC
	harness.app.Shutdown()
	harnessRestarted := newFullChainHarness(t, accountsPath, tempDir, 10*time.Second)
	start := time.Now()
	if err := harnessRestarted.app.ResumeSession(sessionID); err != nil {
		t.Fatalf("restart resume: %v", err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		snap := harnessRestarted.app.Snapshot()
		if snap.Session.ID == sessionID && snap.Session.Status != "restoring" &&
			strings.Contains(conversationText(snap.Conversation), "seed C") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restart cold resume did not land")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("restart_cold_resume C %s", time.Since(start))
	text := conversationText(harnessRestarted.app.Snapshot().Conversation)
	if strings.Contains(text, "seed D") {
		t.Fatalf("restart C polluted by D: %s", text)
	}
	harnessRestarted.app.Shutdown()
}

// TestStorageConcurrentSessionLockProfile 存储层多会话并发读写（锁竞争/热点）。
func TestStorageConcurrentSessionLockProfile(t *testing.T) {
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	projectID := "profile-project"
	sessions := []string{"sess-w0", "sess-w1", "sess-w2", "sess-w3"}
	// 先建布局，避免读者与首写竞争时按“不存在会话”走旧回退报错。
	for _, sessionID := range sessions {
		if err := router.SaveCommitWorkspace(projectID, sessionID, sessionstore.Commit{}); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(sessions)*2)
	for index, sessionID := range sessions {
		wg.Add(1)
		go func(sessionID string, base int) {
			defer wg.Done()
			seq := uint64(0)
			now := time.Now().UTC()
			for round := 0; round < 60; round++ {
				seq++
				event := sessionstore.Event{
					Seq: seq, Role: "user", Content: fmt.Sprintf("w%d-r%d", base, round),
					CreatedAt: now, TokenCount: 1,
				}
				commit := sessionstore.Commit{
					Events: []sessionstore.Event{event},
					State:  []byte(fmt.Sprintf(`{"id":%q,"round":%d}`, sessionID, round)),
				}
				if err := router.SaveCommitWorkspace(projectID, sessionID, commit); err != nil {
					errs <- err
					return
				}
				if _, _, err := router.AssembleWireWorkspace(projectID, sessionID, 200_000, 3); err != nil {
					errs <- err
					return
				}
			}
		}(sessionID, index)
	}
	for _, sessionID := range sessions {
		wg.Add(1)
		go func(sessionID string) {
			defer wg.Done()
			for round := 0; round < 60; round++ {
				if _, _, err := router.LoadRangeWorkspace(projectID, sessionID, 0, 0); err != nil {
					errs <- err
					return
				}
				if _, err := router.LoadEventTailWorkspace(projectID, sessionID, 1<<20, 8); err != nil {
					errs <- err
					return
				}
			}
		}(sessionID)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func newEchoProvider(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		lastUser := "ok"
		for index := len(payload.Messages) - 1; index >= 0; index-- {
			if payload.Messages[index].Role == "user" {
				_ = json.Unmarshal(payload.Messages[index].Content, &lastUser)
				break
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"choices":[{"message":{"role":"assistant","content":"echo:%s"}}]}`, lastUser)))
	}))
	t.Cleanup(server.Close)
	return server
}
