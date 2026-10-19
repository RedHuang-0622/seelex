package record

// stage_test.go — 钉住**叶子暂存**这一级台阶（2026-10-19 方向 C）：
// 本地累加、同步交出、序号递增、以及"条目超限只丢名字细分、不丢计数"。

import (
	"fmt"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

func TestStageAccumulatesLocallyAndTakesSynchronously(t *testing.T) {
	stage := NewStage("subagent:node-a", "sess-a")
	if stage.UnitID() != "subagent:node-a" || stage.SessionID() != "sess-a" {
		t.Fatalf("身份没记住：%q/%q", stage.UnitID(), stage.SessionID())
	}
	stage.Record("bash", true, "")
	stage.Record("bash", false, dto.ToolReasonTimeout)
	stage.Record("", true, "") // 空工具名不记（认不出是谁）

	seq, stats := stage.TakeForCommit()
	if seq != 1 {
		t.Fatalf("第一次交出的 seq 必须是 1：%d", seq)
	}
	if len(stats) != 1 || stats[0].Tool != "bash" || stats[0].Success != 1 || stats[0].Failure != 1 {
		t.Fatalf("交出的是这一窗口的增量：%+v", stats)
	}
	if len(stats[0].Reasons) != 1 || stats[0].Reasons[0].Reason != dto.ToolReasonTimeout {
		t.Fatalf("失败原因要随增量一起交出去：%+v", stats[0].Reasons)
	}
	// 交出即清空：没有新读数时不动 seq、交空增量（重复收口因此幂等）。
	if seq, stats := stage.TakeForCommit(); seq != 0 || len(stats) != 0 {
		t.Fatalf("交完之后应当为空：seq=%d stats=%+v", seq, stats)
	}
	stage.Record("read_file", true, "")
	seq, stats = stage.TakeForCommit()
	if seq != 2 || len(stats) != 1 || stats[0].Tool != "read_file" {
		t.Fatalf("第二个窗口：seq=%d stats=%+v", seq, stats)
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
		t.Fatalf("两次增量都要相加：total=%d success=%d failure=%d", total, success, failure)
	}
	stat, ok := accumulator.Stat("bash")
	if !ok || stat.Total != 3 || len(stat.Reasons) != 1 || stat.Reasons[0].Count != 2 {
		t.Fatalf("同类原因按条数相加：%+v", stat)
	}
}

func TestStageBoundsToolNamesWithoutLosingCounts(t *testing.T) {
	stage := NewStage("subagent:node-many", "sess-a")
	// 塞满条目上限，再多记一个新工具名。
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
		t.Fatalf("计数一分都不许丢：%d（want %d）", total, StageToolNameLimit+1)
	}
	if _, ok := names["tool-overflow"]; ok {
		t.Fatal("超限的新工具名不该自成一格")
	}
	overflow, ok := names[StageOverflowTool]
	if !ok || overflow.Total != 1 || overflow.Failure != 1 {
		t.Fatalf("超限的新工具名并进 %q（只丢名字的细分）：%+v", StageOverflowTool, stats)
	}
}

func TestStageIgnoresEmptyIdentity(t *testing.T) {
	stage := NewStage("  ", "  ")
	if stage.UnitID() != "" || stage.SessionID() != "" {
		t.Fatalf("身份要去掉空白：%q/%q", stage.UnitID(), stage.SessionID())
	}
	var nilStage *Stage
	nilStage.Record("bash", true, "") // nil 安全：不 panic
	if seq, stats := nilStage.TakeForCommit(); seq != 0 || len(stats) != 0 {
		t.Fatalf("nil 暂存交出空增量：%d/%+v", seq, stats)
	}
}
