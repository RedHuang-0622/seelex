package scenario

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/agent/core/tool/builtin"
	"github.com/RedHuang-0622/Seele/agent/core/tool/interfaces"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	"github.com/RedHuang-0622/Seele/workplan/runtime/forkexec"
	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/seelebridge"
)

func TestGoldenJourneyChatToolApproval(t *testing.T) {
	value, err := LoadFile("../fixtures/approval-chat.json")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewHarnessRunner(value)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := runner.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.PassedSteps != len(value.Steps) {
		t.Fatalf("passed steps = %d, want %d", result.PassedSteps, len(value.Steps))
	}
	if result.Snapshot.Chat.Running {
		t.Fatal("chat is still running")
	}
	if result.Snapshot.Interaction != nil {
		t.Fatalf("interaction still open: %+v", result.Snapshot.Interaction)
	}
	if result.Snapshot.Runtime.Effort != "medium" {
		t.Fatalf("effort = %q, want medium", result.Snapshot.Runtime.Effort)
	}
	if len(result.Events) == 0 {
		t.Fatal("scenario did not record events")
	}
}

func TestGoldenJourneyManualPlan(t *testing.T) {
	value, err := LoadFile("../fixtures/manual-plan.json")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := NewHarnessRunnerWithToolFactory(value, newWorkPlanToolExecutor)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := runner.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Snapshot.Runtime.Plan == nil {
		t.Fatal("plan state is missing")
	}
	plan := result.Snapshot.Runtime.Plan
	if plan.Status != application.PlanCompleted || plan.Progress != 1 || len(plan.Nodes) != 1 {
		t.Fatalf("plan = %+v, want completed manual node", plan)
	}
	if plan.Nodes[0].Kind != "manual" || plan.Nodes[0].Status != application.NodeCompleted {
		t.Fatalf("node = %+v, want completed manual node", plan.Nodes[0])
	}
}

func TestParallelPlanUsesIsolatedFactoriesAndLifecycle(t *testing.T) {
	value := Scenario{
		SchemaVersion: SchemaVersion,
		ID:            "parallel-plan",
		Initial:       InitialState{ActiveSessionID: "session-parallel"},
		EngineScript: []EngineTurn{{
			OnUser: "run parallel plan",
			Emit: []Emission{
				{Type: "tool.call", Name: "plan_load", Arguments: `{"entry":"start","nodes":{"start":{"input":"start"},"left":{"input":"left"},"right":{"input":"right"}},"edges":{"start":["left","right"]}}`},
				{Type: "tool.call", Name: "plan_run", Arguments: `{}`},
			},
		}},
		Steps: []Step{
			{Action: "submit", SessionID: "session-parallel", Text: "run parallel plan"},
			{Expect: "tool_status", Tool: "plan_run", Status: "success"},
			{Expect: "chat_running", Running: boolPtr(false)},
		},
	}
	runner, err := NewHarnessRunnerWithToolFactory(value, newParallelWorkPlanToolExecutor)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := runner.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plan := result.Snapshot.Runtime.Plan
	if plan == nil || plan.Status != application.PlanCompleted || plan.Progress != 1 {
		t.Fatalf("plan = %+v, want completed", plan)
	}
	for _, id := range []string{"left", "right"} {
		node := planNodeByID(plan, id)
		if node == nil || node.Status != application.NodeCompleted {
			t.Fatalf("node %q = %+v, want completed", id, node)
		}
		if !strings.Contains(node.Output, "factory:"+id) {
			t.Fatalf("node %q output %q does not prove its factory", id, node.Output)
		}
		for _, status := range []application.NodeStatus{application.NodeQueued, application.NodeRunning, application.NodeCompleted} {
			if !containsPlanNodeStatus(result.Events, id, status) {
				t.Fatalf("node %q did not publish lifecycle status %q", id, status)
			}
		}
	}
}

type workPlanToolExecutor struct {
	handlers map[string]interfaces.ToolHandler
}

