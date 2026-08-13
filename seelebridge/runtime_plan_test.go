package seelebridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge/plan"
)

func BenchmarkPlanLoadSmoke(b *testing.B) {
	runtime := newTestRuntime(b)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	b.SetBytes(int64(len(planLoadSmokeInput)))
	b.ResetTimer()
	for b.Loop() {
		if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", planLoadSmokeInput); err != nil {
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
	result, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", planLoadAdapterInput)
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
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", parallelFourNodes); err == nil || !strings.Contains(err.Error(), "serial chain") {
		t.Fatalf("medium parallel plan error = %v, want serial-chain rejection", err)
	}
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", fiveNodes); err == nil || !strings.Contains(err.Error(), "maximum is 4") {
		t.Fatalf("medium five-node plan error = %v, want node-limit rejection", err)
	}
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", serialFourNodes); err != nil {
		t.Fatalf("medium serial plan error = %v", err)
	}
	if runtime.planExecutor.MaxForkConcurrency() != 1 {
		t.Fatalf("medium concurrency = %d, want 1", runtime.planExecutor.MaxForkConcurrency())
	}

	runtime.SetPlanPolicy(dto.PlanPolicy{Effort: "high", MaxForkConcurrency: 3})
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", parallelFourNodes); err != nil {
		t.Fatalf("high parallel plan error = %v", err)
	}
	if runtime.planExecutor.MaxForkConcurrency() != 3 {
		t.Fatalf("high concurrency = %d, want 3", runtime.planExecutor.MaxForkConcurrency())
	}

	runtime.SetPlanPolicy(dto.PlanPolicy{Effort: "max"})
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", fiveNodes); err != nil {
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

	result, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", planLoadSmokeInput)
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

	for _, tool := range runtime.registry.Registry.Tools() {
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
func TestRuntimePrepareReplanLoadsRecoveryPlanWithBoundedRetry(t *testing.T) {
	plan := map[string]interface{}{
		"entry": "recover",
		"nodes": map[string]interface{}{
			"recover": map[string]string{"input": "diagnose compiler failure"},
		},
		"edges": map[string][]string{},
	}
	var payload struct {
		ToolChoice json.RawMessage `json:"tool_choice"`
		Messages   []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		defer request.Body.Close()
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode replan request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if calls == 1 {
			invalid, _ := json.Marshal(map[string]interface{}{
				"entry": "recover",
				"nodes": map[string]interface{}{
					"recover": map[string]string{"input": "diagnose compiler failure"},
				},
				// This is the provider failure observed in the live smoke: edges
				// arrived as an array. Policy rejects it before plan_load delegates
				// to Seele, so the one corrective retry is idempotent.
				"edges": []interface{}{map[string]string{"to": "recover"}},
			})
			_ = json.NewEncoder(writer).Encode(map[string]interface{}{
				"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{
					"role": "assistant",
					"tool_calls": []interface{}{map[string]interface{}{
						"id": "invalid-recovery-plan", "type": "function",
						"function": map[string]string{"name": "plan_load", "arguments": string(invalid)},
					}},
				}}},
			})
			return
		}
		arguments, _ := json.Marshal(plan)
		_ = json.NewEncoder(writer).Encode(map[string]interface{}{
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{
				"role": "assistant",
				"tool_calls": []interface{}{map[string]interface{}{
					"id": "recovery-plan", "type": "function",
					"function": map[string]string{"name": "plan_load", "arguments": string(arguments)},
				}},
			}}},
		})
	}))
	defer server.Close()

	accounts := filepath.Join(t.TempDir(), "accounts.yaml")
	content := fmt.Sprintf("roles:\n  agent:\n    - model: test-model\n      base_url: %s\n      api_key: test-key\n", server.URL)
	if err := os.WriteFile(accounts, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(RuntimeConfig{AccountsPath: accounts, ToolCallTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	runtime.SetPlanPolicy(dto.PlanPolicy{Effort: "lite"})
	// replan 恢复路径经 plan_load（plan 工具面归位后需 goal 激活可见）。
	runtime.SetRuntimeVisibilityProjection(RuntimeVisibilityProjection{GoalSkillActive: true})

	result, err := runtime.PrepareReplan(context.Background(), dto.ReplanRequest{
		Objective:    "build the release",
		PreviousPlan: `{"entry":"build"}`,
		Failure:      `node "build": compiler failed`,
		Evidence:     "node=lint status=completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	// tool_choice 不再强制（thinking 模型平台拒绝强制；靠 replan prompt
	// 引导）——payload 里必须保持未设置，兼容 OpenAI thinking 模式。
	if len(payload.ToolChoice) != 0 {
		t.Fatalf("tool_choice = %s, want unset (thinking-model compatible)", payload.ToolChoice)
	}
	if calls != 2 {
		t.Fatalf("replan request count = %d, want one bounded retry", calls)
	}
	metrics := runtime.ReplanMetrics()
	if metrics.ProviderRequests != 2 || metrics.Accepted != 1 || metrics.Succeeded != 1 || metrics.InFlight != 0 {
		t.Fatalf("replan metrics = %+v", metrics)
	}
	if len(payload.Messages) < 2 || !strings.Contains(payload.Messages[1].Content, "compiler failed") {
		t.Fatalf("replan payload did not include failure context: %+v", payload.Messages)
	}
	if result.Arguments == "" || !strings.Contains(result.Result, `"status":"loaded"`) {
		t.Fatalf("replan result = %+v", result)
	}
}
