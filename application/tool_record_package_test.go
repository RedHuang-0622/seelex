package application_test

import (
	"go/ast"
	"os"
	"path/filepath"
	"testing"
)

func TestToolRecordApplicationIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	path := filepath.Join(applicationDir, "execution", "toolrecord", "application.go")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("application/execution/toolrecord/application.go is required: %v", err)
	}

	assemblyFiles := []string{"tool_record.go", "tool_record_stage.go"}
	for _, name := range assemblyFiles {
		path := filepath.Join(applicationDir, "assembly", name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			t.Fatalf("检查 core %s: %v", name, err)
		}
		t.Errorf("application/assembly/%s must be removed; ToolRecord host adapters live in toolrecord", name)
	}
}

func TestToolRecordOwnsItsInMemoryLedger(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	for _, name := range []string{"accumulator.go", "stage.go"} {
		path := filepath.Join(applicationDir, "execution", "toolrecord", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("tool-record ledger owner file %s is required: %v", name, err)
		}
	}
}

func fileDeclares(file *ast.File, name string) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		switch declaration := node.(type) {
		case *ast.FuncDecl:
			if declaration.Name.Name == name {
				found = true
				return false
			}
		case *ast.TypeSpec:
			if declaration.Name.Name == name {
				found = true
				return false
			}
		}
		return true
	})
	return found
}
