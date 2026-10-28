package application_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// TestProjectBoardQueryIsExtractedFromCore pins project-board read ownership.
// WorkTable input composition and team-plan projection belong to the execution
// application; the assembly host only adapts the authoritative hosts.
func TestProjectBoardQueryIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	packageFile := filepath.Join(applicationDir, "execution", "project_board.go")
	if _, err := os.Stat(packageFile); err != nil {
		t.Fatalf("application/execution/project_board.go is required: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), packageFile, nil, 0)
	if err != nil {
		t.Fatalf("parse application/execution/project_board.go: %v", err)
	}
	if file.Name.Name != "execution" {
		t.Fatalf("application/execution package name = %q, want execution", file.Name.Name)
	}
}
