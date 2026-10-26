package adapters

import (
	"testing"

	"github.com/RedHuang-0622/seelex/backend/engine/skill"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

type skillLookupProbe struct{}

func (skillLookupProbe) Get(name string) (model.SkillInfo, bool) {
	return model.SkillInfo{Name: name, Description: "test"}, name == "review"
}

func (skillLookupProbe) All() []model.SkillInfo {
	return []model.SkillInfo{{Name: "review", Description: "test"}}
}

func TestSkillActivationPortMatchesEngineActivator(t *testing.T) {
	portResult, portErr := NewSkillActivationPort().Activate(" review ", skillLookupProbe{}, nil)
	engineResult, engineErr := skill.NewActivator(skill.ActivateDeps{Skills: skillLookupProbe{}}).Activate(" review ")
	if portResult != engineResult || (portErr == nil) != (engineErr == nil) {
		t.Fatalf("activation mismatch: got=(%+v,%v), want=(%+v,%v)",
			portResult, portErr, engineResult, engineErr)
	}
}

var _ contract.SkillPort = skillLookupProbe{}
