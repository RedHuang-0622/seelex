package core

import (
	"context"
	"testing"
)

// TestFullAccessOwnershipPerSession（G4）：fullAccess 选择归属进 SessionUnit
// ——每个会话保存自己的模式，切换/新建后互不覆盖；未选择会话回退装配期捕获
// 的进程默认，不继承其它会话遗留的引擎门值。
func TestFullAccessOwnershipPerSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	runtime := service.Deps.Runtime.(*fakeRuntime)
	sessionA := service.Snapshot().Session.ID

	service.SetFullAccess(true)
	if unit := service.sessions.Unit(sessionA); unit == nil {
		t.Fatal("session A unit missing")
	} else if on, ok := unit.FullAccessMode(); !ok || !on {
		t.Fatalf("session A full access not owned by unit: on=%v ok=%v", on, ok)
	}
	if got := service.fullAccessForSession(sessionA); !got {
		t.Fatal("fullAccessForSession(A) = false, want true")
	}
	if !runtime.FullAccessFor(sessionA) {
		t.Fatal("engine gate was not synced for session A")
	}
	if runtime.FullAccessFor("") {
		t.Fatal("进程级默认全权不得被会话级选择改写（未选择会话的回退面）")
	}

	if err := service.BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	draftID := service.Snapshot().Session.ID
	if unit := service.sessions.Unit(draftID); unit == nil {
		t.Fatal("draft unit missing")
	} else if _, ok := unit.FullAccessMode(); ok {
		t.Fatalf("draft must start without a full access choice (ok=%v)", ok)
	}
	// 执行门按会话解析：A 的那一格仍是 true，未选择的草稿回退进程默认 false。
	if got := service.fullAccessForSession(draftID); got {
		t.Fatal("unset draft must fall back to the process default, not inherit A's gate")
	}
	if runtime.FullAccessFor(draftID) {
		t.Fatal("未选择的草稿执行门不得继承 A 的全权")
	}
	if got := service.fullAccessForSession(sessionA); !got {
		t.Fatal("A's choice must survive BeginNewSession")
	}
	if !runtime.FullAccessFor(sessionA) {
		t.Fatal("A 的全权在新建会话后仍须生效")
	}

	service.SetFullAccess(false) // 视图已切到草稿：选择落在草稿上
	if unit := service.sessions.Unit(draftID); unit == nil {
		t.Fatal("draft unit missing")
	} else if on, ok := unit.FullAccessMode(); !ok || on {
		t.Fatalf("draft full access = on=%v ok=%v, want set-false", on, ok)
	}
	if got := service.fullAccessForSession(sessionA); !got {
		t.Fatal("draft toggle must not clear session A's choice")
	}

	// chat 起点同步：按会话各写自己的那一格——草稿的起点同步不得关掉 A。
	service.syncFullAccessFor(draftID)
	if runtime.FullAccessFor(draftID) {
		t.Fatal("syncFullAccessFor(draft) must set the draft gate to the draft's own mode")
	}
	if !runtime.FullAccessFor(sessionA) {
		t.Fatal("draft 的起点同步关掉了 A 的全权（跨会话覆盖）")
	}
	service.syncFullAccessFor(sessionA)
	if !runtime.FullAccessFor(sessionA) {
		t.Fatal("syncFullAccessFor(A) must keep the engine gate at A's mode")
	}
}

// TestFullAccessProjectionPerSession（G4）：运行时投影按会话读取生效的
// fullAccess（view 协调器经 CurrentFullAccess 注入），后台会话不显示视图
// 会话的开关。
func TestFullAccessProjectionPerSession(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	sessionA := service.Snapshot().Session.ID
	service.SetFullAccess(true)

	if err := service.BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	draftID := service.Snapshot().Session.ID

	projectionA := service.collectRuntimeProjectionFor(context.Background(), sessionA)
	if !projectionA.Runtime.FullAccess {
		t.Fatalf("projection of A FullAccess = false, want true")
	}
	projectionDraft := service.collectRuntimeProjectionFor(context.Background(), draftID)
	if projectionDraft.Runtime.FullAccess {
		t.Fatalf("projection of unset draft FullAccess = true, want process default false")
	}
}
