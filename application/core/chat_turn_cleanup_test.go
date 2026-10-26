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

	view_state "github.com/RedHuang-0622/seelex/middle/view/state"
)

func TestChatTurnCleanupRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("conversationTurnCleanup")
	if !ok {
		t.Fatal("serviceComponents 未装配 conversationTurnCleanup")
	}
	if field.Type != reflect.TypeOf((*conversationTurnCleanup)(nil)) {
		t.Fatalf("conversationTurnCleanup 字段类型 = %s", field.Type)
	}
	cleanupType := reflect.TypeOf((*conversationTurnCleanup)(nil))
	if _, ok := cleanupType.MethodByName("Cleanup"); !ok {
		t.Fatal("conversationTurnCleanup 缺少 Cleanup")
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
			if ok && (selector.Sel.Name == "stampServedRoute" ||
				selector.Sel.Name == "discardPendingSubagentContextsFor" ||
				selector.Sel.Name == "collectRuntimeProjectionFor" ||
				selector.Sel.Name == "RemoveTaskContextCheckpointsFor" ||
				selector.Sel.Name == "removeProviderContextRecoveryFor") {
				t.Errorf("runChat 仍直接调用 %s", selector.Sel.Name)
			}
			return true
		})
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "Flush" {
				t.Error("runChat 仍直接调用 batcher.Flush")
			}
			return true
		})
		return
	}
}

func TestConversationTurnCleanupRunsFullSuccessfulPath(t *testing.T) {
	var calls []string
	projection := view_state.RuntimeStateProjection{}
	cleanup := newConversationTurnCleanup(conversationTurnCleanupDeps{
		StampRoute:     func(string, string) { calls = append(calls, "route") },
		DiscardMailbox: func(string) { calls = append(calls, "mailbox") },
		CollectProjection: func(string) view_state.RuntimeStateProjection {
			calls = append(calls, "projection")
			return projection
		},
		RemoveCheckpoints: func(string) error { calls = append(calls, "checkpoints"); return nil },
		RemoveRecovery:    func(string) error { calls = append(calls, "recovery"); return nil },
		RecordError:       func(string, string, error) { t.Fatal("成功路径不得记录未处理错误") },
	})

	got, err := cleanup.Cleanup("session-a", "turn-a", nil,
		func() error { calls = append(calls, "flush"); return nil },
		func() { calls = append(calls, "stop-draft") },
	)

	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if !reflect.DeepEqual(got, projection) {
		t.Fatalf("projection = %+v", got)
	}
	want := "flush,route,mailbox,projection,checkpoints,recovery,stop-draft"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls = %v, want %s", calls, want)
	}
}

func TestConversationTurnCleanupCombinesErrorsAndKeepsRunning(t *testing.T) {
	turnErr := errors.New("turn error")
	checkpointErr := errors.New("checkpoint failed")
	var recorded error
	var calls []string
	cleanup := newConversationTurnCleanup(conversationTurnCleanupDeps{
		StampRoute:     func(string, string) { calls = append(calls, "route") },
		DiscardMailbox: func(string) { calls = append(calls, "mailbox") },
		CollectProjection: func(string) view_state.RuntimeStateProjection {
			calls = append(calls, "projection")
			return view_state.RuntimeStateProjection{}
		},
		RemoveCheckpoints: func(string) error { calls = append(calls, "checkpoints"); return checkpointErr },
		RemoveRecovery: func(string) error {
			t.Fatal("turnErr 非空时不得继续 recovery cleanup")
			return nil
		},
		RecordError: func(string, string, error) {
			calls = append(calls, "record")
			recorded = turnErr
		},
	})

	_, err := cleanup.Cleanup("session-a", "turn-a", turnErr,
		func() error { return errors.New("flush failed") },
		func() { calls = append(calls, "stop-draft") },
	)

	if !errors.Is(err, turnErr) || recorded != turnErr {
		t.Fatalf("Cleanup error = %v recorded=%v", err, recorded)
	}
	want := "route,mailbox,projection,checkpoints,record,stop-draft"
	if strings.Join(calls, ",") != want {
		t.Fatalf("calls = %v, want %s", calls, want)
	}
}

func TestConversationTurnCleanupWrapsFlushAndRecoveryErrors(t *testing.T) {
	flushErr := errors.New("flush")
	recoveryErr := errors.New("recovery")
	tests := []struct {
		name     string
		flush    error
		recovery error
		want     error
		text     string
	}{
		{name: "flush", flush: flushErr, want: flushErr, text: "flush streamed response"},
		{name: "recovery", recovery: recoveryErr, want: recoveryErr, text: "recovery"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cleanup := newConversationTurnCleanup(conversationTurnCleanupDeps{
				StampRoute:        func(string, string) {},
				DiscardMailbox:    func(string) {},
				CollectProjection: func(string) view_state.RuntimeStateProjection { return view_state.RuntimeStateProjection{} },
				RemoveCheckpoints: func(string) error { return nil },
				RemoveRecovery:    func(string) error { return test.recovery },
				RecordError:       func(string, string, error) {},
			})
			_, err := cleanup.Cleanup("session-a", "turn-a", nil,
				func() error { return test.flush },
				func() {},
			)
			if !errors.Is(err, test.want) || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("Cleanup error = %v", err)
			}
		})
	}
}
