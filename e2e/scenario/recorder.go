package scenario

import (
	"context"
	"sync"

	"github.com/RedHuang-0622/seelex/application"
)

type eventRecorder struct {
	mu            sync.Mutex
	events        []application.Event
	notifications chan struct{}
	subscription  application.Subscription
	done          chan struct{}
}

func newEventRecorder(subscription application.Subscription) *eventRecorder {
	recorder := &eventRecorder{
		notifications: make(chan struct{}, 1), subscription: subscription, done: make(chan struct{}),
	}
	go recorder.collect()
	return recorder
}

func (recorder *eventRecorder) collect() {
	defer close(recorder.done)
	for event := range recorder.subscription.Events {
		recorder.mu.Lock()
		recorder.events = append(recorder.events, event)
		recorder.mu.Unlock()
		select {
		case recorder.notifications <- struct{}{}:
		default:
		}
	}
}

func (recorder *eventRecorder) close() {
	recorder.subscription.Close()
	<-recorder.done
}

func (recorder *eventRecorder) snapshot() []application.Event {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]application.Event(nil), recorder.events...)
}

func (recorder *eventRecorder) waitFor(ctx context.Context, predicate func([]application.Event) bool) error {
	for {
		if predicate(recorder.snapshot()) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-recorder.notifications:
		}
	}
}

func (recorder *eventRecorder) waitForChange(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-recorder.notifications:
		return nil
	}
}
