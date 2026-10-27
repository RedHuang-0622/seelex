package assembly

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	session_runtime "github.com/RedHuang-0622/seelex/kernel/session"
)

func TestChatTurnTailRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("conversationTurnTail")
	if !ok {
		t.Fatal("serviceComponents 未装配 conversationTurnTail")
	}
	if field.Type != reflect.TypeOf((*conversationTurnTail)(nil)) {
		t.Fatalf("conversationTurnTail 字段类型 = %s", field.Type)
	}
	tailType := reflect.TypeOf((*conversationTurnTail)(nil))
	if _, ok := tailType.MethodByName("Complete"); !ok {
		t.Fatal("conversationTurnTail 缺少 Complete")
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
	banned := map[string]bool{
		"rebindViewWorkspaceWhenIdle": true,
		"QueueMarkConsumedInput":      true,
		"reconcileSessionRelease":     true,
		"FlushToolRecord":             true,
		"runChat":                     true,
	}
	foundRunChat := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		declaration, ok := node.(*ast.FuncDecl)
		if !ok || declaration.Recv == nil || declaration.Name.Name != "runChat" {
			return true
		}
		foundRunChat = true
		ast.Inspect(declaration.Body, func(inner ast.Node) bool {
			selector, ok := inner.(*ast.SelectorExpr)
			if ok && banned[selector.Sel.Name] {
				t.Errorf("runChat 仍直接调用 %s；ChatTurn 异步尾段应集中在 chat_turn_tail.go", selector.Sel.Name)
			}
			return true
		})
		return false
	})
	if foundRunChat {
		t.Fatal("core runChat forwarder must be removed")
	}
}

func TestConversationTurnTailCompletesWithoutPromotion(t *testing.T) {
	var calls []string
	tail := newConversationTurnTail(conversationTurnTailDeps{
		RebindWorkspaceWhenIdle: func() { calls = append(calls, "rebind") },
		MarkPromotedConsumed: func(session_runtime.Location, string, string) error {
			t.Fatal("无队列提升时不得标记 consumed")
			return nil
		},
		Publish: func(chatTurnPublication) { calls = append(calls, "publish") },
		StartNext: func(context.Context, string, string, chatRequest) {
			t.Fatal("无队列提升时不得启动下一轮")
		},
		ReconcileSessionRelease: func() { calls = append(calls, "release") },
		FlushToolRecord:         func(string) { calls = append(calls, "flush") },
	})

	tail.Complete(session_runtime.Location{}, chatTurnPublication{
		SessionID: "session-a",
		RequestID: "turn-a",
	}, nil)

	want := []string{"rebind", "publish", "release", "flush"}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestConversationTurnTailSettlesAndStartsPromotedBatch(t *testing.T) {
	var calls []string
	tail := newConversationTurnTail(conversationTurnTailDeps{
		RebindWorkspaceWhenIdle: func() { calls = append(calls, "rebind") },
		MarkPromotedConsumed: func(_ session_runtime.Location, sessionID, requestID string) error {
			calls = append(calls, "mark:"+sessionID+":"+requestID)
			return errors.New("mark failed")
		},
		Publish: func(chatTurnPublication) { calls = append(calls, "publish") },
		StartNext: func(_ context.Context, sessionID, requestID string, _ chatRequest) {
			calls = append(calls, "start:"+sessionID+":"+requestID)
		},
		ReconcileSessionRelease: func() { calls = append(calls, "release") },
		FlushToolRecord:         func(string) { calls = append(calls, "flush") },
	})

	tail.Complete(session_runtime.Location{}, chatTurnPublication{
		SessionID: "session-a",
		RequestID: "turn-a",
		Promotion: chatTurnPromotion{
			Promoted:  true,
			RequestID: "turn-b",
		},
	}, context.Background())

	want := []string{
		"rebind",
		"mark:session-a:turn-b",
		"publish",
		"start:session-a:turn-b",
		"release",
		"flush",
	}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}
