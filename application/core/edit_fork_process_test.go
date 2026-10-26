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
	"strings"
	"sync"
	"testing"

	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	"github.com/RedHuang-0622/seelex/store"
)

func TestEditForkProcessRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("editForkProcess")
	if !ok {
		t.Fatal("serviceComponents 未装配 editForkProcess")
	}
	if field.Type != reflect.TypeOf((*editForkProcess)(nil)) {
		t.Fatalf("editForkProcess 字段类型 = %s", field.Type)
	}
	processType := reflect.TypeOf((*editForkProcess)(nil))
	if _, ok := processType.MethodByName("Run"); !ok {
		t.Fatal("editForkProcess 缺少 Run")
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
		if !ok || function.Recv == nil || function.Name.Name != "runEditUserMessageFork" {
			continue
		}
		found = true
		if len(function.Body.List) != 1 {
			t.Fatalf("runEditUserMessageFork 有 %d 条语句，want 一行 component 转发", len(function.Body.List))
		}
		if _, ok := function.Body.List[0].(*ast.ReturnStmt); !ok {
			t.Fatal("runEditUserMessageFork 不是单行 return 转发")
		}
	}
	if !found {
		t.Fatal("未找到 runEditUserMessageFork")
	}
}

func TestEditForkProcessStopsBeforeValidateWhenResolveFails(t *testing.T) {
	resolveErr := errors.New("missing target")
	process := newEditForkProcess(editForkProcessDeps{
		Resolve: func(string, uint64) (store.Event, uint64, uint64, error) {
			return store.Event{}, 0, 0, resolveErr
		},
		Validate: func(string, uint64, uint64, uint64) error {
			t.Fatal("解析失败后不得继续校验")
			return nil
		},
	})

	_, err := process.Run(context.Background(), "parent", model.EditUserMessageForkRequest{})

	if !errors.Is(err, resolveErr) {
		t.Fatalf("Run error = %v, want resolve error", err)
	}
}

func TestEditForkProcessStopsBeforeForkWhenValidateFails(t *testing.T) {
	validateErr := errors.New("stale")
	process := newEditForkProcess(editForkProcessDeps{
		Resolve: func(string, uint64) (store.Event, uint64, uint64, error) {
			return store.Event{Seq: 3}, 2, 7, nil
		},
		Validate: func(parentID string, parentRevision, expectedRevision, targetSeq uint64) error {
			if parentID != "parent" || parentRevision != 7 || expectedRevision != 6 || targetSeq != 3 {
				t.Fatalf("validate args = %s/%d/%d/%d", parentID, parentRevision, expectedRevision, targetSeq)
			}
			return validateErr
		},
		Fork: func(string, model.ForkRequest) (string, error) {
			t.Fatal("校验失败后不得 fork")
			return "", nil
		},
	})

	_, err := process.Run(context.Background(), "parent", model.EditUserMessageForkRequest{
		TargetEventSeq: 3, ExpectedRevision: 6,
	})

	if !errors.Is(err, validateErr) {
		t.Fatalf("Run error = %v, want validate error", err)
	}
}

func TestEditForkProcessForksUnderTransitionAndStartsChild(t *testing.T) {
	locker := &recordingLocker{}
	var calls []string
	process := newEditForkProcess(editForkProcessDeps{
		Resolve: func(string, uint64) (store.Event, uint64, uint64, error) {
			return store.Event{Seq: 3, MessageID: "m3"}, 2, 7, nil
		},
		Validate: func(string, uint64, uint64, uint64) error { return nil },
		TransitionForKey: func(parentID string) sync.Locker {
			if parentID != "parent" {
				t.Fatalf("transition parent = %q", parentID)
			}
			return locker
		},
		Fork: func(parentID string, request model.ForkRequest) (string, error) {
			calls = append(calls, "fork:"+parentID)
			if request.EventSeq != 2 || request.SourceEventSeq != 3 ||
				request.SourceMessageID != "m3" || request.ExpectedEventSeq != 7 {
				t.Fatalf("fork request = %+v", request)
			}
			if locker.locked != 1 {
				t.Fatalf("fork called with %d transition locks, want 1", locker.locked)
			}
			return "child", nil
		},
		Resume: func(childID string) error {
			calls = append(calls, "resume:"+childID)
			if locker.locked != 0 {
				t.Fatalf("resume called with %d transition locks, want 0", locker.locked)
			}
			return nil
		},
		Submit: func(ctx context.Context, childID, text string) error {
			calls = append(calls, "submit:"+childID+":"+text)
			if ctx == nil {
				t.Fatal("context must not be nil")
			}
			return nil
		},
	})

	result, err := process.Run(context.Background(), "parent", model.EditUserMessageForkRequest{
		TargetEventSeq: 3, ExpectedRevision: 7, NewText: "edited",
	})

	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result != (model.EditUserMessageForkResult{ChildSessionID: "child", ForkPoint: 2}) {
		t.Fatalf("result = %+v", result)
	}
	if got := strings.Join(calls, ","); got != "fork:parent,resume:child,submit:child:edited" {
		t.Fatalf("calls = %q", got)
	}
}

func TestEditForkProcessRollsBackWhenChildResumeFails(t *testing.T) {
	locker := &recordingLocker{}
	resumeErr := errors.New("resume failed")
	var rollbackCause error
	process := newEditForkProcess(editForkProcessDeps{
		Resolve: func(string, uint64) (store.Event, uint64, uint64, error) {
			return store.Event{Seq: 3}, 2, 7, nil
		},
		Validate:         func(string, uint64, uint64, uint64) error { return nil },
		TransitionForKey: func(string) sync.Locker { return locker },
		Fork:             func(string, model.ForkRequest) (string, error) { return "child", nil },
		Resume:           func(string) error { return resumeErr },
		Submit:           func(context.Context, string, string) error { t.Fatal("resume 失败后不得提交"); return nil },
		Rollback: func(parentID, childID string, cause error) error {
			rollbackCause = cause
			if parentID != "parent" || childID != "child" {
				t.Fatalf("rollback args = %s/%s", parentID, childID)
			}
			return cause
		},
	})

	_, err := process.Run(context.Background(), "parent", model.EditUserMessageForkRequest{TargetEventSeq: 3})

	if !errors.Is(err, resumeErr) || !strings.Contains(rollbackCause.Error(), "切换编辑分支失败") {
		t.Fatalf("Run error = %v, rollback cause = %v", err, rollbackCause)
	}
}

func TestEditForkProcessRollsBackWhenSubmitFails(t *testing.T) {
	locker := &recordingLocker{}
	submitErr := errors.New("submit failed")
	var rollbackCalled bool
	process := newEditForkProcess(editForkProcessDeps{
		Resolve: func(string, uint64) (store.Event, uint64, uint64, error) {
			return store.Event{Seq: 3}, 2, 7, nil
		},
		Validate:         func(string, uint64, uint64, uint64) error { return nil },
		TransitionForKey: func(string) sync.Locker { return locker },
		Fork:             func(string, model.ForkRequest) (string, error) { return "child", nil },
		Resume:           func(string) error { return nil },
		Submit:           func(context.Context, string, string) error { return submitErr },
		Rollback: func(_, _ string, cause error) error {
			rollbackCalled = strings.Contains(cause.Error(), "提交编辑后的输入失败") && errors.Is(cause, submitErr)
			return cause
		},
	})

	_, err := process.Run(context.Background(), "parent", model.EditUserMessageForkRequest{TargetEventSeq: 3})

	if !errors.Is(err, submitErr) || !rollbackCalled {
		t.Fatalf("Run error = %v, rollback = %v", err, rollbackCalled)
	}
}
