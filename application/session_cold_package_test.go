package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionColdRestoreComponentsAreExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	files := map[string]map[string]bool{
		"read.go": {
			"sessionColdRead":       true,
			"sessionColdReadDeps":   true,
			"sessionColdReadResult": true,
		},
		"history.go": {
			"sessionColdHistory":       true,
			"sessionColdHistoryDeps":   true,
			"sessionColdHistoryResult": true,
		},
		"restore.go": {
			"sessionColdRestore":     true,
			"sessionColdRestoreDeps": true,
		},
		"workspace.go": {
			"sessionColdWorkspace":     true,
			"sessionColdWorkspaceDeps": true,
		},
		"activation.go": {
			"sessionColdActivation":       true,
			"sessionColdActivationDeps":   true,
			"sessionColdActivationResult": true,
		},
		"task.go": {
			"sessionColdTask":     true,
			"sessionColdTaskDeps": true,
		},
		"view.go": {
			"sessionColdView":     true,
			"sessionColdViewDeps": true,
		},
		"commit.go": {
			"sessionColdCommit":       true,
			"sessionColdCommitDeps":   true,
			"sessionColdCommitResult": true,
		},
		"publish.go": {
			"sessionColdPublish":     true,
			"sessionColdPublishDeps": true,
		},
	}
	for name := range files {
		path := filepath.Join(applicationDir, "session", "cold", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("application/session/cold/%s is required: %v", name, err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse application/session/cold/%s: %v", name, err)
		}
		if parsed.Name.Name != "cold" {
			t.Fatalf("application/session/cold/%s package = %q", name, parsed.Name.Name)
		}
	}
	coreFiles := []string{
		"session_cold_load_reader.go",
		"session_cold_history.go",
		"session_cold_restore.go",
		"session_cold_workspace.go",
		"session_cold_activation.go",
		"session_cold_task.go",
		"session_cold_view.go",
		"session_cold_commit.go",
		"session_cold_publish.go",
	}
	for _, name := range coreFiles {
		path := filepath.Join(applicationDir, "core", name)
		core, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
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
