// team_registry.go：A2A 角色注册表（agent-team 工厂与角色管理设置面的持久化）在根包的
// **兼容出口**（M8 起）。
//
// **状态（A06，2026-10-15）：legacy 只读兼容面，没有生产调用方。**
//
// 写它的用户面（装配 / 员工入职 / 团队库 / 员工库 / 全局母本 / 角色管理面板）已于
// 2026-10-12 J-1a 整条退场。2026-10-15 A06 复核结果：本文件对外的
// `ReadTeamRegistryWorkspace` / `WriteTeamRegistryWorkspace` /
// `RemoveTeamRegistryWorkspace` / `EnsureRoleSessionWorkspace` /
// `ReadMessageFloorWorkspace` / `ReadLifecycleOrderWorkspace` 在全仓 `.go` 里
// **没有任何生产调用方**（只剩自身定义与本包用例命中；`SetLifecycleOrderWorkspace`
// 另由 `wiring/adapters` 的端口转发，见 A05）。
//
// 因此本文件现在是**旧数据兼容与诊断面**，按 A06 的兼容政策处理：
//   - **读能力保留**：旧会话里的 `session/team/roles.json` 仍可读出、诊断与迁移
//     （删掉 reader 会让旧数据失去唯一入口，那不是清理）；
//   - **不删文件、不迁移、不停止写入**：任何磁盘清理/迁移必须另行中文预警 + 备份 +
//     用户确认（根 `MEMORY.md` 与 `AGENTS.md` §0）；本条目不做任何磁盘动作；
//   - 将来若确认产品不再读，也只先停止写入；移除必须按**版本化 API 变更**做，
//     并带旧 `roles.json` fixture 的冷读用例；
//   - 完整政策见 [`store/README.md`](README.md) 的「旧 Agent Team 数据（legacy）」。
//
// 原定位（设计史，保留；文件格式与读写语义未改）：
//   - 文件 = `session/team/roles.json`，一次提交整份原子替换；
//     替换成功即发布（不需要 head 水位），不写 message、不碰 sequencer；
//   - 内容 = TeamSpec 里的角色注册表部分（RoleSpec 清单 + team_kind + 顺序策略），
//     `order_policy`/`order_roles` 仍以 lifecycle head 为运行时权威，本文件只做
//     角色配置的持久事实；两者由应用层在同一次装配里一起写，避免出现第二份顺序事实；
//   - 角色身份是 metadata：`role_name` 只用于展示/归属/装配，provider role 仍只有
//     system/user/assistant/tool。
//
// M8 的所有权裁决：注册表的**落盘实现与规范化**已迁入 `store/role`
// （`store/role/registry.go`，领域归属证据见该文件头）。本文件只保留同身份 alias 与
// 单层转发——根包不再持有 `roles.json` 的编解码、写锁、建目录或 `sort` 规范化。
// 下面三个非注册表函数（角色会话创建 / floor 读 / 顺序策略读）仍属 M9 迁移范围，
// 本轮按兵不动。
package store

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	storelayout "github.com/RedHuang-0622/seelex/store/internal/layout"
	storerole "github.com/RedHuang-0622/seelex/store/role"
)

// TeamRegistrySchemaVersion 是 session/team/roles.json 的 schema 版本。
//
// legacy（A06）：版本不认识时写入显式报错（不静默降级），读侧照旧；旧文件必须可读。
const TeamRegistrySchemaVersion = storerole.SchemaVersion

// TeamRoleSpec 是角色注册表里的一个角色（对应 RoleSpec，见 arch 稿 §2.1）。
// 空字段一律表示"未配置/继承 preset 默认"，不表示禁用。
type TeamRoleSpec = storerole.RoleSpec

// TeamRegistry 是 `session/team/roles.json` 的完整内容（整份替换型）。
// Configured 由读侧推导，不落盘：文件不存在 = 未配置过角色注册表。
type TeamRegistry = storerole.Registry

// ReadTeamRegistryWorkspace 读主会话的角色注册表（项目作用域显式传入，禁止回退
// 活跃写作用域）。
//
// legacy（A06）：保留的**读能力**（旧数据可诊断/可迁移）；当前没有生产调用方。
func (router *Router) ReadTeamRegistryWorkspace(projectID, sessionID string) (TeamRegistry, error) {
	var registry TeamRegistry
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: team registry requires session layout")
		}
		var err error
		registry, err = layout.layout.roles.Read(registryScope(projectID, sessionID))
		return err
	})
	return registry, err
}

// WriteTeamRegistryWorkspace 整份写入主会话的角色注册表。
//
// legacy（A06）：用户面写入方（装配）已退场，当前没有生产调用方。**不停止写入、
// 不迁移**：留着它是因为"旧数据还能被写回/修复"是兼容面的一部分（见文件头政策）。
func (router *Router) WriteTeamRegistryWorkspace(projectID, sessionID string, registry TeamRegistry) error {
	return router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: team registry requires session layout")
		}
		return layout.layout.roles.Write(registryScope(projectID, sessionID), registry)
	})
}

