package assembly

import (
	"testing"
)

// TestEffortOwnershipPerSession（G4）：effort 选择归属进 SessionUnit——
// 每个会话保存自己的选择，切换/新建后互不覆盖；未选择会话回退进程默认。
func TestEffortOwnershipPerSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	sessionA := service.components.frontendSnapshot.Snapshot().Session.ID

	if err := service.components.policyApplication.SwitchEffort("high"); err != nil {
		t.Fatalf("SwitchEffort(high): %v", err)
	}
	if unit := service.sessions.Unit(sessionA); unit == nil || unit.EffortLevel() != "high" {
		t.Fatalf("session A effort not owned by unit: %+v", unit)
	}
	if got := service.effortForSession(sessionA); got != "high" {
		t.Fatalf("effortForSession(A) = %q, want high", got)
	}

	if err := service.components.sessionDrafts.entry.Begin(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	draftID := service.components.frontendSnapshot.Snapshot().Session.ID
	if err := service.components.policyApplication.SwitchEffort("lite"); err != nil {
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

// TestSubagentPolicySlotSyncPerSession（G1-C）：chat 起点按会话 effort 把 plan
// 策略写进引擎会话槽（2026-09-07 起 effort 不再携带 MaxForkConcurrency，
// high/max 均为 0 = 子代理并发不设上限），互不覆盖；未选择会话回退进程
// 默认。
func TestSubagentPolicySlotSyncPerSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	runtime := service.Deps.Runtime.(*fakeRuntime)
	sessionA := service.components.frontendSnapshot.Snapshot().Session.ID

	if err := service.components.policyApplication.SwitchEffort("high"); err != nil {
		t.Fatalf("SwitchEffort(high): %v", err)
	}
	service.syncSubagentPolicyFor(sessionA)
	gotA, ok := runtime.subagentPolicyFor(sessionA)
	if !ok || gotA.Effort != "high" || gotA.MaxForkConcurrency != 0 {
		t.Fatalf("plan policy slot A = %+v ok=%v, want high/无并发上限", gotA, ok)
	}

	if err := service.components.sessionDrafts.entry.Begin(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	draftID := service.components.frontendSnapshot.Snapshot().Session.ID
	if err := service.components.policyApplication.SwitchEffort("lite"); err != nil {
		t.Fatalf("SwitchEffort(lite): %v", err)
	}
	service.syncSubagentPolicyFor(draftID)
	gotDraft, ok := runtime.subagentPolicyFor(draftID)
	if !ok || gotDraft.Effort != "lite" {
		t.Fatalf("plan policy slot draft = %+v ok=%v, want lite", gotDraft, ok)
	}

	// 再次同步会话 A：A 的槽保持 high，不被草稿同步覆盖。
	service.syncSubagentPolicyFor(sessionA)
	gotA, ok = runtime.subagentPolicyFor(sessionA)
	if !ok || gotA.Effort != "high" || gotA.MaxForkConcurrency != 0 {
		t.Fatalf("plan policy slot A after draft sync = %+v ok=%v, want high/无并发上限", gotA, ok)
	}
}
