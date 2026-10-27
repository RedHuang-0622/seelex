package application_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestApplicationCoreDirectoryIsRetired pins the final Application
// modularization exit condition. A zero dependency count or an empty facade is
// not enough: the legacy package must no longer exist and therefore cannot keep
// accumulating state or business rules behind a compatibility name.
func TestApplicationCoreDirectoryIsRetired(t *testing.T) {
	legacyPath := filepath.Join(sourceApplicationDir(t), "core")
	if _, err := os.Stat(legacyPath); err == nil {
		t.Fatalf("application/core still exists at %s; move its remaining owners out and delete the package", legacyPath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect application/assembly: %v", err)
	}
}

// TestNoGoPackageImportsRetiredApplicationCore prevents a compatibility import
// from recreating the deleted package. Directory absence alone would leave a
// compile-time alias as the only reminder; the import path itself is retired.
func TestNoGoPackageImportsRetiredApplicationCore(t *testing.T) {
	appDir := sourceApplicationDir(t)
	repoRoot := filepath.Dir(appDir)
	legacyImport := "github.com/RedHuang-0622/seelex/application/core"
	skipped := map[string]bool{
		".git": true, "dist": true, "vendor": true, "_tmp": true,
		"tmp": true, "node_modules": true, ".gocache": true, ".venv": true,
		"_scratch": true, "local": true,
	}
	err := filepath.WalkDir(repoRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != repoRoot && skipped[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imported := range file.Imports {
			if strings.Trim(imported.Path.Value, `"`) == legacyImport {
				t.Errorf("%s still imports retired %s", path, legacyImport)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
