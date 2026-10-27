package assembly

import (
	"errors"
	applicationsessioncold "github.com/RedHuang-0622/seelex/application/session/cold"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/kernel/session"
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

func TestSessionColdRestoreRulesAreExtracted(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("sessionColdRestore")
	if !ok {
		t.Fatal("serviceComponents 未装配 sessionColdRestore")
	}
	if field.Type != reflect.TypeOf((*applicationsessioncold.RestoreApplication)(nil)) {
		t.Fatalf("sessionColdRestore 字段类型 = %s", field.Type)
	}
	restoreType := reflect.TypeOf((*applicationsessioncold.RestoreApplication)(nil))
	if _, ok := restoreType.MethodByName("Restore"); !ok {
		t.Fatal("sessionColdRestore 缺少 Restore")
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试源码")
	}
	path := filepath.Join(filepath.Dir(file), "session_history.go")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return
	}
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name.Name != "resumeSessionCold" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if ok && (selector.Sel.Name == "SetSessionWorkspace" ||
				selector.Sel.Name == "ResumeSession" ||
				selector.Sel.Name == "ReplaceHistory" ||
				selector.Sel.Name == "QueueRecoverInputs" ||
				selector.Sel.Name == "writeSessionSystemPrompt") {
				t.Errorf("resumeSessionCold 仍直接调用 %s", selector.Sel.Name)
			}
			return true
		})
		return
	}
}

func TestSessionColdRestoreResumesEngineAndQueue(t *testing.T) {
	location := session_runtime.Location{WorkspaceID: "workspace-a"}
	history := []contract.EngineMessage{{Role: "user", Content: "resume"}}
	var calls []string
	component := applicationsessioncold.NewRestore(applicationsessioncold.RestoreDeps{
		SetWorkspace: func(sessionID, workspaceID string) {
			calls = append(calls, "workspace:"+workspaceID)
		},
		ResumeEngine: func(sessionID string, got []contract.EngineMessage) (bool, error) {
			calls = append(calls, "resume")
			if len(got) != 1 {
				t.Fatalf("history len = %d", len(got))
			}
			return true, nil
		},
		ReplaceHistory: func(string, []contract.EngineMessage) error {
			t.Fatal("ResumeEngine 成功时不得 replace")
			return nil
		},
		RecoverQueue: func(got session_runtime.Location, sessionID string) error {
			calls = append(calls, "queue")
			if got != location {
				t.Fatalf("queue location = %+v", got)
			}
			return nil
		},
		WritePrompt: func(sessionID string) {
			calls = append(calls, "prompt")
		},
	})

	if err := component.Restore("session-a", location, history); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := strings.Join(calls, ","); got != "workspace:workspace-a,resume,queue,prompt" {
		t.Fatalf("calls = %q", got)
	}
}

func TestSessionColdRestoreFallsBackToReplaceHistory(t *testing.T) {
	var replaced bool
	component := applicationsessioncold.NewRestore(applicationsessioncold.RestoreDeps{
		SetWorkspace: func(string, string) {},
		ResumeEngine: func(string, []contract.EngineMessage) (bool, error) {
			return false, nil
		},
		ReplaceHistory: func(string, []contract.EngineMessage) error {
			replaced = true
			return nil
		},
		RecoverQueue: func(session_runtime.Location, string) error { return nil },
		WritePrompt:  func(string) {},
	})

	if err := component.Restore("session-a", session_runtime.Location{}, nil); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if !replaced {
		t.Fatal("engine 无 resume 能力时必须 replace history")
	}
}

func TestSessionColdRestoreStopsOnEngineAndQueueErrors(t *testing.T) {
	resumeErr := errors.New("resume failed")
	replaceErr := errors.New("replace failed")
	queueErr := errors.New("queue failed")
	tests := []struct {
		name       string
		resumeOK   bool
		resumeErr  error
		replaceErr error
		queueErr   error
		want       error
		text       string
	}{
		{name: "resume", resumeOK: true, resumeErr: resumeErr, want: resumeErr, text: "resume engine session"},
		{name: "replace", replaceErr: replaceErr, want: replaceErr, text: "replace engine history"},
		{name: "queue", resumeOK: true, queueErr: queueErr, want: queueErr, text: "lifecycle recover"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			promptCalled := false
			component := applicationsessioncold.NewRestore(applicationsessioncold.RestoreDeps{
				SetWorkspace: func(string, string) {},
				ResumeEngine: func(string, []contract.EngineMessage) (bool, error) {
					return test.resumeOK, test.resumeErr
				},
				ReplaceHistory: func(string, []contract.EngineMessage) error {
					return test.replaceErr
				},
				RecoverQueue: func(session_runtime.Location, string) error { return test.queueErr },
				WritePrompt:  func(string) { promptCalled = true },
			})

			err := component.Restore("session-a", session_runtime.Location{}, nil)

			if !errors.Is(err, test.want) || !strings.Contains(err.Error(), test.text) {
				t.Fatalf("Restore error = %v, want %v containing %q", err, test.want, test.text)
			}
			if promptCalled {
				t.Fatal("错误路径不得继续写 system prompt")
			}
		})
	}
}
