package admin

import "github.com/RedHuang-0622/seelex/contract"

// AccountWindowPolicy owns the account-window validation, persistence order
// and runtime application decision used by account administration.
type AccountWindowPolicy interface {
	Set(runtime contract.AccountWindowPort, store contract.AccountWindowStore, name string, window int) (int, error)
}
