package core

import (
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	skillContextEnvelopePrefix = "<!-- seelex:skill-context:v1 display="
	skillContextEnvelopeSuffix = " -->"
)

type chatRequest struct {
	displayInput string
	modelInput   string
	skills       []PromptLayer
	requirePlan  bool
	budget       ReActBudget
}

func chatRequestDisplays(requests []chatRequest) []string {
	displays := make([]string, len(requests))
	for index, request := range requests {
		displays[index] = request.displayInput
	}
	return displays
}

func newChatRequest(input string, layers []PromptLayer) chatRequest {
	display := strings.TrimSpace(input)
	return chatRequest{displayInput: display, modelInput: display, skills: selectedSkillLayers(layers)}
}

func selectedSkillLayers(layers []PromptLayer) []PromptLayer {
	skilled := make([]PromptLayer, 0, len(layers))
	for _, layer := range layers {
		if layer.Kind == "skill" {
			skilled = append(skilled, layer)
		}
	}
	return skilled
}

func formatSkillUserInput(layers []PromptLayer, input string) string {
	_ = layers
	return input
}

func writeSkillItem(builder *strings.Builder, skill PromptLayer) {
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
	if strings.HasPrefix(modelInput, preflightPlanAuthorityPrefix) {
		if _, original, ok := strings.Cut(modelInput, preflightPlanAuthorityRequestDelimiter); ok {
			return displayUserInput(original)
		}
		return ""
	}
	if strings.HasPrefix(modelInput, contextRecoveryPrefix) || strings.HasPrefix(modelInput, providerRecoveryPrefix) {
		if _, original, ok := strings.Cut(modelInput, contextRecoveryRequestDelimiter); ok {
			return displayUserInput(original)
		}
		return ""
	}
	if isTaskContextCheckpoint(modelInput) || modelInput == reactBudgetFinalizationInput {
		return ""
	}
	display, _, ok := parseModelEnvelope(modelInput)
	if !ok {
		return modelInput
	}
	return display
}

func combineChatRequests(requests []chatRequest) chatRequest {
	displays := make([]string, 0, len(requests))
	models := make([]string, 0, len(requests))
	decorated := false
	combined := chatRequest{}
	for _, request := range requests {
		displays = append(displays, request.displayInput)
		combined.requirePlan = combined.requirePlan || request.requirePlan
		_, bodyOffset, ok := parseModelEnvelope(request.modelInput)
		if ok {
			decorated = true
			models = append(models, request.modelInput[bodyOffset:])
		} else {
			models = append(models, request.modelInput)
		}
	}
	display := strings.Join(displays, "\n---\n")
	model := strings.Join(models, "\n---\n")
	if decorated {
		model = wrapModelInput(display, model)
	}
	combined.displayInput = display
	combined.modelInput = model
	for _, request := range requests {
		combined.skills = mergeSkillLayers(combined.skills, request.skills)
	}
	return combined
}

func mergeSkillLayers(current, incoming []PromptLayer) []PromptLayer {
	for _, layer := range incoming {
		found := false
		for index := range current {
			if current[index].Name == layer.Name {
				current[index] = layer
				found = true
				break
			}
		}
		if !found {
			current = append(current, layer)
		}
	}
	return current
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
