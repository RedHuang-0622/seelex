package gui

import (
	"context"
	"sync"
	"time"
)

const defaultGracefulCloseTimeout = 5 * time.Second

// closeCoordinator turns a window-close request into a bounded graceful
// application shutdown. Once closing starts it rejects new input through the
// application, waits briefly for accepted work, and cancels a stalled chat
// before invoking quit exactly once.
type closeCoordinator struct {
	app  Application
	quit func()

	mu      sync.Mutex
	waiting bool
	ready   bool
	timeout time.Duration
}

func newCloseCoordinator(app Application, quit func()) *closeCoordinator {
	return newCloseCoordinatorWithTimeout(app, quit, defaultGracefulCloseTimeout)
}

func newCloseCoordinatorWithTimeout(app Application, quit func(), timeout time.Duration) *closeCoordinator {
	if timeout <= 0 {
		timeout = defaultGracefulCloseTimeout
	}
	return &closeCoordinator{app: app, quit: quit, timeout: timeout}
}

// BeforeClose returns true when the native window close must be prevented.
func (coordinator *closeCoordinator) BeforeClose() bool {
	coordinator.mu.Lock()
	if coordinator.ready {
		coordinator.mu.Unlock()
		return false
	}
	if coordinator.waiting {
		coordinator.mu.Unlock()
		return true
	}

	coordinator.app.BeginGracefulShutdown()
	if !coordinator.app.Snapshot().Chat.Running {
		coordinator.mu.Unlock()
		return false
	}
	coordinator.waiting = true
	coordinator.mu.Unlock()

	go coordinator.finishWhenIdle()
	return true
}

func (coordinator *closeCoordinator) finishWhenIdle() {
	ctx, cancel := context.WithTimeout(context.Background(), coordinator.timeout)
	err := coordinator.app.WaitForIdle(ctx)
	cancel()
	if err != nil {
		// A tool, approval, or provider can fail to settle the chat state. Do
		// not leave the native window indefinitely rejected in that state.
		coordinator.app.CancelChat("")
	}
	coordinator.completeClose()
}

func (coordinator *closeCoordinator) completeClose() {
	coordinator.mu.Lock()
	if coordinator.ready {
		coordinator.mu.Unlock()
		return
	}
	coordinator.ready = true
	coordinator.waiting = false
	coordinator.mu.Unlock()
	if coordinator.quit != nil {
		coordinator.quit()
	}
}
