package assembly

import (
	"encoding/base64"
	"fmt"
	"strings"

	context_runtime "github.com/RedHuang-0622/seelex/application/conversation/context"
	"github.com/RedHuang-0622/seelex/application/prompt"
	session_runtime "github.com/RedHuang-0622/seelex/application/session/runtime"
	"github.com/RedHuang-0622/seelex/contract/protocol"
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
	if strings.HasPrefix(modelInput, context_runtime.ContextRecoveryPrefix) || strings.HasPrefix(modelInput, context_runtime.ProviderRecoveryPrefix) {
		if _, original, ok := strings.Cut(modelInput, context_runtime.ContextRecoveryRequestDelimiter); ok {
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

// inputIndexUserText returns the display text for a user input row. Non-user
// rows and internal context markers return an empty string.
func inputIndexUserText(message model.Message) string {
	if message.Role != "user" {
		return ""
	}
	if strings.HasPrefix(message.Content, session_runtime.SessionArchiveResumePrefix) {
		return ""
	}
	if context_runtime.IsProviderOnlyHistoryContent(message.Content) {
		return ""
	}
	return strings.TrimSpace(displayUserInput(message.Content))
}
