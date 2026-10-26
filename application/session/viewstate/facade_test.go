package view_state

import (
	"reflect"
	"testing"

	middlestate "github.com/RedHuang-0622/seelex/middle/view/state"
)

func TestViewStateTypesAreMiddleIdentity(t *testing.T) {
	if reflect.TypeOf(Coordinator{}) != reflect.TypeOf(middlestate.Coordinator{}) {
		t.Fatal("Coordinator is not the Middle identity")
	}
	if reflect.TypeOf(RuntimeStateProjection{}) != reflect.TypeOf(middlestate.RuntimeStateProjection{}) {
		t.Fatal("RuntimeStateProjection is not the Middle identity")
	}
}
