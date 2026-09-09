//go:build redprobe

package sessionstore

// 锁热点红灯验收测试（2026-09-09 分析落盘）。
//
// 这两条测试**当前必定为红**，因为它们断言的是"修复后应当成立"的行为：
//  1. 提交不能因为"有人正在读同一个文件"而失败；
//  2. 一次提交不能因为"有人正在全量读历史"而等待数百毫秒。
//
// 运行：
//   go test ./sessionstore -tags redprobe -count=1 -timeout=600s \
//     -run 'TestRedCommitDiesWhenHistoryFileIsOpen|TestRedCommitWaitsBehindFullHistoryRead' -v
//
// 修复完成后本文件应去掉 build tag 转为常规测试。

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

// TestRedCommitDiesWhenHistoryFileIsOpen 断言"读一个文件不能让写这个文件失败"。
//
// 现状：history.json 由 WriteCommit 用 writeAtomic（tmp + os.Rename）发布，
// 且读写两侧都不持任何模块锁；Windows 上只要目标文件被任何句柄打开（哪怕
// 只读），os.Rename 直接返回 Access is denied，而写侧零重试，于是该错误原样
// 冒泡成"提交失败"。全量 -race 跑已实测命中一次。
func TestRedCommitDiesWhenHistoryFileIsOpen(t *testing.T) {
	root := t.TempDir()
	router := newRedProfileRouter(t, root)
	const projectID, sessionID = "red-project", "sess-history"

	if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{
		ProviderHistory: messages(1, "seed"),
	}); err != nil {
		t.Fatal(err)
	}
	historyPath := findFile(t, root, "history.json")

	// 模拟并发读者：真实读路径 os.ReadFile 同样会打开该文件，这里显式持柄
	// 把那个（真实存在但极短的）窗口放大成确定性复现。
	handle, err := os.Open(historyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()

	err = router.SaveCommitWorkspace(projectID, sessionID, Commit{
		ProviderHistory: messages(2, "reply"),
	})
	if err != nil {
		t.Fatalf("RED: 读者持有句柄期间提交失败: %v", err)
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
