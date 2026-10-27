package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const applicationAssemblyImportPath = "github.com/RedHuang-0622/seelex/application/assembly"

// TestExtractedApplicationPackagesDoNotImportCore 是全领域包的共同反向依赖
// 门禁。单个包提取测试负责“实现确实已迁出”，这里负责它们不能为了复用兼容层
// 再反向导入 core，或持有完整 Service。
func TestExtractedApplicationPackagesDoNotImportCore(t *testing.T) {
	applicationDir := packageBoundaryApplicationDir(t)
	for _, relative := range []string{
		"admin",
		"admin/scheduler",
		"conversation",
		"conversation/search",
		"conversation/context",
		"conversation/edit",
		"execution",
		"execution/table",
		"execution/toolhooks",
		"execution/toolrecord",
		"goal",
		"history",
		"input",
		"input/command",
		"lifecycle",
		"observability",
		"policy",
		"reference",
		"review",
		"session",
		"session/cold",
		"session/query",
		"workspace",
	} {
		packageDir := filepath.Join(applicationDir, filepath.FromSlash(relative))
		var productionFiles int
		entries, err := os.ReadDir(packageDir)
		if err != nil {
			t.Fatalf("read application/%s: %v", relative, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			productionFiles++
			path := filepath.Join(packageDir, name)
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, imported := range file.Imports {
				importPath, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatalf("parse import %s in %s: %v", imported.Path.Value, path, err)
				}
				if importPath == applicationAssemblyImportPath {
					t.Errorf("application/%s imports application/assembly; use consumer-defined deps/ports", relative)
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				field, ok := node.(*ast.Field)
				if !ok {
					return true
				}
				ast.Inspect(field.Type, func(nested ast.Node) bool {
					identifier, ok := nested.(*ast.Ident)
					if ok && identifier.Name == "Service" {
						t.Errorf("application/%s holds a Service field; inject narrow ports", relative)
					}
					return true
				})
				return true
			})
		}
		if productionFiles == 0 {
			t.Errorf("application/%s has no production Go files", relative)
		}
	}
}

func packageBoundaryApplicationDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	return filepath.Dir(file)
}
