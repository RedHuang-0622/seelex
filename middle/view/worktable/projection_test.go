package worktable

import (
	"reflect"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// 身份判据是**注入**的：本包不存在自己的同口径实现（AGENTS §8.3 的同一判据一处）。
// 这条用例把注入函数换成"只看 Task 文本"，合并结果必须跟着它走——证明判据只有
// 宿主给的那一条，本层没有旁路（红灯状态：本用例在签名没有第四个参数前编译不过）。
func TestMergeTaskRecordsUsesInjectedIdentity(t *testing.T) {
	byTaskText := func(record dto.TaskRecord) string { return record.Task }
	primary := []dto.TaskRecord{{ID: "task:1", Key: "k1", Task: "同一件事", SessionID: "sess-live"}}
	secondary := []dto.TaskRecord{
		{ID: "task:9", Key: "k9", Task: "同一件事"},
		{ID: "task:2", Key: "k2", Task: "另一件事"},
	}

	merged := MergeTaskRecords(primary, secondary, "sess-cold", byTaskText)
	if len(merged) != 2 {
		t.Fatalf("merged = %+v, want 两行（注入判据按 Task 文本去重）", merged)
	}
	if merged[0].ID != "task:1" || merged[0].Task != "同一件事" {
		t.Fatalf("primary 优先未生效：%+v", merged[0])
	}
	if merged[1].ID != "task:2" || merged[1].SessionID != "sess-cold" {
		t.Fatalf("secondary 未补归属会话：%+v", merged[1])
	}
}

// 注入判据返回空串 = 该行没有身份：不进合并（防御性分支，宿主判据不会返回空串）。
func TestMergeTaskRecordsSkipsRecordsWithoutIdentity(t *testing.T) {
	noIdentity := func(dto.TaskRecord) string { return "" }
	merged := MergeTaskRecords(
		[]dto.TaskRecord{{ID: "task:1"}},
		[]dto.TaskRecord{{ID: "task:2"}},
		"sess-cold",
		noIdentity,
	)
	if len(merged) != 0 {
		t.Fatalf("merged = %+v, want 空（无身份的行不进合并）", merged)
	}
}

// secondary 为空时原样返回 primary（早返回口径与迁移前一致，不复制切片）。
func TestMergeTaskRecordsReturnsPrimaryWhenSecondaryEmpty(t *testing.T) {
	primary := []dto.TaskRecord{{ID: "task:1", Key: "k1"}}
	merged := MergeTaskRecords(primary, nil, "sess-cold", func(record dto.TaskRecord) string { return record.Key })
	if !reflect.DeepEqual(merged, primary) {
		t.Fatalf("merged = %+v, want 原样返回 primary", merged)
	}
}
