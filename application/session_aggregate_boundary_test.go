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

func TestSessionAggregateLivesInApplicationSession(t *testing.T) {
	root := sourceApplicationDir(t)
	for _, name := range []string{"domain.go", "domain_actor.go", "ports.go", "runtime_slot.go"} {
		path := filepath.Join(root, "session", "state", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("application/session/state must own %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "session", "viewstate", "coordinator.go")); err != nil {
		t.Fatalf("application/session/viewstate must own coordinator.go: %v", err)
	}
	assertDirectoryHasNoGoFiles(t, filepath.Join(root, "..", "middle", "switch", "sessionusecase"))
	assertDirectoryHasNoGoFiles(t, filepath.Join(root, "..", "middle", "view", "state"))
	for _, dir := range []string{"../middle"} {
		assertTreeDoesNotImport(t, filepath.Join(root, dir), "middle/view/state")
	}
	assertTreeDoesNotImport(t, filepath.Join(root, "assembly"), "middle/switch/sessionusecase")
}

func assertDirectoryHasNoGoFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			t.Errorf("%s still contains Go source %s", dir, entry.Name())
		}
	}
}

func assertTreeDoesNotImport(t *testing.T, root, forbidden string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
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
			if strings.HasSuffix(importPath, "/"+forbidden) {
				t.Errorf("%s still imports %s", path, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
