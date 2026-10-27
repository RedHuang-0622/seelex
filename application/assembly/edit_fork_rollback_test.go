package assembly

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
)

func TestEditForkRollbackRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("editForkRollback")
	if !ok {
		t.Fatal("serviceComponents 未装配 editForkRollback")
	}
	if field.Type != reflect.TypeOf((*editForkRollback)(nil)) {
		t.Fatalf("editForkRollback 字段类型 = %s", field.Type)
	}
	componentType := reflect.TypeOf((*editForkRollback)(nil))
	if _, ok := componentType.MethodByName("Rollback"); !ok {
		t.Fatal("editForkRollback 缺少 Rollback")
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
		if !ok || function.Recv == nil || function.Name.Name != "rollbackEditFork" {
			continue
		}
		found = true
		if len(function.Body.List) != 1 {
			t.Fatalf("rollbackEditFork 有 %d 条语句，want 一行 component 转发", len(function.Body.List))
		}
		if _, ok := function.Body.List[0].(*ast.ReturnStmt); !ok {
			t.Fatal("rollbackEditFork 不是单行 return 转发")
		}
	}
	if found {
		t.Fatal("core rollbackEditFork forwarder must be removed")
	}
}

func TestEditForkRollbackReturnsCauseWhenBothActionsSucceed(t *testing.T) {
	cause := errors.New("submit failed")
	component := newEditForkRollback(editForkRollbackDeps{
		Delete: func(childID string) error {
			if childID != "child" {
				t.Fatalf("delete child = %q", childID)
			}
			return nil
		},
		Resume: func(parentID string) error {
			if parentID != "parent" {
				t.Fatalf("resume parent = %q", parentID)
			}
			return nil
		},
	})

	err := component.Rollback("parent", "child", cause)

	if err != cause {
		t.Fatalf("Rollback error = %v, want original cause", err)
	}
}

func TestEditForkRollbackCombinesChildAndParentFailures(t *testing.T) {
	tests := []struct {
		name      string
		deleteErr error
		resumeErr error
		contains  []string
	}{
		{name: "delete", deleteErr: errors.New("delete failed"), contains: []string{"回滚 child 失败"}},
		{name: "resume", resumeErr: errors.New("resume failed"), contains: []string{"恢复父会话失败"}},
		{name: "both", deleteErr: errors.New("delete failed"), resumeErr: errors.New("resume failed"), contains: []string{"回滚 child 失败", "恢复父会话失败"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cause := errors.New("submit failed")
			component := newEditForkRollback(editForkRollbackDeps{
				Delete: func(string) error { return test.deleteErr },
				Resume: func(string) error { return test.resumeErr },
			})

			err := component.Rollback("parent", "child", cause)

			if !errors.Is(err, cause) {
				t.Fatalf("Rollback error = %v, want cause identity", err)
			}
			for _, text := range test.contains {
				if !strings.Contains(err.Error(), text) {
					t.Fatalf("Rollback error = %v, want %q", err, text)
				}
			}
		})
	}
}
