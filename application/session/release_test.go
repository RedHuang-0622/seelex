package session

import (
	"errors"
	"reflect"
	"testing"
)

func TestReleasePreservesOrderingAndIgnoresProtectedIdleSession(t *testing.T) {
	var trace []string
	application := NewReleaseApplication(ReleaseDeps{
		Protected: func(string) bool { return true },
		Persist: func(string) error {
			t.Fatal("idle release must stop before persist for a protected session")
			return nil
		},
		FlushToolRecord:   func(string) {},
		UnloadEngine:      func(string) error { return nil },
		ReleaseBundle:     func(string) {},
		ForgetGoal:        func(string) {},
		ReleaseContent:    func(string) {},
		ReleaseFull:       func(string) {},
		ForgetOrders:      func(string) {},
		ReleaseToolRecord: func(string) {},
		RequestRefresh:    func() {},
		ReconcileResident: func() {},
		ReconcileContent:  func() {},
	})

	if err := application.Release("session-a", ReleaseIdle); err != nil {
		t.Fatal(err)
	}
	if len(trace) != 0 {
		t.Fatalf("protected idle release performed work: %v", trace)
	}
}

func TestReleaseFullOrdering(t *testing.T) {
	var trace []string
	application := NewReleaseApplication(ReleaseDeps{
		Protected:       func(string) bool { return false },
		Persist:         func(string) error { trace = append(trace, "persist"); return nil },
		FlushToolRecord: func(string) { trace = append(trace, "flush_tools") },
		UnloadEngine:    func(string) error { trace = append(trace, "unload_engine"); return nil },
		ReleaseBundle:   func(string) { trace = append(trace, "release_bundle") },
		ForgetGoal:      func(string) { trace = append(trace, "forget_goal") },
		ReleaseContent:  func(string) { trace = append(trace, "release_content") },
		ReleaseFull:     func(string) { trace = append(trace, "release_full") },
		ForgetOrders:    func(string) { trace = append(trace, "forget_orders") },
		ReleaseToolRecord: func(string) {
			trace = append(trace, "release_tool_records")
		},
		RequestRefresh:    func() { trace = append(trace, "refresh") },
		ReconcileResident: func() { trace = append(trace, "reconcile_resident") },
		ReconcileContent:  func() { trace = append(trace, "reconcile_content") },
	})

	if err := application.Release("session-a", ReleaseIdle); err != nil {
		t.Fatal(err)
	}
	application.Reconcile()

	want := []string{
		"persist", "flush_tools", "unload_engine", "release_bundle", "forget_goal",
		"release_content", "forget_orders", "refresh", "reconcile_resident", "reconcile_content",
	}
	if !reflect.DeepEqual(trace, want) {
		t.Fatalf("release order = %v, want %v", trace, want)
	}
}

func TestReleaseStopsOnPersistError(t *testing.T) {
	persistErr := errors.New("disk unavailable")
	unloaded := false
	application := NewReleaseApplication(ReleaseDeps{
		Protected:       func(string) bool { return false },
		Persist:         func(string) error { return persistErr },
		FlushToolRecord: func(string) {},
		UnloadEngine: func(string) error {
			unloaded = true
			return nil
		},
	})

	if err := application.Release("session-a", ReleaseIdle); err == nil || !errors.Is(err, persistErr) {
		t.Fatalf("release error = %v, want wrapped persist error", err)
	}
	if unloaded {
		t.Fatal("engine unloaded after persist failure")
	}
}
