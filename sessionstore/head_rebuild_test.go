package sessionstore

import (
	"os"
	"testing"
	"time"
)

// TestHeadRepairRebuildsMessageFromData 对应 T-M1-09（§2.0 规则 3 / S15）：
// 模块 head 两次校验仍失败时，reader 按数据文件重建 head（修补）并发布，
// 不返回旧值也不判损坏。
func TestHeadRepairRebuildsMessageFromData(t *testing.T) {
	store := newStoreEngine(t.TempDir(), storageSettings{})
	key := Key{ProjectID: "p-m109", SessionID: "s-m109"}
	if _, err := store.messageCommit(key, "c1", []Event{
		{Role: "user", Content: "a", MessageID: "m-1"},
		{Role: "assistant", Content: "b", MessageID: "m-2"},
		{Role: "user", Content: "c", MessageID: "m-3"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.modulePath(key, moduleMessage), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatalf("read after persistent corruption must self-heal: %v", err)
	}
	if head.LastSeq != 3 || head.TotalRows != 3 || head.LastMessageID != "m-3" {
		t.Fatalf("rebuilt head = %+v, want last_seq=3 total=3 last_message=m-3", head)
	}
	// 修补后的 head 已发布，再次读取不再触发重建。
	again, err := store.readMessageHead(key)
	if err != nil || again.LastSeq != 3 {
		t.Fatalf("re-read after repair = %+v err=%v", again, err)
	}
}

// TestHeadRepairRebuildsEventFromData 验证 EVENT head 同样按分片重建。
func TestHeadRepairRebuildsEventFromData(t *testing.T) {
	store := newStoreEngine(t.TempDir(), storageSettings{})
	key := Key{ProjectID: "p-ev109", SessionID: "s-ev109"}
	if _, err := store.structuralEventCommit(key, "c1", []structuralEvent{
		{Kind: structuralEventFork, AnchorSeq: 1, CommitID: "c1", CreatedAt: time.Now().UTC()},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.modulePath(key, moduleEvent), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	head, err := store.readEventHead(key)
	if err != nil {
		t.Fatalf("read after persistent corruption must self-heal: %v", err)
	}
	if head.LastID != 1 || head.Total != 1 {
		t.Fatalf("rebuilt event head = %+v, want last_id=1 total=1", head)
	}
}

// TestHeadRepairRebuildsCompactFromData 验证 compact head 从 compact.jsonl
// 重建（latest frame 冗余恢复）。
func TestHeadRepairRebuildsCompactFromData(t *testing.T) {
	store := newStoreEngine(t.TempDir(), storageSettings{})
	key := Key{ProjectID: "p-cp109", SessionID: "s-cp109"}
	if _, err := store.messageCommit(key, "c1", []Event{
		{Role: "user", Content: "q1", MessageID: "m-1"},
		{Role: "assistant", Content: "a1", MessageID: "m-2"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.compactCommit(key, compactFrameRecord{
		FrameID: "f-1", MessageFrom: "m-1", MessageTo: "m-2",
		MessageFromSeq: 1, MessageToSeq: 2, Summary: "先期摘要",
		BoundaryStatus: "complete", CommitID: "c1", CompressedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.modulePath(key, moduleCompact), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	head, err := store.readCompactHead(key)
	if err != nil {
		t.Fatalf("read after persistent corruption must self-heal: %v", err)
	}
	if head.FrameCount != 1 || head.LastFrameID != "f-1" || head.LatestFrame == nil {
		t.Fatalf("rebuilt compact head = %+v, want frame_count=1 last=f-1", head)
	}
}
