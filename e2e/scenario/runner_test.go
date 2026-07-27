package scenario

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/agent/core/tool/builtin"
	"github.com/RedHuang-0622/Seele/agent/core/tool/interfaces"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"
	"github.com/RedHuang-0622/seelex/application"
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

type workPlanToolExecutor struct {
	handlers map[string]interfaces.ToolHandler
}

func newWorkPlanToolExecutor(requester ApprovalRequester) ToolExecutor {
	tool := builtin.NewWorkPlanTool(planAgentFactory{})
	tool.SetGate(planApprovalGate{requester: requester})
	handlers := make(map[string]interfaces.ToolHandler)
	for _, entry := range tool.Tools() {
		handlers[entry.Definition.Function.Name] = entry.Handler
	}
	return workPlanToolExecutor{handlers: handlers}
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
