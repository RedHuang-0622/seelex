package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestEditForkApplicationsAreExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	files := map[string]map[string]bool{
		"application.go": {
			"EditApplication":     true,
			"EditApplicationDeps": true,
		},
		"registry.go": {
			"editForkOperationRegistry": true,
			"editForkOperation":         true,
		},
		"validator.go": {
			"editForkValidator":     true,
			"editForkValidatorDeps": true,
			"editForkParentState":   true,
		},
		"resolver.go": {
			"editForkTargetResolver":     true,
			"editForkTargetResolverDeps": true,
		},
		"process.go": {
			"editForkProcess":     true,
			"editForkProcessDeps": true,
		},
		"rollback.go": {
			"editForkRollback":     true,
			"editForkRollbackDeps": true,
		},
		"annotation.go": {
			"editForkAnnotation":      true,
			"editForkAnnotationDeps":  true,
			"editForkAnnotationInput": true,
		},
	}
	for name := range files {
		path := filepath.Join(applicationDir, "conversation", "edit", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("application/conversation/edit/%s is required: %v", name, err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse application/conversation/edit/%s: %v", name, err)
		}
		if parsed.Name.Name != "edit" {
			t.Fatalf("application/conversation/edit/%s package = %q", name, parsed.Name.Name)
		}
	}
	coreFiles := []string{
		"edit_fork_operation_registry.go",
		"edit_fork_validator.go",
		"edit_fork_target_resolver.go",
		"edit_fork_process.go",
		"edit_fork_rollback.go",
		"edit_fork_annotation.go",
		"session_message_edit.go",
	}
	for _, name := range coreFiles {
		path := filepath.Join(applicationDir, "core", name)
		core, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
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
				if !ok {
					continue
				}
				for _, forbidden := range files {
					if forbidden[typeSpec.Name.Name] {
						t.Errorf("core/%s still owns %s", name, typeSpec.Name.Name)
					}
				}
			}
		}
	}
}
