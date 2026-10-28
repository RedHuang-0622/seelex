package integration

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	assembly "github.com/RedHuang-0622/seelex/application/assembly"
)

func TestConversationQueuePreservesPresentationEventOrder(t *testing.T) {
	service, _ := startBlockingChat(t)
	enqueueQueuedInputs(t, service, "second", "third")
	subscription := service.Events.Subscribe(16)
	defer subscription.Close()

	if err := service.components.conversationQueue.ReorderQueuedInput("", 0, 1); err != nil {
		t.Fatalf("ReorderQueuedInput: %v", err)
	}
	seen := make([]string, 0, 2)
	deadline := time.After(time.Second)
	for len(seen) < 2 {
		select {
		case published := <-subscription.Events:
			switch published.Kind {
			case assembly.EventSnapshotChanged, assembly.EventChatChanged:
				seen = append(seen, string(published.Kind))
			}
		case <-deadline:
			t.Fatalf("queue edit events = %v, want snapshot.changed then chat.changed", seen)
		}
	}
	if seen[0] != string(assembly.EventSnapshotChanged) || seen[1] != string(assembly.EventChatChanged) {
		t.Fatalf("queue edit event order = %v, want [%s %s]", seen, assembly.EventSnapshotChanged, assembly.EventChatChanged)
	}
}

func TestConversationQueueDoesNotOwnServiceOrDuplicateQueueState(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "conversation", "queue.go")
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
