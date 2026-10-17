package promptassets

import (
	"strings"
	"testing"
)

func TestSystemAssetsContainEvidenceAndAuthorityRules(t *testing.T) {
	instructions := SystemInstructions()
	for _, required := range []string{"Evidence Before Conclusions", "Hypothesis", "Visible Intent Before Tools", "Before the first tool call in a distinct phase", "Subagent Dispatch", "fork_subagents", "Plan tool family is retired", "truncated tool output as", "Task Terminal Protocol", "task_complete", "task_needs_user_decision", "task_failed", "Long-Running Commands", "Use `bash_bg` when:", "Do not use `bash_bg` when:"} {
		if !strings.Contains(instructions, required) {
			t.Fatalf("system instructions missing %q", required)
		}
	}
}

func TestPromptCompletionHarness(t *testing.T) {
	if err := ValidatePlanActHarness(); err != nil {
		t.Fatal(err)
	}
}
