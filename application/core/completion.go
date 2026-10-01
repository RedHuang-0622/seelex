package core

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

// ── 输入前缀（sigil）契约 ──────────────────────────────────────────────
//
// 前缀与含义**一一对应**，不做跨域兜底猜测：面板、输入框内联建议与 Submit
// 路由共用这一张表（权威说明见 docs/gui/modules/shell-and-interactions.md）。
//
//	/  可执行入口：命令 + Skill（工具**不**列出）
//	#  切换 Plugin（含 off/none = 停用全部）
//	$  召回 Skill（激活到当前会话）
//	@  手动召唤团队：团队库里的一支团队，装配到当前会话（可跟一句附言）
//
// `/` 只放"能从输入框直接执行"的东西。工具是模型侧的（由模型调用、经权限门），
// 因此既不进建议也不进路由：一个能力要让用户打 `/名字` 显式调用，前提是它已注册
// 成命令；没注册就没有这个入口，不是"入口藏在别处"。打错到工具名时由
// unknownCommandNotice 说清这一点并给出同名命令入口。
//
// 前缀契约调整过（# 从 Skill 改为 Plugin、$ 接管 Skill、@ 从 Plugin 改为召唤
// 团队），旧肌肉记忆打老前缀时给一句迁移提示（见 sigilMigrationHint），而不是
// 让用户对着"未知"猜该用哪个符号。
const (
	SigilCommand = "/"
	SigilPlugin  = "#"
	SigilSkill   = "$"
	SigilTeam    = "@"
)

// sigils 是前缀的唯一枚举（顺序 = 解析顺序）。任何新增前缀都从这里生效：
// 建议面（Suggestions）与迁移提示按它推导，不在别处再写一份字面量。
var sigils = []string{SigilCommand, SigilPlugin, SigilSkill, SigilTeam}

// 建议条目的域标记：前端按它选图标，TUI 按它显示标签。只有能从输入框直接执行的
// 域才会成为候选（无 tool：工具由模型调用，见文件头前缀契约）。
const (
	SuggestionKindCommand = "command"
	SuggestionKindSkill   = "skill"
	SuggestionKindPlugin  = "plugin"
	SuggestionKindTeam    = "team"
)

type Suggestion struct {
	Text        string `json:"text"`
	Description string `json:"description,omitempty"`
	Kind        string `json:"kind"`

	// alias 是前缀过滤的**补充键**（不导出、不进 JSON、前端看不见）：团队库条目除了
	// team_id 还有用户起的展示名，两种写法都该能被前缀命中（打 `@改良` 要能弹出
	// Text=`goal-a2a` 的那一行）。其它域没有第二个键，留空。
	alias string
}

// Suggestions 按输入前缀给出候选：前缀不认识、或已经进入参数区（含空格）时
// 返回 nil（空建议 = 不弹面板）。
func (service *Service) Suggestions(input string) []Suggestion {
	trigger, prefix, ok := splitSigil(input)
	if !ok {
		return nil
	}
	if strings.Contains(prefix, " ") {
		return nil
	}
	all := make([]Suggestion, 0)
	switch trigger {
	case SigilCommand:
		// `/` 只列可执行入口：命令 + Skill（Skill 的专用前缀是 `$`，但斜杠命令
		// 历史上就能召回技能，不收回）。工具不在这里——要让用户打 `/名字`
		// 显式调用，先把这个能力注册成命令。
		all = append(all, service.commandSuggestions()...)
		all = append(all, service.skillSuggestions()...)
	case SigilPlugin:
		all = append(all, service.pluginSuggestions()...)
	case SigilSkill:
		all = append(all, service.skillSuggestions()...)
	case SigilTeam:
		// `@` 的候选在**团队库**里（全局数据文件），不是零 I/O 的内存注册表：走进程内
		// 快照，见本文件末尾「`@` 的建议面」一段的取舍与失效边界。
		all = append(all, service.teamSuggestions()...)
	}
	lower := strings.ToLower(prefix)
	filtered := all[:0]
	for _, suggestion := range all {
		if lower == "" || strings.HasPrefix(strings.ToLower(suggestion.Text), lower) ||
			strings.HasPrefix(strings.ToLower(suggestion.alias), lower) {
			filtered = append(filtered, suggestion)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		priority := map[string]int{
			SuggestionKindCommand: 0, SuggestionKindSkill: 1,
			SuggestionKindPlugin: 2, SuggestionKindTeam: 3,
		}
		if priority[filtered[i].Kind] != priority[filtered[j].Kind] {
			return priority[filtered[i].Kind] < priority[filtered[j].Kind]
		}
		return filtered[i].Text < filtered[j].Text
	})
	return append([]Suggestion(nil), filtered...)
}

