package assembly

import (
	"context"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"sync"
	"testing"
	"time"
)

// waitApprovalCount 轮询指定会话单元的待批数直到到达目标（approval 观察
// 回调由 broker goroutine 异步触发）。

func waitApprovalCount(t *testing.T, service *Service, sessionID string, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		service.ViewMu.RLock()
		count := 0
		if unit := service.sessions.Unit(sessionID); unit != nil {
			count = unit.PendingApprovalCount()
		}
		service.ViewMu.RUnlock()
		if count == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("session %q approval count never reached %d", sessionID, want)
}

// TestApprovalSessionOwnershipStatusAndSnapshot 波 4 approval 会话级归属：
// 视图会话卡在审批 → 目录行 awaiting_approval + 单格 Interaction + 会话
// 快照 approvals；结案后回到运行态（引擎仍在跑）。
func TestApprovalSessionOwnershipStatusAndSnapshot(t *testing.T) {
	engine := newMultiSessionEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	ctx := context.Background()
	defer service.components.lifecycleApplication.Shutdown()

	if err := service.components.submission.Submit(ctx, "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])
	sessions.setInfos([]model.SessionInfo{{ID: aID}})

	result := make(chan contract.ApprovalDecision, 1)
	go func() {
		decision, err := service.Approval.Request(ctx, contract.ApprovalRequest{
			ID: "ask-A", SessionID: aID, Question: "A 继续？",
			Options: []model.InteractionOption{{ID: "allow", Label: "允许"}, {ID: "deny", Label: "拒绝"}},
		})
		if err == nil {
			result <- decision
		}
	}()
	waitApprovalCount(t, service, aID, 1)

	if got := catalogStatusOf(t, service, aID); got != SessionStatusAwaitingApproval {
		t.Fatalf("catalog status while awaiting = %q, want awaiting_approval", got)
	}
	snapshot := service.components.frontendSnapshot.Snapshot()
	if snapshot.Interaction == nil || snapshot.Interaction.ID != "ask-A" {
		t.Fatalf("view interaction = %#v, want ask-A", snapshot.Interaction)
	}
	unit := service.sessions.Unit(aID)
	if got := unit.ApprovalIDs(); len(got) != 1 || got[0] != "ask-A" {
		t.Fatalf("unit approvals = %#v, want [ask-A]", got)
	}
	sessionSnap, err := service.components.sessionQuery.SnapshotOf(aID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessionSnap.Approvals) != 1 || sessionSnap.Approvals[0].ID != "ask-A" ||
		sessionSnap.Approvals[0].SessionID != aID {
		t.Fatalf("SessionSnapshot approvals = %#v, want ask-A owned by %q", sessionSnap.Approvals, aID)
	}

	if err := service.components.interactionApplication.Resolve(ctx, "ask-A", "allow"); err != nil {
		t.Fatalf("resolve ask-A: %v", err)
	}
	select {
	case decision := <-result:
		if decision.OptionID != "allow" {
			t.Fatalf("decision = %#v, want allow", decision)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ask-A did not resolve")
	}
	waitApprovalCount(t, service, aID, 0)
	if got := catalogStatusOf(t, service, aID); got != SessionStatusRunning {
		t.Fatalf("status after approval while engine still running = %q, want running", got)
	}

	close(engine.release[aID])
	if err := service.components.lifecycleApplication.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestBackgroundApprovalDoesNotClobberViewSlotAndResolvesById 波 4 归属：
// 后台会话卡在审批不进视图单格（不覆盖视图会话的展示），目录行标
// awaiting_approval；ResolveInteraction 按 id 直接结案（跨会话待批）。
func TestBackgroundApprovalDoesNotClobberViewSlotAndResolvesById(t *testing.T) {
	engine := newMultiSessionEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	ctx := context.Background()
	defer service.components.lifecycleApplication.Shutdown()

	if err := service.components.submission.Submit(ctx, "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])

	bID := "sess-approval-B"
	engine.register(bID)
	if err := service.components.submitApplication.SubmitToSession(ctx, bID, "task B"); err != nil {
		t.Fatal(err)
	}
	waitChatStarted(t, engine.started[bID])
	// SubmitToSession 只在目标未加载时切换视图；B 已注册（HasSession）→
	// 后台并行运行，视图仍是 A。
	if got := service.components.frontendSnapshot.Snapshot().Session.ID; got != aID {
		t.Fatalf("view session = %q, want A（B 后台运行不切换视图）", got)
	}
	sessions.setInfos([]model.SessionInfo{{ID: aID}, {ID: bID}})

	resultB := make(chan contract.ApprovalDecision, 1)
	go func() {
		decision, err := service.Approval.Request(ctx, contract.ApprovalRequest{
			ID: "ask-B", SessionID: bID, Question: "B 继续？",
			Options: []model.InteractionOption{{ID: "allow", Label: "允许"}},
		})
		if err == nil {
			resultB <- decision
		}
	}()
	waitApprovalCount(t, service, bID, 1)
	if got := catalogStatusOf(t, service, bID); got != SessionStatusAwaitingApproval {
		t.Fatalf("B catalog status = %q, want awaiting_approval", got)
	}
	if got := catalogStatusOf(t, service, aID); got != SessionStatusRunning {
		t.Fatalf("A catalog status = %q, want running（后台运行不因 B 待批丢失）", got)
	}
	// 视图仍是 A（无审批）：后台 B 的待批不得进入视图单格。
	snapshot := service.components.frontendSnapshot.Snapshot()
	if snapshot.Interaction != nil {
		t.Fatalf("view single slot = %#v, want nil（后台 B 待批不得覆盖视图 A）", snapshot.Interaction)
	}
	sessionSnap, err := service.components.sessionQuery.SnapshotOf(bID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessionSnap.Approvals) != 1 || sessionSnap.Approvals[0].ID != "ask-B" ||
		sessionSnap.Approvals[0].SessionID != bID {
		t.Fatalf("SessionSnapshot(B) approvals = %#v, want ask-B owned by %q", sessionSnap.Approvals, bID)
	}

	// 同会话第二笔待批（B 仍后台）：单格保持为空，broker 两笔都归属 B。
	go func() {
		decision, err := service.Approval.Request(ctx, contract.ApprovalRequest{
			ID: "ask-B2", SessionID: bID, Question: "B 第二笔继续？",
			Options: []model.InteractionOption{{ID: "allow", Label: "允许"}},
		})
		if err == nil && decision.OptionID != "allow" {
			t.Errorf("ask-B2 decision = %#v", decision)
		}
	}()
	waitApprovalCount(t, service, bID, 2)
	if got := service.components.frontendSnapshot.Snapshot().Interaction; got != nil {
		t.Fatalf("view single slot after second background approval = %#v, want nil", got)
	}

	// 热切到 B：单格镜像 B 的首笔待批（激活镜像路径，不依赖审批发生在
	// 视图会话的旧假设）。
	if err := service.components.sessionResume.Resume(bID); err != nil {
		t.Fatalf("resume B while running: %v", err)
	}
	slot := service.components.frontendSnapshot.Snapshot().Interaction
	if slot == nil || slot.ID != "ask-B" || slot.SessionID != bID {
		t.Fatalf("view single slot after hot attach = %#v, want ask-B owned by B", slot)
	}
	// 按 id 结案：先结首笔（视图单格同步镜像下一笔），再结第二笔。
	if err := service.components.interactionApplication.Resolve(ctx, "ask-B", "allow"); err != nil {
		t.Fatalf("resolve ask-B: %v", err)
	}
	waitApprovalCount(t, service, bID, 1)
	slot = service.components.frontendSnapshot.Snapshot().Interaction
	if slot == nil || slot.ID != "ask-B2" {
		t.Fatalf("view single slot after first resolve = %#v, want ask-B2（同会话镜像下一笔）", slot)
	}
	if err := service.components.interactionApplication.Resolve(ctx, "ask-B2", "allow"); err != nil {
		t.Fatalf("resolve ask-B2: %v", err)
	}
	select {
	case <-resultB:
	case <-time.After(3 * time.Second):
		t.Fatal("ask-B did not resolve")
	}
	waitApprovalCount(t, service, bID, 0)
	if got := catalogStatusOf(t, service, bID); got != SessionStatusRunning {
		t.Fatalf("B status after approvals while engine still running = %q, want running", got)
	}

	close(engine.release[aID])
	close(engine.release[bID])
	if err := service.components.lifecycleApplication.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestApprovalConcurrentSessionsStayAttributed -race 靶场：两会话并发开
// 审批，各自归属不断串；全部结案后无残留、状态回退。
func TestApprovalConcurrentSessionsStayAttributed(t *testing.T) {
	engine := newMultiSessionEngine()
	sessions := &liveCatalogSessions{}
	service := newTestService(t, engine, withTestSessions(sessions))
	ctx := context.Background()
	defer service.components.lifecycleApplication.Shutdown()

	if err := service.components.submission.Submit(ctx, "task A"); err != nil {
		t.Fatal(err)
	}
	aID := engine.SessionID()
	waitChatStarted(t, engine.started[aID])
	bID := "sess-approval-concurrent-B"
	engine.register(bID)
	if err := service.components.submitApplication.SubmitToSession(ctx, bID, "task B"); err != nil {
		t.Fatal(err)
	}
	waitChatStarted(t, engine.started[bID])
	sessions.setInfos([]model.SessionInfo{{ID: aID}, {ID: bID}})

	var wg sync.WaitGroup
	results := make(chan contract.ApprovalDecision, 4)
	for index := 0; index < 4; index++ {
		wg.Add(1)
		target := aID
		id := "ask-A-" + string(rune('a'+index))
		if index%2 == 1 {
			target = bID
			id = "ask-B-" + string(rune('a'+index))
		}
		go func(target, id string) {
			defer wg.Done()
			decision, err := service.Approval.Request(ctx, contract.ApprovalRequest{
				ID: id, SessionID: target, Question: "继续？",
				Options: []model.InteractionOption{{ID: "allow", Label: "允许"}},
			})
			if err == nil {
				results <- decision
			}
		}(target, id)
	}
	waitApprovalCount(t, service, aID, 2)
	waitApprovalCount(t, service, bID, 2)
	for _, entry := range service.Approval.Pending() {
		if entry.SessionID != aID && entry.SessionID != bID {
			t.Fatalf("pending approval %q attributed to %q", entry.Interaction.ID, entry.SessionID)
		}
		if entry.SessionID == aID && !startsWithApprovalID(entry.Interaction.ID, "ask-A-") {
			t.Fatalf("A pending contains %q", entry.Interaction.ID)
		}
		if entry.SessionID == bID && !startsWithApprovalID(entry.Interaction.ID, "ask-B-") {
			t.Fatalf("B pending contains %q", entry.Interaction.ID)
		}
	}
	for _, entry := range service.Approval.Pending() {
		if err := service.components.interactionApplication.Resolve(ctx, entry.Interaction.ID, "allow"); err != nil {
			t.Fatalf("resolve %q: %v", entry.Interaction.ID, err)
		}
	}
	waitApprovalCount(t, service, aID, 0)
	waitApprovalCount(t, service, bID, 0)
	wg.Wait()
	for index := 0; index < 4; index++ {
		select {
		case <-results:
		case <-time.After(time.Second):
			t.Fatal("approval result missing")
		}
	}

	close(engine.release[aID])
	close(engine.release[bID])
	if err := service.components.lifecycleApplication.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
}

func startsWithApprovalID(id, prefix string) bool {
	if len(id) < len(prefix) {
		return false
	}
	return id[:len(prefix)] == prefix
}
