package assemble

// runtime_account_hot_add_test.go — 「手动添加账号」在运行时的落点判据：
// RegisterAccount 必须让新账号**真的进池**（能承流、能按角色被选中）并**进管理器**
// （限额/窗口读数、按名字查余额）。只写账号档不算数。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/backend/engine/account"
	"github.com/RedHuang-0622/seelex/internal/config"
	"github.com/RedHuang-0622/seelex/internal/model"
)

const hotAddAccountsFixture = `defaults:
  provider: openai
  max_tokens: 16384

roles:
  agent:
  - model: deepseek-flash
    base_url: https://api.deepseek.com
    api_key: sk-first
    reasoning_effort: session
`

// newHotAddRuntime 摆一个只有一条 agent 账号的真实运行时。
func newHotAddRuntime(t *testing.T) *Runtime {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	if err := os.WriteFile(path, []byte(hotAddAccountsFixture), 0o600); err != nil {
		t.Fatalf("写账号档现场失败: %v", err)
	}
	runtime, err := NewRuntime(RuntimeConfig{AccountsPath: path})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	t.Cleanup(runtime.Shutdown)
	return runtime
}

// TestRegisterAccountHotAddsIntoPoolAndManager：注册之后账号列表里能看到它（池），
// 限额与生效窗口也能按名字读到（管理器），且重名被拒而不是覆盖。
func TestRegisterAccountHotAddsIntoPoolAndManager(t *testing.T) {
	runtime := newHotAddRuntime(t)
	if got := len(runtime.Accounts()); got != 1 {
		t.Fatalf("起始账号数 = %d，期望 1", got)
	}

	spec := model.AccountSpec{
		Name: "agent-2", Provider: "openai", BaseURL: "https://api.deepseek.com",
		APIKey: "sk-second", Model: "deepseek-flash", MaxTokens: 8192,
		ContextWindow: 200_000, MaxOutputTokens: 8192, MaxConcurrency: 1,
		Temperature: 0.3, ReasoningEffort: "session", Role: model.RoleAgent,
	}
	limits := config.AccountLimits{ContextWindow: 300_000, MaxOutputTokens: 8192}
	if err := runtime.RegisterAccount(spec, limits); err != nil {
		t.Fatalf("RegisterAccount: %v", err)
	}

	accounts := runtime.Accounts()
	if len(accounts) != 2 {
		t.Fatalf("注册后账号数 = %d，期望 2（新账号必须进池）", len(accounts))
	}
	var found *Account
	for index := range accounts {
		if accounts[index].Name == "agent-2" {
			found = &accounts[index]
		}
	}
	if found == nil {
		t.Fatalf("账号列表里没有 agent-2: %#v", accounts)
	}
	if found.Provider != "openai" || found.Model != "deepseek-flash" {
		t.Fatalf("agent-2 的路由摘要不对: %#v", *found)
	}

	// 管理器侧：限额按名字可读（界面逐账号显示窗口要靠它）。
	if got := runtime.accounts.LimitsFor("agent-2").ContextWindow; got != 300_000 {
		t.Fatalf("agent-2 的账号档窗口 = %d，期望 300000", got)
	}
	// 能按 agent 角色被选中（新账号真正可用，而不只是"出现在列表里"）。
	role := account.ForRole(runtime.pool, model.RoleAgent)
	if len(role) != 2 {
		t.Fatalf("agent 角色候选数 = %d，期望 2", len(role))
	}

	// 重名必须报错且不覆盖既有条目。
	if err := runtime.RegisterAccount(spec, limits); err == nil {
		t.Fatalf("重名注册应报错")
	}
	if got := len(runtime.Accounts()); got != 2 {
		t.Fatalf("重名注册后账号数 = %d，期望仍为 2", got)
	}
}

