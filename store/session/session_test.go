package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	frameworkStorage "github.com/RedHuang-0622/Seele/seelectx/storage"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
)

func TestPendingTailClassification(t *testing.T) {
	rows := []protocol.TranscriptEvent{{Seq: 3}, {Seq: 4}}
	if status, reason := ClassifyPendingTail(2, rows, false); status != PendingTailRecoverable || reason != "" {
		t.Fatalf("recoverable=(%q,%q)", status, reason)
	}
	if status, _ := ClassifyPendingTail(3, rows, false); status != PendingTailGap {
		t.Fatalf("gap status=%q", status)
	}
	if status, reason := ClassifyPendingTail(2, rows, true); status != PendingTailGap || !strings.Contains(reason, "重复") {
		t.Fatalf("duplicate=(%q,%q)", status, reason)
	}
	if status, _ := ClassifyPendingTail(2, nil, false); status != PendingTailClean {
		t.Fatalf("clean status=%q", status)
	}
}

func TestShardNames(t *testing.T) {
	if name := MessageShardName(1, 100); name != "message_1_100.jsonl" {
		t.Fatal(name)
	}
	if from, to, ok := ParseMessageShardName("message_1_100.jsonl"); !ok || from != 1 || to != 100 {
		t.Fatalf("message parse=(%d,%d,%v)", from, to, ok)
	}
	if _, _, ok := ParseMessageShardName("message_0_1.jsonl"); ok {
		t.Fatal("zero message from accepted")
	}
	if name := EventShardName(2, 9); name != "event_2_9.jsonl" {
		t.Fatal(name)
	}
	if from, to, ok := ParseEventShardName("event_2_9.jsonl"); !ok || from != 2 || to != 9 {
		t.Fatalf("event parse=(%d,%d,%v)", from, to, ok)
	}
}