// splitSigil 拆出输入前缀与它后面的文本（前缀不认识时 ok=false）。
func splitSigil(input string) (trigger, prefix string, ok bool) {
	for _, sigil := range sigils {
		if strings.HasPrefix(input, sigil) {
			return sigil, strings.TrimPrefix(input, sigil), true
		}
	}
	return "", "", false
}

// HasSigilPrefix 报告输入是否以前缀开头（TUI 的 suggMode 与前端内联建议共用同一条
// 判定，避免"哪些字符算前缀"在多个前端各写一份而漂移）。
func HasSigilPrefix(input string) bool {
	_, _, ok := splitSigil(input)
	return ok
}

// SigilOf 返回输入的首字符前缀；不可解析时返回空串（调用方自行给默认值）。
func SigilOf(input string) string {
	trigger, _, ok := splitSigil(input)
	if !ok {
		return ""
	}
	return trigger
}

// sigilMigrationHint 在前缀没命中时给一句跨域迁移提示：命中**别的**域才给，
// 否则空串（调用方据此决定要不要补一句 notice）。
//
// 只查零 I/O 的域（Skill / Plugin / 内置团队形态）：这条提示落在错误路径上，
// 不值得为它读一次团队库。
func (service *Service) sigilMigrationHint(used, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	hints := make([]string, 0, len(sigils))
	if used != SigilPlugin && service.hasPlugin(name) {
		hints = append(hints, fmt.Sprintf("切换插件用 %s%s", SigilPlugin, name))
	}
	if used != SigilSkill {
		if _, ok := service.Deps.Skills.Get(name); ok {
			hints = append(hints, fmt.Sprintf("召回 Skill 用 %s%s", SigilSkill, name))
		}
	}
	// 「召唤团队用 @xxx」这条提示已去掉（2026-10-01）：它的判据曾是内置形态名（零
	// I/O），形态目录删除后判据变成"团队库里有这支团队" = 要读盘，不值得为错误路径
	// 上的一句提示付这个代价。
	if len(hints) == 0 {
		return ""
	}
	return "（" + strings.Join(hints, "；") + "）"
}

// hasPlugin 报告名字是否命中已加载插件（PluginPort 没有按名查询，只能扫列表）。
func (service *Service) hasPlugin(name string) bool {
	for _, plugin := range service.Deps.Plugins.All() {
		if strings.EqualFold(plugin.Name, name) {
			return true
		}
	}
	return false
}

// unknownCommandNotice 报告未知命令，并尽量给一条能走下去的提示。
//
// 两个真实来路：
//   - 打的是**工具名**（`/compact_context`、`/bash`）：工具只在模型那一侧（由模型
//     调用、经权限门），提交路径没有也不会执行工具，`/` 面板因此也不列它。命中工具
//     名就说清这一点，并指出同名能力的命令入口（`compact_context` → `/compact`）；
//     要让某个工具有 `/名字` 入口，唯一途径是把它注册成命令。
//   - 命令名打错（`/comapct`）或写成近义名：与某个命令名互为前缀/包含/近似
//     （编辑距离 ≤ 2）时直接给出正确写法。
func (service *Service) unknownCommandNotice(name string) string {
	trimmed := strings.TrimSpace(name)
	notice := fmt.Sprintf("未知命令: %s。输入 %shelp 查看可用命令。", trimmed, SigilCommand)
	hints := make([]string, 0, 2)
	if service.visibleToolNamed(trimmed) {
		hints = append(hints, fmt.Sprintf("「%s」是模型侧工具（由模型调用、经权限门），不能从输入框直接执行", trimmed))
	}
	if counterpart := service.commandCounterpart(trimmed); counterpart != "" {
		hints = append(hints, fmt.Sprintf("你是想用 %s%s 吗？", SigilCommand, counterpart))
	}
	if len(hints) == 0 {
		return notice
	}
	return notice + "（" + strings.Join(hints, "；") + "）"
}

