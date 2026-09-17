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
// 写法：`@<团队>` 只装配；`@<团队> <附言>` 装配后把附言作为一条输入下发
// （与 `$<skill> <args>` 同一条口径）。团队名可以含空格，所以切分不按空格硬切，
// 而是"最长可命中前缀 = 名字，余下 = 附言"（见 resolveTeamSummon）。
//
// 建议面只列内置形态，不列库条目：见 completion.go 的 teamPresetSuggestions。

import (
	"context"
	"fmt"
	"strings"
	"unicode"

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
//
// `@<团队> <附言>`：路由**不**按空格硬切（团队名可以含空格，见
// teamNameCandidates），切分在这里按"最长可命中前缀"完成；命中后余量就是附言，
// 按 `$<skill> <args>` 的同一条口径作为一条输入下发（原文一并交给会话，与 `$`
// 一致）。这样 `@goal-a2a 看看这个 bug` 既装配了团队，也不吞掉用户那句话。
//
// 失败口径与 `$`/`#` 一致：输入类问题（空名、未知团队、读不到会话）只补一条
// notice（对话里的可见系统消息）并返回 nil，不把用户的打字错误升级成调用方
// 错误；装配真失败（存储未装配、工厂报错）才把错误抛给调用方。
func (service *Service) submitTeam(ctx context.Context, name string) error {
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
	target, tail, ok := service.resolveTeamSummon(sessionID, name)
	if !ok {
		// 报"最可能的名字"（首个 token）而不是整句：候选已经逐级试过，全部落空时
		// 把半句话当名字只会让用户对着自己那句话发愣。
		service.addNotice(service.unknownTeamNotice(sessionID, presumedTeamName(name)))
		return nil
	}
	result, err := service.materializeTeamSummon(sessionID, target)
	if err != nil {
		service.addNotice(fmt.Sprintf("召唤团队 %s 失败：%v", target.displayName(), err))
		return err
	}
	service.addNotice(teamSummonNotice(target, result, tail))
	// 装配通告（team.changed）已由 MaterializeAgentTeam 这个共同收口发出：`@`、
	// goal 自动装配与面板 RPC 走的是同一条路，面板缓存因此不依赖调用方是谁。
	if tail == "" {
		return nil
	}
	// 附言下发：原文 = 前缀 + 名字，即用户输入去掉首尾空白（`$` 同样把原文交给
	// 会话，不裁剪出的人工文本）。
	service.prepareCompletedTaskBoundary()
	return service.submitConversation(ctx, SigilTeam+name)
}

// materializeTeamSummon 把解析结果装配进会话（preset 与库条目各走既有方法）。
func (service *Service) materializeTeamSummon(sessionID string, target teamSummonTarget) (dto.TeamMaterializeResult, error) {
	joinSeq := service.teamJoinSeqFor(sessionID)
	if target.Preset {
		return service.MaterializeAgentTeamPreset(sessionID, target.Kind, joinSeq)
	}
	return service.AgentTeamMaterializeTeam(sessionID, target.ID, joinSeq)
}

// resolveTeamSummon 解析"名字 + 附言"：名字命中内置形态或团队库条目时返回目标与
// 附言（= 名字之后的余量，可能为空），未命中返回 ok=false。
//
// 名字**可以含空格**（团队库条目由用户起名），所以不按空格硬切：候选从整串开始
// 按空白边界逐级回退（见 teamNameCandidates），命中的最长前缀是团队名，余下的是
// 附言。`@审计小队`、`@code review team`、`@goal-a2a 看看这个 bug` 三种写法因此
// 都能落到实处，而不是把半句话整体当名字去查库。
//
// 团队库只读一次（候选逐个查表，不逐个读盘）：库读不到也不该让召唤入口整体失效
// ——内置形态仍可用，库条目只是这一轮查不到。
func (service *Service) resolveTeamSummon(sessionID, name string) (teamSummonTarget, string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return teamSummonTarget{}, "", false
	}
	index := service.teamSummonIndex(sessionID)
	for _, candidate := range teamNameCandidates(name) {
		if target, ok := index.match(candidate); ok {
			return target, strings.TrimSpace(name[len(candidate):]), true
		}
	}
	return teamSummonTarget{}, "", false
}

// teamSummonIndex 是一次召唤解析用的名字集合：内置形态（零 I/O）+ 团队库条目。
type teamSummonIndex struct {
	presets []teamSummonTarget
	library []teamSummonTarget
}

