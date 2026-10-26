package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
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

func TestUnloadSessionOnlyDelegatesToSessionLifecycle(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "session_lifecycle.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name.Name != "UnloadSession" {
			continue
		}
		found = true
		if len(function.Body.List) != 1 {
			t.Fatalf("UnloadSession 有 %d 条语句，want 一行 component 转发", len(function.Body.List))
		}
		returnStatement, ok := function.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(returnStatement.Results) != 1 {
			t.Fatal("UnloadSession 不是单行 return 转发")
		}
	}
	if !found {
		t.Fatal("未找到 UnloadSession")
	}
}
