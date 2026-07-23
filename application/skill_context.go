package application

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
	return chatRequest{displayInput: display, modelInput: formatSkillUserInput(layers, display)}
}

func formatSkillUserInput(layers []PromptLayer, input string) string {
	skills := make([]PromptLayer, 0, len(layers))
	for _, layer := range layers {
		if layer.Kind == "skill" {
			skills = append(skills, layer)
		}
	}
	if len(skills) == 0 {
		return input
	}

	var builder strings.Builder
	builder.WriteString("## Selected Skills\n")
	for _, skill := range skills {
		writeSkillItem(&builder, skill)
	}
	builder.WriteString("\n## User Request\n")
	builder.WriteString(input)
	return wrapModelInput(input, builder.String())
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
	for _, request := range requests {
		displays = append(displays, request.displayInput)
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
	return chatRequest{displayInput: display, modelInput: model}
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
