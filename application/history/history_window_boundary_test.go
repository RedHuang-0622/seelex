package history

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestCoreDoesNotImportHistoryWindowPackage(t *testing.T) {
	root := historyRepositoryRoot(t)
	assemblyDir := filepath.Join(root, "application", "assembly")
	entries, err := os.ReadDir(assemblyDir)
	if err != nil {
		t.Fatal(err)
	}
	const forbidden = "github.com/RedHuang-0622/seelex/middle/switch/history"
	var violations []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(assemblyDir, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if path == forbidden {
				violations = append(violations, name)
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("application/assembly still imports middle/switch/history: %v", violations)
	}
}

func historyRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve history boundary test path")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(filename)))
}
