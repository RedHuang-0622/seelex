package seelebridge

import (
	"strings"
	"sync"

	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// 主会话与子代理 merge-back 的会话侧内部方法。公开会话端口见 ports.go。

// sessionBindings 归组 Runtime 的会话绑定状态：上下文存储、历史路由、
// DurableHistory、项目知识提供者、轮次归档器与当前主会话 ID。
// 原来 10 字段 + 6 把锁收敛为两把锁（data + mainSession）。
type sessionBindings struct {
	mu            sync.RWMutex
	ctxStore      *sessionstore.SessionContextStore
	historyRouter *sessionstore.Router
	mainHistory   *sessionstore.DurableHistory
	project       func() *sessionstore.ProjectRecord
	turnArchiver  seelexctx.TurnArchiver

	mainSessionMu sync.RWMutex
	mainSessionID string
}

func (b *sessionBindings) sessionID() string {
	b.mainSessionMu.RLock()
	defer b.mainSessionMu.RUnlock()
	return b.mainSessionID
}

func (b *sessionBindings) setSessionID(id string) {
	b.mainSessionMu.Lock()
	b.mainSessionID = id
	b.mainSessionMu.Unlock()
}

func (b *sessionBindings) setTurnArchiver(archiver seelexctx.TurnArchiver) {
	b.mu.Lock()
	b.turnArchiver = archiver
	b.mu.Unlock()
}

func (b *sessionBindings) getTurnArchiver() seelexctx.TurnArchiver {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.turnArchiver
}

func (b *sessionBindings) setMainHistory(history *sessionstore.DurableHistory) {
	b.mu.Lock()
	b.mainHistory = history
	b.mu.Unlock()
}

func (b *sessionBindings) attachHistoryRouter(router *sessionstore.Router) {
	b.mu.Lock()
	b.historyRouter = router
	b.mu.Unlock()
}

func (b *sessionBindings) getHistoryRouter() *sessionstore.Router {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.historyRouter
}

func (b *sessionBindings) prepareMainSessionHistory(sessionID string, messages []types.Message) bool {
	b.mu.RLock()
	history := b.mainHistory
	b.mu.RUnlock()
	if history == nil || history.SessionID() != sessionID {
		return false
	}
	history.PrepareNextLoad(messages)
	return true
}

func (b *sessionBindings) attachContextStore(store *sessionstore.SessionContextStore) {
	b.mu.Lock()
	b.ctxStore = store
	b.mu.Unlock()
}

func (b *sessionBindings) contextStore() *sessionstore.SessionContextStore {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.ctxStore
}

func (b *sessionBindings) setProjectKnowledge(provider func() *sessionstore.ProjectRecord) {
	b.mu.Lock()
	b.project = provider
	b.mu.Unlock()
}

// projectBlock 渲染项目级模块语义块（ProjectKnowledge，会话前预读缓存；
// 提供者未注入 → 无块）。
func (b *sessionBindings) projectBlock() *seelectx.PromptBlock {
	b.mu.RLock()
	provider := b.project
	b.mu.RUnlock()
	if provider == nil {
		return nil
	}
	record := provider()
	if record == nil {
		return nil
	}
	return seelexctx.RenderProjectBlock(*record)
}

// enqueueSubagentContext is the subagent-to-main merge-back write. It is
// delegated to the subagentContext actor: the command channel is bounded and
// never blocks the producer for long, and the actor queue preserves every
// message until Drain（2026-08-10 A/B 修复：mailbox 满不再静默丢弃）。
func (r *Runtime) enqueueSubagentContext(content string) {
	if r == nil || strings.TrimSpace(content) == "" {
		return
	}
	r.subagentContext.Enqueue(content)
}

// DrainSubagentContexts returns all currently queued merge-back messages. It
// never waits for a producer or Application lock and is called before a main
// ChatStream starts.
func (r *Runtime) DrainSubagentContexts() []string {
	if r == nil {
		return nil
	}
	return r.subagentContext.Drain()
}

// subagentContextDropped 返回 merge-back 队列溢出计数（诊断面）。
func (r *Runtime) subagentContextDropped() int64 {
	if r == nil {
		return 0
	}
	return r.subagentContext.Overflow()
}

// mergeBackIntoParent 把子代理快照合并进 Runtime 持有的父证据，并把合并
// 结果原子写回 parentEvidence（后续子代理/嵌套 fork 因此能看到先前子代理
// 的 Findings/Decisions——合并累积）。合并由 subagentContext actor 串行执行，
// 防止并行子代理各自基于旧快照合并后互相覆盖（2026-08-10 B 修复）。
func (r *Runtime) mergeBackIntoParent(child *snapshot.ContextSnapshot) *snapshot.ContextSnapshot {
	if r == nil || child == nil {
		return nil
	}
	return r.subagentContext.MergeBackIntoParent(child)
}
