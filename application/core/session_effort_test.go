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
