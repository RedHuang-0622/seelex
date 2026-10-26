package cold

import (
	"time"

	session_runtime "github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/middle/protocol/model"
	sessioncase "github.com/RedHuang-0622/seelex/middle/switch/sessionusecase"
	view_state "github.com/RedHuang-0622/seelex/middle/view/state"
)

// ViewDeps supplies view construction and mirror callbacks.
type ViewDeps struct {
	LoadForkRef      func(location session_runtime.Location, sessionID string) *model.SessionForkRef
	AdvanceSeq       func(sessionID string, messages []model.Message)
	RecordTail       func(record model.SessionRecord, window int) []model.Message
	ViewEmpty        func(sessionID string) bool
	SetView          func(sessionID string, view *sessioncase.View)
	AppendHistory    func(sessionID string, history []contract.EngineMessage)
	MutateView       func(sessionID string, update func(*sessioncase.View))
	MirrorActiveView func()
}

// ViewApplication owns cold view construction and installation.
type ViewApplication struct {
	deps ViewDeps
}

// NewView constructs the cold-view application.
func NewView(deps ViewDeps) *ViewApplication {
	return &ViewApplication{deps: deps}
}

// InstallLocked installs record/legacy visible history under the view lock.
func (application *ViewApplication) InstallLocked(
	sessionID string,
	location session_runtime.Location,
	record model.SessionRecord,
	hasRecord bool,
	visibleHistory []contract.EngineMessage,
	historyTotal int,
	total int,
	offset int,
	window int,
	mayActivate bool,
	snapshot *model.Snapshot,
) {
	view := &sessioncase.View{
		TotalMessages:      total,
		HistoryOffset:      offset,
		HasMoreHistory:     offset > 0,
		ConversationWindow: window,
		ForkedFrom:         application.deps.LoadForkRef(location, sessionID),
	}
	if hasRecord {
		application.deps.AdvanceSeq(sessionID, record.Conversation.Messages)
		view.Conversation = append(view.Conversation, model.Message{
			Role: "system", Content: "已恢复会话: " + sessionID, CreatedAt: time.Now(),
		})
		view.Conversation = append(view.Conversation, application.deps.RecordTail(record, window)...)
		view.ReadFiles = append([]model.ReadFileRef(nil), record.Execution.ReadFiles...)
		if application.deps.ViewEmpty(sessionID) {
			application.deps.SetView(sessionID, view)
		}
		if mayActivate {
			snapshot.Session.ForkedFrom = model.CloneSessionForkRef(view.ForkedFrom)
			if record.Execution.Task != nil {
				task := *record.Execution.Task
				task.ContextCompactions = append(
					[]model.ContextCompaction(nil),
					record.Execution.Task.ContextCompactions...,
				)
				snapshot.Task = &task
			} else {
				snapshot.Task = nil
			}
		}
		return
	}
	application.deps.AppendHistory(sessionID, visibleHistory)
	if historyTotal > 0 {
		application.deps.MutateView(sessionID, func(current *sessioncase.View) {
			visible := view_state.DurableConversationCount(current.Conversation)
			if historyTotal > visible {
				current.TotalMessages = historyTotal
				current.HistoryOffset = historyTotal - visible
				current.HasMoreHistory = true
			}
		})
		application.deps.MirrorActiveView()
	}
}
