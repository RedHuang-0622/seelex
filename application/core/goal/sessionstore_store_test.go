package goal

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

func newSessionRouterForGoalTest(t *testing.T) *sessionstore.Router {
	t.Helper()
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatalf("new session router: %v", err)
	}
	t.Cleanup(func() { _ = router.Close() })
	return router
}

func newGoalSessionStore(t *testing.T, router *sessionstore.Router, sessionID string) *sessionstore.SessionContextStore {
	t.Helper()
	store := sessionstore.NewSessionContextStore(router, sessionID)
	if err := store.Load(context.Background()); err != nil {
		t.Fatalf("load session context %q: %v", sessionID, err)
	}
	return store
}

// TestContextStateStoreReloadRestoresNestedStack 验证会话恢复：Controller
// 栈变更经 ContextStateStore 落 sessionstore GoalStack；新 Controller
// Reload 后恢复嵌套栈（下层 paused / 栈顶 active + progress）与 seq 续号。
func TestContextStateStoreReloadRestoresNestedStack(t *testing.T) {
	router := newSessionRouterForGoalTest(t)
	sessionID := "session-goal-restore"
	store := newGoalSessionStore(t, router, sessionID)
	controller := NewController(Options{Depth: 2, Store: NewContextStateStore(store)})

	if _, err := controller.Begin(testCtx, BeginRequest{Title: "父目标"}); err != nil {
		t.Fatalf("begin parent: %v", err)
	}
	if _, err := controller.Begin(testCtx, BeginRequest{Title: "子目标", Acceptance: []string{"go test 全绿"}}); err != nil {
		t.Fatalf("begin child: %v", err)
	}
	if _, err := controller.Update(testCtx, UpdateRequest{
		ProgressKind: ProgressMilestone, ProgressContent: "已补负路径单测",
	}); err != nil {
		t.Fatalf("update child: %v", err)
	}

	// 模拟重启：同会话新存储 + 新 Controller.Reload。
	reloadedStore := newGoalSessionStore(t, router, sessionID)
	// Depth 放宽到 3：验证恢复后 seq 续号可继续压栈（g-3 不重复）。
	restored := NewController(Options{Depth: 3, Store: NewContextStateStore(reloadedStore)})
	if err := restored.Reload(testCtx); err != nil {
		t.Fatalf("reload: %v", err)
	}
	status := restored.Status()
	if len(status.Stack) != 2 {
		t.Fatalf("restored stack depth = %d, want 2: %+v", len(status.Stack), status)
	}
	if status.Stack[0].Title != "父目标" || status.Stack[0].Status != StatusPaused {
		t.Fatalf("restored parent = %+v", status.Stack[0])
	}
	if status.Active == nil || status.Active.Title != "子目标" || status.Active.Status != StatusActive {
		t.Fatalf("restored active = %+v", status.Active)
	}
	if len(status.Active.Progress) != 1 || status.Active.Progress[0].Content != "已补负路径单测" {
		t.Fatalf("restored progress = %+v", status.Active.Progress)
	}
	// seq 已推进 → 新 begin 生成 g-3（不重复 g-1/g-2）。
	if _, err := restored.Begin(testCtx, BeginRequest{Title: "顶层新目标"}); err != nil {
		t.Fatalf("begin after reload: %v", err)
	}
	active := restored.Status().Active
	if active.ID != "g-3" {
		t.Fatalf("restored seq 续号错误: active id = %q", active.ID)
	}
}

// TestContextStateStoreSessionIsolation 验证 goal 第五栈按会话隔离：
// 会话 A 的 goal 不污染会话 B（恢复路径读各自 GoalStack）。
func TestContextStateStoreSessionIsolation(t *testing.T) {
	router := newSessionRouterForGoalTest(t)
	storeA := newGoalSessionStore(t, router, "session-goal-a")
	storeB := newGoalSessionStore(t, router, "session-goal-b")
	controllerA := NewController(Options{Depth: 1, Store: NewContextStateStore(storeA)})
	controllerB := NewController(Options{Depth: 1, Store: NewContextStateStore(storeB)})

	if _, err := controllerA.Begin(testCtx, BeginRequest{Title: "A 的治理目标"}); err != nil {
		t.Fatalf("begin A: %v", err)
	}
	if status := controllerB.Status(); status.Active != nil {
		t.Fatalf("B 不应看到 A 的 goal: %+v", status)
	}
	// A 恢复后仍在；B 恢复后仍空。
	reloadA := NewController(Options{Depth: 1, Store: NewContextStateStore(newGoalSessionStore(t, router, "session-goal-a"))})
	if err := reloadA.Reload(testCtx); err != nil {
		t.Fatal(err)
	}
	if reloadA.Status().Active == nil || reloadA.Status().Active.Title != "A 的治理目标" {
		t.Fatalf("A 恢复后 active = %+v", reloadA.Status().Active)
	}
	reloadB := NewController(Options{Depth: 1, Store: NewContextStateStore(newGoalSessionStore(t, router, "session-goal-b"))})
	if err := reloadB.Reload(testCtx); err != nil {
		t.Fatal(err)
	}
	if status := reloadB.Status(); status.Active != nil || len(status.Stack) != 0 {
		t.Fatalf("B 恢复后不应有 goal: %+v", status)
	}
}

