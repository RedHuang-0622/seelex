package assembly

import (
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestChatTurnExecutionRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("conversationTurnExecution")
	if !ok {
		t.Fatal("serviceComponents 未装配 conversationTurnExecution")
	}
	if field.Type != reflect.TypeOf((*applicationconversation.TurnExecutionApplication)(nil)) {
		t.Fatalf("conversationTurnExecution 字段类型 = %s", field.Type)
	}
	executionType := reflect.TypeOf((*applicationconversation.TurnExecutionApplication)(nil))
	if _, ok := executionType.MethodByName("Prepare"); !ok {
		t.Fatal("conversationTurnExecution 缺少 Prepare")
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "chat.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name.Name != "runChat" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && (selector.Sel.Name == "bindSessionProjectRoot" ||
				selector.Sel.Name == "syncSubagentPolicyFor" ||
				selector.Sel.Name == "syncSessionReasoningEffortFor" ||
				selector.Sel.Name == "syncFullAccessFor") {
				t.Errorf("runChat 仍直接调用 %s", selector.Sel.Name)
			}
			return true
		})
		return
	}
}

func TestConversationTurnExecutionPreparesInOrder(t *testing.T) {
	var calls []string
	execution := applicationconversation.NewTurnExecution(applicationconversation.TurnExecutionDeps{
		BindProjectRoot:     func(sessionID string) { calls = append(calls, "root") },
		SyncSubagentPolicy:  func(sessionID string) { calls = append(calls, "subagent") },
		SyncReasoningEffort: func(sessionID string) { calls = append(calls, "reasoning") },
		SyncFullAccess:      func(sessionID string) { calls = append(calls, "full-access") },
	})

	execution.Prepare("session-a")

	want := "root,subagent,reasoning,full-access"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls = %v, want %s", calls, want)
	}
}
