package core

import (
	"context"
	"fmt"
	goaldomain "github.com/RedHuang-0622/seelex/backend/multiagent/goal"
	"github.com/RedHuang-0622/seelex/backend/multiagent/team"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"strings"
)

// goal_team_recorder.go — TL 每回合原文的记录器（my_design §8.3 统一写入流程）。
//
// b（ADVISOR/TL）每回合产出两块原文：它实际看到的审查上下文与它自己的回答。
// 两者都先落 tl 角色 draft，再由 sequencer 同步进主文档（role_name=tl、发布
// floor=tl），这样 ADVISOR 会话视图与主对话轮次都能按"主持该轮次的 agent"渲染。
// 记录失败不阻断治理（调用方忽略错误）。
//
// 状态（A13，2026-10-15 已裁决）：本文件是**恒空的写路径**——具备删除条件，但本轮不删。
//
// **唯一目的（已核实）**：把 ADVISOR/TL 每回合的"它看到的原文 + 它的回答"落成 tl 角色
// draft，再经 sequencer 同步进主文档（`kind=tl_directive` 行、floor=tl）。它不是 goal
// 决策或审计的载体。
//
// **现行可达性**：三条来路（`RecordTLRound` / `RecordMainTurn` / `ArchiveTLHistory`）都在
// `sessionAgentTeamViewJ1A` 的 `!view.Configured` 守卫处 no-op——退场前"未装配团队"那一支
// 本来就是这个行为，所以**逐字同解**（守卫用例见
// `goal_team_recorder_test.go:TestRoleDraftRecordingIsUnreachableAfterJ1A`）。
//
// **用户可见的 TL 证据不依赖本文件**（均为独立路径，勿随本文件一起判死）：
//   - `GoalGovernanceView`（活动栈 / status / `RoundSteps` 评审过程）→ GUI 治理面板；
//   - goal 的 `tl_directives`（`store/session_context.go` + headless `goal_tl_directives`）；
//   - GoalStack / GoalAudit / GoalBoard archive（收口与逃生留痕）。
//
// **删除清单（未来做时一次做完，别只删一半）**：
//   - 本文件与 `goalTLRecorderFor`、`goalCoordinatorDeps.TLRecorderFor`
//     （调用点 `goal_coordinator.go` 的 `sup.SetRoundRecorder`）、`service_assembler.go` 的注入；
//   - goal 域端口 `TLRoundRecorder` / `TLHistoryArchiver` 与 `Supervisor.SetRoundRecorder` /
//     `Supervisor.recorder` 的调用（`goal/techleader.go`、`goal/escape.go` 的归档分支）；
//   - **保留**：`AppendRoleDraft` / `SyncRoleDraft` / `RoleSnapshot`（B1 保留面，headless
//     `role.*` 仍在用）、`wiring/adapters` 的 role 字段编解码与旧 role draft 读取面
//     （历史消息/草稿要还能读回）。

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

// sessionAgentTeamViewJ1A 是「本会话已装配团队」这一读面在 J-1a 之后的**保留形态**
// （2026-10-12）：会话团队装配面（`AgentTeamView` / `agentTeamRawView` / 团队注册表
// 的应用面孔）已随本条目整份退场，因此这里恒报「未装配」。
//
// 保留而不删 goal 逻辑的理由（本条目硬约束 B4）：退场前的实际行为就是"未装配 →
// 不记角色行"（见 RecordTLRound 的 `!view.Configured` 守卫），所以恒返回零值
// TeamView 与退场前的可达分支**逐字同解**；用一条命名清楚的函数承载它，比把守卫
// 从三处删掉更能说明"这里少的是读面，不是判据"。本会话的团队事实现在归 teamwork
// （团队看板 / backend/multiagent/teamwork），那条面未动。
func (service *Service) sessionAgentTeamViewJ1A(mainSessionID string) (dto.TeamView, error) {
	_ = mainSessionID
	return dto.TeamView{}, nil
}

func (r goalTLRecorder) RecordTLRound(_ context.Context, record goaldomain.TLRoundRecord) error {
	if r.service == nil || r.sessionID == "" {
		return nil
	}
	view, err := r.service.sessionAgentTeamViewJ1A(r.sessionID)
	if err != nil || !view.Configured {
		return nil // 未装配 team 的会话不记角色行
	}
	const roleName = "tl"
	roleSessionID := team.RoleSessionID(r.sessionID, view.TeamID, roleName)
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
		row(1, "role_context", "system", record.Context),                // b 本轮看到的原文
		row(2, goaldomain.DirectiveRowKind, "assistant", record.Output), // b 本轮回答的原文
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
	view, err := r.service.sessionAgentTeamViewJ1A(r.sessionID)
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

// ArchiveTLHistory 把 b 侧会话历史归档进 tl 角色历史（环逃生收口时调用；实现
// goal domain 的 TLHistoryArchiver）。
//
// 为什么要有这条：b 的"历史"（锚点 + 帧 + 回合摘要）只活在 Supervisor 的进程内
// AdvisorSession 里，而逃生收口会 reap 掉它——不落一行归档，这次 goal 的审查过程
// 就彻底查不到了（role draft 里逐回合的 role_context/tl_directive 是原文，但没有
// "这条 goal 到此为止、原因是逃生"的收口行，事后无法按行归因）。
//
// 归档失败返回错误由调用方（Supervisor.AbortOnEscape）降级成 goal 终态里的说明：
// 逃生收口本身不能因为留痕失败而失败。
func (r goalTLRecorder) ArchiveTLHistory(_ context.Context, record goaldomain.TLArchiveRecord) error {
	if r.service == nil || r.sessionID == "" {
		return nil
	}
	view, err := r.service.sessionAgentTeamViewJ1A(r.sessionID)
	if err != nil || !view.Configured {
		return nil
	}
	const roleName = "tl"
	roleSessionID := team.RoleSessionID(r.sessionID, view.TeamID, roleName)
	if strings.TrimSpace(roleSessionID) == "" {
		return nil
	}
	truncated := ""
	if record.Truncated {
		truncated = "（正文已截断）"
	}
	head := fmt.Sprintf("goal 逃生收口：原因 %s；goal=%s %s；b 历史 rounds=%d frames=%d%s",
		record.Reason, record.GoalID, record.GoalTitle, record.Rounds, record.Frames, truncated)
	row := dto.RoleDraftRow{
		RoleName: roleName, RoleSessionID: roleSessionID, UnitSeq: 1,
		Event: dto.RoleRow{
			Kind: goaldomain.ArchiveKindEscape, Role: "system", Content: head + "\n\n" + record.Content,
			RoleName: roleName, RoleSessionID: roleSessionID, UnitSeq: 1,
		},
	}
	if err := r.service.AppendRoleDraft(r.sessionID, roleName, roleSessionID, []dto.RoleDraftRow{row}); err != nil {
		return err
	}
	_, err = r.service.SyncRoleDraft(r.sessionID, roleName, roleSessionID, view.OrderRoles)
	return err
}
