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
		TaskResolveByKey:      registry.ResolveByKey,
		TaskAdd:               registry.Add,
		TaskSetStatus:         registry.SetStatus,
		TaskAttachParticipant: registry.AttachParticipant,
	})
	first := tool.bindSubagentTask(SubagentSpec{ID: "s1", Goal: "分析作者画像"})
	second := tool.bindSubagentTask(SubagentSpec{ID: "s2", Goal: "分析作者画像"})
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
	if len(record.Participants) != 2 || record.Participants[0] != "s1" || record.Participants[1] != "s2" {
		t.Fatalf("participants = %v, want [s1 s2]", record.Participants)
	}
}
