// Package session owns transcript shard naming and pending-tail classification.
// Head publication and message orchestration remain in the root adapter until
// later M5 slices move them.
package session

import (
	"fmt"

	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
)

const (
	PendingTailClean       = "clean"
	PendingTailRecoverable = "recoverable"
	PendingTailRecovered   = "recovered"
	PendingTailGap         = "gap"
	PendingTailDiscarded   = "discarded"
)

// PendingTailReport is the stable draft-tail report.
type PendingTailReport struct {
	SessionID   string                     `json:"session_id"`
	Status      string                     `json:"status"`
	HeadSeq     uint64                     `json:"head_seq"`
	TailFrom    uint64                     `json:"tail_from,omitempty"`
	TailTo      uint64                     `json:"tail_to,omitempty"`
	Rows        []protocol.TranscriptEvent `json:"rows,omitempty"`
	ExtraShards []string                   `json:"extra_shards,omitempty"`
	Reason      string                     `json:"reason,omitempty"`
}

// RowCount returns the number of unpublished rows.
func (report PendingTailReport) RowCount() int { return len(report.Rows) }

// ClassifyPendingTail preserves the single draft-tail recoverability rule.
func ClassifyPendingTail(headSeq uint64, rows []protocol.TranscriptEvent, duplicate bool) (string, string) {
	if len(rows) == 0 {
		return PendingTailClean, ""
	}
	if duplicate {
		return PendingTailGap, "草稿尾部存在重复 seq（多个分片承载同一 seq）：不发布"
	}
	want := headSeq + 1
	for _, row := range rows {
		if row.Seq != want {
			return PendingTailGap, fmt.Sprintf(
				"草稿尾部与发布点不连续：期望 seq %d，实际 %d（发布点已不被本段独占，只能在一致前缀处截断）",
				want, row.Seq)
		}
		want++
	}
	return PendingTailRecoverable, ""
}

// MessageShardName returns the stable sorted-shard filename.
func MessageShardName(fromSeq, toSeq uint64) string {
	return fmt.Sprintf("message_%d_%d.jsonl", fromSeq, toSeq)
}

// ParseMessageShardName validates and parses a message shard name.
func ParseMessageShardName(name string) (uint64, uint64, bool) {
	var fromSeq, toSeq uint64
	if _, err := fmt.Sscanf(name, "message_%d_%d.jsonl", &fromSeq, &toSeq); err != nil {
		return 0, 0, false
	}
	return fromSeq, toSeq, fromSeq > 0 && toSeq >= fromSeq
}

// EventShardName returns the stable event-shard filename.
func EventShardName(fromID, toID uint64) string {
	return fmt.Sprintf("event_%d_%d.jsonl", fromID, toID)
}

// ParseEventShardName validates and parses an event shard name.
func ParseEventShardName(name string) (uint64, uint64, bool) {
	var fromID, toID uint64
	if _, err := fmt.Sscanf(name, "event_%d_%d.jsonl", &fromID, &toID); err != nil {
		return 0, 0, false
	}
	return fromID, toID, fromID > 0 && toID >= fromID
}
