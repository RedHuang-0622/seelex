package conversation

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	session "github.com/RedHuang-0622/seelex/middle/switch/sessionusecase"
)

func TestReasoningProjectionPairsVisibleSteps(t *testing.T) {
	history := []contract.EngineMessage{
		{Role: "assistant", Content: "plan", ReasoningContent: "first", ToolCalls: []contract.EngineToolCall{{ID: "call-1"}}},
		{Role: "assistant", Content: "final", ReasoningContent: "second"},
	}
	view := &session.View{Conversation: []model.Message{
		{ID: "m1", Role: "assistant", Content: "plan"},
		{ID: "t1", Role: "tool", Tool: &model.ToolCall{ID: "call-1"}},
		{ID: "m2", Role: "assistant", Content: "final"},
	}}
	deltas := AttachStepReasoningLocked(view, ReasoningStepsFromHistory(history))
	if len(deltas) != 2 || view.Conversation[0].ReasoningContent != "first" || view.Conversation[2].ReasoningContent != "second" {
		t.Fatalf("attached = %+v view=%+v", deltas, view.Conversation)
	}
	if LastReasoningFromHistory(history) != "second" {
		t.Fatalf("last reasoning = %q", LastReasoningFromHistory(history))
	}
}

func TestAttachLastReasoningFallback(t *testing.T) {
	view := &session.View{Conversation: []model.Message{
		{ID: "m1", Role: "assistant", Content: "reply"},
	}}
	delta, attached := AttachLastReasoningLocked(view, "fallback")
	if !attached || delta.MessageID != "m1" || view.Conversation[0].ReasoningContent != "fallback" {
		t.Fatalf("fallback = %+v attached=%v view=%+v", delta, attached, view.Conversation)
	}
}
