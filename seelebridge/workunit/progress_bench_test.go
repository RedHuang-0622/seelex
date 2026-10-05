package workunit

// progress_bench_test.go — 步骤②收口后的**量级读数**（不是"更快/更慢"的断言，而是把唯一实现
// 的代价摆出来）：打点编解码、记录 → 折算、按 nodeID 的读法各自一次多少时间、几次分配。
//
// 收口只改"几份实现"，不改算法：因此这些数应当落在**亚微秒 / 个位数纳秒**与
// **0–2 次分配**这一档；若哪一天它跳到毫秒或几十次分配，说明有人把 I/O 塞进了读面。

import (
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

func benchRecords(n int) []sessionstore.NodeSessionRecord {
	records := make([]sessionstore.NodeSessionRecord, 0, n)
	for i := 0; i < n; i++ {
		records = append(records, sessionstore.NodeSessionRecord{
			NodeID:     "exec-wi-" + strings.Repeat("x", i%7) + string(rune('a'+i%26)),
			SessionID:  "sess-" + string(rune('a'+i%26)),
			Goal:       "把读面收成一份",
			Status:     StatusRunning,
			Summary:    "改到一半",
			StagesJSON: EncodeStages([]Stage{{Stage: "round_output", Preview: "已读 worktree_manager"}}),
			UpdatedAt:  time.Unix(int64(1791228649+i), 0).UTC(),
		})
	}
	return records
}

func BenchmarkClipPreview(b *testing.B) {
	long := strings.Repeat("很长的预览", 200)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if ClipPreview(long) == "" {
			b.Fatal("clip 不该返回空")
		}
	}
}

func BenchmarkEncodeStages(b *testing.B) {
	stages := []Stage{{Stage: "round_output", Preview: "已读 worktree_manager"}}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if len(EncodeStages(stages)) == 0 {
			b.Fatal("非空打点表必须编出载荷")
		}
	}
}

func BenchmarkDecodeStages(b *testing.B) {
	payload := EncodeStages([]Stage{{Stage: "round_output", Preview: "已读 worktree_manager"}})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if len(DecodeStages(payload)) != 1 {
			b.Fatal("解不出一条打点")
		}
	}
}

func BenchmarkProgressOf(b *testing.B) {
	record := benchRecords(1)[0]
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if p := ProgressOf(KindTeammate, record); !p.InFlight {
			b.Fatal("running 必须是在跑")
		}
	}
}

func BenchmarkUnitReaderList100(b *testing.B) {
	ledger := &fakeLedger{records: benchRecords(100)}
	reader := NewUnitReader(ledger, "p", "sess", KindTeammate, nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		list, err := reader.List()
		if err != nil || len(list) != 100 {
			b.Fatalf("List 读回 %d 条（err=%v）", len(list), err)
		}
	}
}
