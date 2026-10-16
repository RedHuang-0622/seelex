package task

import "testing"

// ── 残留风险红灯：自动条目 ID 的进程内唯一性（注册表层）──────────────
//
// 自动 ID（`task:<n>` / `todo:<n>`）必须**进程内不重发**：注册表的行键就是
// ID，重发等于新条目覆盖旧行；跨会话台账也会出现同 ID 两行（前端 keyed
// reconciliation 只能认一行）。磁盘恢复/会话切换把外部号带进注册表，因此
// 分配器要看得住这些号。
func TestRegistryAutoIDsNeverReuseRestoredIDs(t *testing.T) {
	registry := NewTaskRegistry()
	defer registry.Close()

	// 磁盘/上一次进程留下的记录（号在分配器水位之内）。
	if err := registry.Restore("task:1", TaskRecord{
		ID: "task:1", Key: "goal:from-disk", Phase: TaskPhaseTask, Task: "磁盘旧任务", Kind: "task",
	}); err != nil {
		t.Fatal(err)
	}
	// 旧待办：todo 行 ID 同样不得重发（重发会让 TodoSnapshot 出现同一条两行）。
	if err := registry.Restore("todo:2", TaskRecord{
		ID: "todo:2", Key: "todo:旧待办", Phase: TaskPhaseTasklist, Task: "旧待办", Kind: "todo",
	}); err != nil {
		t.Fatal(err)
	}

	created, _, err := registry.Add(TaskSpec{Phase: TaskPhaseTask, Task: "新任务", Kind: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "task:1" {
		t.Fatalf("新条目重用了装载记录占用的 ID %q", created.ID)
	}
	if err := registry.AppendTodo(TodoItem{Text: "新待办"}, 10); err != nil {
		t.Fatal(err)
	}

	records := registry.Snapshot()
	seen := map[string]int{}
	for _, record := range records {
		seen[record.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Fatalf("注册表出现同 ID 两行：%q × %d（records=%+v）", id, count, records)
		}
	}
	if len(records) != 4 {
		t.Fatalf("注册表 = %+v，want 装载 task + 装载 todo + 新 task + 新 todo", records)
	}
	todo := registry.TodoSnapshot()
	if len(todo) != 2 || todo[0].ID == todo[1].ID {
		t.Fatalf("待办清单 = %+v，want 两条不同 ID 的条目", todo)
	}
	for _, record := range todo {
		if record.Task == "旧待办" && record.ID != "todo:2" {
			t.Fatalf("装载的待办被覆盖：%+v", record)
		}
	}
}
