package assembly

import (
	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	"reflect"
	"testing"
)

func TestServiceAdminComponentUsesDomainApplication(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("adminApplication")
	if !ok {
		t.Fatal("serviceComponents 未装配 adminApplication")
	}
	if field.Type != reflect.TypeOf((*applicationadmin.AccountApplication)(nil)) {
		t.Fatalf("adminApplication 字段类型 = %s", field.Type)
	}
}
