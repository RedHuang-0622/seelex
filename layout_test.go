package main

import (
	"testing"

	"github.com/RedHuang-0622/seelex/plugin"
	"github.com/RedHuang-0622/seelex/skill"
)

func TestApprovalAccepted(t *testing.T) {
	tests := map[string]bool{
		"Yes":        true,
		"confirm":    true,
		"No":         false,
		"deny":       false,
		"拒绝":         false,
		"__CANCEL__": false,
	}
	for optionID, expected := range tests {
		if actual := approvalAccepted(optionID); actual != expected {
			t.Fatalf("approvalAccepted(%q) = %v, want %v", optionID, actual, expected)
		}
	}
}

func TestRepositorySkillAndPluginLayouts(t *testing.T) {
	skills, err := skill.NewLoader("skills").LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 9 {
		t.Fatalf("loaded %d global skills, want 9", len(skills))
	}
	plugins, err := plugin.NewLoader("plugins").LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 7 {
		t.Fatalf("loaded %d plugins, want 7 (6 original + freecad)", len(plugins))
	}
}
