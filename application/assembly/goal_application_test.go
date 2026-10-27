package assembly

import (
	applicationgoal "github.com/RedHuang-0622/seelex/application/goal"
	"reflect"
	"testing"
)

func TestServiceComponentsAssembleGoalApplication(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("goalApplication")
	if !ok {
		t.Fatal("serviceComponents 未装配 goalApplication")
	}
	if field.Type != reflect.TypeOf((*applicationgoal.Application)(nil)) {
		t.Fatalf("goalApplication 字段类型 = %s", field.Type)
	}
}
