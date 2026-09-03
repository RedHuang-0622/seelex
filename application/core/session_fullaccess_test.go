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
	if !runtime.fullAccess {
		t.Fatal("engine gate was not synced for session A")
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
	// 引擎门仍残留 A 的 true，但未选择的草稿回退进程默认 false。
	if got := service.fullAccessForSession(draftID); got {
		t.Fatal("unset draft must fall back to the process default, not inherit A's gate")
	}
	if got := service.fullAccessForSession(sessionA); !got {
		t.Fatal("A's choice must survive BeginNewSession")
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

	// chat 起点同步：A 再次运行前按自己的选择恢复门。
	service.syncFullAccessFor(sessionA)
	if !runtime.fullAccess {
		t.Fatal("syncFullAccessFor(A) must restore the engine gate to A's mode")
	}
	service.syncFullAccessFor(draftID)
	if runtime.fullAccess {
		t.Fatal("syncFullAccessFor(draft) must reset the engine gate to the draft's mode")
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
