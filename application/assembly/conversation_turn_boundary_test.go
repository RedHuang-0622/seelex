package assembly

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConversationTurnRuntimeHasNoAssemblyForwarder(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "chat.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "runChat" {
			t.Fatal("conversation turn runtime must stay in application/conversation")
		}
	}
}

func TestChatStateWritesUseTurnStateApplication(t *testing.T) {
	scanAssemblySources(t, "chat_turn_state.go", func(name string, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && (selector.Sel.Name == "SetChatState" || selector.Sel.Name == "UpdateChat") {
				t.Errorf("%s directly writes ChatState; use TurnStateApplication", name)
			}
			return true
		})
	})
}

func TestChatTurnRequestConstructionIsCentralized(t *testing.T) {
	banned := map[string]bool{
		"newChatRequest":      true,
		"combineChatRequests": true,
		"queuedChatRequests":  true,
		"chatRequestDisplays": true,
	}
	scanAssemblySources(t, "chat_turn_request.go", func(name string, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if ok && banned[identifier.Name] {
				t.Errorf("%s directly calls %s; request construction belongs to chat_turn_request.go", name, identifier.Name)
			}
			return true
		})
	})
}

func scanAssemblySources(t *testing.T, excluded string, visit func(string, *ast.File)) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == excluded {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		visit(name, file)
	}
}
