package application_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestChatReasoningProjectionIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	path := filepath.Join(applicationDir, "conversation", "reasoning.go")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("application/conversation/reasoning.go is required: %v", err)
	}
	chatPath := filepath.Join(applicationDir, "assembly", "chat.go")
	file, err := parser.ParseFile(token.NewFileSet(), chatPath, nil, 0)
	if err != nil {
		t.Fatalf("解析 core chat.go: %v", err)
	}
	for _, name := range []string{
		"reasoningStepsFromHistory",
		"lastReasoningFromHistory",
		"attachStepReasoningLocked",
		"visibleStepCallIDs",
		"takeReasoningStep",
		"attachLastReasoningLocked",
	} {
		if fileHasFunc(file, name) {
			t.Errorf("%s must live in application/conversation", name)
		}
	}
}
