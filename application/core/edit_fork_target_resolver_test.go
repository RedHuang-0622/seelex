package core

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/store"
)

func TestEditForkTargetResolverRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("editForkTargetResolver")
	if !ok {
		t.Fatal("serviceComponents 未装配 editForkTargetResolver")
	}
	if field.Type != reflect.TypeOf((*editForkTargetResolver)(nil)) {
		t.Fatalf("editForkTargetResolver 字段类型 = %s", field.Type)
	}
	resolverType := reflect.TypeOf((*editForkTargetResolver)(nil))
	if _, ok := resolverType.MethodByName("Resolve"); !ok {
		t.Fatal("editForkTargetResolver 缺少 Resolve")
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "session_message_edit.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name.Name != "resolveEditForkTarget" {
			continue
		}
		found = true
		if len(function.Body.List) != 1 {
			t.Fatalf("resolveEditForkTarget 有 %d 条语句，want 一行 component 转发", len(function.Body.List))
		}
		if _, ok := function.Body.List[0].(*ast.ReturnStmt); !ok {
			t.Fatal("resolveEditForkTarget 不是单行 return 转发")
		}
	}
	if found {
		t.Fatal("core resolveEditForkTarget forwarder must be removed")
	}
}

func TestEditForkTargetResolverRejectsMissingStore(t *testing.T) {
	resolver := newEditForkTargetResolver(editForkTargetResolverDeps{
		LoadEvents: func(string) ([]store.Event, bool, error) {
			return nil, false, nil
		},
	})

	_, _, _, err := resolver.Resolve("parent", 1)

	if err == nil || err.Error() != "会话 fork 存储不可用" {
		t.Fatalf("Resolve error = %v", err)
	}
}

func TestEditForkTargetResolverComputesRevisionAndParagraphCut(t *testing.T) {
	events := []store.Event{
		{Seq: 1, TaskID: "chat-1", Role: "user", Content: "first", MessageID: "m1"},
		{Seq: 2, TaskID: "chat-1", Role: "assistant", Content: "answer", MessageID: "m2"},
		{Seq: 3, TaskID: "chat-2", Role: "user", Content: "edit me", MessageID: "m3"},
		{Seq: 4, TaskID: "chat-2", Role: "assistant", Content: "old branch", MessageID: "m4"},
		{Seq: 5, TaskID: "chat-3", Role: "user", Content: "latest", MessageID: "m5"},
	}
	resolver := newEditForkTargetResolver(editForkTargetResolverDeps{
		LoadEvents: func(parentID string) ([]store.Event, bool, error) {
			if parentID != "parent" {
				t.Fatalf("parentID = %q", parentID)
			}
			return events, true, nil
		},
	})

	target, cut, revision, err := resolver.Resolve("parent", 3)

	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if target.Seq != 3 || target.MessageID != "m3" {
		t.Fatalf("target = %+v, want seq 3", target)
	}
	if cut != 2 || revision != 5 {
		t.Fatalf("cut/revision = %d/%d, want 2/5", cut, revision)
	}
}

func TestEditForkTargetResolverRejectsIneligibleTargets(t *testing.T) {
	events := []store.Event{
		{Seq: 1, TaskID: "chat-1", Role: "user", Content: "first", MessageID: "m1"},
		{Seq: 2, TaskID: "chat-1", Role: "assistant", Content: "answer", MessageID: "m2"},
		{Seq: 3, TaskID: "chat-2", Role: "user", Kind: "internal", Content: "internal", MessageID: "m3"},
	}
	tests := []struct {
		name   string
		target uint64
		text   string
	}{
		{name: "missing", target: 99, text: "找不到 EventSeq=99"},
		{name: "assistant", target: 2, text: "目标事件不是可编辑的用户输入"},
		{name: "internal", target: 3, text: "目标事件不是可编辑的用户输入"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := newEditForkTargetResolver(editForkTargetResolverDeps{
				LoadEvents: func(string) ([]store.Event, bool, error) {
					return events, true, nil
				},
			})
			_, _, _, err := resolver.Resolve("parent", test.target)
			if err == nil || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("Resolve error = %v, want %q", err, test.text)
			}
		})
	}
}

func TestEditForkTargetResolverWrapsLoadFailure(t *testing.T) {
	loadErr := errors.New("read failed")
	resolver := newEditForkTargetResolver(editForkTargetResolverDeps{
		LoadEvents: func(string) ([]store.Event, bool, error) {
			return nil, true, loadErr
		},
	})

	_, _, _, err := resolver.Resolve("parent", 1)

	if !errors.Is(err, loadErr) {
		t.Fatalf("Resolve error = %v, want load error", err)
	}
}
