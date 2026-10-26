package table

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestAsyncConsumerDispatchesSignalsAndStops(t *testing.T) {
	asyncEvents := make(chan struct{}, 1)
	teamworkEvents := make(chan struct{}, 1)
	stop := make(chan struct{})
	refreshed := make(chan struct{}, 1)
	var asyncCalls atomic.Int32
	var teamworkCalls atomic.Int32

	consumer := NewAsyncConsumer(AsyncConsumerDeps{
		AsyncEvents:    asyncEvents,
		TeamworkEvents: teamworkEvents,
		Stop:           stop,
		Refresh: func() {
			refreshed <- struct{}{}
		},
		TriggerAsync: func(map[string]struct{}) int {
			asyncCalls.Add(1)
			return 0
		},
		TriggerTeamwork: func(map[string]struct{}) int {
			teamworkCalls.Add(1)
			return 0
		},
	})
	done := make(chan struct{})
	go func() {
		consumer.Run()
		close(done)
	}()

	asyncEvents <- struct{}{}
	select {
	case <-refreshed:
	case <-time.After(time.Second):
		t.Fatal("async signal did not refresh the worktable")
	}
	if asyncCalls.Load() != 1 {
		t.Fatalf("async trigger calls = %d", asyncCalls.Load())
	}

	teamworkEvents <- struct{}{}
	deadline := time.After(time.Second)
	for teamworkCalls.Load() != 1 {
		select {
		case <-deadline:
			t.Fatalf("teamwork trigger calls = %d", teamworkCalls.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}

	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("consumer did not stop")
	}
}

func TestAsyncConsumerRetriesPendingCompletions(t *testing.T) {
	asyncEvents := make(chan struct{}, 1)
	stop := make(chan struct{})
	var calls atomic.Int32
	consumer := NewAsyncConsumer(AsyncConsumerDeps{
		AsyncEvents: asyncEvents,
		Stop:        stop,
		Refresh:     func() {},
		TriggerAsync: func(map[string]struct{}) int {
			if calls.Add(1) == 1 {
				return 1
			}
			return 0
		},
		RetryDelay: 5 * time.Millisecond,
	})
	done := make(chan struct{})
	go func() {
		consumer.Run()
		close(done)
	}()
	asyncEvents <- struct{}{}
	deadline := time.After(time.Second)
	for calls.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf("retry calls = %d, want at least 2", calls.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("consumer did not stop after retry")
	}
}

func TestAsyncConsumerWithoutSignalsReturns(t *testing.T) {
	done := make(chan struct{})
	go func() {
		NewAsyncConsumer(AsyncConsumerDeps{}).Run()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("consumer without signal sources did not return")
	}
}
