package core

// service_account_test.go — 账号两条链路在应用层的判据：**只解析端口并转发**。
//
// 这里刻意不测"账号档写得好不好、余额解析对不对"——那是 internal/config 与
// backend/engine/account 的判据（各自有测试）。本层要钉住的只有三件事：
// 宿主没装配时给明确错误、装配了就把输入/调用**原样**递过去、nil service 不崩。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

// withTestAccountAdmin 注入「手动添加账号」的宿主能力面（不注入 = 该宿主不支持
// 界面添加，用例应返回可展示错误）。
func withTestAccountAdmin(admin contract.AccountAdminPort) testServiceOption {
	return func(deps *contract.Dependencies) { deps.AccountAdmin = admin }
}

// fakeAccountAdmin 记录被转发的草稿并回执一个名字。
type fakeAccountAdmin struct {
	got    dto.AccountDraft
	update dto.AccountUpdate
	name   string
	err    error
}

func (admin *fakeAccountAdmin) AddAccount(draft dto.AccountDraft) (dto.AccountAddResult, error) {
	admin.got = draft
	if admin.err != nil {
		return dto.AccountAddResult{}, admin.err
	}
	return dto.AccountAddResult{Name: admin.name, Role: draft.Role, Model: draft.Model}, nil
}

func (admin *fakeAccountAdmin) AccountDetails(name string) (dto.AccountDetails, error) {
	return dto.AccountDetails{Name: name}, admin.err
}

func (admin *fakeAccountAdmin) UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error) {
	admin.update = update
	return dto.AccountDetails{Name: update.Name, Model: update.Model}, admin.err
}

func (admin *fakeAccountAdmin) DeleteAccount(name string) (dto.AccountDetails, error) {
	return dto.AccountDetails{Name: name}, admin.err
}

// TestAddAccountNeedsHostPort：宿主没装 AccountAdmin 时要给一句明确错误，而不是
// "看起来成功、实际什么都没发生"。
func TestAddAccountNeedsHostPort(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	_, err := frontendOfTestService(service).AddAccount(context.Background(), dto.AccountDraft{Role: "agent", Model: "m", APIKey: "sk"})
	if err == nil || !strings.Contains(err.Error(), "宿主未提供账号管理能力") {
		t.Fatalf("期望明确的「宿主未提供」错误，实际 %v", err)
	}
}

// TestAddAccountForwardsDraftVerbatim：草稿（含 api_key）必须原样递到宿主——本层
// 不改写、不裁剪、不自己校验（校验在账号档写入器）。
func TestAddAccountForwardsDraftVerbatim(t *testing.T) {
	admin := &fakeAccountAdmin{name: "agent-2"}
	service := newTestService(t, &fakeEngine{}, withTestAccountAdmin(admin))

	draft := dto.AccountDraft{
		Role: "agent", Provider: "openai", Model: "deepseek-flash",
		BaseURL: "https://api.deepseek.com", APIKey: "sk-verbatim", ReasoningEffort: "session",
	}
	result, err := frontendOfTestService(service).AddAccount(context.Background(), draft)
	if err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	if result.Name != "agent-2" {
		t.Fatalf("回执名字 = %q，期望 agent-2", result.Name)
	}
	if admin.got != draft {
		t.Fatalf("转发到宿主的草稿被改写了: %#v", admin.got)
	}
}

// TestAddAccountPropagatesHostError：宿主的错误要原样冒上来（界面靠这句话判断
// "什么都没变"还是"已落盘、重启后生效"）。
func TestAddAccountPropagatesHostError(t *testing.T) {
	hostErr := errors.New("account admin: 账号档已写入 \"agent-2\"，但热注册失败（重启后生效）")
	service := newTestService(t, &fakeEngine{}, withTestAccountAdmin(&fakeAccountAdmin{err: hostErr}))
	_, err := frontendOfTestService(service).AddAccount(context.Background(), dto.AccountDraft{Role: "agent", Model: "m", APIKey: "sk"})
	if !errors.Is(err, hostErr) {
		t.Fatalf("宿主错误未被原样透传: %v", err)
	}
}

