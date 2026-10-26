// Package admin owns account administration application use cases.
//
// Account file validation, runtime registration, balance probing and account
// window persistence remain behind narrow host ports. This package only owns
// request normalization, unavailable-capability errors and the account switch
// orchestration order.
package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	view_state "github.com/RedHuang-0622/seelex/middle/view/state"
)

// ProjectionPort supplies the host's runtime projection and publication
// operations. The host adapter owns all locking and snapshot mutation.
type ProjectionPort interface {
	Collect(context.Context) view_state.RuntimeStateProjection
	LockView()
	SetRuntimeAccount(name string)
	ApplyRuntimeProjectionLocked(view_state.RuntimeStateProjection)
	BumpLocked() uint64
	UnlockView()
	CurrentViewSessionID() string
	RuntimeSnapshot() model.RuntimeState
	PublishRuntimeChanged(revision uint64, sessionID string, runtime model.RuntimeState)
	AddNotice(message string)
}

// AccountDeps contains the narrow host capabilities used by account
// administration. Ports are resolved per call so replacing a host dependency
// takes effect on the next request without rebuilding the application.
type AccountDeps struct {
	Admin      func() contract.AccountAdminPort
	Balance    func() contract.AccountBalancePort
	Select     func(name string) bool
	Window     func(name string, window int) (int, error)
	Projection ProjectionPort
}

// AccountApplication coordinates account administration without owning the
// storage, runtime or projection implementations behind its ports.
type AccountApplication struct {
	deps AccountDeps
}

// NewAccountApplication constructs an account application from consumer-owned
// ports.
func NewAccountApplication(deps AccountDeps) *AccountApplication {
	return &AccountApplication{deps: deps}
}

// AddAccount forwards a validated account draft to the account admin host.
func (application *AccountApplication) AddAccount(_ context.Context, draft dto.AccountDraft) (dto.AccountAddResult, error) {
	accountAdmin, err := application.accountAdmin("无法写入账号档")
	if err != nil {
		return dto.AccountAddResult{}, err
	}
	return accountAdmin.AddAccount(draft)
}

// AccountDetails reads non-secret fields for one account.
func (application *AccountApplication) AccountDetails(name string) (dto.AccountDetails, error) {
	accountAdmin, err := application.accountAdmin("无法读取账号档")
	if err != nil {
		return dto.AccountDetails{}, err
	}
	return accountAdmin.AccountDetails(strings.TrimSpace(name))
}

// UpdateAccount updates one account in place.
func (application *AccountApplication) UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error) {
	accountAdmin, err := application.accountAdmin("无法更新账号档")
	if err != nil {
		return dto.AccountDetails{}, err
	}
	update.Name = strings.TrimSpace(update.Name)
	return accountAdmin.UpdateAccount(update)
}

// DeleteAccount deletes one account and returns its non-secret receipt.
func (application *AccountApplication) DeleteAccount(name string) (dto.AccountDetails, error) {
	accountAdmin, err := application.accountAdmin("无法删除账号档")
	if err != nil {
		return dto.AccountDetails{}, err
	}
	return accountAdmin.DeleteAccount(strings.TrimSpace(name))
}

// AccountBalance probes an account through the optional balance host port.
func (application *AccountApplication) AccountBalance(ctx context.Context, name string) (dto.AccountBalance, error) {
	if application == nil {
		return dto.AccountBalance{}, errors.New("account balance: service is unavailable")
	}
	if application.deps.Balance == nil {
		return dto.AccountBalance{}, errors.New("account balance: 宿主未提供余额查询能力")
	}
	port := application.deps.Balance()
	if port == nil {
		return dto.AccountBalance{}, errors.New("account balance: 宿主未提供余额查询能力")
	}
	return port.AccountBalance(ctx, name)
}

// SelectAccount switches the runtime account, then republishes the projected
// runtime view with one revision bump.
func (application *AccountApplication) SelectAccount(_ context.Context, name string) error {
	if application == nil {
		return errors.New("admin account selection: service is unavailable")
	}
	if application.deps.Select == nil || application.deps.Projection == nil {
		return errors.New("admin account selection: service is unavailable")
	}
	if !application.deps.Select(name) {
		return fmt.Errorf("账号不可用: %s", name)
	}

	projectionPort := application.deps.Projection
	projection := projectionPort.Collect(context.Background())
	projectionPort.LockView()
	projectionPort.SetRuntimeAccount(name)
	projectionPort.ApplyRuntimeProjectionLocked(projection)
	revision := projectionPort.BumpLocked()
	projectionPort.UnlockView()
	sessionID := projectionPort.CurrentViewSessionID()
	runtime := projectionPort.RuntimeSnapshot()
	projectionPort.PublishRuntimeChanged(revision, sessionID, runtime)
	projectionPort.AddNotice("已切换账号: " + name)
	return nil
}

// SetAccountWindow applies a per-account context window through the host's
// account-window setter.
func (application *AccountApplication) SetAccountWindow(_ context.Context, name string, window int) (int, error) {
	if application == nil || application.deps.Window == nil {
		return 0, errors.New("account window: service is unavailable")
	}
	return application.deps.Window(name, window)
}

func (application *AccountApplication) accountAdmin(action string) (contract.AccountAdminPort, error) {
	if application == nil {
		return nil, errors.New("account admin: service is unavailable")
	}
	if application.deps.Admin == nil {
		return nil, errors.New("account admin: 宿主未提供账号管理能力（" + action + "）")
	}
	accountAdmin := application.deps.Admin()
	if accountAdmin == nil {
		return nil, errors.New("account admin: 宿主未提供账号管理能力（" + action + "）")
	}
	return accountAdmin, nil
}
