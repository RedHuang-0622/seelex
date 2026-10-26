package execution

import (
	"context"
	"strings"
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
	switching "github.com/RedHuang-0622/seelex/middle/switch"
)

func TestCompletionTriggersIdleAsyncJobOnce(t *testing.T) {
	record := dto.AsyncRunRecord{
		Handle: "job-1", SessionID: " session-a ", Kind: "process",
		State: dto.AsyncStateDone, ExitCode: 0,
		Description: "build", Summary: strings.Repeat("x", 10),
	}
	var submittedSession, submittedPrompt string
	application := NewCompletion(CompletionDeps{
		AsyncRuns: func() []dto.AsyncRunRecord { return []dto.AsyncRunRecord{record} },
		SessionIdle: func(sessionID string) bool {
			return sessionID == "session-a"
		},
		SubmitConversation: func(_ context.Context, sessionID, prompt string) error {
			submittedSession, submittedPrompt = sessionID, prompt
			return nil
		},
		EvidenceChars: func() int { return 4 },
	})
	triggered := map[string]struct{}{}
	if pending := application.TriggerAsyncCompletions(triggered); pending != 0 {
		t.Fatalf("pending = %d", pending)
	}
	if submittedSession != "session-a" || !strings.Contains(submittedPrompt, "handle: job-1") ||
		!strings.Contains(submittedPrompt, "- 摘要: xxxx") {
		t.Fatalf("submitted = %q, %q", submittedSession, submittedPrompt)
	}
	submittedSession = ""
	application.TriggerAsyncCompletions(triggered)
	if submittedSession != "" {
		t.Fatal("same async handle triggered twice")
	}
}

func TestCompletionKeepsBusyAsyncJobPending(t *testing.T) {
	application := NewCompletion(CompletionDeps{
		AsyncRuns: func() []dto.AsyncRunRecord {
			return []dto.AsyncRunRecord{{
				Handle: "job-busy", SessionID: "session-a", State: dto.AsyncStateFailed,
			}}
		},
		SessionIdle: func(string) bool { return false },
		SubmitConversation: func(context.Context, string, string) error {
			t.Fatal("busy session was submitted")
			return nil
		},
		EvidenceChars: func() int { return 8 },
	})
	triggered := map[string]struct{}{}
	if pending := application.TriggerAsyncCompletions(triggered); pending != 1 {
		t.Fatalf("pending = %d, want 1", pending)
	}
	if _, ok := triggered["job-busy"]; ok {
		t.Fatal("busy job was recorded as triggered")
	}
}

func TestCompletionPrunesRemovedAsyncHandles(t *testing.T) {
	application := NewCompletion(CompletionDeps{
		AsyncRuns:     func() []dto.AsyncRunRecord { return nil },
		SessionIdle:   func(string) bool { return true },
		EvidenceChars: func() int { return 8 },
	})
	triggered := map[string]struct{}{"old": {}}
	application.TriggerAsyncCompletions(triggered)
	if len(triggered) != 0 {
		t.Fatalf("triggered handles not pruned: %+v", triggered)
	}
}

func TestCompletionTriggersTeamworkJobWithNamespacedKey(t *testing.T) {
	var prompt string
	application := NewCompletion(CompletionDeps{
		TeamworkRuns: func() []dto.TeamworkJobCompletionRecord {
			return []dto.TeamworkJobCompletionRecord{{
				Handle: "team-job", SessionID: "session-a", Role: "builder", WorkItem: "item-1",
				Kind: "job", State: dto.AsyncStateDone,
			}}
		},
		SessionIdle: func(string) bool { return true },
		SubmitConversation: func(_ context.Context, sessionID, text string) error {
			if sessionID != "session-a" {
				t.Fatalf("session = %q", sessionID)
			}
			prompt = text
			return nil
		},
		EvidenceChars: func() int { return 8 },
	})
	triggered := map[string]struct{}{}
	if pending := application.TriggerTeamworkJobCompletions(triggered); pending != 0 {
		t.Fatalf("pending = %d", pending)
	}
	key := switching.TeamworkTriggeredPrefix + "team-job"
	if _, ok := triggered[key]; !ok {
		t.Fatalf("triggered keys = %+v", triggered)
	}
	if !strings.Contains(prompt, "teammate: builder（工作项 item-1）") ||
		!strings.Contains(prompt, "team_accept / team_fail") {
		t.Fatalf("teamwork prompt = %q", prompt)
	}
}

func TestCompletionIgnoresNonTerminalAndOrphanJobs(t *testing.T) {
	application := NewCompletion(CompletionDeps{
		AsyncRuns: func() []dto.AsyncRunRecord {
			return []dto.AsyncRunRecord{
				{Handle: "running", SessionID: "session-a", State: dto.AsyncStateRunning},
				{Handle: "killed", SessionID: "session-a", State: dto.AsyncStateKilled},
				{Handle: "orphan", State: dto.AsyncStateDone},
			}
		},
		SessionIdle: func(string) bool { return true },
		SubmitConversation: func(context.Context, string, string) error {
			t.Fatal("non-terminal/orphan job submitted a turn")
			return nil
		},
		EvidenceChars: func() int { return 8 },
	})
	application.TriggerAsyncCompletions(map[string]struct{}{})
}
