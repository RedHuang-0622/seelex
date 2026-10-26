package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestContextCompactionApplicationIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	packageFile := filepath.Join(applicationDir, "conversation", "context", "application.go")
	if _, err := os.Stat(packageFile); err != nil {
		t.Fatalf("application/conversation/context/application.go is required: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), packageFile, nil, 0)
	if err != nil {
		t.Fatalf("parse application/conversation/context/application.go: %v", err)
	}
	if file.Name.Name != "context" {
		t.Fatalf("application/conversation/context package = %q", file.Name.Name)
	}
	coreFile := filepath.Join(applicationDir, "core", "context_compact.go")
	core, err := parser.ParseFile(token.NewFileSet(), coreFile, nil, 0)
	if err != nil {
		t.Fatalf("parse core/context_compact.go: %v", err)
	}
	for _, declaration := range core.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if ok && (typeSpec.Name.Name == "contextCompactionApplication" ||
				typeSpec.Name.Name == "contextCompactionApplicationDeps") {
				t.Errorf("core still owns %s", typeSpec.Name.Name)
			}
		}
	}
}
