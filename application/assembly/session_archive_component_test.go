package assembly

import (
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"reflect"
	"testing"
)

func TestServiceComponentsAssembleSessionArchive(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("sessionArchive")
	if !ok {
		t.Fatal("serviceComponents 未装配 sessionArchive")
	}
	if field.Type != reflect.TypeOf((*applicationsession.ArchiveApplication)(nil)) {
		t.Fatalf("sessionArchive 字段类型 = %s", field.Type)
	}
}
