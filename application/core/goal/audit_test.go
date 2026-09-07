package goal

import (
	"testing"

	"github.com/RedHuang-0622/seelex/sessionstore"
)

// TestControllerAuditAppendOnlyLifecycle 验证 Controller 自动审计：
// begin/update/finish 各追加一条（seq 单调）；GoalStack 活栈终态清空，而
// 审计账本保留终态收口记录（reason/result）。
func TestControllerAuditAppendOnlyLifecycle(t *testing.T) {
	router := newSessionRouterForGoalTest(t)
	sessionID := "session-goal-audit"
	store := newGoalSessionStore(t, router, sessionID)
	account := NewContextStateStore(store)
	controller := NewController(Options{Depth: 1, Store: account, Audit: account})
	if _, err := controller.Begin(testCtx, BeginRequest{Title: "审查 goal 域"}); err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := controller.Update(testCtx, UpdateRequest{
		ProgressKind: ProgressMilestone, ProgressContent: "已完成调研",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := controller.Finish(testCtx, FinishRequest{
		Reason: "证据齐全", Result: "单测全绿",
	}); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if live := store.GoalStackSnapshot(); len(live) != 0 {
		t.Fatalf("GoalStack 终态应清空（什么都不存）: %+v", live)
	}
	audit := store.GoalAuditSnapshot()
	if len(audit) != 3 {
		t.Fatalf("审计账本应保留 3 条: %+v", audit)
	}
	wantKinds := []string{string(EventBegin), string(EventUpdate), string(EventFinish)}
	for index := range wantKinds {
		if audit[index].Kind != wantKinds[index] || audit[index].Seq != uint64(index+1) {
			t.Fatalf("审计[%d] = %+v, want kind=%s seq=%d", index, audit[index], wantKinds[index], index+1)
		}
	}
	last := audit[2]
	if last.GoalID == "" || last.Status != string(StatusCompleted) ||
		last.Reason != "证据齐全" || last.Result != "单测全绿" {
		t.Fatalf("finish 审计应保留 reason/result: %+v", last)
	}
}

// TestControllerAuditNestedRestoreAndTerminal 验证嵌套治理审计：子 finish
// 追加 finish(child)+restore(parent)，父 finish 追加 finish(parent)；seq
// 严格递增且终态帧只进账本、不进活栈。
func TestControllerAuditNestedRestoreAndTerminal(t *testing.T) {
	router := newSessionRouterForGoalTest(t)
	sessionID := "session-goal-audit-nested"
	store := newGoalSessionStore(t, router, sessionID)
	account := NewContextStateStore(store)
	controller := NewController(Options{Depth: 2, Store: account, Audit: account})
	for _, title := range []string{"父目标", "子目标"} {
		if _, err := controller.Begin(testCtx, BeginRequest{Title: title}); err != nil {
			t.Fatalf("begin %s: %v", title, err)
		}
	}
	if _, err := controller.Finish(testCtx, FinishRequest{Result: "子目标完成"}); err != nil {
		t.Fatalf("finish child: %v", err)
	}
	if _, err := controller.Finish(testCtx, FinishRequest{Result: "父目标完成"}); err != nil {
		t.Fatalf("finish parent: %v", err)
	}
	audit := store.GoalAuditSnapshot()
	wantKinds := []string{
		string(EventBegin), string(EventBegin),
		string(EventFinish), string(EventRestore),
		string(EventFinish),
	}
	if len(audit) != len(wantKinds) {
		t.Fatalf("审计条数 = %d, want %d: %+v", len(audit), len(wantKinds), audit)
	}
	for index, kind := range wantKinds {
		if audit[index].Kind != kind || audit[index].Seq != uint64(index+1) {
			t.Fatalf("审计[%d] = %+v, want kind=%s seq=%d", index, audit[index], kind, index+1)
		}
	}
	restore := audit[3]
	if restore.GoalID != "g-1" || restore.Status != string(StatusActive) {
		t.Fatalf("restore 审计应指向父并置 active: %+v", restore)
	}
	if live := store.GoalStackSnapshot(); len(live) != 0 {
		t.Fatalf("全部收口后活栈应清空: %+v", live)
	}
}

// TestAuditSourceSessionProvenanceRoundTrip 验证"用户在其它会话完成 goal"
// 的出处可审计：装配方可在收口条目携带 SourceSession，条目仍留在原会话
// 账本（不跨会话写入），持久化/重载后出处不丢。
func TestAuditSourceSessionProvenanceRoundTrip(t *testing.T) {
	router := newSessionRouterForGoalTest(t)
	sessionID := "session-goal-audit-source"
	store := newGoalSessionStore(t, router, sessionID)
	account := NewContextStateStore(store)
	if err := account.AppendGoalAudit(testCtx, AuditEntry{
		Kind: EventFinish, GoalID: "g-1", Title: "重构 seelexctx", Status: StatusCompleted,
		Result: "已在会话 B 完成", SourceSession: "session-b",
	}); err != nil {
		t.Fatalf("append source audit: %v", err)
	}
	reloaded := newGoalSessionStore(t, router, sessionID)
	audit := reloaded.GoalAuditSnapshot()
	if len(audit) != 1 || audit[0].SourceSession != "session-b" ||
		audit[0].GoalID != "g-1" || audit[0].Status != string(StatusCompleted) {
		t.Fatalf("reloaded audit = %+v", audit)
	}
}

// TestAuditPerSessionIsolation 验证审计按会话隔离：两会话各自审计独立
// 编号，不互相串写。
func TestAuditPerSessionIsolation(t *testing.T) {
	router := newSessionRouterForGoalTest(t)
	makeController := func(sessionID, title string) (*Controller, *sessionstore.SessionContextStore) {
		store := newGoalSessionStore(t, router, sessionID)
		account := NewContextStateStore(store)
		controller := NewController(Options{Depth: 1, Store: account, Audit: account})
		if _, err := controller.Begin(testCtx, BeginRequest{Title: title}); err != nil {
			t.Fatalf("begin %s: %v", title, err)
		}
		if _, err := controller.Finish(testCtx, FinishRequest{Result: title + " 完成"}); err != nil {
			t.Fatalf("finish %s: %v", title, err)
		}
		return controller, store
	}
	_, storeA := makeController("session-goal-audit-a", "A 目标")
	_, storeB := makeController("session-goal-audit-b", "B 目标")
	auditA := storeA.GoalAuditSnapshot()
	auditB := storeB.GoalAuditSnapshot()
	if len(auditA) != 2 || len(auditB) != 2 {
		t.Fatalf("audit lengths = %d/%d", len(auditA), len(auditB))
	}
	for index := 0; index < 2; index++ {
		if auditA[index].Seq != uint64(index+1) || auditB[index].Seq != uint64(index+1) {
			t.Fatalf("seq must restart per session: A=%d B=%d", auditA[index].Seq, auditB[index].Seq)
		}
	}
	if auditA[0].Title == auditB[0].Title {
		t.Fatalf("两会话审计内容串写: %+v / %+v", auditA, auditB)
	}
}
