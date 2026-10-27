package assembly

import (
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"reflect"
	"testing"
)

func TestServiceComponentsAssembleSessionLifecycle(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("sessionLifecycle")
	if !ok {
		t.Fatal("serviceComponents 未装配 sessionLifecycle")
	}
	if field.Type != reflect.TypeOf((*applicationsession.LifecycleApplication)(nil)) {
		t.Fatalf("sessionLifecycle 字段类型 = %s", field.Type)
	}
}
