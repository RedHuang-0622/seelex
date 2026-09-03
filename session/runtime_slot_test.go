package session

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/model"
)

// TestSessionUnitRuntimeSlotIsolation（G1-A）：每会话运行时槽写入/读出是
// 深拷贝——调用方后续改动原投影 slice 不污染槽，返回副本改动不影响槽。
func TestSessionUnitRuntimeSlotIsolation(t *testing.T) {
	unit, err := NewSessionUnit("sess-slot")
	if err != nil {
		t.Fatal(err)
	}
	if unit.RuntimeStateLoaded() {
		t.Fatal("fresh unit must report an empty runtime slot")
	}
	if got := unit.SnapshotRevision(); got != 0 {
		t.Fatalf("fresh unit revision = %d, want 0", got)
	}

	projection := model.RuntimeState{
		Model: "m", Plugin: "default", Effort: "high", Tokens: "100",
		VisibleTools: []model.Tool{{Name: "read"}},
		Skills:       []model.SkillInfo{{Name: "review"}},
		Plan:         &model.PlanState{Status: model.PlanRunning, Nodes: []model.PlanNode{{ID: "n1"}}},
	}
	unit.SetRuntimeState(projection)

	// 写后：槽已加载、Revision 独立于进程修订。
	if !unit.RuntimeStateLoaded() {
		t.Fatal("runtime slot must report loaded after write")
	}
	projection.VisibleTools[0].Name = "mutated"
	projection.Plan.Nodes[0].ID = "mutated"

	stored := unit.RuntimeState()
	if len(stored.VisibleTools) != 1 || stored.VisibleTools[0].Name != "read" {
		t.Fatalf("slot visible tools mutated by caller: %+v", stored.VisibleTools)
	}
	if stored.Plan == nil || stored.Plan.Nodes[0].ID != "n1" {
		t.Fatalf("slot plan mutated by caller: %+v", stored.Plan)
	}

	// 读回副本的改动不污染槽。
	readCopy := unit.RuntimeState()
	readCopy.Skills[0].Name = "mutated"
	again := unit.RuntimeState()
	if again.Skills[0].Name != "review" {
		t.Fatalf("slot skills mutated through read copy: %+v", again.Skills)
	}
}

// TestSessionUnitRevisionBumpsPerSession（G1-A/INV-G5）：Revision 按单元
// 独立递增，两会话互不串扰。
func TestSessionUnitRevisionBumpsPerSession(t *testing.T) {
	a, err := NewSessionUnit("sess-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewSessionUnit("sess-b")
	if err != nil {
		t.Fatal(err)
	}
	if got := a.BumpSnapshotRevision(); got != 1 {
		t.Fatalf("first bump of A = %d, want 1", got)
	}
	if got := a.BumpSnapshotRevision(); got != 2 {
		t.Fatalf("second bump of A = %d, want 2", got)
	}
	if got := b.BumpSnapshotRevision(); got != 1 {
		t.Fatalf("first bump of B = %d, want 1 (isolated from A)", got)
	}
	if got := a.SnapshotRevision(); got != 2 {
		t.Fatalf("A revision = %d, want 2 after B bump", got)
	}
}
