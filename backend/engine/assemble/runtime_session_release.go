package assemble

// 桥侧会话本体释放面（会话释放面的一格）：`bundles[sessionID]` 里的框架 Session
// （provider 历史 / DurableHistory）+ LoopHooks + 绑定状态（含会话上下文存储
// ctxStore）是"打开过一次这个会话"留下的最重的一份内存，而这张注册表在
// 2026-10-14 之前**只增不删**（每次驱逐只丢 application 侧端口引用，本体仍在；
// 见 docs/2026-10-14-memory-cpu-accumulation/verdict.md §3 A1/A2）。
//
// 调用方是 application/core 的释放面（session_release.go）：LRU 驱逐（releaseIdle）、
// 卸载 / 归档 / 删除（releaseFull）都会叫它。释放后重开走冷加载——`newMainSession`
// （bundleFor + attachContextStore）按磁盘事实重建会话与上下文存储；
// `SessionContextStoreFor` 在重建前返回 nil，这是既有契约（"未实例化/未绑定 → nil"）。

import (
	"github.com/RedHuang-0622/seelex/kernel/sessionpolicy"
)

// ReleaseSessionBundle 释放一个会话的桥侧内存占用：移出 bundles 注册表并清掉
// 同会话的 per-会话残格。返回是否真的释放。
//
// **拒绝释放**（返回 false，调用方保持驻留不做半途释放）的三种情形：
//   - 该会话是桥的活跃别名（activeSessionID）：legacy 读面仍指着它；
//   - fork_subagents 在飞（含这一批子代理作业还在跑）：子代理要写回父会话；
//   - 还有在途后台命令：它们的结果要落回该会话。
//
// 已经不在注册表里 ⇒ 返回 false（幂等：重复释放没有副作用）。
func (r *Runtime) ReleaseSessionBundle(sessionID string) bool {
	if r == nil {
		return false
	}
	// 判据（三种拒绝情形 + 顺序）在中间层：本层只把四格宿主读数接进去。
	return switching.ReleaseBundle(sessionID, switching.BundleReleaseDeps{
		ForkInFlight: r.ForkInFlight,
		AsyncPending: r.AsyncPendingFor,
		TakeBundle:   r.takeSessionBundle,
		DropResidue:  r.dropSessionResidue,
	})
}

// takeSessionBundle 在注册表锁内做"活跃别名 / 在册"两项检查并移出本体：这两步必须原子
// （拆开就是一个可被并发切换插进来的窗口），所以合成一格供域包调用。
func (r *Runtime) takeSessionBundle(sessionID string) bool {
	r.bundlesMu.Lock()
	if r.activeSessionID == sessionID {
		r.bundlesMu.Unlock()
		return false
	}
	bundle := r.bundles[sessionID]
	if bundle == nil {
		r.bundlesMu.Unlock()
		return false
	}
	delete(r.bundles, sessionID)
	r.bundlesMu.Unlock()
	return true
}

// dropSessionResidue 清掉同会话的 per-会话残格（调用方已把会话移出 bundles）。
// 这些格子都是"按会话只增不删"的登记表/缓存：不放掉就等于每开过一个会话
// 永久留一格（teamwork 协调器另含看板快照，是其中较重的一个）。
//
// 不在这里动的是 node 维度的残格（实时流分发器的回放窗口/订阅表按 nodeID 键控，
// 见 `backend/multiagent/subagent.LiveDispatcher`：
// 节点与主会话的映射不在本注册表里，映射错了会误删别的会话的回放窗口），
// 以及角色回合面（roleTurns 自己有成套的 ReleaseRoleSessions 收口）。
func (r *Runtime) dropSessionResidue(sessionID string) {
	// 路由面：本会话最近一次承接路由 + unpinned 去重表（键是 sessionID|account）——
	// 注册表在账号域（`account.ServedRouteRegistry.Forget`），这里只转发。
	if r.servedRoute != nil {
		r.servedRoute.Forget(sessionID)
	}

	// teamwork 编排面：按 (项目, 会话) 键控的协调器与看板缓存。下次采集按需重建
	// （缓存本来就是加速层，不是事实源）。
	r.teamworkMu.Lock()
	for key := range r.teamworkCoords {
		if key.SessionID == sessionID {
			delete(r.teamworkCoords, key)
		}
	}
	for key := range r.teamworkBoardCache {
		if key.SessionID == sessionID {
			delete(r.teamworkBoardCache, key)
		}
	}
	r.teamworkMu.Unlock()
}
