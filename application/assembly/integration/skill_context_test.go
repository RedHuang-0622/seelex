package integration

import (
	assembly "github.com/RedHuang-0622/seelex/application/assembly"
	"github.com/RedHuang-0622/seelex/application/session/history"
	"github.com/RedHuang-0622/seelex/contract"
	"testing"
)

func TestDisplayUserInputRejectsInvalidEnvelope(t *testing.T) {
	const input = "<!-- seelex:skill-context:v1 display=invalid! -->\nbody"
	if got := assembly.DisplayUserInput(input); got != input {
		t.Fatalf("invalid envelope changed to %q", got)
	}
}

// TestAdaptEngineMessageRestoresOriginalUserInput 走会话历史的唯一判据
// （application/session/history.Runtime.AdaptEngineMessage）：user 行必须经装配
// 注入的 display 归一化，把模型输入里的 skill 信封还原成用户原文。
func TestAdaptEngineMessageRestoresOriginalUserInput(t *testing.T) {
	const input = "$review 检查这个实现"
	modelInput := assembly.WrapModelInput(input, input)
	history := sessionhistory.NewRuntime(sessionhistory.RuntimeDeps{DisplayInput: assembly.DisplayUserInput})
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
	if got := assembly.DisplayUserInput(recovery); got != "continue audit" {
		t.Fatalf("recovery display = %q", got)
	}
	history := sessionhistory.NewRuntime(sessionhistory.RuntimeDeps{DisplayInput: assembly.DisplayUserInput})
	if history.IsVisibleHistoryMessage(contract.EngineMessage{Role: "system", Content: "assembled system prompt"}) {
		t.Fatal("system history must never be visible in a frontend snapshot")
	}
	hidden := contract.EngineMessage{Role: "user", Content: providerRecoveryPrefix + "\nprivate checkpoint"}
	if assembly.DisplayUserInput(hidden.Content) != "" {
		t.Fatalf("私有恢复信封未折叠成空：%q", assembly.DisplayUserInput(hidden.Content))
	}
	if history.IsVisibleHistoryMessage(hidden) {
		t.Fatal("归一化后正文为空的 user 行不得出现在可见历史里")
	}
}
