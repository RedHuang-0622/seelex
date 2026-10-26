package application_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompactionGateLivesInConversationContext(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	legacy := filepath.Join(applicationDir, "core", "context_compact_gate.go")
	if _, err := os.Stat(legacy); err == nil {
		t.Error("application/core/context_compact_gate.go 仍存在；Compaction gate 必须归 conversation/context")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(applicationDir, "conversation", "context", "compaction_gate.go")); err != nil {
		t.Fatalf("conversation/context compaction gate 缺失: %v", err)
	}
}
