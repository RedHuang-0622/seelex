package main

// AB 双链路会话冒烟（2026-09-09 工作包）：
//   - 旧链路：SEELEX_STORAGE_LAYOUT=legacy → manifest/generation/transcript/
//     rollout 布局 + 旧三读恢复；
//   - 新链路：默认 v8 布局 + R2 装配恢复（AssembleWireHistoryWorkspace）。
//
// 同一场景（多轮快速请求 → 进程重启 → ResumeSession → 下一轮），断言：
//   1) 每条链路内"重启后首请求 == 不重启继续运行"（前缀逐条一致）；
//   2) 两条链路互相之间请求消息一致（无丢/重/乱序差异）；
//   3) 冷恢复内容完整（可见会话消息数、seed 文本不缺）；
// 同时输出恢复耗时与 store 文件/字节分布（读写热点对比），用于报告。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// abChainMetrics 收集一条链路的冒烟指标。
type abChainMetrics struct {
	chain           string
	rounds          int
	roundsWall      time.Duration
	restartResume   time.Duration
	restartRequest  []probeWireMessage
	continuedReq    []probeWireMessage
	restartMatches  bool
	crossChainEqual bool
	storeFiles      int
	storeBytes      int64
	byCategory      map[string]int64
	generationDirs  int
	v8Sessions      int
}

