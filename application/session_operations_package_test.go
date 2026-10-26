package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionLifecycleOperationsAreExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	files := map[string]map[string]bool{
		"lifecycle.go": {
			"sessionLifecycle":     true,
			"sessionLifecycleDeps": true,
		},
		"archive.go": {
			"sessionArchive":     true,
			"sessionArchiveDeps": true,
		},
		"delete.go": {
			"sessionDelete":     true,
			"sessionDeleteDeps": true,
		},
		"draft_submission.go": {
			"sessionDraftSubmission":     true,
			"sessionDraftSubmissionDeps": true,
		},
		"draft_entry.go": {
			"sessionDraftEntry":      true,
			"sessionDraftEntryDeps":  true,
			"sessionDraftEntryState": true,
			"sessionDraftSlot":       true,
		},
		"draft_materialization.go": {
			"sessionDraftMaterialization":       true,
			"sessionDraftMaterializationDeps":   true,
			"sessionDraftMaterializationSource": true,
		},
		"resume.go": {
			"sessionResume":      true,
			"sessionResumeDeps":  true,
			"sessionResumeState": true,
		},
		"restore_recovery.go": {
			"sessionRestoreRecovery":       true,
			"sessionRestoreRecoveryDeps":   true,
			"sessionRestoreRecoveryState":  true,
			"sessionRestoreDraftReset":     true,
			"sessionRestoreDraftResetDeps": true,
		},
		"fork_lineage.go": {
			"sessionForkLineage":     true,
			"sessionForkLineageDeps": true,
		},
		"restoring_shell.go": {
			"sessionRestoringShell":     true,
			"sessionRestoringShellDeps": true,
		},
	}
	for name := range files {
		path := filepath.Join(applicationDir, "session", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("application/session/%s is required: %v", name, err)
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatalf("parse application/session/%s: %v", name, err)
		}
		if parsed.Name.Name != "session" {
			t.Fatalf("application/session/%s package = %q", name, parsed.Name.Name)
		}
	}
	coreFiles := []string{
		filepath.Join(applicationDir, "core", "session_lifecycle_use_case.go"),
		filepath.Join(applicationDir, "core", "session_archive_use_case.go"),
		filepath.Join(applicationDir, "core", "session_delete_use_case.go"),
		filepath.Join(applicationDir, "core", "session_draft_submission.go"),
		filepath.Join(applicationDir, "core", "session_draft_entry.go"),
		filepath.Join(applicationDir, "core", "session_draft_materialization.go"),
		filepath.Join(applicationDir, "core", "session_resume.go"),
		filepath.Join(applicationDir, "core", "session_restore_recovery.go"),
		filepath.Join(applicationDir, "core", "session_restore_draft_reset.go"),
		filepath.Join(applicationDir, "core", "session_fork_lineage.go"),
		filepath.Join(applicationDir, "core", "session_restoring_shell.go"),
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
