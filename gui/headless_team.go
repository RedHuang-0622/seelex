// headless_team.go：AgentTeam 角色管理 RPC（`team.*`）。
//
// 边界：headless 只做参数解码与透传，装配/排序/幂等语义全在 application +
// sessionstore；返回一律是 application/contract/dto 的纯 DTO，不暴露存储类型。
// 方法清单同步登记在 gui/README.md。
package gui

import (
	"encoding/json"
	"fmt"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// teamRPCApplication 是 Application 的 AgentTeam 管理扩展面。
type teamRPCApplication interface {
	AgentTeamPresets() []dto.TeamSpec
	MaterializeAgentTeam(mainSessionID string, spec dto.TeamSpec, joinSeq uint64) (dto.TeamMaterializeResult, error)
	MaterializeAgentTeamPreset(mainSessionID, teamKind string, joinSeq uint64) (dto.TeamMaterializeResult, error)
	AgentTeamView(mainSessionID string) (dto.TeamView, error)
	AgentTeamPutRole(mainSessionID string, role dto.RoleSpec) (dto.TeamRegistry, error)
	AgentTeamDeleteRole(mainSessionID, roleName string) (dto.TeamRegistry, error)
	AgentTeamSetOrder(mainSessionID, policy string, orderRoles []string) (dto.TeamView, error)
}

type teamMaterializeRequest struct {
	MainSessionID string        `json:"main_session_id"`
	TeamKind      string        `json:"team_kind,omitempty"`
	Spec          *dto.TeamSpec `json:"spec,omitempty"`
	JoinSeqID     uint64        `json:"join_seq_id,omitempty"`
}

type teamViewRequest struct {
	MainSessionID string `json:"main_session_id"`
}

type teamRoleRequest struct {
	MainSessionID string       `json:"main_session_id"`
	Role          dto.RoleSpec `json:"role"`
	RoleName      string       `json:"role_name,omitempty"`
}

type teamOrderRequest struct {
	MainSessionID string   `json:"main_session_id"`
	OrderPolicy   string   `json:"order_policy,omitempty"`
	OrderRoles    []string `json:"order_roles,omitempty"`
}

// dispatchTeam 处理 `team.*`：装配、成员表、角色配置 CRUD、工作顺序设置。
func (server *headlessServer) dispatchTeam(method string, args []json.RawMessage) (any, error) {
	app, ok := server.app.(teamRPCApplication)
	if !ok {
		return nil, fmt.Errorf("%s: 当前 Application 未装配 AgentTeam 管理扩展面", method)
	}
	switch method {
	case "team.presets":
		return app.AgentTeamPresets(), nil
	case "team.materialize":
		var request teamMaterializeRequest
		if err := decodeHeadlessObject(method, args, &request); err != nil {
			return nil, err
		}
		if request.Spec != nil {
			return app.MaterializeAgentTeam(request.MainSessionID, *request.Spec, request.JoinSeqID)
		}
		return app.MaterializeAgentTeamPreset(request.MainSessionID, request.TeamKind, request.JoinSeqID)
	case "team.view":
		var request teamViewRequest
		if err := decodeHeadlessObject(method, args, &request); err != nil {
			return nil, err
		}
		return app.AgentTeamView(request.MainSessionID)
	case "team.put_role":
		var request teamRoleRequest
		if err := decodeHeadlessObject(method, args, &request); err != nil {
			return nil, err
		}
		return app.AgentTeamPutRole(request.MainSessionID, request.Role)
	case "team.delete_role":
		var request teamRoleRequest
		if err := decodeHeadlessObject(method, args, &request); err != nil {
			return nil, err
		}
		roleName := request.RoleName
		if roleName == "" {
			roleName = request.Role.RoleName
		}
		return app.AgentTeamDeleteRole(request.MainSessionID, roleName)
	case "team.set_order":
		var request teamOrderRequest
		if err := decodeHeadlessObject(method, args, &request); err != nil {
			return nil, err
		}
		return app.AgentTeamSetOrder(request.MainSessionID, request.OrderPolicy, request.OrderRoles)
	default:
		return nil, fmt.Errorf("未知 team headless 方法: %s", method)
	}
}
