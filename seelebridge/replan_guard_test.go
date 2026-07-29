package seelebridge

import (
	"errors"
	"testing"
	"time"
)

func TestReplanGuardLimitsDuplicateConcurrencyAndRate(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	guard := newReplanGuard(1, 2, 2, time.Minute)
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
	guard := newReplanGuard(2, 4, 1, time.Minute)
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
