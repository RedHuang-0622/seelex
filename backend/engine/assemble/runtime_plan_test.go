package assemble

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/backend/tool/plan"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

func BenchmarkPlanLoadSmoke(b *testing.B) {
	runtime := newTestRuntime(b)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	b.SetBytes(int64(len(planLoadSmokeInput)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := runPlanToolForTest(runtime, context.Background(), "plan_load", planLoadSmokeInput); err != nil {
			b.Fatal(err)
		}
	}
}
func TestNormalizePlanLoadArgumentsMergesReferencedTopLevelNodeSpecs(t *testing.T) {
	legacy := `{"entry":"inspect","nodes":{"inspect":{"input":"read"}},"verify":{"input":"check"},"report":{"input":"summarize"},"edges":{"inspect":["verify"],"verify":["report"]}}`
	canonical, err := plan.NormalizePlanLoadArguments(legacy)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"inspect":{"input":"read"}`, `"verify":{"input":"check"}`, `"report":{"input":"summarize"}`} {
		if !strings.Contains(canonical, expected) {
			t.Fatalf("canonical plan %s is missing %s", canonical, expected)
		}
	}

	unsafe := `{"entry":"inspect","nodes":{"inspect":{"input":"read"}},"item":{"input":"metadata"},"edges":{}}`
	if _, err := plan.NormalizePlanLoadArguments(unsafe); err == nil || !strings.Contains(err.Error(), "unexpected top-level field") {
		t.Fatalf("unreferenced top-level field error = %v", err)
	}
}
func TestNormalizePlanLoadArgumentsNormalizesExplicitEdgeChain(t *testing.T) {
	input := `{"entry":"inspect","nodes":{"inspect":{"input":"read"},"verify":{"input":"check"},"report":{"input":"summarize"}},"edges":"inspect -> verify -> report"}`
	canonical, err := plan.NormalizePlanLoadArguments(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"inspect":["verify"]`, `"verify":["report"]`} {
		if !strings.Contains(canonical, expected) {
			t.Fatalf("canonical plan %s is missing %s", canonical, expected)
		}
	}

	if _, err := plan.NormalizePlanLoadArguments(`{"entry":"inspect","nodes":{"inspect":{"input":"read"}},"edges":"inspect"}`); err == nil || !strings.Contains(err.Error(), "explicit chain") {
		t.Fatalf("ambiguous edge string error = %v", err)
	}
}
func TestNormalizePlanLoadArgumentsNormalizesNestedTargetsAndRejectsAmbiguousEdges(t *testing.T) {
	nested := `{"entry":"inspect","nodes":{"inspect":{"input":"inspect"},"report":{"input":"report"}},"edges":{"inspect":[{"to":"report"}]}}`
	canonical, err := plan.NormalizePlanLoadArguments(nested)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(canonical, `"edges":{"inspect":["report"]}`) {
		t.Fatalf("nested target canonical form = %s", canonical)
	}
	ambiguous := `{"entry":"inspect","nodes":[{"id":"inspect","input":"inspect"},{"id":"report","input":"report"}],"edges":[{"to":"report"}]}`
	if _, err := plan.NormalizePlanLoadArguments(ambiguous); err == nil || !strings.Contains(err.Error(), "from") {
		t.Fatalf("ambiguous edge error = %v, want missing source", err)
	}
}
func TestNormalizePlanLoadArgumentsRecoversOrderedEdgeTargetList(t *testing.T) {
	input := `{"entry":"inspect","nodes":{"inspect":{"input":"inspect"},"implement":{"input":"implement"},"verify":{"input":"verify"}},"edges":["implement","verify"]}`
	canonical, err := plan.NormalizePlanLoadArguments(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"inspect":["implement"]`, `"implement":["verify"]`} {
		if !strings.Contains(canonical, expected) {
			t.Fatalf("ordered target list canonical plan %s is missing %s", canonical, expected)
		}
	}

	duplicate := `{"entry":"inspect","nodes":{"inspect":{"input":"inspect"},"verify":{"input":"verify"}},"edges":["verify","verify"]}`
	if _, err := plan.NormalizePlanLoadArguments(duplicate); err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatalf("duplicate ordered edge target error = %v", err)
	}
}
func TestPlanLoadAdapterNormalizesLLMFriendlyDAG(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	canonical, err := plan.NormalizePlanLoadArguments(planLoadAdapterInput)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"inspect":{"input":"inspect module boundaries"}`, `"verify":["report"]`} {
		if !strings.Contains(canonical, required) {
			t.Errorf("canonical plan %q is missing %s", canonical, required)
		}
	}
	result, err := runPlanToolForTest(runtime, context.Background(), "plan_load", planLoadAdapterInput)
	if err != nil {
		t.Fatalf("adapter plan_load returned an error: %v", err)
	}
	for _, required := range []string{`"status":"loaded"`, `"node_count":3`, `"edge_count":2`, `"entry":"inspect"`} {
		if !strings.Contains(result, required) {
			t.Errorf("adapter plan_load result %q is missing %s", result, required)
		}
	}
}
func TestPlanLoadEnforcesEffortPolicy(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	serialFourNodes := `{"entry":"one","nodes":{"one":{"input":"one"},"two":{"input":"two"},"three":{"input":"three"},"four":{"input":"four"}},"edges":{"one":["two"],"two":["three"],"three":["four"]}}`
	parallelFourNodes := `{"entry":"start","nodes":{"start":{"input":"start"},"left":{"input":"left"},"right":{"input":"right"},"finish":{"input":"finish"}},"edges":{"start":["left","right"],"left":["finish"],"right":["finish"]}}`
	fiveNodes := `{"entry":"one","nodes":{"one":{"input":"one"},"two":{"input":"two"},"three":{"input":"three"},"four":{"input":"four"},"five":{"input":"five"}},"edges":{"one":["two"],"two":["three"],"three":["four"],"four":["five"]}}`

	runtime.SetPlanPolicy(dto.PlanPolicy{Effort: "medium", MaxNodes: 4, RequireSerial: true, MaxForkConcurrency: 1})
	if _, err := runPlanToolForTest(runtime, context.Background(), "plan_load", parallelFourNodes); err == nil || !strings.Contains(err.Error(), "serial chain") {
		t.Fatalf("medium parallel plan error = %v, want serial-chain rejection", err)
	}
	if _, err := runPlanToolForTest(runtime, context.Background(), "plan_load", fiveNodes); err == nil || !strings.Contains(err.Error(), "maximum is 4") {
		t.Fatalf("medium five-node plan error = %v, want node-limit rejection", err)
	}
	if _, err := runPlanToolForTest(runtime, context.Background(), "plan_load", serialFourNodes); err != nil {
		t.Fatalf("medium serial plan error = %v", err)
	}
	if runtime.planExecutor.MaxForkConcurrency() != 1 {
		t.Fatalf("medium concurrency = %d, want 1", runtime.planExecutor.MaxForkConcurrency())
	}

	runtime.SetPlanPolicy(dto.PlanPolicy{Effort: "high", MaxForkConcurrency: 3})
	if _, err := runPlanToolForTest(runtime, context.Background(), "plan_load", parallelFourNodes); err != nil {
		t.Fatalf("high parallel plan error = %v", err)
	}
	if runtime.planExecutor.MaxForkConcurrency() != 3 {
		t.Fatalf("high concurrency = %d, want 3", runtime.planExecutor.MaxForkConcurrency())
	}

	runtime.SetPlanPolicy(dto.PlanPolicy{Effort: "max"})
	if _, err := runPlanToolForTest(runtime, context.Background(), "plan_load", fiveNodes); err != nil {
		t.Fatalf("max plan error = %v", err)
	}
	if runtime.planExecutor.MaxForkConcurrency() != 5 {
		t.Fatalf("max concurrency = %d, want node count 5", runtime.planExecutor.MaxForkConcurrency())
	}
}
func TestPlanLoadSmoke(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	result, err := runPlanToolForTest(runtime, context.Background(), "plan_load", planLoadSmokeInput)
	if err != nil {
		t.Fatalf("plan_load returned an error: %v", err)
	}
	for _, required := range []string{`"status":"loaded"`, `"node_count":2`, `"edge_count":1`, `"entry":"search"`} {
		if !strings.Contains(result, required) {
			t.Errorf("plan_load result %q is missing %s", result, required)
		}
	}
}
func TestPlanReplanPromptRendersCopyableManualRecovery(t *testing.T) {
	prompt := plan.ReplanPrompt(dto.PlanPolicy{Effort: "high", MaxForkConcurrency: 3})
	for _, required := range []string{
		"Copy this complete recovery shape first", `"entry":"diagnose"`, `"kind":"manual"`,
		"only top-level keys", "Effort: `high`",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("replan prompt %q is missing %q", prompt, required)
		}
	}
}
func TestRuntimePlanLoadToolPublishesStrictJSONContract(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	for _, entry := range runtime.planExecutor.Provider().Tools() {
		tool := entry.Definition
		if tool.Function.Name != "plan_load" {
			continue
		}
		for _, required := range []string{
			"Emit exactly three top-level fields: entry, nodes, and edges. Do not use item.",
			"Nodes MUST be an object keyed by node ID",
			"Valid simple example:",
			"Valid audit example:",
			"Valid code-change example:",
			"Do not use node or edge arrays in preflight.",
			`{"entry":"inspect","nodes":{"inspect":{"input":"read source"},"verify":{"input":"verify claims"},"report":{"input":"report findings"}},"edges":{"inspect":["verify"],"verify":["report"]}}`,
		} {
			if !strings.Contains(tool.Function.Description, required) {
				t.Errorf("plan_load description is missing %q", required)
			}
		}
		properties := tool.Function.Parameters["properties"].(map[string]interface{})
		nodes := properties["nodes"].(map[string]interface{})
		if nodes["type"] != "object" || nodes["oneOf"] != nil {
			t.Fatalf("plan_load nodes schema = %#v, want canonical object only", nodes)
		}
		edges := properties["edges"].(map[string]interface{})
		if edges["type"] != "object" || edges["oneOf"] != nil {
			t.Fatalf("plan_load edges schema = %#v, want canonical object only", edges)
		}
		if tool.Function.Parameters["additionalProperties"] != false {
			t.Fatalf("plan_load root schema must reject unexpected fields: %#v", tool.Function.Parameters)
		}
		return
	}
	t.Fatal("plan_load tool was not registered")
}
