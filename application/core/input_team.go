package core

// input_team.go — `@` 前缀（手动召唤团队）的落点。
//
// 生态位：`@` 是**人**的显式召唤入口——把**团队库里的一支团队**装配到当前会话，
// 与目标态里"leader 按 team plan 组装团队"是同一条装配通道（同一个工厂、同一份
// TeamSpec：建角色会话 + 写会话注册表 + 写 lifecycle 顺序），不新增第二份团队事实。
//
// **没有内置形态这条路了**（2026-10-01）：内置形态目录（goal-a2a / review-team /
// research-team 三支代码模板）已删除，所以 `@` 只认团队库条目；goal 也不再在
// 上线时自动装配团队（见 service.GoalBeginForFor 的说明）。
//
// 入伙切点：joinSeq 取装配那一刻主会话已提交的 message 尾 seq（
// service.teamJoinSeqFor），与其它装配来路同一条判据——teammate 的记录从"它入伙的
// 那一回合"开始，而不是把整段历史都算成它记得的上下文。
//
// 写法：`@<团队>` 只装配；`@<团队> <附言>` 装配后把附言作为一条输入下发
// （与 `$<skill> <args>` 同一条口径）。团队名可以含空格，所以切分不按空格硬切，
// 而是"最长可命中前缀 = 名字，余下 = 附言"（见 resolveTeamSummon）。
//
// 建议面：`@` 会弹补全，候选 = 团队库里的每支团队（`Suggestions` 收到 `@` 时列
// 出、`@go` 这类前缀可过滤）。代价口径是"读一次、留进程内快照"，不是"每次按键
// 读盘"——取舍与失效边界见 completion.go 末尾「`@` 的建议面」一段。可用名字另外
// 由 `@` 空参的 notice 列出（见 teamSummonHelp），两条面读的是同一份库。

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	goaldomain "github.com/RedHuang-0622/seelex/application/core/goal"
)

// teamSummonTarget 是一条可召唤团队的解析结果（团队库条目）。
type teamSummonTarget struct {
	ID   string // 团队库 team_id
	Kind string // team_kind（= 展示别名；空 = 按 ID 解析）
	Name string // 展示名（库条目可能是用户起的中文名）
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
		service.addNotice(service.teamSummonHelp(service.currentViewSessionID()))
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
	if tail == "" {
		service.addNotice(teamSummonNotice(target, result, "", nil))
		return nil
	}
	// 召唤即干活：先落 goal，再把附言作为一条输入下发（原文交给会话，与
	// `$<skill> <args>` 同一条口径）。
	//
	// 顺序不能颠倒：goal 必须在主会话这一轮跑起来之前就在栈上，否则 leader 这一轮
	// 看不到活动目标（无从按阶段派活）。
	record, err := service.beginGoalForSummon(ctx, sessionID, tail)
	if err != nil {
		// 落 goal 失败不该吞掉这次召唤：团队已经装配好了，至少把附言按旧口径
		// 作为一条输入下发，并把失败原因明说（静默等于又把用户的话吃掉一次）。
		service.addNotice(fmt.Sprintf("召唤团队 %s 已装配，但落 goal 失败：%v", target.displayName(), err))
		service.prepareCompletedTaskBoundary()
		return service.submitConversation(ctx, SigilTeam+name)
	}
	service.addNotice(teamSummonNotice(target, result, tail, record))
	// 装配通告（team.changed）已由 MaterializeAgentTeam 这个共同收口发出：`@`、
	// goal 自动装配与面板 RPC 走的是同一条路，面板缓存因此不依赖调用方是谁。
	service.prepareCompletedTaskBoundary()
	return service.submitConversation(ctx, SigilTeam+name)
}

// beginGoalForSummon 是"召唤即干活"的落点：`@<团队> <附言>` 里的附言是一条要干的
// 活——只装配不落 goal，召唤完就停在"在编但没有人开工"（这正是"teammate 没有开始
// 工作"的根因）。
//
// 于是：装配成功后把附言落成一个 goal（附言 = 目标陈述），随后由**主代理（leader）
// 按 team_plan 阶段派活**推进；目标收口后团队离场（见 goal_service.go 的
// dismissTeamWhenGoalClosed）。
//
// 与 goal_begin 工具路径的差别：这里**不**调 ensureGoalAgentTeam——召唤已经装配了
// 用户点名的那支团队，再补一支 goal-a2a 等于替用户改团队（召唤 review-team 却长出
// 一个 tl）。
func (service *Service) beginGoalForSummon(ctx context.Context, sessionID, tail string) (*goaldomain.GoalRecord, error) {
	coordinator, err := service.goalCoordinatorFor(sessionID)
	if err != nil {
		return nil, err
	}
	record, err := coordinator.Begin(ctx, sessionID, goaldomain.BeginRequest{
		Title:     goalTitleForSummon(tail),
		Statement: tail,
	})
	if err != nil {
		return nil, err
	}
	service.refreshGoalRuntimeProjection(sessionID)
	return record, nil
}

// summonGoalTitleMaxRunes 是"附言 → goal 标题"的截断上限（runes）。
const summonGoalTitleMaxRunes = 40

// goalTitleForSummon 由附言派生 goal 标题：goal_begin 要求 title 必填，而召唤场景
// 里用户只给了那句话——取它的首行、压掉换行与多余空白，超长按 rune 截断（按字节截
// 会把中文截成半个字）。
func goalTitleForSummon(tail string) string {
	title := strings.Join(strings.Fields(tail), " ")
	if title == "" {
		return "召唤团队"
	}
	if runes := []rune(title); len(runes) > summonGoalTitleMaxRunes {
		title = strings.TrimSpace(string(runes[:summonGoalTitleMaxRunes])) + "…"
	}
	return title
}

