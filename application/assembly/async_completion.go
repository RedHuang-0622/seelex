package assembly

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/RedHuang-0622/seelex/application/assembly/continuation"
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	goaldomain "github.com/RedHuang-0622/seelex/kernel/goal"
)

// 自动续跑的装配入口：把既有组件作为端口值交给 application/assembly/continuation
// 子包（判据在 application/execution，事实源在各自的域）。
// 接线清单与语义：docs/arch/goal-auto-continue.md §2.8.8。

// sessionIdleForAsyncTrigger reports whether a session can start a completion
// turn. A missing unit is deliberately treated as unavailable.
func (service *Service) sessionIdleForAsyncTrigger(sessionID string) bool {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	if service.closed || service.draining {
		return false
	}
	unit := service.sessions.Unit(sessionID)
	if unit == nil {
		return false
	}
	return !unit.ChatState().Running
}

// continuationGate 组装续跑 gate。
func (service *Service) continuationGate() *applicationexecution.ContinuationApplication {
	return continuation.New(continuation.Ports{
		Tasks:        service.components.tasks,
		Goals:        service.components.goalApplication,
		Sessions:     service.sessions,
		RoleSessions: service.components.roleSessions,
		Idle:         service.sessionIdleForAsyncTrigger,
		Submit:       service.components.conversationTurn.submission.SubmitFor,
		Audit: func(sessionID string, entry applicationexecution.ContinuationAudit) {
			detail := strings.TrimSpace(entry.Detail)
			if entry.Truncated {
				detail = strings.TrimSpace(detail + " truncated=true")
			}
			if entry.Manual {
				detail = strings.TrimSpace(detail + " manual=true")
			}
			_ = service.components.goalApplication.AuditFor(context.Background(), sessionID, goaldomain.AuditEntry{
				Kind:   goaldomain.EventKind(entry.Kind),
				GoalID: entry.GoalID,
				At:     time.Now().Unix(),
				Reason: entry.Reason,
				Detail: strings.TrimSpace(detail + " attempt=" + strconv.Itoa(entry.Attempt)),
			})
		},
	})
}
