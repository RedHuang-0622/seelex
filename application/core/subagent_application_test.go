package core

import (
	"reflect"
	"testing"

	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
)

func TestServiceComponentsAssembleSubagentApplication(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("subagentApplication")
	if !ok {
		t.Fatal("serviceComponents 未装配 subagentApplication")
	}
	if field.Type != reflect.TypeOf((*applicationexecution.SubagentApplication)(nil)) {
		t.Fatalf("subagentApplication 字段类型 = %s", field.Type)
	}
}
