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
	return factory.Materialize(mainSessionID, spec, joinSeq)
}

// MaterializeAgentTeamPreset 按内置 preset 名装配（goal-a2a / review-team / research-team）。
func (service *Service) MaterializeAgentTeamPreset(mainSessionID, teamKind string, joinSeq uint64) (dto.TeamMaterializeResult, error) {
	spec, err := agentteam.Preset(teamKind)
	if err != nil {
		return dto.TeamMaterializeResult{}, err
	}
	return service.MaterializeAgentTeam(mainSessionID, spec, joinSeq)
}

// AgentTeamView 返回成员表（身份/顺序/定时分区/配置状态）。
func (service *Service) AgentTeamView(mainSessionID string) (dto.TeamView, error) {
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
	return registry.PutRole(mainSessionID, role)
}

// AgentTeamDeleteRole 删除一个角色配置（并把它从工作顺序里摘除）。
func (service *Service) AgentTeamDeleteRole(mainSessionID, roleName string) (dto.TeamRegistry, error) {
	registry, err := service.agentTeamRegistry()
	if err != nil {
		return dto.TeamRegistry{}, err
	}
	return registry.DeleteRole(mainSessionID, roleName)
}

// AgentTeamSetOrder 写工作顺序（前端拖拽/上下移只提交这个字段）。
func (service *Service) AgentTeamSetOrder(mainSessionID, policy string, orderRoles []string) (dto.TeamView, error) {
	registry, err := service.agentTeamRegistry()
	if err != nil {
		return dto.TeamView{}, err
	}
	return registry.SetOrder(mainSessionID, policy, orderRoles)
}
