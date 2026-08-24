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
}

func (fake *sessionAwareFakeApplication) SubmitToSession(_ context.Context, sessionID, text string) error {
	fake.submitted = sessionID + ":" + text
	return fake.submitToSessionErr
}

func (fake *sessionAwareFakeApplication) ActivateSession(sessionID string) error {
	fake.activated = sessionID
	return nil
}

func (fake *sessionAwareFakeApplication) SnapshotOf(sessionID string) (application.Snapshot, error) {
	fake.snapshotOfID = sessionID
	return application.Snapshot{}, nil
}

func (fake *sessionAwareFakeApplication) SubscribeSession(_ string, _ int) (application.Subscription, error) {
	return application.Subscription{}, fake.subscribeErr
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
