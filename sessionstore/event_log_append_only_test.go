package sessionstore

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
)

// 本文件钉住执行事实事件库的「真·追加」契约（锁面审计 §2.9）：
//
//   - 文件是**逐行日志**（每行一条事实），不是整份 JSON 数组；
//   - 追加只做 O(1) 尾写：旧字节原样保留，新事实贴到末尾；
//   - 旧形态（整份数组）与 v1 遗留（generation 内 events.json）在首次追加时迁移；
//   - 崩溃截断的半条尾记录读时丢弃、写时自愈；
//   - 同 Seq 重复追加幂等（后写者胜，与旧「读整份 → merge」口径一致）。
//
// 前三条是**红→绿牙**：把 AppendFrameworkEvent 换回「读整份 → merge → 重写」
// 的实现，TestEventLogAppendsLinesWithoutRewriting 立刻转红（文件不是逐行、
// 前缀被改掉）。

// eventLogFixture 打开一个 JSON v8 后端并返回具体实现（用例要取会话目录路径）。
func eventLogFixture(t *testing.T) (*jsonRepository, Key) {
	t.Helper()
	opened, err := Open(context.Background(), Config{Backend: BackendJSON, Path: filepath.Join(t.TempDir(), "json")})
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	repository, ok := opened.(*jsonRepository)
	if !ok {
		t.Fatalf("Open returned %T, want *jsonRepository", opened)
	}
	return repository, Key{ProjectID: "project", SessionID: "session"}
}

func eventLogPathFor(repository *jsonRepository, key Key) string {
	return filepath.Join(repository.sessionDir(key), frameworkEventLogFile)
}

func readEventLogBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read event log %s: %v", path, err)
	}
	return data
}

func eventLogTail(data []byte) string {
	if len(data) > 120 {
		data = data[len(data)-120:]
	}
	return string(data)
}

// assertEventLogIsLineLog 断言事件库是逐行日志，并返回行数。
// 判据：不以 '[' 开头（不是整份数组），且每一行都能独立解成一条事实。
func assertEventLogIsLineLog(t *testing.T, path string) int {
	t.Helper()
	data := readEventLogBytes(t, path)
	if len(bytes.TrimSpace(data)) == 0 {
		t.Fatalf("event log %s is empty", path)
	}
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("[")) {
		t.Fatalf("event log is a whole-file JSON array, not an append-only line log: %q", eventLogTail(data))
	}
	lines := 0
	for index, raw := range bytes.Split(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var entry EventLogEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			t.Fatalf("event log line %d is not a single entry: %v\n%q", index+1, err, raw)
		}
		lines++
	}
	return lines
}

func assertEventSeqs(t *testing.T, entries []EventLogEntry, want ...uint64) {
	t.Helper()
	if len(entries) != len(want) {
		t.Fatalf("event log length = %d, want %d (%#v)", len(entries), len(want), entries)
	}
	for index, seq := range want {
		if entries[index].Seq != seq {
			t.Fatalf("event %d seq = %d, want %d (%#v)", index, entries[index].Seq, seq, entries)
		}
	}
}

// TestEventLogAppendsLinesWithoutRewriting 是 §2.9 的主判据：追加不得重写整份
// 文件（临界区长度与事件库体积解耦），落盘形态必须是逐行日志。
func TestEventLogAppendsLinesWithoutRewriting(t *testing.T) {
	repository, key := eventLogFixture(t)
	ctx := context.Background()
	path := eventLogPathFor(repository, key)

	for seq := uint64(1); seq <= 3; seq++ {
		if err := repository.AppendFrameworkEvent(ctx, key, eventLogEntry(seq, frameworkevent.StatusRunning)); err != nil {
			t.Fatalf("append seq %d: %v", seq, err)
		}
	}
	if lines := assertEventLogIsLineLog(t, path); lines != 3 {
		t.Fatalf("event log has %d lines, want 3", lines)
	}
	before := readEventLogBytes(t, path)

	if err := repository.AppendFrameworkEvent(ctx, key, eventLogEntry(4, frameworkevent.StatusCompleted)); err != nil {
		t.Fatalf("append seq 4: %v", err)
	}
	after := readEventLogBytes(t, path)

	// 纯追加：旧字节是前缀（旧实现整文件重写会把尾部 ']' 改成 ','，前缀断言立刻红）。
	if !bytes.HasPrefix(after, before) {
		t.Fatalf("event log was rewritten instead of appended:\n before tail %q\n after tail %q",
			eventLogTail(before), eventLogTail(after))
	}
	if len(after) <= len(before) {
		t.Fatalf("append added no bytes: before %d, after %d", len(before), len(after))
	}
	if bytes.HasSuffix(after, []byte("]")) {
		t.Fatalf("event log ends with a JSON array terminator: %q", eventLogTail(after))
	}
	if lines := assertEventLogIsLineLog(t, path); lines != 4 {
		t.Fatalf("event log has %d lines, want 4", lines)
	}

	entries, err := repository.ReadFrameworkEvents(ctx, key)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	assertEventSeqs(t, entries, 1, 2, 3, 4)
}

