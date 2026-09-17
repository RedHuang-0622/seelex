package core

// input_team.go — `@` 前缀（手动召唤团队）的落点。
//
// 生态位：`@` 是**人**的显式召唤入口。goal 上线时 goal 治理会自己装配
// goal-a2a（见 service.goal_service.ensureGoalAgentTeam 的自动路径），那条路
// 只在 goal 存在时成立；本文件给的是"用户在会话里点名一支团队"的对称入口：
// 同一个工厂、同一份 TeamSpec、同一条装配通道（建角色会话 + 写会话注册表 +
// 写 lifecycle 顺序），不新增第二份团队事实。
//
// 入伙切点：joinSeq 取装配那一刻主会话已提交的 message 尾 seq（
// service.teamJoinSeqFor），与自动路径同一条判据——teammate 的记录从"它入伙的
// 那一回合"开始，而不是把整段历史都算成它记得的上下文。
//
// 解析顺序（都是零/一次 I/O，便于在 Submit 路径上跑）：
//  1. 内置形态（agentteam.Presets，零 I/O，team_id == team_kind）；
//  2. 团队库条目（一次读，按 team_id 或 name 匹配）——用户自己存的团队也能召唤。
//
// 建议面只列内置形态，不列库条目：见 completion.go 的 teamPresetSuggestions。

import (
	"context"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/agentteam"
)

// teamSummonTarget 是一条可召唤团队的解析结果：内置形态（Preset）或团队库条目。
type teamSummonTarget struct {
	ID     string // 团队库 team_id / preset 的 team_id（同一值域）
	Kind   string // team_kind（装配时按它选形态；空 = 按 ID 解析）
	Name   string // 展示名（库条目可能是用户起的中文名；preset 用 ID）
	Preset bool
}

// submitTeam 是 `@` 前缀的落点：手动召唤一支团队到当前会话。
//
// name 已由路由去掉前缀并 TrimSpace；空名 = 让召唤面自述可用团队。
// 失败口径与 `$`/`#` 一致：输入类问题（空名、未知团队、读不到会话）只补一条
// notice（对话里的可见系统消息）并返回 nil，不把用户的打字错误升级成调用方
// 错误；装配真失败（存储未装配、工厂报错）才把错误抛给调用方。
func (service *Service) submitTeam(_ context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		service.addNotice(teamSummonHelp())
		return nil
	}
	sessionID := service.currentViewSessionID()
	if sessionID == "" {
		service.addNotice("当前没有可召唤团队的会话。")
		return nil
	}
	target, ok := service.resolveTeamSummon(sessionID, name)
	if !ok {
		service.addNotice(service.unknownTeamNotice(sessionID, name))
		return nil
	}
	result, err := service.materializeTeamSummon(sessionID, target)
	if err != nil {
		service.addNotice(fmt.Sprintf("召唤团队 %s 失败：%v", target.displayName(), err))
		return err
	}
	service.addNotice(teamSummonNotice(target, result))
	// 装配改了会话侧事实（注册表 + lifecycle 顺序 + 团队环），让订阅方按新状态
	// 重取快照——输入路径不返回视图给调用方，事件是唯一的下行通道。
	service.publishTeamSummonChanged()
	return nil
}

// materializeTeamSummon 把解析结果装配进会话（preset 与库条目各走既有方法）。
func (service *Service) materializeTeamSummon(sessionID string, target teamSummonTarget) (dto.TeamMaterializeResult, error) {
	joinSeq := service.teamJoinSeqFor(sessionID)
	if target.Preset {
		return service.MaterializeAgentTeamPreset(sessionID, target.Kind, joinSeq)
	}
	return service.AgentTeamMaterializeTeam(sessionID, target.ID, joinSeq)
}

// resolveTeamSummon 按名字解析可召唤团队：内置形态优先（零 I/O），再查团队库。
// 匹配不区分大小写；库条目同时接受 team_id 与用户起的 name。
func (service *Service) resolveTeamSummon(sessionID, name string) (teamSummonTarget, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return teamSummonTarget{}, false
	}
	for _, target := range teamPresetTargets() {
		if strings.EqualFold(target.ID, name) || strings.EqualFold(target.Kind, name) {
			return target, true
		}
	}
	library, err := service.AgentTeamLibrary(sessionID)
	if err != nil {
		// 库读不到（宿主未装配团队存储 / 无项目作用域）不应让召唤入口整体失效：
		// 内置形态仍可用，库条目只是这一轮查不到。
		return teamSummonTarget{}, false
	}
	for _, entry := range library.Teams {
		if strings.EqualFold(entry.TeamID, name) || strings.EqualFold(entry.Name, name) {
			return teamSummonTarget{ID: entry.TeamID, Kind: entry.TeamKind, Name: entry.Name}, true
		}
	}
	return teamSummonTarget{}, false
}

