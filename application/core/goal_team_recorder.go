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

// RecordMainTurn 在 b 交还发言权时发布 EXEC 主持标记：main 的过程行照旧实时
// 落盘（B 方案不藏过程），这里只补一次"本轮由 EXEC 主持 + 进度"的发布，
// 让前端能把这一轮的过程归到 EXEC 名下。main 复用主会话（role_session_id =
// 主会话号），标记行经同一 draft → sequencer 通道发布并更新 floor=main。
func (r goalTLRecorder) RecordMainTurn(_ context.Context, record goaldomain.MainTurnRecord) error {
	if r.service == nil || r.sessionID == "" {
		return nil
	}
	view, err := r.service.AgentTeamView(r.sessionID)
	if err != nil || !view.Configured {
		return nil
	}
	const roleName = "main"
	content := "本轮由 EXEC 主持（TL 交还发言权"
	if record.Directive != "" {
		content += "，" + string(record.Directive)
	}
	content += "）"
	row := dto.RoleDraftRow{
		RoleName: roleName, RoleSessionID: r.sessionID, UnitSeq: 1, RoundID: record.RoundID,
		Event: dto.RoleRow{
			Kind: "round_host", Role: "assistant", Content: content,
			RoleName: roleName, RoleSessionID: r.sessionID, UnitSeq: 1, RoundID: record.RoundID,
		},
	}
	if err := r.service.AppendRoleDraft(r.sessionID, roleName, r.sessionID, []dto.RoleDraftRow{row}); err != nil {
		return err
	}
	_, err = r.service.SyncRoleDraft(r.sessionID, roleName, r.sessionID, view.OrderRoles)
	return err
}
