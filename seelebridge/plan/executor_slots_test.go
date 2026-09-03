package plan

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
)

func newSlotTestExecutor(t *testing.T) *Executor {
	t.Helper()
	executor := &Executor{
		deps: ExecutorDeps{
			Model: "test-model",
			LoadPlanDefinition: func() (types.Tool, bool) {
				return types.Tool{}, false
			},
			Dispatch: func(context.Context, string, string) (string, error) {
				return "", nil
			},
		},
		events:     NewEventSink(),
		nodeEvents: make(chan PlanNodeEvent, 8),
		replans:    NewReplanGuards(1, 2, 3, 0),
	}
	executor.provider = NewToolProvider(executor)
	return executor
}

// TestPlanSlotsPolicyIsolation（G1-C/M6）：策略按会话建槽互不覆盖；无 sid
// 槽未写入时，显式会话读取回退默认槽。
func TestPlanSlotsPolicyIsolation(t *testing.T) {
	executor := newSlotTestExecutor(t)
	policyA := dto.PlanPolicy{Effort: "high", MaxForkConcurrency: 4, MaxNodes: 12}
	policyB := dto.PlanPolicy{Effort: "lite", MaxForkConcurrency: 1}

	executor.SetPolicyFor("sess-a", policyA)
	executor.SetPolicyFor("sess-b", policyB)
	if got := executor.PolicyFor("sess-a"); got != policyA {
		t.Fatalf("PolicyFor(sess-a) = %+v", got)
	}
	if got := executor.PolicyFor("sess-b"); got != policyB {
		t.Fatalf("PolicyFor(sess-b) = %+v", got)
	}
	if got := executor.PolicyFor("sess-c"); got != (dto.PlanPolicy{}) {
		t.Fatalf("unslotted session must fall back to default, got %+v", got)
	}

	executor.SetPolicy(dto.PlanPolicy{Effort: "default"})
	if got := executor.Policy(); got.Effort != "default" {
		t.Fatalf("legacy Policy() = %+v", got)
	}
}

// TestPlanSlotsBindingAndRunIDIsolation（G1-C/M6）：分支绑定与 run ID 按
// 会话分槽；SetBinding 同时写默认槽作为单飞 legacy 别名。
func TestPlanSlotsBindingAndRunIDIsolation(t *testing.T) {
	executor := newSlotTestExecutor(t)
	bindingA := dto.PlanBranchBinding{SessionID: "sess-a", PlanID: "plan-a", TraceID: "chat-a"}
	bindingB := dto.PlanBranchBinding{SessionID: "sess-b", PlanID: "plan-b", TraceID: "chat-b"}

	executor.SetBinding(bindingA)
	if got := executor.BindingFor("sess-a"); got != bindingA {
		t.Fatalf("BindingFor(sess-a) = %+v", got)
	}
	if got := executor.Binding(); got != bindingA {
		t.Fatalf("legacy Binding() alias = %+v", got)
	}
	executor.SetBindingFor("sess-b", bindingB)
	if got := executor.BindingFor("sess-a"); got != bindingA {
		t.Fatalf("sess-b binding leaked into sess-a: %+v", got)
	}
	if got := executor.BindingFor("sess-b"); got != bindingB {
		t.Fatalf("BindingFor(sess-b) = %+v", got)
	}

	runA := executor.beginRunFor("sess-a")
	runB := executor.beginRunFor("sess-b")
	if executor.CurrentRunIDFor("sess-a") != runA || executor.CurrentRunIDFor("sess-b") != runB {
		t.Fatalf("run IDs not isolated: a=%q b=%q", executor.CurrentRunIDFor("sess-a"), executor.CurrentRunIDFor("sess-b"))
	}
	executor.endRunFor("sess-a", runA)
	if executor.CurrentRunIDFor("sess-a") != "" {
		t.Fatalf("run ID for sess-a not cleared: %q", executor.CurrentRunIDFor("sess-a"))
	}
	if executor.CurrentRunIDFor("sess-b") != runB {
		t.Fatalf("clearing sess-a run touched sess-b: %q", executor.CurrentRunIDFor("sess-b"))
	}
}
