package session

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/merger"
	"github.com/RedHuang-0622/seelex/seelexctx/provider"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

// ── 子代理上下文 actor（装配件拆分第一步）────────────────────────
// 原 Runtime 直接持有 parentEvidence / subagentMailbox / overflow 三组状态
// 和三把锁，父证据的「读-合并-写回」与 merge-back 消息队列耦合在上帝对象里。
//
// 本组件把全部可变状态收进单一 goroutine（actor 模型）：外部通过命令
// channel 投递操作，actor 串行处理——天然串行化父证据合并（不再需要
// parentEvidenceMu），mailbox 满时溢出队列只在 actor 内访问（不再需要
// subagentOverflowMu）。读取面（nodeParentEvidence）经 atomic.Pointer
// 无锁快照，不阻塞 actor。
//
// 生命周期：NewRuntime 时创建，Runtime.Shutdown 关闭命令通道并等待退出。
// 命令通道有界；actor 停摆（极端：被外部泄漏的 reply 阻塞）时投递带超时，
// 超时返回 nil 而非永久阻塞子代理。
type SubagentContextActor struct {
	cmd   chan subagentContextCmd
	state atomic.Pointer[snapshot.ContextSnapshot] // 读取面（actor 写，外部无锁读）
	trace provider.TraceSource                     // 投影时提取 Findings/Decisions（nil 降级）
	// queueCap 是 merge-back 队列的 soft cap：超过后仅计数 overflow，
	// 内容仍保留（不丢）；测试可注入小值触发溢出路径。
	queueCap int
	done     chan struct{}
	once     sync.Once
	wg       sync.WaitGroup

	// 以下字段只在 actor goroutine 内访问。
	queue    []string     // merge-back 文本队列（channel 语义；Drain 全量回收）
	overflow atomic.Int64 // 队列超容量的累计（诊断；内容仍保留在 queue）
}

type subagentContextCmd struct {
	kind subagentContextCmdKind
	// merge
	child *snapshot.ContextSnapshot
	reply chan subagentContextReply
	// enqueue
	content string
	// projection
	projection *model.ParentEvidenceProjection
}

type subagentContextCmdKind int

const (
	subagentCmdMerge subagentContextCmdKind = iota
	subagentCmdEnqueue
	subagentCmdSetProjection
	subagentCmdDrain
	subagentCmdShutdown
)

// subagentContextReply 是命令应答（drain 携带全量队列；merge 携带合并快照）。
type subagentContextReply struct {
	items []string
	merge *snapshot.ContextSnapshot
}

const (
	subagentContextCmdCap     = 256
	subagentContextQueueCap   = 4096
	subagentContextCmdTimeout = 10 * time.Second
)

func NewSubagentContextActor(trace provider.TraceSource, queueCap ...int) *SubagentContextActor {
	cap := subagentContextQueueCap
	if len(queueCap) > 0 && queueCap[0] > 0 {
		cap = queueCap[0]
	}
	actor := &SubagentContextActor{
		cmd:      make(chan subagentContextCmd, subagentContextCmdCap),
		trace:    trace,
		queueCap: cap,
		done:     make(chan struct{}),
	}
	actor.wg.Add(1)
	go actor.run()
	return actor
}

func (a *SubagentContextActor) run() {
	defer a.wg.Done()
	for {
		select {
		case cmd, ok := <-a.cmd:
			if !ok {
				return
			}
			a.handle(cmd)
		case <-a.done:
			return
		}
	}
}

func (a *SubagentContextActor) handle(cmd subagentContextCmd) {
	switch cmd.kind {
	case subagentCmdMerge:
		a.handleMerge(cmd)
	case subagentCmdEnqueue:
		a.handleEnqueue(cmd.content)
	case subagentCmdSetProjection:
		a.handleSetProjection(cmd.projection)
		if cmd.reply != nil {
			cmd.reply <- subagentContextReply{}
		}
	case subagentCmdDrain:
		if cmd.reply != nil {
			cmd.reply <- subagentContextReply{items: a.drainLocked()}
		}
	}
}

// handleMerge 串行执行「读当前父证据 → 合并子代理快照 → 写回」并发布
// 读取面快照。合并文本入队由调用方负责（agent_node.mergeBack 在拿到合并
// 快照后 Format + Enqueue），避免双重入队。
func (a *SubagentContextActor) handleMerge(cmd subagentContextCmd) {
	parent := a.NodeParentEvidence()
	if parent == nil {
		parent = &snapshot.ContextSnapshot{
			SourceSessionID: cmd.child.SourceSessionID,
			ExportedAt:      time.Now(),
		}
	}
	if cmd.child != nil {
		if err := merger.NewMerger().MergeBack(parent, cmd.child); err == nil {
			a.state.Store(parent)
		}
	}
	if cmd.reply != nil {
		cmd.reply <- subagentContextReply{merge: parent}
	}
}

