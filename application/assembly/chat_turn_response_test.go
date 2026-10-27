package assembly

import (
	"context"
	"errors"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/contract"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestChatTurnResponseRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("conversationTurnResponse")
	if !ok {
		t.Fatal("serviceComponents 未装配 conversationTurnResponse")
	}
	if field.Type != reflect.TypeOf((*applicationconversation.TurnResponseApplication)(nil)) {
		t.Fatalf("conversationTurnResponse 字段类型 = %s", field.Type)
	}
	responseType := reflect.TypeOf((*applicationconversation.TurnResponseApplication)(nil))
	if _, ok := responseType.MethodByName("Finalize"); !ok {
		t.Fatal("conversationTurnResponse 缺少 Finalize")
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
			if ok && (selector.Sel.Name == "attachLatestReasoning" ||
				selector.Sel.Name == "ObserveModelOutput" ||
				selector.Sel.Name == "EnsureFinalAssistantTranscript" ||
				selector.Sel.Name == "BackfillAssistantReasoning") {
				t.Errorf("runChat 仍直接调用 %s", selector.Sel.Name)
			}
			return true
		})
		return
	}
}

func TestConversationTurnResponseFinalizesSuccessfulReply(t *testing.T) {
	history := []contract.EngineMessage{{Role: "assistant", Content: "reply"}}
	var calls []string
	response := applicationconversation.NewTurnResponse(applicationconversation.TurnResponseDeps{
		AttachReasoning: func(sessionID, requestID string) { calls = append(calls, "reasoning") },
		ObserveOutput: func(ctx context.Context, requestID, reply string, err error) error {
			calls = append(calls, "observe:"+reply)
			if err != nil {
				t.Fatalf("observe err = %v", err)
			}
			return errors.New("ignored observe error")
		},
		EnsureFinal: func(requestID, reply string) { calls = append(calls, "final:"+reply) },
		HistoryFor:  func(sessionID string) []contract.EngineMessage { return history },
		BackfillReasoning: func(sessionID string, got []contract.EngineMessage) {
			calls = append(calls, "backfill")
		},
	})

	response.Finalize(context.Background(), "session-a", "turn-a", "reply", nil)

	want := []string{"reasoning", "observe:reply", "final:reply", "backfill"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestConversationTurnResponseSkipsReasoningOnErrorAndEmptyFinal(t *testing.T) {
	turnErr := errors.New("provider failed")
	var calls []string
	response := applicationconversation.NewTurnResponse(applicationconversation.TurnResponseDeps{
		AttachReasoning: func(string, string) { calls = append(calls, "reasoning") },
		ObserveOutput: func(_ context.Context, _, reply string, err error) error {
			if !errors.Is(err, turnErr) {
				t.Fatalf("observe err = %v", err)
			}
			calls = append(calls, "observe")
			return nil
		},
		EnsureFinal: func(string, string) { calls = append(calls, "final") },
		HistoryFor:  func(string) []contract.EngineMessage { return nil },
		BackfillReasoning: func(string, []contract.EngineMessage) {
			calls = append(calls, "backfill")
		},
	})

	response.Finalize(context.Background(), "session-a", "turn-a", "", turnErr)

	want := []string{"observe", "backfill"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}
