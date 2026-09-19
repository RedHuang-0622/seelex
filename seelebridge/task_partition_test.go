package seelebridge

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// TestTaskAddForRoutesToOwningSessionPartition 验证 task 写按归属会话路由 +
// 工作表格的**项目/全局**读面：
//   - 全局表（TaskSnapshot，worktable 投影数据源）是跨会话台账，切到 B 后
//     仍看得到 A 的条目（工作表格不是会话粒度）；
//   - 会话级读面（TaskSnapshotFor）仍按会话取数：B 的 scope 不含 A 的条目，
//     A 的 scope 实时一致（后台写自有域）。
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

	// 工作表格（全局台账）看得到 A 的两条——切到 B 不丢行。
	if got := runtime.TaskSnapshot(); len(got) != 2 {
		t.Fatalf("global work table = %+v, want A's plan:n1 + plan:n2", got)
	}
	// 会话级读面按会话取数：B 的 scope 不含 A 的条目。
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
