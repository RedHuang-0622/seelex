package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// TestProjectBoardQueryIsExtractedFromCore pins project-board read ownership.
// WorkTable input composition and team-plan projection belong to the execution
// application; core may only adapt the authoritative hosts.
func TestProjectBoardQueryIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	packageFile := filepath.Join(applicationDir, "execution", "project_board.go")
	if _, err := os.Stat(packageFile); err != nil {
		t.Fatalf("application/execution/project_board.go is required: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), packageFile, nil, 0)
	if err != nil {
		t.Fatalf("parse application/execution/project_board.go: %v", err)
	}
	if file.Name.Name != "execution" {
		t.Fatalf("application/execution package name = %q, want execution", file.Name.Name)
	}

	assemblyFile := filepath.Join(applicationDir, "assembly", "project_board_read.go")
	core, err := parser.ParseFile(token.NewFileSet(), assemblyFile, nil, 0)
	if err != nil {
		t.Fatalf("parse core/project_board_read.go: %v", err)
	}
	forbiddenTypes := map[string]bool{
		"projectBoardQuery":     true,
		"projectBoardQueryDeps": true,
	}
	for _, declaration := range core.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if ok && forbiddenTypes[typeSpec.Name.Name] {
				t.Errorf("core still owns %s; move project-board reads to application/execution",
					typeSpec.Name.Name)
			}
		}
	}
}
