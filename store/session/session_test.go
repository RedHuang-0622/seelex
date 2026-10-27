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
