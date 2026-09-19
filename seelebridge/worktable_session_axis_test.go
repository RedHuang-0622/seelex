package seelebridge

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// ── 会话筛选轴的数据面：全局台账每行必须能判定归属会话 ────────────────
//
// 工作表格是跨会话台账（全局读面默认全量），但「仅本会话」要能用——前提是
// 合并后的行带**归属会话**。运行时的标注规则：实时注册表的记录归当前任务
// 会话，切换保存的分区记录归分区键；行 ID 不是会话（自动号进程级唯一，跟
// 会话无关）。
func TestTaskSnapshotTagsOwningSession(t *testing.T) {
	runtime, err := NewRuntime(RuntimeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	runtime.SwitchSessionTasks("session-live", nil)

	live, _, err := runtime.TaskAdd(dto.TaskSpec{Phase: dto.TaskPhaseTask, Task: "当前会话", Kind: "task"})
	if err != nil {
		t.Fatal(err)
	}
	background, _, err := runtime.TaskAddFor("session-a", dto.TaskSpec{Phase: dto.TaskPhaseTask, Task: "后台会话", Kind: "task"})
	if err != nil {
		t.Fatal(err)
	}

	assertSessions := func(stage string, want map[string]string) {
		t.Helper()
		table := runtime.TaskSnapshot()
		got := make(map[string]string, len(table))
		for _, record := range table {
			got[record.ID] = record.SessionID
		}
		for id, wantSession := range want {
			if got[id] != wantSession {
				t.Fatalf("%s：记录 %q 的归属会话 = %q, want %q（table=%+v）", stage, id, got[id], wantSession, table)
			}
		}
		if len(table) != len(want) {
			t.Fatalf("%s：全局台账 = %+v, want %d 条", stage, table, len(want))
		}
	}
	assertSessions("当前会话 + 后台分区", map[string]string{
		live.ID:       "session-live",
		background.ID: "session-a",
	})

	// 切走后 live 的记录进 session-live 分区：归属会话不因切换而变（仍是产出
	// 它的会话），否则「仅本会话」在切走后就认不出自己的行了。
	runtime.SwitchSessionTasks("session-b", nil)
	assertSessions("切到 session-b 后", map[string]string{
		live.ID:       "session-live",
		background.ID: "session-a",
	})

	// 会话级读面刻意不带标记：归属由容器 `SessionRecord` 表达，落盘不必把
	// 会话号写进每条记录（读面元数据，不参与持久化语义）。
	if got := runtime.TaskSnapshotFor("session-a"); len(got) != 1 || got[0].ID != background.ID || got[0].SessionID != "" {
		t.Fatalf("TaskSnapshotFor(session-a) = %+v, want 一条且不带 SessionID 标记", got)
	}
}
