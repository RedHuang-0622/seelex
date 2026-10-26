package adapters

import (
	"testing"

	"github.com/RedHuang-0622/seelex/backend/limit/accountwindow"
	"github.com/RedHuang-0622/seelex/contract"
)

type accountWindowRuntimeProbe struct {
	window int
}

func (probe *accountWindowRuntimeProbe) SetAccountWindow(_ string, window int) {
	probe.window = window
}

func (probe *accountWindowRuntimeProbe) AccountWindowFor(string) int {
	return probe.window
}

type accountWindowStoreProbe struct{}

func (accountWindowStoreProbe) SaveAccountWindow(string, int) error { return nil }

func TestAccountWindowPolicyMatchesMiddleSetter(t *testing.T) {
	portRuntime := &accountWindowRuntimeProbe{}
	middleRuntime := &accountWindowRuntimeProbe{}
	got, gotErr := NewAccountWindowPolicy().Set(
		portRuntime,
		accountWindowStoreProbe{},
		"account-a",
		accountwindow.Presets()[1],
	)
	want, wantErr := (accountwindow.Setter{
		Runtime: middleRuntime,
		Store:   accountWindowStoreProbe{},
	}).Set("account-a", accountwindow.Presets()[1])
	if got != want || (gotErr == nil) != (wantErr == nil) || portRuntime.window != middleRuntime.window {
		t.Fatalf("policy mismatch: got=(%d,%v,%d), want=(%d,%v,%d)",
			got, gotErr, portRuntime.window, want, wantErr, middleRuntime.window)
	}
}

var _ contract.AccountWindowPort = (*accountWindowRuntimeProbe)(nil)
