package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestChatTurnRunnerIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	path := filepath.Join(applicationDir, "conversation", "turn_runner.go")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("application/conversation/turn_runner.go is required: %v", err)
	}
	chatPath := filepath.Join(applicationDir, "core", "chat.go")
	file, err := parser.ParseFile(token.NewFileSet(), chatPath, nil, 0)
	if err != nil {
		t.Fatalf("解析 core chat.go: %v", err)
	}
	runChat := findFunc(file, "runChat")
	if runChat == nil {
		t.Fatal("core runChat compatibility wrapper is missing")
	}
	if runChat.Body == nil || len(runChat.Body.List) != 1 {
		t.Fatal("core runChat must be a one-statement compatibility wrapper")
	}
}

func findFunc(file *ast.File, name string) *ast.FuncDecl {
	var found *ast.FuncDecl
	ast.Inspect(file, func(node ast.Node) bool {
		declaration, ok := node.(*ast.FuncDecl)
		if ok && declaration.Name.Name == name {
			found = declaration
			return false
		}
		return true
	})
	return found
}
