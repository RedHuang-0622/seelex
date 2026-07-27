package scenario

import (
	"strings"
	"testing"
)

func TestLoadApprovalChatFixture(t *testing.T) {
	value, err := LoadFile("../fixtures/approval-chat.json")
	if err != nil {
		t.Fatal(err)
	}
	if value.ID != "approval-chat" || len(value.Steps) != 10 {
		t.Fatalf("unexpected scenario: id=%q steps=%d", value.ID, len(value.Steps))
	}
}

func TestLoadManualPlanFixture(t *testing.T) {
	value, err := LoadFile("../fixtures/manual-plan.json")
	if err != nil {
		t.Fatal(err)
	}
	if value.ID != "manual-plan" || len(value.Steps) != 6 {
		t.Fatalf("unexpected scenario: id=%q steps=%d", value.ID, len(value.Steps))
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	_, err := Load(strings.NewReader(`{
  "schema_version":"seelex.scenario/v1",
  "id":"bad",
  "initial":{},
  "engine_script":[{"on_user":"hello","emit":[{"type":"assistant.delta","value":"hi","extra":true}]}],
  "steps":[{"action":"submit","text":"hello"}]
}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("error = %v, want unknown field", err)
	}
}

func TestLoadRejectsUnsupportedExpectation(t *testing.T) {
	_, err := Load(strings.NewReader(`{
  "schema_version":"seelex.scenario/v1",
  "id":"future-card",
  "initial":{},
  "engine_script":[{"on_user":"hello","emit":[{"type":"assistant.delta","value":"hi"}]}],
  "steps":[{"expect":"conversation_card","id":"card-1"}]
}`))
	if err == nil || !strings.Contains(err.Error(), "unsupported expectation") {
		t.Fatalf("error = %v, want unsupported expectation", err)
	}
}
