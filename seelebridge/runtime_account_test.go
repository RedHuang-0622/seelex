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
	seelaccount "github.com/RedHuang-0622/seelex/backend/engine/account"
	"github.com/RedHuang-0622/seelex/backend/limit"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/config"
	"github.com/RedHuang-0622/seelex/internal/model"
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

// TestRuntimeContextWindowOverrideFromLimits 钉住「窗口可以配在 seelex.yaml」这条
// 2026-10-15 的需求：`limits.context_window > 0` 时，Runtime 报出的窗口是它，而不是
// 账号档的值——窗口是预算的基数（预算 = 窗口 − 账号档 max_tokens − 窗口÷安全除数），
// 而账号档是本地凭据文件、不进版本库、也不参与 bootseed 同步；配在这里才随出厂档分发。
//
// 两件事必须同时成立，缺一条这条旋钮就会被读成"改了个没生效的键"：
//   - 覆盖是**全局**的：切到子代理账号后窗口仍是覆盖值（子账号账号档里的 32768 被盖掉）；
//   - 输出预留**不受覆盖影响**：MaxOutputTokens 仍取账号档（8192 / 2048）——窗口与输出
//     预留是两族数，覆盖窗口不该顺手改掉输出预留。
func TestRuntimeContextWindowOverrideFromLimits(t *testing.T) {
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
	runtime, err := NewRuntime(RuntimeConfig{
		AccountsPath:    path,
		ToolCallTimeout: time.Second,
		Limits:          limit.Limits{ContextWindow: 400_000},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Shutdown()
	if runtime.ContextWindow() != 400_000 || runtime.MaxOutputTokens() != 8_192 {
		t.Fatalf("覆盖后的窗口/输出预留 = %d/%d，want 400000/8192（窗口取覆盖值，输出预留仍取账号档）",
			runtime.ContextWindow(), runtime.MaxOutputTokens())
	}
	if !runtime.SelectAccount("subagent-1") {
		t.Fatal("select subagent account")
	}
	if runtime.ContextWindow() != 400_000 {
		t.Fatalf("覆盖必须是全局的（子代理账号也用它），got %d", runtime.ContextWindow())
	}
	if runtime.MaxOutputTokens() != 2_048 {
		t.Fatalf("输出预留仍应取账号档（2048），got %d", runtime.MaxOutputTokens())
	}
}

// TestRuntimeAccountWindowPriority 钉住「每账号上下文窗口」（设置 → 账号）的优先级
// 与"按下去就作数"：
//
//	账号级覆盖（界面设过） > limits.context_window（seelex.yaml 的全局值） > 账号档自报值
//
// 三跳少一跳都会出事：少第一跳 = 界面设了没用；少第二跳 = 全局值被账号档盖掉；
// 少第三跳 = 没配过的账号没有窗口。顺带钉住两族数不互相污染：窗口覆盖不动输出预留。
func TestRuntimeAccountWindowPriority(t *testing.T) {
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
	runtime, err := NewRuntime(RuntimeConfig{
		AccountsPath:    path,
		ToolCallTimeout: time.Second,
		Limits:          limit.Limits{ContextWindow: 400_000},
		AccountWindows:  map[string]int{"subagent-1": 1_000_000},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Shutdown()

	// 第二跳：没被账号级设过的账号走全局值（账号档自报的 200000 被压掉）。
	if got := runtime.ContextWindowFor("agent-1"); got != 400_000 {
		t.Fatalf("ContextWindowFor(agent-1) = %d, want 400000（全局值压账号档）", got)
	}
	// 第一跳：账号级覆盖压全局值（子代理账号单独设 1M）。
	if got := runtime.ContextWindowFor("subagent-1"); got != 1_000_000 {
		t.Fatalf("ContextWindowFor(subagent-1) = %d, want 1000000（账号级压全局）", got)
	}
	// ContextWindow() 跟着选中账号走，且不改输出预留（两族数分开）。
	if !runtime.SelectAccount("subagent-1") {
		t.Fatal("select subagent account")
	}
	if runtime.ContextWindow() != 1_000_000 || runtime.MaxOutputTokens() != 2_048 {
		t.Fatalf("选中子代理账号后 = %d/%d, want 1000000/2048", runtime.ContextWindow(), runtime.MaxOutputTokens())
	}
	if !runtime.SelectAccount("agent-1") {
		t.Fatal("select agent account")
	}
	if runtime.ContextWindow() != 400_000 || runtime.MaxOutputTokens() != 8_192 {
		t.Fatalf("选中主账号后 = %d/%d, want 400000/8192", runtime.ContextWindow(), runtime.MaxOutputTokens())
	}

	// 界面上改一格：立刻生效（不需要重启），清单里也读得到。
	runtime.SetAccountWindow("agent-1", 600_000)
	if got := runtime.ContextWindowFor("agent-1"); got != 600_000 {
		t.Fatalf("SetAccountWindow 后立刻生效：%d, want 600000", got)
	}
	if windows := runtime.AccountWindows(); windows["agent-1"] != 600_000 || windows["subagent-1"] != 1_000_000 {
		t.Fatalf("AccountWindows() = %v, want agent-1=600000 / subagent-1=1000000", windows)
	}
	// 清除（window <= 0）→ 回落到全局值；清掉的账号从清单里消失。
	runtime.SetAccountWindow("agent-1", 0)
	if got := runtime.ContextWindowFor("agent-1"); got != 400_000 {
		t.Fatalf("清除覆盖后 = %d, want 400000（回落全局值）", got)
	}
	if _, ok := runtime.AccountWindows()["agent-1"]; ok {
		t.Fatalf("清除后不该还在清单里：%v", runtime.AccountWindows())
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
func TestRuntimeToleratesEmptyAccountsWithWarning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	if err := os.WriteFile(path, []byte("roles: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(RuntimeConfig{AccountsPath: path})
	if err != nil {
		t.Fatalf("empty accounts should start with fallback, got: %v", err)
	}
	defer runtime.Shutdown()
	if len(runtime.StartupWarnings()) == 0 {
		t.Fatal("empty accounts should surface a startup warning")
	}
}
func TestRuntimeToleratesLegacyAccountsListWithWarning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	content := "accounts:\n  - name: main\n    model: test-model\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(RuntimeConfig{AccountsPath: path})
	if err != nil {
		t.Fatalf("legacy accounts-list should start with fallback, got: %v", err)
	}
	defer runtime.Shutdown()
	if len(runtime.StartupWarnings()) == 0 {
		t.Fatal("legacy accounts-list should surface a startup warning")
	}
}
