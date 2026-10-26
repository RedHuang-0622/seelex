package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// TestSessionQueryApplicationIsExtractedFromCore freezes the read-only session
// query boundary. Directory projection, metadata reads and durable reads belong
// to application/session/query; core may only adapt ports and forward.
func TestSessionQueryApplicationIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	packageFile := filepath.Join(applicationDir, "session", "query", "application.go")
	if _, err := os.Stat(packageFile); err != nil {
		t.Fatalf("application/session/query/application.go is required: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), packageFile, nil, 0)
	if err != nil {
		t.Fatalf("parse application/session/query/application.go: %v", err)
	}
	if file.Name.Name != "query" {
		t.Fatalf("application/session/query package name = %q, want query", file.Name.Name)
	}

	coreFile := filepath.Join(applicationDir, "core", "session_query.go")
	core, err := parser.ParseFile(token.NewFileSet(), coreFile, nil, 0)
	if err != nil {
		t.Fatalf("parse core/session_query.go: %v", err)
	}
	forbiddenTypes := map[string]bool{
		"sessionQuery":     true,
		"sessionQueryDeps": true,
	}
	for _, declaration := range core.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if ok && forbiddenTypes[typeSpec.Name.Name] {
				t.Errorf("core still owns %s; move session query use cases to application/session/query",
					typeSpec.Name.Name)
			}
		}
	}
}
