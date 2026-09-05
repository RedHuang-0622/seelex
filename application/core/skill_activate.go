package core

import (
	"errors"
	"fmt"
	"strings"
)

// ActivateSkill 把当前插件的一个技能激活进当前会话的 skill 层（模型侧
// skill_activate 工具的后端）。语义与用户输入 #<name> 的 applySkill 一致：
//
//   - 查询当前激活插件的技能表（Deps.Skills 端口 → skill.Registry 按
//     activePlugin 隔离），技能列表随 switch_plugin 切换；
//   - 命中后把该技能作为 kind=skill 层压入进程级 promptStack（同名覆盖，
//     幂等），后续 submitConversation 会经 newChatRequest(promptStack.Layers())
//     自动带进任务的 TrustedSkillLayers（S2 内容注入），ActivateTaskSkillsLocked
//     把正文作为 internal user 事件 append-only 落进 transcript（## Trusted
//     Active Skill 段，见 task_context.ensureActiveSkillEventsLocked）——装配随
//     已定稿轮次携带，不再进 system；
//   - 工具本轮直接拿到技能正文（main.go 回传给模型），因此模型在同一
//     ReAct 循环内即可按技能继续执行——即 Codex "激活后按技能继续" 的语义。
//
// 安全边界：本方法只改 promptStack 与会话通知，不触碰引擎锁（运行中会话
// 的 SetSystemPromptFor 会阻塞，见 hot_attach 注释），也不提交新对话。
func (service *Service) ActivateSkill(name string) (SkillInfo, error) {
	if service == nil {
		return SkillInfo{}, errors.New("service is not initialized")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return SkillInfo{}, errors.New("skill name is required")
	}
	if name == "goal" {
		return SkillInfo{}, fmt.Errorf("skill %q is not model-activatable", name)
	}
	skill, ok := service.Deps.Skills.Get(name)
	if !ok {
		return SkillInfo{}, fmt.Errorf("unknown skill %q in the active plugin", name)
	}
	service.applySkill(skill)
	return skill, nil
}
