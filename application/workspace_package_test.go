package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// TestWorkspaceApplicationIsExtractedFromCore freezes the workspace package
// boundary: environment, git and batch-registration orchestration belong to
// application/workspace; core may only retain compatibility forwards and host
// adapters.
func TestWorkspaceApplicationIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	packageFile := filepath.Join(applicationDir, "workspace", "application.go")
	if _, err := os.Stat(packageFile); err != nil {
		t.Fatalf("application/workspace/application.go is required: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), packageFile, nil, 0)
	if err != nil {
		t.Fatalf("parse application/workspace/application.go: %v", err)
	}
	if file.Name.Name != "workspace" {
		t.Fatalf("application/workspace package name = %q, want workspace", file.Name.Name)
	}

	forbiddenTypes := map[string]bool{
		"workspaceApplication":                true,
		"workspaceApplicationDeps":            true,
		"workspaceEnvironmentApplication":     true,
		"workspaceEnvironmentApplicationDeps": true,
		"workspaceGitApplication":             true,
		"workspaceGitApplicationDeps":         true,
	}
	for _, name := range []string{
		"workspace_environments.go",
		"workspace_gitrepos.go",
		"workspace_usecase.go",
	} {
		source := filepath.Join(applicationDir, "assembly", name)
		if _, err := os.Stat(source); os.IsNotExist(err) {
			continue
		} else if err != nil {
			t.Fatalf("stat core/%s: %v", name, err)
		}
		core, err := parser.ParseFile(token.NewFileSet(), source, nil, 0)
		if err != nil {
			t.Fatalf("parse core/%s: %v", name, err)
		}
		for _, declaration := range core.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if ok && forbiddenTypes[typeSpec.Name.Name] {
					t.Errorf("core still owns %s; move workspace use cases to application/workspace",
						typeSpec.Name.Name)
				}
			}
		}
	}
}
