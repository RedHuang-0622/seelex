package application_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/application"
)

// applicationBudget 是 Application 结构的可审计预算。预算只允许下调；
// 方法集与直接依赖白名单分开保存，避免“删一个、加一个”的净零漂移。
type applicationBudget struct {
	ServiceMethodCount                 int      `json:"service_method_count"`
	CoreInternalDependencyLimit        int      `json:"core_internal_dependency_limit"`
	CoreProductionFileLimit            int      `json:"core_production_file_limit"`
	CoreProductionLineLimit            int      `json:"core_production_line_limit"`
	AllowedCoreInternalDependencies    []string `json:"allowed_core_internal_dependencies"`
	CompatibilityLedger                string   `json:"compatibility_ledger"`
	CompatibilityLedgerRequiredHeading string   `json:"compatibility_ledger_required_heading"`
}

func TestApplicationArchitectureBudget(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	var budget applicationBudget
	readJSON(t, filepath.Join(applicationDir, "testdata", "application_budget.json"), &budget)

	wantMethods := readLines(t, filepath.Join(applicationDir, "testdata", "service_methods.txt"))
	gotMethods := exportedServiceMethods()
	bothEmpty := len(gotMethods) == 0 && len(wantMethods) == 0
	if !bothEmpty && !reflect.DeepEqual(gotMethods, wantMethods) {
		t.Fatalf("Service 公开方法集漂移（必须显式更新预算；允许减少，不允许静默新增）:\n got=%v\nwant=%v", gotMethods, wantMethods)
	}
	if len(gotMethods) != budget.ServiceMethodCount {
		t.Fatalf("Service 方法预算 = %d，实际 = %d", budget.ServiceMethodCount, len(gotMethods))
	}

	gotDeps := directInternalDependencies(t, filepath.Join(applicationDir, "core"))
	if len(gotDeps) > budget.CoreInternalDependencyLimit {
		t.Fatalf("application/core 直接内部依赖 = %d，预算 = %d:\n%s",
			len(gotDeps), budget.CoreInternalDependencyLimit, strings.Join(gotDeps, "\n"))
	}
	allowed := make(map[string]bool, len(budget.AllowedCoreInternalDependencies))
	for _, dependency := range budget.AllowedCoreInternalDependencies {
		allowed[dependency] = true
	}
	for _, dependency := range gotDeps {
		if !allowed[dependency] {
			t.Fatalf("application/core 新增未登记的直接内部依赖：%s", dependency)
		}
	}

	productionFiles, productionLines := coreProductionSize(t, filepath.Join(applicationDir, "core"))
	if productionFiles > budget.CoreProductionFileLimit {
		t.Fatalf("application/core 生产 Go 文件 = %d，预算 = %d：包提取必须减少或显式收紧预算",
			productionFiles, budget.CoreProductionFileLimit)
	}
	if productionLines > budget.CoreProductionLineLimit {
		t.Fatalf("application/core 生产代码 = %d 行，预算 = %d：包提取必须让 core 净下降或显式更新预算",
			productionLines, budget.CoreProductionLineLimit)
	}

	ledgerPath := filepath.Join(applicationDir, "..", budget.CompatibilityLedger)
	ledger, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatalf("读取兼容层台账 %s: %v", ledgerPath, err)
	}
	if !strings.Contains(string(ledger), budget.CompatibilityLedgerRequiredHeading) {
		t.Fatalf("兼容层台账缺少必需章节 %q", budget.CompatibilityLedgerRequiredHeading)
	}
}

func sourceApplicationDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位预算测试源码")
	}
	return filepath.Dir(file)
}

func readJSON(t *testing.T, path string, target any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s: %v", path, err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("解析 %s: %v", path, err)
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s: %v", path, err)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return []string{}
	}
	lines := strings.Split(trimmed, "\n")
	for index := range lines {
		lines[index] = strings.TrimSpace(lines[index])
	}
	sort.Strings(lines)
	return lines
}

func exportedServiceMethods() []string {
	serviceType := reflect.TypeOf((*application.Service)(nil))
	names := make([]string, 0, serviceType.NumMethod())
	for index := 0; index < serviceType.NumMethod(); index++ {
		names = append(names, serviceType.Method(index).Name)
	}
	sort.Strings(names)
	return names
}

func directInternalDependencies(t *testing.T, packageDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		t.Fatalf("读取 %s: %v", packageDir, err)
	}
	dependencies := make(map[string]bool)
	fileSet := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, filepath.Join(packageDir, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("解析 %s: %v", name, err)
		}
		for _, imported := range file.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("解析 %s 的 import %s: %v", name, imported.Path.Value, err)
			}
			if isApplicationInternalDependency(path) {
				dependencies[path] = true
			}
		}
	}
	result := make([]string, 0, len(dependencies))
	for dependency := range dependencies {
		result = append(result, dependency)
	}
	sort.Strings(result)
	return result
}

func isApplicationInternalDependency(path string) bool {
	const prefix = "github.com/RedHuang-0622/seelex/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	relative := strings.TrimPrefix(path, prefix)
	return relative == "store" ||
		strings.HasPrefix(relative, "store/") ||
		strings.HasPrefix(relative, "backend/") ||
		strings.HasPrefix(relative, "middle/")
}

func coreProductionSize(t *testing.T, packageDir string) (files, lines int) {
	t.Helper()
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		t.Fatalf("读取 %s: %v", packageDir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files++
		raw, err := os.ReadFile(filepath.Join(packageDir, name))
		if err != nil {
			t.Fatalf("读取 %s: %v", name, err)
		}
		lines += sourceLineCount(raw)
	}
	return files, lines
}

func sourceLineCount(raw []byte) int {
	if len(raw) == 0 {
		return 0
	}
	lines := strings.Count(string(raw), "\n")
	if raw[len(raw)-1] != '\n' {
		lines++
	}
	return lines
}

func TestQueryImplementationsAvoidSharedWriteLockAndServiceField(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	queries := []struct {
		path     string
		typeName string
	}{
		{
			path:     filepath.Join(applicationDir, "session", "query", "application.go"),
			typeName: "Application",
		},
		{
			path:     filepath.Join(applicationDir, "history", "application.go"),
			typeName: "Application",
		},
	}
	for _, query := range queries {
		file, err := parser.ParseFile(token.NewFileSet(), query.path, nil, 0)
		if err != nil {
			t.Fatalf("解析查询实现 %s: %v", query.path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			identifier, ok := selector.X.(*ast.Ident)
			if ok && identifier.Name == "ViewMu" && selector.Sel.Name == "Lock" {
				t.Errorf("%s 在查询路径获取了共享写锁 ViewMu.Lock", filepath.Base(query.path))
			}
			return true
		})
		ast.Inspect(file, func(node ast.Node) bool {
			declaration, ok := node.(*ast.TypeSpec)
			if !ok || declaration.Name.Name != query.typeName {
				return true
			}
			structure, ok := declaration.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range structure.Fields.List {
				ast.Inspect(field.Type, func(nested ast.Node) bool {
					identifier, ok := nested.(*ast.Ident)
					if ok && identifier.Name == "Service" {
						t.Errorf("%s 持有完整 Service 门面", declaration.Name.Name)
					}
					return true
				})
			}
			return true
		})
	}
}
