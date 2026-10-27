package assembly

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

func TestArchiveSessionOnlyDelegatesToSessionArchive(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "..", "compat", "frontend.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name.Name != "ArchiveSession" ||
			!functionHasReceiver(function, "Frontend") {
			continue
		}
		found = true
		if len(function.Body.List) != 1 {
			t.Fatalf("ArchiveSession 有 %d 条语句，want 一行 component 转发", len(function.Body.List))
		}
	}
	if !found {
		t.Fatal("未找到 ArchiveSession")
	}
}
