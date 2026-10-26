package application_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionScopeRuntimeProjectionIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	for _, name := range []string{"runtime.go", "snapshot_read.go", "resident_snapshot.go"} {
		path := filepath.Join(applicationDir, "session", "query", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("application/session/query/%s is required: %v", name, err)
		}
	}
	scopePath := filepath.Join(applicationDir, "core", "session_scope.go")
	file, err := parser.ParseFile(token.NewFileSet(), scopePath, nil, 0)
	if err != nil {
		t.Fatalf("解析 core session_scope: %v", err)
	}
	for _, name := range []string{"sessionRuntimeOf", "cloneGoalGovernanceView"} {
		if fileHasFunc(file, name) {
			t.Errorf("%s must live in application/session/query", name)
		}
	}
	readSnapshot := findFunc(file, "readSnapshotOf")
	if readSnapshot == nil {
		t.Fatal("core readSnapshotOf compatibility wrapper is missing")
	}
	if len(readSnapshot.Body.List) != 1 {
		t.Fatal("readSnapshotOf must be a one-statement wrapper over session/query")
	}
}
