package session_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	applicationsession "github.com/RedHuang-0622/seelex/application/session"
)

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

func TestDraftSubmissionSkipsLoadedTarget(t *testing.T) {
	locker := &recordingLocker{}
	component := applicationsession.NewDraftSubmission(applicationsession.DraftSubmissionDeps{
		IsDraftTarget: func(string) bool { return false },
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

func TestDraftSubmissionRejectsStaleDraftView(t *testing.T) {
	locker := &recordingLocker{}
	notInView := errors.New("not in view")
	component := applicationsession.NewDraftSubmission(applicationsession.DraftSubmissionDeps{
		IsDraftTarget: func(sessionID string) bool { return sessionID == "draft-a" },
		Transition:    func() sync.Locker { return locker },
		IsInView:      func(string) bool { return false },
		Materialize: func(string) error {
			t.Fatal("视图已离开草稿时不得物化")
			return nil
		},
		NotInView: notInView,
	})

	if err := component.Materialize("draft-a", "input"); !errors.Is(err, notInView) {
		t.Fatalf("Materialize error = %v, want %v", err, notInView)
	}
	if got := strings.Join(locker.calls, ","); got != "lock,unlock" {
		t.Fatalf("lock calls = %q, want lock,unlock", got)
	}
}

func TestDraftSubmissionTrimsAfterViewCheck(t *testing.T) {
	locker := &recordingLocker{}
	var materialized string
	component := applicationsession.NewDraftSubmission(applicationsession.DraftSubmissionDeps{
		IsDraftTarget: func(sessionID string) bool { return sessionID == "draft-a" },
		Transition:    func() sync.Locker { return locker },
		IsInView:      func(sessionID string) bool { return sessionID == "draft-a" },
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
