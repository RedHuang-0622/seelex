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
	"sync"
	"testing"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
)

func TestSessionResumeRulesAreCentralized(t *testing.T) {
	components := reflect.TypeOf(serviceComponents{})
	field, ok := components.FieldByName("sessionResume")
	if !ok {
		t.Fatal("serviceComponents 未装配 sessionResume")
	}
	if field.Type != reflect.TypeOf((*applicationsession.ResumeApplication)(nil)) {
		t.Fatalf("sessionResume 字段类型 = %s", field.Type)
	}
	componentType := reflect.TypeOf((*applicationsession.ResumeApplication)(nil))
	if _, ok := componentType.MethodByName("Resume"); !ok {
		t.Fatal("sessionResume 缺少 Resume")
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
		if !ok || function.Recv == nil || function.Name.Name != "resumeSession" {
			continue
		}
		found = true
		if len(function.Body.List) != 1 {
			t.Fatalf("resumeSession 有 %d 条语句，want 一行 component 转发", len(function.Body.List))
		}
		if _, ok := function.Body.List[0].(*ast.ReturnStmt); !ok {
			t.Fatal("resumeSession 不是单行 return 转发")
		}
	}
	if found {
		t.Fatal("core resumeSession forwarder must be removed")
	}
}

func TestSessionResumeRequiresSessionID(t *testing.T) {
	component := applicationsession.NewResume(applicationsession.ResumeDeps{})
	err := component.Resume("   ")
	if err == nil || !strings.Contains(err.Error(), "session ID is required") {
		t.Fatalf("Resume error = %v, want missing session ID", err)
	}
}

func TestSessionResumeRestoringShellKeepsExistingLoad(t *testing.T) {
	locker := &recordingLocker{}
	var hotCalled bool
	component := applicationsession.NewResume(applicationsession.ResumeDeps{
		TransitionForSession: func(sessionID string) sync.Locker {
			if sessionID != "session-b" {
				t.Fatalf("transition target = %q", sessionID)
			}
			return locker
		},
		Snapshot: func(string) applicationsession.ResumeState {
			return applicationsession.ResumeState{Restoring: true}
		},
		PointViewAtRestoringShell: func(string) bool { return true },
		HotAttach: func(string) error {
			hotCalled = true
			return nil
		},
	})

	if err := component.Resume(" session-b "); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if hotCalled {
		t.Fatal("恢复空壳仍有效时不得重复热挂载")
	}
	if got := strings.Join(locker.calls, ","); got != "lock,unlock" {
		t.Fatalf("transition calls = %q, want lock,unlock", got)
	}
}

func TestSessionResumeRestoringSettledFallsBackToHotAttach(t *testing.T) {
	locker := &recordingLocker{}
	var hotCalled bool
	component := applicationsession.NewResume(applicationsession.ResumeDeps{
		TransitionForSession: func(string) sync.Locker { return locker },
		Snapshot: func(string) applicationsession.ResumeState {
			return applicationsession.ResumeState{Restoring: true}
		},
		PointViewAtRestoringShell: func(string) bool { return false },
		HotAttach: func(sessionID string) error {
			hotCalled = sessionID == "session-b"
			return nil
		},
	})

	if err := component.Resume("session-b"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if !hotCalled {
		t.Fatal("装载已收口时必须改走热挂载")
	}
}

func TestSessionResumeHotTargetMarksIntentBeforeAttach(t *testing.T) {
	locker := &recordingLocker{}
	var calls []string
	component := applicationsession.NewResume(applicationsession.ResumeDeps{
		TransitionForSession: func(string) sync.Locker { return locker },
		Snapshot: func(string) applicationsession.ResumeState {
			return applicationsession.ResumeState{Hot: true}
		},
		MarkViewIntent: func(sessionID string) {
			calls = append(calls, "intent:"+sessionID)
		},
		HotAttach: func(sessionID string) error {
			calls = append(calls, "hot:"+sessionID)
			return nil
		},
	})

	if err := component.Resume("session-b"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got := strings.Join(calls, ","); got != "intent:session-b,hot:session-b" {
		t.Fatalf("calls = %q", got)
	}
	if got := strings.Join(locker.calls, ","); got != "lock,unlock" {
		t.Fatalf("transition calls = %q, want lock,unlock", got)
	}
}

func TestSessionResumeColdTargetRunsBackgroundWhileChatBusy(t *testing.T) {
	locker := &recordingLocker{}
	var calls []string
	component := applicationsession.NewResume(applicationsession.ResumeDeps{
		TransitionForSession: func(string) sync.Locker { return locker },
		Snapshot: func(string) applicationsession.ResumeState {
			return applicationsession.ResumeState{Running: true, Previous: "session-a"}
		},
		BeginAsyncRestore: func(sessionID string) (uint64, error) {
			calls = append(calls, "begin:"+sessionID)
			return 9, nil
		},
		ResumeColdInBackground: func(sessionID, previous string, epoch uint64) {
			calls = append(calls, "background:"+sessionID+":"+previous)
			if epoch != 9 {
				t.Fatalf("epoch = %d, want 9", epoch)
			}
		},
		ResumeCold: func(string, uint64) error {
			t.Fatal("运行中忙碌时不得同步冷加载")
			return nil
		},
	})

	if err := component.Resume("session-b"); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if got := strings.Join(calls, ","); got != "begin:session-b,background:session-b:session-a" {
		t.Fatalf("calls = %q", got)
	}
	if got := strings.Join(locker.calls, ","); got != "lock,unlock" {
		t.Fatalf("transition calls = %q, want lock,unlock", got)
	}
}

func TestSessionResumeColdFailureRunsRecovery(t *testing.T) {
	locker := &recordingLocker{}
	restoreErr := errors.New("disk failed")
	var handled bool
	component := applicationsession.NewResume(applicationsession.ResumeDeps{
		TransitionForSession: func(string) sync.Locker { return locker },
		Snapshot: func(string) applicationsession.ResumeState {
			return applicationsession.ResumeState{Previous: "session-a"}
		},
		BeginAsyncRestore: func(string) (uint64, error) { return 4, nil },
		ResumeCold:        func(string, uint64) error { return restoreErr },
		HandleFailure: func(sessionID, previous string, epoch uint64, cause error) {
			handled = sessionID == "session-b" && previous == "session-a" && epoch == 4 && errors.Is(cause, restoreErr)
		},
	})

	err := component.Resume("session-b")

	if !errors.Is(err, restoreErr) {
		t.Fatalf("Resume error = %v, want restore error", err)
	}
	if !handled {
		t.Fatal("冷加载失败必须调用失败恢复")
	}
}
