package sessionstore

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRetentionPrefixEvictionKeepsRewrittenShard 回归（小分片淘汰前缀）：
// 新分片按「存活行首末 seq」命名，淘汰前缀后可能与**旧片同名**——30 行 / 每片
// 10 行，淘汰 1..20 后存活行 21..30 写成的新片名 message_21_30.jsonl 正好等于
// 旧末片名。
//
// 缺陷形态（修复前）：旧片清扫只按 oldShards 名单 os.Remove，把刚发布的新片
// 一并删掉；而"分片文件不存在"在读侧被当作"该片无行"（readMessageRowsFileAt
// 的 LRU 容忍），于是 head 说 TotalRows=10、读者却读到 0 行——正文无声消失，
// 且 verify 之外没有任何报错。
//
// 本测试以「head 引用的每个分片文件都必须存在」+「可见行数 = head.TotalRows」
// 两条判据钉住它。
func TestRetentionPrefixEvictionKeepsRewrittenShard(t *testing.T) {
	store, key := messageFixture(t, 10)
	buildMessageRows(t, store, key, 30, 10) // 3 片：1..10 / 11..20 / 21..30

	retention, err := store.lRUDelete(key, 20, true)
	if err != nil {
		t.Fatalf("lru delete: %v", err)
	}
	if retention.WatermarkSeq != 20 {
		t.Fatalf("watermark = %d, want 20", retention.WatermarkSeq)
	}
	head, err := store.readMessageHead(key)
	if err != nil {
		t.Fatal(err)
	}
	if head.LastSeq != 30 || head.TotalRows != 10 {
		t.Fatalf("head last=%d total=%d, want last=30 total=10", head.LastSeq, head.TotalRows)
	}
	if len(head.Shards) != 1 || head.Shards[0].Path != "message_21_30.jsonl" {
		t.Fatalf("淘汰后分片索引 = %+v, want 单片 message_21_30.jsonl", head.Shards)
	}
	// 判据一：head 引用的分片必须真实存在（缺片读被当作"无行"，不会报错）。
	for _, shard := range head.Shards {
		if _, statErr := os.Stat(filepath.Join(store.messageDir(key), shard.Path)); statErr != nil {
			t.Fatalf("head 引用的分片不存在: %s (%v)", shard.Path, statErr)
		}
	}
	// 判据二：可见行数 = head.TotalRows，且首行落在淘汰水位之上。
	rows, err := store.readAllRows(key)
	if err != nil {
		t.Fatal(err)
	}
	if uint64(len(rows)) != head.TotalRows {
		t.Fatalf("可见行数 %d != head.TotalRows %d（正文被静默吃掉）", len(rows), head.TotalRows)
	}
	if len(rows) != 10 || rows[0].Seq != 21 || rows[len(rows)-1].Seq != 30 {
		t.Fatalf("淘汰后可见行 %d 行（首 %d 末 %d）, want 10 行（21..30）",
			len(rows), rows[0].Seq, rows[len(rows)-1].Seq)
	}
	// 判据三：自校验面（分片行数/区间/总数与 head 一致）通过。
	if err := store.verifyMessage(key); err != nil {
		t.Fatalf("verifyMessage: %v", err)
	}
}
