package host

import (
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	"github.com/RedHuang-0622/seelex/backend/engine/skill"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

type skillActivationPort struct{}

var _ applicationpolicy.SkillActivationPort = skillActivationPort{}

// NewSkillActivationPort exposes the Engine skill activator through the
// application-owned policy port.
func NewSkillActivationPort() applicationpolicy.SkillActivationPort {
	return skillActivationPort{}
}

func (skillActivationPort) Activate(
	name string,
	skills contract.SkillPort,
	apply func(model.SkillInfo),
) (model.SkillInfo, error) {
	return skill.NewActivator(skill.ActivateDeps{Skills: skills, Apply: apply}).Activate(name)
}
