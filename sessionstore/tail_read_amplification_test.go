package sessionstore

import (
	"reflect"
	"testing"
	"time"
)

// TestTailReadAmplificationMeasurement 量化“尾窗读取”相对“全量读文件”的
// 读放大：5000 行（50 个分片）下对比 readRows(全量) 与
// readTailRowsForSelection(尾窗)，并断言两者经 selectEventTail 后语义一致。
func TestTailReadAmplificationMeasurement(t *testing.T) {
	store := newStoreEngine(t.TempDir(), 0)
	key := Key{ProjectID: "p-amplify", SessionID: "s-amplify"}
	const totalRows = 5000
	batch := make([]Event, 0, totalRows)
	for index := 1; index <= totalRows; index++ {
		row := messageRow(0, "", "user", EventKindUserInput, "x")
		row.TokenCount = 1
		batch = append(batch, row)
	}
	if _, err := store.messageCommit(key, "bulk", batch); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	full, err := store.readRows(key, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	fullDuration := time.Since(start)
	start = time.Now()
	tailRows, err := store.readTailRowsForSelection(key, 200_000, 4)
	if err != nil {
		t.Fatal(err)
	}
	tailDuration := time.Since(start)
	want := selectEventTail(full, 200_000, 4)
	got := selectEventTail(tailRows, 200_000, 4)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("tail-window selection mismatch: full=%d tailInput=%d", len(want), len(tailRows))
	}
	t.Logf("rows=%d shards=%d full_read=%s tail_read=%s tail_input_rows=%d selected=%d speedup=%.1fx",
		totalRows, len(full)/store.shardRows, fullDuration, tailDuration, len(tailRows), len(want),
		float64(fullDuration)/float64(maxDuration(tailDuration, time.Nanosecond)))
}

func maxDuration(left, right time.Duration) time.Duration {
	if left > right {
		return left
	}
	return right
}