func TestUpdateAndDeleteAccountForwardIdentities(t *testing.T) {
	admin := &fakeAccountAdmin{name: "agent-1"}
	service := newTestService(t, &fakeEngine{}, withTestAccountAdmin(admin))
	update := dto.AccountUpdate{Name: "agent-1", Provider: "openai", Model: "gpt-5"}
	if _, err := frontendOfTestService(service).UpdateAccount(update); err != nil {
		t.Fatal(err)
	}
	if admin.update != update {
		t.Fatalf("更新输入被改写: %+v", admin.update)
	}
	deleted, err := frontendOfTestService(service).DeleteAccount(" agent-1 ")
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Name != "agent-1" {
		t.Fatalf("删除回执 = %+v", deleted)
	}
}

// balanceFakeRuntime 在内嵌 fakeRuntime（因此满足整条 RuntimePort）之上补出
// AccountBalancePort——只有这样才能分别验证"装了"与"没装"两条路径。
type balanceFakeRuntime struct {
	*fakeRuntime
	gotName string
	balance dto.AccountBalance
	err     error
}

func (runtime *balanceFakeRuntime) AccountBalance(_ context.Context, name string) (dto.AccountBalance, error) {
	runtime.gotName = name
	if runtime.err != nil {
		return dto.AccountBalance{}, runtime.err
	}
	return runtime.balance, nil
}

// TestAccountBalanceNeedsHostPort：宿主没装余额能力时明确报错（demo/headless 宿主）。
func TestAccountBalanceNeedsHostPort(t *testing.T) {
	service := newTestService(t, &fakeEngine{})
	_, err := frontendOfTestService(service).AccountBalance(context.Background(), "agent-1")
	if err == nil || !strings.Contains(err.Error(), "宿主未提供余额查询能力") {
		t.Fatalf("期望明确的「宿主未提供」错误，实际 %v", err)
	}
}

// TestAccountBalanceForwardsName：按名字查（不是"当前选中账号"），读数原样返回。
func TestAccountBalanceForwardsName(t *testing.T) {
	runtime := &balanceFakeRuntime{
		fakeRuntime: &fakeRuntime{},
		balance: dto.AccountBalance{
			Name: "subagent-1", Available: true,
			Infos: []dto.AccountBalanceInfo{{Currency: "CNY", Total: "110.00"}},
		},
	}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))

	balance, err := frontendOfTestService(service).AccountBalance(context.Background(), "subagent-1")
	if err != nil {
		t.Fatalf("AccountBalance: %v", err)
	}
	if runtime.gotName != "subagent-1" {
		t.Fatalf("转发给宿主的账号名 = %q，期望 subagent-1", runtime.gotName)
	}
	if !balance.Available || len(balance.Infos) != 1 || balance.Infos[0].Total != "110.00" {
		t.Fatalf("读数未原样返回: %#v", balance)
	}
}

// TestAccountBalancePropagatesHostError：探测失败（含"该供应商没有余额接口"）
// 要让界面看到原因。
func TestAccountBalancePropagatesHostError(t *testing.T) {
	hostErr := errors.New("seelex: 该供应商没有可查询的余额接口")
	runtime := &balanceFakeRuntime{fakeRuntime: &fakeRuntime{}, err: hostErr}
	service := newTestService(t, &fakeEngine{}, withTestRuntime(runtime))
	if _, err := frontendOfTestService(service).AccountBalance(context.Background(), "agent-1"); !errors.Is(err, hostErr) {
		t.Fatalf("宿主错误未被原样透传: %v", err)
	}
}

// TestAccountMethodsNilServiceGuard：nil Service 不许崩（与 SetAccountWindow 同一口径）。
func TestAccountMethodsNilServiceGuard(t *testing.T) {
	var service *Service
	if _, err := frontendOfTestService(service).AddAccount(context.Background(), dto.AccountDraft{}); err == nil {
		t.Fatalf("nil service 上 AddAccount 应报错")
	}
	if _, err := frontendOfTestService(service).AccountBalance(context.Background(), "agent-1"); err == nil {
		t.Fatalf("nil service 上 AccountBalance 应报错")
	}
}
