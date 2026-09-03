package plan

import (
	"errors"
	"testing"
	"time"
)

func TestReplanGuardLimitsDuplicateConcurrencyAndRate(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	guard := NewReplanGuard(1, 2, 2, time.Minute)
	guard.now = func() time.Time { return now }

	finish, err := guard.acquire("replan-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guard.acquire("replan-a"); !errors.Is(err, ErrReplanDuplicate) {
		t.Fatalf("duplicate error = %v, want %v", err, ErrReplanDuplicate)
	}
	if _, err := guard.acquire("replan-b"); !errors.Is(err, ErrReplanConcurrencyLimit) {
		t.Fatalf("concurrency error = %v, want %v", err, ErrReplanConcurrencyLimit)
	}
	finish(nil)

	finish, err = guard.acquire("replan-b")
	if err != nil {
		t.Fatal(err)
	}
	finish(nil)
	if _, err := guard.acquire("replan-c"); !errors.Is(err, ErrReplanRateLimit) {
		t.Fatalf("rate error = %v, want %v", err, ErrReplanRateLimit)
	}
	metrics := guard.snapshot()
	if metrics.Accepted != 2 || metrics.Succeeded != 2 || metrics.Rejected != 3 || metrics.DuplicateRejected != 1 || metrics.WindowAttempts != 2 || metrics.ProviderWindowRequests != 0 {
		t.Fatalf("metrics = %+v", metrics)
	}

	now = now.Add(time.Minute + time.Nanosecond)
	finish, err = guard.acquire("replan-c")
	if err != nil {
		t.Fatal(err)
	}
	finish(errors.New("provider failed"))
	metrics = guard.snapshot()
	if metrics.WindowAttempts != 1 || metrics.Failed != 1 {
		t.Fatalf("metrics after window reset = %+v", metrics)
	}
}

func TestReplanGuardCapsProviderRequestsBeforeRetry(t *testing.T) {
	guard := NewReplanGuard(2, 4, 1, time.Minute)
	finish, err := guard.acquire("replan-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.acquireProviderRequest(); err != nil {
		t.Fatal(err)
	}
	err = guard.acquireProviderRequest()
	if !errors.Is(err, ErrReplanProviderBudget) {
		t.Fatalf("provider budget error = %v, want %v", err, ErrReplanProviderBudget)
	}
	finish(err)
	metrics := guard.snapshot()
	if metrics.ProviderRequests != 1 || metrics.ProviderWindowRequests != 1 || metrics.Rejected != 1 || metrics.Failed != 1 {
		t.Fatalf("metrics = %+v", metrics)
	}
}

// TestReplanGuardsIsolateBySession（G1/M5）：每个会话独立额度槽——A 占用
// 并发额度与重复键不会影响 B；统计按会话分离。
func TestReplanGuardsIsolateBySession(t *testing.T) {
	guards := NewReplanGuards(1, 1, 1, time.Minute)

	finishA, err := guards.For("sess-a").acquire("replan-a")
	if err != nil {
		t.Fatal(err)
	}
	// A 的同键重复被拒、并发上限在 A 槽内生效。
	if _, err := guards.For("sess-a").acquire("replan-a"); !errors.Is(err, ErrReplanDuplicate) {
		t.Fatalf("session A duplicate err = %v, want ErrReplanDuplicate", err)
	}
	if _, err := guards.For("sess-a").acquire("replan-a2"); !errors.Is(err, ErrReplanConcurrencyLimit) {
		t.Fatalf("session A concurrency err = %v, want ErrReplanConcurrencyLimit", err)
	}
	// B 是独立槽：A 占用不阻塞 B。
	finishB, err := guards.For("sess-b").acquire("replan-b")
	if err != nil {
		t.Fatalf("session B acquire blocked by session A slot: %v", err)
	}
	if got := guards.MetricsFor("sess-a").InFlight; got != 1 {
		t.Fatalf("session A in-flight = %d, want 1", got)
	}
	if got := guards.MetricsFor("sess-b").InFlight; got != 1 {
		t.Fatalf("session B in-flight = %d, want 1", got)
	}
	if got := guards.MetricsFor("sess-a").Accepted; got != 1 {
		t.Fatalf("session A accepted = %d, want 1", got)
	}
	if got := guards.MetricsFor("sess-b").Accepted; got != 1 {
		t.Fatalf("session B accepted = %d, want 1", got)
	}
	finishA(nil)
	finishB(nil)
	if got := guards.MetricsFor("sess-a").Succeeded; got != 1 {
		t.Fatalf("session A succeeded = %d, want 1", got)
	}
}

// TestReplanGuardsDefaultSlotForEmptySessionID 无 sid legacy 路径归默认槽，
// 与显式空槽统计一致。
func TestReplanGuardsDefaultSlotForEmptySessionID(t *testing.T) {
	guards := NewReplanGuards(2, 2, 2, time.Minute)
	finish, err := guards.For("").acquire("legacy-key")
	if err != nil {
		t.Fatal(err)
	}
	finish(nil)
	if got := guards.MetricsFor("").Accepted; got != 1 {
		t.Fatalf("default slot accepted = %d, want 1", got)
	}
}
