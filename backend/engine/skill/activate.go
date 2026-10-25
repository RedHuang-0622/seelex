package skill

// activate.go 是"模型侧激活技能"（`skill_activate` 工具的后端）的落点。
//
// 落点沿革：2026-10-16 随 M5-R-skill **接收者转移**从 `application/core/skill_activate.go`
// 归位（plan §2.5）。判据（名字校验 / 可激活性 / 命中查询）与错误文案只此一份；门面只留
// 宿主面装配（技能表端口 + 落地动作）与一行转发。
//
// 安全边界（与迁移前一致）：本落点只做判据，**不触碰引擎锁**（运行中会话的
// SetSystemPromptFor 会阻塞），也不提交新对话——落地动作由宿主注入。

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// SkillLookup 是"按名查技能"的窄读口（结构满足 `contract.SkillPort` 的 Get）。
type SkillLookup interface {
	Get(name string) (model.SkillInfo, bool)
}

// ActivateDeps 是激活落点需要的宿主面。
type ActivateDeps struct {
	// Skills 是当前激活插件的技能表（按 activePlugin 隔离，技能列表随 switch_plugin 切换）。
	Skills SkillLookup
	// Apply 是命中后的落地动作：把该技能作为 kind=skill 层压入进程级 promptStack（同名覆盖、
	// 幂等）并通知会话（后续提交会把它带进 TrustedSkillLayers，正文作为 internal user 事件
	// append-only 落进 transcript）。
	Apply func(model.SkillInfo)
}

// Activator 是激活落点本体（无状态）。
type Activator struct{ deps ActivateDeps }

// NewActivator 建落点。
func NewActivator(deps ActivateDeps) *Activator { return &Activator{deps: deps} }

// Activate 把当前插件的一个技能激活进当前会话的 skill 层。
//
// 语义与用户输入 `$<name>` 的 applySkill 一致：
//   - 查询当前激活插件的技能表（`ActivateDeps.Skills` 端口 → 注册表按 activePlugin 隔离），
//     技能列表随 switch_plugin 切换；
//   - 命中后交给 `ActivateDeps.Apply` 落地（压层 + 会话通知），因此模型在同一 ReAct 循环内
//     即可按技能继续执行——即"激活后按技能继续"的语义；
//   - 空名、未知技能与不可激活技能（`goal`）一律在落地之前拒绝，错误文案逐字保留。
func (a *Activator) Activate(name string) (model.SkillInfo, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return model.SkillInfo{}, errors.New("skill name is required")
	}
	if name == "goal" {
		return model.SkillInfo{}, fmt.Errorf("skill %q is not model-activatable", name)
	}
	if a.deps.Skills == nil {
		return model.SkillInfo{}, errors.New("skill registry is not assembled")
	}
	found, ok := a.deps.Skills.Get(name)
	if !ok {
		return model.SkillInfo{}, fmt.Errorf("unknown skill %q in the active plugin", name)
	}
	if a.deps.Apply != nil {
		a.deps.Apply(found)
	}
	return found, nil
}
