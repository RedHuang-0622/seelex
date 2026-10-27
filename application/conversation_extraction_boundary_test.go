package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestConversationProjectionOwnersAreExtracted(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	chatPath := filepath.Join(applicationDir, "assembly", "chat.go")
	chat, err := parser.ParseFile(token.NewFileSet(), chatPath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", chatPath, err)
	}
	forbidden := map[string]bool{
		"appendDelta":                  true,
		"appendVisibleDelta":           true,
		"appendVisibleDeltaBackground": true,
		"attachLatestReasoning":        true,
		"stampServedRoute":             true,
		"streamedAssistantTextLocked":  true,
		"reasoningStepsFromHistory":    true,
		"lastReasoningFromHistory":     true,
		"attachStepReasoningLocked":    true,
		"visibleStepCallIDs":           true,
		"takeReasoningStep":            true,
		"attachLastReasoningLocked":    true,
	}
	for _, declaration := range chat.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && forbidden[function.Name.Name] {
			t.Errorf("application/assembly/chat.go still owns extracted method %s", function.Name.Name)
		}
	}
	for _, relative := range []string{"conversation/stream_runtime.go", "conversation/reasoning.go"} {
		if _, err := parser.ParseFile(token.NewFileSet(), filepath.Join(applicationDir, filepath.FromSlash(relative)), nil, 0); err != nil {
			t.Errorf("owner file application/%s missing: %v", relative, err)
		}
	}
}
