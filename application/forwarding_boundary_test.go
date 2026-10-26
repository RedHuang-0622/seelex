package application_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"
)

// TestMigratedServiceMethodsRemainOneLineForwards preserves the compatibility
// rule for already-extracted capabilities: core may adapt ports and forward,
// but it may not grow a second implementation beside the domain package.
func TestMigratedServiceMethodsRemainOneLineForwards(t *testing.T) {
	applicationDir := sourceApplicationDir(t)
	targets := map[string]bool{
		"UpdateWorkItemStatus":       true,
		"ListSubagentRecovery":       true,
		"ResumeInterruptedSubagents": true,
		"ResumeSubagent":             true,
		"ForkSubagents":              true,
		"SubagentSessionDetail":      true,
		"ClearSubagentTree":          true,
		"RequestFileReview":          true,
		"RefreshRuntimeSnapshot":     true,
		"ScheduleTask":               true,
		"UpdateScheduledTask":        true,
		"CancelScheduledTask":        true,
		"AssembleScheduledRun":       true,
		"StartScheduledSession":      true,
		"ActivateSkill":              true,
		"SetReasoningEffort":         true,
		"SetLoopLimit":               true,
		"SetSessionReleaseWindow":    true,
		"SessionReleaseWindow":       true,
		"SetPermissionTier":          true,
		"SetFullAccess":              true,
		"CompactContextNow":          true,
		"CompactContextHandler":      true,
		"EditUserMessageAndFork":     true,
		"GoalGovernanceViewFor":      true,
		"GoalBeginFor":               true,
		"GoalUpdateFor":              true,
		"GoalDoneFor":                true,
		"GoalStatusFor":              true,
		"UnloadSession":              true,
		"ArchiveSession":             true,
		"SetSessionMeta":             true,
		"BeginGracefulShutdown":      true,
		"WaitForIdle":                true,
		"AnyChatRunning":             true,
		"CancelAllChats":             true,
		"CancelChat":                 true,
		"Shutdown":                   true,
		"ReadCompressedTurnHandler":  true,
	}
	found := make(map[string]bool, len(targets))
	coreDir := filepath.Join(applicationDir, "core")
	entries, err := os.ReadDir(coreDir)
	if err != nil {
		t.Fatalf("read core: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !isGoFile(name) || isTestGoFile(name) {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(coreDir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || !targets[function.Name.Name] || !isServiceReceiver(function) {
				continue
			}
			found[function.Name.Name] = true
			if !isSingleForwardStatement(function.Body) {
				t.Errorf("Service.%s must remain a one-line forward to its domain application", function.Name.Name)
			}
		}
	}
	for method := range targets {
		if !found[method] {
			t.Errorf("Service.%s is missing from core compatibility entries", method)
		}
	}
}

func isSingleForwardStatement(body *ast.BlockStmt) bool {
	if body == nil || len(body.List) != 1 {
		return false
	}
	var expression ast.Expr
	switch statement := body.List[0].(type) {
	case *ast.ReturnStmt:
		if len(statement.Results) != 1 {
			return false
		}
		expression = statement.Results[0]
	case *ast.ExprStmt:
		expression = statement.X
	default:
		return false
	}
	_, ok := expression.(*ast.CallExpr)
	return ok
}

func isServiceReceiver(function *ast.FuncDecl) bool {
	if function.Recv == nil || len(function.Recv.List) != 1 {
		return false
	}
	expression := function.Recv.List[0].Type
	if pointer, ok := expression.(*ast.StarExpr); ok {
		expression = pointer.X
	}
	identifier, ok := expression.(*ast.Ident)
	return ok && identifier.Name == "Service"
}

func isGoFile(name string) bool {
	return len(name) > 3 && name[len(name)-3:] == ".go"
}

func isTestGoFile(name string) bool {
	return len(name) > 8 && name[len(name)-8:] == "_test.go"
}
