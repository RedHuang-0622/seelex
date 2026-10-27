package assembly

import (
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"reflect"
	"testing"
)

func TestSessionMetaApplicationIsAssembled(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("sessionMetaApplication")
	if !ok {
		t.Fatal("serviceComponents 未装配 sessionMetaApplication")
	}
	if field.Type != reflect.TypeOf((*applicationsession.MetaApplication)(nil)) {
		t.Fatalf("sessionMetaApplication 字段类型 = %s", field.Type)
	}
}
