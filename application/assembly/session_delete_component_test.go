package assembly

import (
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"reflect"
	"testing"
)

func TestServiceComponentsAssembleSessionDelete(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("sessionDelete")
	if !ok {
		t.Fatal("serviceComponents 未装配 sessionDelete")
	}
	if field.Type != reflect.TypeOf((*applicationsession.DeleteApplication)(nil)) {
		t.Fatalf("sessionDelete 字段类型 = %s", field.Type)
	}
}
