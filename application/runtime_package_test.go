package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeApplicationIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	packageFile := filepath.Join(applicationDir, "admin", "runtime.go")
	if _, err := os.Stat(packageFile); err != nil {
		t.Fatalf("application/admin/runtime.go is required: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), packageFile, nil, 0)
	if err != nil {
		t.Fatalf("parse application/admin/runtime.go: %v", err)
	}
	if file.Name.Name != "admin" {
		t.Fatalf("application/admin/runtime.go package = %q", file.Name.Name)
	}
	coreFile := filepath.Join(applicationDir, "core", "runtime_projection.go")
	core, err := parser.ParseFile(token.NewFileSet(), coreFile, nil, 0)
	if err != nil {
		t.Fatalf("parse core/runtime_projection.go: %v", err)
	}
	for _, declaration := range core.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if ok && (typeSpec.Name.Name == "runtimeApplication" ||
				typeSpec.Name.Name == "runtimeApplicationDeps") {
				t.Errorf("core still owns %s", typeSpec.Name.Name)
			}
		}
	}
}
