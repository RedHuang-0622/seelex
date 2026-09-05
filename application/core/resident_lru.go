package core

// G6 驻留 LRU：引擎 bundle（seelebridge HasSession）是驻留内存的重物件，
// Unit 是轻量会话事实挂载点。驻留上限走 seelexctx.Limits.resident_limit
// （默认 6）；驱逐前置 = 非 running/queued/awaiting_approval 且不驱逐当前
// 视图会话；驱逐前先 flush（非活跃会话 PersistCurrentSession），驱逐后
// Unit/View/Runtime 槽保留（Resident=false），重开走 cold_load——
// 驱逐后再进 = 冷（INV-G8）。
//
// LRU 宿主归 core 的会话治理（与 session.Domain/Coordinator 同生态位，
// 见 AGENTS.md 新功能归属决策）：seelebridge 只提供 bundle 注册与释放面
// （HasSession/UnloadSession），业务规则（状态守卫/flush/上限）不进引擎。

import (
	"log"

	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/session"
)

// residentEngine 是驻留 LRU 依赖的引擎面（可选能力：legacy 单会话引擎不
// 参与 LRU，避免进程级 bundle 被误驱逐）。
type residentEngine interface {
	HasSession(sessionID string) bool
	UnloadSession(sessionID string) error
}

// touchResident 记录一次会话引擎使用（冷加载/热切换/物化），并触发超限
// 驱逐（INV-G8）。调用方不得持有 Core.ViewMu（本方法自取锁）。
func (service *Service) touchResident(sessionID string) {
	if service == nil || sessionID == "" {
		return
	}
	engine, ok := service.Deps.Engine.(residentEngine)
	if !ok || !engine.HasSession(sessionID) {
		return
	}
	service.ViewMu.Lock()
	service.markResidentRecentLocked(sessionID, true)
	service.ViewMu.Unlock()
	service.reconcileResidentLimit()
}

// markResidentRecentLocked 把会话移到 LRU 使用序最前并标记驻留（调用方持
// 有 Core.ViewMu；resident=false 用于驱逐后移除序）。
func (service *Service) markResidentRecentLocked(sessionID string, resident bool) {
	next := make([]string, 0, len(service.residentOrder)+1)
	next = append(next, sessionID)
	for _, existing := range service.residentOrder {
		if existing != sessionID {
			next = append(next, existing)
		}
	}
	service.residentOrder = next
	if unit := service.sessions.Unit(sessionID); unit != nil {
		unit.SetResident(resident)
	}
}

// reconcileResidentLimit 在超限时按 LRU 驱逐空闲驻留会话（无候选则容忍
// 超限：运行/待批会话不可驱逐；驱逐失败保持原状，避免丢数据）。
func (service *Service) reconcileResidentLimit() {
	if service == nil {
		return
	}
	engine, ok := service.Deps.Engine.(residentEngine)
	if !ok {
		return
	}
	limit := Limits().ResidentSessionLimit
	if limit <= 0 {
		limit = seelexctx.DefaultLimits().ResidentSessionLimit
	}
	for {
		service.ViewMu.Lock()
		residentCount := 0
		for _, sid := range service.residentOrder {
			unit := service.sessions.Unit(sid)
			if unit != nil && unit.Resident() && engine.HasSession(sid) {
				residentCount++
			}
		}
		if residentCount <= limit {
			service.ViewMu.Unlock()
			return
		}
		candidate := service.pickEvictableCandidateLocked(engine)
		service.ViewMu.Unlock()
		if candidate == "" {
			// 超限但全部忙/视图会话：容忍（运行结束/审批结案后下轮 touch
			// 或显式卸载再收敛）。
			return
		}
		if err := service.evictResident(candidate, engine); err != nil {
			log.Printf("[resident-lru] evict %q: %v", candidate, err)
			return
		}
	}
}

// pickEvictableCandidateLocked 从 LRU 最旧端挑一个可驱逐会话（调用方持有
// Core.ViewMu）：非当前视图、引擎已驻留、非 busy（running/queued/
// awaiting_approval 都不可驱逐）。
func (service *Service) pickEvictableCandidateLocked(engine residentEngine) string {
	current := service.Core.Snapshot.Session.ID
	for index := len(service.residentOrder) - 1; index >= 0; index-- {
		sessionID := service.residentOrder[index]
		if sessionID == "" || sessionID == current {
			continue
		}
		if service.isRestoringLocked(sessionID) {
			continue
		}
		unit := service.sessions.Unit(sessionID)
		if unit == nil || !unit.Resident() || !engine.HasSession(sessionID) {
			continue
		}
		if service.sessionBusy(unit) {
			continue
		}
		return sessionID
	}
	return ""
}

// evictResident 驱逐一个空闲驻留会话：先 flush（非活跃持久化），再释放
// 引擎 bundle，保留 Unit/View/Runtime 槽（Resident=false）。
func (service *Service) evictResident(sessionID string, engine residentEngine) error {
	transition := service.transitionForKey(sessionID)
	transition.Lock()
	defer transition.Unlock()

	service.ViewMu.RLock()
	active := sessionID == service.Core.Snapshot.Session.ID
	unit := service.sessions.Unit(sessionID)
	busy := unit != nil && service.sessionBusy(unit)
	restoring := service.isRestoringLocked(sessionID)
	hasSession := engine.HasSession(sessionID)
	service.ViewMu.RUnlock()
	if active || busy || restoring || !hasSession {
		// 状态在挑选与驱逐之间已变：跳过，保持超限容忍。
		return nil
	}
	location := service.components.sessions.LocateSession(sessionID)
	if err := service.components.sessions.PersistCurrentSession(location, sessionID); err != nil {
		// flush 失败不卸载：引擎保留，数据不丢；下轮 touch 再试。
		return err
	}
	if err := engine.UnloadSession(sessionID); err != nil {
		return err
	}
	service.ViewMu.Lock()
	service.markResidentRecentLocked(sessionID, false)
	// 移出 LRU 序（SetResident(false) 后 pick 不再命中；此处彻底移除）。
	filtered := service.residentOrder[:0]
	for _, existing := range service.residentOrder {
		if existing != sessionID {
			filtered = append(filtered, existing)
		}
	}
	service.residentOrder = filtered
	service.ViewMu.Unlock()
	service.components.sessions.RequestCatalogRefresh()
	return nil
}

func (service *Service) sessionBusy(unit *session.SessionUnit) bool {
	if unit == nil {
		return false
	}
	if unit.PendingApprovalCount() > 0 {
		return true
	}
	chat := unit.ChatState()
	if chat.Running || chat.QueuedCount > 0 {
		return true
	}
	return len(unit.PendingRequests()) > 0
}
