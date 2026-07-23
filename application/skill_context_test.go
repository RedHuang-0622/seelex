package application

import (
	"strings"
	"testing"
)

func TestFormatSkillUserInputKeepsPlainInputUnchanged(t *testing.T) {
	const input = "检查这个实现"
	if got := formatSkillUserInput(nil, input); got != input {
		t.Fatalf("plain input = %q, want %q", got, input)
	}
}

func TestFormatSkillUserInputCreatesItemizedContext(t *testing.T) {
	layers := []PromptLayer{
		{Kind: "base", Name: "base", Text: "system"},
		{Kind: "skill", Name: "review", Text: "Check correctness.\nReport evidence."},
		{Kind: "skill", Name: "security", Text: "Inspect inputs."},
	}
	const input = "#review 检查这个实现"
	modelInput := formatSkillUserInput(layers, input)

	for _, expected := range []string{
		"## Selected Skills",
		"- name: review\n  instructions: |\n    Check correctness.\n    Report evidence.",
		"- name: security",
		"## User Request\n" + input,
	} {
		if !strings.Contains(modelInput, expected) {
			t.Fatalf("model input missing %q:\n%s", expected, modelInput)
		}
	}
	if got := displayUserInput(modelInput); got != input {
		t.Fatalf("display input = %q, want %q", got, input)
	}
}

func TestDisplayUserInputRejectsInvalidEnvelope(t *testing.T) {
	const input = "<!-- seelex:skill-context:v1 display=invalid! -->\nbody"
	if got := displayUserInput(input); got != input {
		t.Fatalf("invalid envelope changed to %q", got)
	}
}

func TestAdaptEngineMessageRestoresOriginalUserInput(t *testing.T) {
	const input = "#review 检查这个实现"
	modelInput := formatSkillUserInput([]PromptLayer{{Kind: "skill", Name: "review", Text: "review prompt"}}, input)
	adapted := adaptEngineMessage(EngineMessage{Role: "user", Content: modelInput})
	if adapted.Content != input {
		t.Fatalf("adapted content = %q, want %q", adapted.Content, input)
	}
}

func TestCombineChatRequestsPreservesDisplayAndSkillBodies(t *testing.T) {
	first := newChatRequest("plain", nil)
	second := newChatRequest("#review focused", []PromptLayer{{Kind: "skill", Name: "review", Text: "review prompt"}})
	combined := combineChatRequests([]chatRequest{first, second})

	if got, want := combined.displayInput, "plain\n---\n#review focused"; got != want {
		t.Fatalf("display = %q, want %q", got, want)
	}
	if got := displayUserInput(combined.modelInput); got != combined.displayInput {
		t.Fatalf("decoded display = %q, want %q", got, combined.displayInput)
	}
	if strings.Count(combined.modelInput, skillContextEnvelopePrefix) != 1 {
		t.Fatalf("combined input must have one outer envelope:\n%s", combined.modelInput)
	}
	for _, expected := range []string{"plain", "- name: review", "#review focused"} {
		if !strings.Contains(combined.modelInput, expected) {
			t.Fatalf("combined model input missing %q:\n%s", expected, combined.modelInput)
		}
	}
}
