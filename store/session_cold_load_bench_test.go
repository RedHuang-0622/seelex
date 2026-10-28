package store

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

// sessionColdLoadBenchFixture 是一份可重复的会话冷读现场：message 事件行是
// 冷恢复的唯一正文事实源，rowBytes 用来模拟普通会话与含大工具结果的会话。
type sessionColdLoadBenchFixture struct {
	repository *jsonRepository
	key        Key
	rows       int
	rowBytes   int
}

// newSessionColdLoadBenchFixture 在临时目录写入合成会话；构造成本不计入
// benchmark 计时。分片保持生产默认的 100 行粒度。
func newSessionColdLoadBenchFixture(b *testing.B, rows, rowBytes, compactAt int) *sessionColdLoadBenchFixture {
	b.Helper()
	repository, err := newJSONRepository(b.TempDir(), storeroot.Settings{MessageShardRows: 100})
	if err != nil {
		b.Fatalf("new repository: %v", err)
	}
	fixture := &sessionColdLoadBenchFixture{
		repository: repository,
		key:        Key{ProjectID: "project-cold-load-bench", SessionID: "session-cold-load-bench"},
		rows:       rows,
		rowBytes:   rowBytes,
	}
	b.Cleanup(func() { _ = repository.Close() })

	const batchRows = 100
	for start := 0; start < rows; start += batchRows {
		end := min(start+batchRows, rows)
		events := make([]Event, 0, end-start)
		for index := start; index < end; index++ {
			events = append(events, sessionColdLoadBenchRow(index, rowBytes))
		}
		if _, err := repository.layout.messageCommit(fixture.key, fmt.Sprintf("seed-%d", start), events); err != nil {
			b.Fatalf("seed rows %d..%d: %v", start, end, err)
		}
	}
	if compactAt > 0 && compactAt < rows {
		if _, err := repository.layout.compactCommit(fixture.key, compactFrameRecord{
			FrameID:        "bench-compact",
			MessageFrom:    fmt.Sprintf("message-%06d", 1),
			MessageTo:      fmt.Sprintf("message-%06d", compactAt),
			MessageFromSeq: 1,
			MessageToSeq:   uint64(compactAt),
			Summary:        strings.Repeat("summary ", compactAt/5),
			BoundaryStatus: CompactBoundaryComplete,
		}); err != nil {
			b.Fatalf("compact fixture: %v", err)
		}
	}
	return fixture
}

// sessionColdLoadBenchRow 生成可预测的 user/assistant 交替行。内容长度覆盖
// JSON 编解码与分片读取成本；seq、消息 ID 与 token 估算都保持稳定。
func sessionColdLoadBenchRow(index, rowBytes int) Event {
	role := "assistant"
	kind := EventKindLLM
	if index%2 == 0 {
		role = "user"
		kind = EventKindUserInput
	}
	content := fmt.Sprintf("row-%06d:", index)
	if rowBytes > len(content) {
		content += strings.Repeat(string(rune('a'+index%26)), rowBytes-len(content))
	}
	return Event{
		Seq:        uint64(index + 1),
		MessageID:  fmt.Sprintf("message-%06d", index+1),
		Role:       role,
		Kind:       kind,
		Content:    content,
		TokenCount: max(1, rowBytes/4),
		CreatedAt:  time.Unix(int64(index), 0).UTC(),
	}
}

