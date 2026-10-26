package core

import (
	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationsessioncold "github.com/RedHuang-0622/seelex/application/session/cold"
	session_runtime "github.com/RedHuang-0622/seelex/application/session/runtime"
	applicationwindow "github.com/RedHuang-0622/seelex/application/window"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

func newServiceSessionColdHistory(service *Service) *applicationsessioncold.HistoryApplication {
	return applicationsessioncold.NewHistory(applicationsessioncold.HistoryDeps{
		TailBudget: func() int { return RetainedReadTailBudget(service.Deps.Runtime) },
		MinRounds:  func() int { return applicationwindow.Current().MinRounds },
		LatestUser: func(messages []model.Message) string {
			return service.components.sessions.LatestUserContent(messages)
		},
		TranscriptContainsUser: func(transcript []model.TranscriptEvent, latestUser string) bool {
			return service.components.sessions.TranscriptContainsUser(transcript, latestUser)
		},
		RecordTranscript: func(record model.SessionRecord) []model.TranscriptEvent {
			return service.components.sessions.RecordConversationTranscript(record)
		},
		TranscriptTailHistory: task_context.TranscriptTailHistory,
		AssembleWireHistory: func(sessionID string, tailBudget, k int) ([]contract.EngineMessage, bool, error) {
			location := service.components.sessions.LocateSession(sessionID)
			return service.components.sessions.AssembleWireHistoryWorkspace(location, sessionID, tailBudget, k)
		},
		RecordHistory: func(record model.SessionRecord, tailBudget, maxTurns int) []contract.EngineMessage {
			return service.components.sessions.RecordConversationResumeHistory(record, tailBudget, maxTurns)
		},
		HistoryContainsUser: func(history []contract.EngineMessage, latestUser string) bool {
			return service.components.sessions.HistoryContainsUser(history, latestUser)
		},
		RecordResumeHistory: session_runtime.RecordResumeHistory,
	})
}