func newWorkPlanToolExecutor(requester ApprovalRequester, onBranchEvent func(seelebridge.PlanBranchEvent)) ToolExecutor {
	tool := builtin.NewWorkPlanTool(planAgentFactory{})
	tool.SetGate(planApprovalGate{requester: requester})
	tool.SetBranchEventHook(adaptBranchEvent(onBranchEvent))
	handlers := make(map[string]interfaces.ToolHandler)
	for _, entry := range tool.Tools() {
		handlers[entry.Definition.Function.Name] = entry.Handler
	}
	return workPlanToolExecutor{handlers: handlers}
}

func newParallelWorkPlanToolExecutor(_ ApprovalRequester, onBranchEvent func(seelebridge.PlanBranchEvent)) ToolExecutor {
	tool := builtin.NewWorkPlanTool(planAgentFactory{})
	tool.SetBranchEventHook(adaptBranchEvent(onBranchEvent))
	tool.SetBranchRuntimeResolver(func(branchID string) forkexec.BranchRuntime {
		return forkexec.BranchRuntime{AgentFactory: taggedPlanAgentFactory{tag: branchID}}
	})
	handlers := make(map[string]interfaces.ToolHandler)
	for _, entry := range tool.Tools() {
		handlers[entry.Definition.Function.Name] = entry.Handler
	}
	return workPlanToolExecutor{handlers: handlers}
}

func adaptBranchEvent(callback func(seelebridge.PlanBranchEvent)) func(forkexec.Event) {
	return func(event forkexec.Event) {
		if callback == nil {
			return
		}
		branchEvent := seelebridge.PlanBranchEvent{Type: string(event.Type), BranchID: event.BranchID, NodeID: event.NodeID, At: event.At}
		if event.Err != nil {
			branchEvent.Error = event.Err.Error()
		}
		callback(branchEvent)
	}
}

func (executor workPlanToolExecutor) Execute(ctx context.Context, name, arguments string) (string, error) {
	handler, ok := executor.handlers[name]
	if !ok {
		return "", fmt.Errorf("tool %q is unavailable", name)
	}
	return handler.Execute(ctx, arguments)
}

type planApprovalGate struct {
	requester ApprovalRequester
}

func (gate planApprovalGate) Ask(ctx context.Context, question approve.Question) (any, error) {
	options := make([]application.InteractionOption, 0, len(question.Options))
	for _, option := range question.Options {
		options = append(options, application.InteractionOption{
			ID: option.Key, Label: option.Label, Description: option.Description, Style: option.Style,
		})
	}
	decision, err := gate.requester.Request(ctx, application.ApprovalRequest{
		ID: question.ID, Question: question.Content, Options: options, Timeout: question.Timeout,
	})
	if err != nil {
		return nil, err
	}
	return decision.OptionID, nil
}

type planAgentFactory struct{}

func (planAgentFactory) NewAgent(string) node.Agent { return planAgent{} }

type planAgent struct{}

func (planAgent) Chat(ctx context.Context, input string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "completed: " + input, nil
}

type taggedPlanAgentFactory struct{ tag string }

func (factory taggedPlanAgentFactory) NewAgent(string) node.Agent {
	return taggedPlanAgent{tag: factory.tag}
}

type taggedPlanAgent struct{ tag string }

func (agent taggedPlanAgent) Chat(ctx context.Context, input string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "factory:" + agent.tag + ": " + input, nil
}

func boolPtr(value bool) *bool { return &value }

func planNodeByID(plan *application.PlanState, id string) *application.PlanNode {
	for index := range plan.Nodes {
		if plan.Nodes[index].ID == id {
			return &plan.Nodes[index]
		}
	}
	return nil
}

func containsPlanNodeStatus(events []application.Event, nodeID string, status application.NodeStatus) bool {
	for _, event := range events {
		if event.Kind != application.EventRuntimeChanged {
			continue
		}
		var runtime application.RuntimeState
		if json.Unmarshal(event.Payload, &runtime) != nil || runtime.Plan == nil {
			continue
		}
		node := planNodeByID(runtime.Plan, nodeID)
		if node != nil && node.Status == status {
			return true
		}
	}
	return false
}
