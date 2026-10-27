package task_context

import (
	"reflect"
	"testing"

	taskcontext "github.com/RedHuang-0622/seelex/kernel/task"
)

func TestTaskContextTypesAreBackendIdentity(t *testing.T) {
	if reflect.TypeOf(Coordinator{}) != reflect.TypeOf(taskcontext.Coordinator{}) {
		t.Fatal("Coordinator is not the Backend identity")
	}
	if reflect.TypeOf(ContextBudget{}) != reflect.TypeOf(taskcontext.ContextBudget{}) {
		t.Fatal("ContextBudget is not the Backend identity")
	}
}
