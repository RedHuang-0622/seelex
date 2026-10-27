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

func TestCoreDoesNotImportGoalDomain(t *testing.T) {
	assemblyDir := filepath.Join(sourceApplicationDir(t), "assembly")
	entries, err := os.ReadDir(assemblyDir)
	if err != nil {
		t.Fatalf("read core: %v", err)
	}
	const forbidden = "github.com/RedHuang-0622/seelex/backend/multiagent/goal"
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
		t.Fatalf("application/assembly still imports goal domain: %v", violations)
	}
}

func TestGoalApplicationDoesNotImportStore(t *testing.T) {
	goalDir := filepath.Join(sourceApplicationDir(t), "goal")
	entries, err := os.ReadDir(goalDir)
	if err != nil {
		t.Fatalf("read goal application: %v", err)
	}
	const forbidden = "github.com/RedHuang-0622/seelex/store"
	var violations []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isGoFile(name) || isTestGoFile(name) {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(goalDir, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("unquote %s import: %v", name, err)
			}
			if path == forbidden || strings.HasPrefix(path, forbidden+"/") {
				violations = append(violations, name)
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("application/goal production files still import store: %v", violations)
	}
}

func TestApplicationProductionDoesNotImportStore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	const forbidden = "github.com/RedHuang-0622/seelex/store"
	var violations []string
	err := filepath.WalkDir(applicationDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		name := entry.Name()
		if !isGoFile(name) || isTestGoFile(name) {
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
			if importPath == forbidden || strings.HasPrefix(importPath, forbidden+"/") {
				relative, _ := filepath.Rel(applicationDir, path)
				violations = append(violations, filepath.ToSlash(relative))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan Application Store imports: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("application production files still import store: %v", violations)
	}
}
