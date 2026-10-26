package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestToolHookBridgeIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	path := filepath.Join(applicationDir, "execution", "toolhooks", "bridge.go")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("application/execution/toolhooks/bridge.go is required: %v", err)
	}

	corePath := filepath.Join(applicationDir, "core", "tool_hooks.go")
	file, err := parser.ParseFile(token.NewFileSet(), corePath, nil, 0)
	if err != nil {
		t.Fatalf("解析 core tool hooks: %v", err)
	}
	for _, name := range []string{"beginTool", "completeTool", "nextToolIDLocked", "toolHookKey", "observeDiagnostic"} {
		if fileHasFunc(file, name) {
			t.Errorf("duplicate tool-hook state machine %s remains in application/core/tool_hooks.go; "+
				"move it to application/execution/toolhooks", name)
		}
	}
}

func fileHasFunc(file *ast.File, name string) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		declaration, ok := node.(*ast.FuncDecl)
		if ok && declaration.Name.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}
