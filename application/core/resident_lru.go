package core

// G6 驻留 LRU：引擎 bundle（seelebridge HasSession）是驻留内存的重物件，
// Unit 是轻量会话事实挂载点。**释放面口径见 session_release.go**（受保护集 +
// 窗口 = limits.session_release_window，默认 1 ⇒ 只保留上一个）；本文件只负责
// 引擎层的使用序与收敛循环：
//   - 使用序：touchResident 把会话移到序首（索引 0 = 最近使用）；
//   - 收敛：受保护集不参与计数也不参与挑选，其余驻留会话按 LRU 释放，额度 =
//     releaseBudget（默认统一窗口；resident_limit 显式 >0 时按老口径折算）；
//   - 释放动作统一走 releaseSessionMemory（flush → 引擎 → 桥侧本体 → 正文），
//     不再在这里自己调引擎卸载——只丢引擎引用等于把"驱逐"做成空操作。
//
// 无候选（全忙/全被视图占用）则容忍超限；释放失败保持原状（下一轮 touch 再试），
// 避免丢数据（INV-G8）。
//
// LRU 宿主归 core 的会话治理（与 session.Domain/Coordinator 同生态位，
// 见 AGENTS.md 新功能归属决策）：seelebridge 只提供 bundle 注册与释放面
// （HasSession/UnloadSession/ReleaseSession），业务规则（状态守卫/flush/上限）
// 不进引擎。

import (
	"log"

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

// reconcileResidentLimit 按释放面口径收敛驻留引擎（引擎层）：受保护集（当前视图 /
// 待审批 / 运行·排队·在飞 / 恢复中）既不计数也不参与挑选；其余驻留会话按 LRU
// 释放，额度 = releaseBudget（默认统一窗口 1；resident_limit 显式 >0 时按"该层
// 总上限 − 受保护数"折算）。无候选则容忍超限，释放失败保持原状。
func (service *Service) reconcileResidentLimit() {
	if service == nil {
		return
	}
	engine, ok := service.Deps.Engine.(residentEngine)
	if !ok {
		return
	}
	for {
		service.ViewMu.Lock()
		protected := 0
		// residentOrder[0] = 最近使用 ⇒ 末尾最旧；可释放集合按使用序收集，
		// 因此集合末尾就是本次该释放的那个。
		evictable := make([]string, 0, len(service.residentOrder))
		for _, sessionID := range service.residentOrder {
			unit := service.sessions.Unit(sessionID)
			if sessionID == "" || unit == nil || !unit.Resident() || !engine.HasSession(sessionID) {
				continue
			}
			if service.protectedSessionLocked(sessionID) {
				protected++
				continue
			}
			evictable = append(evictable, sessionID)
		}
		budget := releaseBudget(Limits().ResidentSessionLimit, protected)
		if len(evictable) <= budget {
			service.ViewMu.Unlock()
			return
		}
		candidate := evictable[len(evictable)-1]
		service.ViewMu.Unlock()
		if err := service.releaseSessionMemory(candidate, releaseIdle); err != nil {
			log.Printf("[session-release] engine evict %q: %v", candidate, err)
			return
		}
	}
}

// sessionBusy 报告会话是否正忙（释放面的守卫判据之一：忙的会话绝不释放）：
// 待审批、运行中、排队输入、在飞请求都算忙。
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
