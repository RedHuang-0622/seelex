// Package session_runtime is the Application-facing identity facade for the
// Engine-owned session domain coordinator and its pure helpers.
package session_runtime

import (
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	sessionruntime "github.com/RedHuang-0622/seelex/kernel/session"
)

type (
	Coordinator                  = sessionruntime.Coordinator
	Deps                         = sessionruntime.Deps
	Location                     = sessionruntime.Location
	SessionContextPort           = sessionruntime.SessionContextPort
	SessionConversationRangePort = sessionruntime.SessionConversationRangePort
	SessionForkPort              = sessionruntime.SessionForkPort
	SessionTranscriptPort        = sessionruntime.SessionTranscriptPort
)

var SessionArchiveResumePrefix = sessionruntime.SessionArchiveResumePrefix

func NewCoordinator(deps Deps) *Coordinator {
	return sessionruntime.NewCoordinator(deps)
}

func SessionTitle(input string) string {
	return sessionruntime.SessionTitle(input)
}

func SessionTitleFromHistory(history []contract.EngineMessage, displayUserInput func(string) string) string {
	return sessionruntime.SessionTitleFromHistory(history, displayUserInput)
}

func WorkspaceID(workspace *model.WorkspaceInfo) string {
	return sessionruntime.WorkspaceID(workspace)
}

func RecordResumeHistory(record model.SessionRecord) []contract.EngineMessage {
	return sessionruntime.RecordResumeHistory(record)
}

func CloneSessionPlanStack(stack []model.SessionPlanFrame) []model.SessionPlanFrame {
	return sessionruntime.CloneSessionPlanStack(stack)
}