// BenchmarkSessionColdLoad 测量冷恢复会穿过的主要存储读面：
//
//   - message_full：读取完整 message 行；
//   - message_resume：从 compact 指针读取可见后缀行；
//   - history_total：只取总数（端口语义，但当前实现仍会构造全部 provider 消息）；
//   - history_tail：总数探测 + 200 条尾窗；
//   - transcript_tail：保留预算下的尾部事件窗口；
//   - pending_tail：冷加载前的草稿尾部探测；
//   - wire：compact 摘要 + 尾窗 + 最近 K 次尝试；
//   - parallel_reads_legacy：优化前的全量 message/history/transcript 三路并行；
//   - parallel_reads：优化后的 compact 后缀 message/transcript 并行 + wire 装配。
//
// 运行示例：
//
//	go test ./store -run '^$' -bench '^BenchmarkSessionColdLoad$' -benchmem -benchtime=3x
func BenchmarkSessionColdLoad(b *testing.B) {
	scenarios := []struct {
		name      string
		rows      int
		rowBytes  int
		compactAt int
	}{
		{name: "median-316rows-4KiB", rows: 316, rowBytes: 4 << 10},
		{name: "long-1700rows-4KiB", rows: 1700, rowBytes: 4 << 10, compactAt: 1200},
		{name: "tool-heavy-1700rows-64KiB", rows: 1700, rowBytes: 64 << 10, compactAt: 1200},
	}
	operations := []struct {
		name string
		run  func(*sessionColdLoadBenchFixture) error
	}{
		{name: "message_full", run: func(fixture *sessionColdLoadBenchFixture) error {
			_, err := fixture.repository.layout.readRows(fixture.key, 1, 0)
			return err
		}},
		{name: "message_resume", run: func(fixture *sessionColdLoadBenchFixture) error {
			head, err := fixture.repository.layout.readCompactHead(fixture.key)
			if err != nil {
				return err
			}
			fromSeq := uint64(1)
			if head.LatestFrame != nil {
				fromSeq = head.LatestFrame.MessageToSeq + 1
			}
			_, err = fixture.repository.layout.readRows(fixture.key, fromSeq, 0)
			return err
		}},
		{name: "history_total", run: func(fixture *sessionColdLoadBenchFixture) error {
			_, _, err := fixture.repository.ReadRange(context.Background(), fixture.key, 0, 0)
			return err
		}},
		{name: "history_tail", run: func(fixture *sessionColdLoadBenchFixture) error {
			_, total, err := fixture.repository.ReadRange(context.Background(), fixture.key, 0, 0)
			if err != nil {
				return err
			}
			offset := max(0, total-200)
			_, _, err = fixture.repository.ReadRange(context.Background(), fixture.key, offset, 200)
			return err
		}},
		{name: "transcript_tail", run: func(fixture *sessionColdLoadBenchFixture) error {
			_, err := fixture.repository.ReadEventTail(
				context.Background(), fixture.key, 200_000, 4,
			)
			return err
		}},
		{name: "pending_tail", run: func(fixture *sessionColdLoadBenchFixture) error {
			_, _, err := fixture.repository.pendingTailWorkspace(fixture.key)
			return err
		}},
		{name: "wire", run: func(fixture *sessionColdLoadBenchFixture) error {
			_, _, err := fixture.repository.assembleWireWorkspace(fixture.key, 200_000, 3)
			return err
		}},
		{name: "parallel_reads_legacy", run: func(fixture *sessionColdLoadBenchFixture) error {
			return runSessionColdLoadReads(fixture, true)
		}},
		{name: "parallel_reads", run: func(fixture *sessionColdLoadBenchFixture) error {
			return runSessionColdLoadReads(fixture, false)
		}},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			fixture := newSessionColdLoadBenchFixture(b, scenario.rows, scenario.rowBytes, scenario.compactAt)
			for _, operation := range operations {
				b.Run(operation.name, func(b *testing.B) {
					benchmarkSessionColdLoadOperation(b, fixture, operation.run)
				})
			}
		})
	}
}

// benchmarkSessionColdLoadOperation 执行单一读面并报告每次操作的整分片解码
// 次数；该计数比墙钟时间更能固定“同一批行被重复解码”的机制。
func benchmarkSessionColdLoadOperation(
	b *testing.B,
	fixture *sessionColdLoadBenchFixture,
	operation func(*sessionColdLoadBenchFixture) error,
) {
	b.Helper()
	b.ReportAllocs()
	var decodes atomic.Int64
	shardDecodeHook = func() { decodes.Add(1) }
	b.Cleanup(func() { shardDecodeHook = nil })

	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if err := operation(fixture); err != nil {
			b.Fatalf("operation: %v", err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(decodes.Load())/float64(b.N), "shard-decodes/op")
	b.ReportMetric(float64(fixture.rows), "rows")
	b.ReportMetric(float64(fixture.rows*fixture.rowBytes), "row-bytes")
}

// runSessionColdLoadReads 复刻应用冷加载的并行磁盘读，并在收口后执行 wire
// 装配。includeHistory=true 保留优化前的 provider history 探测，用于在同一
// 现场内做前后 A/B；生产路径在 record 命中时不再读取该缓存。
func runSessionColdLoadReads(fixture *sessionColdLoadBenchFixture, includeHistory bool) error {
	var (
		group         sync.WaitGroup
		messageErr    error
		historyErr    error
		transcriptErr error
	)
	group.Add(2)
	go func() {
		defer group.Done()
		if includeHistory {
			_, messageErr = fixture.repository.layout.readRows(fixture.key, 1, 0)
			return
		}
		head, err := fixture.repository.layout.readCompactHead(fixture.key)
		if err != nil {
			messageErr = err
			return
		}
		fromSeq := uint64(1)
		if head.LatestFrame != nil {
			fromSeq = head.LatestFrame.MessageToSeq + 1
		}
		_, messageErr = fixture.repository.layout.readRows(fixture.key, fromSeq, 0)
	}()
	if includeHistory {
		group.Add(1)
		go func() {
			defer group.Done()
			_, total, err := fixture.repository.ReadRange(context.Background(), fixture.key, 0, 0)
			if err == nil {
				offset := max(0, total-200)
				_, _, err = fixture.repository.ReadRange(context.Background(), fixture.key, offset, 200)
			}
			historyErr = err
		}()
	}
	go func() {
		defer group.Done()
		_, transcriptErr = fixture.repository.ReadEventTail(
			context.Background(), fixture.key, 200_000, 4,
		)
	}()
	group.Wait()
	if messageErr != nil {
		return fmt.Errorf("message: %w", messageErr)
	}
	if historyErr != nil {
		return fmt.Errorf("history: %w", historyErr)
	}
	if transcriptErr != nil {
		return fmt.Errorf("transcript: %w", transcriptErr)
	}
	_, _, err := fixture.repository.assembleWireWorkspace(fixture.key, 200_000, 3)
	return err
}
