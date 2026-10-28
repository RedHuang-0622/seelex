package e2e

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestStoreDDDM10FacadeGate locks the M10 endpoint: capability-discovery exits
// are gone, Board and ToolRecord I/O live in their owning packages, and the
// production import budget for the root package ratchets down.
func TestStoreDDDM10FacadeGate(t *testing.T) {
	repo := repositoryRoot(t)

	rootMethods := scanPackageMethods(t, filepath.Join(repo, "store"))
	for _, name := range []string{"BoardsFor", "BoardsForSession", "TeamworkFor", "ToolRecordFor"} {
		if _, ok := rootMethods[name]; ok {
			t.Errorf("store 根包仍声明能力发现方法 %s；生产调用方必须持有装配期注入的窄端口", name)
		}
	}

	for _, name := range []string{"Boards", "Teamwork", "ToolRecord", "MediaStoreOf"} {
		if _, ok := scanPackageFunctions(t, filepath.Join(repo, "store"))[name]; ok {
			t.Errorf("store 根包仍以自由函数 %s 做运行期能力发现；应由领域包构造器或消费方端口替代", name)
		}
	}

	for _, relative := range []string{
		"store/board/session.go",
		"store/record/store.go",
		"store/stores.go",
	} {
		if _, err := os.Stat(filepath.Join(repo, filepath.FromSlash(relative))); err != nil {
			t.Errorf("M10 领域归位缺失 %s: %v", relative, err)
		}
	}

	assertSourceExcludes(t, filepath.Join(repo, "store", "board.go"),
		"json.Marshal", "json.Unmarshal", "os.OpenFile", "os.ReadFile", "os.MkdirAll")
	assertSourceExcludes(t, filepath.Join(repo, "store", "tool_record.go"),
		"json.Marshal", "json.Unmarshal", "os.OpenFile", "os.ReadFile", "os.MkdirAll")

	productionImports := directRootStoreImports(t, repo, false)
	if len(productionImports) > 80 {
		t.Errorf("生产代码直接 import 根 store 的文件数 %d 超过 M10 上限 80：\n%s",
			len(productionImports), strings.Join(productionImports, "\n"))
	}

	for _, relative := range []string{
		"wiring/bootstrap/bootstrap.go",
		"wiring/bootstrap/runtime.go",
		"wiring/adapters/goal_coordinator.go",
	} {
		text := readText(t, filepath.Join(repo, filepath.FromSlash(relative)))
		for _, forbidden := range []string{
			".BoardsFor(", ".BoardsForSession(", ".TeamworkFor(", ".ToolRecordFor(",
		} {
			if strings.Contains(text, forbidden) {
				t.Errorf("%s 仍在调用能力发现出口 %s；应由组合根把具体实现注入消费方窄端口", relative, forbidden)
			}
		}
	}
}

func scanPackageMethods(t *testing.T, dir string) map[string]struct{} {
	t.Helper()
	methods := make(map[string]struct{})
	for _, path := range productionGoFiles(t, dir) {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil {
				continue
			}
			methods[function.Name.Name] = struct{}{}
		}
	}
	return methods
}

func scanPackageFunctions(t *testing.T, dir string) map[string]struct{} {
	t.Helper()
	functions := make(map[string]struct{})
	for _, path := range productionGoFiles(t, dir) {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil {
				continue
			}
			functions[function.Name.Name] = struct{}{}
		}
	}
	return functions
}

func productionGoFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var paths []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		paths = append(paths, filepath.Join(dir, name))
	}
	return paths
}

func directRootStoreImports(t *testing.T, repo string, tests bool) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(repo, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			relative, _ := filepath.Rel(repo, path)
			switch filepath.ToSlash(relative) {
			case ".git", "store", "tmp", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || (!tests && strings.HasSuffix(path, "_test.go")) {
			return nil
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(text), `"github.com/RedHuang-0622/seelex/store"`) {
			relative, _ := filepath.Rel(repo, path)
			files = append(files, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan root-store imports: %v", err)
	}
	return files
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate e2e test source")
	}
	return filepath.Dir(filepath.Dir(file))
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func assertSourceExcludes(t *testing.T, path string, forbidden ...string) {
	t.Helper()
	text := readText(t, path)
	for _, token := range forbidden {
		if strings.Contains(text, token) {
			t.Errorf("%s 仍包含领域实现 %q；I/O 与判据必须留在领域 owner", path, token)
		}
	}
}
