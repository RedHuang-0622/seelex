package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestServiceComponentsAssembleConversationTurnState(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("conversationTurnState")
	if !ok {
		t.Fatal("serviceComponents 未装配 conversationTurnState")
	}
	if field.Type != reflect.TypeOf((*conversationTurnState)(nil)) {
		t.Fatalf("conversationTurnState 字段类型 = %s", field.Type)
	}
}

func TestChatStateWritesAreCentralized(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	dir := filepath.Dir(file)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "chat_turn_state.go" {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("解析 %s: %v", name, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && (selector.Sel.Name == "SetChatState" || selector.Sel.Name == "UpdateChat") {
				t.Errorf("%s 直接写 ChatState；唯一写入点应为 chat_turn_state.go", name)
			}
			return true
		})
	}
}

func TestChatTurnRequestConstructionIsCentralized(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	dir := filepath.Dir(file)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	banned := map[string]bool{
		"newChatRequest":      true,
		"combineChatRequests": true,
		"queuedChatRequests":  true,
		"chatRequestDisplays": true,
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "chat_turn_request.go" {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("解析 %s: %v", name, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if ok && banned[identifier.Name] {
				t.Errorf("%s 直接调用 %s；ChatTurn 请求构造应集中在 chat_turn_request.go", name, identifier.Name)
			}
			return true
		})
	}
}
