package promptassets

import (
	"strings"
	"testing"
)

func TestSystemAssetsContainEvidenceAndAuthorityRules(t *testing.T) {
	instructions := SystemInstructions()
	for _, required := range []string{"Evidence Before Conclusions", "Hypothesis", "Visible Intent Before Tools", "Before the first tool call in a distinct phase", "Optional WorkPlan", "not a mandatory preflight gate", "Use a Plan when:", "Do not use a Plan when:", "truncated tool output as", "Task Terminal Protocol", "task_complete", "task_needs_user_decision", "task_failed"} {
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

func TestPlanPreflightTeachesCanonicalDAGCorrections(t *testing.T) {
	preflight := PlanPreflight(PlanData{})
	for _, expected := range []string{
		"Optional Plan Selection and Compilation Contract",
		"Common Invalid Shapes and Their Corrections",
		"a bare edge list loses every edge source",
		"name each edge source in the `edges` object",
		"every planned node must be reachable from `entry`",
		"do not fabricate a one-node reply Plan for a greeting",
		"limited\ncompatibility for legacy array-shaped input",
	} {
		if !strings.Contains(preflight, expected) {
			t.Fatalf("preflight prompt missing correction %q", expected)
		}
	}
}

func TestPromptCompletionHarness(t *testing.T) {
	if err := ValidatePlanActHarness(); err != nil {
		t.Fatal(err)
	}
}
