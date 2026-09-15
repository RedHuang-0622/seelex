// agentteam_service.go 把 AgentTeam 工厂/角色注册表接到 Application 能力面。
//
// 边界（AGENTS.md §1、arch 稿 §7）：application 只做**窄转发 + DTO 适配**，
// 不解释顺序/幂等语义；角色会话、注册表与 lifecycle 顺序分别由 sessionstore
// 的对应入口原子发布。headless/前端只消费 dto.TeamView / dto.TeamRegistry。
package core

import (
	"errors"

	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/agentteam"
)

// agentTeamPort 是会话端口可选实现的 A2A 角色管理面。生产实现为
// internal/adapters.SessionPort（DTO 形态，不把 sessionstore 类型漏给 application）。
type agentTeamPort interface {
	EnsureRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error)
	ReadLifecycleOrder(sessionID string) (string, []string, error)
	ReadTeamRegistry(mainSessionID string) (dto.TeamRegistry, error)
	WriteTeamRegistry(mainSessionID string, registry dto.TeamRegistry) error
}

// agentTeamFloorPort 是会话端口**可选**实现的 floor 读面（message head 当前发言
// 角色）。宿主没实现时成员表不填 floor_role——旧宿主能照常装配，不因缺一个读面
// 而整个 Agent Team 面板报错。
type agentTeamFloorPort interface {
	ReadFloorRole(mainSessionID string) (string, error)
}

// agentTeamAdapter 把会话端口（DTO 形态）适配为 agentteam.Port。
// 顺序读写复用既有 contract.RoleSessionPort.SetLifecycleOrder，避免第二套写入口。
type agentTeamAdapter struct {
	port agentTeamPort
	role contract.RoleSessionPort
}

func (adapter agentTeamAdapter) EnsureRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (bool, error) {
	return adapter.port.EnsureRoleSession(mainSessionID, roleName, roleSessionID, joinSeq)
}

func (adapter agentTeamAdapter) ReadLifecycleOrder(sessionID string) (string, []string, error) {
	return adapter.port.ReadLifecycleOrder(sessionID)
}

func (adapter agentTeamAdapter) SetLifecycleOrder(sessionID, policy string, roles []string) error {
	return adapter.role.SetLifecycleOrder(sessionID, policy, roles)
}

func (adapter agentTeamAdapter) ReadTeamRegistry(mainSessionID string) (dto.TeamRegistry, error) {
	return adapter.port.ReadTeamRegistry(mainSessionID)
}

func (adapter agentTeamAdapter) WriteTeamRegistry(mainSessionID string, registry dto.TeamRegistry) error {
	return adapter.port.WriteTeamRegistry(mainSessionID, registry)
}

// ReadFloorRole 实现 agentteam.FloorPort：宿主端口实现了 floor 读面才转读，
// 否则返回空串（成员表 floor 留空，不报错）。
func (adapter agentTeamAdapter) ReadFloorRole(mainSessionID string) (string, error) {
	floor, ok := adapter.port.(agentTeamFloorPort)
	if !ok {
		return "", nil
	}
	return floor.ReadFloorRole(mainSessionID)
}

func (service *Service) agentTeamPorts() (agentTeamPort, contract.RoleSessionPort, error) {
	if service == nil || service.Deps.Sessions == nil {
		return nil, nil, errors.New("agent team storage is not assembled")
	}
	role, ok := service.Deps.Sessions.(contract.RoleSessionPort)
	if !ok {
		return nil, nil, errors.New("session port does not expose role session storage")
	}
	port, ok := service.Deps.Sessions.(agentTeamPort)
	if !ok {
		return nil, nil, errors.New("session port does not expose agent team role registry")
	}
	return port, role, nil
}

func (service *Service) agentTeamFactory() (*agentteam.Factory, error) {
	port, role, err := service.agentTeamPorts()
	if err != nil {
		return nil, err
	}
	return agentteam.NewFactory(agentTeamAdapter{port: port, role: role})
}

func (service *Service) agentTeamRegistry() (*agentteam.Registry, error) {
	port, role, err := service.agentTeamPorts()
	if err != nil {
		return nil, err
	}
	return agentteam.NewRegistry(agentTeamAdapter{port: port, role: role})
}

// AgentTeamPresets 列出内置团队形态（前端角色管理页的可选模板）。
func (service *Service) AgentTeamPresets() []dto.TeamSpec {
	return agentteam.Presets()
}

