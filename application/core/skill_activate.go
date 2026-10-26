package core

// skill_activate.go — 「模型侧激活技能」落点的**宿主装配 + 门面转发**。
//
// 判据（名字校验 / 可激活性 / 命中查询与错误文案）已在 M5-R-skill 随接收者转移归位
// `backend/engine/skill.Activator`（plan §2.5）；本文件只剩门面的两件事：
// nil 守卫与把宿主面（技能表端口 + 落地动作 applySkill）交给落点。
//
// 安全边界（历史结论，逐字保留）：本路径只改 promptStack 与会话通知，不触碰引擎锁
// （运行中会话的 SetSystemPromptFor 会阻塞），也不提交新对话。

import "github.com/RedHuang-0622/seelex/middle/protocol/model"

// ActivateSkill 把当前插件的一个技能激活进当前会话的 skill 层（模型侧 `skill_activate`
// 工具的后端）。判据与文案在 `backend/engine/skill.Activator`；本方法只转发。
func (service *Service) ActivateSkill(name string) (model.SkillInfo, error) {
	return service.policyApplicationComponent().ActivateSkill(name)
}
