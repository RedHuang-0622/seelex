package admin

import (
	"context"
	"errors"
	"strings"
	"testing"

	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

type fakeAccountAdmin struct {
	draft   dto.AccountDraft
	details dto.AccountDetails
	update  dto.AccountUpdate
	deleted string
	err     error
}

func (fake *fakeAccountAdmin) AddAccount(draft dto.AccountDraft) (dto.AccountAddResult, error) {
	fake.draft = draft
	return dto.AccountAddResult{Name: "agent-2"}, fake.err
}

func (fake *fakeAccountAdmin) AccountDetails(name string) (dto.AccountDetails, error) {
	fake.details = dto.AccountDetails{Name: name}
	return fake.details, fake.err
}

func (fake *fakeAccountAdmin) UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error) {
	fake.update = update
	return dto.AccountDetails{Name: update.Name}, fake.err
}

func (fake *fakeAccountAdmin) DeleteAccount(name string) (dto.AccountDetails, error) {
	fake.deleted = name
	return dto.AccountDetails{Name: name}, fake.err
}

type fakeBalance struct {
	name string
}

func (fake *fakeBalance) AccountBalance(context.Context, string) (dto.AccountBalance, error) {
	return dto.AccountBalance{Name: fake.name, Available: true}, nil
}

type projectionFake struct {
	calls      []string
	projection view_state.RuntimeStateProjection
}

func (fake *projectionFake) Collect(context.Context) view_state.RuntimeStateProjection {
	fake.calls = append(fake.calls, "collect")
	return fake.projection
}

func (fake *projectionFake) LockView() {
	fake.calls = append(fake.calls, "lock")
}

func (fake *projectionFake) SetRuntimeAccount(name string) {
	fake.calls = append(fake.calls, "account:"+name)
}

func (fake *projectionFake) ApplyRuntimeProjectionLocked(view_state.RuntimeStateProjection) {
	fake.calls = append(fake.calls, "apply")
}

func (fake *projectionFake) BumpLocked() uint64 {
	fake.calls = append(fake.calls, "bump")
	return 7
}

func (fake *projectionFake) UnlockView() {
	fake.calls = append(fake.calls, "unlock")
}

func (fake *projectionFake) CurrentViewSessionID() string {
	fake.calls = append(fake.calls, "session")
	return "session-a"
}

func (fake *projectionFake) RuntimeSnapshot() model.RuntimeState {
	fake.calls = append(fake.calls, "snapshot")
	return model.RuntimeState{Account: "agent-1"}
}

func (fake *projectionFake) PublishRuntimeChanged(revision uint64, sessionID string, runtime model.RuntimeState) {
	fake.calls = append(fake.calls, "publish")
	if revision != 7 || sessionID != "session-a" || runtime.Account != "agent-1" {
		panic("projection publication changed")
	}
}

func (fake *projectionFake) AddNotice(message string) {
	fake.calls = append(fake.calls, "notice:"+message)
}

func TestAccountApplicationDelegatesAccountOperations(t *testing.T) {
	accountAdmin := &fakeAccountAdmin{}
	balance := &fakeBalance{name: "agent-1"}
	application := NewAccountApplication(AccountDeps{
		Admin:   func() contract.AccountAdminPort { return accountAdmin },
		Balance: func() contract.AccountBalancePort { return balance },
	})

	draft := dto.AccountDraft{
		Role: "agent", Provider: "openai", Model: "deepseek-flash",
		BaseURL: "https://api.deepseek.com", APIKey: "sk-verbatim",
	}
	added, err := application.AddAccount(context.Background(), draft)
	if err != nil || added.Name != "agent-2" || accountAdmin.draft != draft {
		t.Fatalf("AddAccount = %+v, forwarded %#v, %v", added, accountAdmin.draft, err)
	}

	details, err := application.AccountDetails(" agent-1 ")
	if err != nil || details.Name != "agent-1" {
		t.Fatalf("AccountDetails = %+v, %v", details, err)
	}

	updated, err := application.UpdateAccount(dto.AccountUpdate{Name: " agent-1 ", Model: "gpt-5"})
	if err != nil || updated.Name != "agent-1" || accountAdmin.update.Name != "agent-1" {
		t.Fatalf("UpdateAccount = %+v, forwarded %+v, %v", updated, accountAdmin.update, err)
	}

	deleted, err := application.DeleteAccount(" agent-1 ")
	if err != nil || deleted.Name != "agent-1" || accountAdmin.deleted != "agent-1" {
		t.Fatalf("DeleteAccount = %+v, forwarded %q, %v", deleted, accountAdmin.deleted, err)
	}

	accountBalance, err := application.AccountBalance(context.Background(), "agent-1")
	if err != nil || !accountBalance.Available || accountBalance.Name != "agent-1" {
		t.Fatalf("AccountBalance = %+v, %v", accountBalance, err)
	}
}

