package scenario

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/RedHuang-0622/seelex/application"
)

type Runner struct {
	scenario Scenario
	deps     Dependencies
	script   ScriptState
}

type Result struct {
	SchemaVersion string               `json:"schema_version"`
	ScenarioID    string               `json:"scenario_id"`
	PassedSteps   int                  `json:"passed_steps"`
	Events        []application.Event  `json:"events"`
	Snapshot      application.Snapshot `json:"snapshot"`
}

func NewRunner(value Scenario, deps Dependencies, script ScriptState) *Runner {
	return &Runner{scenario: value, deps: deps, script: script}
}

func (runner *Runner) Run(ctx context.Context) (Result, error) {
	if runner.deps.Submission == nil || runner.deps.Snapshot == nil || runner.deps.Events == nil ||
		runner.deps.Interaction == nil || runner.deps.Lifecycle == nil || runner.script == nil {
		return Result{}, fmt.Errorf("runner dependencies and script state are required")
	}
	if err := runner.scenario.Validate(); err != nil {
		return Result{}, err
	}
	initialRevision := runner.deps.Snapshot.Snapshot().Revision
	recorder := newEventRecorder(runner.deps.Events.Subscribe(256))
	defer func() {
		recorder.close()
		runner.deps.Lifecycle.Shutdown()
	}()

	passed := 0
	for index, step := range runner.scenario.Steps {
		if err := runner.executeStep(ctx, recorder, step); err != nil {
			return Result{}, fmt.Errorf("step %d: %w", index+1, err)
		}
		passed++
	}
	if err := runner.waitForSnapshot(ctx, recorder, func(snapshot application.Snapshot) bool {
		return !snapshot.Chat.Running
	}); err != nil {
		return Result{}, fmt.Errorf("wait for chat completion: %w", err)
	}
	if remaining := runner.script.Remaining(); remaining != 0 {
		return Result{}, fmt.Errorf("script has %d unconsumed turn(s)", remaining)
	}
	finalSnapshot := runner.deps.Snapshot.Snapshot()
	// Snapshot state is committed before its corresponding event is published.
	// Wait for that event so the result cannot omit earlier lifecycle events
	// that are still buffered in the recorder goroutine.
	if finalSnapshot.Revision > initialRevision {
		if err := recorder.waitForRevision(ctx, finalSnapshot.Revision); err != nil {
			return Result{}, fmt.Errorf("wait for event recorder: %w", err)
		}
	}
	return Result{
		SchemaVersion: SchemaVersion, ScenarioID: runner.scenario.ID, PassedSteps: passed,
		Events: recorder.snapshot(), Snapshot: finalSnapshot,
	}, nil
}

func (runner *Runner) executeStep(ctx context.Context, recorder *eventRecorder, step Step) error {
	if step.Action != "" {
		return runner.executeAction(ctx, step)
	}
	switch step.Expect {
	case "chat_running":
		return runner.waitForSnapshot(ctx, recorder, func(snapshot application.Snapshot) bool {
			return snapshot.Chat.Running == *step.Running
		})
	case "message_delta":
		return recorder.waitFor(ctx, func(events []application.Event) bool {
			for _, event := range events {
				if event.Kind != application.EventMessageDelta {
					continue
				}
				var delta application.MessageDelta
				if json.Unmarshal(event.Payload, &delta) == nil && strings.Contains(delta.Delta, step.Value) {
					return true
				}
			}
			return false
		})
	case "tool_status":
		return runner.waitForSnapshot(ctx, recorder, func(snapshot application.Snapshot) bool {
			for _, message := range snapshot.Conversation {
				if message.Tool != nil && message.Tool.Name == step.Tool && message.Tool.Status.String() == step.Status {
					return true
				}
			}
			return false
		})
	case "interaction":
		return runner.waitForSnapshot(ctx, recorder, func(snapshot application.Snapshot) bool {
			return snapshot.Interaction != nil && snapshot.Interaction.ID == step.ID
		})
	case "event_sequence":
		return recorder.waitFor(ctx, func(events []application.Event) bool {
			return containsEventSequence(events, step.Kinds)
		})
	case "event_set":
		return recorder.waitFor(ctx, func(events []application.Event) bool {
			return containsEventSet(events, step.Kinds)
		})
	default:
		return fmt.Errorf("unsupported expectation %q", step.Expect)
	}
}

func (runner *Runner) executeAction(ctx context.Context, step Step) error {
	switch step.Action {
	case "submit":
		if step.SessionID != "" {
			current := runner.deps.Snapshot.Snapshot().Session.ID
			if step.SessionID != current {
				return fmt.Errorf("submit session %q does not match active session %q", step.SessionID, current)
			}
		}
		return runner.deps.Submission.Submit(ctx, step.Text)
	case "resolve_interaction":
		id := step.ID
		if id == "" {
			interaction := runner.deps.Snapshot.Snapshot().Interaction
			if interaction == nil {
				return fmt.Errorf("no active interaction to resolve")
			}
			id = interaction.ID
		}
		return runner.deps.Interaction.Resolve(ctx, id, step.Option)
	default:
		return fmt.Errorf("unsupported action %q", step.Action)
	}
}

func (runner *Runner) waitForSnapshot(ctx context.Context, recorder *eventRecorder, predicate func(application.Snapshot) bool) error {
	for {
		if predicate(runner.deps.Snapshot.Snapshot()) {
			return nil
		}
		if err := recorder.waitForChange(ctx); err != nil {
			return err
		}
	}
}

func containsEventSequence(events []application.Event, kinds []string) bool {
	next := 0
	for _, event := range events {
		if string(event.Kind) == kinds[next] {
			next++
			if next == len(kinds) {
				return true
			}
		}
	}
	return false
}

func containsEventSet(events []application.Event, kinds []string) bool {
	found := make(map[string]bool, len(kinds))
	for _, event := range events {
		found[string(event.Kind)] = true
	}
	for _, kind := range kinds {
		if !found[kind] {
			return false
		}
	}
	return true
}

func (result Result) WriteJSON(writer io.Writer) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
