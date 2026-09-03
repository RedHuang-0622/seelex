package gui

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/application"
)

// sessionAwareFakeApplication 是带会话级扩展的 Application 桩
// （embedded *fakeApplication 提供其余接口面）。
type sessionAwareFakeApplication struct {
	*fakeApplication
	submitToSessionErr error
	activated          string
	snapshotOfID       string
	subscribeErr       error
	subscribeIDs       []string
}

func (fake *sessionAwareFakeApplication) SubmitToSession(_ context.Context, sessionID, text string) error {
	fake.submitted = sessionID + ":" + text
	return fake.submitToSessionErr
}

func (fake *sessionAwareFakeApplication) ActivateSession(sessionID string) error {
	fake.activated = sessionID
	fake.snapshotMu.Lock()
	fake.snapshot.Session = application.SessionState{ID: sessionID}
	fake.snapshotMu.Unlock()
	return nil
}

func (fake *sessionAwareFakeApplication) ResumeSession(sessionID string) error {
	fake.resumedSession = sessionID
	fake.snapshotMu.Lock()
	fake.snapshot.Session = application.SessionState{ID: sessionID}
	fake.snapshotMu.Unlock()
	return nil
}

func (fake *sessionAwareFakeApplication) ForkSessionLatest(sessionID string) (string, error) {
	fake.forkedSession = sessionID
	child := "child-" + sessionID
	fake.snapshotMu.Lock()
	fake.snapshot.Session = application.SessionState{ID: child}
	fake.snapshotMu.Unlock()
	return child, nil
}

func (fake *sessionAwareFakeApplication) BeginNewSession() error {
	fake.beganNewSession = true
	// G4 先行：草稿从新建即持有早分配的真实 SID。
	fake.snapshotMu.Lock()
	fake.snapshot.Session = application.SessionState{ID: "draft-session", Draft: true}
	fake.snapshotMu.Unlock()
	return nil
}

func (fake *sessionAwareFakeApplication) SnapshotOf(sessionID string) (application.SessionSnapshot, error) {
	fake.snapshotOfID = sessionID
	return application.SessionSnapshot{
		ProtocolVersion: application.ProtocolVersion,
		Session:         application.SessionState{ID: sessionID},
		Capabilities:    application.Capabilities{SessionResume: true, SessionSnapshot: true},
	}, nil
}

func (fake *sessionAwareFakeApplication) SubscribeSession(sessionID string, buffer int) (application.Subscription, error) {
	fake.subscribeIDs = append(fake.subscribeIDs, sessionID)
	if fake.subscribeErr != nil {
		return application.Subscription{}, fake.subscribeErr
	}
	return fake.hub.SubscribeSession(sessionID, buffer), nil
}

func TestBridgeSessionAwareAPIsRouteToApplication(t *testing.T) {
	app := &sessionAwareFakeApplication{fakeApplication: newFakeApplication()}
	bridge, err := NewBridge(app, Options{})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}

	if err := bridge.SubmitToSession("s1", "hi"); err != nil {
		t.Fatalf("SubmitToSession: %v", err)
	}
	if app.submitted != "s1:hi" {
		t.Fatalf("SubmitToSession routed text = %q, want %q", app.submitted, "s1:hi")
	}
	if err := bridge.ActivateSession("s2"); err != nil {
		t.Fatalf("ActivateSession: %v", err)
	}
	if app.activated != "s2" {
		t.Fatalf("ActivateSession routed id = %q, want s2", app.activated)
	}
	if _, err := bridge.SnapshotOf("s3"); err != nil {
		t.Fatalf("SnapshotOf: %v", err)
	}
	if app.snapshotOfID != "s3" {
		t.Fatalf("SnapshotOf routed id = %q, want s3", app.snapshotOfID)
	}
	if _, err := bridge.SubscribeSession("s4", 8); err != nil {
		t.Fatalf("SubscribeSession: %v", err)
	}
}

