package workspace

import (
	"reflect"
	"testing"

	switchworkspace "github.com/RedHuang-0622/seelex/middle/switch/workspace"
)

func TestSwitchWorkspaceTypesAreMiddleIdentity(t *testing.T) {
	if reflect.TypeOf(SwitchUseCase{}) != reflect.TypeOf(switchworkspace.UseCase{}) {
		t.Fatal("UseCase is not the Middle identity")
	}
	if reflect.TypeOf(SwitchProjection{}) != reflect.TypeOf(switchworkspace.Projection{}) {
		t.Fatal("Projection is not the Middle identity")
	}
}
