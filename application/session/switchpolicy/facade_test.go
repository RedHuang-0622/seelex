package switching

import (
	"errors"
	"reflect"
	"testing"

	middleswitch "github.com/RedHuang-0622/seelex/middle/switch"
)

func TestSwitchPolicyTypesAndErrorsAreMiddleIdentity(t *testing.T) {
	if reflect.TypeOf(BusyState{}) != reflect.TypeOf(middleswitch.BusyState{}) {
		t.Fatal("BusyState is not the Middle identity")
	}
	if !errors.Is(ErrQueueNotRunning, middleswitch.ErrQueueNotRunning) {
		t.Fatal("ErrQueueNotRunning identity changed")
	}
}
