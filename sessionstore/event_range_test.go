package sessionstore

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"testing"
)

func rangeEvents(count int) []Event {
	events := make([]Event, count)
	for index := range events {
		events[index] = Event{Seq: uint64(index + 1), Role: "user", Content: fmt.Sprintf("event-%d", index+1), TokenCount: 1}
	}
	return events
}

// TestEventRangeAcrossLocalBackends 契约测试：EventSeq 范围读取（含端点）
// 在 JSON/SQLite 后端语义一致，跨 shard 边界连续、倒置范围显式报错。
func TestEventRangeAcrossLocalBackends(t *testing.T) {
	for _, config := range []Config{
		{Backend: BackendJSON, Path: filepath.Join(t.TempDir(), "json")},
		{Backend: BackendSQLite, Path: filepath.Join(t.TempDir(), "sessions.db")},
	} {
		t.Run(string(config.Backend), func(t *testing.T) {
			repository, err := Open(context.Background(), config)
			if err != nil {
				t.Fatal(err)
			}
			defer repository.Close()
			key := Key{ProjectID: "project", SessionID: "session"}
			events := rangeEvents(250) // 3 event shards（100/100/50）
			if err := repository.WriteCommit(context.Background(), key, Commit{Events: events}); err != nil {
				t.Fatal(err)
			}

			// 单 shard 内范围：seq 3-5（含端点）。
			got, err := repository.ReadEventRange(context.Background(), key, 3, 5)
			if err != nil {
				t.Fatal(err)
			}
			if want := events[2:5]; !reflect.DeepEqual(got, want) {
				t.Fatalf("range [3,5] = %d events, want %d", len(got), len(want))
			}
			// 跨 shard 边界：seq 90-115 覆盖 shard 0/1，保持连续性。
			got, err = repository.ReadEventRange(context.Background(), key, 90, 115)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 26 || got[0].Seq != 90 || got[25].Seq != 115 {
				t.Fatalf("cross-shard range = %d events [%d..%d], want 26 [90..115]", len(got), got[0].Seq, got[len(got)-1].Seq)
			}
			// 尾部越界收敛：seq 5-1000 → 5..250。
			got, err = repository.ReadEventRange(context.Background(), key, 5, 1000)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 246 || got[0].Seq != 5 || got[245].Seq != 250 {
				t.Fatalf("clamped range = %d events [%d..%d], want 246 [5..250]", len(got), got[0].Seq, got[len(got)-1].Seq)
			}
			// 倒置范围显式报错。
			if _, err := repository.ReadEventRange(context.Background(), key, 10, 5); err == nil {
				t.Fatal("inverted range must fail")
			}
			// 空范围（无该区间事件）返回空切片。
			got, err = repository.ReadEventRange(context.Background(), key, 1000, 2000)
			if err != nil || len(got) != 0 {
				t.Fatalf("empty range = %#v err=%v, want empty", got, err)
			}
		})
	}
}

// TestEventRangeMissingSessionFails 验证缺失会话的 event range 读取显式
// 报错（不静默伪造为空；interfaces.md 降级矩阵）。
func TestEventRangeMissingSessionFails(t *testing.T) {
	repository, err := newJSONRepository(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.ReadEventRange(context.Background(), Key{ProjectID: "project", SessionID: "missing"}, 1, 5)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing session event range err = %v, want not-found", err)
	}
}
