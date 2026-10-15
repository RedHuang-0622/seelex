package toolrecord

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

func TestAccumulatorCountsBothOutcomes(t *testing.T) {
	accumulator := NewAccumulator()
	accumulator.Record("bash", true, "")
	accumulator.Record("bash", true, "")
	accumulator.Record("bash", false, dto.ToolReasonTimeout)
	accumulator.Record("read_file", false, dto.ToolReasonNotFound)
	accumulator.Record("glob", true, "") // 只成功过的工具：不该有"最后一次失败"时刻

	stats := accumulator.Stats()
	if len(stats) != 3 {
		t.Fatalf("三个工具在册：%+v", stats)
	}
	if stats[0].Tool != "bash" || stats[0].Success != 2 || stats[0].Failure != 1 || stats[0].Total != 3 {
		t.Fatalf("降序首格必须是调用最多的工具，且成功/失败分开记：%+v", stats[0])
	}
	if len(stats[0].Reasons) != 1 || stats[0].Reasons[0].Reason != dto.ToolReasonTimeout {
		t.Fatalf("失败原因必须落成一格：%+v", stats[0].Reasons)
	}
	if stats[0].LastAt.IsZero() {
		t.Fatal("LastAt 必须落上最后一次失败的时刻（成功不刷新它）")
	}
	for _, stat := range stats {
		switch {
		case stat.Tool == "glob" && !stat.LastAt.IsZero():
			t.Fatal("只成功过的工具不该有 LastAt")
		case stat.Tool == "glob" && stat.Total != 1:
			t.Fatalf("glob 那笔是成功：%+v", stat)
		}
	}
	total, success, failure := accumulator.Totals()
	if total != 5 || success != 3 || failure != 2 {
		t.Fatalf("合计：total=%d success=%d failure=%d", total, success, failure)
	}
}

func TestAccumulatorSnapshotHonoursDirtyGate(t *testing.T) {
	accumulator := NewAccumulator()
	if _, dirty := accumulator.Snapshot(); dirty {
		t.Fatal("没记过任何东西时快照不该标记 dirty（调用方据此跳过写盘）")
	}
	accumulator.Record("bash", true, "")
	accumulator.Record("bash", false, dto.ToolReasonRefused)
	snapshot, dirty := accumulator.Snapshot()
	if !dirty {
		t.Fatal("有新读数时必须标记 dirty")
	}
	if len(snapshot.Tools) != 1 || snapshot.Tools[0].Success != 1 || snapshot.Tools[0].Failure != 1 {
		t.Fatalf("快照必须是整份最新读数：%+v", snapshot.Tools)
	}
	if snapshot.At.IsZero() {
		t.Fatal("快照必须带时刻（落盘行用它回答'这一批是什么时候写的'）")
	}
	if _, dirtyAgain := accumulator.Snapshot(); dirtyAgain {
		t.Fatal("取过一次之后（没有新读数）不该再标记 dirty")
	}
}

func TestAccumulatorLoadOnlyGrows(t *testing.T) {
	accumulator := NewAccumulator()
	accumulator.Record("bash", true, "") // 进程内已经记了一笔
	accumulator.Load(dto.ToolRecordSnapshot{At: time.Now().UTC(), Tools: []dto.ToolRecordStat{
		{Tool: "bash", Success: 7, Failure: 2, Total: 9, Reasons: []dto.ToolRecordReason{{Reason: dto.ToolReasonTimeout, Count: 2}}},
		{Tool: "bash_bg", Success: 1, Total: 1},
	}})
	stats := accumulator.Stats()
	if len(stats) != 2 {
		t.Fatalf("回填的另一个工具也要在册：%+v", stats)
	}
	bash := stats[0]
	if bash.Tool != "bash" || bash.Success != 7 || bash.Failure != 2 {
		t.Fatalf("落盘读数更大时取较大者：%+v", bash)
	}
	if len(bash.Reasons) != 1 || bash.Reasons[0].Count != 2 {
		t.Fatalf("原因计数也要回填：%+v", bash.Reasons)
	}
	// 再回填一份更小的（例如换了机器）：不许把已经记到的改小。
	accumulator.Load(dto.ToolRecordSnapshot{Tools: []dto.ToolRecordStat{{Tool: "bash", Success: 1, Total: 1}}})
	after, _ := accumulator.Stat("bash")
	if after.Success != 7 {
		t.Fatalf("回填只增不减：%+v", after)
	}
}

func TestAccumulatorNormalizesReasonVocabulary(t *testing.T) {
	accumulator := NewAccumulator()
	accumulator.Record("bash", false, " timeout ") // 词表内的词：去空白 + 折大小写后原样留
	accumulator.Record("bash", false, "TIMEOUT")
	accumulator.Record("bash", false, "made-up") // 词表外：一律 other
	stat, _ := accumulator.Stat("bash")
	byReason := map[string]int{}
	for _, reason := range stat.Reasons {
		byReason[reason.Reason] = reason.Count
	}
	if byReason[dto.ToolReasonTimeout] != 2 || byReason[dto.ToolReasonOther] != 1 {
		t.Fatalf("词表内（含大小写/空白）归一后原样留，词表外归 other：%+v", stat.Reasons)
	}
}
