package core

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
)

func TestEditForkOperationRegistryIsAssembledAndOwnsState(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("editForkOperationRegistry")
	if !ok {
		t.Fatal("serviceComponents 未装配 editForkOperationRegistry")
	}
	if field.Type != reflect.TypeOf((*editForkOperationRegistry)(nil)) {
		t.Fatalf("editForkOperationRegistry 字段类型 = %s", field.Type)
	}
	state := reflect.TypeOf(serviceState{})
	if _, ok := state.FieldByName("editForkOps"); ok {
		t.Fatal("serviceState 仍持有 editForkOps；operation map 应归注册组件所有")
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
	for _, name := range []string{"beginEditForkOperation", "finishEditForkOperation"} {
		found := false
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv == nil || function.Name.Name != name {
				continue
			}
			found = true
			if len(function.Body.List) != 1 {
				t.Fatalf("%s 有 %d 条语句，want 一行 component 转发", name, len(function.Body.List))
			}
			if name == "beginEditForkOperation" {
				if _, ok := function.Body.List[0].(*ast.ReturnStmt); !ok {
					t.Fatalf("%s 不是单行 return 转发", name)
				}
			} else if _, ok := function.Body.List[0].(*ast.ExprStmt); !ok {
				t.Fatalf("%s 不是单行调用转发", name)
			}
		}
		if !found {
			t.Fatalf("未找到 %s", name)
		}
	}
}

func TestEditForkOperationRegistryDeduplicatesMatchingRequest(t *testing.T) {
	registry := newEditForkOperationRegistry(5*time.Minute, time.Now)
	request := model.EditUserMessageForkRequest{OperationID: "op-a", NewText: "new"}

	first, owner, err := registry.Begin("parent-a", request)
	if err != nil || !owner {
		t.Fatalf("first Begin = owner %v, err %v", owner, err)
	}
	second, owner, err := registry.Begin("parent-a", request)
	if err != nil || owner {
		t.Fatalf("duplicate Begin = owner %v, err %v", owner, err)
	}
	if first != second {
		t.Fatal("相同 operation_id 和请求必须复用同一 operation")
	}
}

func TestEditForkOperationRegistryRejectsReusedIDWithDifferentRequest(t *testing.T) {
	registry := newEditForkOperationRegistry(5*time.Minute, time.Now)
	if _, _, err := registry.Begin("parent-a", model.EditUserMessageForkRequest{OperationID: "op-a", NewText: "one"}); err != nil {
		t.Fatal(err)
	}

	_, _, err := registry.Begin("parent-a", model.EditUserMessageForkRequest{OperationID: "op-a", NewText: "two"})

	if err == nil || err.Error() != "同一 operation_id 不能复用于不同的编辑请求" {
		t.Fatalf("Begin error = %v", err)
	}
}

func TestEditForkOperationRegistryExpiresSuccessfulOperations(t *testing.T) {
	now := time.Unix(100, 0)
	registry := newEditForkOperationRegistry(5*time.Minute, func() time.Time { return now })
	request := model.EditUserMessageForkRequest{OperationID: "op-a", NewText: "new"}
	operation, owner, err := registry.Begin("parent-a", request)
	if err != nil || !owner {
		t.Fatalf("Begin = owner %v, err %v", owner, err)
	}
	registry.Finish(operation, model.EditUserMessageForkResult{ChildSessionID: "child-a"}, nil)

	now = now.Add(5*time.Minute + time.Second)
	replacement, owner, err := registry.Begin("parent-a", request)

	if err != nil || !owner {
		t.Fatalf("expired Begin = owner %v, err %v", owner, err)
	}
	if replacement == operation {
		t.Fatal("TTL 过期后必须创建新 operation")
	}
}

func TestEditForkOperationRegistryDropsFailedOperation(t *testing.T) {
	registry := newEditForkOperationRegistry(5*time.Minute, time.Now)
	request := model.EditUserMessageForkRequest{OperationID: "op-a", NewText: "new"}
	operation, owner, err := registry.Begin("parent-a", request)
	if err != nil || !owner {
		t.Fatalf("Begin = owner %v, err %v", owner, err)
	}
	runErr := errors.New("fork failed")
	registry.Finish(operation, model.EditUserMessageForkResult{}, runErr)

	select {
	case <-operation.Done():
	default:
		t.Fatal("Finish 必须关闭 operation.done")
	}
	_, operationErr := operation.Outcome()
	if !errors.Is(operationErr, runErr) {
		t.Fatalf("operation err = %v, want %v", operationErr, runErr)
	}
	replacement, owner, err := registry.Begin("parent-a", request)
	if err != nil || !owner {
		t.Fatalf("failed request rerun = owner %v, err %v", owner, err)
	}
	if replacement == operation {
		t.Fatal("失败 operation 必须从表里删除")
	}
}

func TestEditForkOperationRegistryWaiterSeesCompletedResult(t *testing.T) {
	registry := newEditForkOperationRegistry(5*time.Minute, time.Now)
	request := model.EditUserMessageForkRequest{OperationID: "op-a", NewText: "new"}
	operation, owner, err := registry.Begin("parent-a", request)
	if err != nil || !owner {
		t.Fatalf("Begin = owner %v, err %v", owner, err)
	}
	want := model.EditUserMessageForkResult{ChildSessionID: "child-a"}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-operation.Done()
	}()
	registry.Finish(operation, want, nil)
	wg.Wait()

	got, _ := operation.Outcome()
	if got != want {
		t.Fatalf("result = %+v, want %+v", got, want)
	}
	if operation.CompletedAt().IsZero() {
		t.Fatal("成功 operation 必须记录 completedAt")
	}
}
