package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
)

func TestServiceComponentsAssembleConversationQueue(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("conversationQueue")
	if !ok {
		t.Fatal("serviceComponents 未装配 conversationQueue")
	}
	if field.Type != reflect.TypeOf((*applicationconversation.QueueApplication)(nil)) {
		t.Fatalf("conversationQueue 字段类型 = %s，want *conversationQueue", field.Type)
	}
}

func TestQueueFacadeOnlyDelegatesToConversationQueue(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "..", "compat", "frontend.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("解析 compat/frontend.go: %v", err)
	}
	want := map[string]bool{"ReorderQueuedInput": true, "RecallQueuedInput": true}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || !want[function.Name.Name] {
			continue
		}
		if len(function.Body.List) != 1 {
			t.Fatalf("%s 仍有 %d 条语句，want 一行 conversationQueue 转发", function.Name.Name, len(function.Body.List))
		}
		returnStatement, ok := function.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(returnStatement.Results) != 1 {
			t.Fatalf("%s 不是单行 return 转发", function.Name.Name)
		}
		call, ok := returnStatement.Results[0].(*ast.CallExpr)
		if !ok {
			t.Fatalf("%s 的 return 不是组件方法调用", function.Name.Name)
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			t.Fatalf("%s 的 return 不是 selector 调用", function.Name.Name)
		}
		componentCall, ok := selector.X.(*ast.CallExpr)
		if !ok {
			t.Fatalf("%s 未转发到 conversationQueue component", function.Name.Name)
		}
		componentSelector, ok := componentCall.Fun.(*ast.SelectorExpr)
		if !ok || componentSelector.Sel.Name != "conversationQueue" {
			t.Fatalf("%s 未经 conversationQueue 转发到队列组件", function.Name.Name)
		}
		delete(want, function.Name.Name)
	}
	for name := range want {
		t.Fatalf("未找到公开队列方法 %s", name)
	}
}

func TestQueueFacadeNilServiceKeepsExistingError(t *testing.T) {
	var service *Service
	if err := frontendOfTestService(service).ReorderQueuedInput("", 0, 1); err == nil || err.Error() != "application is not available" {
		t.Fatalf("nil ReorderQueuedInput error = %v", err)
	}
	if _, err := frontendOfTestService(service).RecallQueuedInput("", 0); err == nil || err.Error() != "application is not available" {
		t.Fatalf("nil RecallQueuedInput error = %v", err)
	}
}

func TestConversationQueuePreservesPresentationEventOrder(t *testing.T) {
	service, _ := startBlockingChat(t)
	enqueueQueuedInputs(t, service, "second", "third")
	subscription := service.Events.Subscribe(16)
	defer subscription.Close()

	if err := frontendOfTestService(service).ReorderQueuedInput("", 0, 1); err != nil {
		t.Fatalf("ReorderQueuedInput: %v", err)
	}
	seen := make([]string, 0, 2)
	deadline := time.After(time.Second)
	for len(seen) < 2 {
		select {
		case published := <-subscription.Events:
			switch published.Kind {
			case EventSnapshotChanged, EventChatChanged:
				seen = append(seen, string(published.Kind))
			}
		case <-deadline:
			t.Fatalf("queue edit events = %v, want snapshot.changed then chat.changed", seen)
		}
	}
	if seen[0] != string(EventSnapshotChanged) || seen[1] != string(EventChatChanged) {
		t.Fatalf("queue edit event order = %v, want [%s %s]", seen, EventSnapshotChanged, EventChatChanged)
	}
}

func TestConversationQueueDoesNotOwnServiceOrDuplicateQueueState(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "..", "conversation", "queue.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("解析 chat_queue.go: %v", err)
	}
	checked := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		declaration, ok := node.(*ast.TypeSpec)
		if !ok || declaration.Name.Name != "QueueApplication" {
			return true
		}
		checked = true
		structure, ok := declaration.Type.(*ast.StructType)
		if !ok {
			t.Fatal("QueueApplication 不是 struct")
		}
		if len(structure.Fields.List) != 1 {
			t.Fatalf("QueueApplication 有 %d 个字段，want 仅 deps", len(structure.Fields.List))
		}
		for _, field := range structure.Fields.List {
			ast.Inspect(field.Type, func(nested ast.Node) bool {
				identifier, ok := nested.(*ast.Ident)
				if ok && (identifier.Name == "Service" || identifier.Name == "InputQueue" || identifier.Name == "QueuedRequest") {
					t.Fatalf("QueueApplication 持有重复状态或完整门面类型 %s", identifier.Name)
				}
				return true
			})
		}
		return true
	})
	if !checked {
		t.Fatal("未找到 QueueApplication struct")
	}
}
