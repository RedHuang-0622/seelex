package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestGoalApplicationIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	packageFile := filepath.Join(applicationDir, "goal", "application.go")
	if _, err := os.Stat(packageFile); err != nil {
		t.Fatalf("application/goal/application.go is required: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), packageFile, nil, 0)
	if err != nil {
		t.Fatalf("parse application/goal/application.go: %v", err)
	}
	if file.Name.Name != "goal" {
		t.Fatalf("application/goal package = %q", file.Name.Name)
	}
	assemblyFile := filepath.Join(applicationDir, "assembly", "goal_application.go")
	core, err := parser.ParseFile(token.NewFileSet(), assemblyFile, nil, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("parse core/goal_application.go: %v", err)
	}
	for _, declaration := range core.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if ok && (typeSpec.Name.Name == "goalApplication" ||
				typeSpec.Name.Name == "goalApplicationDeps") {
				t.Errorf("core still owns %s", typeSpec.Name.Name)
			}
		}
	}
}