// TestContextStateStoreFinishPopsToEmpty 验证栈空语义落盘：子 goal 完成 →
// 父恢复 active；父完成 → 栈空，重载后治理从零开始。
func TestContextStateStoreFinishPopsToEmpty(t *testing.T) {
	router := newSessionRouterForGoalTest(t)
	sessionID := "session-goal-pop"
	store := newGoalSessionStore(t, router, sessionID)
	controller := NewController(Options{Depth: 2, Store: NewContextStateStore(store)})
	if _, err := controller.Begin(testCtx, BeginRequest{Title: "父目标"}); err != nil {
		t.Fatalf("begin parent: %v", err)
	}
	if persisted := store.GoalStackSnapshot(); len(persisted) != 1 ||
		persisted[0].Title != "父目标" || persisted[0].Status != string(StatusActive) {
		t.Fatalf("begin 后持久化活栈 = %+v", persisted)
	}
	if _, err := controller.Begin(testCtx, BeginRequest{Title: "子目标"}); err != nil {
		t.Fatalf("begin child: %v", err)
	}
	if persisted := store.GoalStackSnapshot(); len(persisted) != 2 ||
		persisted[0].Title != "父目标" || persisted[0].Status != string(StatusPaused) ||
		persisted[1].Title != "子目标" {
		t.Fatalf("嵌套压栈后持久化活栈 = %+v", persisted)
	}
	if _, err := controller.Finish(testCtx, FinishRequest{Result: "子目标完成"}); err != nil {
		t.Fatalf("finish child: %v", err)
	}
	if persisted := store.GoalStackSnapshot(); len(persisted) != 1 ||
		persisted[0].Title != "父目标" || persisted[0].Status != string(StatusActive) {
		t.Fatalf("子完成弹栈应同步删除 g-2，持久化只剩父且 active: %+v", persisted)
	}

	reloadedSessionStore := newGoalSessionStore(t, router, sessionID)
	reloaded := NewController(Options{Depth: 2, Store: NewContextStateStore(reloadedSessionStore)})
	if err := reloaded.Reload(testCtx); err != nil {
		t.Fatal(err)
	}
	if status := reloaded.Status(); len(status.Stack) != 1 || status.Active == nil ||
		status.Active.Title != "父目标" || status.Active.Status != StatusActive {
		t.Fatalf("子完成后父应恢复 active 且落盘: %+v", status)
	}
	if _, err := reloaded.Finish(testCtx, FinishRequest{Result: "父目标完成"}); err != nil {
		t.Fatalf("finish parent: %v", err)
	}
	if persisted := reloadedSessionStore.GoalStackSnapshot(); len(persisted) != 0 {
		t.Fatalf("父完成弹栈后持久化栈应清空（弹栈即删除）: %+v", persisted)
	}
	// History 是进程内审计：重启后新 Controller 只审计本次会话内收口
	// （子目标审计留在旧 Controller），不落 GoalStack。
	if history := reloaded.History(); len(history) != 1 || history[0].Title != "父目标" {
		t.Fatalf("进程内 History 应含本次收口审计（不落 GoalStack）: %+v", history)
	}
	again := NewController(Options{Depth: 2, Store: NewContextStateStore(newGoalSessionStore(t, router, sessionID))})
	if err := again.Reload(testCtx); err != nil {
		t.Fatal(err)
	}
	if status := again.Status(); len(status.Stack) != 0 || status.Active != nil {
		t.Fatalf("栈空后重载应为空栈: %+v", status)
	}
}

// TestContextStateStoreNilRejects 验证未装配会话上下文存储时 Store 显式失败。
func TestContextStateStoreNilRejects(t *testing.T) {
	store := NewContextStateStore(nil)
	if _, err := store.Load(testCtx); err == nil {
		t.Fatal("nil session context store Load 应失败")
	}
	if err := store.Save(testCtx, nil); err == nil {
		t.Fatal("nil session context store Save 应失败")
	}
}