// MaterializeAgentTeam 按 preset/自定义 TeamSpec 装配一支 AgentTeam。
// joinSeq 是本次装配把角色挂到主会话的可见起点。
func (service *Service) MaterializeAgentTeam(mainSessionID string, spec dto.TeamSpec, joinSeq uint64) (dto.TeamMaterializeResult, error) {
	factory, err := service.agentTeamFactory()
	if err != nil {
		return dto.TeamMaterializeResult{}, err
	}
	result, err := factory.Materialize(mainSessionID, spec, joinSeq)
	if err != nil {
		return dto.TeamMaterializeResult{}, err
	}
	// 装配即建环：新团队的"下一个谁发言"立即可观测（并带上逃生上限）。
	service.teamRuntimeFor(mainSessionID, result.View)
	if schedule := service.teamScheduleFor(mainSessionID); schedule != nil {
		result.View.Schedule = schedule
	}
	return result, nil
}

// MaterializeAgentTeamPreset 按内置 preset 名装配（goal-a2a / review-team / research-team）。
func (service *Service) MaterializeAgentTeamPreset(mainSessionID, teamKind string, joinSeq uint64) (dto.TeamMaterializeResult, error) {
	spec, err := agentteam.Preset(teamKind)
	if err != nil {
		return dto.TeamMaterializeResult{}, err
	}
	return service.MaterializeAgentTeam(mainSessionID, spec, joinSeq)
}

// AgentTeamView 返回成员表（身份/顺序/定时分区/配置状态/发言调度运行态）。
func (service *Service) AgentTeamView(mainSessionID string) (dto.TeamView, error) {
	view, err := service.agentTeamRawView(mainSessionID)
	if err != nil {
		return dto.TeamView{}, err
	}
	// 读路径也建环：前端「下一个谁发言」需要运行态；建环只读事实（链表顺序来自
	// lifecycle），不写盘、不新增第二份顺序。
	service.teamRuntimeFor(mainSessionID, view)
	if schedule := service.teamScheduleFor(mainSessionID); schedule != nil {
		view.Schedule = schedule
	}
	return view, nil
}

// agentTeamRawView 返回不带运行态的成员表（装配面内部用；避免
// teamRuntimeFor ← AgentTeamView 的互相递归）。
func (service *Service) agentTeamRawView(mainSessionID string) (dto.TeamView, error) {
	registry, err := service.agentTeamRegistry()
	if err != nil {
		return dto.TeamView{}, err
	}
	return registry.View(mainSessionID)
}

// AgentTeamPutRole 新增/覆盖一个角色配置。
func (service *Service) AgentTeamPutRole(mainSessionID string, role dto.RoleSpec) (dto.TeamRegistry, error) {
	registry, err := service.agentTeamRegistry()
	if err != nil {
		return dto.TeamRegistry{}, err
	}
	stored, err := registry.PutRole(mainSessionID, role)
	if err != nil {
		return dto.TeamRegistry{}, err
	}
	service.syncTeamRuntime(mainSessionID)
	return stored, nil
}

// AgentTeamDeleteRole 删除一个角色配置（并把它从工作顺序里摘除）。
func (service *Service) AgentTeamDeleteRole(mainSessionID, roleName string) (dto.TeamRegistry, error) {
	registry, err := service.agentTeamRegistry()
	if err != nil {
		return dto.TeamRegistry{}, err
	}
	stored, err := registry.DeleteRole(mainSessionID, roleName)
	if err != nil {
		return dto.TeamRegistry{}, err
	}
	service.syncTeamRuntime(mainSessionID)
	return stored, nil
}

// AgentTeamSetOrder 写工作顺序（前端拖拽/上下移只提交这个字段）。顺序是环的
// 唯一事实来源，写完必须同步环——否则"下一个谁发言"会停在旧次序上。
func (service *Service) AgentTeamSetOrder(mainSessionID, policy string, orderRoles []string) (dto.TeamView, error) {
	registry, err := service.agentTeamRegistry()
	if err != nil {
		return dto.TeamView{}, err
	}
	view, err := registry.SetOrder(mainSessionID, policy, orderRoles)
	if err != nil {
		return dto.TeamView{}, err
	}
	service.teamRuntimeFor(mainSessionID, view)
	if schedule := service.teamScheduleFor(mainSessionID); schedule != nil {
		view.Schedule = schedule
	}
	return view, nil
}

// AgentTeamInstantiateRole 一步实例化一个角色（"员工入职"）：规整配置 → 幂等建
// 角色会话 → 落注册表 → 按 join_policy 决定是否进工作顺序 → 报执行者绑定。
// 这是 Agent Team 管理面的"增加员工"入口；修改员工走同一条路径（按 role_name
// 覆盖），因此前端不需要"新增/编辑"两套调用。
func (service *Service) AgentTeamInstantiateRole(mainSessionID string, role dto.RoleSpec, joinSeq uint64) (dto.RoleInstantiation, error) {
	factory, err := service.agentTeamFactory()
	if err != nil {
		return dto.RoleInstantiation{}, err
	}
	result, err := factory.InstantiateRole(mainSessionID, role, joinSeq)
	if err != nil {
		return dto.RoleInstantiation{}, err
	}
	service.syncTeamRuntime(mainSessionID)
	return result, nil
}
