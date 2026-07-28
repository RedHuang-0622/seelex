package seelebridge

import (
	"context"
	"fmt"
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

func newTestRuntime(t *testing.T) *Runtime {
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
