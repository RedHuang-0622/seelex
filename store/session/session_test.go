package session

import (
	"strings"
	"testing"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
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
