package store

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	storeroot "github.com/RedHuang-0622/seelex/store/root"
)

// BenchmarkDraftSnapshotAppend 对照周期快照复用末分片指针前后的成本。
// legacy_rescan 每次强制清空指针，模拟旧路径重新定位末分片；
// tail_pointer 保持周期快照的真实连续写行为。
func BenchmarkDraftSnapshotAppend(b *testing.B) {
	for _, testCase := range []struct {
		name       string
		resetCache bool
	}{
		{name: "legacy_rescan", resetCache: true},
		{name: "tail_pointer"},
	} {
		b.Run(testCase.name, func(b *testing.B) {
			benchmarkDraftSnapshotAppend(b, testCase.resetCache)
		})
	}
}

func benchmarkDraftSnapshotAppend(b *testing.B, resetCache bool) {
	store := newStoreEngine(b.TempDir(), storeroot.Settings{MessageShardRows: 100})
	key := Key{ProjectID: "p-draft-bench", SessionID: "s-draft-bench"}
	rows := roundRows(50)
	for index := range rows {
		rows[index].Content += strings.Repeat("x", 4<<10)
	}
	if _, err := store.messageCommit(key, "published", rows); err != nil {
		b.Fatal(err)
	}
	var decodes atomic.Int64
	shardDecodeHook = func() { decodes.Add(1) }
	b.Cleanup(func() { shardDecodeHook = nil })
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if resetCache {
			store.locks(key).draftTail.Store(nil)
		}
		seq := uint64(51 + index)
		if _, err := store.appendDraftRows(key, "turn-draft", []Event{
			messageRow(seq, fmt.Sprintf("draft-%d", seq), "assistant", EventKindLLM, "draft"),
		}); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(decodes.Load())/float64(b.N), "shard-decodes/op")
}
