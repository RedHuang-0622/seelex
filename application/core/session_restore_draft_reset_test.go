package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
)

func TestSessionRestoreDraftResetRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("sessionRestoreDraftReset")
	if !ok {
		t.Fatal("serviceComponents 未装配 sessionRestoreDraftReset")
	}
	if field.Type != reflect.TypeOf((*applicationsession.RestoreDraftResetApplication)(nil)) {
		t.Fatalf("sessionRestoreDraftReset 字段类型 = %s", field.Type)
	}
	resetType := reflect.TypeOf((*applicationsession.RestoreDraftResetApplication)(nil))
	if _, ok := resetType.MethodByName("Reset"); !ok {
		t.Fatal("sessionRestoreDraftReset 缺少 Reset")
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "session_history.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name.Name != "resetViewToDraftAfterRestoreFailure" {
			continue
		}
		found = true
		if len(function.Body.List) != 1 {
			t.Fatalf("resetViewToDraftAfterRestoreFailure 有 %d 条语句，want 一行 component 调用", len(function.Body.List))
		}
		if _, ok := function.Body.List[0].(*ast.ExprStmt); !ok {
			t.Fatal("resetViewToDraftAfterRestoreFailure 不是单行调用转发")
		}
	}
	if found {
		t.Fatal("core resetViewToDraftAfterRestoreFailure forwarder must be removed")
	}
}

func TestSessionRestoreDraftResetPublishesDraftShell(t *testing.T) {
	locker := &recordingLocker{}
	var calls []string
	reset := applicationsession.NewRestoreDraftReset(applicationsession.RestoreDraftResetDeps{
		Lock: func() sync.Locker { return locker },
		EnsureSlotLocked: func() string {
			if locker.locked != 1 {
				t.Fatalf("EnsureSlotLocked called with %d locks", locker.locked)
			}
			calls = append(calls, "slot")
			return "draft-a"
		},
		EnterShellLocked: func(draftID string) uint64 {
			if locker.locked != 1 || draftID != "draft-a" {
				t.Fatalf("EnterShellLocked = %q with %d locks", draftID, locker.locked)
			}
			calls = append(calls, "shell")
			return 31
		},
		PublishRuntimeProjections: func() {
			if locker.locked != 0 {
				t.Fatalf("runtime publish called with %d locks", locker.locked)
			}
			calls = append(calls, "runtime")
		},
		PublishSnapshot: func(revision uint64, draftID string) {
			if locker.locked != 0 || revision != 31 || draftID != "draft-a" {
				t.Fatalf("PublishSnapshot = %d/%q with %d locks", revision, draftID, locker.locked)
			}
			calls = append(calls, "snapshot")
		},
	})

	reset.Reset()

	want := []string{"slot", "shell", "runtime", "snapshot"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}
