package seelebridge

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// ── 红灯复现：工作表格的内容是「项目/全局」粒度，不是会话粒度 ──────────
//
// 工作表格（worktable）是**一条跨会话台账**：plan / todo / task / subagent
// 四类生产者产出的条目共处同一张表（`TaskSnapshot` 是 worktable 投影数据
// 源）。会话只是条目的**产生地**，不是表格的**作用域**——切走会话后，先前
// 的条目仍应在表里可见（用户看的是"这个项目在办什么"，不是"这个会话在办
// 什么"）。
//
// 现状（缺陷）：`SwitchSessionTasks` 把离开会话的注册表整表搬到分区、把
// 目标会话的分区装回实时注册表（`ReplaceAll` = 换会话即换表），于是
// `TaskSnapshot()` 只反映"当前会话"——切走即丢，工作表格被做成了会话粒度。
func TestWorkTableGlobalScopeRepro(t *testing.T) {
	runtime, err := NewRuntime(RuntimeConfig{})
	if err != nil {
		t.Fatal(err)
	}

	// 会话 A：四类生产者各产出一条。
	runtime.SwitchSessionTasks("session-a", nil)
	if err := runtime.tasks.ReplaceTodo([]dto.TodoItem{{Text: "A 的待办"}}); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []dto.TaskSpec{
		{Key: "plan:n1", Phase: dto.TaskPhasePlan, Task: "A 的 plan 节点", Kind: "plan", SourceID: "n1"},
		{Key: "task:1", Phase: dto.TaskPhaseTask, Task: "A 的主动任务", Kind: "task"},
		{Key: "subagent:s1", Phase: dto.TaskPhaseSubagent, Task: "A 的子代理", Kind: "subagent", SourceID: "s1"},
	} {
		if _, created, err := runtime.TaskAdd(spec); err != nil || !created {
			t.Fatalf("seed %+v: created=%v err=%v", spec, created, err)
		}
	}

	// 切到会话 B（同一进程 / 同一项目）。
	runtime.SwitchSessionTasks("session-b", nil)

	// 工作表格（全局读面）必须仍然看得到 A 的四类条目。
	kinds := map[string]bool{}
	for _, record := range runtime.TaskSnapshot() {
		kinds[record.Kind] = true
	}
	for _, kind := range []string{"plan", "todo", "task", "subagent"} {
		if !kinds[kind] {
			t.Fatalf("工作表格是会话粒度：切到 session-b 后 %q 条目从全局表消失（table=%+v）",
				kind, runtime.TaskSnapshot())
		}
	}
}

// 会话级读面仍按会话取数（持久化/上下文打点用），全局读面不改变它：
// TaskSnapshotFor(B) 只含 B 的条目，TaskSnapshotFor(A) 只含 A 的条目。
func TestWorkTableGlobalReadKeepsSessionReadScoped(t *testing.T) {
	runtime, err := NewRuntime(RuntimeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	runtime.SwitchSessionTasks("session-a", nil)
	if _, _, err := runtime.TaskAddFor("session-a", dto.TaskSpec{Key: "plan:n1", Phase: dto.TaskPhasePlan, Task: "A", Kind: "plan", SourceID: "n1"}); err != nil {
		t.Fatal(err)
	}
	runtime.SwitchSessionTasks("session-b", nil)
	if _, _, err := runtime.TaskAddFor("session-b", dto.TaskSpec{Key: "plan:n2", Phase: dto.TaskPhasePlan, Task: "B", Kind: "plan", SourceID: "n2"}); err != nil {
		t.Fatal(err)
	}

	for _, sessionID := range []string{"session-a", "session-b"} {
		records := runtime.TaskSnapshotFor(sessionID)
		if len(records) != 1 {
			t.Fatalf("TaskSnapshotFor(%q) = %+v, want exactly its own row", sessionID, records)
		}
	}
}
