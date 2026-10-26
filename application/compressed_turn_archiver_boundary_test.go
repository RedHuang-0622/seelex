package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestCompressedTurnArchiverLivesInReferenceApplication(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	coreFile := filepath.Join(applicationDir, "core", "compressed_turn.go")
	referenceFile := filepath.Join(applicationDir, "reference", "archiver.go")

	if _, err := os.Stat(coreFile); err == nil {
		t.Fatal("core still owns compressed_turn.go")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat core compressed turn: %v", err)
	}
	if _, err := os.Stat(referenceFile); err != nil {
		t.Fatalf("application/reference must own the compressed-turn archiver: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(applicationDir, "core"))
	if err != nil {
		t.Fatalf("read core: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isGoFile(name) || isTestGoFile(name) {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(applicationDir, "core", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.GenDecl:
				for _, spec := range declaration.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok && typeSpec.Name.Name == "CompressedTurnArchiver" {
						t.Errorf("%s still aliases CompressedTurnArchiver in core", name)
					}
				}
			case *ast.FuncDecl:
				if declaration.Recv == nil && declaration.Name.Name == "CompressedTurnArchiver" {
					t.Errorf("%s still defines CompressedTurnArchiver in core", name)
				}
			}
		}
	}
}
