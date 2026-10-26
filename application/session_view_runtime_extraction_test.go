package application_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// TestSessionViewRuntimeLivesInViewState keeps restoring/epoch/view-intent
// ownership and Snapshot directory enrichment in the Session view owner.
func TestSessionViewRuntimeLivesInViewState(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	legacy := filepath.Join(applicationDir, "core", "service_snapshot.go")
	if _, err := os.Stat(legacy); err == nil {
		t.Errorf("application/core/service_snapshot.go 仍存在；Session view runtime 必须由 application/session/viewstate 拥有")
	} else if !os.IsNotExist(err) {
		t.Fatalf("检查 %s: %v", legacy, err)
	}

	runtimePath := filepath.Join(applicationDir, "session", "viewstate", "session_runtime.go")
	if _, err := parser.ParseFile(token.NewFileSet(), runtimePath, nil, 0); err != nil {
		t.Fatalf("读取 Session view runtime %s: %v", runtimePath, err)
	}
}
