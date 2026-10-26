package execution

import (
	"testing"

	"github.com/RedHuang-0622/seelex/application/event"
	"github.com/RedHuang-0622/seelex/contract/dto"
)

func TestRoleActivityApplicationRoutesStagesAndPublishes(t *testing.T) {
	var dropped []string
	var staged []string
	var published []string
	application := NewRoleActivity(RoleActivityDeps{
		RequestID:     func() string { return "req" },
		EvidenceChars: func() int { return 4 },
		Truncate: func(value string, limit int) string {
			runes := []rune(value)
			if len(runes) <= limit {
				return value
			}
			return string(runes[:limit]) + "…"
		},
		Stage: func(owner, unitID, name, status, errorText string) {
			staged = append(staged, owner+":"+unitID+":"+name+":"+status+":"+errorText)
		},
		Publish: func(kind event.EventKind, revision uint64, requestID, sessionID string, _ any) {
			published = append(published, string(kind)+":"+requestID+":"+sessionID)
		},
		NoteDropped:   func(reason string) { dropped = append(dropped, reason) },
		StartedKind:   event.EventTeammateToolStarted,
		CompletedKind: event.EventTeammateToolCompleted,
	})

	application.Handle(dto.RoleToolActivity{ID: "call-1", RoleSessionID: "role"})
	application.Handle(dto.RoleToolActivity{
		ID: "call-2", MainSessionID: "main", RoleSessionID: "role", Name: "bash",
		Status: "running", Arguments: "abcdefgh", Result: "ijklmnop", Error: "qrstuvwx",
	})
	application.Handle(dto.RoleToolActivity{
		ID: "call-3", MainSessionID: "main", RoleSessionID: "role", Name: "bash",
		Status: "failed", Error: "boom",
	})

	if len(dropped) != 1 || dropped[0] == "" {
		t.Fatalf("dropped = %v", dropped)
	}
	if len(staged) != 2 || staged[0] != "main:role:role:bash:running:qrst…" || staged[1] != "main:role:role:bash:failed:boom" {
		t.Fatalf("staged = %v", staged)
	}
	if len(published) != 2 || published[0] != string(event.EventTeammateToolStarted)+":req:main" ||
		published[1] != string(event.EventTeammateToolCompleted)+":req:main" {
		t.Fatalf("published = %v", published)
	}
}
