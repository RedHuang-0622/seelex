package gui

import (
	"context"
	"sync"
)

// closeCoordinator turns a window-close request into a graceful application
// shutdown. Once closing starts it rejects new input through the application,
// waits for accepted chat work to finish, and then invokes quit exactly once.
type closeCoordinator struct {
	app  Application
	quit func()

	mu      sync.Mutex
	waiting bool
	ready   bool
}

func newCloseCoordinator(app Application, quit func()) *closeCoordinator {
	return &closeCoordinator{app: app, quit: quit}
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
	if err := coordinator.app.WaitForIdle(context.Background()); err != nil {
		coordinator.mu.Lock()
		coordinator.waiting = false
		coordinator.mu.Unlock()
		return
	}
	coordinator.mu.Lock()
	coordinator.ready = true
	coordinator.mu.Unlock()
	if coordinator.quit != nil {
		coordinator.quit()
	}
}
