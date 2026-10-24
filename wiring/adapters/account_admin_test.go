package adapters

// account_admin_test.go — 「手动添加账号」宿主适配器的判据。
//
// 分工：账号档的写法与校验由 internal/config 的测试钉住；热注册之后"池里真的多了一条、
// 能承流"由 assemble 的 RegisterAccount 测试钉住。这里钉住的是**两者的交接**：
// 同一个动作里先写盘、再从盘上按加载器读回规格、再原样交给运行时；任何一步失败
// 都要在错误里说清"盘上改了没有"。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/backend/engine/account"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/internal/config"
	"github.com/RedHuang-0622/seelex/internal/model"
)

const adminAccountsFixture = `defaults:
  provider: openai
  max_tokens: 16384

roles:
  agent:
  - model: deepseek-flash
    base_url: https://api.deepseek.com
    api_key: sk-old
    reasoning_effort: session
`

// fakeRegistrar 记录"交给运行时的那条账号"。
type fakeRegistrar struct {
	got           *model.AccountSpec
	replaced      *model.AccountSpec
	removed       string
	limits        config.AccountLimits
	err           error
	replaceErr    error
	unregisterErr error
}

func (registrar *fakeRegistrar) RegisterAccount(spec model.AccountSpec, limits config.AccountLimits) error {
	registrar.got = &spec
	registrar.limits = limits
	return registrar.err
}

func (registrar *fakeRegistrar) ReplaceAccount(spec model.AccountSpec, limits config.AccountLimits) error {
	registrar.replaced = &spec
	registrar.limits = limits
	return registrar.replaceErr
}

func (registrar *fakeRegistrar) UnregisterAccount(name string) error {
	registrar.removed = name
	return registrar.unregisterErr
}

func writeAdminFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.yaml")
	if err := os.WriteFile(path, []byte(adminAccountsFixture), 0o600); err != nil {
		t.Fatalf("写账号档现场失败: %v", err)
	}
	return path
}

// TestAccountAdminWritesThenHandsResolvedSpecToRuntime：写盘 → 加载器读回 →
// 交给运行时。交给运行时的规格必须来自**加载器**（继承 defaults 的 provider、
// 由位置推导的名字），而不是把界面输入自己拼成 spec——否则"现在能用、重启就变样"。
func TestAccountAdminWritesThenHandsResolvedSpecToRuntime(t *testing.T) {
	path := writeAdminFixture(t)
	registrar := &fakeRegistrar{}
	admin := AccountAdmin{Path: path, Runtime: registrar}

	result, err := admin.AddAccount(dto.AccountDraft{
		Role:            "agent",
		Model:           "deepseek-flash",
		BaseURL:         "https://api.deepseek.com",
		APIKey:          "sk-new",
		ReasoningEffort: "low",
	})
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	if result.Name != "agent-2" || result.Role != "agent" {
		t.Fatalf("回执 = %#v，期望 name=agent-2 role=agent", result)
	}
	// provider 没在草稿里写，必须由加载器按 defaults 继承出来——回执与交给运行时的
	// 规格都该是 openai（这条同时证明"读回用的是加载器"）。
	if result.Provider != "openai" || result.Model != "deepseek-flash" {
		t.Fatalf("回执字段 = %#v", result)
	}

	if registrar.got == nil {
		t.Fatalf("没有把新账号交给运行时（热注册缺失）")
	}
	if registrar.got.Name != "agent-2" || registrar.got.APIKey != "sk-new" ||
		registrar.got.Provider != "openai" || registrar.got.ReasoningEffort != "low" {
		t.Fatalf("交给运行时的规格不对: %#v", registrar.got)
	}
	if registrar.limits.MaxOutputTokens <= 0 {
		t.Fatalf("没有把账号限额一起交接: %#v", registrar.limits)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回账号档失败: %v", err)
	}
	if !strings.Contains(string(raw), "    api_key: sk-new\n") {
		t.Fatalf("账号档里没有新条目:\n%s", raw)
	}
	if !strings.Contains(string(raw), "    reasoning_effort: low\n") {
		t.Fatalf("账号档里没有写入思考强度:\n%s", raw)
	}
}

// TestAccountAdminReportsHotRegisterFailure：已落盘但热注册失败时，错误必须说明
// "重启后生效"——否则用户会以为没加上而重复添加。
func TestAccountAdminReportsHotRegisterFailure(t *testing.T) {
	path := writeAdminFixture(t)
	admin := AccountAdmin{Path: path, Runtime: &fakeRegistrar{err: errors.New("pool full")}}

	_, err := admin.AddAccount(dto.AccountDraft{Role: "agent", Model: "m", APIKey: "sk"})
	if err == nil {
		t.Fatalf("热注册失败应报错")
	}
	if !strings.Contains(err.Error(), "重启后生效") {
		t.Fatalf("错误文案应说明重启后生效: %v", err)
	}
	// 真相是"盘上改了、进程没认"：文件里确实有这条。
	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("读回账号档失败: %v", readErr)
	}
	if !strings.Contains(string(raw), "sk\n") && !strings.Contains(string(raw), "api_key: sk") {
		t.Fatalf("已落盘的条目不该被回滚:\n%s", raw)
	}
}

