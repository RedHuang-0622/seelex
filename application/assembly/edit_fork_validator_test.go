package assembly

import (
	"errors"
	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestEditForkValidatorRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("editForkValidator")
	if !ok {
		t.Fatal("serviceComponents 未装配 editForkValidator")
	}
	if field.Type != reflect.TypeOf((*applicationedit.Validator)(nil)) {
		t.Fatalf("editForkValidator 字段类型 = %s", field.Type)
	}
	validatorType := reflect.TypeOf((*applicationedit.Validator)(nil))
	if _, ok := validatorType.MethodByName("Validate"); !ok {
		t.Fatal("editForkValidator 缺少 Validate")
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
		if !ok || function.Recv == nil || function.Name.Name != "validateEditForkParent" {
			continue
		}
		found = true
		if len(function.Body.List) != 1 {
			t.Fatalf("validateEditForkParent 有 %d 条语句，want 一行 component 转发", len(function.Body.List))
		}
		if _, ok := function.Body.List[0].(*ast.ReturnStmt); !ok {
			t.Fatal("validateEditForkParent 不是单行 return 转发")
		}
	}
	if found {
		t.Fatal("core validateEditForkParent forwarder must be removed")
	}
}

func TestEditForkValidatorRejectsNonCurrentParent(t *testing.T) {
	validator := applicationedit.NewValidator(applicationedit.ValidatorDeps{
		Snapshot: func(string) applicationedit.ParentState {
			return applicationedit.ParentState{CurrentSessionID: "session-b", HasWindow: true}
		},
	})

	err := validator.Validate("session-a", 7, 7, 3)

	if err == nil || err.Error() != "只能编辑当前会话中的用户消息" {
		t.Fatalf("Validate error = %v", err)
	}
}

func TestEditForkValidatorRejectsBusyAndQueuedParent(t *testing.T) {
	tests := []struct {
		name  string
		state applicationedit.ParentState
		want  error
		text  string
	}{
		{name: "running", state: applicationedit.ParentState{CurrentSessionID: "session-a", Running: true, HasWindow: true}, want: ErrChatRunning},
		{name: "queued", state: applicationedit.ParentState{CurrentSessionID: "session-a", Queued: true, HasWindow: true}, text: "父会话还有排队输入"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validator := applicationedit.NewValidator(applicationedit.ValidatorDeps{
				Snapshot:         func(string) applicationedit.ParentState { return test.state },
				ChatRunningError: ErrChatRunning,
			})
			err := validator.Validate("session-a", 7, 7, 3)
			if test.want != nil {
				if !errors.Is(err, test.want) {
					t.Fatalf("Validate error = %v, want %v", err, test.want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("Validate error = %v, want %q", err, test.text)
			}
		})
	}
}

func TestEditForkValidatorRejectsRevisionWindowAndCompression(t *testing.T) {
	tests := []struct {
		name             string
		state            applicationedit.ParentState
		parentRevision   uint64
		expectedRevision uint64
		targetSeq        uint64
		text             string
	}{
		{
			name: "revision", state: applicationedit.ParentState{CurrentSessionID: "session-a", HasWindow: true},
			parentRevision: 8, expectedRevision: 7, targetSeq: 3, text: "父会话已推进",
		},
		{
			name: "no-window", state: applicationedit.ParentState{CurrentSessionID: "session-a"},
			parentRevision: 7, expectedRevision: 7, targetSeq: 3, text: editForkNoWindow,
		},
		{
			name: "compressed", state: applicationedit.ParentState{CurrentSessionID: "session-a", HasWindow: true, Floor: 5},
			parentRevision: 7, expectedRevision: 7, targetSeq: 3, text: editForkCompressed,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validator := applicationedit.NewValidator(applicationedit.ValidatorDeps{
				Snapshot: func(string) applicationedit.ParentState { return test.state },
			})
			err := validator.Validate("session-a", test.parentRevision, test.expectedRevision, test.targetSeq)
			if err == nil || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("Validate error = %v, want %q", err, test.text)
			}
		})
	}
}

func TestEditForkValidatorAcceptsEditableCurrentWindow(t *testing.T) {
	validator := applicationedit.NewValidator(applicationedit.ValidatorDeps{
		Snapshot: func(sessionID string) applicationedit.ParentState {
			if sessionID != "session-a" {
				t.Fatalf("snapshot session = %q", sessionID)
			}
			return applicationedit.ParentState{CurrentSessionID: "session-a", HasWindow: true, Floor: 3}
		},
	})

	if err := validator.Validate("session-a", 7, 7, 3); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := validator.Validate("session-a", 7, 0, 4); err != nil {
		t.Fatalf("expectedRevision=0 should accept current revision: %v", err)
	}
}
