package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"testing"

	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
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

func TestServiceAdminFacadeMethodsOnlyDelegate(t *testing.T) {
	paths := []string{
		filepath.Join(".", "service_account.go"),
		filepath.Join(".", "service_account_window.go"),
	}
	want := map[string]bool{
		"AddAccount":       false,
		"AccountDetails":   false,
		"UpdateAccount":    false,
		"DeleteAccount":    false,
		"AccountBalance":   false,
		"SetAccountWindow": false,
	}
	for _, path := range paths {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("解析 %s: %v", path, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || function.Name == nil {
				continue
			}
			if _, tracked := want[function.Name.Name]; !tracked {
				continue
			}
			want[function.Name.Name] = true
			if len(function.Body.List) != 1 {
				t.Fatalf("%s 有 %d 条语句，want 一行 admin 转发", function.Name.Name, len(function.Body.List))
			}
			statement, ok := function.Body.List[0].(*ast.ReturnStmt)
			if !ok || len(statement.Results) != 1 {
				t.Fatalf("%s 不是单行 return 转发", function.Name.Name)
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("未找到账号兼容入口 %s", name)
		}
	}
}
