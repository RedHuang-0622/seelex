package core

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core/agentteam"
)

// ── 输入前缀（sigil）契约 ──────────────────────────────────────────────
//
// 前缀与含义**一一对应**，不做跨域兜底猜测：面板、输入框内联建议与 Submit
// 路由共用这一张表（权威说明见 docs/gui/modules/shell-and-interactions.md）。
//
//	/  可执行入口：命令 + Skill（工具**不**列出）
//	#  切换 Plugin（含 off/none = 停用全部）
//	$  召回 Skill（激活到当前会话）
//	@  手动召唤团队：内置形态 + 团队库条目，装配到当前会话（可跟一句附言）
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
		all = append(all, teamPresetSuggestions()...)
	}
	lower := strings.ToLower(prefix)
	filtered := all[:0]
	for _, suggestion := range all {
		if lower == "" || strings.HasPrefix(strings.ToLower(suggestion.Text), lower) {
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
	if used != SigilTeam && isPresetTeam(name) {
		hints = append(hints, fmt.Sprintf("召唤团队用 %s%s", SigilTeam, name))
	}
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

// teamPresetSuggestions 列出可召唤的内置团队形态（goal-a2a / review-team /
// research-team）。
//
// 刻意**不**在这里读团队库：Suggestions 在 TUI 的 View() 渲染路径上，也在 GUI
// 每次输入事件上被调用，加一次文件读等于把 I/O 塞进渲染循环。库条目不做建议，
// 但在召唤时按 team_id / name 解析（见 input_team.go），因此"用户自己存的团队"
// 仍可直接召唤——这条取舍记在 application/core/README-input.md。
func teamPresetSuggestions() []Suggestion {
	specs := agentteam.Presets()
	suggestions := make([]Suggestion, 0, len(specs))
	for _, spec := range specs {
		suggestions = append(suggestions, Suggestion{
			Text: spec.TeamID, Description: teamSpecSummary(spec), Kind: SuggestionKindTeam,
		})
	}
	return suggestions
}

// isPresetTeam 报告名字是否命中内置团队形态（team_id 与 team_kind 同值）。
func isPresetTeam(name string) bool {
	for _, spec := range agentteam.Presets() {
		if strings.EqualFold(spec.TeamID, name) || strings.EqualFold(spec.TeamKind, name) {
			return true
		}
	}
	return false
}

// teamSpecSummary 用形态自身的事实拼一句摘要（不另写一份人为描述，避免与
// presets.go 的真值漂移）。
func teamSpecSummary(spec dto.TeamSpec) string {
	parts := make([]string, 0, 2)
	if len(spec.OrderRoles) > 0 {
		parts = append(parts, "顺序 "+strings.Join(spec.OrderRoles, "→"))
	}
	parts = append(parts, fmt.Sprintf("%d 个角色", len(spec.Roles)))
	return strings.Join(parts, " · ")
}
