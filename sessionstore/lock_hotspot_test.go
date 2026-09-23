package sessionstore

// 锁热点验收测试（2026-09-09 分析落盘；2026-09-10 由 redprobe 文件转常规）。
//
//  1. TestProbeNoHistoryCacheWritten：v8 JSON 布局不再产生 history.json
//     （D9/S11：H1 的持柄场景随文件退役消除）；
//  2. TestCommitNotBlockedByFullHistoryRead：一次全量历史读不得让提交多等
//     超过预算（H2/H3：pprof 里 66% 的锁等待是**写者在等读者**）。
//
// 两条都是常规测试（不带 build tag），随 `go test ./sessionstore` 一起跑。
// H2 的结构性判据在 message_read_lock_test.go
// （TestMessageReadDecodeOutsideWriterLock）——本文件的第 2 条是同一根因的
// 时序面验收。

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// errorsIsSkipAll 兼容 filepath.SkipAll 被包成 error 返回的调用姿势。
func errorsIsSkipAll(err error) bool {
	return err != nil && strings.Contains(err.Error(), "skip all")
}

// newLockProfileRouter 打开一个指向 tempdir 的 router（锁热点测试专用）。
func newLockProfileRouter(t *testing.T, root string) *Router {
	t.Helper()
	router, err := NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router
}

// signalFirstPublishedRead 把"读者已走到解码边界"变成确定性前提：返回的
// entered 在第一个读者即将解码时关闭（不阻塞读者）。
//
// 旧版本用 `time.Sleep(5ms)` 赌读者先进入临界区。实测这个赌注会输：读者的
// 前置工作（取 head、枚举分片）就够把提交放过去，于是即使把解码放回写者锁内，
// 只靠 sleep 的版本依然"绿"——假绿。改成事件同步后，读者是否持锁不再取决于
// 调度运气：读者已在解码时发起提交，独占读必然把提交挡在后面。
func signalFirstPublishedRead(t *testing.T) <-chan struct{} {
	t.Helper()
	entered := make(chan struct{})
	var once sync.Once
	publishedReadHook = func() { once.Do(func() { close(entered) }) }
	t.Cleanup(func() { publishedReadHook = nil })
	return entered
}

// TestProbeNoHistoryCacheWritten 断言 v8 JSON 布局的完整提交不再产生
// history.json（S11：provider 缓存文件已退役；正文事实源 = message 事件行）。
// 回归面：T-DP-02 的局部断言。
func TestProbeNoHistoryCacheWritten(t *testing.T) {
	root := t.TempDir()
	router := newLockProfileRouter(t, root)
	const projectID, sessionID = "lock-project", "sess-history"

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

// TestCommitNotBlockedByFullHistoryRead 断言"提交不被一次全量历史读长时间挡住"。
//
// 判据用"相对同轮无竞争基线的增量"，不用绝对耗时：绝对提交耗时由分片数量
// 决定（读侧修好后依然存在的写侧成本），只有增量才是读者独占造成的等待。
func TestCommitNotBlockedByFullHistoryRead(t *testing.T) {
	const (
		projectID   = "lock-project"
		sessionID   = "sess-big"
		rows        = 1500
		batch       = 100
		payload     = 8 * 1024
		extraBudget = 30 * time.Millisecond
		absoluteCap = 500 * time.Millisecond
	)
	root := t.TempDir()
	router := newLockProfileRouter(t, root)

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

	// 读者已在解码（钩子事件），此时发起提交：读者若还占着 messageMu，
	// 提交就必须等它把全量历史解码完。
	entered := signalFirstPublishedRead(t)
	readerDone := make(chan error, 1)
	go func() {
		_, _, err := router.LoadRangeWorkspace(projectID, sessionID, 0, 0)
		readerDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("读者未进入解码：publishedReadHook 未被调用")
	}

	seq++
	latency := commit(seq, "contended")
	if err := <-readerDone; err != nil {
		t.Fatal(err)
	}

	extra := latency - baseline
	t.Logf("无竞争中位=%v，全量读并发下提交=%v，读者造成的额外等待=%v（预算 %v）",
		baseline, latency, extra, extraBudget)
	if latency > absoluteCap {
		t.Fatalf("提交绝对耗时 %v 超过兜底上限 %v", latency, absoluteCap)
	}
	if extra > extraBudget {
		t.Fatalf("RED: 一次全量历史读让提交多等了 %v（基线 %v），超过预算 %v", extra, baseline, extraBudget)
	}
}
