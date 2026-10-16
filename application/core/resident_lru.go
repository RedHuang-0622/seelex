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
	"github.com/RedHuang-0622/seelex/middle/residency"
	"github.com/RedHuang-0622/seelex/middle/switch"
	"github.com/RedHuang-0622/seelex/session"
	"log"
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
	service.residentOrder = service.residentOrder.Touch(sessionID)
	if unit := service.sessions.Unit(sessionID); unit != nil {
		unit.SetResident(resident)
	}
}

// reconcileResidentLimit 按释放面口径收敛驻留引擎（引擎层）：受保护集（当前视图 /
// 待审批 / 运行·排队·在飞 / 恢复中）既不计数也不参与挑选；其余驻留会话按 LRU
// 释放，额度 = releaseBudget（默认统一窗口 1；resident_limit 显式 >0 时按"该层
// 总上限 − 受保护数"折算）。无候选则容忍超限，释放失败保持原状。
//
// 落点沿革：2026-10-17 随 **M5-R6-resident-lru** 接收者转移，**挑选与迭代判据**
// 归位 `middle/residency`（`EvictUntilWithinLimit`）——它与正文层用的是**同一份**
// 判据（两层只差读数与动作，见该文件头）；这里只留"取数 + 执行"两件事：
// `residentEvictionSnapshotLocked` 在同一临界区内取齐一次读数，`releaseSessionMemory`
// 做释放动作。口径与迁移前逐字一致。
func (service *Service) reconcileResidentLimit() {
	if service == nil {
		return
	}
	if _, ok := service.Deps.Engine.(residentEngine); !ok {
		return
	}
	residency.EvictUntilWithinLimit(residency.EvictionDeps{
		Snapshot: func() residency.EvictionSnapshot {
			service.ViewMu.Lock()
			defer service.ViewMu.Unlock()
			return service.residentEvictionSnapshotLocked()
		},
		Evict: func(sessionID string) error {
			return service.releaseSessionMemory(sessionID, releaseIdle)
		},
		OnEvictError: func(sessionID string, err error) {
			log.Printf("[session-release] engine evict %q: %v", sessionID, err)
		},
	})
}

// residentEvictionSnapshotLocked 在同一临界区内取齐一次收敛读数（调用方持有
// Core.ViewMu）：使用序不做事先收敛（引擎层的占用以"单元标记驻留 + 引擎真有
// 该会话"现算，见文件头「LRU 宿主归 core 的会话治理」）；受保护集只对**占用本层**
// 的会话读，与迁移前挑选循环的读取次序逐字一致。
func (service *Service) residentEvictionSnapshotLocked() residency.EvictionSnapshot {
	engine, ok := service.Deps.Engine.(residentEngine)
	if !ok {
		return residency.EvictionSnapshot{}
	}
	// residentOrder[0] = 最近使用 ⇒ 末尾最旧。
	snapshot := residency.EvictionSnapshot{
		// 与正文层同一口径：判据在临界区外遍历，`Order.Forget` 又会就地写底层数组 ⇒ 复制。
		Order:       append(residency.Order(nil), service.residentOrder...),
		Occupied:    make(map[string]bool, len(service.residentOrder)),
		Protected:   make(map[string]bool, len(service.residentOrder)),
		LegacyLimit: Limits().ResidentSessionLimit,
		Window:      Limits().ReleaseWindow(),
	}
	for _, sessionID := range service.residentOrder {
		if sessionID == "" {
			continue
		}
		unit := service.sessions.Unit(sessionID)
		if unit == nil || !unit.Resident() || !engine.HasSession(sessionID) {
			continue
		}
		snapshot.Occupied[sessionID] = true
		if service.protectedSessionLocked(sessionID) {
			snapshot.Protected[sessionID] = true
		}
	}
	return snapshot
}

// sessionBusy 报告会话是否正忙（释放面的守卫判据之一：忙的会话绝不释放）：
// 待审批、运行中、排队输入、在飞请求都算忙。
func (service *Service) sessionBusy(unit *session.SessionUnit) bool {
	return switching.Busy(sessionBusyState(unit))
}

// sessionBusyState 把会话单元的四个忙态读数装配成中间层的值（装配缝；判据在
// `middle/switch.Busy`，M5-R6-release-surface 接收者转移）。
func sessionBusyState(unit *session.SessionUnit) switching.BusyState {
	if unit == nil {
		return switching.BusyState{}
	}
	chat := unit.ChatState()
	return switching.BusyState{
		PendingApprovals: unit.PendingApprovalCount(),
		Running:          chat.Running,
		QueuedCount:      chat.QueuedCount,
		PendingRequests:  len(unit.PendingRequests()),
	}
}
