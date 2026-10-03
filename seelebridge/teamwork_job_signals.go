package seelebridge

// teamwork_job_signals.go — teammate 作业信号口的**一次扇出**。
//
// 背景：Seele `jobs.Manager.Events()` 是**容量 1、单接收者**的通道（latest-wins：
// "有事发生"，不推进游标、不进上下文）。同一次发送只会交给先等待的那一个读者，
// 两个消费者抢它 = 后起的那个永远收不到（application 侧 consumeAsyncRuns 的注释里
// 记过同一个坑）。
//
// 但 teammate 作业现在有**两个**读侧动作要跟着它走：
//   - 事件投影（jobs_events.go：把在册作业的新状态追加到会话事件库）；
//   - 终态触发回合（application 的 triggerTeamworkJobCompletions：空闲会话"做完自动返回"）。
//
// 所以上游只许有一个读者：本扇出读一次，广播给所有订阅者。订阅通道各自容量 1、
// 丢新保旧（与上游同语义：信号只承诺"有事发生"，被合并掉的中间态由读侧重读全量补齐）。
//
// 与 Runtime 的生命周期同序（见 SetTeamworkBackend 的登记顺序）：停机时先停扇出、
// 再停事件投影、最后取消在途作业。

import "sync"

// teamworkJobSignals 是 jobs.Manager.Events() 的扇出器。
type teamworkJobSignals struct {
	mu      sync.Mutex
	subs    []chan struct{}
	started bool
	stop    chan struct{}
	done    chan struct{}
}

func newTeamworkJobSignals() *teamworkJobSignals {
	return &teamworkJobSignals{stop: make(chan struct{}), done: make(chan struct{})}
}

// subscribe 登记一个新订阅者（容量 1；广播时通道已满就跳过这一次）。
//
// 订阅必须在 start 之前或之后都安全：广播时对当前订阅表做快照，后加的订阅者不会
// 收到它之前的那次信号——订阅者首次唤醒本就该自己重读全量（jobs.Manager 的读面是
// 唯一事实），因此"错过一次信号"不会丢事实。
func (h *teamworkJobSignals) subscribe() <-chan struct{} {
	if h == nil {
		return nil
	}
	channel := make(chan struct{}, 1)
	h.mu.Lock()
	h.subs = append(h.subs, channel)
	h.mu.Unlock()
	return channel
}

// start 起扇出协程（幂等；upstream 为 nil 时不起——没有作业面就没有信号）。
func (h *teamworkJobSignals) start(upstream <-chan struct{}) {
	if h == nil || upstream == nil {
		return
	}
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return
	}
	h.started = true
	h.mu.Unlock()
	go func() {
		defer close(h.done)
		for {
			select {
			case <-h.stop:
				return
			case _, ok := <-upstream:
				if !ok {
					return
				}
				h.broadcast()
			}
		}
	}()
}

// broadcast 把一次信号派给所有订阅者（非阻塞：满了就是"已经有一次待读"，够用）。
func (h *teamworkJobSignals) broadcast() {
	h.mu.Lock()
	subs := append([]chan struct{}(nil), h.subs...)
	h.mu.Unlock()
	for _, channel := range subs {
		select {
		case channel <- struct{}{}:
		default:
		}
	}
}

// close 停扇出协程（幂等）。未 start 过时直接返回——不能等一个不会关闭的 done。
func (h *teamworkJobSignals) close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	started := h.started
	h.mu.Unlock()
	if !started {
		return
	}
	h.stopOnce()
	<-h.done
}

func (h *teamworkJobSignals) stopOnce() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stop == nil {
		return
	}
	select {
	case <-h.stop:
	default:
		close(h.stop)
	}
}
