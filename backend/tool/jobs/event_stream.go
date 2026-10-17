// Package jobs 承载 Seelex 的**异步作业域**：作业生命周期的事件投影（订阅
// `jobs.Manager` 的变化信号，把在册作业的新状态投影成会话级事实事件）与后台执行表
// 到 Application 侧的只读投影。
//
// 物理位置：2026-10-17 按 plan.md §2.6（`seelebridge/runtime_async.go`、`jobs_events.go`
// → `backend/tool/jobs/`）从 `backend/engine/assemble` 归位；装配层只剩薄转发与接线。
// 作业表的**状态**不在本包：teammate 作业活在 Seele 的 `jobs.Manager`（装配层持有），
// 后台命令执行表活在 `backend/tool/registry`（`Router.AsyncRuns()`）。本包只做只读搬运。
package jobs

// event_stream.go 是**团队作业的生命周期事件流**（Seelex 侧构建）。
//
// 为什么事件流不留在 Seele `jobs`：
//
//   - `event.Sink` 的实现必须在 `jobs.Manager` 的**构造期**定下，而构造期拿不到
//     「这条作业属于哪个会话的哪条事件流」——Seele 的 `event.Recorder` 是**单例、
//     序号全局**，而会话事件库（`store.EventStore`）是**按会话**追加、按
//     会话排序的。
//   - 于是框架侧发出来的事件 **append 不到会话事件流的尾部**，只能由产品事后
//     **回填**会话归属（这也正是 `backend/engine/obs/event_correlation.go` 里
//     `CorrelateMainSessionID` 对 workplan runner 事件做的事）；而且全局序号在按会话排序下不成立，
//     与 Seelex 自己的时间基序号（`uint64(at.UnixNano())`，见 `obs.SummaryLog` /
//     `plan.AppendPhase`）也对不齐。
//
// 所以这里只消费框架给的**信号口 + 读面**：订阅 `jobs.Manager.Events()`——**经一次扇出**
// （装配层的 `teamwork_job_signals.go`：上游是容量 1 的单接收者通道，而"事件投影"与"终态触发回合"
// 两个读侧动作都要跟着它走）——每次被唤醒就对在册作业做一次 `Snapshot`，把**新出现的状态**
// 投影成 `frameworkevent.Event`——补上 `agent.runtime` 会话定位、序号取时间基——再经装配期
// 注入的 persister 追加到该会话的事件库。
//
// 事件是**观察**，不是唤醒：它绝不把作业结果投递进忙会话。

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	frameworkevent "github.com/RedHuang-0622/Seele/event"
	seelejobs "github.com/RedHuang-0622/Seele/jobs"
)

// EventSource 是作业生命周期事件在统一事件库里的稳定 Source 标识
// （与 workplan.runner / seelex.subagent / seelex.telemetry.summary 同库共存）。
const EventSource = "seelex.jobs"

// MainAgentID 是主会话在事件定位里的稳定标识（与 plan 域同一取值口径；
// `store.EventStore` 只按 agent.runtime 的 session_id 路由，agent_id 仅供读面）。
const MainAgentID = "seelex-main"

// EventStream 把作业生命周期投影成会话事件流。
//
// 一个 Runtime 只有一条这样的流（作业表本身也是进程内单例）：它是"某个作业出现了
// 新状态"的**读侧**投影器，而不是作业表的第二个写者。
type EventStream struct {
	reader    SnapshotReader
	persister func() func(context.Context, frameworkevent.Event) error
	// signals 是**扇出后**的变化信号口（见装配层的 teamwork_job_signals.go）：上游
	// jobs.Manager.Events() 只能有一个读者，本投影读它的一份订阅。
	signals <-chan struct{}
	clock   func() time.Time

	stop chan struct{}
	done chan struct{}
	once sync.Once

	mu      sync.Mutex
	started bool
	seen    map[seelejobs.Handle]seelejobs.State
}

// SnapshotReader 是事件投影需要的**唯一一格**框架读数（`seelejobs.Manager` 的读面）。
// 端口取窄而不是直接吃整个 `jobs.Manager`：投影只读快照、不派发、不取回、不终止
// （结构满足 + 编译期断言 > 适配层，见下）。
type SnapshotReader interface {
	Snapshot(scope seelejobs.Scope) []seelejobs.Record
}

// 编译期钉子：Seele 的 `jobs.Manager` 在**结构上**满足本端口（不需要适配层）。
var _ SnapshotReader = (seelejobs.Manager)(nil)

// NewEventStream 构造事件流；persister 是**延迟读取**的持久化钩子取用器——装配顺序上
// `SetTeamworkBackend` 可能早于 `SetEventPersister`，因此不能在构造期取定。
// signals 由装配层从扇出器订阅（nil = 没有信号可读，投影就只做一次全量对齐）。
//
// reader 为 nil 时返回 nil（没有作业表就没有可投影的事实），Start/Close 对 nil 接收者安全。
func NewEventStream(reader SnapshotReader, persister func() func(context.Context, frameworkevent.Event) error, signals <-chan struct{}) *EventStream {
	if reader == nil {
		return nil
	}
	return &EventStream{
		reader:    reader,
		persister: persister,
		signals:   signals,
		clock:     time.Now,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		seen:      map[seelejobs.Handle]seelejobs.State{},
	}
}