// teamPresetTargets 把内置团队形态投影成召唤候选（零 I/O）。
func teamPresetTargets() []teamSummonTarget {
	specs := agentteam.Presets()
	targets := make([]teamSummonTarget, 0, len(specs))
	for _, spec := range specs {
		targets = append(targets, teamSummonTarget{
			ID: spec.TeamID, Kind: spec.TeamKind, Name: spec.TeamID, Preset: true,
		})
	}
	return targets
}

func (target teamSummonTarget) displayName() string {
	if strings.TrimSpace(target.Name) != "" {
		return target.Name
	}
	return target.ID
}

// publishTeamSummonChanged 在召唤成功后发一次快照变更事件（会话作用域），
// 与"命令/技能改变了会话状态"同一条下行口径。
func (service *Service) publishTeamSummonChanged() {
	service.ViewMu.Lock()
	revision := service.bumpLocked()
	service.ViewMu.Unlock()
	service.publishSessionEvent(EventSnapshotChanged, revision, "", service.currentViewSessionID(), nil)
}

// teamSummonNotice 是装配回执：团队名 + 在编席位 + 发言顺序，并把 TeamView 的
// DesignNotice（"有装配没执行者"这类设计期提醒）原样带上——召唤完就看见，不用
// 再去面板里找。
func teamSummonNotice(target teamSummonTarget, result dto.TeamMaterializeResult) string {
	lines := []string{fmt.Sprintf("已召唤团队 %s：%d 个席位在编", target.displayName(), len(result.View.Members))}
	if roles := memberNames(result.View.Members); len(roles) > 0 {
		lines = append(lines, "成员 "+strings.Join(roles, " · "))
	}
	if len(result.View.OrderRoles) > 0 {
		lines = append(lines, "发言顺序 "+strings.Join(result.View.OrderRoles, "→"))
	}
	lines = append(lines, result.View.DesignNotice...)
	return strings.Join(lines, "\n")
}

// memberNames 按成员表顺序取角色名（跳过空名）。
func memberNames(members []dto.TeamMember) []string {
	names := make([]string, 0, len(members))
	for _, member := range members {
		if strings.TrimSpace(member.RoleName) == "" {
			continue
		}
		names = append(names, member.RoleName)
	}
	return names
}

// unknownTeamNotice 在名字没命中任何团队时给出可行动提示。
//
// 这条路径每次失败只跑一次（不是渲染循环），所以这里**可以**读一次团队库：把
// 库里现有的 team_id 列出来，用户打错字时不至于对着一句"未知团队"干瞪眼。
func (service *Service) unknownTeamNotice(sessionID, name string) string {
	lines := []string{fmt.Sprintf("未知团队: %s。", name), teamSummonHelp()}
	if library, err := service.AgentTeamLibrary(sessionID); err == nil && len(library.Teams) > 0 {
		ids := make([]string, 0, len(library.Teams))
		for _, entry := range library.Teams {
			ids = append(ids, entry.TeamID)
		}
		lines = append(lines, "团队库里有： "+strings.Join(ids, " · "))
	}
	if strings.EqualFold(name, "off") || strings.EqualFold(name, "none") {
		// 旧写法 @off = 停用插件（前缀契约调整前 @ 就是插件面）。
		lines = append(lines, fmt.Sprintf("停用插件请用 %soff。", SigilPlugin))
	}
	if hint := service.sigilMigrationHint(SigilTeam, name); hint != "" {
		lines = append(lines, hint)
	}
	return strings.Join(lines, "\n")
}

// teamSummonHelp 是 `@` 的自述：内置形态逐个列出（摘要取自形态自身的事实），
// 并说明库条目同样可召唤。
func teamSummonHelp() string {
	lines := []string{fmt.Sprintf("%s 手动召唤团队：%s<团队> 把一支团队装配到当前会话（入伙切点 = 当前消息尾）。", SigilTeam, SigilTeam)}
	for _, suggestion := range teamPresetSuggestions() {
		lines = append(lines, fmt.Sprintf("  %s%s  %s", SigilTeam, suggestion.Text, suggestion.Description))
	}
	lines = append(lines, "团队库条目（自己存的团队）也可用 team_id 或名字召唤。")
	return strings.Join(lines, "\n")
}