func (a *SubagentContextActor) handleEnqueue(content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	if len(a.queue) >= a.queueCap {
		// soft cap 只作诊断计数，绝不丢弃：merge-back 结果必须在 Drain 时
		// 全量回收（A 修复语义；内存上界由 fork 规模决定，可接受）。
		a.overflow.Add(1)
	}
	a.queue = append(a.queue, content)
}

func (a *SubagentContextActor) handleSetProjection(projection *model.ParentEvidenceProjection) {
	if projection == nil || strings.TrimSpace(projection.SessionID) == "" {
		a.state.Store(nil)
		return
	}
	evidence := seelexctx.ExportSnapshotFromData(
		projection.SessionID,
		projection.Goal,
		projection.ConversationCount,
		a.trace,
	)
	a.state.Store(evidence)
}

func (a *SubagentContextActor) drainLocked() []string {
	items := a.queue
	a.queue = nil
	a.overflow.Store(0)
	return items
}

// send 投递命令（带超时；actor 关闭后快速返回 false）。
func (a *SubagentContextActor) send(cmd subagentContextCmd) bool {
	if a == nil {
		return false
	}
	timer := time.NewTimer(subagentContextCmdTimeout)
	defer timer.Stop()
	select {
	case a.cmd <- cmd:
		return true
	case <-timer.C:
		return false
	}
}

// SetParentEvidenceProjection 应用侧发布父证据投影。命令投递后等待 actor
// 处理完成再返回——保持与旧同步 Store 相同的可见性（后续读取面立即读到
// 新投影）；空白 session 清空旧证据。
func (a *SubagentContextActor) SetParentEvidenceProjection(projection model.ParentEvidenceProjection) {
	if a == nil {
		return
	}
	reply := make(chan subagentContextReply, 1)
	cmd := subagentContextCmd{kind: subagentCmdSetProjection, projection: &projection, reply: reply}
	if !a.send(cmd) {
		return
	}
	select {
	case <-reply:
	case <-time.After(subagentContextCmdTimeout):
	}
}

// MergeBackIntoParent 投递合并命令并等待 actor 串行完成，返回合并后的
// 快照（供调用方 Format 注入；nil 表示投递超时/actor 关闭）。
func (a *SubagentContextActor) MergeBackIntoParent(child *snapshot.ContextSnapshot) *snapshot.ContextSnapshot {
	if a == nil || child == nil {
		return nil
	}
	reply := make(chan subagentContextReply, 1)
	cmd := subagentContextCmd{kind: subagentCmdMerge, child: child, reply: reply}
	if !a.send(cmd) {
		return nil
	}
	select {
	case result := <-reply:
		return result.merge
	case <-time.After(subagentContextCmdTimeout):
		return nil
	}
}

// Enqueue 投递 merge-back 文本（mergeBackSink 使用；actor 内入队不丢）。
func (a *SubagentContextActor) Enqueue(content string) {
	if a == nil {
		return
	}
	a.send(subagentContextCmd{kind: subagentCmdEnqueue, content: content})
}

// Drain 投递并等待全量 merge-back 文本（channel + overflow 合并回收）。
func (a *SubagentContextActor) Drain() []string {
	if a == nil {
		return nil
	}
	reply := make(chan subagentContextReply, 1)
	cmd := subagentContextCmd{kind: subagentCmdDrain, reply: reply}
	if !a.send(cmd) {
		return nil
	}
	select {
	case result := <-reply:
		return result.items
	case <-time.After(subagentContextCmdTimeout):
		return nil
	}
}

// Overflow 返回队列超容量累计（诊断；内容不丢）。
func (a *SubagentContextActor) Overflow() int64 {
	if a == nil {
		return 0
	}
	return a.overflow.Load()
}

// NodeParentEvidence 返回当前父证据快照的无锁拷贝（读取面；nil = 无证据）。
// 供根包 nodeParentEvidence 门面与 actor 内部 handleMerge 共用。
func (a *SubagentContextActor) NodeParentEvidence() *snapshot.ContextSnapshot {
	if a == nil {
		return nil
	}
	return cloneContextSnapshot(a.state.Load())
}

// Close 关闭命令通道并等待 actor 退出（幂等）。
func (a *SubagentContextActor) Close() {
	if a == nil {
		return
	}
	a.once.Do(func() { close(a.done) })
	a.wg.Wait()
}
