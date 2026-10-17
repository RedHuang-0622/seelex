package jobs

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/backend/tool/registry"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// TestRecordMappingCarriesEveryColumn 钉住"探针读数每一列都真的到了 Application 侧"。
// 漏搬一列的后果是界面上永久缺一栏，而编译不会报错——所以必须由用例盯。
func TestRecordMappingCarriesEveryColumn(t *testing.T) {
	started := time.Now().Add(-3 * time.Second)
	ended := started.Add(2 * time.Second)
	lastByte := ended.Add(-time.Second)
	info := registry.AsyncRunInfo{
		Handle: "a3", SessionID: "sess-1", Description: "跑集成测试", Command: "go test ./...",
		State: dto.AsyncStateFailed, Exit: 2, LogPath: `C:\Temp\seelex-async-1\a3.log`, LogBytes: 2048,
		LastByteAt: lastByte, Tail: "FAIL backend/tool/registry", Truncated: true, Degraded: true,
		BatchID: "req-9", StartedAt: started, EndedAt: ended,
		// 打点 K-2 的行 schema：类别 / 有界摘要 / 行数 / 回填位 / 同批下标。
		Kind: "inline", Summary: "failed · exit=2 · 40 行 · 2.0KiB · 末行: FAIL", Lines: 40,
		Notified: true, Index: 3,
	}
	record := recordFrom(info)

	if record.Handle != info.Handle || record.SessionID != info.SessionID ||
		record.Description != info.Description || record.Command != info.Command ||
		record.State != info.State || record.ExitCode != info.Exit ||
		record.LogPath != info.LogPath || record.LogBytes != info.LogBytes ||
		record.Kind != info.Kind || record.Summary != info.Summary || record.Lines != info.Lines ||
		record.Notified != info.Notified || record.Index != info.Index ||
		record.Tail != info.Tail || record.Truncated != info.Truncated ||
		record.Degraded != info.Degraded || record.BatchID != info.BatchID ||
		!record.StartedAt.Equal(started) || !record.EndedAt.Equal(ended) ||
		!record.LastByteAt.Equal(lastByte) {
		t.Fatalf("探针读数搬运不完整:\ninfo=%+v\nrecord=%+v", info, record)
	}
}

// TestProjectAsyncRunsEmptyIsNil 钉住空表归一：没有行时返回 nil（而不是空切片），
// 消费侧据此判断"要不要出这一块"。
func TestProjectAsyncRunsEmptyIsNil(t *testing.T) {
	if got := ProjectAsyncRuns(nil); got != nil {
		t.Fatalf("空表投影 = %+v, want nil", got)
	}
	if got := ProjectAsyncRuns([]registry.AsyncRunInfo{}); got != nil {
		t.Fatalf("零长表投影 = %+v, want nil", got)
	}
	got := ProjectAsyncRuns([]registry.AsyncRunInfo{{Handle: "a1"}, {Handle: "a2"}})
	if len(got) != 2 || got[0].Handle != "a1" || got[1].Handle != "a2" {
		t.Fatalf("顺序被改变：%+v", got)
	}
}
