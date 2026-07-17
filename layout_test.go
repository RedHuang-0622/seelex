package main

import (
	"testing"

	"github.com/RedHuang-0622/seelex/plugin"
	"github.com/RedHuang-0622/seelex/skill"
)

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
	if len(plugins) != 6 {
		t.Fatalf("loaded %d plugins, want 6", len(plugins))
	}
}
