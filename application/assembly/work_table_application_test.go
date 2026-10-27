package assembly

import (
	"reflect"
	"testing"

	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
)

func TestServiceComponentsAssembleExecutionApplication(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("executionApplication")
	if !ok {
		t.Fatal("serviceComponents 未装配 executionApplication")
	}
	if field.Type != reflect.TypeOf((*applicationexecution.WorkItemApplication)(nil)) {
		t.Fatalf("executionApplication 字段类型 = %s", field.Type)
	}
}