func TestBridgeSessionAwareAPIsFallBackWhenUnsupported(t *testing.T) {
	bridge, err := NewBridge(newFakeApplication(), Options{})
	if err != nil {
		t.Fatalf("NewBridge: %v", err)
	}
	if err := bridge.SubmitToSession("s1", "hi"); err == nil {
		t.Fatal("SubmitToSession on unsupported app should error")
	}
	if err := bridge.ActivateSession("s1"); err == nil {
		t.Fatal("ActivateSession on unsupported app should error")
	}
	if _, err := bridge.SnapshotOf("s1"); err == nil {
		t.Fatal("SnapshotOf on unsupported app should error")
	}
	if _, err := bridge.SubscribeSession("s1", 8); err == nil {
		t.Fatal("SubscribeSession on unsupported app should error")
	}
}

// TestBridgeRelaySubscribesToViewOnce 验证订阅键含显式当前视图 sid（G2）：
// Bridge 只以 Snapshot().Session.ID 订阅一次；会话切换（Resume/Activate/
// Fork/New）成功且视图变化后重建订阅（切换即重订阅）。中继只做透传，
// 投递序号由 Hub 赋值。路由正确性见
// application/core/subscribe_session_test.go。
func TestBridgeRelaySubscribesToViewOnce(t *testing.T) {
	app := &sessionAwareFakeApplication{fakeApplication: newFakeApplication()}
	app.snapshotMu.Lock()
	app.snapshot.Session = application.SessionState{ID: "session-b"}
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
	if len(app.subscribeIDs) != 1 || app.subscribeIDs[0] != "session-b" {
		t.Fatalf("subscription keys = %v, want one explicit session-b subscription", app.subscribeIDs)
	}

	// 视图会话之外的事件在投递端就不属于本订阅，因此根本不到达渲染层。
	app.hub.PublishSession(application.EventMessageDelta, 2, "", "session-background", application.MessageDelta{MessageID: "m-bg"})
	// 视图会话自身的 snapshot.changed 可达，且携带订阅内投递序号。
	app.hub.PublishSession(application.EventSnapshotChanged, 3, "", "session-b", nil)
	relayed := waitEmitted(t, emitted)
	if relayed.name != eventName {
		t.Fatalf("relayed event name = %q, want %q", relayed.name, eventName)
	}
	event, ok := relayed.payload.(application.Event)
	if !ok {
		t.Fatalf("relayed payload type = %T, want application.Event", relayed.payload)
	}
	if event.SessionID != "session-b" || event.DeliverySeq == 0 {
		t.Fatalf("relayed event = %+v, want session-b event with hub-assigned delivery seq", event)
	}
	select {
	case extra := <-emitted:
		t.Fatalf("background-session event reached the renderer: %+v", extra)
	default:
	}

	// 切换/新建/分支成功且视图变化时重建订阅（订阅键含 sid）。
	if err := bridge.ResumeSession("session-b"); err != nil {
		t.Fatalf("ResumeSession: %v", err)
	}
	if len(app.subscribeIDs) != 1 {
		t.Fatalf("resume to the same view must not rebuild the subscription: keys=%v", app.subscribeIDs)
	}
	if err := bridge.ActivateSession("session-c"); err != nil {
		t.Fatalf("ActivateSession: %v", err)
	}
	if len(app.subscribeIDs) != 2 || app.subscribeIDs[1] != "session-c" {
		t.Fatalf("subscription keys after ActivateSession = %v, want rebuilt for session-c", app.subscribeIDs)
	}
	child, err := bridge.ForkSessionLatest("session-c")
	if err != nil {
		t.Fatalf("ForkSessionLatest: %v", err)
	}
	if child != "child-session-c" || len(app.subscribeIDs) != 3 || app.subscribeIDs[2] != child {
		t.Fatalf("subscription keys after ForkSessionLatest = %v (child=%s), want rebuilt for %s", app.subscribeIDs, child, child)
	}
	if err := bridge.BeginNewSession(); err != nil {
		t.Fatalf("BeginNewSession: %v", err)
	}
	if len(app.subscribeIDs) != 4 || app.subscribeIDs[3] != "draft-session" {
		t.Fatalf("subscription keys after BeginNewSession = %v, want rebuilt for draft-session", app.subscribeIDs)
	}
}
