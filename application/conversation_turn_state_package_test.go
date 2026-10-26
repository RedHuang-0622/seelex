package application_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestConversationTurnStateApplicationIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	packageFile := filepath.Join(applicationDir, "conversation", "turn_state.go")
	if _, err := os.Stat(packageFile); err != nil {
		t.Fatalf("application/conversation/turn_state.go is required: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), packageFile, nil, 0)
	if err != nil {
		t.Fatalf("parse application/conversation/turn_state.go: %v", err)
	}
	if file.Name.Name != "conversation" {
		t.Fatalf("application/conversation/turn_state.go package = %q", file.Name.Name)
	}
}
