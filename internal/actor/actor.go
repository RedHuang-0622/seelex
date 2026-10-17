// Package actor 提供有界命令通道 + 单消费者 goroutine 的通用底座，
// 吸收各域手写 actor 的样板：通道创建、done 关闭、WaitGroup、带超时投递、
// 幂等 Close。handler 由调用方闭包提供；命令的回复通道由命令自身携带
// （回复类型域相关，不由本包感知）。
package actor

import (
	"sync"
	"sync/atomic"
	"time"
)

// defaultCap 是命令通道的默认容量（有界；满则投递阻塞或超时）。
const defaultCap = 256

// Actor 是单消费者 actor 底座：外部经 Send 投递命令，handler 在唯一
// goroutine 内串行执行，天然免锁。
type Actor[T any] struct {
	mailbox   chan T
	done      chan struct{}
	closeOnce sync.Once
	closed    atomic.Bool
	wg        sync.WaitGroup
}

// Options 是 New 的构造选项。
type Options struct {
	// Cap 命令通道容量（0 = 无缓冲）。
	Cap int
}

// Option 修改构造选项。
type Option func(*Options)

// WithCap 设置命令通道容量（0 = 无缓冲；默认 256）。
func WithCap(cap int) Option {
	return func(options *Options) {
		options.Cap = cap
	}
}

// New 构造并启动 actor：handler 在单一 goroutine 内串行处理每条命令。
func New[T any](handler func(T), options ...Option) *Actor[T] {
	opts := Options{Cap: defaultCap}
	for _, apply := range options {
		apply(&opts)
	}
	mailbox := make(chan T, opts.Cap)
	actor := &Actor[T]{
		mailbox: mailbox,
		done:    make(chan struct{}),
	}
	actor.wg.Add(1)
	go func() {
		defer actor.wg.Done()
		for {
			select {
			case command, ok := <-mailbox:
				if !ok {
					return
				}
				handler(command)
			case <-actor.done:
				return
			}
		}
	}()
	return actor
}

// Send 投递命令（阻塞直到入队；actor 已关闭返回 false）。
func (a *Actor[T]) Send(command T) bool {
	if a == nil || a.closed.Load() {
		return false
	}
	select {
	case a.mailbox <- command:
		return true
	case <-a.done:
		return false
	}
}

// SendTimeout 投递命令（上限 timeout；超时或 actor 已关闭返回 false）。
func (a *Actor[T]) SendTimeout(command T, timeout time.Duration) bool {
	if a == nil || a.closed.Load() {
		return false
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case a.mailbox <- command:
		return true
	case <-timer.C:
		return false
	case <-a.done:
		return false
	}
}

// TrySend 非阻塞投递（通道满或 actor 已关闭返回 false）。
func (a *Actor[T]) TrySend(command T) bool {
	if a == nil || a.closed.Load() {
		return false
	}
	select {
	case a.mailbox <- command:
		return true
	default:
		return false
	}
}

// Done 返回 actor 关闭信号（调用方可在回复等待中 select 快返）。
func (a *Actor[T]) Done() <-chan struct{} {
	if a == nil {
		return nil
	}
	return a.done
}

// Close 幂等关闭：close(done)，消费者 goroutine 处理完当前命令后退出。
func (a *Actor[T]) Close() {
	if a == nil {
		return
	}
	a.closeOnce.Do(func() {
		a.closed.Store(true)
		close(a.done)
	})
}

// Wait 等待消费者 goroutine 退出（应在 Close 之后调用）。
func (a *Actor[T]) Wait() {
	if a == nil {
		return
	}
	a.wg.Wait()
}
