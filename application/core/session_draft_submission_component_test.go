package core

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
)

func TestSessionDraftSubmissionRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("sessionDraftSubmission")
	if !ok {
		t.Fatal("serviceComponents 未装配 sessionDraftSubmission")
	}
	if field.Type != reflect.TypeOf((*applicationsession.DraftSubmissionApplication)(nil)) {
		t.Fatalf("sessionDraftSubmission 字段类型 = %s", field.Type)
	}
	componentType := reflect.TypeOf((*applicationsession.DraftSubmissionApplication)(nil))
	if _, ok := componentType.MethodByName("Materialize"); !ok {
		t.Fatal("sessionDraftSubmission 缺少 Materialize")
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "session_draft.go")
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name.Name != "materializeDraftForSubmit" {
			continue
		}
		found = true
		if len(function.Body.List) != 1 {
			t.Fatalf("materializeDraftForSubmit 有 %d 条语句，want 一行 component 转发", len(function.Body.List))
		}
		if _, ok := function.Body.List[0].(*ast.ReturnStmt); !ok {
			t.Fatal("materializeDraftForSubmit 不是单行 return 转发")
		}
	}
	if !found {
		t.Fatal("未找到 materializeDraftForSubmit")
	}
}

type recordingLocker struct {
	mu     sync.Mutex
	calls  []string
	locked int
}

func (locker *recordingLocker) Lock() {
	locker.mu.Lock()
	locker.calls = append(locker.calls, "lock")
	locker.locked++
}

func (locker *recordingLocker) Unlock() {
	locker.calls = append(locker.calls, "unlock")
	locker.locked--
	locker.mu.Unlock()
}

func TestSessionDraftSubmissionSkipsLoadedTarget(t *testing.T) {
	locker := &recordingLocker{}
	component := applicationsession.NewDraftSubmission(applicationsession.DraftSubmissionDeps{
		IsDraftTarget: func(string) bool {
			return false
		},
		Transition: func() sync.Locker {
			t.Fatal("非草稿目标不应获取视图过渡锁")
			return locker
		},
		IsInView: func(string) bool {
			t.Fatal("非草稿目标不应做锁内归属复判")
			return false
		},
		Materialize: func(string) error {
			t.Fatal("非草稿目标不应物化")
			return nil
		},
	})

	if err := component.Materialize("session-a", " input "); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
}

func TestSessionDraftSubmissionRejectsStaleDraftView(t *testing.T) {
	locker := &recordingLocker{}
	component := applicationsession.NewDraftSubmission(applicationsession.DraftSubmissionDeps{
		IsDraftTarget: func(sessionID string) bool {
			return sessionID == "draft-a"
		},
		Transition: func() sync.Locker {
			return locker
		},
		IsInView: func(string) bool {
			return false
		},
		Materialize: func(string) error {
			t.Fatal("视图已离开草稿时不得物化")
			return nil
		},
		NotInView: ErrDraftNotInView,
	})

	err := component.Materialize("draft-a", "input")

	if err != ErrDraftNotInView {
		t.Fatalf("Materialize error = %v, want %v", err, ErrDraftNotInView)
	}
	if got := strings.Join(locker.calls, ","); got != "lock,unlock" {
		t.Fatalf("lock calls = %q, want lock,unlock", got)
	}
}

func TestSessionDraftSubmissionMaterializesCurrentDraft(t *testing.T) {
	locker := &recordingLocker{}
	var materialized string
	component := applicationsession.NewDraftSubmission(applicationsession.DraftSubmissionDeps{
		IsDraftTarget: func(sessionID string) bool {
			return sessionID == "draft-a"
		},
		Transition: func() sync.Locker {
			return locker
		},
		IsInView: func(sessionID string) bool {
			return sessionID == "draft-a"
		},
		Materialize: func(firstInput string) error {
			materialized = firstInput
			return nil
		},
	})

	if err := component.Materialize("draft-a", "  first input  "); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if materialized != "first input" {
		t.Fatalf("物化输入 = %q, want trimmed input", materialized)
	}
	if got := strings.Join(locker.calls, ","); got != "lock,unlock" {
		t.Fatalf("lock calls = %q, want lock,unlock", got)
	}
}
