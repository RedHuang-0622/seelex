package application_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

// TestWorkTableRuntimeLivesInExecutionTable keeps the WorkTable host state,
// lifecycle consumers and projection orchestration out of application/core.
func TestWorkTableRuntimeLivesInExecutionTable(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	applicationDir := filepath.Dir(file)
	legacy := filepath.Join(applicationDir, "core", "work_table.go")
	if _, err := os.Stat(legacy); err == nil {
		t.Errorf("application/core/work_table.go 仍存在；WorkTable runtime 必须由 application/execution/table 拥有")
	} else if !os.IsNotExist(err) {
		t.Fatalf("检查 %s: %v", legacy, err)
	}

	runtimePath := filepath.Join(applicationDir, "execution", "table", "runtime.go")
	source, err := parser.ParseFile(token.NewFileSet(), runtimePath, nil, 0)
	if err != nil {
		t.Fatalf("读取 WorkTable runtime %s: %v", runtimePath, err)
	}
	for _, imported := range source.Imports {
		importPath, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			t.Fatalf("解析 import %s: %v", imported.Path.Value, err)
		}
		if importPath == "github.com/RedHuang-0622/seelex/application/core" {
			t.Errorf("application/execution/table 不得反向 import application/core")
		}
	}
}