// visibleToolNamed 报告名字是否命中当前可见工具（大小写不敏感）。
func (service *Service) visibleToolNamed(name string) bool {
	if service == nil || service.Deps.Runtime == nil || strings.TrimSpace(name) == "" {
		return false
	}
	for _, tool := range service.Deps.Runtime.VisibleTools(context.Background()) {
		if strings.EqualFold(tool.Name, strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}

// commandCounterpart 找与用户输入最像的命令名：前缀/包含关系优先，其次编辑距离
// ≤ 2（漏字、换位、邻键错字）。没有相似命令时返回 ""。
func (service *Service) commandCounterpart(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return ""
	}
	best, bestScore := "", 0
	for _, command := range service.commands.All() {
		candidate := strings.ToLower(command.Name())
		if candidate == "" || candidate == lower {
			continue
		}
		matched := false
		switch {
		case strings.HasPrefix(lower, candidate), strings.Contains(lower, candidate):
			matched = true
		case len(candidate) <= 16 && len(lower) <= 16:
			matched = editDistanceAtMost(lower, candidate, 2)
		}
		if matched && len(candidate) > bestScore {
			best, bestScore = candidate, len(candidate)
		}
	}
	return best
}

// editDistanceAtMost 判定两串的 Levenshtein 距离是否 ≤ limit（有界实现：只保留
// 相邻两行，任一行最小值超限即早退）。
func editDistanceAtMost(a, b string, limit int) bool {
	ar, br := []rune(a), []rune(b)
	if diff := len(ar) - len(br); diff > limit || diff < -limit {
		return false
	}
	previous := make([]int, len(br)+1)
	current := make([]int, len(br)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		current[0] = i
		rowMin := current[0]
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, min(current[j-1]+1, previous[j-1]+cost))
			if current[j] < rowMin {
				rowMin = current[j]
			}
		}
		if rowMin > limit {
			return false
		}
		copy(previous, current)
	}
	return previous[len(br)] <= limit
}

func (service *Service) commandSuggestions() []Suggestion {
	suggestions := make([]Suggestion, 0)
	for _, command := range service.commands.All() {
		suggestions = append(suggestions, Suggestion{
			Text: command.Name(), Description: command.Description(), Kind: SuggestionKindCommand,
		})
	}
	return suggestions
}

func (service *Service) skillSuggestions() []Suggestion {
	suggestions := make([]Suggestion, 0)
	for _, skill := range service.Deps.Skills.All() {
		suggestions = append(suggestions, Suggestion{
			Text: skill.Name, Description: skill.Description, Kind: SuggestionKindSkill,
		})
	}
	return suggestions
}

// pluginSuggestions 列出可切换插件；off 是"停用全部"的内置入口（与
// SwitchPlugin 的 off/none/空 三种写法同义，面板只暴露一个）。
func (service *Service) pluginSuggestions() []Suggestion {
	suggestions := make([]Suggestion, 0)
	for _, plugin := range service.Deps.Plugins.All() {
		suggestions = append(suggestions, Suggestion{
			Text: plugin.Name, Description: plugin.Description, Kind: SuggestionKindPlugin,
		})
	}
	return append(suggestions, Suggestion{
		Text: "off", Description: "停用所有插件", Kind: SuggestionKindPlugin,
	})
}

// ── `@` 的建议面：团队库的进程内快照 ──────────────────────────────────
//
// 2026-10-01 曾把 `@` 的建议面整个去掉，理由是"候选在团队库里、Suggestions 跑在
// TUI 的 View() 渲染路径与 GUI 每次输入事件上，逐键读盘不划算"。那个取舍只对
// **逐键读盘**成立，不对"没有建议面"成立：用户打 `@` 却看不到库里有什么团队，
// 只能靠记忆把 team_id 打全。于是这里保留零 I/O 的结论，改掉"无建议面"的实现：
// 候选读一次、留进程内缓存，按键路径只走内存。
//
// 失效边界（缓存能看到的本进程变化，两条都覆盖）：
//
//  1. 库写路径：AgentTeamSaveTeam / AgentTeamSaveCurrentTeam / AgentTeamDeleteTeam /
//     AgentTeamPublishToGlobal 一律清缓存——本进程刚改的库，下一次按键就反映；
//  2. 库读回：AgentTeamLibrary（面板 RPC / `@` 空参回执的公共读面）与
//     AgentTeamGlobalConfig 也清缓存——"刚看过磁盘"的时刻顺手让缓存重新取一份，
//     比让它继续陈旧便宜。
//
// 已知缺口：**另一个进程**（外部编辑器、另一个 Seelex 实例）改了 `team/library.json`
// 时，本进程既没写也没读回，缓存不会自己发现——界面停在旧库，直到上面两类事件之一
// 发生。要有界地收口只能加文件指纹或定时重读，那等于把"逐键零 I/O"换成"逐键 stat
// 一次盘"，对建议面不值；这条缺口比"@ 永远列不出团队"轻得多。
//
// 按会话分键：库读面的**作用域**由锚定会话解析（团队库本身是全局母表，但旧的
// 项目级布局回退按锚定会话定位所属项目），所以"哪个会话问的"是快照的一部分——切到
// 另一侧的会话读到的是那一侧解析出来的库，而不是上一个会话缓存下来的那份。
//
// 读取失败（宿主未装配团队存储 / 无会话号 / 库文件损坏）**不**缓存：失败通常意味着
// "此刻读不到"，把它当空库缓存下来会压住之后成功的读取。
type teamLibrarySuggestions struct {
	mu       sync.RWMutex
	sessions map[string][]Suggestion
}