// TestAccountBalanceUsesCredentialFromAccountsFile：完整一跳——账号档里的 base_url 与
// api_key 被加载成 AccountSpec，再交给账号域的探测。这是"余额链路上最容易错"的地方
// （凭据拿错账号 / 端点拼错都会让读数变成别人的或直接失败），用真 httptest 上游钉住。
func TestAccountBalanceUsesCredentialFromAccountsFile(t *testing.T) {
	var gotAuth, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotAuth = request.Header.Get("Authorization")
		gotPath = request.URL.Path
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"is_available":true,"balance_infos":[` +
			`{"currency":"CNY","total_balance":"42.50","granted_balance":"2.50","topped_up_balance":"40.00"}]}`))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "accounts.yaml")
	fixture := "defaults:\n  provider: openai\n  max_tokens: 16384\n\nroles:\n  agent:\n" +
		"  - model: deepseek-flash\n    base_url: " + server.URL + "\n    api_key: sk-from-file\n"
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatalf("写账号档现场失败: %v", err)
	}
	runtime, err := NewRuntime(RuntimeConfig{AccountsPath: path})
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	t.Cleanup(runtime.Shutdown)

	balance, err := runtime.AccountBalance(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("AccountBalance: %v", err)
	}
	if gotAuth != "Bearer sk-from-file" {
		t.Fatalf("探测用的凭据 = %q，期望账号档里那一把", gotAuth)
	}
	if gotPath != "/user/balance" {
		t.Fatalf("探测路径 = %q，期望 /user/balance", gotPath)
	}
	if !balance.Available || len(balance.Infos) != 1 || balance.Infos[0].Total != "42.50" {
		t.Fatalf("读数没原样带回来: %#v", balance)
	}
}

// TestAccountBalanceRejectsUnknownAccount：余额按名字查，未知名字要报错——绝不能
// 回退到"当前选中账号"（那会让用户看到另一个账号的钱）。
func TestAccountBalanceRejectsUnknownAccount(t *testing.T) {
	runtime := newHotAddRuntime(t)
	_, err := runtime.AccountBalance(context.Background(), "agent-9")
	if err == nil || !strings.Contains(err.Error(), "unknown account") {
		t.Fatalf("未知账号应报 unknown account，实际 %v", err)
	}
}

// TestAccountBalanceDispatchesToAccountDomain：已知账号要走到账号域的探测（这里用
// 不支持的供应商来证明"确实按这条账号的规格进了探测"——账号域会在发请求前就以
// "该供应商没有余额接口"拒绝，说明规格被正确取到）。
func TestAccountBalanceDispatchesToAccountDomain(t *testing.T) {
	runtime := newHotAddRuntime(t)
	// 现有 fixture 的账号是 deepseek-flash + deepseek 域名：会真的发请求。为了不让
	// 单测打外网，把它的 base_url 换成一个本地不可达的 deepseek 域名端口——连接失败
	// 也算"进了探测"的证据。
	runtime.accounts.Add(model.AccountSpec{
		Name: "agent-local", Provider: "openai", BaseURL: "http://127.0.0.1:1",
		APIKey: "sk-local", Model: "deepseek-flash", Role: model.RoleAgent,
	}, config.AccountLimits{ContextWindow: 200_000, MaxOutputTokens: 8192})

	_, err := runtime.AccountBalance(context.Background(), "agent-local")
	if err == nil {
		t.Fatalf("不可达端点应报错")
	}
	// 错误来自探测（连接被拒），不是"未知账号"。
	if strings.Contains(err.Error(), "unknown account") {
		t.Fatalf("不该退化成未知账号: %v", err)
	}
	if !strings.Contains(err.Error(), "agent-local") {
		t.Fatalf("错误文案应带上账号名以便定位: %v", err)
	}
}

// TestRegisterAccountRefusesWhenPoolWouldReject：池层拒绝（重名）时管理器不该先认领
// ——顺序必须是"先进池，再进管理器"，否则列表里会出现一条永远承不了流的假账号。
func TestRegisterAccountRefusesWhenPoolWouldReject(t *testing.T) {
	runtime := newHotAddRuntime(t)
	duplicate := model.AccountSpec{
		Name: "agent-1", Provider: "openai", BaseURL: "https://api.deepseek.com",
		APIKey: "sk-dup", Model: "deepseek-flash", Role: model.RoleAgent, MaxConcurrency: 1,
	}
	if err := runtime.RegisterAccount(duplicate, config.AccountLimits{ContextWindow: 1, MaxOutputTokens: 1}); err == nil {
		t.Fatalf("与既有账号重名时池层应拒绝")
	}
	// 管理器里 agent-1 的限额必须还是原来那份，没被这次失败的注册改掉。
	if got := runtime.accounts.LimitsFor("agent-1"); got.ContextWindow == 1 {
		t.Fatalf("失败的注册污染了管理器里的账号限额: %#v", got)
	}
}

func TestReplaceAndUnregisterAccountStayInPoolAndManager(t *testing.T) {
	runtime := newHotAddRuntime(t)
	old, ok := runtime.accounts.Spec("agent-1")
	if !ok {
		t.Fatal("fixture 缺少 agent-1")
	}
	updated := old
	updated.Provider = "deepseek"
	updated.Model = "deepseek-reasoner"
	if err := runtime.ReplaceAccount(updated, config.AccountLimits{ContextWindow: 250_000, MaxOutputTokens: 8192}); err != nil {
		t.Fatalf("ReplaceAccount: %v", err)
	}
	accounts := runtime.Accounts()
	if len(accounts) != 1 || accounts[0].Provider != "deepseek" || accounts[0].Model != "deepseek-reasoner" {
		t.Fatalf("热替换后的账号列表 = %+v", accounts)
	}
	if got := runtime.accounts.LimitsFor("agent-1").ContextWindow; got != 250_000 {
		t.Fatalf("热替换后的限额 = %d", got)
	}
	if err := runtime.UnregisterAccount("agent-1"); err != nil {
		t.Fatalf("UnregisterAccount: %v", err)
	}
	if len(runtime.Accounts()) != 0 {
		t.Fatalf("删除后账号仍在池里: %+v", runtime.Accounts())
	}
	if _, ok := runtime.accounts.Spec("agent-1"); ok {
		t.Fatal("删除后管理器仍保留规格")
	}
}
