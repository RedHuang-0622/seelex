package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// TestConversationHistoryApplicationIsExtractedFromCore pins the history-window
// application boundary. Core may keep cold reads and view installation, but the
// paging decision and page/install types belong to application/history.
func TestConversationHistoryApplicationIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	packageFile := filepath.Join(applicationDir, "history", "application.go")
	if _, err := os.Stat(packageFile); err != nil {
		t.Fatalf("application/history/application.go is required: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), packageFile, nil, 0)
	if err != nil {
		t.Fatalf("parse application/history/application.go: %v", err)
	}
	if file.Name.Name != "history" {
		t.Fatalf("application/history package name = %q, want history", file.Name.Name)
	}

	coreFile := filepath.Join(applicationDir, "core", "session_conversation_history_query.go")
	core, err := parser.ParseFile(token.NewFileSet(), coreFile, nil, 0)
	if err != nil {
		t.Fatalf("parse core/session_conversation_history_query.go: %v", err)
	}
	forbiddenTypes := map[string]bool{
		"conversationHistoryQuery":     true,
		"conversationHistoryQueryDeps": true,
	}
	for _, declaration := range core.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if ok && forbiddenTypes[typeSpec.Name.Name] {
				t.Errorf("core still owns %s; move history paging decisions to application/history",
					typeSpec.Name.Name)
			}
		}
	}
}
