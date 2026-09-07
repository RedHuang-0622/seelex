package sessionstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestJSONTranscriptLogAppendOnly 验证 JSON 后端把 transcript 事件按增量追加到
// 每会话 transcript.log，而不是随 generation rollover 整代重写事件分片；重复
// 提交同一事件列表幂等（不产生重复行）。
func TestJSONTranscriptLogAppendOnly(t *testing.T) {
	repository, err := newJSONRepository(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	key := Key{ProjectID: "project", SessionID: "session"}
	now := time.Now().UTC()
	events := func(seq uint64, role string) Event {
		return Event{Seq: seq, Role: role, Kind: EventKindLLM, Content: role, CreatedAt: now}
	}

	if err := repository.WriteCommit(context.Background(), key, Commit{
		ProviderHistory: nil,
		Events:          []Event{events(1, "user")},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteCommit(context.Background(), key, Commit{
		ProviderHistory: nil,
		Events:          []Event{events(1, "user"), events(2, "assistant")},
	}); err != nil {
		t.Fatal(err)
	}
	// 幂等重试：同一批事件再次提交不应产生重复日志行。
	if err := repository.WriteCommit(context.Background(), key, Commit{
		ProviderHistory: nil,
		Events:          []Event{events(1, "user"), events(2, "assistant")},
	}); err != nil {
		t.Fatal(err)
	}

	directory := repository.sessionDir(key)
	logPath := filepath.Join(directory, transcriptEventLogFile)
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			lines++
		}
	}
	if lines != 2 {
		t.Fatalf("transcript.log lines = %d, want 2 (append-only, no duplicates)", lines)
	}

	// 新布局不再把事件作为 generation 内的分片文件写入。
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "generation-") {
			continue
		}
		generationEntries, err := os.ReadDir(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, generationEntry := range generationEntries {
			if strings.HasPrefix(generationEntry.Name(), "events.") {
				t.Fatalf("generation %s still contains event shard %s", entry.Name(), generationEntry.Name())
			}
		}
	}

	eventsFromLog, err := repository.ReadEventTail(context.Background(), key, 1<<20, 16)
	if err != nil || len(eventsFromLog) != 2 {
		t.Fatalf("ReadEventTail len=%d err=%v, want 2", len(eventsFromLog), err)
	}
}
