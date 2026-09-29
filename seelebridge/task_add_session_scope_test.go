package seelebridge

import (
	"context"
	"strings"
	"testing"

	seetelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
)

// ── taskadd 的会话归属（2026-09-29 与子代理树同一批收口）──────────────────
//
// 工具写的是**调用它的那个会话**的 scope，不是实时注册表（视图会话）。子代理
// 看得见 task_add/todo 工具族（只排除 plan 族与 task 终态工具），而子代理可能在
// 一个**后台会话**里跑：若工具走无会话的 TaskAdd，行就落进"当前视图会话"的注册表
// ——工作表格的会话轴（前端「仅本会话」/「实发」按 row.session_id 取值）当场
// 张冠李戴：行属于谁取决于"谁在看"，而不是"谁在办"。
func TestTaskAddToolWritesToCallingSessionScope(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	// 视图会话 = session-live；调用来自后台会话 session-a（子代理/并行会话）。
	runtime.SwitchSessionTasks("session-live", nil)
	const goal = "后台会话的子代理任务"

	ctx := seetelemetry.WithSessionID(context.Background(), "session-a")
	if _, err := runtime.Agent().DirectDispatch(ctx, "task_add", `{"goal":"`+goal+`"}`); err != nil {
		t.Fatal(err)
	}

	// 视图会话的表格不得出现这条行：它属于 session-a。
	for _, record := range runtime.TaskSnapshotFor("session-live") {
		if record.Task == goal {
			t.Fatalf("跨会话污染：后台会话的 taskadd 行落进了视图会话注册表：%+v", record)
		}
	}
	found := false
	for _, record := range runtime.TaskSnapshotFor("session-a") {
		if record.Task == goal {
			found = true
		}
	}
	if !found {
		t.Fatalf("session-a 的 scope 丢了自己的行：%+v", runtime.TaskSnapshotFor("session-a"))
	}

	// 无 ctx 会话归属（旧调用面）仍按实时注册表判：空键 = 当前会话。
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "task_add", `{"goal":"无归属会话的行"}`); err != nil {
		t.Fatal(err)
	}
	live := false
	for _, record := range runtime.TaskSnapshotFor("session-live") {
		if record.Task == "无归属会话的行" {
			live = true
		}
	}
	if !live {
		t.Fatalf("空会话键必须落实时注册表（旧调用面口径）：%+v", runtime.TaskSnapshotFor("session-live"))
	}
}

// TestTodoToolsWriteToCallingSessionScope 钉住清单工具族（todo_init/add/done/
// status）的会话归属：子代理/后台会话里的 todo_init 只换**自己那个会话**的清单，
// 不顶掉视图会话的清单，行也带自己的会话号（工作表格会话轴）。
func TestTodoToolsWriteToCallingSessionScope(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	runtime.SwitchSessionTasks("session-live", nil)

	liveCtx := seetelemetry.WithSessionID(context.Background(), "session-live")
	bgCtx := seetelemetry.WithSessionID(context.Background(), "session-bg")

	if _, err := runtime.Agent().DirectDispatch(liveCtx, "todo_init", `{"items":["视图会话的待办"]}`); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Agent().DirectDispatch(bgCtx, "todo_init", `{"items":["后台会话的待办"]}`); err != nil {
		t.Fatal(err)
	}

	liveItems := runtime.TodoSnapshotFor("session-live")
	if len(liveItems) != 1 || liveItems[0].Text != "视图会话的待办" {
		t.Fatalf("视图会话的清单被别的会话顶掉：%+v", liveItems)
	}
	bgItems := runtime.TodoSnapshotFor("session-bg")
	if len(bgItems) != 1 || bgItems[0].Text != "后台会话的待办" {
		t.Fatalf("后台会话自己的清单：%+v", bgItems)
	}

	// 台账里后台会话的清单行必须带自己的会话号（缺键/错键都会被前端当成本会话）。
	found := false
	for _, record := range runtime.TaskSnapshot() {
		if record.Kind != "todo" || record.Task != "后台会话的待办" {
			continue
		}
		found = true
		if record.SessionID != "session-bg" {
			t.Fatalf("后台会话的清单行归属 = %q, want session-bg", record.SessionID)
		}
	}
	if !found {
		t.Fatalf("台账里没有后台会话的清单行：%+v", runtime.TaskSnapshot())
	}

	// todo_status 读的是调用会话自己的清单。
	out, err := runtime.Agent().DirectDispatch(bgCtx, "todo_status", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "后台会话的待办") || strings.Contains(out, "视图会话的待办") {
		t.Fatalf("后台会话读到的清单不对：%s", out)
	}

	// todo_done 也落在调用会话：后台会话完成第 0 项，视图会话的清单不受影响。
	if _, err := runtime.Agent().DirectDispatch(bgCtx, "todo_done", `{"index":0}`); err != nil {
		t.Fatal(err)
	}
	if items := runtime.TodoSnapshotFor("session-bg"); len(items) != 1 || !items[0].Done {
		t.Fatalf("后台会话的清单项未落定：%+v", items)
	}
	if items := runtime.TodoSnapshotFor("session-live"); len(items) != 1 || items[0].Done {
		t.Fatalf("视图会话的清单被后台会话的状态更新污染：%+v", items)
	}
}
