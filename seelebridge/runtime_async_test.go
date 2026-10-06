package seelebridge

import (
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	seeltools "github.com/RedHuang-0622/seelex/seelebridge/tools"
)

// 投影面只做字段搬运：这一条钉住"探针读数每一列都真的到了 Application 侧"。
// 漏搬一列的后果是界面上永久缺一栏，而编译不会报错——所以必须由用例盯。
func TestAsyncRunRecordMappingCarriesEveryColumn(t *testing.T) {
	started := time.Now().Add(-3 * time.Second)
	ended := started.Add(2 * time.Second)
	lastByte := ended.Add(-time.Second)
	info := seeltools.AsyncRunInfo{
		Handle: "a3", SessionID: "sess-1", Description: "跑集成测试", Command: "go test ./...",
		State: dto.AsyncStateFailed, Exit: 2, LogPath: `C:\Temp\seelex-async-1\a3.log`, LogBytes: 2048,
		LastByteAt: lastByte, Tail: "FAIL seelebridge/tools", Truncated: true, Degraded: true,
		BatchID: "req-9", StartedAt: started, EndedAt: ended,
		// 打点 K-2 的行 schema：类别 / 有界摘要 / 行数 / 回填位 / 同批下标。
		Kind: "inline", Summary: "failed · exit=2 · 40 行 · 2.0KiB · 末行: FAIL", Lines: 40,
		Notified: true, Index: 3,
	}
	record := asyncRunRecordFrom(info)

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

// Runtime 还没装配出工具路由（或整个 Runtime 为空）时，投影口必须回"没有行/没有信号"，
// 不得 panic——core 每次重投影都会调它。
func TestAsyncPortsAreNilSafeBeforeAssembly(t *testing.T) {
	var nilRuntime *Runtime
	if got := nilRuntime.AsyncRunsSnapshot(); got != nil {
		t.Fatalf("空 Runtime 的快照 = %+v", got)
	}
	if got := nilRuntime.AsyncRunEvents(); got != nil {
		t.Fatalf("空 Runtime 的信号口 = %v", got)
	}
	partial := &Runtime{}
	if got := partial.AsyncRunsSnapshot(); got != nil {
		t.Fatalf("未装配工具路由的快照 = %+v", got)
	}
	if got := partial.AsyncRunEvents(); got != nil {
		t.Fatalf("未装配工具路由的信号口 = %v", got)
	}
}
