package core

import "sync"

// ── worktable.changed CSP 汇聚发布器 ─────────────────────────
// 高并发数据流转走 channel 的 CSP 模型：
//  - 生产端：状态变更后把最新表格（不可变快照）Send 进缓冲 channel（cap=1），
//    消费者忙时阻塞等待（有界背压，不会无限排队）；
//  - 消费端：单 goroutine 阻塞接收，突发期间 drain 到"最新一次"再发布
//    （latest-wins 汇聚，避免 subagent 工具事件洪峰逐条 JSON）；
//  - 关闭：排空最后一次更新后退出，保证尾态不丢。

// worktableUpdate 是一次工作表格快照（revision/requestID 与 items 必须在
// 同一临界区生成，保证内容与修订号一致）。
type worktableUpdate struct {
	revision  uint64
	requestID string
	items     []WorkItem
}

// workTablePublisher 是 worktable.changed 的汇聚发布器。
type workTablePublisher struct {
	updates chan worktableUpdate
	publish func(worktableUpdate)
	done    chan struct{}
	once    sync.Once
}

func newWorkTablePublisher(publish func(worktableUpdate)) *workTablePublisher {
	publisher := &workTablePublisher{
		updates: make(chan worktableUpdate, 1),
		publish: publish,
		done:    make(chan struct{}),
	}
	go publisher.loop()
	return publisher
}

// Send 投递一次表格更新（CSP 阻塞语义：消费者在途时等待；关闭后快速返回）。
func (publisher *workTablePublisher) Send(update worktableUpdate) {
	if publisher == nil {
		return
	}
	select {
	case publisher.updates <- update:
	case <-publisher.done:
	}
}

func (publisher *workTablePublisher) loop() {
	for {
		select {
		case <-publisher.done:
			publisher.drainAndPublish()
			return
		case update := <-publisher.updates:
			publisher.publish(publisher.drainLatest(update))
		}
	}
}

// drainLatest 消费缓冲区内积压的全部更新，返回最后一次（latest-wins）。
func (publisher *workTablePublisher) drainLatest(initial worktableUpdate) worktableUpdate {
	latest := initial
	for {
		select {
		case more := <-publisher.updates:
			latest = more
		default:
			return latest
		}
	}
}

// drainAndPublish 关闭路径：排空积压并发布尾态（best-effort）。
func (publisher *workTablePublisher) drainAndPublish() {
	select {
	case update := <-publisher.updates:
		publisher.publish(publisher.drainLatest(update))
	default:
	}
}

// Close 优雅关闭：停止消费者 goroutine 并发布尾态。
func (publisher *workTablePublisher) Close() {
	if publisher == nil {
		return
	}
	publisher.once.Do(func() { close(publisher.done) })
}
