package conversation

import (
	"strings"

	session "github.com/RedHuang-0622/seelex/application/session/state"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/event"
)

// ReasoningStep is one assistant reasoning unit paired with its tool calls.
type ReasoningStep struct {
	Content   string
	CallIDs   map[string]bool
	Reasoning string
}

// ReasoningStepsFromHistory extracts assistant reasoning in history order.
func ReasoningStepsFromHistory(history []contract.EngineMessage) []ReasoningStep {
	steps := make([]ReasoningStep, 0, len(history))
	for _, message := range history {
		if message.Role != "assistant" || strings.TrimSpace(message.ReasoningContent) == "" {
			continue
		}
		step := ReasoningStep{
			Content:   strings.TrimSpace(message.Content),
			Reasoning: message.ReasoningContent,
			CallIDs:   make(map[string]bool, len(message.ToolCalls)),
		}
		for _, call := range message.ToolCalls {
			step.CallIDs[call.ID] = true
		}
		steps = append(steps, step)
	}
	return steps
}

// LastReasoningFromHistory returns the last assistant reasoning text.
func LastReasoningFromHistory(history []contract.EngineMessage) string {
	for index := len(history) - 1; index >= 0; index-- {
		if history[index].Role == "assistant" && history[index].ReasoningContent != "" {
			return history[index].ReasoningContent
		}
	}
	return ""
}

// AttachStepReasoningLocked attaches each visible assistant step to its own
// reasoning and returns only newly written deltas.
func AttachStepReasoningLocked(view *session.View, steps []ReasoningStep) []event.MessageDelta {
	var attached []event.MessageDelta
	for index := range view.Conversation {
		message := &view.Conversation[index]
		if message.Role != "assistant" || message.Tool != nil || message.ReasoningContent != "" {
			continue
		}
		match := takeReasoningStep(steps, message.Content, visibleStepCallIDs(view.Conversation, index))
		if match < 0 {
			continue
		}
		step := steps[match]
		steps = append(steps[:match], steps[match+1:]...)
		message.ReasoningContent = step.Reasoning
		attached = append(attached, event.MessageDelta{MessageID: message.ID, ReasoningContent: step.Reasoning})
	}
	return attached
}

func visibleStepCallIDs(conversation []model.Message, index int) map[string]bool {
	ids := map[string]bool{}
	for next := index + 1; next < len(conversation); next++ {
		message := conversation[next]
		if message.Role != "tool" || message.Tool == nil {
			break
		}
		ids[message.Tool.ID] = true
	}
	return ids
}

func takeReasoningStep(steps []ReasoningStep, content string, ids map[string]bool) int {
	trimmed := strings.TrimSpace(content)
	for index := range steps {
		step := steps[index]
		if len(step.CallIDs) != len(ids) {
			continue
		}
		matched := true
		for id := range ids {
			if !step.CallIDs[id] {
				matched = false
				break
			}
		}
		if !matched {
			continue
		}
		if len(ids) > 0 {
			if step.Content != "" && step.Content != trimmed {
				continue
			}
			return index
		}
		if trimmed != step.Content {
			continue
		}
		return index
	}
	return -1
}

// AttachLastReasoningLocked is the legacy fallback when no step matches.
func AttachLastReasoningLocked(view *session.View, reasoning string) (event.MessageDelta, bool) {
	if reasoning == "" {
		return event.MessageDelta{}, false
	}
	for index := len(view.Conversation) - 1; index >= 0; index-- {
		message := &view.Conversation[index]
		if message.Role != "assistant" || message.Tool != nil {
			continue
		}
		if message.ReasoningContent == reasoning {
			return event.MessageDelta{}, false
		}
		message.ReasoningContent = reasoning
		return event.MessageDelta{MessageID: message.ID, ReasoningContent: reasoning}, true
	}
	return event.MessageDelta{}, false
}
