package adapters

import (
	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	"github.com/RedHuang-0622/seelex/backend/limit/accountwindow"
	"github.com/RedHuang-0622/seelex/contract"
)

type accountWindowPolicy struct{}

var _ applicationadmin.AccountWindowPolicy = accountWindowPolicy{}

// NewAccountWindowPolicy exposes the account-window setter through the
// application-owned policy port.
func NewAccountWindowPolicy() applicationadmin.AccountWindowPolicy {
	return accountWindowPolicy{}
}

func (accountWindowPolicy) Set(
	runtime contract.AccountWindowPort,
	store contract.AccountWindowStore,
	name string,
	window int,
) (int, error) {
	return accountwindow.Setter{Runtime: runtime, Store: store}.Set(name, window)
}
