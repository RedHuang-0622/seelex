package gui

// 复现「切换失败后输入框内容发送不出去」的桥接链路：
//
// 会话 A 运行中切到冷会话 B → 应用先激活 B 的 restoring 空壳（Bridge 因此
// 重订阅到 B，渲染层按 restoring 禁用输入区）→ 后台冷装载失败 →
// handleColdRestoreFailure 把权威视图**异步**改回 A（hot attach）并发布进程级
// view.session.changed 事件。若 Bridge 的订阅键仍钉在失败的 B 上，B 的会话级
// 订阅之后将永远沉默：渲染层停在 restoring 空壳（输入区禁用、消息发不出去），
// 且再次 ResumeSession(A) 因前后会话相同也不会重建订阅。
//
// 断言：Bridge 收到进程级 view.session.changed 后必须把订阅对齐到权威视图
// （session-a）并重建，把权威基线经 seelex:ready 重投渲染层，且不把该信号
// 当普通事件透传。

import (
	"context"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application"
)

func TestBridgeSelfHealsSubscriptionAfterAsyncViewRollback(t *testing.T) {
	app := &sessionAwareFakeApplication{fakeApplication: newFakeApplication()}
	app.snapshotMu.Lock()
	app.snapshot.Session = application.SessionState{ID: "failed-target-b"} // async restore 刚激活的空壳
	app.snapshotMu.Unlock()

	bridge, err := NewBridge(app, Options{})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	emitted := make(chan emittedEvent, 32)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		bridge.Stop()
	})
	bridge.Start(ctx, func(_ context.Context, name string, payload any) {
		emitted <- emittedEvent{name: name, payload: payload}
	})

	if ready := waitEmitted(t, emitted); ready.name != "seelex:ready" {
		t.Fatalf("first event = %q, want seelex:ready", ready.name)
	}
	if len(app.subscribeIDs) != 1 || app.subscribeIDs[0] != "failed-target-b" {
		t.Fatalf("subscription keys = %v, want explicit failed-target-b subscription", app.subscribeIDs)
	}

	// 应用侧后台冷恢复失败：权威视图异步回退到切换前会话 A（模拟
	// handleColdRestoreFailure → hotAttachSession(previousID) + 进程级通告）。
	app.snapshotMu.Lock()
	app.snapshot.Revision = 9
	app.snapshot.Session = application.SessionState{ID: "session-a"}
	app.snapshotMu.Unlock()
	app.hub.PublishSession(application.EventViewSessionChanged, 9, "", "", nil)

	// Bridge 必须把订阅键对齐到权威视图 A 并重建。
	deadline := time.Now().Add(5 * time.Second)
	for {
		if len(app.subscribeIDs) >= 2 && app.subscribeIDs[1] == "session-a" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("subscription keys = %v, want rebuilt for session-a after async view rollback", app.subscribeIDs)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 新订阅把权威基线经 seelex:ready 重投渲染层（渲染层借此离开 restoring
	// 空壳、恢复输入区）。
	ready := waitEmitted(t, emitted)
	if ready.name != "seelex:ready" {
		t.Fatalf("after rollback, next GUI event = %q, want seelex:ready", ready.name)
	}
	snapshot, ok := ready.payload.(application.Snapshot)
	if !ok {
		t.Fatalf("seelex:ready payload type = %T, want application.Snapshot", ready.payload)
	}
	if snapshot.Session.ID != "session-a" {
		t.Fatalf("seelex:ready baseline session = %q, want session-a", snapshot.Session.ID)
	}

	// view.session.changed 是 Bridge 内部订阅对齐信号，绝不透传渲染层。
	for {
		select {
		case extra := <-emitted:
			if event, ok := extra.payload.(application.Event); ok && event.Kind == application.EventViewSessionChanged {
				t.Fatalf("view.session.changed must not be relayed to the renderer: %+v", extra)
			}
			continue
		default:
			return
		}
	}
}

// TestBridgeSameSessionRetryAfterRollbackHealsSubscription 复现二次伤害路径：
// 异步回退后用户再次点击"上一会话 A"（ResumeSession(A)），应用视图本来就
// 是 A（before==after），旧逻辑因此跳过重订阅。修复后重订阅判据是"订阅键
// 是否等于权威视图会话"，必须把钉在 B 上的订阅重建到 A。
func TestBridgeSameSessionRetryAfterRollbackHealsSubscription(t *testing.T) {
	app := &sessionAwareFakeApplication{fakeApplication: newFakeApplication()}
	// 模拟回退已发生：权威视图在 A，但 Bridge 订阅还钉在 B（由上一个场景
	// 的竞态残留，本测试直接构造这个漂移状态）。
	app.snapshotMu.Lock()
	app.snapshot.Session = application.SessionState{ID: "session-a"}
	app.snapshotMu.Unlock()

	bridge, err := NewBridge(app, Options{})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	emitted := make(chan emittedEvent, 16)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		bridge.Stop()
	})
	bridge.Start(ctx, func(_ context.Context, name string, payload any) {
		emitted <- emittedEvent{name: name, payload: payload}
	})
	if ready := waitEmitted(t, emitted); ready.name != "seelex:ready" {
		t.Fatalf("first event = %q, want seelex:ready", ready.name)
	}
	if len(app.subscribeIDs) != 1 || app.subscribeIDs[0] != "session-a" {
		t.Fatalf("initial subscription keys = %v, want session-a", app.subscribeIDs)
	}

	// 构造漂移：应用视图被异步切到 B 后又被 Bridge 之外的路径（无进程事件
	// 到达的极端竞态）改回 A；Bridge 订阅仍钉在 B。
	bridge.ResumeSession("session-b") // 正常切换：视图→B、订阅→B
	if len(app.subscribeIDs) != 2 || app.subscribeIDs[1] != "session-b" {
		t.Fatalf("subscription keys after switch = %v, want session-b", app.subscribeIDs)
	}
	// 应用内部把视图静默改回 A（模拟迟到回退），无任何事件先到。
	app.snapshotMu.Lock()
	app.snapshot.Session = application.SessionState{ID: "session-a"}
	app.snapshotMu.Unlock()

	// 用户再次点击会话 A：视图本来就在 A，但订阅漂移必须被纠正。
	if err := bridge.ResumeSession("session-a"); err != nil {
		t.Fatalf("ResumeSession: %v", err)
	}
	if len(app.subscribeIDs) != 3 || app.subscribeIDs[2] != "session-a" {
		t.Fatalf("subscription keys after same-session retry = %v, want rebuilt for session-a", app.subscribeIDs)
	}
}
