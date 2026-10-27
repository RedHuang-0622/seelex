package application_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestApplicationDoesNotImportMiddleBoardPackage(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	ownerPath := filepath.Join(applicationDir, "..", "kernel", "board", "projection.go")
	if _, err := os.Stat(ownerPath); err != nil {
		t.Fatalf("board owner missing: %v", err)
	}
	assemblyDir := filepath.Join(applicationDir, "assembly")
	entries, err := os.ReadDir(assemblyDir)
	if err != nil {
		t.Fatalf("read core: %v", err)
	}
	const forbidden = "github.com/RedHuang-0622/seelex/middle/view/board"
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
		t.Fatalf("application/assembly still imports Middle board package: %v", violations)
	}
	err = filepath.WalkDir(applicationDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !isGoFile(entry.Name()) || isTestGoFile(entry.Name()) {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if importPath == forbidden {
				relative, _ := filepath.Rel(applicationDir, path)
				violations = append(violations, filepath.ToSlash(relative))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk application: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("Application imports Middle board instead of kernel/board:\n%s",
			strings.Join(violations, "\n"))
	}
}
