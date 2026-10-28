package sessionstate

import (
	"testing"

	"github.com/RedHuang-0622/seelex/contract/dto"
)

func TestProjectVisibleStatusPriority(t *testing.T) {
	tests := []struct {
		name  string
		facts VisibleStatusFacts
		want  dto.SessionStatus
	}{
		{
			name: "restoring wins over approval",
			facts: VisibleStatusFacts{
				Restoring:            true,
				PendingApprovalCount: 1,
			},
			want: dto.SessionStatusRestoring,
		},
		{
			name: "approval wins over running",
			facts: VisibleStatusFacts{
				HasUnit:              true,
				PendingApprovalCount: 1,
				Running:              true,
			},
			want: dto.SessionStatusAwaitingApproval,
		},
		{
			name:  "running wins over pending requests",
			facts: VisibleStatusFacts{HasUnit: true, Running: true, PendingRequests: 2, QueuedCount: 0},
			want:  dto.SessionStatusRunning,
		},
		{
			name:  "pending requests own queued state",
			facts: VisibleStatusFacts{HasUnit: true, PendingRequests: 1},
			want:  dto.SessionStatusQueued,
		},
		{
			name:  "chat queued count is display-only",
			facts: VisibleStatusFacts{HasUnit: true, QueuedCount: 3},
			want:  dto.SessionStatusIdle,
		},
		{
			name:  "missing unit is idle",
			facts: VisibleStatusFacts{},
			want:  dto.SessionStatusIdle,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ProjectVisibleStatus(test.facts); got != test.want {
				t.Fatalf("ProjectVisibleStatus(%+v) = %q, want %q", test.facts, got, test.want)
			}
		})
	}
}