// lookup 返回该会话的快照（第二个值 = 是否有快照）。返回的切片归缓存所有，调用方
// 只读——Suggestions 先把候选 append 进自己的切片再做前缀过滤。
func (cache *teamLibrarySuggestions) lookup(sessionID string) ([]Suggestion, bool) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	entries, ok := cache.sessions[sessionID]
	return entries, ok
}

func (cache *teamLibrarySuggestions) store(sessionID string, entries []Suggestion) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.sessions == nil {
		cache.sessions = make(map[string][]Suggestion)
	}
	cache.sessions[sessionID] = entries
}

// forget 丢**全部**快照：库是全局母表，写一次影响所有键，逐个键清没有意义。
func (cache *teamLibrarySuggestions) forget() {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.sessions = nil
}

// invalidateTeamLibrarySuggestions 让 `@` 的建议面快照过期（库写路径与库读回共用，
// 见本段开头列出的边界）。
func (service *Service) invalidateTeamLibrarySuggestions() {
	if service == nil {
		return
	}
	service.teamLibrarySnapshots.forget()
}

// teamSuggestions 返回 `@` 的候选：团队库里的每一支团队。
func (service *Service) teamSuggestions() []Suggestion {
	// 团队库是全局粒度，但存储端口要一个会话号来解析数据根（旧项目级布局的只读回退
	// 也按它定位），所以这里取当前视图会话——与 `@` 召回路上的取法一致，快照也按它分键。
	sessionID := service.currentViewSessionID()
	if cached, ok := service.teamLibrarySnapshots.lookup(sessionID); ok {
		return cached
	}
	library, err := service.agentTeamLibrary(sessionID)
	if err != nil {
		return nil
	}
	view, err := library.View()
	if err != nil {
		return nil
	}
	entries := make([]Suggestion, 0, len(view.Teams))
	for _, entry := range view.Teams {
		entries = append(entries, teamSuggestion(entry))
	}
	service.teamLibrarySnapshots.store(sessionID, entries)
	return entries
}

// teamSuggestion 把一条库条目投影成候选项。
//
// Text 用 team_id 而不是展示名：库里查询 id/名字/kind 都能命中（见
// teamSummonIndex.match），但 id 唯一、且不会像用户起的中文名那样含空格——候选项被
// 前端原样插进输入框（`@<text> ` 再续写附言），带空格的 Text 会让输入框在附言还没
// 写之前就进入参数区（于是面板自己消失）。
//
// 展示名不另造一条候选（那就成了"同一支团队两行、两行 Text 不同"，选中哪条都不确定），
// 而是进 alias 这个补充过滤键：打 `@改良`（名字前缀）照样弹出这一行，按 Tab/回车插
// 进输入框的仍是 team_id。按名召唤本身一直都在（resolveTeamSummon 认 id / 名字 /
// kind，不区分大小写），`@` 空参的回执也照样列名字——面板只是不再要求用户先把名字
// 打全。
//
// Description 放"展示名 + 员工数"：展示名与 id 相同就没额外信息，故省略；员工数来自
// 库条目自带的角色清单（不需要再读会话），是"这支多大"的唯一低成本事实。
func teamSuggestion(entry dto.TeamLibraryEntry) Suggestion {
	description := strings.TrimSpace(entry.Name)
	if description == entry.TeamID {
		description = ""
	}
	if count := len(entry.Roles); count > 0 {
		if description != "" {
			description += " · "
		}
		description += fmt.Sprintf("%d 个员工", count)
	}
	suggestion := Suggestion{Text: entry.TeamID, Description: description, Kind: SuggestionKindTeam}
	// 名字与 team_id 同值时 Text 已经覆盖，不必再匹配一遍；只有"用户另起了名字"
	// 才多出这一个可命中的键。
	if name := strings.TrimSpace(entry.Name); name != "" && name != entry.TeamID {
		suggestion.alias = name
	}
	return suggestion
}
