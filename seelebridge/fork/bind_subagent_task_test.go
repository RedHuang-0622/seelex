package fork

import (
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge/task"
)

// TestBindSubagentTaskIdempotent 验证 B6 装配件：相同 goal 的子代理绑定同一
// task（幂等），第二个子代理作为参与者挂到同一 task。
func TestBindSubagentTaskIdempotent(t *testing.T) {
	registry := task.NewTaskRegistry()
	defer registry.Close()
	tool := NewTool(Deps{
		// 会话维度的读写面：本用例只关心幂等，把 sessionID 吸收掉后落到同一张
		// 注册表（生产用 Runtime 的 For 入口，按会话分 scope）。
		TaskResolveByKeyFor: func(_ string, key string) (task.TaskRecord, bool, error) {
			return registry.ResolveByKey(key)
		},
		TaskAddFor: func(_ string, spec task.TaskSpec) (task.TaskRecord, bool, error) {
			return registry.Add(spec)
		},
		TaskSetStatusFor: func(_ string, id string, status task.TaskStatus, evidence string) (task.TaskRecord, error) {
			return registry.SetStatus(id, status, evidence)
		},
		TaskAttachParticipant: registry.AttachParticipant,
	})
	first := tool.bindSubagentTask("session-a", SubagentSpec{ID: "s1", Goal: "分析作者画像"})
	second := tool.bindSubagentTask("session-a", SubagentSpec{ID: "s2", Goal: "分析作者画像"})
	if first == "" || first != second {
		t.Fatalf("same goal must bind same task: first=%q second=%q", first, second)
	}
	records := registry.Snapshot()
	if len(records) != 1 {
		t.Fatalf("tasks = %+v, want 1（同一 task 不重复建条目）", records)
	}
	record := records[0]
	if record.Status != task.TaskQueued {
		t.Fatalf("task status after bind = %v, want queued（会话未启动前不显示 running）", record.Status)
	}
	if len(record.Participants) != 0 || record.Assignee != "" {
		t.Fatalf("participants/assignee = %v / %q, want empty（被动认领在子代理会话注册后发生）", record.Participants, record.Assignee)
	}
}

// TestBindSubagentTaskRoutesByOwningSession 钉住归属会话被**一路带到写入口**：
// 后台会话里 fork 的子代理，其台账行的查重/新建/状态打点都必须落在该会话的
// scope（否则行会写进当前视图会话的注册表，工作表格会话轴张冠李戴）。
func TestBindSubagentTaskRoutesByOwningSession(t *testing.T) {
	const owner = "session-bg"
	var resolved, added, statusSet string
	tool := NewTool(Deps{
		TaskResolveByKeyFor: func(sessionID, _ string) (task.TaskRecord, bool, error) {
			resolved = sessionID
			return task.TaskRecord{}, false, nil
		},
		TaskAddFor: func(sessionID string, spec task.TaskSpec) (task.TaskRecord, bool, error) {
			added = sessionID
			return task.TaskRecord{ID: spec.ID, Key: spec.Key, Status: task.TaskPending}, true, nil
		},
		TaskSetStatusFor: func(sessionID, _ string, _ task.TaskStatus, _ string) (task.TaskRecord, error) {
			statusSet = sessionID
			return task.TaskRecord{}, nil
		},
	})

	if id := tool.bindSubagentTask(owner, SubagentSpec{ID: "s1", Goal: "g"}); id != "subagent:s1" {
		t.Fatalf("bound task id = %q, want subagent:s1", id)
	}
	if resolved != owner || added != owner || statusSet != owner {
		t.Fatalf("归属会话未一路带到写入口：resolve=%q add=%q status=%q want %q",
			resolved, added, statusSet, owner)
	}
}
