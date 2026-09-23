//go:build redprobe

// v8.3 设计稿符合度红灯探针（打点表 §5/§7 引用；修复后应转绿）。
//
//  1. TestProbeForkEventConstantCommitID：fork_store.go 用常量 commit_id 提交父侧
//     fork EVENT → A.2 规则 3 把同父会话第二次 fork 的事件整次吞掉。违反 §2.0
//     规则 4「稳定 commit_id = 逻辑操作标识」。已修（D13：逐操作唯一凭据），
//     本探针留在仓库作为回归钉子。
//
// 已退役的探针（**2026-09-23，随 A3 专项收口**）：
//
//	TestProbeStackStatusUpdateUnpublishedInvisible —— 它钉的是「未发布的状态迁移
//	必须冷重载后不可见」，前提是把 active.jsonl 当归追加型通道用戳号做闸门。
//	该口径已按 D12/§2.0 通道类型表修订：active.jsonl 是**整份替换型**（每次提交
//	整文件原子替换，文件不存在「已 append 未发布」中间态，戳号只是标签），因此
//	「head 回退后数据文件的变更照样可见」是**正确语义**而不是缺陷（H9 重新定性）。
//	语义断言由常规测试 `channel_semantics_test.go` 的 T-STK-13
//	（TestStackChannelActiveWholeReplacement）承担；探针留着只会让
//	`go test -tags redprobe ./sessionstore` 长期红灯（运行期失败，不是构建失败）。
package sessionstore

import (
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
