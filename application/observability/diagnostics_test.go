package observability

import (
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func TestRenderDiagReadsRuntimeReadings(t *testing.T) {
	snapshot := model.Snapshot{
		Runtime: model.RuntimeState{
			Provider: "provider-x",
			Model:    "model-y",
			Effort:   "high",
			Tokens:   "1.2k",
			Account:  "acct-1",
			Plugin:   "plug-a",
			Plan: &model.PlanState{
				Name:   "plan-a",
				Status: model.PlanRunning,
				Nodes: []model.PlanNode{
					{Status: model.NodeCompleted},
					{Status: model.NodeSkipped},
					{Status: model.NodePending},
				},
			},
			Plugins:  []model.PluginInfo{{Name: "plug-a", Description: "A"}},
			Accounts: []model.AccountInfo{{Name: "acct-1", Provider: "provider-x", Model: "model-y"}},
			Skills:   []model.SkillInfo{{Name: "skill-a"}},
		},
	}
	text := RenderDiag(snapshot)
	for _, want := range []string{
		"Provider: provider-x", "Plan: plan-a", "2/3 nodes",
		"Active: plug-a", "acct-1", "skill-a",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("RenderDiag output missing %q\n---\n%s", want, text)
		}
	}
}

func TestRenderDiagWithoutRuntimeSections(t *testing.T) {
	text := RenderDiag(model.Snapshot{})
	for _, want := range []string{"Active: none (0 total)", "0 configured", "0 loaded"} {
		if !strings.Contains(text, want) {
			t.Errorf("RenderDiag output missing %q\n---\n%s", want, text)
		}
	}
	if strings.Contains(text, "Plan:") {
		t.Errorf("RenderDiag printed a plan section without a plan:\n%s", text)
	}
}
