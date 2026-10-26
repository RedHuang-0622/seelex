package toolrecord

import (
	"fmt"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

func TestStageAccumulatesLocallyAndTakesSynchronously(t *testing.T) {
	stage := NewStage("subagent:node-a", "sess-a")
	if stage.UnitID() != "subagent:node-a" || stage.SessionID() != "sess-a" {
		t.Fatalf("identity mismatch: %q/%q", stage.UnitID(), stage.SessionID())
	}
	stage.Record("bash", true, "")
	stage.Record("bash", false, dto.ToolReasonTimeout)
	stage.Record("", true, "")

	seq, stats := stage.TakeForCommit()
	if seq != 1 {
		t.Fatalf("first commit seq = %d, want 1", seq)
	}
	if len(stats) != 1 || stats[0].Tool != "bash" || stats[0].Success != 1 || stats[0].Failure != 1 {
		t.Fatalf("stage must return one delta: %+v", stats)
	}
	if len(stats[0].Reasons) != 1 || stats[0].Reasons[0].Reason != dto.ToolReasonTimeout {
		t.Fatalf("failure reason must travel with delta: %+v", stats[0].Reasons)
	}
	if seq, stats := stage.TakeForCommit(); seq != 0 || len(stats) != 0 {
		t.Fatalf("empty take must be a no-op: seq=%d stats=%+v", seq, stats)
	}
	stage.Record("read_file", true, "")
	seq, stats = stage.TakeForCommit()
	if seq != 2 || len(stats) != 1 || stats[0].Tool != "read_file" {
		t.Fatalf("second window: seq=%d stats=%+v", seq, stats)
	}
}

func TestStageMergesIntoAccumulatorByAddition(t *testing.T) {
	stage := NewStage("role:role-impl", "sess-a")
	accumulator := NewAccumulator()

	stage.Record("bash", true, "")
	stage.Record("bash", false, dto.ToolReasonPermission)
	_, first := stage.TakeForCommit()
	accumulator.Merge(first)
	stage.Record("bash", false, dto.ToolReasonPermission)
	_, second := stage.TakeForCommit()
	accumulator.Merge(second)

	total, success, failure := accumulator.Totals()
	if total != 3 || success != 1 || failure != 2 {
		t.Fatalf("deltas must add: total=%d success=%d failure=%d", total, success, failure)
	}
	stat, ok := accumulator.Stat("bash")
	if !ok || stat.Total != 3 || len(stat.Reasons) != 1 || stat.Reasons[0].Count != 2 {
		t.Fatalf("reasons must add: %+v", stat)
	}
}

func TestStageBoundsToolNamesWithoutLosingCounts(t *testing.T) {
	stage := NewStage("subagent:node-many", "sess-a")
	for index := 0; index < StageToolNameLimit; index++ {
		stage.Record(fmt.Sprintf("tool-%03d", index), true, "")
	}
	stage.Record("tool-overflow", false, dto.ToolReasonOther)

	_, stats := stage.TakeForCommit()
	names := map[string]*dto.ToolRecordStat{}
	total := 0
	for index := range stats {
		names[stats[index].Tool] = &stats[index]
		total += stats[index].Total
	}
	if total != StageToolNameLimit+1 {
		t.Fatalf("counts must not be dropped: %d (want %d)", total, StageToolNameLimit+1)
	}
	if _, ok := names["tool-overflow"]; ok {
		t.Fatal("overflow tool must not get its own row")
	}
	overflow, ok := names[StageOverflowTool]
	if !ok || overflow.Total != 1 || overflow.Failure != 1 {
		t.Fatalf("overflow must fold into %q: %+v", StageOverflowTool, stats)
	}
}

func TestStageIgnoresEmptyIdentity(t *testing.T) {
	stage := NewStage("  ", "  ")
	if stage.UnitID() != "" || stage.SessionID() != "" {
		t.Fatalf("identity must be trimmed: %q/%q", stage.UnitID(), stage.SessionID())
	}
	var nilStage *Stage
	nilStage.Record("bash", true, "")
	if seq, stats := nilStage.TakeForCommit(); seq != 0 || len(stats) != 0 {
		t.Fatalf("nil stage must return empty delta: %d/%+v", seq, stats)
	}
}