// materializeTeamSummon 把解析结果装配进会话（走团队库条目那条既有方法）。
func (service *Service) materializeTeamSummon(sessionID string, target teamSummonTarget) (dto.TeamMaterializeResult, error) {
	joinSeq := service.teamJoinSeqFor(sessionID)
	return service.AgentTeamMaterializeTeam(sessionID, target.ID, joinSeq)
}

// resolveTeamSummon 解析"名字 + 附言"：名字命中团队库条目时返回目标与附言
// （= 名字之后的余量，可能为空），未命中返回 ok=false。
//
// 名字**可以含空格**（团队库条目由用户起名），所以不按空格硬切：候选从整串开始
// 按空白边界逐级回退（见 teamNameCandidates），命中的最长前缀是团队名，余下的是
// 附言。`@审计小队`、`@code review team`、`@my-team 看看这个 bug` 三种写法因此
// 都能落到实处，而不是把半句话整体当名字去查库。
//
// 团队库只读一次（候选逐个查表，不逐个读盘）：库读不到时召唤入口报"未知团队"
// （不再有内置形态可退）。
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

// teamSummonIndex 是一次召唤解析用的名字集合（团队库条目，一次读取）。
type teamSummonIndex struct {
	library []teamSummonTarget
}

// teamSummonIndex 组装解析用的名字集合（团队库一次读取）。
func (service *Service) teamSummonIndex(sessionID string) teamSummonIndex {
	index := teamSummonIndex{}
	library, err := service.AgentTeamLibrary(sessionID)
	if err != nil {
		// 库读不到（宿主未装配团队存储 / 无项目作用域）：没有内置形态可退，召唤面
		// 就报"未知团队"，但入口本身不炸。
		return index
	}
	for _, entry := range library.Teams {
		index.library = append(index.library, teamSummonTarget{
			ID: entry.TeamID, Kind: entry.TeamKind, Name: entry.Name,
		})
	}
	return index
}

// match 按既有口径查名：团队库条目看 team_id/名字（team_kind 只是别名，也能命中）；
// 匹配不区分大小写。
func (index teamSummonIndex) match(name string) (teamSummonTarget, bool) {
	for _, entry := range index.library {
		if strings.EqualFold(entry.ID, name) || strings.EqualFold(entry.Name, name) || (entry.Kind != "" && strings.EqualFold(entry.Kind, name)) {
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

func (target teamSummonTarget) displayName() string {
	if strings.TrimSpace(target.Name) != "" {
		return target.Name
	}
	return target.ID
}

// teamSummonNotice 是装配回执：团队名 + 在编成员 + 发言顺序，并把 TeamView 的
// DesignNotice（"有装配没执行者"这类设计期提醒）原样带上——召唤完就看见，不用
// 再去面板里找。带附言时明说"已落目标、附言已下发、目标收口后离场"，免得用户以为
// 那句话被吞了、或者以为召完就有人在干（两件事以前都不会被说出来）。
func teamSummonNotice(target teamSummonTarget, result dto.TeamMaterializeResult, tail string, record *goaldomain.GoalRecord) string {
	lines := []string{fmt.Sprintf("已召唤团队 %s：%d 名成员在编", target.displayName(), len(result.View.Members))}
	if roles := memberNames(result.View.Members); len(roles) > 0 {
		lines = append(lines, "成员 "+strings.Join(roles, " · "))
	}
	if len(result.View.OrderRoles) > 0 {
		lines = append(lines, "发言顺序 "+strings.Join(result.View.OrderRoles, "→"))
	}
	if tail != "" {
		lines = append(lines, "附言已作为本会话的一条输入下发。")
	}
	if record != nil {
		lines = append(lines, fmt.Sprintf(
			"已落目标 %s「%s」：teammate 随本轮开工，目标收口后离场。", record.ID, record.Title))
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
	lines := []string{fmt.Sprintf("未知团队: %s。", name), service.teamSummonHelp(sessionID)}
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

// teamSummonHelp 是 `@` 的自述：**可用团队从团队库里读**（一次读取——这条不是渲染
// 路径：`@` 空参或打错字时各跑一次），逐个列出 team_id 与展示名；不再有"内置形态"
// 这类模板（目录已删，见本文件头注）。
func (service *Service) teamSummonHelp(sessionID string) string {
	lines := []string{fmt.Sprintf("%s 手动召唤团队：%s<团队> [附言] 把一支团队装配到当前会话（入伙切点 = 当前消息尾）；写了附言就是「召唤即干活」——附言落成一个目标并作为一条输入下发，teammate 随本轮开工，目标收口后团队离场。", SigilTeam, SigilTeam)}
	library, err := service.AgentTeamLibrary(sessionID)
	if err != nil || len(library.Teams) == 0 {
		lines = append(lines, "团队库还是空的：先在 Agent Team 面板的「团队库」里新建一支团队，再用它的名字召唤。")
		return strings.Join(lines, "\n")
	}
	for _, entry := range library.Teams {
		label := strings.TrimSpace(entry.Name)
		if label == "" || label == entry.TeamID {
			lines = append(lines, fmt.Sprintf("  %s%s", SigilTeam, entry.TeamID))
			continue
		}
		lines = append(lines, fmt.Sprintf("  %s%s  %s", SigilTeam, entry.TeamID, label))
	}
	lines = append(lines, "（team_id 或团队名都能命中；团队名可以含空格。）")
	return strings.Join(lines, "\n")
}
