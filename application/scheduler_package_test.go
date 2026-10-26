package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestSchedulerApplicationsAreExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	files := map[string]map[string]bool{
		"application.go": {
			"schedulerApplication":     true,
			"schedulerApplicationDeps": true,
		},
		"run.go": {
			"scheduledRunApplication":     true,
			"scheduledRunApplicationDeps": true,
		},
		"session.go": {
			"scheduledSessionApplication":     true,
			"scheduledSessionApplicationDeps": true,
		},
	}
	for name := range files {
		path := filepath.Join(applicationDir, "admin", "scheduler", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("application/admin/scheduler/%s is required: %v", name, err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse application/admin/scheduler/%s: %v", name, err)
		}
		if parsed.Name.Name != "scheduler" {
			t.Fatalf("application/admin/scheduler/%s package = %q", name, parsed.Name.Name)
		}
	}
	coreFile := filepath.Join(applicationDir, "core", "service_scheduler.go")
	core, err := parser.ParseFile(token.NewFileSet(), coreFile, nil, 0)
	if err != nil {
		t.Fatalf("parse core/service_scheduler.go: %v", err)
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
					t.Errorf("core still owns %s", typeSpec.Name.Name)
				}
			}
		}
	}
}
