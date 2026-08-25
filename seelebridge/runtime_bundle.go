package seelebridge

import (
	"sync"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
)

// sessionBundle 是一个逻辑会话在 seelebridge 侧的独立运行时槽位：独立主
// 会话对象、独立锁与独立会话绑定状态（ctxStore/mainHistory/mainSessionID）。
//
// 决策契约（docs/research/2026-08-24-session-resource-granularity.md §决策
// 契约）：runtime 生成与生命周期、锁、actor 保护层级一律会话粒度且不传播；
// fork 会话深拷贝 = 新建 bundle + 拷贝数据面，不共享本结构任何实例句柄。
// 因此本结构禁止整体复制（内部含 sync.Mutex 与活动句柄），子会话必须经
// bundleFor 新建。
type sessionBundle struct {
	mu      sync.Mutex
	session *frameworkSession.Session
	hooks   *frameworkSession.LoopHooks
	binding sessionBindings
}

// bundleFor 返回 sessionID 对应的 bundle（不存在时新建并登记），并把
// activeSessionID 指向该会话（新建/恢复/切换的主入口语义：目标会话成为
// 当前会话）。注册表由 bundlesMu 保护；返回后使用 bundle 内部锁。
func (r *Runtime) bundleFor(sessionID string) *sessionBundle {
	if r == nil {
		return nil
	}
	r.bundlesMu.Lock()
	defer r.bundlesMu.Unlock()
	bundle := r.bundles[sessionID]
	if bundle == nil {
		bundle = &sessionBundle{}
		r.bundles[sessionID] = bundle
	}
	r.activeSessionID = sessionID
	return bundle
}

// activeBundle 返回当前激活会话的 bundle（不存在返回 nil）。只读面
// （Session()/sessionContextStore() 等）使用；运行中切换由 application
// 单飞门控保证顺序。
func (r *Runtime) activeBundle() *sessionBundle {
	if r == nil {
		return nil
	}
	r.bundlesMu.RLock()
	defer r.bundlesMu.RUnlock()
	if r.activeSessionID == "" {
		return nil
	}
	return r.bundles[r.activeSessionID]
}
