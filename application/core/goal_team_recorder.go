package core

import (
	"context"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/agentteam"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// goal_team_recorder.go — TL 每回合原文的记录器（my_design §8.3 统一写入流程）。
//
// b（ADVISOR/TL）每回合产出两块原文：它实际看到的审查上下文与它自己的回答。
// 两者都先落 tl 角色 draft，再由 sequencer 同步进主文档（role_name=tl、发布
// floor=tl），这样 ADVISOR 会话视图与主对话轮次都能按"主持该轮次的 agent"渲染。
// 记录失败不阻断治理（调用方忽略错误）。

type goalTLRecorder struct {
	service   *Service
	sessionID string
}

// goalTLRecorderFor 是装配根注入的按会话记录器工厂。
func (service *Service) goalTLRecorderFor(sessionID string) goaldomain.TLRoundRecorder {
	if service == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	return goalTLRecorder{service: service, sessionID: strings.TrimSpace(sessionID)}
}

func (r goalTLRecorder) RecordTLRound(_ context.Context, record goaldomain.TLRoundRecord) error {
	if r.service == nil || r.sessionID == "" {
		return nil
	}
	view, err := r.service.AgentTeamView(r.sessionID)
	if err != nil || !view.Configured {
		return nil // 未装配 team 的会话不记角色行
	}
	const roleName = "tl"
	roleSessionID := agentteam.RoleSessionID(view.TeamID, roleName)
	if strings.TrimSpace(roleSessionID) == "" {
		return nil
	}
	row := func(unit uint64, kind, role, content string) dto.RoleDraftRow {
		return dto.RoleDraftRow{
			RoleName: roleName, RoleSessionID: roleSessionID, UnitSeq: unit,
			Event: dto.RoleRow{
				Kind: kind, Role: role, Content: content,
				RoleName: roleName, RoleSessionID: roleSessionID, UnitSeq: unit,
			},
		}
	}
	rows := []dto.RoleDraftRow{
		row(1, "role_context", "system", record.Context),   // b 本轮看到的原文
		row(2, "tl_directive", "assistant", record.Output), // b 本轮回答的原文
	}
	if err := r.service.AppendRoleDraft(r.sessionID, roleName, roleSessionID, rows); err != nil {
		return err
	}
	_, err = r.service.SyncRoleDraft(r.sessionID, roleName, roleSessionID, view.OrderRoles)
	return err
}
