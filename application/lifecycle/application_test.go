package lifecycle

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWaitForIdleReturnsWhenIdle(t *testing.T) {
	idle := make(chan struct{})
	close(idle)
	application := New(Deps{Idle: func() <-chan struct{} { return idle }})
	if err := application.WaitForIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestWaitForIdleHonorsContext(t *testing.T) {
	application := New(Deps{Idle: func() <-chan struct{} { return make(chan struct{}) }})
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if err := application.WaitForIdle(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func TestShutdownRunsFixedSequenceOnce(t *testing.T) {
	var calls []string
	closed := false
	application := New(Deps{
		BeginShutdown: func() bool {
			if closed {
				return false
			}
			closed = true
			calls = append(calls, "begin")
			return true
		},
		FlushToolRecords:       func() { calls = append(calls, "flush") },
		StopCatalogRefresh:     func() { calls = append(calls, "catalog") },
		CloseSessions:          func() { calls = append(calls, "sessions") },
		StopLifecycleConsumers: func() { calls = append(calls, "consumers") },
		CloseWorkTable:         func() { calls = append(calls, "worktable") },
		ShutdownApproval:       func() { calls = append(calls, "approval") },
	})
	application.Shutdown()
	application.Shutdown()
	want := "begin,flush,catalog,sessions,consumers,worktable,approval"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls = %v, want %s", calls, want)
	}
}

func TestFaultGuardDegradesOnceThenPanics(t *testing.T) {
	calls := 0
	guard := NewFaultGuard(FaultGuardDeps{
		EnterDegraded:    func() bool { calls++; return true },
		StopCatalog:      func() {},
		CloseSessions:    func() {},
		StopConsumers:    func() {},
		CloseWorkTable:   func() {},
		ShutdownApproval: func() {},
		PublishExit:      func() {},
	})
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected panic to be rethrown")
		}
		if calls != 1 {
			t.Fatalf("degrade calls = %d, want 1", calls)
		}
	}()
	guard.HandleLifecycleFault(errors.New("boom"))
}
