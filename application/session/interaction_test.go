package session

import (
	"context"
	"errors"
	"testing"

	"github.com/RedHuang-0622/seelex/contract"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	switching "github.com/RedHuang-0622/seelex/kernel/sessionpolicy"
)

func TestInteractionApplicationResolvesViewAndBackgroundApprovals(t *testing.T) {
	current := &protocol.Interaction{ID: "ask-view", Kind: switching.InteractionKindApproval}
	var resolved []string
	var closed []string
	application := NewInteraction(InteractionDeps{
		Current: func() *protocol.Interaction { return current },
		PendingApprovalSession: func(id string) (string, bool) {
			return "background-session", id == "ask-bg"
		},
		ResolveApproval: func(id, option string) error {
			resolved = append(resolved, id+":"+option)
			return nil
		},
		CloseInteraction: func(id string) { closed = append(closed, id) },
		ResumeSession:    func(string) error { return nil },
		AddNotice:        func(string) {},
		SelectAccount:    func(context.Context, string) error { return nil },
	})

	if err := application.Resolve(context.Background(), "ask-view", "allow"); err != nil {
		t.Fatal(err)
	}
	if err := application.Resolve(context.Background(), "ask-bg", "allow"); err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 2 || resolved[0] != "ask-view:allow" || resolved[1] != "ask-bg:allow" {
		t.Fatalf("resolved = %v", resolved)
	}
	if len(closed) != 0 {
		t.Fatalf("approval broker owns close observation, got closes = %v", closed)
	}
	if err := application.Resolve(context.Background(), "missing", "allow"); !errors.Is(err, contract.ErrInteractionNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestInteractionApplicationSessionFailureNotifiesAndCloses(t *testing.T) {
	resumeErr := errors.New("resume failed")
	var notices []string
	var closed []string
	application := NewInteraction(InteractionDeps{
		Current: func() *protocol.Interaction {
			return &protocol.Interaction{ID: "resume", Kind: switching.InteractionKindSession}
		},
		ResolveApproval:  func(string, string) error { return nil },
		CloseInteraction: func(id string) { closed = append(closed, id) },
		ResumeSession:    func(string) error { return resumeErr },
		AddNotice:        func(notice string) { notices = append(notices, notice) },
		SelectAccount:    func(context.Context, string) error { return nil },
	})
	if err := application.Resolve(context.Background(), "resume", "saved-session"); !errors.Is(err, resumeErr) {
		t.Fatalf("resume err = %v", err)
	}
	if len(notices) != 1 || len(closed) != 1 {
		t.Fatalf("notices = %v closed = %v", notices, closed)
	}
}
