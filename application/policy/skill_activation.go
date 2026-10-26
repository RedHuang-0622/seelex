package policy

import (
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// SkillActivationPort owns model-side skill activation decisions.
type SkillActivationPort interface {
	Activate(name string, skills contract.SkillPort, apply func(model.SkillInfo)) (model.SkillInfo, error)
}
