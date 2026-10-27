package assembly

import (
	"errors"
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

func TestChatTurnInputRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("conversationTurnInput")
	if !ok {
		t.Fatal("serviceComponents 未装配 conversationTurnInput")
	}
	if field.Type != reflect.TypeOf((*applicationconversation.TurnInputApplication)(nil)) {
		t.Fatalf("conversationTurnInput 字段类型 = %s", field.Type)
	}
	inputType := reflect.TypeOf((*applicationconversation.TurnInputApplication)(nil))
	if _, ok := inputType.MethodByName("Prepare"); !ok {
		t.Fatal("conversationTurnInput 缺少 Prepare")
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
			if ok && (selector.Sel.Name == "ApplyActiveTaskSystemPromptFor" ||
				selector.Sel.Name == "PrepareExecutionContextFor") {
				t.Errorf("runChat 仍直接调用 %s", selector.Sel.Name)
			}
			return true
		})
		return
	}
}

func TestConversationTurnInputPreparesPromptAndContext(t *testing.T) {
	var calls []string
	input := applicationconversation.NewTurnInput(applicationconversation.TurnInputDeps{
		ApplyPrompt: func(sessionID, requestID string) {
			calls = append(calls, "prompt:"+sessionID+":"+requestID)
		},
		PrepareContext: func(sessionID, requestID, modelInput string) (string, error) {
			calls = append(calls, "context:"+modelInput)
			return "prepared", nil
		},
		Normalize: func(value string) string {
			calls = append(calls, "normalize:"+value)
			return value + "-normalized"
		},
	})

	got, err := input.Prepare("session-a", "turn-a", "input")

	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got != "prepared-normalized" {
		t.Fatalf("input = %q", got)
	}
	if got := strings.Join(calls, ","); got != "prompt:session-a:turn-a,context:input,normalize:prepared" {
		t.Fatalf("calls = %q", got)
	}
}

func TestConversationTurnInputStopsOnContextError(t *testing.T) {
	prepareErr := errors.New("context failed")
	input := applicationconversation.NewTurnInput(applicationconversation.TurnInputDeps{
		ApplyPrompt: func(string, string) {},
		PrepareContext: func(string, string, string) (string, error) {
			return "", prepareErr
		},
		Normalize: func(string) string {
			t.Fatal("context 失败后不得归一化输入")
			return ""
		},
	})

	_, err := input.Prepare("session-a", "turn-a", "input")

	if !errors.Is(err, prepareErr) {
		t.Fatalf("Prepare error = %v, want context error", err)
	}
}
