package promptassets

import (
	"strings"
	"testing"
)

func TestSystemAssetsContainEvidenceAndAuthorityRules(t *testing.T) {
	instructions := SystemInstructions()
	for _, required := range []string{"Evidence Before Conclusions", "Hypothesis", "authority=preflight-loaded", "truncated tool output as", "Task Terminal Protocol", "task_complete", "task_needs_user_decision", "task_failed"} {
		if !strings.Contains(instructions, required) {
			t.Fatalf("system instructions missing %q", required)
		}
	}
}

func TestPlanTemplatesRenderRuntimeFacts(t *testing.T) {
	data := PlanData{
		Effort: "medium", NodeLimit: "at most 4 nodes",
		Topology: "one serial chain", Concurrency: "at most 1 node concurrently",
		Verification: "include a verification node",
	}
	for name, rendered := range map[string]string{
		"preflight": PlanPreflight(data),
		"replan":    PlanReplan(data),
	} {
		for _, expected := range []string{"Effort: `medium`", "at most 4 nodes", "one serial chain", "at most 1 node concurrently"} {
			if !strings.Contains(rendered, expected) {
				t.Fatalf("%s template missing %q: %s", name, expected, rendered)
			}
		}
	}
}

func TestPlanActPromptHarness(t *testing.T) {
	if err := ValidatePlanActHarness(); err != nil {
		t.Fatal(err)
	}
}
