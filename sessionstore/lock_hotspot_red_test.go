//go:build redprobe

package sessionstore

// 锁热点红灯验收测试（2026-09-09 分析落盘）。
//
// 这两条测试是修复行为的验收：
//  1. v8 JSON 布局不再产生 history.json（D9/S11：H1 的持柄场景随文件退役
//     消除）——回归断言绿；
//  2. 一次提交不能因为"有人正在全量读历史"而等待数百毫秒（H2/H3 专项，
//     当前仍红）。
//
// 运行：
//   go test ./sessionstore -tags redprobe -count=1 -timeout=600s \
//     -run 'TestRedCommitDiesWhenHistoryFileIsOpen|TestRedCommitWaitsBehindFullHistoryRead' -v
//
// 第 2 条修复完成后本文件应去掉 build tag 转为常规测试。

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// findFile 在压测目录树里定位同名文件（不依赖内部路径拼接函数）。
func findFile(t *testing.T, root, name string) string {
	t.Helper()
	var found string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && filepath.Base(path) == name {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && !errorsIsSkipAll(err) {
		t.Fatal(err)
	}
	if found == "" {
		t.Fatalf("%s not found under %s", name, root)
	}
	return found
}

func errorsIsSkipAll(err error) bool {
	return err != nil && strings.Contains(err.Error(), "skip all")
}

func newRedProfileRouter(t *testing.T, root string) *Router {
	t.Helper()
	router, err := NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router
}

// TestProbeNoHistoryCacheWritten 断言 v8 JSON 布局的完整提交不再产生
// history.json（S11：provider 缓存文件已退役；正文事实源 = message 事件行）。
// 回归面：T-DP-02 的局部断言。
func TestProbeNoHistoryCacheWritten(t *testing.T) {
	root := t.TempDir()
	router := newRedProfileRouter(t, root)
	const projectID, sessionID = "red-project", "sess-history"

	if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{
		Events: []Event{{Role: "user", Content: "seed", MessageID: "seed"}},
	}); err != nil {
		t.Fatal(err)
	}
	found := ""
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && filepath.Base(path) == "history.json" {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && !errorsIsSkipAll(err) {
		t.Fatal(err)
	}
	if found != "" {
		t.Fatalf("history.json 复活: %s", found)
	}
}

// TestRedCommitWaitsBehindFullHistoryRead 断言"提交不被一次全量历史读长时间独占"。
//
// 现状：message 通道的读与写共用同一把独占 sync.Mutex，而全量读
// （LoadRangeWorkspace(0,0) → readAllRows）会在锁内遍历解码全部分片。
// pprof 里 66% 的锁等待其实是**写者**在等读者，不是读者在等写者。
//
// 判据用"相对同轮无竞争基线的增量"，不用绝对耗时：绝对提交耗时由分片数量
// 决定（读侧修好后依然存在的写侧成本），只有增量才是读者独占造成的等待。
func TestRedCommitWaitsBehindFullHistoryRead(t *testing.T) {
	const (
		projectID   = "red-project"
		sessionID   = "sess-big"
		rows        = 1500
		batch       = 100
		payload     = 8 * 1024
		extraBudget = 30 * time.Millisecond
		absoluteCap = 500 * time.Millisecond
	)
	root := t.TempDir()
	router := newRedProfileRouter(t, root)

	pad := strings.Repeat("x", payload)
	commit := func(seq uint64, content string) time.Duration {
		begin := time.Now()
		err := router.SaveCommitWorkspace(projectID, sessionID, Commit{Events: []Event{
			{Seq: seq, Role: "user", Content: content, CreatedAt: time.Now().UTC(), TokenCount: 1},
		}})
		if err != nil {
			t.Fatal(err)
		}
		return time.Since(begin)
	}

	for start := 0; start < rows; start += batch {
		events := make([]Event, 0, batch)
		for index := start; index < start+batch; index++ {
			events = append(events, Event{
				Seq: uint64(index + 1), Role: "user",
				Content:   fmt.Sprintf("row-%d-%s", index, pad),
				CreatedAt: time.Now().UTC(), TokenCount: payload / 4,
			})
		}
		if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{Events: events}); err != nil {
			t.Fatal(err)
		}
	}

	// 同轮基线：3 次无竞争提交取中位数，抵消机器/磁盘漂移。
	seq := uint64(rows)
	samples := make([]time.Duration, 3)
	for index := range samples {
		seq++
		samples[index] = commit(seq, "baseline")
	}
	slices.Sort(samples)
	baseline := samples[len(samples)/2]

	readerDone := make(chan error, 1)
	go func() {
		_, _, err := router.LoadRangeWorkspace(projectID, sessionID, 0, 0)
		readerDone <- err
	}()
	time.Sleep(5 * time.Millisecond) // 让读者先进临界区

	seq++
	latency := commit(seq, "contended")
	if err := <-readerDone; err != nil {
		t.Fatal(err)
	}

	extra := latency - baseline
	t.Logf("无竞争中位=%v，全量读并发下提交=%v，读者造成的额外等待=%v（预算 %v）",
		baseline, latency, extra, extraBudget)
	if latency > absoluteCap {
		t.Fatalf("RED: 提交绝对耗时 %v 超过兜底上限 %v", latency, absoluteCap)
	}
	if extra > extraBudget {
		t.Fatalf("RED: 一次全量历史读让提交多等了 %v（基线 %v），超过预算 %v", extra, baseline, extraBudget)
	}
}
