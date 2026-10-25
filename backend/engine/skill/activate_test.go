package skill

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// fakeLookup 是最小的 SkillLookup 结构满足实现。
type fakeLookup struct {
	skills map[string]model.SkillInfo
}

func (lookup fakeLookup) Get(name string) (model.SkillInfo, bool) {
	info, ok := lookup.skills[name]
	return info, ok
}

// TestActivateRejectsBadNamesWithoutApplying：空名 / 不可激活（goal）/ 未知技能都在落地之前拒绝，
// 且**不调用**落地动作（否则会推出一个没命中的层）。
func TestActivateRejectsBadNamesWithoutApplying(t *testing.T) {
	applied := 0
	activator := NewActivator(ActivateDeps{
		Skills: fakeLookup{skills: map[string]model.SkillInfo{"review": {Name: "review"}}},
		Apply:  func(model.SkillInfo) { applied++ },
	})

	for _, tc := range []struct {
		name string
		want string
	}{
		{"   ", "skill name is required"},
		{"goal", `skill "goal" is not model-activatable`},
		{"nope", `unknown skill "nope" in the active plugin`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := activator.Activate(tc.name)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Activate(%q) err = %v, want %q", tc.name, err, tc.want)
			}
		})
	}
	if applied != 0 {
		t.Fatalf("被拒的名字不该触发落地，got %d 次", applied)
	}
}

// TestActivateNormalizesNameAndApplies：命中时名字先 trim，落地动作恰好一次，并回传技能信息。
func TestActivateNormalizesNameAndApplies(t *testing.T) {
	var applied []string
	activator := NewActivator(ActivateDeps{
		Skills: fakeLookup{skills: map[string]model.SkillInfo{"review": {Name: "review", Description: "d"}}},
		Apply:  func(info model.SkillInfo) { applied = append(applied, info.Name) },
	})

	info, err := activator.Activate(" review ")
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "review" || info.Description != "d" {
		t.Fatalf("应回传命中的技能信息，got %+v", info)
	}
	if len(applied) != 1 || applied[0] != "review" {
		t.Fatalf("落地动作应恰好一次，got %v", applied)
	}
}

// TestActivateWithoutRegistry：技能表未装配时给明确错误（不是静默无效）。
func TestActivateWithoutRegistry(t *testing.T) {
	if _, err := NewActivator(ActivateDeps{}).Activate("review"); err == nil ||
		err.Error() != "skill registry is not assembled" {
		t.Fatalf("未装配技能表时文案必须明确，got %v", err)
	}
}
