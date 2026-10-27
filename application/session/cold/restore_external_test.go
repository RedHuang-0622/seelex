package cold_test

import (
	"errors"
	cold "github.com/RedHuang-0622/seelex/application/session/cold"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/kernel/session"
	"strings"
	"testing"
)

func TestSessionColdRestoreResumesEngineAndQueue(t *testing.T) {
	location := session_runtime.Location{WorkspaceID: "workspace-a"}
	history := []contract.EngineMessage{{Role: "user", Content: "resume"}}
	var calls []string
	component := cold.NewRestore(cold.RestoreDeps{
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
	component := cold.NewRestore(cold.RestoreDeps{
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
			component := cold.NewRestore(cold.RestoreDeps{
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
