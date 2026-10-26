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

func TestApplicationDoesNotImportViewErrorPresentation(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	ownerPath := filepath.Join(applicationDir, "errorpresentation", "presentation.go")
	if _, err := os.Stat(ownerPath); err != nil {
		t.Fatalf("error presentation owner missing: %v", err)
	}
	coreDir := filepath.Join(applicationDir, "core")
	entries, err := os.ReadDir(coreDir)
	if err != nil {
		t.Fatalf("read core: %v", err)
	}
	const forbidden = "github.com/RedHuang-0622/seelex/middle/view/errors"
	var violations []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isGoFile(name) || isTestGoFile(name) {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(coreDir, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("unquote %s import: %v", name, err)
			}
			if strings.TrimSpace(path) == forbidden {
				violations = append(violations, name)
			}
		}
	}
	if len(violations) > 0 {
		t.Fatalf("application/core still imports view error presentation: %v", violations)
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
		t.Fatalf("Application imports Middle view errors instead of application/errorpresentation:\n%s",
			strings.Join(violations, "\n"))
	}
}
