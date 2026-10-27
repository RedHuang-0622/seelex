package application_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestCoreDoesNotImportEngineTaskContext(t *testing.T) {
	assemblyDir := filepath.Join(sourceApplicationDir(t), "assembly")
	entries, err := os.ReadDir(assemblyDir)
	if err != nil {
		t.Fatalf("read core: %v", err)
	}
	const forbidden = "github.com/RedHuang-0622/seelex/backend/engine/task"
	var violations []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isGoFile(name) || isTestGoFile(name) {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(assemblyDir, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("unquote %s import: %v", name, err)
			}
			if path == forbidden {
				violations = append(violations, name)
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("application/assembly still imports Engine task context: %v", violations)
	}
}
