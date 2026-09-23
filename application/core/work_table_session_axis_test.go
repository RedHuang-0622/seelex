package core

import (
	"encoding/json"
	"testing"
	"time"

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

// 增量面：task.changed 单行必须带归属会话（与整表路径 taskSnapshotAll 同源
// 补齐）。实时注册表记录本身不带 SessionID，若增量原样下发，前端按 task_id
// 整行替换（protocol.js）后该行 session_id 变空，「仅本会话」筛选当场丢掉
// 正在运行的行——子代理任务每状态迁移都发增量，最易撞上。
func TestTaskChangedIncrementCarriesOwningSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	sessionID := service.Snapshot().Session.ID
	if sessionID == "" {
		t.Fatal("view session has no ID")
	}
	subscription := service.Subscribe(16)
	defer subscription.Close()

	// 记录本身不带会话键（实时注册表语义）。
	service.publishTaskChanged(dto.TaskRecord{
		ID: "task:sub-1", Key: "subagent:node-1", Phase: dto.TaskPhaseTask, Task: "跑子代理",
		Status: dto.TaskRunning, Kind: "subagent",
	}, 1, "req-sub", sessionID)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-subscription.Events:
			if event.Kind != EventTaskChanged {
				continue
			}
			var payload TaskChangedEvent
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatalf("decode task.changed: %v", err)
			}
			if payload.TaskID != "task:sub-1" {
				continue
			}
			if payload.Task.SessionID != sessionID {
				t.Fatalf("BUG REPRO: task.changed 行归属会话 = %q, want %q（前端按会话筛选会丢掉该行）",
					payload.Task.SessionID, sessionID)
			}
			return
		case <-deadline:
			t.Fatal("timeout waiting for task.changed")
		}
	}
}
