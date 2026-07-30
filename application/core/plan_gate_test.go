package core

import (
	"context"
	"testing"

	"github.com/RedHuang-0622/seelex/seelebridge"
)

func TestConversationalTurnBypassesPlanningDecision(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{}
	service := newTestService(engine)
	service.deps.Runtime = runtime
	defer service.Shutdown()

	if err := service.Submit(context.Background(), "你好？"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	if len(runtime.preflight) != 0 || len(runtime.planScopeAcquired) != 0 {
		t.Fatalf("greeting entered a planning decision: preflight=%v scopes=%v", runtime.preflight, runtime.planScopeAcquired)
	}
	if got := engine.lastInput; got != "你好？" {
		t.Fatalf("greeting model input = %q", got)
	}
}

func TestWorkRequestBypassesHighEffortPlanPreflight(t *testing.T) {
	engine := &fakeEngine{}
	runtime := &fakeRuntime{preflightResult: seelebridge.PlanPreflight{
		Arguments: `{"entry":"inspect","nodes":{"inspect":{"input":"inspect the issue"}},"edges":{}}`,
		Result:    `{"status":"loaded"}`,
	}}
	service := newTestService(engine)
	service.deps.Runtime = runtime
	defer service.Shutdown()

	if err := service.Submit(context.Background(), "修复登录失败的问题"); err != nil {
		t.Fatal(err)
	}
	waitForChatCompletion(t, service)
	if len(runtime.preflight) != 0 || len(runtime.planScopeAcquired) != 0 {
		t.Fatalf("work request entered PlanAct: preflight=%v scopes=%v", runtime.preflight, runtime.planScopeAcquired)
	}
}
