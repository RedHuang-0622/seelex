package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestExecutionApplicationsAreExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	files := map[string]map[string]bool{
		"work_item.go": {
			"executionApplication":     true,
			"executionApplicationDeps": true,
		},
		"subagent.go": {
			"subagentApplication":     true,
			"subagentApplicationDeps": true,
		},
		"completion.go": {
			"asyncCompletionApplication":     true,
			"asyncCompletionApplicationDeps": true,
		},
	}
	for name := range files {
		path := filepath.Join(applicationDir, "execution", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("application/execution/%s is required: %v", name, err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse application/execution/%s: %v", name, err)
		}
		if parsed.Name.Name != "execution" {
			t.Fatalf("application/execution/%s package = %q", name, parsed.Name.Name)
		}
	}

	coreFiles := []string{
		filepath.Join(applicationDir, "core", "work_table_application.go"),
		filepath.Join(applicationDir, "core", "subagent_application.go"),
		filepath.Join(applicationDir, "core", "async_completion.go"),
	}
	for _, path := range coreFiles {
		core, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, declaration := range core.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				for _, forbidden := range files {
					if forbidden[typeSpec.Name.Name] {
						t.Errorf("core/%s still owns %s", filepath.Base(path), typeSpec.Name.Name)
					}
				}
			}
		}
	}
}
