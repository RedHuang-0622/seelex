package gui

import (
	"context"
	"sync"
	"time"
)

const (
	defaultGracefulCloseTimeout = 5 * time.Second
	// cancelDrainTimeout 是取消全部运行中会话后等待其收尾的预算：取消后每个
	// runChat 立即返回并走持久化收尾（逐会话 flush），正常远快于该上限；到点
	// 仍未收敛则按最佳努力退出（运行中会话由后续 app.Shutdown 兜底）。
	cancelDrainTimeout = 2 * time.Second
)

// sessionActivityApplication 是 Application 的可选扩展（G0c）：桌面宿主需要
// 判定「任一会话是否在运行」（视图空闲、后台在跑也必须 graceful drain），
// 并在超时后取消**全部**运行中会话——不只视图会话。
type sessionActivityApplication interface {
	AnyChatRunning() bool
	CancelAllChats()
}

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
	if !coordinator.hasActiveChat() {
		coordinator.mu.Unlock()
		return false
	}
	coordinator.waiting = true
	coordinator.mu.Unlock()

	go coordinator.finishWhenIdle()
	return true
}

// hasActiveChat 报告是否存在任一会话的运行中回合。生产 Application 实现
// AnyChatRunning（每会话单元权威状态；视图空闲、后台在跑也要等待），其它
// 实现回退到视图快照的 Chat.Running。
func (coordinator *closeCoordinator) hasActiveChat() bool {
	if app, ok := coordinator.app.(sessionActivityApplication); ok {
		return app.AnyChatRunning()
	}
	return coordinator.app.Snapshot().Chat.Running
}

func (coordinator *closeCoordinator) finishWhenIdle() {
	ctx, cancel := context.WithTimeout(context.Background(), coordinator.timeout)
	err := coordinator.app.WaitForIdle(ctx)
	cancel()
	if err != nil {
		// A tool, approval, or provider can fail to settle the chat state. Do
		// not leave the native window indefinitely rejected in that state.
		// 取消全部运行中会话（G0c：后台会话同样占用引擎；只取消视图会话会让
		// WaitForIdle 永远等不到后台收尾）。
		coordinator.cancelActiveChats()
		// 取消后 runChat 走正常收尾（逐会话 flush），再等一个短预算让其收敛，
		// 避免窗口在数据落盘完成前退出。
		drainCtx, drainCancel := context.WithTimeout(context.Background(), cancelDrainTimeout)
		_ = coordinator.app.WaitForIdle(drainCtx)
		drainCancel()
	}
	coordinator.completeClose()
}

// cancelActiveChats 取消全部运行中会话；不支持会话级活动面的宿主回退到只
// 取消视图会话（旧语义）。
func (coordinator *closeCoordinator) cancelActiveChats() {
	if app, ok := coordinator.app.(sessionActivityApplication); ok {
		app.CancelAllChats()
		return
	}
	coordinator.app.CancelChat("")
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