func TestAccountAdminDetailsUpdateDeleteRoundTrip(t *testing.T) {
	path := writeAdminFixture(t)
	registrar := &fakeRegistrar{}
	admin := AccountAdmin{Path: path, Runtime: registrar}

	details, err := admin.AccountDetails("agent-1")
	if err != nil {
		t.Fatalf("AccountDetails: %v", err)
	}
	if details.Model != "deepseek-flash" || !details.APIKeySet || details.Role != "agent" ||
		details.Provider != "" || details.ReasoningEffort != "session" {
		t.Fatalf("details = %+v", details)
	}
	updated, err := admin.UpdateAccount(dto.AccountUpdate{
		Name: "agent-1", Provider: "openai", Model: "gpt-5", BaseURL: "https://api.openai.com/v1",
	})
	if err != nil {
		t.Fatalf("UpdateAccount: %v", err)
	}
	if updated.Model != "gpt-5" || registrar.replaced == nil || registrar.replaced.Model != "gpt-5" {
		t.Fatalf("更新回执/热替换 = %+v / %+v", updated, registrar.replaced)
	}
	if registrar.replaced.APIKey != "sk-old" {
		t.Fatalf("空 api_key 更新应保留旧密钥，得到 %q", registrar.replaced.APIKey)
	}
	if _, err := admin.AddAccount(dto.AccountDraft{Role: "agent", Model: "gpt-5-mini", APIKey: "sk-second"}); err != nil {
		t.Fatalf("准备第二条账号: %v", err)
	}
	deleted, err := admin.DeleteAccount("agent-1")
	if err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	if deleted.Name != "agent-1" || registrar.removed != "agent-1" {
		t.Fatalf("删除回执/运行时退租 = %+v / %q", deleted, registrar.removed)
	}
}

// TestAccountAdminValidationFailureLeavesFileUntouched：校验失败时不落盘、也不碰运行时。
func TestAccountAdminValidationFailureLeavesFileUntouched(t *testing.T) {
	path := writeAdminFixture(t)
	registrar := &fakeRegistrar{}
	admin := AccountAdmin{Path: path, Runtime: registrar}

	if _, err := admin.AddAccount(dto.AccountDraft{Role: "agent", Model: "m"}); err == nil {
		t.Fatalf("缺 api_key 应报错")
	}
	if registrar.got != nil {
		t.Fatalf("校验失败却动了运行时")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回账号档失败: %v", err)
	}
	if string(raw) != adminAccountsFixture {
		t.Fatalf("校验失败却改动了账号档:\n%s", raw)
	}
}

// TestAccountAdminNeedsPathAndRuntime：宿主没解析出路径 / 没装运行时时给明确错误。
func TestAccountAdminNeedsPathAndRuntime(t *testing.T) {
	if _, err := (AccountAdmin{Runtime: &fakeRegistrar{}}).AddAccount(dto.AccountDraft{}); err == nil {
		t.Fatalf("空路径应报错")
	}
	if _, err := (AccountAdmin{Path: "x.yaml"}).AddAccount(dto.AccountDraft{}); err == nil {
		t.Fatalf("缺运行时（typed nil）应报错")
	}
}

// TestAccountBalanceDTOKeepsProviderShape：余额映射必须原样搬字符串（"110.00" 不能
// 变成 "110"）、按序保留币种、available 如实、时刻由调用方给定。
func TestAccountBalanceDTOKeepsProviderShape(t *testing.T) {
	fetchedAt := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	got := accountBalanceDTO("agent-1", account.Balance{
		Available: false,
		Infos: []account.BalanceInfo{
			{Currency: "CNY", Total: "110.00", Granted: "10.00", ToppedUp: "100.00"},
			{Currency: "USD", Total: "0.00"},
		},
	}, fetchedAt)

	if got.Name != "agent-1" || got.Available {
		t.Fatalf("名字/可用性搬错: %#v", got)
	}
	if !got.FetchedAt.Equal(fetchedAt) {
		t.Fatalf("时刻应为调用方给定的值: %v", got.FetchedAt)
	}
	if len(got.Infos) != 2 || got.Infos[0].Currency != "CNY" || got.Infos[0].Total != "110.00" ||
		got.Infos[0].Granted != "10.00" || got.Infos[0].ToppedUp != "100.00" {
		t.Fatalf("币种/金额搬错: %#v", got.Infos)
	}
	if got.Infos[1].Currency != "USD" || got.Infos[1].Total != "0.00" {
		t.Fatalf("顺序或金额搬错: %#v", got.Infos)
	}
}
