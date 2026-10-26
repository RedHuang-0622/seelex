package application_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionForkRuntimeLivesInSessionForkPackage(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	if _, err := os.Stat(filepath.Join(applicationDir, "core", "session_fork.go")); err == nil {
		t.Error("application/core/session_fork.go 仍存在；Session fork runtime 必须归 application/session/fork")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(applicationDir, "session", "fork", "fork_runtime.go")); err != nil {
		t.Fatalf("session fork runtime owner missing: %v", err)
	}
}