// TestEventLogMigratesLegacyArrayFileOnFirstAppend 覆盖线上存量形态（整份 JSON
// 数组）：未追加时按旧格式读得到，首次追加迁移成逐行日志且旧事实不丢。
func TestEventLogMigratesLegacyArrayFileOnFirstAppend(t *testing.T) {
	repository, key := eventLogFixture(t)
	ctx := context.Background()
	path := eventLogPathFor(repository, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := []EventLogEntry{
		eventLogEntry(1, frameworkevent.StatusRunning),
		eventLogEntry(2, frameworkevent.StatusCompleted),
	}
	writeLegacyEventLogArray(t, path, legacy)

	entries, err := repository.ReadFrameworkEvents(ctx, key)
	if err != nil {
		t.Fatalf("read legacy array before any append: %v", err)
	}
	assertEventSeqs(t, entries, 1, 2)

	if err := repository.AppendFrameworkEvent(ctx, key, eventLogEntry(3, frameworkevent.StatusCompleted)); err != nil {
		t.Fatalf("append after legacy array: %v", err)
	}
	if lines := assertEventLogIsLineLog(t, path); lines != 3 {
		t.Fatalf("migrated event log has %d lines, want 3", lines)
	}
	entries, err = repository.ReadFrameworkEvents(ctx, key)
	if err != nil {
		t.Fatalf("read back after migration: %v", err)
	}
	assertEventSeqs(t, entries, 1, 2, 3)
}

// TestEventLogMigratesLegacyGenerationFilesOnFirstAppend 覆盖 v1 布局遗留
// （generation-N/events.json 与会话根 events.json）：首次追加仍按旧口径迁移合并。
func TestEventLogMigratesLegacyGenerationFilesOnFirstAppend(t *testing.T) {
	repository, key := eventLogFixture(t)
	ctx := context.Background()
	directory := repository.sessionDir(key)
	generation := filepath.Join(directory, "generation-1")
	if err := os.MkdirAll(generation, 0o700); err != nil {
		t.Fatal(err)
	}
	writeLegacyEventLogArray(t, filepath.Join(generation, frameworkEventLegacyFile),
		[]EventLogEntry{eventLogEntry(1, frameworkevent.StatusRunning)})
	writeLegacyEventLogArray(t, filepath.Join(directory, frameworkEventLegacyFile),
		[]EventLogEntry{eventLogEntry(2, frameworkevent.StatusCompleted)})

	if err := repository.AppendFrameworkEvent(ctx, key, eventLogEntry(3, frameworkevent.StatusCompleted)); err != nil {
		t.Fatalf("append with v1 residue: %v", err)
	}
	entries, err := repository.ReadFrameworkEvents(ctx, key)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	assertEventSeqs(t, entries, 1, 2, 3)
	if lines := assertEventLogIsLineLog(t, eventLogPathFor(repository, key)); lines != 3 {
		t.Fatalf("migrated event log has %d lines, want 3", lines)
	}
}

// TestEventLogDropsTornTailAndHealsOnNextAppend 覆盖「追加写被崩溃截断」：
// 半条尾记录读时丢弃（不报损坏），下次追加把它截掉，之后日志仍是合法逐行日志。
func TestEventLogDropsTornTailAndHealsOnNextAppend(t *testing.T) {
	repository, key := eventLogFixture(t)
	ctx := context.Background()
	path := eventLogPathFor(repository, key)

	for seq := uint64(1); seq <= 2; seq++ {
		if err := repository.AppendFrameworkEvent(ctx, key, eventLogEntry(seq, frameworkevent.StatusRunning)); err != nil {
			t.Fatalf("append seq %d: %v", seq, err)
		}
	}
	// 模拟崩溃：从尾部切掉若干字节，最后一条记录没有换行结尾。
	data := readEventLogBytes(t, path)
	if err := os.Truncate(path, int64(len(data)-12)); err != nil {
		t.Fatal(err)
	}

	entries, err := repository.ReadFrameworkEvents(ctx, key)
	if err != nil {
		t.Fatalf("read with torn tail must not fail: %v", err)
	}
	assertEventSeqs(t, entries, 1)

	if err := repository.AppendFrameworkEvent(ctx, key, eventLogEntry(3, frameworkevent.StatusCompleted)); err != nil {
		t.Fatalf("append after torn tail: %v", err)
	}
	if lines := assertEventLogIsLineLog(t, path); lines != 2 {
		t.Fatalf("healed event log has %d lines, want 2 (seq 1 + seq 3)", lines)
	}
	if healed := readEventLogBytes(t, path); !bytes.HasSuffix(healed, []byte{'\n'}) {
		t.Fatalf("healed event log does not end with a newline: %q", eventLogTail(healed))
	}
	entries, err = repository.ReadFrameworkEvents(ctx, key)
	if err != nil {
		t.Fatalf("read back after healing: %v", err)
	}
	assertEventSeqs(t, entries, 1, 3)
}

// TestEventLogKeepsLastEntryForRepeatedSeq 钉住重试幂等口径：同 Seq 重复追加
// 只留一条，且与旧「读整份 → mergeEventLogEntries」一致为**后写者胜**。
func TestEventLogKeepsLastEntryForRepeatedSeq(t *testing.T) {
	repository, key := eventLogFixture(t)
	ctx := context.Background()

	if err := repository.AppendFrameworkEvent(ctx, key, eventLogEntry(1, frameworkevent.StatusRunning)); err != nil {
		t.Fatal(err)
	}
	retry := eventLogEntry(1, frameworkevent.StatusCompleted)
	if err := repository.AppendFrameworkEvent(ctx, key, retry); err != nil {
		t.Fatal(err)
	}
	entries, err := repository.ReadFrameworkEvents(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	assertEventSeqs(t, entries, 1)
	if string(entries[0].Payload) != string(retry.Payload) {
		t.Fatalf("repeated seq kept the older payload:\n got %s\nwant %s", entries[0].Payload, retry.Payload)
	}
}

// TestEventLogConcurrentAppendsKeepEveryEntry 覆盖并发追加：每一条都要完整落盘
// （-race 下同时验证追加路径不引入数据竞争），且读回按 Seq 升序。
func TestEventLogConcurrentAppendsKeepEveryEntry(t *testing.T) {
	repository, key := eventLogFixture(t)
	ctx := context.Background()
	path := eventLogPathFor(repository, key)

	const writers, perWriter = 6, 20
	var wait sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		wait.Add(1)
		go func(writer int) {
			defer wait.Done()
			for index := 0; index < perWriter; index++ {
				seq := uint64(writer*perWriter + index + 1)
				if err := repository.AppendFrameworkEvent(ctx, key, eventLogEntry(seq, frameworkevent.StatusRunning)); err != nil {
					t.Errorf("append seq %d: %v", seq, err)
					return
				}
			}
		}(writer)
	}
	wait.Wait()

	if lines := assertEventLogIsLineLog(t, path); lines != writers*perWriter {
		t.Fatalf("event log has %d lines, want %d", lines, writers*perWriter)
	}
	entries, err := repository.ReadFrameworkEvents(ctx, key)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(entries) != writers*perWriter {
		t.Fatalf("event log length = %d, want %d", len(entries), writers*perWriter)
	}
	for index := 1; index < len(entries); index++ {
		if entries[index-1].Seq >= entries[index].Seq {
			t.Fatalf("event log is not sorted by seq: %d then %d", entries[index-1].Seq, entries[index].Seq)
		}
	}
}

// writeLegacyEventLogArray 手写旧形态（整份 JSON 数组）到盘上。
func writeLegacyEventLogArray(t *testing.T, path string, entries []EventLogEntry) {
	t.Helper()
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
