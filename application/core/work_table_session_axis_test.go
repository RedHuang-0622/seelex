package core

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// 归属会话必须从注册表记录透传到 WorkItem（GUI「会话」列与「仅本会话」筛选
// 的数据面）；漏掉就是全局台账里"行看得见、会话判不出"。
func TestWorkItemCarriesOwningSession(t *testing.T) {
	rows := buildWorkTable(nil, []dto.TaskRecord{{
		ID: "task:1", Phase: dto.TaskPhaseTask, Task: "a", Kind: "task", SessionID: "sess-a",
	}}, nil)
	if len(rows) != 1 || rows[0].SessionID != "sess-a" {
		t.Fatalf("WorkItem = %+v, want SessionID=sess-a", rows)
	}
}

// 全局台账行带归属会话：实时注册表 = 当前会话，后台 scope 分区 = 分区键。
func TestGlobalWorkTableCarriesOwningSession(t *testing.T) {
	runtime := &fakeRuntime{currentTaskSession: "session-live"}
	runtime.tasks = map[string]dto.TaskRecord{
		"task:a": {ID: "task:a", Key: "k-a", Phase: dto.TaskPhaseTask, Task: "当前会话的任务", Kind: "task"},
	}
	runtime.sessionTaskSnapshots = map[string][]dto.TaskRecord{
		"session-bg": {{ID: "task:b", Key: "k-b", Phase: dto.TaskPhaseTask, Task: "后台会话的任务", Kind: "task"}},
	}

	rows := buildWorkTable(nil, runtime.TaskSnapshot(), nil)
	sessions := make(map[string]string, len(rows))
	for _, row := range rows {
		sessions[row.ID] = row.SessionID
	}
	if len(rows) != 2 {
		t.Fatalf("全局工作表格 = %+v, want 两行（当前会话 + 后台分区）", rows)
	}
	if sessions["task:a"] != "session-live" {
		t.Fatalf("task:a 归属会话 = %q, want session-live（rows=%+v）", sessions["task:a"], rows)
	}
	if sessions["task:b"] != "session-bg" {
		t.Fatalf("task:b 归属会话 = %q, want session-bg（rows=%+v）", sessions["task:b"], rows)
	}
}

// 冷读合并：磁盘记录不带会话标记（归属由 SessionRecord 容器表达），合并时按
// 该会话补标记，且不覆盖全局表带来的标记（primary 优先）。
func TestMergeTaskRecordsStampsSecondarySession(t *testing.T) {
	primary := []dto.TaskRecord{{ID: "task:1", Key: "k1", Task: "实时", SessionID: "sess-live"}}
	secondary := []dto.TaskRecord{
		{ID: "task:2", Key: "k2", Task: "磁盘"},
		{ID: "task:1", Key: "k1", Task: "磁盘旧副本"},
	}

	merged := mergeTaskRecords(primary, secondary, "sess-cold")
	if len(merged) != 2 {
		t.Fatalf("merged = %+v, want 两行（同身份去重）", merged)
	}
	byID := make(map[string]dto.TaskRecord, len(merged))
	for _, record := range merged {
		byID[record.ID] = record
	}
	if byID["task:1"].SessionID != "sess-live" || byID["task:1"].Task != "实时" {
		t.Fatalf("primary 记录被改写：%+v", byID["task:1"])
	}
	if byID["task:2"].SessionID != "sess-cold" {
		t.Fatalf("磁盘记录未补归属会话：%+v", byID["task:2"])
	}
}
