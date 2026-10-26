package core

import (
	"context"
	"fmt"
	"strings"

	applicationinput "github.com/RedHuang-0622/seelex/application/input"
)

// ── 输入前缀（sigil）契约 ──────────────────────────────────────────────
//
// 前缀与含义**一一对应**，不做跨域兜底猜测：面板、输入框内联建议与 Submit
// 路由共用这一张表（权威说明见 docs/gui/modules/shell-and-interactions.md）。
//
//	/  可执行入口：命令 + Skill（工具**不**列出）
//	#  切换 Plugin（含 off/none = 停用全部）
//	$  召回 Skill（激活到当前会话）
//
// `@` 手动召唤团队**已退场**（2026-10-12 J-1a，用户拍板"直接删除"）：这个字符
// 让给文件指代（`@文件`），Agent Team 的召唤面 / 团队库 / 员工库 / 母本整条退场，
// 因此它不再是前缀、不再有建议面、也不再有 SigilTeam 常量。旧肌肉记忆打 `@` 时
// 不会再拿到"未知前缀"提示——`@` 不再是前缀，输入按普通对话下发。
//
// `@文件`（2026-10-12 J-1b）**仍然不是前缀，不在这张表里登记**：引用是**正文文本**
// 的一部分（工作区相对路径），前端负责给候选（已加载的工作区树，不逐键读盘）与渲染
// chip（同一枚 chip 两处宿主），提交仍走普通对话那条路（`SubmitToSession` + 显式视图
// 会话 ID）。选"前端输入增强"而不是"重新登记一个 sigil"的理由：
//  1. 引用必须**留在正文里**让 agent 看见（`@src/app.js` 就是文本），agent 据此自己
//     `read_file` / `grep`；sigil 的语义是"这条输入由路由器接管"，与"正文里带着路径"
//     互斥——登记成 sigil 反而要把路径从正文里搬进路由分支；
//  2. 这里**没有任何需要后端分派的行为**：不新增 RPC、不搬内容字节、不加能力声明，
//     登记一个只有前端在用的前缀就是纯复制；
//  3. 三入口同一载体（`@` / `+` / 拖拽）由此成立：三处只产出同一形状的路径文本，
//     路由侧一视同仁（都是普通对话）。
//
// 前端的引用规则与 chip 在 `gui/frontend/dist/file-reference.js`。
//
// `/` 只放"能从输入框直接执行"的东西。工具是模型侧的（由模型调用、经权限门），
// 因此既不进建议也不进路由：一个能力要让用户打 `/名字` 显式调用，前提是它已注册
// 成命令；没注册就没有这个入口，不是"入口藏在别处"。打错到工具名时由
// unknownCommandNotice 说清这一点并给出同名命令入口。
//
// 前缀契约调整过（# 从 Skill 改为 Plugin、$ 接管 Skill、@ 从 Plugin 改为召唤
// 团队），旧肌肉记忆打老前缀时给一句迁移提示（见 sigilMigrationHint），而不是
// 让用户对着"未知"猜该用哪个符号。
// sigils 是前缀的唯一枚举（顺序 = 解析顺序）。任何新增前缀都从这里生效：
// 建议面（Suggestions）与迁移提示按它推导，不在别处再写一份字面量。
var sigils = applicationinput.Sigils()

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
	if used != applicationinput.SigilPlugin && service.hasPlugin(name) {
		hints = append(hints, fmt.Sprintf("切换插件用 %s%s", applicationinput.SigilPlugin, name))
	}
	if used != applicationinput.SigilSkill {
		if _, ok := service.Deps.Skills.Get(name); ok {
			hints = append(hints, fmt.Sprintf("召回 Skill 用 %s%s", applicationinput.SigilSkill, name))
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
	notice := fmt.Sprintf("未知命令: %s。输入 %shelp 查看可用命令。", trimmed, applicationinput.SigilCommand)
	hints := make([]string, 0, 2)
	if service.visibleToolNamed(trimmed) {
		hints = append(hints, fmt.Sprintf("「%s」是模型侧工具（由模型调用、经权限门），不能从输入框直接执行", trimmed))
	}
	if counterpart := service.commandCounterpart(trimmed); counterpart != "" {
		hints = append(hints, fmt.Sprintf("你是想用 %s%s 吗？", applicationinput.SigilCommand, counterpart))
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

func (service *Service) commandSuggestions() []applicationinput.Suggestion {
	suggestions := make([]applicationinput.Suggestion, 0)
	for _, command := range service.commands.All() {
		suggestions = append(suggestions, applicationinput.Suggestion{
			Text: command.Name(), Description: command.Description(), Kind: applicationinput.SuggestionKindCommand,
		})
	}
	return suggestions
}

func (service *Service) skillSuggestions() []applicationinput.Suggestion {
	suggestions := make([]applicationinput.Suggestion, 0)
	for _, skill := range service.Deps.Skills.All() {
		suggestions = append(suggestions, applicationinput.Suggestion{
			Text: skill.Name, Description: skill.Description, Kind: applicationinput.SuggestionKindSkill,
		})
	}
	return suggestions
}

// pluginSuggestions 列出可切换插件；off 是"停用全部"的内置入口（与
// SwitchPlugin 的 off/none/空 三种写法同义，面板只暴露一个）。
func (service *Service) pluginSuggestions() []applicationinput.Suggestion {
	suggestions := make([]applicationinput.Suggestion, 0)
	for _, plugin := range service.Deps.Plugins.All() {
		suggestions = append(suggestions, applicationinput.Suggestion{
			Text: plugin.Name, Description: plugin.Description, Kind: applicationinput.SuggestionKindPlugin,
		})
	}
	return append(suggestions, applicationinput.Suggestion{
		Text: "off", Description: "停用所有插件", Kind: applicationinput.SuggestionKindPlugin,
	})
}
