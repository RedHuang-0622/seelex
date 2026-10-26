package application_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionHistoryRuntimeLivesInSessionHistoryPackage(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	if _, err := os.Stat(filepath.Join(applicationDir, "core", "session_history.go")); err == nil {
		t.Error("application/core/session_history.go 仍存在；history/restore runtime 必须归 application/session/history")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(applicationDir, "session", "history", "history_runtime.go")); err != nil {
		t.Fatalf("session history runtime owner missing: %v", err)
	}
}
