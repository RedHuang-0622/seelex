package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestReviewApplicationIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	packageFile := filepath.Join(applicationDir, "review", "application.go")
	if _, err := os.Stat(packageFile); err != nil {
		t.Fatalf("application/review/application.go is required: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), packageFile, nil, 0)
	if err != nil {
		t.Fatalf("parse application/review/application.go: %v", err)
	}
	if file.Name.Name != "review" {
		t.Fatalf("application/review package name = %q, want review", file.Name.Name)
	}

	coreFile := filepath.Join(applicationDir, "core", "review_files.go")
	core, err := parser.ParseFile(token.NewFileSet(), coreFile, nil, 0)
	if err != nil {
		t.Fatalf("parse core/review_files.go: %v", err)
	}
	forbiddenTypes := map[string]bool{
		"reviewApplication":     true,
		"reviewApplicationDeps": true,
		"reviewApplicationHost": true,
	}
	for _, declaration := range core.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if ok && forbiddenTypes[typeSpec.Name.Name] {
				t.Errorf("core still owns %s; move review application to application/review",
					typeSpec.Name.Name)
			}
		}
	}
}
