// Package switching is the Application-facing identity facade for the shared
// Middle session-switch policies and small interaction/draft helpers.
package switching

import (
	"time"

	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	middleswitch "github.com/RedHuang-0622/seelex/middle/switch"
)

type (
	BusyState         = middleswitch.BusyState
	DraftTarget       = middleswitch.DraftTarget
	ForkGate          = middleswitch.ForkGate
	MetaDeps          = middleswitch.MetaDeps
	PendingTailDeps   = middleswitch.PendingTailDeps
	ProtectedInput    = middleswitch.ProtectedInput
	ReleaseWindowDeps = middleswitch.ReleaseWindowDeps
	SessionChoice     = middleswitch.SessionChoice
	AccountChoice     = middleswitch.AccountChoice
	UnloadGate        = middleswitch.UnloadGate
)

const (
	InteractionCancelOptionID = middleswitch.InteractionCancelOptionID
	InteractionKindApproval   = middleswitch.InteractionKindApproval
	InteractionKindSession    = middleswitch.InteractionKindSession
	InteractionKindAccount    = middleswitch.InteractionKindAccount
	DraftSessionName          = middleswitch.DraftSessionName
)

var (
	ErrQueueNotRunning      = middleswitch.ErrQueueNotRunning
	ErrQueueIndexOutOfRange = middleswitch.ErrQueueIndexOutOfRange
	ErrQueueSessionNotFound = middleswitch.ErrQueueSessionNotFound
)

func TeamworkJobCompletions(runtime any) []dto.TeamworkJobCompletionRecord {
	return middleswitch.TeamworkJobCompletions(runtime)
}

func TeamworkJobEvents(runtime any) <-chan struct{} {
	return middleswitch.TeamworkJobEvents(runtime)
}

func QueueTargetSession(sessionID, viewSessionID string) string {
	return middleswitch.QueueTargetSession(sessionID, viewSessionID)
}

func QueueTargetError(sessionID, viewSessionID string, known, running bool) error {
	return middleswitch.QueueTargetError(sessionID, viewSessionID, known, running)
}

func Busy(state BusyState) bool {
	return middleswitch.Busy(state)
}

func NewDraftSessionID(now time.Time, seq uint64) string {
	return middleswitch.NewDraftSessionID(now, seq)
}

func MaxTranscriptEventSeq(events []model.TranscriptEvent) uint64 {
	return middleswitch.MaxTranscriptEventSeq(events)
}

func DeepCopyRecord(
	record model.SessionRecord,
	clonePlanStack func([]model.SessionPlanFrame) []model.SessionPlanFrame,
) model.SessionRecord {
	return middleswitch.DeepCopyRecord(record, clonePlanStack)
}

func SetReleaseWindow(deps ReleaseWindowDeps, window int) (int, error) {
	return middleswitch.SetReleaseWindow(deps, window)
}

func ReleaseWindow(deps ReleaseWindowDeps) int {
	return middleswitch.ReleaseWindow(deps)
}

func RecoverPendingTail(deps PendingTailDeps, sessionID string) string {
	return middleswitch.RecoverPendingTail(deps, sessionID)
}

func Protected(input ProtectedInput) bool {
	return middleswitch.Protected(input)
}

func UnsupportedInteractionKind(kind string) error {
	return middleswitch.UnsupportedInteractionKind(kind)
}

func SessionInteractionID(now time.Time) string {
	return middleswitch.SessionInteractionID(now)
}

func BuildSessionInteraction(id string, sessions []SessionChoice, openedAt time.Time) model.Interaction {
	return middleswitch.BuildSessionInteraction(id, sessions, openedAt)
}

func AccountInteractionID(now time.Time) string {
	return middleswitch.AccountInteractionID(now)
}

func BuildAccountInteraction(id string, accounts []AccountChoice, openedAt time.Time) model.Interaction {
	return middleswitch.BuildAccountInteraction(id, accounts, openedAt)
}