func abChainScenario(t *testing.T, chain string, settledRounds, resumeRepeats int) abChainMetrics {
	t.Helper()
	if chain == "legacy" {
		t.Setenv(sessionstore.V8WireLayoutEnv, "legacy")
	} else {
		t.Setenv(sessionstore.V8WireLayoutEnv, "v8")
	}
	const nextPrompt = "round-next"
	metrics := abChainMetrics{chain: chain, rounds: settledRounds, byCategory: make(map[string]int64)}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	// 对照 A：不重启继续第 settledRounds+1 轮。
	continueRecorder := newPrefixRecordingServer(t)
	continueStore := t.TempDir()
	continueAccounts := filepath.Join(continueStore, "accounts.yaml")
	if err := os.WriteFile(continueAccounts, []byte(fmt.Sprintf(
		"roles:\n  agent:\n    - model: test-model\n      base_url: %s\n      api_key: test-key\n",
		continueRecorder.URL,
	)), 0o600); err != nil {
		t.Fatal(err)
	}
	harnessContinue := newFullChainHarness(t, continueAccounts, continueStore, 10*time.Second)
	roundStart := time.Now()
	runSettledRounds(t, ctx, harnessContinue.app, settledRounds)
	if err := harnessContinue.app.Submit(ctx, nextPrompt); err != nil {
		t.Fatalf("[%s] continue submit: %v", chain, err)
	}
	if err := harnessContinue.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("[%s] continue idle: %v", chain, err)
	}
	metrics.roundsWall = time.Since(roundStart)
	continueRecords := continueRecorder.snapshot()
	if len(continueRecords) != settledRounds+1 {
		t.Fatalf("[%s] continue requests=%d want %d", chain, len(continueRecords), settledRounds+1)
	}
	metrics.continuedReq = continueRecords[len(continueRecords)-1]
	harnessContinue.app.Shutdown()

	// 对照 B：同样轮数 → 重启 → ResumeSession → 提交 next。
	restartRecorder := newPrefixRecordingServer(t)
	restartStore := t.TempDir()
	restartAccounts := filepath.Join(restartStore, "accounts.yaml")
	if err := os.WriteFile(restartAccounts, []byte(fmt.Sprintf(
		"roles:\n  agent:\n    - model: test-model\n      base_url: %s\n      api_key: test-key\n",
		restartRecorder.URL,
	)), 0o600); err != nil {
		t.Fatal(err)
	}
	harnessFirst := newFullChainHarness(t, restartAccounts, restartStore, 10*time.Second)
	runSettledRounds(t, ctx, harnessFirst.app, settledRounds)
	sessionID := harnessFirst.app.Snapshot().Session.ID
	if sessionID == "" {
		t.Fatal("session did not materialize")
	}
	harnessFirst.app.Shutdown()
	restartRecorder.reset()

	harnessRestarted := newFullChainHarness(t, restartAccounts, restartStore, 10*time.Second)
	resumeStart := time.Now()
	if err := harnessRestarted.app.ResumeSession(sessionID); err != nil {
		t.Fatalf("[%s] resume: %v", chain, err)
	}
	// 冷加载内容落地等待（重启后无驻留，ResumeSession 后台装载）。
	deadline := time.Now().Add(8 * time.Second)
	for {
		snap := harnessRestarted.app.Snapshot()
		if snap.Session.ID == sessionID && snap.Session.Status != "restoring" &&
			strings.Contains(conversationText(snap.Conversation), fmt.Sprintf("round-%02d", settledRounds-1)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("[%s] cold resume did not land: %+v", chain, snap)
		}
		time.Sleep(10 * time.Millisecond)
	}
	metrics.restartResume = time.Since(resumeStart)
	if err := harnessRestarted.app.Submit(ctx, nextPrompt); err != nil {
		t.Fatalf("[%s] restart submit: %v", chain, err)
	}
	if err := harnessRestarted.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("[%s] restart idle: %v", chain, err)
	}
	restartRecords := restartRecorder.snapshot()
	if len(restartRecords) != 1 {
		t.Fatalf("[%s] restart requests=%d want 1", chain, len(restartRecords))
	}
	metrics.restartRequest = restartRecords[0]
	metrics.restartMatches = len(compareWirePrefix(metrics.restartRequest, metrics.continuedReq)) == 0
	harnessRestarted.app.Shutdown()

	// 冷恢复耗时多次采样取平均（同一 store 只读重开，不追加轮次）。
	totalResume := metrics.restartResume
	for repeat := 1; repeat < resumeRepeats; repeat++ {
		repeatHarness := newFullChainHarness(t, restartAccounts, restartStore, 10*time.Second)
		start := time.Now()
		if err := repeatHarness.app.ResumeSession(sessionID); err != nil {
			t.Fatalf("[%s] repeat resume: %v", chain, err)
		}
		deadline := time.Now().Add(8 * time.Second)
		for {
			snap := repeatHarness.app.Snapshot()
			if snap.Session.ID == sessionID && snap.Session.Status != "restoring" &&
				strings.Contains(conversationText(snap.Conversation), fmt.Sprintf("round-%02d", settledRounds-1)) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("[%s] repeat cold resume did not land: %+v", chain, snap)
			}
			time.Sleep(10 * time.Millisecond)
		}
		totalResume += time.Since(start)
		repeatHarness.app.Shutdown()
	}
	metrics.restartResume = totalResume / time.Duration(resumeRepeats)

	metrics.storeFiles, metrics.storeBytes, metrics.byCategory, metrics.generationDirs, metrics.v8Sessions = abStoreMetrics(t, restartStore)
	return metrics
}

func abStoreMetrics(t *testing.T, root string) (files int, totalBytes int64, byCategory map[string]int64, generationDirs, v8Sessions int) {
	t.Helper()
	byCategory = make(map[string]int64)
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), "generation-") {
				generationDirs++
			}
			if strings.HasPrefix(entry.Name(), "session-") {
				if _, statErr := os.Stat(filepath.Join(path, "metadata", "guide.json")); statErr == nil {
					v8Sessions++
				}
			}
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		files++
		totalBytes += info.Size()
		name := entry.Name()
		category := "other"
		switch {
		case strings.HasPrefix(name, "message_"):
			category = "v8-message"
		case strings.HasPrefix(name, "event_"):
			category = "v8-event"
		case name == "guide.json" || name == "message.json" || name == "event.json" ||
			name == "compact.json" || name == "toolresult.json":
			category = "v8-metadata-head"
		case name == "transcript.log":
			category = "transcript.log"
		case name == "rollout.jsonl":
			category = "rollout.jsonl"
		case strings.HasPrefix(name, "history."):
			category = "legacy-history-shard"
		case name == "manifest.json":
			category = "manifest.json"
		case name == "history.json":
			category = "v8-history-cache"
		case name == "state.json":
			category = "state.json"
		case name == "context.json":
			category = "context.json"
		}
		byCategory[category] += info.Size()
		return nil
	})
	return
}

