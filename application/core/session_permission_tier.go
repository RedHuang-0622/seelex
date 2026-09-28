package core

// 主会话权限档位的**会话级持久化**（需求变更 2026-09-17：档位从"只有内存槽"
// 改为"这一 session 的权限设置"，跨重启必须记住）。
//
// # 为什么落点是"会话级用户设置"而不是 SessionRecord
//
// 这是实测结论，不是设计偏好：现行存储布局（v8/S20）下 record 通道已退役——
// SaveRecordRaw 只把 payload 里的 status/title 写穿到目录面，LoadRecordRaw 交回
// 的是 derivedRecordPayload 派生的 (version/id/status/updated_at/conversation)。
// 往 SessionRecord 加字段既写不进去也读不回来。会话级用户设置落在项目级会话
// 元数据 blob（与置顶/别名/排序位同一份存储），它的存在理由恰好就是我们要的：
// 不随回合结束的整体重建被覆盖、跨重启保留。

import (
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/session"
)

// settingPort 返回会话端口的可选"会话级用户设置"扩展（未装配 = 档位退回内存态，
// 与落盘前语义一致：最小宿主与测试桩不因此报错）。
func (service *Service) settingPort() (session.SessionSettingPort, bool) {
	if service == nil || service.Deps.Sessions == nil {
		return nil, false
	}
	port, ok := service.Deps.Sessions.(session.SessionSettingPort)
	return port, ok
}

// persistPermissionTier 把档位写进会话级设置（"" = 清除该会话的选择）。
// 未装配设置端口时返回 nil：内存态档位是受支持的降级形态，不是错误。
func (service *Service) persistPermissionTier(sessionID, tier string) error {
	port, ok := service.settingPort()
	if !ok {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	return port.SetSessionPermissionTier(sessionID, tier)
}

// readStoredPermissionTier 读取该会话持久化的权限档位（"" = 从未选择 / 未装配
// 设置端口 / 读失败）。
//
// 含磁盘读，调用方不得持有 Core.ViewMu。读失败按"未选择"处理：档位是增强项，
// 读不到只该退化成进程默认，不该阻断会话切换与恢复。
func (service *Service) readStoredPermissionTier(sessionID string) string {
	port, ok := service.settingPort()
	if !ok {
		return ""
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	tier, err := port.SessionPermissionTier(sessionID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(tier)
}

// applyStoredPermissionTier 把持久化档位落到该会话的内存槽 + 执行门 + 审批自动
// 放行，并把**视图会话**的生效档位重算进进程快照。
//
// 只写内存态（会话槽/门表/审批表/快照字段各自持锁），不阻塞 I/O，因此调用方可以
// 在 Core.ViewMu 临界区内的 `sessionUnitLocked` 之后调用它，让紧随其后的运行时
// 投影直接读到恢复后的档位。
//
// tier 为空或不可识别时**不写会话槽**（未选择 = 回退进程默认；存储里的脏值不得变成
// 一次静默降级或静默放行），但**视图快照仍要重算**（见 syncViewPermissionTierLocked）：
// 切换会话后快照里若留着上一个会话的档位，用户看到的就是错的——尤其上一个会话是 full
// 时，"以为免审其实要问人"只是别扭，"以为要问人其实免审"是安全问题。
func (service *Service) applyStoredPermissionTier(sessionID, tier string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	if normalized, err := dto.NormalizePermissionTier(strings.TrimSpace(tier)); err == nil && strings.TrimSpace(tier) != "" {
		if unit := service.sessions.Unit(sessionID); unit != nil {
			unit.SetPermissionTier(normalized)
		}
		// 执行门按会话解析：只写目标会话那一格（与 syncFullAccessFor 同一口径）。
		service.syncFullAccessFor(sessionID)
		// 审批面同步：full 档的会话切回来不该再问人（审批自动放行是会话级决定）。
		if service.Approval != nil {
			service.Approval.SetPermissionAutoApprovalFor(sessionID, dto.PermissionTierIsFullAccess(normalized))
		}
	}
	if service.Core.Snapshot.Session.ID != sessionID {
		// 非视图会话（后台冷加载等）：只落地它自己的槽，不改视图快照。
		return
	}
	service.syncViewPermissionTierLocked(sessionID)
}

// syncViewPermissionTierLocked 把 sessionID 的**生效档位**重算进进程快照，保证
// 「快照里的档位 == 该会话真正生效的档位」。调用方持有 Core.ViewMu，本方法不读盘。
//
// 它存在的理由是一条**不变量**，而不是某一条切换路径的补丁：快照里的
// Runtime.PermissionTier/FullAccess 是 chip 与运行状态面板的唯一读面，任何一次
// 「视图指针换到另一个会话」都必须在同一次临界区里把这个字段重算成本会话的值。
// 漏掉一次，用户看到的就是**上一个会话的档位**——2026-09-17 的切会话路径
// （TestPermissionTierSwitchMirrorsViewSnapshot）修过一次，但**进入新会话**这条
// 路径当时漏了：BeginNewSession 只清了 Plan/会话/聊天态，档位字段原样留着，于是
// 「新建会话后 chip 显示上一次的档位，实际生效的是新会话的默认档位」。方向不对称：
// 上一个是 full 而新会话是 manual 只是别扭；反过来"看着手动其实全权放着"是安全问题。
//
// 取值口径与执行门一致（permissionTierForSession：会话槽优先，未选择回退进程默认），
// 因此重算出来的值就是 chat 起点 syncFullAccessFor 会写进门的那个值。
func (service *Service) syncViewPermissionTierLocked(sessionID string) {
	if service == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || service.Core.Snapshot.Session.ID != sessionID {
		return
	}
	effective := service.permissionTierForSession(sessionID)
	service.Core.Snapshot.Runtime.PermissionTier = effective
	service.Core.Snapshot.Runtime.FullAccess = dto.PermissionTierIsFullAccess(effective)
}

// restorePermissionTierFor 读回并落地该会话的档位（非锁内衔接场合的便捷入口）。
func (service *Service) restorePermissionTierFor(sessionID string) {
	service.applyStoredPermissionTier(sessionID, service.readStoredPermissionTier(sessionID))
}
