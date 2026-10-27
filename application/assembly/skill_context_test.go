package assembly

import (
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/application/session/history"
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

// TestAdaptEngineMessageRestoresOriginalUserInput 走会话历史的唯一判据
// （application/session/history.Runtime.AdaptEngineMessage）：user 行必须经装配
// 注入的 display 归一化，把模型输入里的 skill 信封还原成用户原文。
func TestAdaptEngineMessageRestoresOriginalUserInput(t *testing.T) {
	const input = "$review 检查这个实现"
	modelInput := wrapModelInput(input, formatSkillUserInput([]prompt.PromptLayer{{Kind: "skill", Name: "review", Text: "review prompt"}}, input))
	history := sessionhistory.NewRuntime(sessionhistory.RuntimeDeps{DisplayInput: displayUserInput})
	adapted := history.AdaptEngineMessage(contract.EngineMessage{Role: "user", Content: modelInput})
	if adapted.Content != input {
		t.Fatalf("adapted content = %q, want %q", adapted.Content, input)
	}
}

// TestDisplayUserInputHidesPrivateRecoveryEnvelopes 钉住「显示归一化」与「历史可见性」
// 是同一套判据：私有恢复信封折叠成空后，该行既不能显示正文，也不能作为空壳行留在
// 可见历史里（可见性只按 role 判会把空壳行留下）。
func TestDisplayUserInputHidesPrivateRecoveryEnvelopes(t *testing.T) {
	recovery := providerRecoveryPrefix + "\nprivate checkpoint" + contextRecoveryRequestDelimiter + "continue audit"
	if got := displayUserInput(recovery); got != "continue audit" {
		t.Fatalf("recovery display = %q", got)
	}
	history := sessionhistory.NewRuntime(sessionhistory.RuntimeDeps{DisplayInput: displayUserInput})
	if history.IsVisibleHistoryMessage(contract.EngineMessage{Role: "system", Content: "assembled system prompt"}) {
		t.Fatal("system history must never be visible in a frontend snapshot")
	}
	hidden := contract.EngineMessage{Role: "user", Content: providerRecoveryPrefix + "\nprivate checkpoint"}
	if displayUserInput(hidden.Content) != "" {
		t.Fatalf("私有恢复信封未折叠成空：%q", displayUserInput(hidden.Content))
	}
	if history.IsVisibleHistoryMessage(hidden) {
		t.Fatal("归一化后正文为空的 user 行不得出现在可见历史里")
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
