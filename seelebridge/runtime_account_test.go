package seelebridge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	seelaccount "github.com/RedHuang-0622/seelex/seelebridge/account"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/config"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
)

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
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Limits["agent-1"]; got != (config.AccountLimits{ContextWindow: 200_000, MaxOutputTokens: 8_192}) {
		t.Fatalf("agent limits = %+v", got)
	}
	if got := loaded.Limits["subagent-1"]; got != (config.AccountLimits{ContextWindow: 32_768, MaxOutputTokens: 2_048}) {
		t.Fatalf("subagent limits = %+v", got)
	}
	if account := seelaccount.ByName(loaded.Specs, "agent-1"); account == nil || account.Provider != "openai" || account.MaxTokens != 8_192 {
		t.Fatalf("agent provider config = %+v", account)
	}
	if account := seelaccount.ByName(loaded.Specs, "subagent-1"); account == nil || account.MaxTokens != 2_048 {
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
			if _, err := config.Load(path); err == nil {
				t.Fatal("unsafe context limits should fail")
			}
		})
	}
}
func TestMissingAccountsUsesEnvironmentCredential(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	path := filepath.Join(t.TempDir(), "missing-accounts.yaml")

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	specs := loaded.Specs
	if len(specs) != 1 || specs[0].APIKey != "test-key" {
		t.Fatalf("fallback accounts = %+v", specs)
	}
	if len(loaded.AvailableRoles) != 1 || loaded.AvailableRoles[0] != model.RoleAgent {
		t.Fatalf("fallback roles = %v", loaded.AvailableRoles)
	}
	if limits := loaded.Limits[specs[0].Name]; limits.ContextWindow != config.DefaultContextWindow || limits.MaxOutputTokens != config.DefaultMaxOutputTokens {
		t.Fatalf("fallback limits = %+v", limits)
	}
}
func TestResolveAccountForBranchIsStableAndRoleScoped(t *testing.T) {
	pool := accountpool.New[agent.Completer]()
	for _, name := range []string{"subagent-1", "subagent-2", "agent-1"} {
		spec := model.AccountSpec{Name: name, Provider: "openai", Model: "test-model", MaxTokens: 8192}
		if err := pool.Register(accountpool.Account[agent.Completer]{
			ID: name, Value: seelaccount.ClientFor(spec), MaxConcurrency: 1,
			Metadata: map[string]string{"provider": "openai", "model": "test-model"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := seelaccount.ResolveForBranch(pool, model.RoleSubAgent, "plan:left")
	if err != nil {
		t.Fatal(err)
	}
	again, err := seelaccount.ResolveForBranch(pool, model.RoleSubAgent, "plan:left")
	if err != nil || first != again {
		t.Fatalf("unstable account selection: first=%q again=%q err=%v", first, again, err)
	}
	seen := map[string]bool{}
	for index := 0; index < 64; index++ {
		account, err := seelaccount.ResolveForBranch(pool, model.RoleSubAgent, fmt.Sprintf("plan:branch-%d", index))
		if err != nil {
			t.Fatal(err)
		}
		seen[account] = true
	}
	if !seen["subagent-1"] || !seen["subagent-2"] {
		t.Fatalf("role-scoped accounts were not distributed: %v", seen)
	}
}
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
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Specs) != 2 || len(loaded.AvailableRoles) != 2 {
		t.Fatalf("unexpected accounts=%d roles=%d", len(loaded.Specs), len(loaded.AvailableRoles))
	}
	spec, err := model.ResolveAccountSpec(loaded.Specs, model.RoleSubAgent)
	if err != nil || spec.Name != "subagent-1" {
		t.Fatalf("resolved account=%v err=%v", spec, err)
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
	runtime.SetPlanBranchBinding(dto.PlanBranchBinding{
		SessionID: "session-1", WorkspaceID: "workspace-1", PlanID: "plan-1", EntryNodeID: "start", TraceID: "trace-1",
	})
	binding := runtime.currentPlanBranchBinding()
	if role := roleForPlanBranch(binding, "start"); role != model.RoleAgent {
		t.Fatalf("entry role = %q, want agent", role)
	}
	if role := roleForPlanBranch(binding, "left"); role != model.RoleSubAgent {
		t.Fatalf("left role = %q, want subagent", role)
	}
	if traceID := branchTraceID(binding, "left"); traceID != "trace-1:left" {
		t.Fatalf("branch trace ID = %q", traceID)
	}
	entryAccount, err := runtime.resolvePlanBranchAccount(binding, model.RoleAgent, "start")
	if err != nil || entryAccount != "agent-1" {
		t.Fatalf("entry account = %q err=%v", entryAccount, err)
	}
	leftAccount, err := runtime.resolvePlanBranchAccount(binding, model.RoleSubAgent, "left")
	if err != nil || (leftAccount != "subagent-1" && leftAccount != "subagent-2") {
		t.Fatalf("left account = %q err=%v", leftAccount, err)
	}

	runtime.SetPlanBranchBinding(dto.PlanBranchBinding{SessionID: "session-1", AccountID: "subagent-2"})
	pinned := runtime.currentPlanBranchBinding()
	override, err := runtime.resolvePlanBranchAccount(pinned, model.RoleSubAgent, "left")
	if err != nil || override != "subagent-2" {
		t.Fatalf("explicit account override = %q err=%v", override, err)
	}

	runtime.SetPlanBranchBinding(dto.PlanBranchBinding{SessionID: "session-1", AccountID: "missing-account"})
	missing := runtime.currentPlanBranchBinding()
	if _, err := runtime.resolvePlanBranchAccount(missing, model.RoleSubAgent, "left"); err == nil {
		t.Fatal("unavailable pinned account must fail")
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
