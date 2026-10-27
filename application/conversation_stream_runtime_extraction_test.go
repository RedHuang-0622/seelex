package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestConversationStreamRuntimeLivesInConversation(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	coreChat, err := parser.ParseFile(token.NewFileSet(), filepath.Join(applicationDir, "assembly", "chat.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range coreChat.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name == nil {
			continue
		}
		switch function.Name.Name {
		case "appendDelta", "appendVisibleDelta", "appendVisibleDeltaBackground",
			"attachLatestReasoning", "stampServedRoute", "streamedAssistantTextLocked":
			t.Errorf("application/assembly/chat.go still owns stream method %s", function.Name.Name)
		}
	}
	if _, err := parser.ParseFile(token.NewFileSet(), filepath.Join(applicationDir, "conversation", "stream_runtime.go"), nil, 0); err != nil {
		t.Fatalf("conversation stream runtime owner missing: %v", err)
	}
}
