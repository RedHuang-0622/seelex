package main

// 新链路会话冒烟与基准（2026-09-09，旧链路已删除后）。
//
// 场景与先前 AB 的 v8 链路一致：6 轮快速请求 → 进程重启 → ResumeSession →
// 提交下一轮；断言"重启后首请求 == 不重启继续运行"逐条一致，并采集：
//   - 轮次总耗时与冷恢复耗时（3 次采样平均）；
//   - store 文件数/字节/目录数与按类别字节分布（读写热点）；
//   - 可见会话消息数与重启请求消息数。
//
// 运行：
//   go test . -run TestSessionChainSmokeAndMetricsNew -count=1 -v -timeout=600s

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
)

// chainRunMetrics 是一次新链路会话冒烟的实测指标。
type chainRunMetrics struct {
	Rounds          int              `json:"rounds"`
	RoundsWall      time.Duration    `json:"rounds_wall"`
	ColdResumeAvg   time.Duration    `json:"cold_resume_avg"`
	RestartMessages int              `json:"restart_messages"`
	StoreFiles      int              `json:"store_files"`
	StoreBytes      int64            `json:"store_bytes"`
	GenerationDirs  int              `json:"generation_dirs"`
	LayoutSessions  int              `json:"layout_sessions"`
	BytesByCategory map[string]int64 `json:"bytes_by_category"`
	RestartMatches  bool             `json:"restart_matches_continue"`
	FinishedAt      string           `json:"finished_at"`
	GoVersion       string           `json:"go_version"`
	Platform        string           `json:"platform"`
}

// TestSessionChainSmokeAndMetricsNew 新链路会话冒烟 + 指标采集。
func TestSessionChainSmokeAndMetricsNew(t *testing.T) {
	const settledRounds = 6
	const nextPrompt = "round-next"
	metrics := chainRunMetrics{
		Rounds:          settledRounds,
		BytesByCategory: make(map[string]int64),
		FinishedAt:      time.Now().UTC().Format(time.RFC3339),
		GoVersion:       runtime.Version(),
		Platform:        runtime.GOOS + "/" + runtime.GOARCH,
	}

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
		t.Fatalf("continue submit: %v", err)
	}
	if err := harnessContinue.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("continue idle: %v", err)
	}
	metrics.RoundsWall = time.Since(roundStart)
	continueRecords := continueRecorder.snapshot()
	if len(continueRecords) != settledRounds+1 {
		t.Fatalf("continue requests=%d want %d", len(continueRecords), settledRounds+1)
	}
	continued := continueRecords[len(continueRecords)-1]
	harnessContinue.app.Shutdown()

	// 对照 B：同样轮数 → 重启 → ResumeSession → 提交下一轮。
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
		t.Fatalf("resume: %v", err)
	}
	waitColdContent := func(app *application.Service, sid string) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for {
			snap := app.Snapshot()
			if snap.Session.ID == sid && snap.Session.Status != "restoring" &&
				strings.Contains(conversationText(snap.Conversation), fmt.Sprintf("round-%02d", settledRounds-1)) {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("cold resume did not land: %+v", snap)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitColdContent(harnessRestarted.app, sessionID)
	firstResume := time.Since(resumeStart)
	if err := harnessRestarted.app.Submit(ctx, nextPrompt); err != nil {
		t.Fatalf("restart submit: %v", err)
	}
	if err := harnessRestarted.app.WaitForIdle(ctx); err != nil {
		t.Fatalf("restart idle: %v", err)
	}
	restartRecords := restartRecorder.snapshot()
	if len(restartRecords) != 1 {
		t.Fatalf("restart requests=%d want 1", len(restartRecords))
	}
	restarted := restartRecords[0]
	metrics.RestartMessages = len(restarted)
	metrics.RestartMatches = len(compareWirePrefix(restarted, continued)) == 0
	if !metrics.RestartMatches {
		t.Fatalf("重启后首请求与不重启继续运行不一致:\n%s", strings.Join(compareWirePrefix(restarted, continued), "\n"))
	}
	harnessRestarted.app.Shutdown()

	// 冷恢复耗时 3 次采样取平均。
	totalResume := firstResume
	for repeat := 1; repeat < 3; repeat++ {
		repeatHarness := newFullChainHarness(t, restartAccounts, restartStore, 10*time.Second)
		start := time.Now()
		if err := repeatHarness.app.ResumeSession(sessionID); err != nil {
			t.Fatalf("repeat resume: %v", err)
		}
		waitColdContent(repeatHarness.app, sessionID)
		totalResume += time.Since(start)
		repeatHarness.app.Shutdown()
	}
	metrics.ColdResumeAvg = totalResume / 3

	metrics.StoreFiles, metrics.StoreBytes, metrics.BytesByCategory, metrics.GenerationDirs, metrics.LayoutSessions = chainStoreMetrics(t, restartStore)

	t.Logf("新链路会话冒烟通过：重启后首请求 == 不重启继续运行（消息数=%d）", metrics.RestartMessages)
	rows := []struct {
		name  string
		value string
	}{
		{"rounds", fmt.Sprint(metrics.Rounds)},
		{"rounds_wall(6+1轮)", metrics.RoundsWall.String()},
		{"cold_resume_avg(3次)", metrics.ColdResumeAvg.String()},
		{"restart_messages", fmt.Sprint(metrics.RestartMessages)},
		{"store_files", fmt.Sprint(metrics.StoreFiles)},
		{"store_bytes", fmt.Sprint(metrics.StoreBytes)},
		{"layout_sessions", fmt.Sprint(metrics.LayoutSessions)},
	}
	for _, row := range rows {
		t.Logf("%-24s %s", row.name, row.value)
	}
	names := make([]string, 0, len(metrics.BytesByCategory))
	for name := range metrics.BytesByCategory {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Logf("bytes[%-22s] %d", name, metrics.BytesByCategory[name])
	}
	data, _ := json.MarshalIndent(metrics, "", "  ")
	t.Logf("metrics_json=%s", data)
}

func chainStoreMetrics(t *testing.T, root string) (int, int64, map[string]int64, int, int) {
	t.Helper()
	files := 0
	var total int64
	byCategory := make(map[string]int64)
	generationDirs := 0
	layoutSessions := 0
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
					layoutSessions++
				}
			}
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		files++
		total += info.Size()
		name := entry.Name()
		category := "other"
		switch {
		case strings.HasPrefix(name, "message_"):
			category = "message-rows"
		case strings.HasPrefix(name, "event_"):
			category = "structural-events"
		case name == "guide.json" || name == "message.json" || name == "event.json" ||
			name == "compact.json" || name == "toolresult.json":
			category = "metadata-heads"
		case name == "history.json":
			category = "history-cache"
		case name == "state.json":
			category = "state.json"
		case name == "context.json":
			category = "context.json"
		}
		byCategory[category] += info.Size()
		return nil
	})
	return files, total, byCategory, generationDirs, layoutSessions
}