// teamSummonIndex 组装解析用的名字集合（内置形态优先，库条目一次读取）。
func (service *Service) teamSummonIndex(sessionID string) teamSummonIndex {
	index := teamSummonIndex{presets: teamPresetTargets()}
	library, err := service.AgentTeamLibrary(sessionID)
	if err != nil {
		// 库读不到（宿主未装配团队存储 / 无项目作用域）不应让召唤入口整体失效：
		// 内置形态仍可用，库条目只是这一轮查不到。
		return index
	}
	for _, entry := range library.Teams {
		index.library = append(index.library, teamSummonTarget{
			ID: entry.TeamID, Kind: entry.TeamKind, Name: entry.Name,
		})
	}
	return index
}

// match 按既有口径查名：内置形态看 team_id/team_kind，库条目看 team_id/名字；
// 匹配不区分大小写。先内置后库 = 保留"内置形态优先"这条旧判据。
func (index teamSummonIndex) match(name string) (teamSummonTarget, bool) {
	for _, target := range index.presets {
		if strings.EqualFold(target.ID, name) || strings.EqualFold(target.Kind, name) {
			return target, true
		}
	}
	for _, entry := range index.library {
		if strings.EqualFold(entry.ID, name) || strings.EqualFold(entry.Name, name) {
			return entry, true
		}
	}
	return teamSummonTarget{}, false
}

// teamNameCandidates 给出"名字可能是哪一段"的候选，**最长优先**：整串，以及它在
// 空白边界上的逐级前缀（`a b c` → `a b c`、`a b`、`a`）。
//
// 每个候选都是原串的字节前缀，因此 `name[len(candidate):]` 恒为附言。最长优先是
// 为了让"更长更具体"的名字赢：库里同时有 `审计` 与 `审计 小队` 时，`@审计 小队 请审核`
// 命中后者，而不是命中前者 + 附言 `小队 请审核`。
func teamNameCandidates(name string) []string {
	candidates := []string{name}
	remaining := name
	for {
		trimmed := strings.TrimRightFunc(remaining, unicode.IsSpace)
		cut := strings.LastIndexFunc(trimmed, unicode.IsSpace)
		if cut <= 0 {
			return candidates
		}
		remaining = strings.TrimRightFunc(remaining[:cut], unicode.IsSpace)
		if remaining == "" {
			return candidates
		}
		candidates = append(candidates, remaining)
	}
}

// presumedTeamName 从"名字 + 附言"里取最可能的名字（首个 token）：全部候选都没
// 命中时用它报"未知团队"，不把用户整句话当成名字回显。
func presumedTeamName(name string) string {
	if fields := strings.Fields(name); len(fields) > 0 {
		return fields[0]
	}
	return strings.TrimSpace(name)
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

// teamSummonNotice 是装配回执：团队名 + 在编席位 + 发言顺序，并把 TeamView 的
// DesignNotice（"有装配没执行者"这类设计期提醒）原样带上——召唤完就看见，不用
// 再去面板里找。带附言时明说附言已下发，免得用户以为那句话被吞了。
func teamSummonNotice(target teamSummonTarget, result dto.TeamMaterializeResult, tail string) string {
	lines := []string{fmt.Sprintf("已召唤团队 %s：%d 个席位在编", target.displayName(), len(result.View.Members))}
	if roles := memberNames(result.View.Members); len(roles) > 0 {
		lines = append(lines, "成员 "+strings.Join(roles, " · "))
	}
	if len(result.View.OrderRoles) > 0 {
		lines = append(lines, "发言顺序 "+strings.Join(result.View.OrderRoles, "→"))
	}
	if tail != "" {
		lines = append(lines, "附言已作为本会话的一条输入下发。")
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
	lines := []string{fmt.Sprintf("%s 手动召唤团队：%s<团队> [附言] 把一支团队装配到当前会话（入伙切点 = 当前消息尾）；写了附言时它随召唤作为一条输入下发。", SigilTeam, SigilTeam)}
	for _, suggestion := range teamPresetSuggestions() {
		lines = append(lines, fmt.Sprintf("  %s%s  %s", SigilTeam, suggestion.Text, suggestion.Description))
	}
	lines = append(lines, "团队库条目（自己存的团队）也可用 team_id 或名字召唤。")
	return strings.Join(lines, "\n")
}
