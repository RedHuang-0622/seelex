package scenario

import (
	"fmt"
	"regexp"
	"strings"
)

const SchemaVersion = "seelex.scenario/v1"

var scenarioIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type Scenario struct {
	SchemaVersion string       `json:"schema_version"`
	ID            string       `json:"id"`
	Initial       InitialState `json:"initial"`
	EngineScript  []EngineTurn `json:"engine_script"`
	Steps         []Step       `json:"steps"`
}

type InitialState struct {
	Plugin          string   `json:"plugin,omitempty"`
	Effort          string   `json:"effort,omitempty"`
	OpenSessionIDs  []string `json:"open_session_ids,omitempty"`
	ActiveSessionID string   `json:"active_session_id,omitempty"`
}

type EngineTurn struct {
	OnUser string     `json:"on_user"`
	Emit   []Emission `json:"emit"`
}

type Emission struct {
	Type      string        `json:"type"`
	Value     string        `json:"value,omitempty"`
	Name      string        `json:"name,omitempty"`
	Arguments string        `json:"arguments,omitempty"`
	Result    string        `json:"result,omitempty"`
	Error     string        `json:"error,omitempty"`
	Approval  *ApprovalSpec `json:"approval,omitempty"`
}

type ApprovalSpec struct {
	ID          string   `json:"id"`
	Question    string   `json:"question"`
	Risk        string   `json:"risk,omitempty"`
	Preview     string   `json:"preview,omitempty"`
	Options     []Option `json:"options"`
	AllowOption string   `json:"allow_option,omitempty"`
}

type Option struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Style       string `json:"style,omitempty"`
}

type Step struct {
	Action    string   `json:"action,omitempty"`
	Expect    string   `json:"expect,omitempty"`
	SessionID string   `json:"session_id,omitempty"`
	Text      string   `json:"text,omitempty"`
	ID        string   `json:"id,omitempty"`
	Option    string   `json:"option,omitempty"`
	Value     string   `json:"value,omitempty"`
	Running   *bool    `json:"running,omitempty"`
	Tool      string   `json:"tool,omitempty"`
	Status    string   `json:"status,omitempty"`
	Kinds     []string `json:"kinds,omitempty"`
}

func (scenario Scenario) Validate() error {
	if scenario.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version must be %q", SchemaVersion)
	}
	if !scenarioIDPattern.MatchString(scenario.ID) {
		return fmt.Errorf("id %q must match %s", scenario.ID, scenarioIDPattern)
	}
	if len(scenario.Initial.OpenSessionIDs) > 1 {
		return fmt.Errorf("minimal runner supports at most one open session")
	}
	if len(scenario.Initial.OpenSessionIDs) == 1 && scenario.Initial.ActiveSessionID != "" && scenario.Initial.OpenSessionIDs[0] != scenario.Initial.ActiveSessionID {
		return fmt.Errorf("active_session_id must match the only open_session_id")
	}
	if len(scenario.EngineScript) == 0 {
		return fmt.Errorf("engine_script is required")
	}
	for index, turn := range scenario.EngineScript {
		if err := turn.validate(); err != nil {
			return fmt.Errorf("engine_script[%d]: %w", index, err)
		}
	}
	if len(scenario.Steps) == 0 {
		return fmt.Errorf("steps is required")
	}
	for index, step := range scenario.Steps {
		if err := step.validate(); err != nil {
			return fmt.Errorf("steps[%d]: %w", index, err)
		}
	}
	return nil
}

func (turn EngineTurn) validate() error {
	if strings.TrimSpace(turn.OnUser) == "" {
		return fmt.Errorf("on_user is required")
	}
	if len(turn.Emit) == 0 {
		return fmt.Errorf("emit is required")
	}
	for index, emission := range turn.Emit {
		if err := emission.validate(); err != nil {
			return fmt.Errorf("emit[%d]: %w", index, err)
		}
	}
	return nil
}

func (emission Emission) validate() error {
	switch emission.Type {
	case "assistant.delta":
		if emission.Value == "" {
			return fmt.Errorf("assistant.delta requires value")
		}
	case "tool.call":
		if strings.TrimSpace(emission.Name) == "" {
			return fmt.Errorf("tool.call requires name")
		}
		if emission.Approval != nil {
			return emission.Approval.validate()
		}
	case "approval.request":
		if emission.Approval == nil {
			return fmt.Errorf("approval.request requires approval")
		}
		return emission.Approval.validate()
	case "engine.error":
		if emission.Error == "" {
			return fmt.Errorf("engine.error requires error")
		}
	default:
		return fmt.Errorf("unsupported emission type %q", emission.Type)
	}
	return nil
}

func (approval ApprovalSpec) validate() error {
	if strings.TrimSpace(approval.ID) == "" {
		return fmt.Errorf("approval id is required")
	}
	if strings.TrimSpace(approval.Question) == "" {
		return fmt.Errorf("approval question is required")
	}
	if len(approval.Options) == 0 {
		return fmt.Errorf("approval options are required")
	}
	for index, option := range approval.Options {
		if option.ID == "" || option.Label == "" {
			return fmt.Errorf("approval options[%d] requires id and label", index)
		}
	}
	return nil
}

func (step Step) validate() error {
	if (step.Action == "") == (step.Expect == "") {
		return fmt.Errorf("exactly one of action or expect is required")
	}
	if step.Action != "" {
		switch step.Action {
		case "submit":
			if strings.TrimSpace(step.Text) == "" {
				return fmt.Errorf("submit requires text")
			}
		case "resolve_interaction":
			if strings.TrimSpace(step.Option) == "" {
				return fmt.Errorf("resolve_interaction requires option")
			}
		default:
			return fmt.Errorf("unsupported action %q", step.Action)
		}
		return nil
	}
	switch step.Expect {
	case "chat_running":
		if step.Running == nil {
			return fmt.Errorf("chat_running requires running")
		}
	case "message_delta":
		if step.Value == "" {
			return fmt.Errorf("message_delta requires value")
		}
	case "tool_status":
		if step.Tool == "" || step.Status == "" {
			return fmt.Errorf("tool_status requires tool and status")
		}
	case "interaction":
		if step.ID == "" {
			return fmt.Errorf("interaction requires id")
		}
	case "event_sequence":
		if len(step.Kinds) == 0 {
			return fmt.Errorf("event_sequence requires kinds")
		}
	case "event_set":
		if len(step.Kinds) == 0 {
			return fmt.Errorf("event_set requires kinds")
		}
	default:
		return fmt.Errorf("unsupported expectation %q", step.Expect)
	}
	return nil
}
