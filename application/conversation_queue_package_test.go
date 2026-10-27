package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestConversationQueueApplicationIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	packageFile := filepath.Join(applicationDir, "conversation", "queue.go")
	if _, err := os.Stat(packageFile); err != nil {
		t.Fatalf("application/conversation/queue.go is required: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), packageFile, nil, 0)
	if err != nil {
		t.Fatalf("parse application/conversation/queue.go: %v", err)
	}
	if file.Name.Name != "conversation" {
		t.Fatalf("application/conversation/queue.go package = %q", file.Name.Name)
	}
	assemblyFile := filepath.Join(applicationDir, "assembly", "chat_queue.go")
	core, err := parser.ParseFile(token.NewFileSet(), assemblyFile, nil, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("parse core/chat_queue.go: %v", err)
	}
	for _, declaration := range core.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if ok && (typeSpec.Name.Name == "conversationQueue" ||
				typeSpec.Name.Name == "conversationQueueDeps" ||
				typeSpec.Name.Name == "conversationQueueUnit") {
				t.Errorf("core still owns %s", typeSpec.Name.Name)
			}
		}
	}
}