// Start 起投影协程（幂等；nil 接收者安全）。
func (stream *EventStream) Start() {
	if stream == nil {
		return
	}
	stream.mu.Lock()
	stream.started = true
	stream.mu.Unlock()
	go stream.loop()
}

// Close 停止投影协程（幂等；nil 接收者安全）。
//
// 没起过协程就直接返回：停机链上 Close 可能与"从未 Start"（装配中途失败）相遇，
// 等一个永远不会被关闭的 done 会把停机钉死。
func (stream *EventStream) Close() {
	if stream == nil {
		return
	}
	stream.once.Do(func() { close(stream.stop) })
	stream.mu.Lock()
	started := stream.started
	stream.mu.Unlock()
	if started {
		<-stream.done
	}
}

// loop 订阅变更信号口，每次被唤醒投影一次增量。
func (stream *EventStream) loop() {
	defer close(stream.done)
	// 先做一次全量对齐：装配之前已存在的在册作业也必须出现在事件流里。
	stream.drain()
	signals := stream.signals
	for {
		select {
		case <-stream.stop:
			return
		case _, ok := <-signals:
			if !ok {
				return
			}
			stream.drain()
		}
	}
}

// drain 把"自上次投影以来新出现的状态"追加到各会话的事件库。
//
// 它只读快照、不推进任何游标、不销项：取回（Fetch）依旧是消费式的，事件投影不得
// 改变面板上的在册作业。销项后的句柄从句柄记忆里移除（句柄的序号单调、永不复用，
// 命名空间见 dto.JobNamespace）。
func (stream *EventStream) drain() {
	records := stream.reader.Snapshot(seelejobs.Scope{})
	present := make(map[seelejobs.Handle]struct{}, len(records))
	for _, record := range records {
		present[record.Handle] = struct{}{}
		stream.mu.Lock()
		previous, known := stream.seen[record.Handle]
		stream.seen[record.Handle] = record.State
		stream.mu.Unlock()
		if known && previous == record.State {
			continue
		}
		stream.append(record)
	}
	stream.mu.Lock()
	for handle := range stream.seen {
		if _, ok := present[handle]; !ok {
			delete(stream.seen, handle)
		}
	}
	stream.mu.Unlock()
}

// append 把一条作业读数的当下状态投影为一个会话级事实事件。
func (stream *EventStream) append(record seelejobs.Record) {
	sessionID := record.Scope.Session
	if sessionID == "" {
		// 没有会话归属就没有可以追加的尾部：宁可丢弃，也不回填到别的会话。
		return
	}
	persister := stream.persister
	if persister == nil {
		return
	}
	appendEvent := persister()
	if appendEvent == nil {
		return
	}
	at := stream.clock()
	content, _ := json.Marshal(map[string]any{
		"state":     string(record.State),
		"exit_code": record.ExitCode,
		"summary":   record.Summary,
	})
	event := frameworkevent.Event{
		// 序号取时间基（与 Seelex 自己的事件同策略）：本库按会话排序，
		// 框架 recorder 的进程级序号在这里不成立。
		Sequence:   uint64(at.UnixNano()),
		OccurredAt: at,
		Source:     EventSource,
		Type:       frameworkevent.TypeLifecycle,
		Status:     eventStatus(record.State),
		Scope: frameworkevent.Scope{
			AgentID:    record.Scope.Subject,
			NodeID:     record.Node,
			ToolCallID: string(record.Handle),
		},
		Locations: []frameworkevent.Location{{
			Kind: "agent.runtime",
			IDs:  map[string]string{"agent_id": MainAgentID, "session_id": sessionID},
		}},
		Attributes: map[string]string{
			"handle":  string(record.Handle),
			"kind":    string(record.Kind),
			"node":    record.Node,
			"batch":   record.Batch,
			"subject": record.Scope.Subject,
		},
		Content: content,
	}
	if err := appendEvent(context.Background(), event); err != nil {
		// 观察失败不改变控制流（见 Seele event/README.md）：只记一行，不返回错误。
		log.Printf("seelex: 追加作业事件失败（句柄 %s）：%v", record.Handle, err)
	}
}

// eventStatus 把作业状态映射为框架事件状态。
func eventStatus(state seelejobs.State) frameworkevent.Status {
	switch state {
	case seelejobs.StateDone:
		return frameworkevent.StatusCompleted
	case seelejobs.StateFailed:
		return frameworkevent.StatusFailed
	case seelejobs.StateKilled:
		return frameworkevent.StatusCanceled
	default:
		return frameworkevent.StatusRunning
	}
}
