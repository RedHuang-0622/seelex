package seelebridge

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// ── 残留风险红灯：跨会话台账的行身份必须**进程内唯一** ────────────────
//
// 工作表格是跨会话台账（`TaskSnapshot` = 实时注册表 + 各会话 scope 分区），
// 台账按跨会话身份去重（幂等键优先，否则行 ID）。于是「自动生成的条目 ID」
// 必须是**进程内唯一**的，否则两张表都错：
//
//   - 同 ID 两行：前端按行 ID 做 keyed reconciliation（`data-work-row=row.id`、
//     `task.changed` 单行 upsert），同 ID 是同一行；
//   - 更糟：合并去重会把其中一个会话的行**整个丢掉**（用户看不到它在办什么）。
//
// 现状（缺陷）：分区分配用 `task:<len(records)+1>`（每个会话各自从 1 数），
// 所以两个会话各产一条无显式 ID 的条目就同号。
func TestWorkTableLedgerRowIDsUniqueAcrossSessions(t *testing.T) {
	runtime, err := NewRuntime(RuntimeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	// 当前会话（实时注册表）与两个后台会话（各自 scope 分区）。
	runtime.SwitchSessionTasks("session-live", nil)

	live, _, err := runtime.TaskAdd(dto.TaskSpec{Phase: dto.TaskPhaseTask, Task: "当前会话的无键任务", Kind: "task"})
	if err != nil {
		t.Fatal(err)
	}
	backgroundA, _, err := runtime.TaskAddFor("session-a", dto.TaskSpec{Phase: dto.TaskPhaseTask, Task: "A 的无键任务", Kind: "task"})
	if err != nil {
		t.Fatal(err)
	}
	backgroundB, _, err := runtime.TaskAddFor("session-b", dto.TaskSpec{Phase: dto.TaskPhaseTask, Task: "B 的无键任务", Kind: "task"})
	if err != nil {
		t.Fatal(err)
	}

	ids := map[string]string{live.ID: live.Task, backgroundA.ID: backgroundA.Task, backgroundB.ID: backgroundB.Task}
	if len(ids) != 3 {
		t.Fatalf("自动分配的条目 ID 跨会话重号：live=%q A=%q B=%q", live.ID, backgroundA.ID, backgroundB.ID)
	}

	// 台账（全局读面）必须三行都在：同 ID 会被去重丢掉。
	table := runtime.TaskSnapshot()
	if len(table) != 3 {
		t.Fatalf("跨会话台账丢行：table=%+v（want live + A + B 三行）", table)
	}
	rows := map[string]int{}
	for _, record := range table {
		rows[record.ID]++
	}
	for id := range ids {
		if rows[id] == 0 {
			t.Fatalf("台账缺行 %q：table=%+v", id, table)
		}
	}
}

// 装载（会话切换/磁盘恢复）进来的记录占用的号，不得被后续新条目重发——
// 重发在注册表里就是**覆盖**（`state.tasks[id]` 以 ID 为键，新条目直接写掉
// 旧行），用户的旧任务凭空消失。
func TestWorkTableLedgerNeverReusesRestoredRowID(t *testing.T) {
	runtime, err := NewRuntime(RuntimeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	// 装载一条“来自磁盘/上一次进程”的记录，它的号是分配器的小号。
	const restoredID = "task:1"
	runtime.SwitchSessionTasks("session-a", []dto.TaskRecord{{
		ID: restoredID, Key: "goal:from-disk", Phase: dto.TaskPhaseTask,
		Task: "磁盘上的旧任务", Status: dto.TaskPending, Kind: "task",
	}})

	created, _, err := runtime.TaskAdd(dto.TaskSpec{Phase: dto.TaskPhaseTask, Task: "新任务", Kind: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == restoredID {
		t.Fatalf("新条目重用了被装载记录占用的 ID %q（会覆盖旧行）", restoredID)
	}

	table := runtime.TaskSnapshot()
	if len(table) != 2 {
		t.Fatalf("台账 = %+v，want 装载行 + 新行", table)
	}
	var found bool
	for _, record := range table {
		if record.ID == restoredID {
			found = true
			if record.Task != "磁盘上的旧任务" {
				t.Fatalf("装载行被覆盖：%+v", record)
			}
		}
	}
	if !found {
		t.Fatalf("装载行从台账消失：%+v", table)
	}
}
