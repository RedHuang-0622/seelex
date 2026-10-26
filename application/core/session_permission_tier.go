package core

// session_permission_tier.go — 权限档位的**宿主适配 + 门面转发**。
//
// 判据与编排本体已在 M5-R-limitc 随接收者转移归位 `backend/limit/tier`（plan §2.8）：
// 生效档位解析、会话级持久化（读/写/落地/恢复）与"视图快照重算"的口径都在那边，
// 本文件只剩三件事：
//  1. `serviceTierHost`：把 Service 的会话槽、视图锁、执行门、进程默认档位实现成
//     `tier.Deps` 需要的窄端口；
//  2. 原方法名保留为一行转发（`persistPermissionTier` / `readStoredPermissionTier` /
//     `applyStoredPermissionTier` / `restorePermissionTierFor`），外部与 core 内部调用点零改动；
//  3. 主会话权限档位的**落点说明**（历史结论，逐字保留）。
//
// # 为什么落点是"会话级用户设置"而不是 SessionRecord
//
// 这是实测结论，不是设计偏好：现行存储布局（v8/S20）下 record 通道已退役——
// SaveRecordRaw 只把 payload 里的 status/title 写穿到目录面，LoadRecordRaw 交回的
// 是 derivedRecordPayload 派生的 (version/id/status/updated_at/conversation)。
// 往 SessionRecord 加字段既写不进去也读不回来。会话级用户设置落在项目级会话元数据
// blob（与置顶/别名/排序位同一份存储），它的存在理由恰好就是我们要的：不随回合结束的
// 整体重建被覆盖、跨重启保留。

import (
	"strings"

	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/switch/sessionusecase"
)

// tierDeps 装配档位用例的宿主面（每次调用构造一次值语义的 Deps；无状态、无锁）。
func (service *Service) tierDeps() applicationpolicy.TierDeps {
	if service == nil {
		return applicationpolicy.TierDeps{}
	}
	return applicationpolicy.TierDeps{
		Settings:    service.settingPort(),
		Units:       serviceTierUnits{service: service},
		Approval:    service.Approval,
		View:        serviceTierView{service: service},
		Gate:        service.tierGate(),
		DefaultTier: func() string { return service.permissionTierDefault },
	}
}

// tierGate 返回"把档位写进执行门"的闭包；宿主没装配运行面时整步 no-op（与迁移前同口径）。
func (service *Service) tierGate() applicationpolicy.TierGate {
	if service == nil {
		return nil
	}
	return func(sessionID, tier string) {
		if service.Deps.Runtime == nil {
			return
		}
		_ = service.Deps.Runtime.SetPermissionTierFor(sessionID, tier)
	}
}

// settingPort 返回会话端口的可选"会话级用户设置"扩展（未装配 = 档位退回内存态）。
func (service *Service) settingPort() session.SessionSettingPort {
	if service == nil || service.Deps.Sessions == nil {
		return nil
	}
	port, ok := service.Deps.Sessions.(session.SessionSettingPort)
	if !ok {
		return nil
	}
	return port
}

// serviceTierUnits 把会话域实现成 tier.Units（Unit 自带锁，这里不加锁）。
type serviceTierUnits struct{ service *Service }

func (units serviceTierUnits) UnitPermissionTier(sessionID string) (string, bool) {
	unit := units.service.sessions.Unit(sessionID)
	if unit == nil {
		return "", false
	}
	return unit.PermissionTier()
}

func (units serviceTierUnits) SetUnitPermissionTier(sessionID, tier string) {
	if unit := units.service.sessions.Unit(sessionID); unit != nil {
		unit.SetPermissionTier(tier)
	}
}

// serviceTierView 把视图侧读写面实现成 tier.View（锁语义由 tier.Deps 的注释约定）。
type serviceTierView struct{ service *Service }

func (view serviceTierView) ViewSessionID() string { return view.service.currentViewSessionID() }

func (view serviceTierView) SnapshotSessionID() string {
	return view.service.Core.Snapshot.Session.ID
}

func (view serviceTierView) WriteTier(sessionID, effective string) {
	view.service.Core.Snapshot.Runtime.PermissionTier = effective
	view.service.Core.Snapshot.Runtime.FullAccess = dto.PermissionTierIsFullAccess(effective)
}

// persistPermissionTier 把档位写进会话级设置（"" = 清除该会话的选择）。
func (service *Service) persistPermissionTier(sessionID, tierValue string) error {
	return service.tierDeps().Persist(sessionID, tierValue)
}

// readStoredPermissionTier 读取该会话持久化的权限档位（"" = 从未选择 / 未装配 / 读失败）。
func (service *Service) readStoredPermissionTier(sessionID string) string {
	return service.tierDeps().Read(sessionID)
}

// applyStoredPermissionTier 把持久化档位落到该会话的内存槽 + 执行门 + 审批自动放行，
// 并把视图会话的生效档位重算进进程快照。
func (service *Service) applyStoredPermissionTier(sessionID, storedTier string) {
	service.tierDeps().ApplyStored(strings.TrimSpace(sessionID), storedTier)
}

// restorePermissionTierFor 读回并落地该会话的档位（非锁内衔接场合的便捷入口）。
func (service *Service) restorePermissionTierFor(sessionID string) {
	service.tierDeps().RestoreFor(sessionID)
}

// syncViewPermissionTierLocked 把 sessionID 的**生效档位**重算进进程快照，保证
// 「快照里的档位 == 该会话真正生效的档位」。调用方持有 Core.ViewMu，本方法不读盘。
//
// 不变量与理由（含 2026-09-17 切会话路径与"进入新会话"漏算那次的方向不对称风险）逐字保留在
// `backend/limit/tier.Deps.SyncViewLocked`（M5-R-limitc 接收者转移）；本方法只转发。
func (service *Service) syncViewPermissionTierLocked(sessionID string) {
	service.tierDeps().SyncViewLocked(sessionID)
}