// TestABSessionChainSmokeLegacyVsV8 对比旧/新链路会话冒烟。
func TestABSessionChainSmokeLegacyVsV8(t *testing.T) {
	settledRounds := 6
	legacy := abChainScenario(t, "legacy", settledRounds, 3)
	v8 := abChainScenario(t, "v8", settledRounds, 3)

	// 链路内一致性：重启后首请求 == 不重启继续。
	if !legacy.restartMatches {
		t.Fatalf("legacy 链路重启前缀不一致:\n%s", strings.Join(compareWirePrefix(legacy.restartRequest, legacy.continuedReq), "\n"))
	}
	if !v8.restartMatches {
		t.Fatalf("v8 链路重启前缀不一致:\n%s", strings.Join(compareWirePrefix(v8.restartRequest, v8.continuedReq), "\n"))
	}
	// 跨链路一致性：两条链路同一场景结果相同。
	if mismatches := compareWirePrefix(v8.restartRequest, legacy.restartRequest); len(mismatches) > 0 {
		t.Fatalf("v8 与 legacy 重启请求不一致:\n%s", strings.Join(mismatches, "\n"))
	}
	if mismatches := compareWirePrefix(v8.continuedReq, legacy.continuedReq); len(mismatches) > 0 {
		t.Fatalf("v8 与 legacy 继续运行请求不一致:\n%s", strings.Join(mismatches, "\n"))
	}

	reportAB(t, legacy, v8)
}

func reportAB(t *testing.T, legacy, v8 abChainMetrics) {
	t.Helper()
	rows := []struct {
		metric string
		legacy string
		v8     string
	}{
		{"rounds", fmt.Sprint(legacy.rounds), fmt.Sprint(v8.rounds)},
		{"rounds_wall(6+1轮)", legacy.roundsWall.String(), v8.roundsWall.String()},
		{"cold_resume", legacy.restartResume.String(), v8.restartResume.String()},
		{"restart_messages", fmt.Sprint(len(legacy.restartRequest)), fmt.Sprint(len(v8.restartRequest))},
		{"store_files", fmt.Sprint(legacy.storeFiles), fmt.Sprint(v8.storeFiles)},
		{"store_bytes", fmt.Sprint(legacy.storeBytes), fmt.Sprint(v8.storeBytes)},
		{"generation_dirs", fmt.Sprint(legacy.generationDirs), fmt.Sprint(v8.generationDirs)},
		{"v8_session_dirs", fmt.Sprint(legacy.v8Sessions), fmt.Sprint(v8.v8Sessions)},
	}
	t.Logf("\n=== AB 会话链路对比（%d 轮 + 重启恢复 + 1 轮）===", legacy.rounds)
	for _, row := range rows {
		t.Logf("%-18s legacy=%-16s v8=%-16s", row.metric, row.legacy, row.v8)
	}
	t.Log("--- 字节分布（热点） ---")
	categories := make(map[string]struct{})
	for category := range legacy.byCategory {
		categories[category] = struct{}{}
	}
	for category := range v8.byCategory {
		categories[category] = struct{}{}
	}
	names := make([]string, 0, len(categories))
	for category := range categories {
		names = append(names, category)
	}
	sort.Strings(names)
	for _, category := range names {
		t.Logf("%-22s legacy=%-10d v8=%-10d", category, legacy.byCategory[category], v8.byCategory[category])
	}
}
