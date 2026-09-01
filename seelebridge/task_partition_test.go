package seelebridge

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// TestTaskAddForRoutesToOwningSessionPartition 验证 task 写按归属会话路由：
// 后台会话 A 的写只进 A 的 scope 分区，当前注册表（B）不被污染；
// A 的 scope 实时一致（TaskSnapshotFor(A) 能看到后台新增任务）。
func TestTaskAddForRoutesToOwningSessionPartition(t *testing.T) {
	runtime, err := NewRuntime(RuntimeConfig{})
	if err != nil {
		t.Fatal(err)
	}

	// 当前任务会话 = session-b
	runtime.SwitchSessionTasks("session-b", nil)

	// 后台会话 A 写入两个任务（会话域重构：写自有域）
	recordN1, created, err := runtime.TaskAddFor("session-a", dto.TaskSpec{Key: "plan:n1", Phase: dto.TaskPhasePlan, Task: "A 调研", Kind: "plan", SourceID: "n1"})
	if err != nil || !created {
		t.Fatalf("TaskAddFor(A, plan:n1) = created=%v err=%v", created, err)
	}
	recordN2, created, err := runtime.TaskAddFor("session-a", dto.TaskSpec{Key: "plan:n2", Phase: dto.TaskPhasePlan, Task: "A 第二个节点", Kind: "plan", SourceID: "n2"})
	if err != nil || !created {
		t.Fatalf("TaskAddFor(A, plan:n2) = created=%v err=%v", created, err)
	}

	// 当前注册表（B）必须干净
	if got := runtime.TaskSnapshot(); len(got) != 0 {
		t.Fatalf("current registry polluted by session A: %+v", got)
	}
	if got := runtime.TaskSnapshotFor("session-b"); len(got) != 0 {
		t.Fatalf("session B scope polluted by session A: %+v", got)
	}

	// A 的 scope 实时一致
	aRecords := runtime.TaskSnapshotFor("session-a")
	if len(aRecords) != 2 {
		t.Fatalf("session A scope = %+v, want plan:n1 + plan:n2", aRecords)
	}

	// 幂等：重复登记命中既有记录
	if _, created, err := runtime.TaskAddFor("session-a", dto.TaskSpec{Key: "plan:n1", Phase: dto.TaskPhasePlan, Task: "A 调研", Kind: "plan", SourceID: "n1"}); err != nil || created {
		t.Fatalf("TaskAddFor duplicate = created=%v err=%v, want idempotent", created, err)
	}

	// 状态更新路由到 A 的 scope
	if _, err := runtime.TaskSetStatusFor("session-a", recordN2.ID, dto.TaskCompleted, "node:completed"); err != nil {
		t.Fatalf("TaskSetStatusFor(A): %v", err)
	}
	found := false
	for _, record := range runtime.TaskSnapshotFor("session-a") {
		if record.ID == recordN2.ID && record.Status == dto.TaskCompleted {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("session A scope status update missing: %+v", runtime.TaskSnapshotFor("session-a"))
	}
	if recordN1.ID == recordN2.ID {
		t.Fatalf("partition ids collided: %q", recordN1.ID)
	}
}
