//go:build lockprobe

package sessionstore

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

// TestProbeCommitVsFullRead 复现"写者在等读者"：message 通道读写共用同一把
// 独占 sync.Mutex，全量读在锁内解码全部分片，提交必须排队；同时量化
// metadata head 的写成本（整份重写 + rename，随分片数增长）。
func TestProbeCommitVsFullRead(t *testing.T) {
	const (
		projectID = "probe-project"
		sessionID = "probe-sess"
		rows      = 1500
		batch     = 100
		payload   = 8 * 1024
	)
	root := t.TempDir()
	router, err := NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Close() })

	pad := strings.Repeat("x", payload)
	for start := 0; start < rows; start += batch {
		events := make([]Event, 0, batch)
		for index := start; index < start+batch; index++ {
			events = append(events, Event{
				Seq: uint64(index + 1), Role: "user",
				Content: fmt.Sprintf("row-%d-%s", index, pad), CreatedAt: time.Now().UTC(),
			})
		}
		if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{Events: events}); err != nil {
			t.Fatal(err)
		}
	}

	// head 体积随分片数增长：每次 head 发布都是整份重写（publishModuleHead
	// 全量 Marshal + writeAtomic rename），因此 head 写成本随分片数增长。
	headPath := ""
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == "message.json" {
			headPath = path
			return fs.SkipAll
		}
		return nil
	})
	if headPath != "" {
		if info, statErr := os.Stat(headPath); statErr == nil {
			t.Logf("message head 体积=%d 字节（%d 行 / 每片 %d 行）", info.Size(), rows, batch)
		}
	}

	commit := func(seq uint64, content string) time.Duration {
		begin := time.Now()
		if err := router.SaveCommitWorkspace(projectID, sessionID, Commit{Events: []Event{
			{Seq: seq, Role: "user", Content: content, CreatedAt: time.Now().UTC()},
		}}); err != nil {
			t.Fatal(err)
		}
		return time.Since(begin)
	}

	seq := uint64(rows)
	samples := make([]time.Duration, 3)
	for index := range samples {
		seq++
		samples[index] = commit(seq, "baseline")
	}
	slices.Sort(samples)
	baseline := samples[len(samples)/2]

	readBegin := time.Now()
	readerDone := make(chan error, 1)
	go func() {
		_, _, readErr := router.LoadRangeWorkspace(projectID, sessionID, 0, 0)
		readerDone <- readErr
	}()
	time.Sleep(5 * time.Millisecond)
	seq++
	contended := commit(seq, "contended")
	readSpent := time.Since(readBegin)
	if readErr := <-readerDone; readErr != nil {
		t.Fatal(readErr)
	}
	t.Logf("无竞争中位=%v 全量读并发下提交=%v 额外等待=%v 全量读耗时=%v",
		baseline, contended, contended-baseline, readSpent)

	titleBegin := time.Now()
	for index := 0; index < 20; index++ {
		if _, err := router.SetSessionTitleWorkspace(projectID, sessionID, fmt.Sprintf("t-%d", index)); err != nil {
			t.Logf("set title %d: %v", index, err)
			break
		}
	}
	t.Logf("20 次 head 写穿（整份重写 metadata/message.json + rename）总耗时=%v", time.Since(titleBegin))
}
