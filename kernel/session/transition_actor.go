package session_runtime

// SessionTransitionActor 以 actor 方式显式管理"会话切换互斥"状态：
// transitionInFlight 只由单一 goroutine 持有，外部经 channel 命令
// Acquire/Release，不使用互斥锁（无锁化）。
//
// 相比 sync.Mutex：
//   - 状态归属显式（单 owner、单 goroutine 读写），可审计；
//   - 命令队列即等待队列：等待者 FIFO，无锁序/重入问题；
//   - 引擎锁等外部阻塞不会形成 ABBA（actor 只维护自身状态，不调用外部）；
//   - 可安全 Close：关闭后 Acquire/Release 退化为无操作，不会 panic 或
//     永久阻塞调用方（配合 Shutdown/测试 teardown）。
type SessionTransitionActor struct {
	cmds   chan transitionCmd
	stopCh chan struct{}
	done   chan struct{}
}

type transitionCmd struct {
	acquire bool
	reply   chan struct{} // acquire 被授予时关闭（cap=1，防泄漏）
}

// NewSessionTransitionActor 启动切换互斥 actor（单 goroutine）。
func NewSessionTransitionActor() *SessionTransitionActor {
	actor := &SessionTransitionActor{
		cmds:   make(chan transitionCmd, 16),
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}
	go actor.loop()
	return actor
}

// loop 是 actor 的唯一状态持有者：inFlight 与等待队列只在本 goroutine 内
// 读写，天然无竞争。
func (actor *SessionTransitionActor) loop() {
	defer close(actor.done)
	inFlight := false
	waiters := make([]chan struct{}, 0, 4)
	for {
		select {
		case cmd := <-actor.cmds:
			switch {
			case cmd.acquire:
				if inFlight {
					waiters = append(waiters, cmd.reply)
					continue
				}
				inFlight = true
				close(cmd.reply)
			default: // release
				inFlight = false
				if len(waiters) > 0 {
					next := waiters[0]
					waiters = waiters[1:]
					inFlight = true
					close(next)
				}
			}
		case <-actor.stopCh:
			return
		}
	}
}

// Acquire 阻塞直到获得切换互斥（FIFO；Close 后退化为无操作）。
func (actor *SessionTransitionActor) Acquire() {
	if actor == nil {
		return
	}
	reply := make(chan struct{}, 1)
	select {
	case actor.cmds <- transitionCmd{acquire: true, reply: reply}:
	case <-actor.stopCh:
		return
	}
	select {
	case <-reply:
	case <-actor.stopCh:
	}
}

// Release 释放切换互斥（未持有也可调用：空操作；Close 后同样安全）。
func (actor *SessionTransitionActor) Release() {
	if actor == nil {
		return
	}
	select {
	case actor.cmds <- transitionCmd{acquire: false}:
	case <-actor.stopCh:
	}
}

// Close 停止 actor goroutine（幂等）。契约：调用方须保证无活跃持有者；
// Shutdown/测试 teardown 使用。停止后 Acquire/Release 均为空操作。
func (actor *SessionTransitionActor) Close() {
	if actor == nil {
		return
	}
	select {
	case <-actor.stopCh:
		return
	default:
		close(actor.stopCh)
		<-actor.done
	}
}

// transitionLocker 把 actor 适配为 sync.Locker（既有 TransitionLock 调用
// 方零改动；无 mutex，显式 actor 状态）。
type transitionLocker struct {
	actor *SessionTransitionActor
}

func (locker transitionLocker) Lock()   { locker.actor.Acquire() }
func (locker transitionLocker) Unlock() { locker.actor.Release() }
