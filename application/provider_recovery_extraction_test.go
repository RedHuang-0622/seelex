package application_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProviderRecoveryLivesInConversationContext(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	if _, err := os.Stat(filepath.Join(applicationDir, "core", "history_safety.go")); err == nil {
		t.Error("application/core/history_safety.go 仍存在；provider recovery 必须归 conversation/context")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(applicationDir, "conversation", "context", "recovery.go")); err != nil {
		t.Fatalf("conversation/context recovery owner 缺失: %v", err)
	}
}
