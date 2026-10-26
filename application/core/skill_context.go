package core

import (
	"encoding/base64"
	"fmt"
	"github.com/RedHuang-0622/seelex/application/prompt"
	"github.com/RedHuang-0622/seelex/backend/engine/context"
	"strings"
)

const (
	skillContextEnvelopePrefix = "<!-- seelex:skill-context:v1 display="
	skillContextEnvelopeSuffix = " -->"
)

func formatSkillUserInput(layers []prompt.PromptLayer, input string) string {
	_ = layers
	return input
}

func writeSkillItem(builder *strings.Builder, skill prompt.PromptLayer) {
	fmt.Fprintf(builder, "- name: %s\n  instructions: |\n", skill.Name)
	for _, line := range strings.Split(strings.TrimSpace(skill.Text), "\n") {
		builder.WriteString("    ")
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
}

func wrapModelInput(displayInput, body string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(displayInput))
	return skillContextEnvelopePrefix + encoded + skillContextEnvelopeSuffix + "\n" + body
}

func displayUserInput(modelInput string) string {
	if strings.HasPrefix(modelInput, contextRecoveryPrefix) || strings.HasPrefix(modelInput, providerRecoveryPrefix) {
		if _, original, ok := strings.Cut(modelInput, contextRecoveryRequestDelimiter); ok {
			return displayUserInput(original)
		}
		return ""
	}
	if context_runtime.IsTaskContextCheckpoint(modelInput) || modelInput == reactBudgetFinalizationInput {
		return ""
	}
	display, _, ok := parseModelEnvelope(modelInput)
	if !ok {
		return modelInput
	}
	return display
}

func parseModelEnvelope(input string) (string, int, bool) {
	lineEnd := strings.IndexByte(input, '\n')
	if lineEnd < 0 {
		return "", 0, false
	}
	header := input[:lineEnd]
	if !strings.HasPrefix(header, skillContextEnvelopePrefix) || !strings.HasSuffix(header, skillContextEnvelopeSuffix) {
		return "", 0, false
	}
	encoded := strings.TrimSuffix(strings.TrimPrefix(header, skillContextEnvelopePrefix), skillContextEnvelopeSuffix)
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", 0, false
	}
	return string(decoded), lineEnd + 1, true
}
