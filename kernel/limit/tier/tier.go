// Package tier 是「主会话权限档位」的落点：会话级持久化、生效档位解析，以及
// 执行门 / 审批面 / 视图快照的同步口径。
//
// 落点沿革：判据与编排本体于 2026-10-16 随 M5-R-limitc 从
// `application/core/{session_permission_tier.go,session_scope.go,service_interaction.go}`
// **接收者转移**到此处（plan §2.8）；门面只留宿主适配与一行转发。
//
// 依赖全是契约与窄端口（结构满足即可）：本包不 import `application/*`、不 import `session`，
// 也不持有任何状态——会话槽、执行门、审批面、快照都由宿主经端口代劳。
package tier

import (
	"strings"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

// SettingStore 是会话级用户设置端口（结构满足 `session.SessionSettingPort`）。
// nil = 未装配：档位退回内存态，与落盘前语义一致（最小宿主与测试桩不因此报错）。
type SettingStore interface {
	SessionPermissionTier(sessionID string) (string, error)
	SetSessionPermissionTier(sessionID, tier string) error
}

// Units 是会话槽的读写口（Unit 自带锁）。
type Units interface {
	UnitPermissionTier(sessionID string) (string, bool)
	SetUnitPermissionTier(sessionID, tier string)
}

// View 是视图侧读写口。
//
// `SnapshotSessionID` / `WriteTier` 由**已经持有视图锁**的调用方使用（实现方不得再取锁）；
// `ViewSessionID` 由实现方自行持锁，只该在锁外调用。
type View interface {
	ViewSessionID() string
	SnapshotSessionID() string
	WriteTier(sessionID, tier string)
}

// Gate 把目标会话的生效档位写进执行门（按会话解析）。nil = 不装配。
type Gate func(sessionID, tier string)

// AutoApproval 是审批面的**窄可选**端口：只需要"按会话开关自动放行"这一件事
// （结构满足 `contract.ApprovalBroker` / `backend/limit/approval.ApprovalBroker`，
// 所以宿主无需适配层）。nil = 无审批面。
type AutoApproval interface {
	SetPermissionAutoApprovalFor(sessionID string, on bool)
}

// Deps 是档位用例需要的宿主能力面；全部零依赖可空（空 = 降级而不是报错）。
type Deps struct {
	Settings SettingStore
	Units    Units
	Approval AutoApproval
	View     View
	Gate     Gate
	// DefaultTier 返回进程默认档位（装配期捕获；空 = manual）。
	DefaultTier func() string
}

// EffectiveTier 返回指定会话生效的权限档位（G4：会话槽优先；未选择回退进程默认）。
//
// 取值口径与执行门一致（SyncGateFor 写进门的正是本函数的结果），因此快照重算出来的值
// 就是 chat 起点会写进门的那个值。
func (d Deps) EffectiveTier(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID != "" && d.Units != nil {
		if tier, ok := d.Units.UnitPermissionTier(sessionID); ok && tier != "" {
			return tier
		}
	}
	if d.DefaultTier != nil {
		if def := d.DefaultTier(); def != "" {
			return def
		}
	}
	return dto.PermissionTierManual
}

// FullAccessFor 是档位的**兼容派生读面**：full 档 ⇔ 旧的全权开启。
func (d Deps) FullAccessFor(sessionID string) bool {
	return dto.PermissionTierIsFullAccess(d.EffectiveTier(sessionID))
}

// Persist 把档位写进会话级设置（"" = 清除该会话的选择）。
// 未装配设置端口时返回 nil：内存态档位是受支持的降级形态，不是错误。
func (d Deps) Persist(sessionID, tier string) error {
	if d.Settings == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	return d.Settings.SetSessionPermissionTier(sessionID, tier)
}

// Read 读取该会话持久化的权限档位（"" = 从未选择 / 未装配设置端口 / 读失败）。
//
// 含磁盘读，调用方不得持有宿主的视图锁。读失败按"未选择"处理：档位是增强项，
// 读不到只该退化成进程默认，不该阻断会话切换与恢复。
func (d Deps) Read(sessionID string) string {
	if d.Settings == nil {
		return ""
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	tier, err := d.Settings.SessionPermissionTier(sessionID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(tier)
}

// ApplyStored 把持久化档位落到该会话的内存槽 + 执行门 + 审批自动放行，并把**视图会话**的
// 生效档位重算进进程快照。
//
// 只写内存态（会话槽/门/审批表/快照字段各自持锁），不阻塞 I/O，因此调用方可以在视图锁
// 临界区内的会话切换之后调用它，让紧随其后的运行时投影直接读到恢复后的档位。
//
// tier 为空或不可识别时**不写会话槽**（未选择 = 回退进程默认；存储里的脏值不得变成一次
// 静默降级或静默放行），但**视图快照仍要重算**：切换会话后快照里若留着上一个会话的档位，
// 用户看到的就是错的——尤其上一个会话是 full 时，"以为免审其实要问人"只是别扭，
// "以为要问人其实免审"是安全问题。
func (d Deps) ApplyStored(sessionID, tier string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	trimmed := strings.TrimSpace(tier)
	if normalized, err := dto.NormalizePermissionTier(trimmed); err == nil && trimmed != "" {
		d.ApplyTier(sessionID, normalized)
	}
	if d.View == nil || d.View.SnapshotSessionID() != sessionID {
		// 非视图会话（后台冷加载等）：只落地它自己的槽，不改视图快照。
		return
	}
	d.SyncViewLocked(sessionID)
}

// ApplyTier 把**已校验**的档位落到会话槽 + 执行门 + 审批面（不改视图快照；快照由调用方按
// 自己的发布口径写）。session 切换恢复（ApplyStored）与用户显式切档（门面 SetPermissionTier）
// 共用这一处落地动作。
func (d Deps) ApplyTier(sessionID, normalized string) {
	if d.Units != nil {
		d.Units.SetUnitPermissionTier(sessionID, normalized)
	}
	// 执行门按会话解析：只写目标会话那一格。
	d.SyncGateFor(sessionID)
	// 审批面同步：full 档的会话切回来不该再问人（审批自动放行是会话级决定）。
	if d.Approval != nil {
		d.Approval.SetPermissionAutoApprovalFor(sessionID, dto.PermissionTierIsFullAccess(normalized))
	}
}

// RestoreFor 读回并落地该会话的档位（非锁内衔接场合的便捷入口）。
func (d Deps) RestoreFor(sessionID string) {
	d.ApplyStored(sessionID, d.Read(sessionID))
}

// SyncViewLocked 把 sessionID 的**生效档位**重算进进程快照，保证「快照里的档位 == 该会话
// 真正生效的档位」。调用方持有视图锁，本方法不读盘。
//
// 它存在的理由是一条**不变量**，而不是某一条切换路径的补丁：快照里的
// Runtime.PermissionTier/FullAccess 是 chip 与运行状态面板的唯一读面，任何一次
// 「视图指针换到另一个会话」都必须在同一次临界区里把这个字段重算成本会话的值。
// 漏掉一次，用户看到的就是**上一个会话的档位**——2026-09-17 的切会话路径
// （TestPermissionTierSwitchMirrorsViewSnapshot）修过一次，但**进入新会话**这条路径当时漏了：
// BeginNewSession 只清了 Plan/会话/聊天态，档位字段原样留着，于是「新建会话后 chip 显示
// 上一次的档位，实际生效的是新会话的默认档位」。方向不对称：上一个是 full 而新会话是 manual
// 只是别扭；反过来"看着手动其实全权放着"是安全问题。
func (d Deps) SyncViewLocked(sessionID string) {
	if d.View == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || d.View.SnapshotSessionID() != sessionID {
		return
	}
	d.View.WriteTier(sessionID, d.EffectiveTier(sessionID))
}

// SyncGateFor 按会话档位同步执行门（G4：chat 起点调用，保证每个会话都按自己的选择运行——
// 后台/新会话不继承其它会话的遗留档位）。
//
// 门是**按会话解析**的：本调用只写目标会话那一格，不会覆盖别的会话已生效的档位——历史缺陷
// 正是「进程级单布尔 + 起点同步」，B 会话起跑会把 A 会话的全权关掉，用户看到「点了全权仍弹审批
// 仍被拒」。
func (d Deps) SyncGateFor(sessionID string) {
	if d.Gate == nil {
		return
	}
	d.Gate(sessionID, d.EffectiveTier(sessionID))
}
