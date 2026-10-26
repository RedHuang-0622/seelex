package core

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func TestChatTurnFinalizeRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("conversationTurnFinalize")
	if !ok {
		t.Fatal("serviceComponents 未装配 conversationTurnFinalize")
	}
	if field.Type != reflect.TypeOf((*conversationTurnFinalize)(nil)) {
		t.Fatalf("conversationTurnFinalize 字段类型 = %s", field.Type)
	}
	finalizeType := reflect.TypeOf((*conversationTurnFinalize)(nil))
	if _, ok := finalizeType.MethodByName("Finalize"); !ok {
		t.Fatal("conversationTurnFinalize 缺少 Finalize")
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
			switch value := node.(type) {
			case *ast.CompositeLit:
				if identifier, ok := value.Type.(*ast.Ident); ok && identifier.Name == "chatTurnPublication" {
					t.Error("runChat 仍直接构造 chatTurnPublication")
				}
			case *ast.SelectorExpr:
				if value.Sel.Name == "conversationTurnTail" {
					t.Error("runChat 仍直接访问 conversationTurnTail")
				}
			}
			return true
		})
		return
	}
}

func TestConversationTurnFinalizeMapsCommitToPublication(t *testing.T) {
	location := session_runtime.Location{WorkspaceID: "workspace-a"}
	promotion := chatTurnPromotion{Promoted: true, RequestID: "turn-b", Context: nil}
	nextUser := &model.Message{ID: "user"}
	nextAssistant := &model.Message{ID: "assistant"}
	turnErr := errors.New("turn failed")
	var gotLocation session_runtime.Location
	var gotPublication chatTurnPublication
	finalize := newConversationTurnFinalize(conversationTurnFinalizeDeps{
		TailComplete: func(got session_runtime.Location, publication chatTurnPublication, nextContext context.Context) {
			gotLocation = got
			gotPublication = publication
		},
	})

	finalize.Finalize(chatTurnCommitResult{
		SessionID: "session-a", RequestID: "turn-a",
		Location: location, TurnErr: turnErr, VisibleError: "visible",
		Promotion: promotion, NextUser: nextUser, NextAssistant: nextAssistant, Revision: 9,
	})

	if gotLocation != location {
		t.Fatalf("location = %+v", gotLocation)
	}
	if gotPublication.SessionID != "session-a" || gotPublication.RequestID != "turn-a" ||
		gotPublication.Revision != 9 || gotPublication.VisibleError != "visible" ||
		gotPublication.TurnErr != turnErr || gotPublication.Promotion.RequestID != "turn-b" ||
		gotPublication.NextUser != nextUser || gotPublication.NextAssistant != nextAssistant {
		t.Fatalf("publication = %+v", gotPublication)
	}
}

func TestConversationTurnFinalizeIgnoresStaleCommit(t *testing.T) {
	finalize := newConversationTurnFinalize(conversationTurnFinalizeDeps{
		TailComplete: func(session_runtime.Location, chatTurnPublication, context.Context) {
			t.Fatal("stale commit 不得发布或启动 tail")
		},
	})

	finalize.Finalize(chatTurnCommitResult{Stale: true})
}
