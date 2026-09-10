//go:build redprobe

// v8.3 设计稿符合度红灯探针（打点表 §5/§7 引用；修复后应转绿）。
//
//  1. TestProbeForkEventConstantCommitID：fork_store.go:126 用常量 commit_id
//     提交父侧 fork EVENT → A.2 规则 3 把同父会话第二次 fork 的事件整次吞掉。
//     违反 §2.0 规则 4「稳定 commit_id = 逻辑操作标识」。
//  2. TestProbeStackStatusUpdateUnpublishedInvisible：active.jsonl 是整文件
//     重写且状态迁移不重盖 revision → head 发布失败后，未发布的内容变更在冷
//     重载后照样可见（已发布态被原地覆盖且无回退副本）。违反 §2.0 规则 1
//     「append 未发布 = 未提交」。
package sessionstore

import (
	"os"
	"strconv"
	"testing"
)

func TestProbeForkEventConstantCommitID(t *testing.T) {
	store := newStoreEngine(t.TempDir(), storageSettings{})
	parent := Key{ProjectID: "p", SessionID: "parent"}
	if _, err := store.messageCommit(parent, "", []Event{
		{Role: "user", Content: "a"}, {Role: "assistant", Content: "b"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, child := range []string{"child-1", "child-2"} {
		if err := store.forkSession(parent, Key{ProjectID: "p", SessionID: child}, 2); err != nil {
			t.Fatal(err)
		}
	}
	events, err := store.readEvents(parent, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	marks := make([]string, 0, len(events))
	for _, row := range events {
		marks = append(marks, string(row.Kind)+"#"+row.CommitID)
	}
	t.Logf("父会话 EVENT rows=%d kinds=%v", len(events), marks)
	if len(events) < 2 {
		t.Fatalf("两次 fork 只剩 %d 行 EVENT：第二次被常量 commit_id 判成重复提交", len(events))
	}
}

func TestProbeStackStatusUpdateUnpublishedInvisible(t *testing.T) {
	harness := newJSONStackHarness(t)
	store, key := harness.store, harness.key
	harness.seedMessages(1)
	if _, err := harness.push(StackKindTask, "b1", StackItemInput{ItemID: "A"}); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.push(StackKindTask, "b1", StackItemInput{ItemID: "B"}); err != nil {
		t.Fatal(err)
	}
	headPath := store.modulePath(key, moduleForStackKind(StackKindTask))
	published, err := os.ReadFile(headPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := harness.setStatus(StackKindTask, "A", "review"); err != nil {
		t.Fatal(err)
	}
	// 该次提交的 head 发布失败：数据文件已落盘，head 停在上一份已发布态。
	if err := os.WriteFile(headPath, published, 0o600); err != nil {
		t.Fatal(err)
	}
	store.dropStackViews(key)

	head := harness.head(StackKindTask)
	active := harness.active(StackKindTask)
	rows := make([]string, 0, len(active))
	publishedStatus := ""
	for _, row := range active {
		rows = append(rows, row.ItemID+":"+row.Status+"@rev"+strconv.FormatUint(row.Revision, 10))
		if row.ItemID == "A" {
			publishedStatus = row.Status
		}
	}
	t.Logf("head_seq=%d kinds=%v / 冷重载 active=%v", head.HeadSeq, head.Kinds, rows)
	if publishedStatus == "review" {
		t.Fatalf("未发布的状态迁移变得可见：active=%v（head_seq=%d）", rows, head.HeadSeq)
	}
}