func TestAssignRowSeqs(t *testing.T) {
	rows := []protocol.TranscriptEvent{
		{Seq: 0, MessageID: "auto"},
		{Seq: 5, MessageID: "explicit"},
	}
	assigned, err := AssignRowSeqs(rows, 2, "commit-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(assigned) != 2 || assigned[0].Seq != 3 || assigned[1].Seq != 5 ||
		assigned[0].CommitID != "commit-1" || assigned[1].CommitID != "commit-1" {
		t.Fatalf("assigned=%+v", assigned)
	}
	duplicate, err := AssignRowSeqs([]protocol.TranscriptEvent{{Seq: 2}}, 2, "commit")
	if err != nil || len(duplicate) != 0 {
		t.Fatalf("duplicate=(%+v,%v)", duplicate, err)
	}
	if _, err := AssignRowSeqs([]protocol.TranscriptEvent{{Seq: 5}, {Seq: 5}}, 2, "commit"); err == nil {
		t.Fatal("non-increasing explicit seq must fail")
	}
}

func TestMessageShardRouteOnHead(t *testing.T) {
	head := EmptyMessageHead("session")
	head.Shards = []ShardInfo{{Path: "message_1_2.jsonl", FromSeq: 1, ToSeq: 2, Count: 2}}
	head.LastSeq = 2
	if head.SessionID != "session" || head.Shards[0].Path != "message_1_2.jsonl" || head.LastSeq != 2 {
		t.Fatalf("head=%+v", head)
	}
}

func TestStructuralEventIdentity(t *testing.T) {
	events := []StructuralEvent{{
		Kind: StructuralEventFork, AnchorMessageID: "m1", FrameID: "f1",
		Payload: json.RawMessage(`{"reason":"branch"}`),
	}}
	first := StructuralEventsCommitID(events)
	second := StructuralEventsCommitID(events)
	if first == "" || first != second || first[:3] != "ev-" {
		t.Fatalf("commit ids=%q %q", first, second)
	}
	events[0].AnchorMessageID = "m2"
	if next := StructuralEventsCommitID(events); next == first {
		t.Fatal("event identity ignored anchor change")
	}
}

func TestMessageShardReadAppendRepairsTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "message_1_1.jsonl")
	rows := []protocol.TranscriptEvent{{Seq: 1, MessageID: "m1", Content: "one"}}
	if err := AppendMessageRows(path, rows); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"seq":2,"message_id":"torn`); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	read, err := ReadMessageRowsAt(path, 0, nil)
	if err != nil || len(read) != 1 || read[0].MessageID != "m1" {
		t.Fatalf("read=%+v err=%v", read, err)
	}
	if err := AppendMessageRows(path, []protocol.TranscriptEvent{{Seq: 2, MessageID: "m2"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "torn") {
		t.Fatalf("crash tail was not repaired: %s", data)
	}
}

func TestTypedHeadPublish(t *testing.T) {
	layout := storelayout.New(t.TempDir())
	scope := storelayout.Scope{ProjectID: "p", SessionID: "s"}
	message := MessageHead{SessionID: "s", LastSeq: 3}
	if commit, err := PublishMessageHead(layout, scope, "c-message", message, time.Unix(1, 0)); err != nil || commit != "c-message" {
		t.Fatalf("message publish=(%q,%v)", commit, err)
	}
	event := EventHead{SessionID: "s", LastID: 2, Total: 2}
	if commit, err := PublishEventHead(layout, scope, "c-event", event, time.Unix(2, 0)); err != nil || commit != "c-event" {
		t.Fatalf("event publish=(%q,%v)", commit, err)
	}
	messageHead, err := layout.ReadHead(scope, "message")
	if err != nil || messageHead.CommitID != "c-message" {
		t.Fatalf("message head=%+v err=%v", messageHead, err)
	}
	eventHead, err := layout.ReadHead(scope, "event")
	if err != nil || eventHead.CommitID != "c-event" {
		t.Fatalf("event head=%+v err=%v", eventHead, err)
	}
}

func TestApplyPendingTailHead(t *testing.T) {
	head := MessageHead{SessionID: "s", LastSeq: 2, Meta: frameworkStorage.SessionMeta{SessionID: "s"}}
	rows := []protocol.TranscriptEvent{
		{Seq: 3, MessageID: "m3", TokenCount: 2, CommitID: "commit"},
		{Seq: 4, MessageID: "m4", TokenCount: 3, CommitID: "commit"},
	}
	shards := []ShardInfo{{Path: "message_1_4.jsonl", FromSeq: 1, ToSeq: 4, Count: 4}}
	next := ApplyPendingTailHead(head, rows, shards, 4, "", time.Unix(1, 0))
	if next.LastSeq != 4 || next.LastMessageID != "m4" || next.LastCommitID != "commit" ||
		next.Meta.TokenCount != 5 || next.Meta.ShardCount != 1 {
		t.Fatalf("head=%+v", next)
	}
	if MessageRowsCommitID(rows) == "" {
		t.Fatal("empty commit identity")
	}
	committed := ApplyCommittedMessageHead(MessageHead{SessionID: "s"}, rows, "commit-2", time.Unix(2, 0))
	if committed.LastCommitID != "commit-2" || committed.Meta.TokenCount != 5 ||
		committed.Meta.SessionID != "s" || committed.Meta.ShardCount != 0 {
		t.Fatalf("committed head=%+v", committed)
	}
}

func TestListAndDraftBaseSeq(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"message_5_6.jsonl", "message_1_2.jsonl", "junk.jsonl"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := AppendMessageRows(filepath.Join(dir, "message_5_6.jsonl"), []protocol.TranscriptEvent{{Seq: 5}, {Seq: 6}}); err != nil {
		t.Fatal(err)
	}
	names, err := ListMessageShardFiles(dir)
	if err != nil || len(names) != 2 || names[0] != "message_1_2.jsonl" || names[1] != "message_5_6.jsonl" {
		t.Fatalf("names=%v err=%v", names, err)
	}
	seq, err := DraftBaseSeq(dir, 3)
	if err != nil || seq != 6 {
		t.Fatalf("draft base=(%d,%v)", seq, err)
	}
}

func TestSessionStateValidators(t *testing.T) {
	items := []QueueItem{{State: QueueQueued}, {State: QueueSending}}
	if QueueHeadState(items) != QueueQueued || QueueHeadState(nil) != "" {
		t.Fatal("queue head state mismatch")
	}
	if err := ValidateCompactFrame(CompactFrameRecord{MessageToSeq: 4}, 3); err == nil {
		t.Fatal("compact beyond message head must fail")
	}
	if err := ValidateCompactFrame(CompactFrameRecord{MessageToSeq: 3}, 3); err != nil {
		t.Fatal(err)
	}
	if err := ValidateForkStart("p", "s", "p", "child", 5, 4, false); !errors.Is(err, ErrForkBeforeWatermark) {
		t.Fatalf("watermark err=%v", err)
	}
	if err := ValidateForkStart("p", "s", "p", "s", 0, 1, false); err == nil {
		t.Fatal("same fork identity must fail")
	}
	if err := ValidateForkStart("p", "s", "p", "child", 0, 1, true); err == nil {
		t.Fatal("existing child must fail")
	}
	remaining := []protocol.TranscriptEvent{{Seq: 5}}
	if err := ValidateRetentionPrefix(remaining, 2, 6); err == nil {
		t.Fatal("retention gap must fail")
	}
	if err := ValidateRetentionPrefix(remaining, 4, 6); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleQueueTransitions(t *testing.T) {
	state := LifecycleState{Draft: &DraftItem{Content: "draft"}}
	state = MoveDraftToQueue(state, "s", "draft", time.Unix(1, 0))
	if state.Draft != nil || len(state.Queue) != 1 || state.Queue[0].Seq != 1 {
		t.Fatalf("move draft=%+v", state)
	}
	state, changed := Enqueue(state, "request-1", "second", time.Unix(2, 0))
	if !changed || len(state.Queue) != 2 || state.Queue[1].RequestID != "request-1" {
		t.Fatalf("enqueue=%+v changed=%v", state, changed)
	}
	if _, duplicate := Enqueue(state, "request-1", "second", time.Unix(3, 0)); duplicate {
		t.Fatal("same requestID must be idempotent")
	}
	state, item, err := SendFront(state)
	if err != nil || item.State != QueueSending || item.SendCount != 1 {
		t.Fatalf("send front=%+v err=%v", item, err)
	}
	state, err = FailFront(state, "s", time.Unix(4, 0))
	if err != nil || state.Draft == nil || state.Draft.Content != "draft" || len(state.Queue) != 1 {
		t.Fatalf("fail front=%+v err=%v", state, err)
	}
	if _, _, err := SendFront(state); err != nil {
		t.Fatal(err)
	}
	state, err = ConfirmFront(state, "request-1")
	if err != nil || len(state.Queue) != 0 {
		t.Fatalf("confirm=%+v err=%v", state, err)
	}
}
