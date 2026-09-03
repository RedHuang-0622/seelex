package core

import (
	"context"
	"testing"
)

// TestEffortOwnershipPerSession（G4）：effort 选择归属进 SessionUnit——
// 每个会话保存自己的选择，切换/新建后互不覆盖；未选择会话回退进程默认。
func TestEffortOwnershipPerSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	sessionA := service.Snapshot().Session.ID

	if err := service.SwitchEffort(context.Background(), "high"); err != nil {
		t.Fatalf("SwitchEffort(high): %v", err)
	}
	if unit := service.sessions.Unit(sessionA); unit == nil || unit.EffortLevel() != "high" {
		t.Fatalf("session A effort not owned by unit: %+v", unit)
	}
	if got := service.effortForSession(sessionA); got != "high" {
		t.Fatalf("effortForSession(A) = %q, want high", got)
	}

	if err := service.BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	draftID := service.Snapshot().Session.ID
	if err := service.SwitchEffort(context.Background(), "lite"); err != nil {
		t.Fatalf("SwitchEffort(lite): %v", err)
	}
	if unit := service.sessions.Unit(draftID); unit == nil || unit.EffortLevel() != "lite" {
		t.Fatalf("draft effort not owned by unit: %+v", unit)
	}
	if got := service.effortForSession(sessionA); got != "high" {
		t.Fatalf("switching draft effort leaked into session A: %q", got)
	}
	if got := service.effortForSession(draftID); got != "lite" {
		t.Fatalf("effortForSession(draft) = %q, want lite", got)
	}
}

// TestPlanPolicySlotSyncPerSession（G1-C）：chat 起点按会话 effort 把 plan
// 策略写进引擎会话槽（high → fork 3；lite → 串行 fork 1），互不覆盖；未
// 选择会话回退进程默认。
func TestPlanPolicySlotSyncPerSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	runtime := service.Deps.Runtime.(*fakeRuntime)
	sessionA := service.Snapshot().Session.ID

	if err := service.SwitchEffort(context.Background(), "high"); err != nil {
		t.Fatalf("SwitchEffort(high): %v", err)
	}
	service.syncPlanPolicyFor(sessionA)
	gotA, ok := runtime.planPolicyFor(sessionA)
	if !ok || gotA.Effort != "high" || gotA.MaxForkConcurrency != 3 {
		t.Fatalf("plan policy slot A = %+v ok=%v, want high/fork-3", gotA, ok)
	}

	if err := service.BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	draftID := service.Snapshot().Session.ID
	if err := service.SwitchEffort(context.Background(), "lite"); err != nil {
		t.Fatalf("SwitchEffort(lite): %v", err)
	}
	service.syncPlanPolicyFor(draftID)
	gotDraft, ok := runtime.planPolicyFor(draftID)
	if !ok || gotDraft.Effort != "lite" {
		t.Fatalf("plan policy slot draft = %+v ok=%v, want lite", gotDraft, ok)
	}

	// 再次同步会话 A：A 的槽保持 high，不被草稿同步覆盖。
	service.syncPlanPolicyFor(sessionA)
	gotA, ok = runtime.planPolicyFor(sessionA)
	if !ok || gotA.Effort != "high" || gotA.MaxForkConcurrency != 3 {
		t.Fatalf("plan policy slot A after draft sync = %+v ok=%v, want high/fork-3", gotA, ok)
	}
}
