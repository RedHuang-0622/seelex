package toolrecord

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

func TestAccumulatorCountsBothOutcomes(t *testing.T) {
	accumulator := NewAccumulator()
	accumulator.Record("bash", true, "")
	accumulator.Record("bash", true, "")
	accumulator.Record("bash", false, dto.ToolReasonTimeout)
	accumulator.Record("read_file", false, dto.ToolReasonNotFound)
	accumulator.Record("glob", true, "")

	stats := accumulator.Stats()
	if len(stats) != 3 {
		t.Fatalf("three tools must be present: %+v", stats)
	}
	if stats[0].Tool != "bash" || stats[0].Success != 2 || stats[0].Failure != 1 || stats[0].Total != 3 {
		t.Fatalf("stats must be ordered by calls with outcomes separated: %+v", stats[0])
	}
	if len(stats[0].Reasons) != 1 || stats[0].Reasons[0].Reason != dto.ToolReasonTimeout {
		t.Fatalf("failure reason must be retained: %+v", stats[0].Reasons)
	}
	if stats[0].LastAt.IsZero() {
		t.Fatal("LastAt must track the last failure")
	}
	for _, stat := range stats {
		switch {
		case stat.Tool == "glob" && !stat.LastAt.IsZero():
			t.Fatal("success-only tools must not have LastAt")
		case stat.Tool == "glob" && stat.Total != 1:
			t.Fatalf("glob should retain one success: %+v", stat)
		}
	}
	total, success, failure := accumulator.Totals()
	if total != 5 || success != 3 || failure != 2 {
		t.Fatalf("totals: total=%d success=%d failure=%d", total, success, failure)
	}
}

func TestAccumulatorSnapshotHonoursDirtyGate(t *testing.T) {
	accumulator := NewAccumulator()
	if _, dirty := accumulator.Snapshot(); dirty {
		t.Fatal("empty accumulator must not be dirty")
	}
	accumulator.Record("bash", true, "")
	accumulator.Record("bash", false, dto.ToolReasonRefused)
	snapshot, dirty := accumulator.Snapshot()
	if !dirty {
		t.Fatal("new readings must mark the accumulator dirty")
	}
	if len(snapshot.Tools) != 1 || snapshot.Tools[0].Success != 1 || snapshot.Tools[0].Failure != 1 {
		t.Fatalf("snapshot must contain the latest totals: %+v", snapshot.Tools)
	}
	if snapshot.At.IsZero() {
		t.Fatal("snapshot must include a timestamp")
	}
	if _, dirtyAgain := accumulator.Snapshot(); dirtyAgain {
		t.Fatal("snapshot must clear dirty")
	}
}

func TestAccumulatorLoadOnlyGrows(t *testing.T) {
	accumulator := NewAccumulator()
	accumulator.Record("bash", true, "")
	accumulator.Load(dto.ToolRecordSnapshot{At: time.Now().UTC(), Tools: []dto.ToolRecordStat{
		{Tool: "bash", Success: 7, Failure: 2, Total: 9, Reasons: []dto.ToolRecordReason{{Reason: dto.ToolReasonTimeout, Count: 2}}},
		{Tool: "bash_bg", Success: 1, Total: 1},
	}})
	stats := accumulator.Stats()
	if len(stats) != 2 {
		t.Fatalf("loaded tool must be present: %+v", stats)
	}
	bash := stats[0]
	if bash.Tool != "bash" || bash.Success != 7 || bash.Failure != 2 {
		t.Fatalf("load must keep the larger persisted counts: %+v", bash)
	}
	if len(bash.Reasons) != 1 || bash.Reasons[0].Count != 2 {
		t.Fatalf("load must merge failure reasons: %+v", bash.Reasons)
	}
	accumulator.Load(dto.ToolRecordSnapshot{Tools: []dto.ToolRecordStat{{Tool: "bash", Success: 1, Total: 1}}})
	after, _ := accumulator.Stat("bash")
	if after.Success != 7 {
		t.Fatalf("load must never reduce counts: %+v", after)
	}
}

func TestAccumulatorNormalizesReasonVocabulary(t *testing.T) {
	accumulator := NewAccumulator()
	accumulator.Record("bash", false, " timeout ")
	accumulator.Record("bash", false, "TIMEOUT")
	accumulator.Record("bash", false, "made-up")
	stat, _ := accumulator.Stat("bash")
	byReason := map[string]int{}
	for _, reason := range stat.Reasons {
		byReason[reason.Reason] = reason.Count
	}
	if byReason[dto.ToolReasonTimeout] != 2 || byReason[dto.ToolReasonOther] != 1 {
		t.Fatalf("reason normalization drift: %+v", stat.Reasons)
	}
}
