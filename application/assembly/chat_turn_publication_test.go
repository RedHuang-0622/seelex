package assembly

import (
	"errors"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/middle/protocol/event"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestChatTurnPublicationRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("conversationTurnPublisher")
	if !ok {
		t.Fatal("serviceComponents 未装配 conversationTurnPublisher")
	}
	if field.Type != reflect.TypeOf((*applicationconversation.TurnPublisherApplication)(nil)) {
		t.Fatalf("conversationTurnPublisher 字段类型 = %s", field.Type)
	}
	publisherType := reflect.TypeOf((*applicationconversation.TurnPublisherApplication)(nil))
	if _, ok := publisherType.MethodByName("Publish"); !ok {
		t.Fatal("conversationTurnPublisher 缺少 Publish")
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
		"publishChatStateFor":       true,
		"publishSessionEvent":       true,
		"SetCurrentTaskBatch":       true,
		"publishRuntimeProjections": true,
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
				t.Errorf("runChat 仍直接调用 %s；ChatTurn 终态发布规则应集中在 chat_turn_publication.go", selector.Sel.Name)
			}
			return true
		})
		return false
	})
	if foundRunChat {
		t.Fatal("core runChat forwarder must be removed")
	}
}

func TestConversationTurnPublisherPublishesSnapshotWithoutPromotion(t *testing.T) {
	var calls []string
	publisher := applicationconversation.NewTurnPublisher(applicationconversation.TurnPublisherDeps{
		PublishChatState: func(sessionID string) {
			calls = append(calls, "chat:"+sessionID)
		},
		PublishEvent: func(kind event.EventKind, revision uint64, requestID, sessionID string, _ any) {
			calls = append(calls, string(kind)+":"+requestID+":"+sessionID)
		},
		SetCurrentTaskBatch: func(string, string) {
			t.Fatal("无队列提升时不得切换任务批次")
		},
		PublishRuntimeProjections: func() {
			t.Fatal("无队列提升时不得发布下一轮 runtime projection")
		},
	})

	publisher.Publish(chatTurnPublication{
		SessionID: "session-a",
		RequestID: "turn-a",
		Revision:  7,
	})

	want := []string{
		"chat:session-a",
		string(EventSnapshotChanged) + ":turn-a:session-a",
	}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestConversationTurnPublisherReportsErrorBeforePromotedBatch(t *testing.T) {
	var calls []string
	publisher := applicationconversation.NewTurnPublisher(applicationconversation.TurnPublisherDeps{
		PublishChatState: func(sessionID string) {
			calls = append(calls, "chat:"+sessionID)
		},
		PublishEvent: func(kind event.EventKind, _ uint64, requestID, sessionID string, payload any) {
			call := string(kind) + ":" + requestID + ":" + sessionID
			switch value := payload.(type) {
			case model.Message:
				call += ":" + value.ID
			case map[string]string:
				call += ":" + value["message"]
			}
			calls = append(calls, call)
		},
		SetCurrentTaskBatch: func(sessionID, requestID string) {
			calls = append(calls, "batch:"+sessionID+":"+requestID)
		},
		PublishRuntimeProjections: func() {
			calls = append(calls, "projections")
		},
	})

	publisher.Publish(chatTurnPublication{
		SessionID:    "session-a",
		RequestID:    "turn-a",
		Revision:     8,
		VisibleError: "provider down",
		TurnErr:      errors.New("provider down"),
		Promotion: chatTurnPromotion{
			Promoted:  true,
			RequestID: "turn-b",
		},
		NextUser:      &model.Message{ID: "user-b"},
		NextAssistant: &model.Message{ID: "assistant-b"},
	})

	want := []string{
		"chat:session-a",
		string(EventError) + ":turn-a:session-a:provider down",
		string(EventMessageAdded) + ":turn-b:session-a:user-b",
		string(EventMessageAdded) + ":turn-b:session-a:assistant-b",
		"batch:session-a:turn-b",
		"projections",
	}
	if strings.Join(calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}