func TestAccountApplicationPreservesUnavailableErrors(t *testing.T) {
	var nilApplication *AccountApplication
	if _, err := nilApplication.AddAccount(context.Background(), dto.AccountDraft{}); err == nil ||
		!strings.Contains(err.Error(), "account admin: service is unavailable") {
		t.Fatalf("nil AddAccount error = %v", err)
	}

	application := NewAccountApplication(AccountDeps{})
	if _, err := application.AccountDetails("agent-1"); err == nil ||
		!strings.Contains(err.Error(), "宿主未提供账号管理能力") {
		t.Fatalf("missing account admin error = %v", err)
	}
	if _, err := application.AccountBalance(context.Background(), "agent-1"); err == nil ||
		!strings.Contains(err.Error(), "宿主未提供余额查询能力") {
		t.Fatalf("missing balance error = %v", err)
	}
}

func TestAccountApplicationPropagatesHostErrors(t *testing.T) {
	hostErr := errors.New("host failure")
	application := NewAccountApplication(AccountDeps{
		Admin: func() contract.AccountAdminPort {
			return &fakeAccountAdmin{err: hostErr}
		},
	})
	if _, err := application.AddAccount(context.Background(), dto.AccountDraft{}); !errors.Is(err, hostErr) {
		t.Fatalf("AddAccount host error = %v", err)
	}
}

func TestAccountApplicationSelectAccountOrder(t *testing.T) {
	projection := &projectionFake{}
	application := NewAccountApplication(AccountDeps{
		Select: func(name string) bool {
			projection.calls = append(projection.calls, "select:"+name)
			return true
		},
		Projection: projection,
	})
	if err := application.SelectAccount(context.Background(), "agent-1"); err != nil {
		t.Fatalf("SelectAccount: %v", err)
	}
	want := "select:agent-1,collect,lock,account:agent-1,apply,bump,unlock,session,snapshot,publish,notice:已切换账号: agent-1"
	if got := strings.Join(projection.calls, ","); got != want {
		t.Fatalf("calls = %s, want %s", got, want)
	}
}

func TestAccountApplicationSelectAccountStopsWhenUnavailable(t *testing.T) {
	projection := &projectionFake{}
	application := NewAccountApplication(AccountDeps{
		Select:     func(string) bool { return false },
		Projection: projection,
	})
	err := application.SelectAccount(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "账号不可用: missing") {
		t.Fatalf("error = %v", err)
	}
	if len(projection.calls) != 0 {
		t.Fatalf("unavailable select calls = %v", projection.calls)
	}
}

func TestAccountApplicationSetAccountWindowDelegates(t *testing.T) {
	var gotName string
	var gotWindow int
	application := NewAccountApplication(AccountDeps{
		Window: func(name string, window int) (int, error) {
			gotName, gotWindow = name, window
			return 600_000, nil
		},
	})
	applied, err := application.SetAccountWindow(context.Background(), "agent-1", 600_000)
	if err != nil || applied != 600_000 || gotName != "agent-1" || gotWindow != 600_000 {
		t.Fatalf("SetAccountWindow = %d, %v; forwarded %q, %d", applied, err, gotName, gotWindow)
	}
}

func TestAccountApplicationSetAccountWindowPreservesUnavailableErrors(t *testing.T) {
	var nilApplication *AccountApplication
	if _, err := nilApplication.SetAccountWindow(context.Background(), "agent-1", 400_000); err == nil ||
		!strings.Contains(err.Error(), "account window: service is unavailable") {
		t.Fatalf("nil SetAccountWindow error = %v", err)
	}
	application := NewAccountApplication(AccountDeps{})
	if _, err := application.SetAccountWindow(context.Background(), "agent-1", 400_000); err == nil ||
		!strings.Contains(err.Error(), "account window: service is unavailable") {
		t.Fatalf("missing SetAccountWindow error = %v", err)
	}
}
