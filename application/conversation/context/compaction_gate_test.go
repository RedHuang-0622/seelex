package context

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/kernel/state"
)

func TestCompactionGateSerializesOneSession(t *testing.T) {
	gate := NewCompactionGate(CompactionGateDeps{Core: state.New(contract.Dependencies{})})
	if err := gate.Acquire(context.Background(), "session-a"); err != nil {
		t.Fatal(err)
	}
	acquired := make(chan error, 1)
	go func() { acquired <- gate.Acquire(context.Background(), "session-a") }()
	select {
	case err := <-acquired:
		t.Fatalf("second acquire returned before release: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	gate.Release("session-a")
	select {
	case err := <-acquired:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("second acquire did not wake")
	}
	gate.Release("session-a")
}

func TestCompactionGateAcquireHonorsContext(t *testing.T) {
	gate := NewCompactionGate(CompactionGateDeps{Core: state.New(contract.Dependencies{})})
	if err := gate.Acquire(context.Background(), "session-a"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := gate.Acquire(ctx, "session-a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want canceled", err)
	}
}
