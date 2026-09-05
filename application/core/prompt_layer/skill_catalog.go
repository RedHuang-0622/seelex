package prompt_layer

import (
	"sort"
	"strings"

	"github.com/RedHuang-0622/seelex/application/model"
)

// skillCatalogHeader 是"可用技能"被动目录段的标题。
//
// 职责边界：目录只宣告当前激活插件里【可激活】的技能（name + description），
// 永不携带指令正文（Prompt）——"发现不泄全文"；正文仍只在用户 #<name>
// 激活后经 Trusted Active Skill 段注入。
const skillCatalogHeader = "## Available Skills"

// skillCatalogActivationHint 是目录尾的激活纪律：目录只宣告可激活技能，
// 激活入口是模型 skill_activate 工具（或用户输入 #<name>）；未激活不得声称生效。
const skillCatalogActivationHint = "When this task matches a skill below, activate it with the skill_activate tool (or ask the user to send #<name>); only then are its contents injected as a Trusted Active Skill for the task. The listed set belongs to the active plugin and changes when you switch plugins. Never claim an unactivated skill is in effect."

// RenderSkillCatalog 把当前插件的技能清单渲染为字节稳定的目录段（被动技能
// 的原子单元：[]model.SkillInfo → string，纯函数，无 I/O、无状态）：
//
//   - 无技能/全空名 → 返回 ""（不占 system 字节）；
//   - 按 name 排序，同输入恒同输出（插件不变则目录段字节不变 → 前缀缓存友好）；
//   - 每项一行 "- <name>: <description>"（description 为空则仅 "- <name>"）；
//   - 只消费 Name/Description，Prompt 永不进入本段。
func RenderSkillCatalog(skills []model.SkillInfo) string {
	kept := make([]model.SkillInfo, 0, len(skills))
	for _, item := range skills {
		if name := strings.TrimSpace(item.Name); name != "" {
			item.Name = name
			item.Description = strings.TrimSpace(item.Description)
			kept = append(kept, item)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].Name < kept[j].Name })

	var b strings.Builder
	b.WriteString(skillCatalogHeader)
	for _, item := range kept {
		b.WriteString("\n- ")
		b.WriteString(item.Name)
		if item.Description != "" {
			b.WriteString(": ")
			b.WriteString(item.Description)
		}
	}
	b.WriteString("\n")
	b.WriteString(skillCatalogActivationHint)
	return b.String()
}
