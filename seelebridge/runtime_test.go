package seelebridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/agent/core/api"
)

func TestRuntimeAccountsToolsAndPlugins(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()

	runtime.RegisterTool("cad_draw", "draw", map[string]interface{}{"type": "object"},
		func(context.Context, string) (string, error) { return "ok", nil })
	if err := runtime.DefinePlugin("cad", "CAD", []string{"cad_*"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := runtime.ActivatePlugin("cad"); err != nil {
		t.Fatal(err)
	}
	tools := runtime.VisibleTools(context.Background())
	if len(tools) != 1 || tools[0].Name != "cad_draw" || runtime.ActivePlugin() != "cad" {
		t.Fatalf("unexpected plugin tools: %#v active=%q", tools, runtime.ActivePlugin())
	}
	accounts := runtime.Accounts()
	if len(accounts) != 1 || accounts[0].Name != "agent-1" || runtime.Model() != "test-model" {
		t.Fatalf("unexpected accounts: %#v model=%q", accounts, runtime.Model())
	}
	if !runtime.SelectAccount("agent-1") || runtime.SelectAccount("missing") {
		t.Fatal("account selection result is incorrect")
	}
	if runtime.Provider() != "openai" {
		t.Fatalf("provider=%q", runtime.Provider())
	}
	runtime.SetProvider("anthropic")
	if runtime.Provider() != "anthropic" {
		t.Fatalf("provider was not updated: %q", runtime.Provider())
	}
	if err := runtime.DefinePlugin("", "", nil, nil); err == nil {
		t.Fatal("empty plugin name should fail")
	}
	runtime.DeactivatePlugin()
	runtime.UndefinePlugin("cad")
	if runtime.ActivePlugin() != "" {
		t.Fatal("plugin was not deactivated")
	}
}

func TestRuntimeRejectsEmptyAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	if err := os.WriteFile(path, []byte("roles: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRuntime(RuntimeConfig{AccountsPath: path}); err == nil {
		t.Fatal("empty accounts should fail")
	}
}

func TestMissingAccountsUsesEnvironmentCredential(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	path := filepath.Join(t.TempDir(), "missing-accounts.yaml")

	pool, roles, err := loadSimplifiedConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	accounts := pool.All()
	if len(accounts) != 1 || accounts[0].APIKey != "test-key" {
		t.Fatalf("fallback accounts = %+v", accounts)
	}
	if len(roles) != 1 || roles[0] != RoleAgent {
		t.Fatalf("fallback roles = %v", roles)
	}
}

func TestRuntimeLoadsGroupedAccountRoles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	content := `roles:
  subagent:
    - model: child-model
      base_url: http://localhost
      api_key: test-key
  agent:
    - model: main-model
      base_url: http://localhost
      api_key: test-key
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	pool, roles, err := loadSimplifiedConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(pool.All()) != 2 || len(roles) != 2 {
		t.Fatalf("unexpected accounts=%d roles=%d", len(pool.All()), len(roles))
	}
	account, err := ResolveAccount(pool, RoleSubAgent)
	if err != nil || account.Name != "subagent-1" {
		t.Fatalf("resolved account=%v err=%v", account, err)
	}
}

func TestRuntimeRejectsLegacyAccountsList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	content := "accounts:\n  - name: main\n    model: test-model\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRuntime(RuntimeConfig{AccountsPath: path}); err == nil {
		t.Fatal("legacy accounts-list config should fail")
	}
}

func TestRuntimeBuiltinsAndMCPEmptyState(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()
	runtime.RegisterTool("read_file", "unsafe override", map[string]interface{}{"type": "object"}, func(context.Context, string) (string, error) {
		return "unsafe", nil
	})
	if len(runtime.AllTools()) == 0 || runtime.Agent() == nil {
		t.Fatal("builtins or Agent accessor missing")
	}
	registered := make(map[string]bool)
	for _, tool := range runtime.AllTools() {
		registered[tool.Name] = true
	}
	for _, name := range []string{"plan_load", "plan_run", "plan_status", "plan_export", "plan_clear"} {
		if !registered[name] {
			t.Errorf("initial builtin tools are missing %q", name)
		}
	}
	if names := runtime.MCPServerNames(); len(names) != 0 {
		t.Fatalf("unexpected MCP servers: %v", names)
	}
	if err := runtime.DetachMCP("missing"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RefreshMCP(context.Background(), "missing"); err == nil {
		t.Fatal("refreshing missing MCP should fail")
	}
}

func TestRuntimePlanLoadToolPublishesStrictJSONContract(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	for _, tool := range runtime.Agent().Tools().Tools() {
		if tool.Function.Name != "plan_load" {
			continue
		}
		for _, required := range []string{
			"Use only these top-level fields: entry, nodes, and edges. Do not use item.",
			"Canonical nodes is an object keyed by node ID",
			"LLM-friendly adapter form is also accepted",
			"Every array edge MUST name both its source and target",
			`{"entry":"search","nodes":[{"input":"find files"}],"edges":{}}`,
			`{"entry":"search","nodes":{"search":{"input":"find files"},"summarize":{"input":"summarize"}},"edges":[{"to":"summarize"}]}`,
			`{"entry":"search","nodes":[{"id":"search","input":"find files"},{"key":"summarize","input":"summarize the file list"}],"edges":[{"from":"search","to":"summarize"}]}`,
			`{"entry":"search","nodes":{"search":{"input":"find files"},"summarize":{"input":"summarize the file list"}},"edges":{"search":["summarize"]}}`,
		} {
			if !strings.Contains(tool.Function.Description, required) {
				t.Errorf("plan_load description is missing %q", required)
			}
		}
		properties := tool.Function.Parameters["properties"].(map[string]interface{})
		nodes := properties["nodes"].(map[string]interface{})
		if nodes["oneOf"] == nil || nodes["type"] != nil {
			t.Fatalf("plan_load nodes schema = %#v, want object-or-array oneOf", nodes)
		}
		edges := properties["edges"].(map[string]interface{})
		if edges["oneOf"] == nil || edges["type"] != nil {
			t.Fatalf("plan_load edges schema = %#v, want object-or-array oneOf", edges)
		}
		if tool.Function.Parameters["additionalProperties"] != false {
			t.Fatalf("plan_load root schema must reject unexpected fields: %#v", tool.Function.Parameters)
		}
		return
	}
	t.Fatal("plan_load tool was not registered")
}

const planLoadSmokeInput = `{
  "entry": "search",
  "nodes": {
    "search": {"input": "find files"},
    "summarize": {"input": "summarize the file list"}
  },
  "edges": {
    "search": ["summarize"]
  }
}`

const planLoadAdapterInput = `{
  "entry": "inspect",
  "nodes": [
    {"id": "inspect", "input": "inspect module boundaries"},
    {"key": "verify", "input": "verify with tests"},
    {"id": "report", "input": "write a report"}
  ],
  "edges": [
    {"from": "inspect", "to": "verify"},
    {"source": "verify", "target": "report"}
  ]
}`

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

func TestPlanLoadAdapterNormalizesLLMFriendlyDAG(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	canonical, err := NormalizePlanLoadArguments(planLoadAdapterInput)
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

func TestPlanLoadRejectsReplacementWhilePreflightIsAuthoritative(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	// Capture handlers before the scope is acquired. This simulates an LLM
	// retaining a stale tool snapshot while Runtime enters preflight or ReAct.
	entries := runtime.planProvider.Tools()
	scope, err := runtime.AcquirePlanActScope("chat-authority-test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.AcquirePlanActScope("second-request"); err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatalf("concurrent scope acquire error = %v, want active-request rejection", err)
	}
	checked := map[string]bool{}
	for _, entry := range entries {
		name := entry.Definition.Function.Name
		if name != "plan_load" && name != "plan_clear" {
			continue
		}
		checked[name] = true
		if _, err := entry.Handler.Execute(context.Background(), `{}`); err == nil || !strings.Contains(err.Error(), "preflight is reserved") {
			t.Fatalf("stale %s handler during preflight error = %v, want scope rejection", name, err)
		}
	}
	if !checked["plan_load"] || !checked["plan_clear"] {
		t.Fatalf("pre-scope tool snapshot checked %v, want plan_load and plan_clear", checked)
	}
	if _, err := runtime.Agent().DirectDispatch(scope.PreflightContext(context.Background()), "plan_load", planLoadAdapterInput); err != nil {
		t.Fatalf("scope preflight plan_load error = %v", err)
	}
	if err := scope.Promote(); err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Definition.Function.Name
		if name != "plan_load" && name != "plan_clear" {
			continue
		}
		if _, err := entry.Handler.Execute(context.Background(), `{}`); err == nil || !strings.Contains(err.Error(), "authoritative preflight plan") {
			t.Fatalf("stale %s handler after promote error = %v, want authority rejection", name, err)
		}
	}
	for _, tool := range runtime.Agent().VisibleTools(context.Background()) {
		if tool.Function.Name == "plan_load" || tool.Function.Name == "plan_clear" {
			t.Fatalf("authoritative visible tools still expose %q", tool.Function.Name)
		}
	}
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", planLoadAdapterInput); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("authoritative plan_load error = %v, want hidden replacement tool", err)
	}

	scope.Release()
	scope.Release()
	available := false
	for _, tool := range runtime.Agent().VisibleTools(context.Background()) {
		if tool.Function.Name == "plan_load" {
			available = true
		}
	}
	if !available {
		t.Fatal("plan_load was not restored after authority unlock")
	}
	result, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", planLoadAdapterInput)
	if err != nil {
		t.Fatalf("plan_load must be available after authority unlock: %v", err)
	}
	if !strings.Contains(result, `"status":"loaded"`) {
		t.Fatalf("unlocked plan_load result = %q", result)
	}
}

func TestPlanPreflightPromptRendersPolicyAndEvidenceContract(t *testing.T) {
	prompt := planPreflightPrompt(PlanPolicy{Effort: "high", RequirePlan: true, MaxForkConcurrency: 3})
	for _, required := range []string{
		"Prefer this canonical object shape", "Compatibility input may use `nodes[]`", "`from`/`source` and `to`/`target`", "put node IDs such as `inspect`", "`verify` beside `entry`",
		"Effort: `high`", "at most 3 nodes concurrently", "exact node IDs `inspect`", "`verify`, and `report`", "planning document alone is not proof",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("preflight prompt %q is missing %q", prompt, required)
		}
	}
	if strings.Contains(prompt, "Never use arrays") {
		t.Fatalf("preflight prompt contradicts adapter contract: %q", prompt)
	}
}

func TestPlanPreflightPromptRendersMediumSerialConstraint(t *testing.T) {
	prompt := planPreflightPrompt(PlanPolicy{Effort: "medium", RequirePlan: true, MaxNodes: 4, RequireSerial: true, MaxForkConcurrency: 1})
	for _, required := range []string{"at most 4 nodes", "one serial chain from entry", "at most 1 nodes concurrently"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("medium preflight prompt %q is missing %q", prompt, required)
		}
	}
}

func TestNormalizePlanLoadArgumentsNormalizesNestedTargetsAndRejectsAmbiguousEdges(t *testing.T) {
	nested := `{"entry":"inspect","nodes":{"inspect":{"input":"inspect"},"report":{"input":"report"}},"edges":{"inspect":[{"to":"report"}]}}`
	canonical, err := NormalizePlanLoadArguments(nested)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(canonical, `"edges":{"inspect":["report"]}`) {
		t.Fatalf("nested target canonical form = %s", canonical)
	}
	ambiguous := `{"entry":"inspect","nodes":[{"id":"inspect","input":"inspect"},{"id":"report","input":"report"}],"edges":[{"to":"report"}]}`
	if _, err := NormalizePlanLoadArguments(ambiguous); err == nil || !strings.Contains(err.Error(), "from") {
		t.Fatalf("ambiguous edge error = %v, want missing source", err)
	}
}

func TestPlanLoadEnforcesEffortPolicy(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	serialFourNodes := `{"entry":"one","nodes":{"one":{"input":"one"},"two":{"input":"two"},"three":{"input":"three"},"four":{"input":"four"}},"edges":{"one":["two"],"two":["three"],"three":["four"]}}`
	parallelFourNodes := `{"entry":"start","nodes":{"start":{"input":"start"},"left":{"input":"left"},"right":{"input":"right"},"finish":{"input":"finish"}},"edges":{"start":["left","right"],"left":["finish"],"right":["finish"]}}`
	fiveNodes := `{"entry":"one","nodes":{"one":{"input":"one"},"two":{"input":"two"},"three":{"input":"three"},"four":{"input":"four"},"five":{"input":"five"}},"edges":{"one":["two"],"two":["three"],"three":["four"],"four":["five"]}}`

	runtime.SetPlanPolicy(PlanPolicy{Effort: "medium", RequirePlan: true, MaxNodes: 4, RequireSerial: true, MaxForkConcurrency: 1})
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", parallelFourNodes); err == nil || !strings.Contains(err.Error(), "serial chain") {
		t.Fatalf("medium parallel plan error = %v, want serial-chain rejection", err)
	}
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", fiveNodes); err == nil || !strings.Contains(err.Error(), "maximum is 4") {
		t.Fatalf("medium five-node plan error = %v, want node-limit rejection", err)
	}
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", serialFourNodes); err != nil {
		t.Fatalf("medium serial plan error = %v", err)
	}
	if runtime.planTool.MaxForkConcurrency != 1 {
		t.Fatalf("medium concurrency = %d, want 1", runtime.planTool.MaxForkConcurrency)
	}

	runtime.SetPlanPolicy(PlanPolicy{Effort: "high", RequirePlan: true, MaxForkConcurrency: 3})
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", parallelFourNodes); err != nil {
		t.Fatalf("high parallel plan error = %v", err)
	}
	if runtime.planTool.MaxForkConcurrency != 3 {
		t.Fatalf("high concurrency = %d, want 3", runtime.planTool.MaxForkConcurrency)
	}

	runtime.SetPlanPolicy(PlanPolicy{Effort: "max", RequirePlan: true})
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", fiveNodes); err != nil {
		t.Fatalf("max plan error = %v", err)
	}
	if runtime.planTool.MaxForkConcurrency != 5 {
		t.Fatalf("max concurrency = %d, want node count 5", runtime.planTool.MaxForkConcurrency)
	}
}

func TestRuntimePreparePlanForcesPlanLoad(t *testing.T) {
	plan := map[string]interface{}{
		"entry": "inspect",
		"nodes": map[string]interface{}{
			"inspect": map[string]string{"input": "inspect request"},
			"report":  map[string]string{"input": "report findings"},
		},
		"edges": map[string][]string{"inspect": {"report"}},
	}
	var choice struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		defer request.Body.Close()
		var payload struct {
			ToolChoice json.RawMessage `json:"tool_choice"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode preflight request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(payload.ToolChoice, &choice); err != nil {
			t.Errorf("decode tool choice: %v", err)
		}
		arguments, _ := json.Marshal(plan)
		_ = json.NewEncoder(writer).Encode(map[string]interface{}{
			"choices": []interface{}{map[string]interface{}{"message": map[string]interface{}{
				"role": "assistant",
				"tool_calls": []interface{}{map[string]interface{}{
					"id": "preflight-plan", "type": "function",
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
	runtime.SetPlanPolicy(PlanPolicy{Effort: "medium", RequirePlan: true, MaxNodes: 4, RequireSerial: true, MaxForkConcurrency: 1})

	result, err := runtime.PreparePlan(context.Background(), "inspect the repository")
	if err != nil {
		t.Fatal(err)
	}
	if choice.Type != "function" || choice.Function.Name != "plan_load" {
		t.Fatalf("tool_choice = %+v, want forced plan_load", choice)
	}
	if result.Arguments == "" || !strings.Contains(result.Result, `"status":"loaded"`) {
		t.Fatalf("preflight result = %+v", result)
	}
}

func TestRuntimePrepareReplanForcesPlanLoadForExplicitLiteRecovery(t *testing.T) {
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
	runtime.SetPlanPolicy(PlanPolicy{Effort: "lite"})

	result, err := runtime.PrepareReplan(context.Background(), ReplanRequest{
		Objective:    "build the release",
		PreviousPlan: `{"entry":"build"}`,
		Failure:      `node "build": compiler failed`,
		Evidence:     "node=lint status=completed",
	})
	if err != nil {
		t.Fatal(err)
	}
	var choice struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(payload.ToolChoice, &choice); err != nil {
		t.Fatal(err)
	}
	if choice.Type != "function" || choice.Function.Name != "plan_load" {
		t.Fatalf("tool_choice = %+v, want forced plan_load", choice)
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

func TestRuntimeProjectScopedToolsUseBoundProject(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	projectA := t.TempDir()
	projectB := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectA, "marker.txt"), []byte("project-a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectB, "marker.txt"), []byte("project-b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if result, err := runtime.Agent().DirectDispatch(context.Background(), "read_file", `{"path":"marker.txt"}`); err == nil || result == "unsafe" {
		t.Fatal("unbound read_file must fail closed")
	}
	if err := runtime.BindProjectRoot(projectA); err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Agent().DirectDispatch(context.Background(), "read_file", `{"path":"marker.txt"}`)
	if err != nil || result != "project-a" {
		t.Fatalf("project A read = %q, err=%v", result, err)
	}
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "read_file", `{"path":"../marker.txt"}`); err == nil {
		t.Fatal("read_file traversal must fail")
	}
	if err := runtime.BindProjectRoot(projectB); err != nil {
		t.Fatal(err)
	}
	result, err = runtime.Agent().DirectDispatch(context.Background(), "read_file", `{"path":"marker.txt"}`)
	if err != nil || result != "project-b" {
		t.Fatalf("project B read = %q, err=%v", result, err)
	}
	result, err = runtime.Agent().DirectDispatch(context.Background(), "bash", `{"command":"pwd","timeout":10}`)
	if err != nil || !strings.Contains(result, filepath.Base(projectB)) {
		t.Fatalf("bash did not use project root: result=%q err=%v", result, err)
	}
}

func TestResolveAccountForBranchIsStableAndRoleScoped(t *testing.T) {
	pool := api.NewAccountPool(
		&api.Account{Name: "subagent-1", Provider: api.ProviderOpenAI},
		&api.Account{Name: "subagent-2", Provider: api.ProviderOpenAI},
		&api.Account{Name: "agent-1", Provider: api.ProviderOpenAI},
	)
	first, err := ResolveAccountForBranch(pool, RoleSubAgent, "plan:left")
	if err != nil {
		t.Fatal(err)
	}
	again, err := ResolveAccountForBranch(pool, RoleSubAgent, "plan:left")
	if err != nil || first.Name != again.Name {
		t.Fatalf("unstable account selection: first=%v again=%v err=%v", first, again, err)
	}
	seen := map[string]bool{}
	for index := 0; index < 64; index++ {
		account, err := ResolveAccountForBranch(pool, RoleSubAgent, fmt.Sprintf("plan:branch-%d", index))
		if err != nil {
			t.Fatal(err)
		}
		seen[account.Name] = true
	}
	if !seen["subagent-1"] || !seen["subagent-2"] {
		t.Fatalf("role-scoped accounts were not distributed: %v", seen)
	}
}

func TestRuntimePlanBranchBindingBuildsPrivateFactories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	content := `roles:
  agent:
    - model: main-model
      base_url: http://localhost
      api_key: test-key
  subagent:
    - model: child-one
      base_url: http://localhost
      api_key: test-key
    - model: child-two
      base_url: http://localhost
      api_key: test-key
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(RuntimeConfig{AccountsPath: path, ToolCallTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Shutdown()
	runtime.SetPlanBranchBinding(PlanBranchBinding{
		SessionID: "session-1", WorkspaceID: "workspace-1", PlanID: "plan-1", EntryNodeID: "start", TraceID: "trace-1",
	})
	entry := runtime.resolvePlanBranchRuntime("start")
	left := runtime.resolvePlanBranchRuntime("left")
	if entry.Role != string(RoleAgent) || left.Role != string(RoleSubAgent) {
		t.Fatalf("roles entry=%q left=%q", entry.Role, left.Role)
	}
	if left.SessionID != "session-1" || left.WorkspaceID != "workspace-1" || left.TraceID != "trace-1:left" {
		t.Fatalf("left runtime = %+v", left)
	}
	if entry.AgentFactory == nil || left.AgentFactory == nil {
		t.Fatalf("missing private factories: entry=%+v left=%+v", entry, left)
	}
	if reflect.ValueOf(entry.AgentFactory).Pointer() == reflect.ValueOf(left.AgentFactory).Pointer() {
		t.Fatal("branch factories share one instance")
	}

	runtime.SetPlanBranchBinding(PlanBranchBinding{SessionID: "session-1", AccountID: "subagent-2"})
	override := runtime.resolvePlanBranchRuntime("left")
	if override.AccountID != "subagent-2" {
		t.Fatalf("explicit account override = %q", override.AccountID)
	}

	runtime.SetPlanBranchBinding(PlanBranchBinding{})
	if fallback := runtime.resolvePlanBranchRuntime("left"); fallback.AgentFactory != nil {
		t.Fatalf("empty binding must preserve legacy factory path: %+v", fallback)
	}
}

func TestFrameworkMCPValidation(t *testing.T) {
	tests := []struct {
		name string
		cfg  MCPServer
		ok   bool
	}{
		{name: "stdio inferred", cfg: MCPServer{Name: "fs", Command: "npx"}, ok: true},
		{name: "sse inferred", cfg: MCPServer{Name: "web", URL: "http://localhost"}, ok: true},
		{name: "empty name", cfg: MCPServer{Command: "x"}},
		{name: "missing command", cfg: MCPServer{Name: "x", Transport: "stdio"}},
		{name: "invalid transport", cfg: MCPServer{Name: "x", Transport: "http"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := toFrameworkMCP(tt.cfg)
			if (err == nil) != tt.ok {
				t.Fatalf("err=%v ok=%v", err, tt.ok)
			}
		})
	}
}

func newTestRuntime(t testing.TB) *Runtime {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	content := `roles:
  agent:
    - model: test-model
      base_url: http://localhost
      api_key: test-key-not-used
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(RuntimeConfig{AccountsPath: path, ToolCallTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}
