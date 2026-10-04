package sessionstore

// 压缩记录通道（moduleCompaction）的存储语义验收：水位式追加 + 幂等空操作 +
// 前缀不符时的整份重写（自愈）+ head 从数据文件重建。
//
// 这条通道承载的是**应用侧压缩记录的原文**（见 compaction_records.go 的文件头），
// 因此这里只钉存储层承诺的四件事：按 Seq 保序、重复提交同一份列表是空操作、
// 内存列表前缀与盘上不符时不把两份历史缝在一起、head 损坏时按数据文件重建。

import (
	"encoding/json"
	"os"
	"testing"
)

func compactionPayload(t *testing.T, version int, failed bool) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"version": version, "failed": failed})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestCompactionRecordsAppendIsWatermarkedAndIdempotent(t *testing.T) {
	store, key := messageFixture(t, 0)
	first := compactionPayload(t, 1, false)
	second := compactionPayload(t, 2, true)

	head, added, err := store.commitCompactionRecords(key, []json.RawMessage{first})
	if err != nil {
		t.Fatalf("首次提交: %v", err)
	}
	if added != 1 || head.Count != 1 {
		t.Fatalf("首次提交应追加 1 行：added=%d head=%+v", added, head)
	}
	// 重复提交同一份列表 = 幂等空操作（每回合收尾都会调一次）。
	if _, added, err = store.commitCompactionRecords(key, []json.RawMessage{first}); err != nil || added != 0 {
		t.Fatalf("重复提交同一份列表应是空操作：added=%d err=%v", added, err)
	}
	// 追加尾部：只写增量。
	if _, added, err = store.commitCompactionRecords(key, []json.RawMessage{first, second}); err != nil || added != 1 {
		t.Fatalf("第二次提交应只追加 1 行：added=%d err=%v", added, err)
	}

	rows, err := store.readCompactionRecords(key)
	if err != nil {
		t.Fatalf("读回: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("应读回 2 行：%+v", rows)
	}
	for index, row := range rows {
		if row.Seq != uint64(index+1) {
			t.Fatalf("Seq 必须从 1 单调递增：%+v", rows)
		}
	}
	if string(rows[1].Payload) != string(second) {
		t.Fatalf("第二行不是落进去的那一条：%s", rows[1].Payload)
	}
	// append-only 通道不做删除：调用方给的列表短于盘上时，盘上的历史原样保留。
	if head, _, err = store.commitCompactionRecords(key, nil); err != nil || head.Count != 2 {
		t.Fatalf("空列表不得截断已发布历史：head=%+v err=%v", head, err)
	}
}

func TestCompactionRecordsRewritesOnDivergentPrefix(t *testing.T) {
	store, key := messageFixture(t, 0)
	original := compactionPayload(t, 1, false)
	if _, _, err := store.commitCompactionRecords(key, []json.RawMessage{original}); err != nil {
		t.Fatal(err)
	}
	// 内存列表被别的事实源重建过：第 1 条不再是盘上那条。直接追加会把两份互不
	// 衔接的历史缝成一条（读者看到两条"第一次压缩"），因此必须整份重写。
	rebuilt := compactionPayload(t, 1, true)
	third := compactionPayload(t, 2, false)
	if _, added, err := store.commitCompactionRecords(key, []json.RawMessage{rebuilt, third}); err != nil || added != 2 {
		t.Fatalf("前缀不符应整份重写：added=%d err=%v", added, err)
	}
	rows, err := store.readCompactionRecords(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || string(rows[0].Payload) != string(rebuilt) || string(rows[1].Payload) != string(third) {
		t.Fatalf("重写后应只剩调用方给的那两条：%+v", rows)
	}
}

func TestCompactionRecordsHeadRebuildsFromData(t *testing.T) {
	store, key := messageFixture(t, 0)
	if _, _, err := store.commitCompactionRecords(key, []json.RawMessage{
		compactionPayload(t, 1, false), compactionPayload(t, 2, false),
	}); err != nil {
		t.Fatal(err)
	}
	// head 写坏（半截字节）：读侧自愈按数据文件重建，压缩记录不会因一次 head
	// 损坏整条消失。
	if err := os.WriteFile(store.modulePath(key, moduleCompaction), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := store.readCompactionRecords(key)
	if err != nil {
		t.Fatalf("head 损坏后读回: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("head 自愈后应仍能读回 2 行：%+v", rows)
	}
	head, err := store.readCompactionHeadLocked(key)
	if err != nil {
		t.Fatalf("重建后的 head: %v", err)
	}
	if head.Count != 2 || head.LastChecksum == "" {
		t.Fatalf("重建后的 head 水位应与数据文件一致：%+v", head)
	}
}
