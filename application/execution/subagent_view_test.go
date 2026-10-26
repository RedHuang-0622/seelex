package execution

import (
	"reflect"
	"testing"

	subagentview "github.com/RedHuang-0622/seelex/middle/view/subagent"
)

func TestSubagentViewTypesAreMiddleIdentity(t *testing.T) {
	if reflect.TypeOf(SubagentViewCoordinator{}) != reflect.TypeOf(subagentview.Coordinator{}) {
		t.Fatal("SubagentViewCoordinator is not the Middle identity")
	}
}
