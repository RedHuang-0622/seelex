package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
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

	coreFiles := []string{"tool_record.go", "tool_record_stage.go"}
	for _, name := range coreFiles {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(applicationDir, "core", name), nil, 0)
		if err != nil {
			t.Fatalf("解析 core %s: %v", name, err)
		}
		for _, duplicate := range []string{
			"toolRecordAccumulators",
			"toolRecordStages",
			"toolStatusFailed",
			"toolRecordReason",
		} {
			if fileDeclares(file, duplicate) {
				t.Errorf("duplicate tool-record state machine %s remains in application/core/%s", duplicate, name)
			}
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
