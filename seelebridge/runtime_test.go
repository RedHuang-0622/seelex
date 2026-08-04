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

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/seelex/sessionstore"
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

func TestRuntimeNewMainSessionWithIDKeepsDurableResumeIdentity(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	root := t.TempDir()
	router, err := sessionstore.NewRouter(filepath.Join(root, "session-storage.json"), root)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	runtime.AttachHistoryRouter(router)

	session, err := runtime.NewMainSessionWithID("resume-session-42", nil)
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionID() != "resume-session-42" {
		t.Fatalf("framework session ID = %q, want durable resume key", session.SessionID())
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

	loaded, err := loadSimplifiedConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	specs := loaded.Specs
	if len(specs) != 1 || specs[0].APIKey != "test-key" {
		t.Fatalf("fallback accounts = %+v", specs)
	}
	if len(loaded.AvailableRoles) != 1 || loaded.AvailableRoles[0] != RoleAgent {
		t.Fatalf("fallback roles = %v", loaded.AvailableRoles)
	}
	if limits := loaded.Limits[specs[0].Name]; limits.ContextWindow != defaultContextWindow || limits.MaxOutputTokens != defaultMaxOutputTokens {
		t.Fatalf("fallback limits = %+v", limits)
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
	loaded, err := loadSimplifiedConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Specs) != 2 || len(loaded.AvailableRoles) != 2 {
		t.Fatalf("unexpected accounts=%d roles=%d", len(loaded.Specs), len(loaded.AvailableRoles))
	}
	spec, err := ResolveAccountSpec(loaded.Specs, RoleSubAgent)
	if err != nil || spec.Name != "subagent-1" {
		t.Fatalf("resolved account=%v err=%v", spec, err)
	}
}

func TestAccountLimitsHonorDefaultsAndAccountOverrides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	content := `defaults:
  provider: openai
  context_window: 200000
  max_tokens: 8192
roles:
  agent:
    - model: main-model
      base_url: http://localhost
      api_key: test-key
  subagent:
    - model: child-model
      base_url: http://localhost
      api_key: test-key
      context_window: 32768
      max_tokens: 2048
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadSimplifiedConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Limits["agent-1"]; got != (accountLimits{ContextWindow: 200_000, MaxOutputTokens: 8_192}) {
		t.Fatalf("agent limits = %+v", got)
	}
	if got := loaded.Limits["subagent-1"]; got != (accountLimits{ContextWindow: 32_768, MaxOutputTokens: 2_048}) {
		t.Fatalf("subagent limits = %+v", got)
	}
	if account := accountByName(loaded.Specs, "agent-1"); account == nil || account.Provider != "openai" || account.MaxTokens != 8_192 {
		t.Fatalf("agent provider config = %+v", account)
	}
	if account := accountByName(loaded.Specs, "subagent-1"); account == nil || account.MaxTokens != 2_048 {
		t.Fatalf("subagent provider config = %+v", account)
	}
}

func TestAccountLimitsRejectUnsafeValues(t *testing.T) {
	tests := []struct {
		name          string
		contextWindow int
		maxTokens     int
	}{
		{name: "zero context", contextWindow: 0, maxTokens: 1_024},
		{name: "zero output", contextWindow: 8_192, maxTokens: 0},
		{name: "output consumes safe window", contextWindow: 8_192, maxTokens: 7_168},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "accounts.yaml")
			content := fmt.Sprintf(`defaults:
  context_window: %d
  max_tokens: %d
roles:
  agent:
    - model: test-model
      base_url: http://localhost
      api_key: test-key
`, tt.contextWindow, tt.maxTokens)
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := loadSimplifiedConfig(path); err == nil {
				t.Fatal("unsafe context limits should fail")
			}
		})
	}
}

func TestRuntimeExposesSelectedAccountLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	content := `defaults:
  context_window: 200000
  max_tokens: 8192
roles:
  agent:
    - model: main-model
      base_url: http://localhost
      api_key: test-key
  subagent:
    - model: child-model
      base_url: http://localhost
      api_key: test-key
      context_window: 32768
      max_tokens: 2048
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(RuntimeConfig{AccountsPath: path, ToolCallTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Shutdown()
	if runtime.ContextWindow() != 200_000 || runtime.MaxOutputTokens() != 8_192 {
		t.Fatalf("default runtime limits = %d/%d", runtime.ContextWindow(), runtime.MaxOutputTokens())
	}
	if !runtime.SelectAccount("subagent-1") {
		t.Fatal("select subagent account")
	}
	if runtime.ContextWindow() != 32_768 || runtime.MaxOutputTokens() != 2_048 {
		t.Fatalf("selected runtime limits = %d/%d", runtime.ContextWindow(), runtime.MaxOutputTokens())
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

	for _, tool := range runtime.registry.registry.Tools() {
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

func TestPlanReplanPromptRendersCopyableManualRecovery(t *testing.T) {
	prompt := replanPrompt(PlanPolicy{Effort: "high", MaxForkConcurrency: 3})
	for _, required := range []string{
		"Copy this complete recovery shape first", `"entry":"diagnose"`, `"kind":"manual"`,
		"only top-level keys", "Effort: `high`",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("replan prompt %q is missing %q", prompt, required)
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

func TestNormalizePlanLoadArgumentsRecoversOrderedEdgeTargetList(t *testing.T) {
	input := `{"entry":"inspect","nodes":{"inspect":{"input":"inspect"},"implement":{"input":"implement"},"verify":{"input":"verify"}},"edges":["implement","verify"]}`
	canonical, err := NormalizePlanLoadArguments(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"inspect":["implement"]`, `"implement":["verify"]`} {
		if !strings.Contains(canonical, expected) {
			t.Fatalf("ordered target list canonical plan %s is missing %s", canonical, expected)
		}
	}

	duplicate := `{"entry":"inspect","nodes":{"inspect":{"input":"inspect"},"verify":{"input":"verify"}},"edges":["verify","verify"]}`
	if _, err := NormalizePlanLoadArguments(duplicate); err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatalf("duplicate ordered edge target error = %v", err)
	}
}

func TestNormalizePlanLoadArgumentsMergesReferencedTopLevelNodeSpecs(t *testing.T) {
	legacy := `{"entry":"inspect","nodes":{"inspect":{"input":"read"}},"verify":{"input":"check"},"report":{"input":"summarize"},"edges":{"inspect":["verify"],"verify":["report"]}}`
	canonical, err := NormalizePlanLoadArguments(legacy)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"inspect":{"input":"read"}`, `"verify":{"input":"check"}`, `"report":{"input":"summarize"}`} {
		if !strings.Contains(canonical, expected) {
			t.Fatalf("canonical plan %s is missing %s", canonical, expected)
		}
	}

	unsafe := `{"entry":"inspect","nodes":{"inspect":{"input":"read"}},"item":{"input":"metadata"},"edges":{}}`
	if _, err := NormalizePlanLoadArguments(unsafe); err == nil || !strings.Contains(err.Error(), "unexpected top-level field") {
		t.Fatalf("unreferenced top-level field error = %v", err)
	}
}

func TestNormalizePlanLoadArgumentsNormalizesExplicitEdgeChain(t *testing.T) {
	input := `{"entry":"inspect","nodes":{"inspect":{"input":"read"},"verify":{"input":"check"},"report":{"input":"summarize"}},"edges":"inspect -> verify -> report"}`
	canonical, err := NormalizePlanLoadArguments(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"inspect":["verify"]`, `"verify":["report"]`} {
		if !strings.Contains(canonical, expected) {
			t.Fatalf("canonical plan %s is missing %s", canonical, expected)
		}
	}

	if _, err := NormalizePlanLoadArguments(`{"entry":"inspect","nodes":{"inspect":{"input":"read"}},"edges":"inspect"}`); err == nil || !strings.Contains(err.Error(), "explicit chain") {
		t.Fatalf("ambiguous edge string error = %v", err)
	}
}

func TestPlanLoadEnforcesEffortPolicy(t *testing.T) {
	runtime := newTestRuntime(t)
	defer runtime.Shutdown()
	runtime.RegisterBuiltins()

	serialFourNodes := `{"entry":"one","nodes":{"one":{"input":"one"},"two":{"input":"two"},"three":{"input":"three"},"four":{"input":"four"}},"edges":{"one":["two"],"two":["three"],"three":["four"]}}`
	parallelFourNodes := `{"entry":"start","nodes":{"start":{"input":"start"},"left":{"input":"left"},"right":{"input":"right"},"finish":{"input":"finish"}},"edges":{"start":["left","right"],"left":["finish"],"right":["finish"]}}`
	fiveNodes := `{"entry":"one","nodes":{"one":{"input":"one"},"two":{"input":"two"},"three":{"input":"three"},"four":{"input":"four"},"five":{"input":"five"}},"edges":{"one":["two"],"two":["three"],"three":["four"],"four":["five"]}}`

	runtime.SetPlanPolicy(PlanPolicy{Effort: "medium", MaxNodes: 4, RequireSerial: true, MaxForkConcurrency: 1})
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", parallelFourNodes); err == nil || !strings.Contains(err.Error(), "serial chain") {
		t.Fatalf("medium parallel plan error = %v, want serial-chain rejection", err)
	}
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", fiveNodes); err == nil || !strings.Contains(err.Error(), "maximum is 4") {
		t.Fatalf("medium five-node plan error = %v, want node-limit rejection", err)
	}
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", serialFourNodes); err != nil {
		t.Fatalf("medium serial plan error = %v", err)
	}
	if runtime.planProvider.maxForkConcurrency != 1 {
		t.Fatalf("medium concurrency = %d, want 1", runtime.planProvider.maxForkConcurrency)
	}

	runtime.SetPlanPolicy(PlanPolicy{Effort: "high", MaxForkConcurrency: 3})
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", parallelFourNodes); err != nil {
		t.Fatalf("high parallel plan error = %v", err)
	}
	if runtime.planProvider.maxForkConcurrency != 3 {
		t.Fatalf("high concurrency = %d, want 3", runtime.planProvider.maxForkConcurrency)
	}

	runtime.SetPlanPolicy(PlanPolicy{Effort: "max"})
	if _, err := runtime.Agent().DirectDispatch(context.Background(), "plan_load", fiveNodes); err != nil {
		t.Fatalf("max plan error = %v", err)
	}
	if runtime.planProvider.maxForkConcurrency != 5 {
		t.Fatalf("max concurrency = %d, want node count 5", runtime.planProvider.maxForkConcurrency)
	}
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
	runtime.SetPlanPolicy(PlanPolicy{Effort: "lite"})
	// replan 恢复路径经 plan_load（plan 工具面归位后需 goal 激活可见）。
	runtime.SetRuntimeVisibilityProjection(RuntimeVisibilityProjection{GoalSkillActive: true})

	result, err := runtime.PrepareReplan(context.Background(), ReplanRequest{
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
	result, err = runtime.Agent().DirectDispatch(context.Background(), "bash", `{"command":"pwd && ls -la","timeout":10}`)
	if err != nil || !strings.Contains(result, filepath.Base(projectB)) {
		t.Fatalf("bash did not use project root: result=%q err=%v", result, err)
	}
}

func TestResolveAccountForBranchIsStableAndRoleScoped(t *testing.T) {
	pool := accountpool.New[agent.Completer]()
	for _, name := range []string{"subagent-1", "subagent-2", "agent-1"} {
		spec := accountSpec{Name: name, Provider: "openai", Model: "test-model", MaxTokens: 8192}
		if err := pool.Register(accountpool.Account[agent.Completer]{
			ID: name, Value: clientFor(spec), MaxConcurrency: 1,
			Metadata: map[string]string{"provider": "openai", "model": "test-model"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := ResolveAccountForBranch(pool, RoleSubAgent, "plan:left")
	if err != nil {
		t.Fatal(err)
	}
	again, err := ResolveAccountForBranch(pool, RoleSubAgent, "plan:left")
	if err != nil || first != again {
		t.Fatalf("unstable account selection: first=%q again=%q err=%v", first, again, err)
	}
	seen := map[string]bool{}
	for index := 0; index < 64; index++ {
		account, err := ResolveAccountForBranch(pool, RoleSubAgent, fmt.Sprintf("plan:branch-%d", index))
		if err != nil {
			t.Fatal(err)
		}
		seen[account] = true
	}
	if !seen["subagent-1"] || !seen["subagent-2"] {
		t.Fatalf("role-scoped accounts were not distributed: %v", seen)
	}
}

func TestRuntimePlanBranchBindingResolvesAccountsByRoleAndPin(t *testing.T) {
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
	binding := runtime.currentPlanBranchBinding()
	if role := roleForPlanBranch(binding, "start"); role != RoleAgent {
		t.Fatalf("entry role = %q, want agent", role)
	}
	if role := roleForPlanBranch(binding, "left"); role != RoleSubAgent {
		t.Fatalf("left role = %q, want subagent", role)
	}
	if traceID := branchTraceID(binding, "left"); traceID != "trace-1:left" {
		t.Fatalf("branch trace ID = %q", traceID)
	}
	entryAccount, err := runtime.resolvePlanBranchAccount(binding, RoleAgent, "start")
	if err != nil || entryAccount != "agent-1" {
		t.Fatalf("entry account = %q err=%v", entryAccount, err)
	}
	leftAccount, err := runtime.resolvePlanBranchAccount(binding, RoleSubAgent, "left")
	if err != nil || (leftAccount != "subagent-1" && leftAccount != "subagent-2") {
		t.Fatalf("left account = %q err=%v", leftAccount, err)
	}

	runtime.SetPlanBranchBinding(PlanBranchBinding{SessionID: "session-1", AccountID: "subagent-2"})
	pinned := runtime.currentPlanBranchBinding()
	override, err := runtime.resolvePlanBranchAccount(pinned, RoleSubAgent, "left")
	if err != nil || override != "subagent-2" {
		t.Fatalf("explicit account override = %q err=%v", override, err)
	}

	runtime.SetPlanBranchBinding(PlanBranchBinding{SessionID: "session-1", AccountID: "missing-account"})
	missing := runtime.currentPlanBranchBinding()
	if _, err := runtime.resolvePlanBranchAccount(missing, RoleSubAgent, "left"); err == nil {
		t.Fatal("unavailable pinned account must fail")
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
	// ToolCallTimeout 放宽到 30s：注册表 WithCallTimeout 会封顶单次工具调用，
	// 而 scoped bash 测试显式要求 10s 窗口（PowerShell 冷启动在并行测试负载下
	// 可能超过 1s）。工具自身的显式超时仍是生效边界。
	runtime, err := NewRuntime(RuntimeConfig{AccountsPath: path, ToolCallTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	// 测试基座默认 goal skill 激活（plan 工具可见——多数 plan 测试需要）。
	runtime.SetRuntimeVisibilityProjection(RuntimeVisibilityProjection{GoalSkillActive: true})
	return runtime
}
