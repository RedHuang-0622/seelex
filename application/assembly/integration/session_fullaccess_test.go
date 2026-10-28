package integration

import (
	"context"
	"testing"
)

// TestFullAccessOwnershipPerSession（G4）：fullAccess 选择归属进 SessionUnit
// ——每个会话保存自己的模式，切换/新建后互不覆盖；未选择会话回退装配期捕获
// 的进程默认，不继承其它会话遗留的引擎门值。
//
// 迁移说明：原先直接调 `fullAccessForSession`（未导出，运行时投影的唯一读面）
// 与 `syncFullAccessFor`（未导出，chat 起点同步）。这里改读投影字段
// （projectedFullAccess），并把"起点同步"换成它唯一的公开触发点——在该会话上
// 真的起跑一个回合（Conversation.Submission.Submit），断言不变。
func TestFullAccessOwnershipPerSession(t *testing.T) {
	service := newLegacyTestService(t, &fakeEngine{})
	runtime := service.Deps.Runtime.(*fakeRuntime)
	sessionA := service.components.frontendSnapshot.Snapshot().Session.ID

	service.components.policyApplication.SetFullAccess(true)
	if unit := service.sessions.Unit(sessionA); unit == nil {
		t.Fatal("session A unit missing")
	} else if on, ok := unit.FullAccessMode(); !ok || !on {
		t.Fatalf("session A full access not owned by unit: on=%v ok=%v", on, ok)
	}
	if got := projectedFullAccess(t, service, sessionA); !got {
		t.Fatal("projected full access(A) = false, want true")
	}
	if !runtime.FullAccessFor(sessionA) {
		t.Fatal("engine gate was not synced for session A")
	}
	if runtime.FullAccessFor("") {
		t.Fatal("进程级默认全权不得被会话级选择改写（未选择会话的回退面）")
	}

	if err := service.components.sessionDrafts.entry.Begin(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	draftID := service.components.frontendSnapshot.Snapshot().Session.ID
	if unit := service.sessions.Unit(draftID); unit == nil {
		t.Fatal("draft unit missing")
	} else if _, ok := unit.FullAccessMode(); ok {
		t.Fatalf("draft must start without a full access choice (ok=%v)", ok)
	}
	// 执行门按会话解析：A 的那一格仍是 true，未选择的草稿回退进程默认 false。
	if got := projectedFullAccess(t, service, draftID); got {
		t.Fatal("unset draft must fall back to the process default, not inherit A's gate")
	}
	if runtime.FullAccessFor(draftID) {
		t.Fatal("未选择的草稿执行门不得继承 A 的全权")
	}
	if got := projectedFullAccess(t, service, sessionA); !got {
		t.Fatal("A's choice must survive BeginNewSession")
	}
	if !runtime.FullAccessFor(sessionA) {
		t.Fatal("A 的全权在新建会话后仍须生效")
	}

	service.components.policyApplication.SetFullAccess(false) // 视图已切到草稿：选择落在草稿上
	if unit := service.sessions.Unit(draftID); unit == nil {
		t.Fatal("draft unit missing")
	} else if on, ok := unit.FullAccessMode(); !ok || on {
		t.Fatalf("draft full access = on=%v ok=%v, want set-false", on, ok)
	}
	if got := projectedFullAccess(t, service, sessionA); !got {
		t.Fatal("draft toggle must not clear session A's choice")
	}

	// chat 起点同步：在草稿上真起跑一个回合（等价于旧的 syncFullAccessFor(draftID)
	// ——它按草稿自己的模式写它自己那一格），A 的全权不得被连带关掉。
	if err := service.components.submission.Submit(context.Background(), "sync draft gate"); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if err := service.components.lifecycleApplication.WaitForIdle(context.Background()); err != nil {
		t.Fatalf("WaitForIdle: %v", err)
	}
	if runtime.FullAccessFor(draftID) {
		t.Fatal("草稿的起点同步必须写它自己的模式（manual），不得是 A 的全权")
	}
	if !runtime.FullAccessFor(sessionA) {
		t.Fatal("draft 的起点同步关掉了 A 的全权（跨会话覆盖）")
	}
}

// TestFullAccessProjectionPerSession（G4）：运行时投影按会话读取生效的
// fullAccess（view 协调器经 CurrentFullAccess 注入），后台会话不显示视图
// 会话的开关。
func TestFullAccessProjectionPerSession(t *testing.T) {
	service := newLegacyTestService(t, &fakeEngine{})
	sessionA := service.components.frontendSnapshot.Snapshot().Session.ID
	service.components.policyApplication.SetFullAccess(true)

	if err := service.components.sessionDrafts.entry.Begin(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	draftID := service.components.frontendSnapshot.Snapshot().Session.ID

	projectionA := service.components.view.CollectRuntimeProjectionFor(context.Background(), sessionA)
	if !projectionA.Runtime.FullAccess {
		t.Fatalf("projection of A FullAccess = false, want true")
	}
	projectionDraft := service.components.view.CollectRuntimeProjectionFor(context.Background(), draftID)
	if projectionDraft.Runtime.FullAccess {
		t.Fatalf("projection of unset draft FullAccess = true, want process default false")
	}
}
