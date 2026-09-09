package sessionstore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func rolloutFixtureEvents(now time.Time) []Event {
	return []Event{
		{Seq: 1, Role: "user", Kind: EventKindUserInput, Content: "q", CreatedAt: now},
		{Seq: 2, Role: "assistant", Kind: EventKindLLM, Content: "a", CreatedAt: now},
	}
}

// TestJSONRolloutAppendOnlyOrdinals 验证 rollout.jsonl：
//   - 首条为 session_meta（Ordinal=1）；
//   - 对话类事件按提交顺序追加、Ordinal 连续；
//   - 重复提交同一批事件幂等（不重复行、Ordinal 不回退）。
func TestJSONRolloutAppendOnlyOrdinals(t *testing.T) {
	repository, err := newLegacyJSONRepository(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	key := Key{ProjectID: "project", SessionID: "session"}
	now := time.Now().UTC()
	first := rolloutFixtureEvents(now)
	if err := repository.WriteCommit(context.Background(), key, Commit{Events: first}); err != nil {
		t.Fatal(err)
	}
	directory := repository.sessionDir(key)
	entries, head, err := repository.readRolloutLocked(directory)
	if err != nil {
		t.Fatal(err)
	}
	if head != 4 || len(entries) != 4 {
		t.Fatalf("after first commit: head=%d entries=%d, want head=4 entries=4", head, len(entries))
	}
	if entries[0].Kind != LogSessionMeta || entries[0].Ordinal != 1 {
		t.Fatalf("first rollout entry = kind=%s ordinal=%d, want session_meta ordinal=1", entries[0].Kind, entries[0].Ordinal)
	}
	if entries[1].Kind != LogUserInput || entries[2].Kind != LogAssistant || entries[3].Kind != LogTokenUsage {
		t.Fatalf("conversation kinds = %s,%s,%s, want user_input,assistant,token_usage", entries[1].Kind, entries[2].Kind, entries[3].Kind)
	}
	for index, entry := range entries[:3] {
		if entry.Ordinal != uint64(index+1) {
			t.Fatalf("ordinal discontinuity: entry[%d].Ordinal=%d", index, entry.Ordinal)
		}
	}

	// 第二提交追加新事件；重复提交幂等。
	second := append(rolloutFixtureEvents(now), Event{Seq: 3, Role: "tool", Kind: EventKindToolOutput, Content: "r", CreatedAt: now})
	if err := repository.WriteCommit(context.Background(), key, Commit{Events: second}); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteCommit(context.Background(), key, Commit{Events: second}); err != nil {
		t.Fatal(err)
	}
	entries, head, err = repository.readRolloutLocked(directory)
	if err != nil {
		t.Fatal(err)
	}
	if head != 6 || len(entries) != 6 {
		t.Fatalf("after idempotent re-commit: head=%d entries=%d, want head=6 entries=6", head, len(entries))
	}
	if entries[4].Kind != LogToolOutput || entries[4].Seq != 3 {
		t.Fatalf("third event = kind=%s seq=%d, want tool_output seq=3", entries[4].Kind, entries[4].Seq)
	}
}

// TestJSONRolloutCrashTailResumes 验证崩溃残尾（未换行半行）被跳过，后续
// 提交从已落盘 head 之后续写 Ordinal（无空洞）。
func TestJSONRolloutCrashTailResumes(t *testing.T) {
	repository, err := newLegacyJSONRepository(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	key := Key{ProjectID: "project", SessionID: "session"}
	now := time.Now().UTC()
	if err := repository.WriteCommit(context.Background(), key, Commit{Events: rolloutFixtureEvents(now)}); err != nil {
		t.Fatal(err)
	}
	directory := repository.sessionDir(key)
	path := filepath.Join(directory, rolloutLogFile)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"ordinal":99,"kind":"` + string(LogToolOutput) + `","payload":`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	entries, head, err := repository.readRolloutLocked(directory)
	if err != nil {
		t.Fatal(err)
	}
	if head != 4 || len(entries) != 4 {
		t.Fatalf("crash tail must be skipped: head=%d entries=%d, want head=4 entries=4", head, len(entries))
	}
	if err := repository.WriteCommit(context.Background(), key, Commit{
		Events: append(rolloutFixtureEvents(now), Event{Seq: 3, Role: "tool", Kind: EventKindToolOutput, Content: "r", CreatedAt: now}),
	}); err != nil {
		t.Fatal(err)
	}
	entries, head, err = repository.readRolloutLocked(directory)
	if err != nil {
		t.Fatal(err)
	}
	if head != 6 || len(entries) != 6 {
		t.Fatalf("resume after crash tail: head=%d entries=%d, want head=6 entries=6", head, len(entries))
	}
	if entries[4].Ordinal != 5 || entries[5].Ordinal != 6 {
		t.Fatalf("resumed entries ordinal=%d,%d, want 5,6 (no gap at crash tail ordinal 99)", entries[4].Ordinal, entries[5].Ordinal)
	}
}

// TestJSONRolloutLifecycleKinds 验证生命周期 kind 单写：
//   - 带 TaskID 的 user_input 前写 request_begin/turn_begin；
//   - 其后写 request_end/turn_end；
//   - state 携带 context_compactions 时写 compacted；
//   - 重复提交幂等（指纹去重）。
func TestJSONRolloutLifecycleKinds(t *testing.T) {
	repository, err := newLegacyJSONRepository(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	key := Key{ProjectID: "project", SessionID: "session-life"}
	now := time.Now().UTC()
	state := []byte(`{"execution":{"task":{"request_id":"req-1","context_compactions":[{"version":2,"reason":"context_budget","messages_before":10,"estimated_tokens":5000,"compacted_at":"2026-09-08T00:00:00Z"}]}}}`)
	commit := Commit{
		Events: []Event{
			{Seq: 1, TaskID: "req-1", Role: "user", Kind: EventKindUserInput, Content: "q", CreatedAt: now},
			{Seq: 2, TaskID: "req-1", Role: "assistant", Kind: EventKindLLM, Content: "a", CreatedAt: now},
		},
		State: state,
	}
	if err := repository.WriteCommit(context.Background(), key, commit); err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteCommit(context.Background(), key, commit); err != nil {
		t.Fatal(err)
	}
	entries, _, err := repository.readRolloutLocked(repository.sessionDir(key))
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]SessionLogKind, 0, len(entries))
	for _, entry := range entries {
		kinds = append(kinds, entry.Kind)
	}
	wantPrefix := []SessionLogKind{
		LogSessionMeta, LogRequestBegin, LogTurnBegin, LogUserInput,
		LogAssistant, LogRequestEnd, LogTurnEnd, LogCompacted, LogTokenUsage,
	}
	if len(kinds) != len(wantPrefix) {
		t.Fatalf("rollout kinds = %v, want %v", kinds, wantPrefix)
	}
	for index := range wantPrefix {
		if kinds[index] != wantPrefix[index] {
			t.Fatalf("rollout kinds = %v, want prefix %v", kinds, wantPrefix)
		}
	}
	// I-LOG-5：压缩标记（compacted）之后日志仍可继续追加与重放（续播）。
	second := Commit{
		Events: []Event{
			{Seq: 3, TaskID: "req-2", Role: "user", Kind: EventKindUserInput, Content: "q2", CreatedAt: now},
		},
		State: state,
	}
	if err := repository.WriteCommit(context.Background(), key, second); err != nil {
		t.Fatal(err)
	}
	entries, _, err = repository.readRolloutLocked(repository.sessionDir(key))
	if err != nil {
		t.Fatal(err)
	}
	compactedAt := -1
	continuationAt := -1
	for index, entry := range entries {
		if entry.Kind == LogCompacted && compactedAt < 0 {
			compactedAt = index
		}
		if entry.Kind == LogUserInput && entry.RequestID == "req-2" && continuationAt < 0 {
			continuationAt = index
		}
	}
	if compactedAt < 0 || continuationAt < 0 || continuationAt <= compactedAt {
		t.Fatalf("I-LOG-5 violated: compacted@%d must precede continuation user@%d", compactedAt, continuationAt)
	}
}

// TestJSONRolloutReplayMatchesEventOrder 验证 rollout 正序重放得到的对话事件
// 与 transcript.log 事件序一致（P2 resume 用 rollout 重建 transcript 的前提）。
func TestJSONRolloutReplayMatchesEventOrder(t *testing.T) {
	repository, err := newLegacyJSONRepository(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	key := Key{ProjectID: "project", SessionID: "session-replay"}
	now := time.Now().UTC()
	events := []Event{
		{Seq: 1, TaskID: "req-1", Role: "user", Kind: EventKindUserInput, Content: "q1", CreatedAt: now},
		{Seq: 2, TaskID: "req-1", Role: "assistant", Kind: EventKindToolCall, ToolCalls: []EventToolCall{{ID: "c1", Name: "get_time", Arguments: `{}`}}, CreatedAt: now},
		{Seq: 3, TaskID: "req-1", Role: "tool", Kind: EventKindToolOutput, ToolCallID: "c1", Name: "get_time", Content: "ok", CreatedAt: now},
		{Seq: 4, TaskID: "req-1", Role: "assistant", Kind: EventKindLLM, Content: "a1", CreatedAt: now},
	}
	commit := Commit{Events: events}
	if err := repository.WriteCommit(context.Background(), key, commit); err != nil {
		t.Fatal(err)
	}
	rollout, err := repository.ReadRollout(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	var replayed []Event
	for _, entry := range rollout {
		switch entry.Kind {
		case LogUserInput, LogInternalUser, LogAssistant, LogToolCall, LogToolOutput:
			var event Event
			if json.Unmarshal(entry.Payload, &event) == nil {
				replayed = append(replayed, event)
			}
		}
	}
	if len(replayed) != len(events) {
		t.Fatalf("replayed events = %d, want %d", len(replayed), len(events))
	}
	for index := range events {
		if replayed[index].Seq != events[index].Seq || replayed[index].Role != events[index].Role {
			t.Fatalf("replayed[%d] = seq=%d role=%s, want seq=%d role=%s",
				index, replayed[index].Seq, replayed[index].Role, events[index].Seq, events[index].Role)
		}
	}
}

// TestJSONRolloutAppendOnlyNoRewrite 验证 I-LOG-3：正常提交只追加，已落盘
// 前缀逐字节不变（无原地改写/重排/删除；崩溃残尾截断是唯一例外路径）。
func TestJSONRolloutAppendOnlyNoRewrite(t *testing.T) {
	repository, err := newLegacyJSONRepository(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	key := Key{ProjectID: "project", SessionID: "session-append"}
	now := time.Now().UTC()
	commitEvents := func(seqs ...uint64) []Event {
		events := make([]Event, 0, len(seqs))
		for _, seq := range seqs {
			role := "assistant"
			if seq%2 == 1 {
				role = "user"
			}
			events = append(events, Event{Seq: seq, Role: role, Kind: EventKindOf(Event{Role: role}), Content: "x", CreatedAt: now})
		}
		return events
	}
	if err := repository.WriteCommit(context.Background(), key, Commit{Events: commitEvents(1)}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repository.sessionDir(key), rolloutLogFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.WriteCommit(context.Background(), key, Commit{Events: commitEvents(1, 2)}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) <= len(before) || string(after[:len(before)]) != string(before) {
		t.Fatalf("I-LOG-3 violated: committed rollout prefix was rewritten")
	}
}
