package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkTablePublisherIsExtractedFromCore(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	for _, name := range []string{"application.go", "trace.go", "consumer.go", "lifecycle.go", "projection.go"} {
		path := filepath.Join(applicationDir, "execution", "table", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("application/execution/table/%s is required: %v", name, err)
		}
	}

	statePath := filepath.Join(applicationDir, "core", "service_state.go")
	stateFile, err := parser.ParseFile(token.NewFileSet(), statePath, nil, 0)
	if err != nil {
		t.Fatalf("解析 core service_state: %v", err)
	}
	for _, field := range []string{"workTablePublisher", "lastPublishedSubagentTreeSig"} {
		if structHasField(stateFile, field) {
			t.Errorf("work-table publisher state %s remains in application/core", field)
		}
	}
	workTablePath := filepath.Join(applicationDir, "core", "work_table.go")
	workTableFile, err := parser.ParseFile(token.NewFileSet(), workTablePath, nil, 0)
	if err != nil {
		t.Fatalf("解析 core work_table: %v", err)
	}
	if fileHasFunc(workTableFile, "workTableEventPayload") {
		t.Error("workTableEventPayload must live in application/execution/table")
	}
	if fileDeclares(workTableFile, "workTableTraceMaxLines") {
		t.Error("workTableTraceMaxLines must live in application/execution/table")
	}
	if fileDeclares(workTableFile, "asyncTriggerRetryDelay") {
		t.Error("asyncTriggerRetryDelay must live in application/execution/table")
	}
	for _, consumer := range []string{"consumeSubagentLifecycle", "consumeTaskChanges"} {
		if fileHasFunc(workTableFile, consumer) {
			t.Errorf("%s must live in application/execution/table", consumer)
		}
	}
	for _, call := range []string{"BuildWorkTable", "BuildWorkTableBatches", "TaskRecordToWorkItem"} {
		if fileCallsSelector(workTableFile, call) {
			t.Errorf("core work_table.go still builds rows via worktable.%s", call)
		}
	}
}

func structHasField(file *ast.File, name string) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		structure, ok := node.(*ast.StructType)
		if !ok {
			return true
		}
		for _, field := range structure.Fields.List {
			for _, ident := range field.Names {
				if ident.Name == name {
					found = true
					return false
				}
			}
		}
		return true
	})
	return found
}

func fileCallsSelector(file *ast.File, name string) bool {
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}
