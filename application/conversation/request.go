package conversation

import (
	"strings"

	"github.com/RedHuang-0622/seelex/application/prompt"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
)

// Request is the immutable accepted-turn payload.
type Request struct {
	DisplayInput string
	ModelInput   string
	Skills       []prompt.PromptLayer
	Budget       prompt.ReActBudget
}

// RequestDeps supplies model-envelope parsing and wrapping rules owned by the
// existing Skill context path.
type RequestDeps struct {
	ParseModelEnvelope func(input string) (display string, bodyOffset int, ok bool)
	WrapModelInput     func(display, body string) string
}

// RequestApplication owns request construction and queue-batch merging.
type RequestApplication struct {
	deps RequestDeps
}

// NewRequestApplication constructs the request factory.
func NewRequestApplication(deps RequestDeps) *RequestApplication {
	return &RequestApplication{deps: deps}
}

// NewRequest freezes one accepted input and its Skill layers.
func (*RequestApplication) NewRequest(input string, layers []prompt.PromptLayer) Request {
	display := strings.TrimSpace(input)
	return Request{
		DisplayInput: display,
		ModelInput:   display,
		Skills:       SelectedSkillLayers(layers),
	}
}

// QueuedRequests restores valid request payloads from opaque queue rows.
func (*RequestApplication) QueuedRequests(requests []sessioncase.QueuedRequest) []Request {
	out := make([]Request, 0, len(requests))
	for _, request := range requests {
		if payload, ok := request.Payload.(Request); ok {
			out = append(out, payload)
		}
	}
	return out
}

// Combine merges a promoted queue batch with the existing separator and Skill
// merge semantics.
func (application *RequestApplication) Combine(requests []Request) Request {
	displays := make([]string, 0, len(requests))
	models := make([]string, 0, len(requests))
	decorated := false
	combined := Request{}
	for _, request := range requests {
		displays = append(displays, request.DisplayInput)
		_, bodyOffset, ok := application.deps.ParseModelEnvelope(request.ModelInput)
		if ok {
			decorated = true
			models = append(models, request.ModelInput[bodyOffset:])
		} else {
			models = append(models, request.ModelInput)
		}
	}
	display := strings.Join(displays, "\n---\n")
	model := strings.Join(models, "\n---\n")
	if decorated {
		model = application.deps.WrapModelInput(display, model)
	}
	combined.DisplayInput = display
	combined.ModelInput = model
	for _, request := range requests {
		combined.Skills = mergeSkillLayers(combined.Skills, request.Skills)
	}
	return combined
}

// ChatRequestDisplays extracts display inputs.
func ChatRequestDisplays(requests []Request) []string {
	displays := make([]string, len(requests))
	for index, request := range requests {
		displays[index] = request.DisplayInput
	}
	return displays
}

// QueuedInputRefs extracts non-empty display inputs.
func QueuedInputRefs(queue []Request) []string {
	refs := make([]string, 0, len(queue))
	for _, request := range queue {
		if input := strings.TrimSpace(request.DisplayInput); input != "" {
			refs = append(refs, input)
		}
	}
	return refs
}

// SelectedSkillLayers filters frozen Skill layers.
func SelectedSkillLayers(layers []prompt.PromptLayer) []prompt.PromptLayer {
	skilled := make([]prompt.PromptLayer, 0, len(layers))
	for _, layer := range layers {
		if layer.Kind == "skill" {
			skilled = append(skilled, layer)
		}
	}
	return skilled
}

func mergeSkillLayers(current, incoming []prompt.PromptLayer) []prompt.PromptLayer {
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
