package assembly

import (
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/contract"
	"strings"
	"testing"
)

func TestFormatSkillUserInputKeepsPlainInputUnchanged(t *testing.T) {
	const input = "检查这个实现"
	if got := formatSkillUserInput(nil, input); got != input {
		t.Fatalf("plain input = %q, want %q", got, input)
	}
}

func TestSkillSelectionKeepsUserInputPlainAndFreezesTrustedLayers(t *testing.T) {
	layers := []prompt.PromptLayer{
		{Kind: "base", Name: "base", Text: "system"},
		{Kind: "skill", Name: "review", Text: "Check correctness.\nReport evidence."},
		{Kind: "skill", Name: "security", Text: "Inspect inputs."},
	}
	const input = "$review 检查这个实现"
	modelInput := formatSkillUserInput(layers, input)
	if modelInput != input {
		t.Fatalf("trusted Skill text leaked into user input: %q", modelInput)
	}
	selected := selectedSkillLayers(layers)
	if len(selected) != 2 || selected[0].Name != "review" || selected[1].Name != "security" {
		t.Fatalf("selected Skills = %#v", selected)
	}
}

func TestDisplayUserInputRejectsInvalidEnvelope(t *testing.T) {
	const input = "<!-- seelex:skill-context:v1 display=invalid! -->\nbody"
	if got := displayUserInput(input); got != input {
		t.Fatalf("invalid envelope changed to %q", got)
	}
}

func TestAdaptEngineMessageRestoresOriginalUserInput(t *testing.T) {
	const input = "$review 检查这个实现"
	modelInput := formatSkillUserInput([]prompt.PromptLayer{{Kind: "skill", Name: "review", Text: "review prompt"}}, input)
	adapted := adaptEngineMessage(contract.EngineMessage{Role: "user", Content: modelInput}, nil)
	if adapted.Content != input {
		t.Fatalf("adapted content = %q, want %q", adapted.Content, input)
	}
}

func TestDisplayUserInputHidesPrivateRecoveryEnvelopes(t *testing.T) {
	recovery := providerRecoveryPrefix + "\nprivate checkpoint" + contextRecoveryRequestDelimiter + "continue audit"
	if got := displayUserInput(recovery); got != "continue audit" {
		t.Fatalf("recovery display = %q", got)
	}
	if isVisibleHistoryMessage(contract.EngineMessage{Role: "system", Content: "assembled system prompt"}) {
		t.Fatal("system history must never be visible in a frontend snapshot")
	}
}

func TestCombineChatRequestsPreservesDisplayAndFrozenSkillLayers(t *testing.T) {
	turns := newConversationTurnState()
	first := turns.NewRequest("plain", nil)
	second := turns.NewRequest("$review focused", []prompt.PromptLayer{{Kind: "skill", Name: "review", Text: "review prompt"}})
	combined := turns.Combine([]chatRequest{first, second})

	if got, want := combined.DisplayInput, "plain\n---\n$review focused"; got != want {
		t.Fatalf("display = %q, want %q", got, want)
	}
	for _, expected := range []string{"plain", "$review focused"} {
		if !strings.Contains(combined.ModelInput, expected) {
			t.Fatalf("combined model input missing %q:\n%s", expected, combined.ModelInput)
		}
	}
	if strings.Contains(combined.ModelInput, "review prompt") || len(combined.Skills) != 1 || combined.Skills[0].Name != "review" {
		t.Fatalf("combined trusted Skills = %#v, input=%q", combined.Skills, combined.ModelInput)
	}
}
