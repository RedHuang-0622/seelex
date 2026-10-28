package sessionstate

import "github.com/RedHuang-0622/seelex/contract/dto"

// VisibleStatusFacts 是可见会话状态投影所需的运行期事实。
//
// Restoring 与 PendingApprovalCount 来自会话运行时；Running 来自执行内核；
// PendingRequests 是运行期队列的唯一权威。QueuedCount 只作为 ChatState 的
// 展示投影传入，不能反向决定状态。
type VisibleStatusFacts struct {
	Restoring            bool
	HasUnit              bool
	PendingApprovalCount int
	Running              bool
	PendingRequests      int
	QueuedCount          int
}

// ProjectVisibleStatus 是会话可见状态的唯一判据。
func ProjectVisibleStatus(facts VisibleStatusFacts) dto.SessionStatus {
	switch {
	case facts.Restoring:
		return dto.SessionStatusRestoring
	case !facts.HasUnit:
		return dto.SessionStatusIdle
	case facts.PendingApprovalCount > 0:
		return dto.SessionStatusAwaitingApproval
	case facts.Running:
		return dto.SessionStatusRunning
	case facts.PendingRequests > 0:
		return dto.SessionStatusQueued
	default:
		return dto.SessionStatusIdle
	}
}