// RemoveTeamRegistryWorkspace 删除主会话的角色注册表（团队离场；幂等）。
//
// legacy（A06）：同 WriteTeamRegistryWorkspace；**不删用户文件**，本函数只提供幂等删除原语。
func (router *Router) RemoveTeamRegistryWorkspace(projectID, sessionID string) error {
	return router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: team registry requires session layout")
		}
		return layout.layout.roles.Remove(registryScope(projectID, sessionID))
	})
}

// registryScope 构造注册表的会话作用域（与 `layoutScope` 同一口径：sessionID 去空白）。
func registryScope(projectID, sessionID string) storelayout.Scope {
	return layoutScope(Key{ProjectID: projectID, SessionID: strings.TrimSpace(sessionID)})
}

// EnsureRoleSessionWorkspace 幂等创建角色会话：已存在返回 created=false（不报错），
// 供 AgentTeamFactory 重复装配同一条 TeamSpec 时不产生第二个会话。
//
// legacy（A06）：调用方（`AgentTeamFactory`）已随 J-1a 删除，当前没有生产调用方。
// 角色会话本身的创建仍有活动路径（worker 作业面），但不再经由这里。
func (router *Router) EnsureRoleSessionWorkspace(projectID, mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error) {
	created := false
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: role sessions require session layout")
		}
		mainKey := Key{ProjectID: projectID, SessionID: strings.TrimSpace(mainSessionID)}
		roleStore, roleKey := roleSessionEngine(layout.layout, mainKey, roleName, roleSessionID)
		if roleStore.sessionExists(roleKey) {
			return nil
		}
		if _, _, err := createRoleSession(layout.layout, mainKey, roleName, roleSessionID, joinSeq); err != nil {
			// 并发装配时另一方可能已经建好：再查一次，存在即视为成功。
			if roleStore.sessionExists(roleKey) {
				return nil
			}
			return err
		}
		created = true
		return nil
	})
	return created, err
}

// ReadMessageFloorWorkspace 读主会话 message head 的 floor：当前轮到这里发言的
// 角色（唯一写者 = sequencer，随 message head 原子发布）。Agent Team 面板的
// floor 高亮由此而来——数据一直在 message head 里，缺的只是出口。
//
// 状态（A05/A06）：**floor 字段本身是活动的历史/归属事实**（sequencer 仍在写、
// 历史渲染与审计仍要读），只有"Agent Team 面板的高亮出口"随 J-1a 退场；这个读函数
// 当前没有生产调用方，保留为只读兼容面。
//
// 空值语义：会话还没发生过一次角色 draft sync（或工程解析为空、目录不存在）时
// 返回 nil，不报错（"还没有人发言"是正常状态，不是故障）。
func (router *Router) ReadMessageFloorWorkspace(projectID, sessionID string) (*Floor, error) {
	var floor *Floor
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: message floor requires session layout")
		}
		head, err := layout.layout.readMessageHead(Key{ProjectID: projectID, SessionID: strings.TrimSpace(sessionID)})
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		floor = cloneFloor(head.Floor)
		return nil
	})
	return floor, err
}

// ReadLifecycleOrderWorkspace 读群聊顺序策略（lifecycle head 是运行时权威）。
// 空值 = 尚未编排过顺序，不是错误。
//
// legacy（A05/A06）：`order_policy`/`order_roles` 是历史字段，已不驱动任何行为
// （seatPlan 与发言环都已删除），本函数当前没有生产调用方；保留是为了旧会话可读。
func (router *Router) ReadLifecycleOrderWorkspace(projectID, sessionID string) (string, []string, error) {
	var policy string
	var roles []string
	err := router.withRepositoryAt(projectID, func(repository Repository, projectID string) error {
		layout, ok := repository.(*jsonRepository)
		if !ok {
			return fmt.Errorf("session storage: lifecycle order requires session layout")
		}
		head, err := layout.layout.readLifecycleHead(Key{ProjectID: projectID, SessionID: strings.TrimSpace(sessionID)})
		if errors.Is(err, fs.ErrNotExist) {
			// 会话还没有 lifecycle head（没编排过顺序），或工程解析为空、目录
			// 根本不存在：都表示"暂无顺序"，不是错误。把它当错误抛出去会让
			// Agent Team 面板在未装配/未编排的会话上直接报
			// `open ...metadata/lifecycle.json: cannot find the path`。
			return nil
		}
		if err != nil {
			return err
		}
		policy = head.OrderPolicy
		roles = append([]string(nil), head.OrderRoles...)
		return nil
	})
	return policy, roles, err
}
