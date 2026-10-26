package table

import (
	"reflect"
	"testing"

	worktable "github.com/RedHuang-0622/seelex/middle/view/worktable"
)

func TestWorkTableFacadeTypesAreMiddleIdentity(t *testing.T) {
	if reflect.TypeOf(Limits{}) != reflect.TypeOf(worktable.Limits{}) {
		t.Fatal("Limits is not the Middle identity")
	}
	if reflect.TypeOf(WorkTableUpdate{}) != reflect.TypeOf(worktable.WorkTableUpdate{}) {
		t.Fatal("WorkTableUpdate is not the Middle identity")
	}
}
