// Package sessionhistory owns session resume, cold restore and visible-history
// window installation.
package sessionhistory

import (
	appconversation "github.com/RedHuang-0622/seelex/application/conversation"
	"github.com/RedHuang-0622/seelex/application/event"
	applicationhistory "github.com/RedHuang-0622/seelex/application/history"
	appsession "github.com/RedHuang-0622/seelex/application/session"
	appsessioncold "github.com/RedHuang-0622/seelex/application/session/cold"
	appsessionresidency "github.com/RedHuang-0622/seelex/application/session/residency"
	sessionruntime "github.com/RedHuang-0622/seelex/application/session/runtime"
	sessionstate "github.com/RedHuang-0622/seelex/application/session/state"
	viewstate "github.com/RedHuang-0622/seelex/application/session/viewstate"
	switchworkspace "github.com/RedHuang-0622/seelex/application/workspace/switch"
	"github.com/RedHuang-0622/seelex/contract"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	kernelstate "github.com/RedHuang-0622/seelex/kernel/state"
)

const (
	EventSnapshotChanged = event.EventSnapshotChanged
	RoleNameMain         = protocol.RoleNameMain
)

type historyComponents struct {
	conversationStreams    appconversation.StreamOutputPort
	workspace              *switchworkspace.UseCase
	residencyRuntime       *appsessionresidency.Runtime
	sessions               *sessionruntime.Coordinator
	view                   *viewstate.Coordinator
	sessionColdRead        *appsessioncold.ReadApplication
	sessionColdHistory     *appsessioncold.HistoryApplication
	sessionColdRestore     *appsessioncold.RestoreApplication
	sessionColdWorkspace   *appsessioncold.WorkspaceApplication
	sessionColdActivation  *appsessioncold.ActivationApplication
	sessionColdTask        *appsessioncold.TaskApplication
	sessionColdView        *appsessioncold.ViewApplication
	sessionColdPublish     *appsessioncold.PublishApplication
	sessionColdCommit      *appsessioncold.CommitApplication
	sessionRestoreRecovery *appsession.RestoreRecoveryApplication
	sessionRestoringShell  *appsession.RestoringShellApplication
}

// RuntimeDeps supplies owner components and the few cross-capability callbacks
// needed by session history/restore.
type RuntimeDeps struct {
	Core                *kernelstate.Core
	Deps                contract.Dependencies
	ConversationStreams appconversation.StreamOutputPort
	Workspace           *switchworkspace.UseCase
	Residency           *appsessionresidency.Runtime
	Sessions            *sessionruntime.Coordinator
	View                *viewstate.Coordinator
	ColdRead            *appsessioncold.ReadApplication
	ColdHistory         *appsessioncold.HistoryApplication
	ColdRestore         *appsessioncold.RestoreApplication
	ColdWorkspace       *appsessioncold.WorkspaceApplication
	ColdActivation      *appsessioncold.ActivationApplication
	ColdTask            *appsessioncold.TaskApplication
	ColdView            *appsessioncold.ViewApplication
	ColdPublish         *appsessioncold.PublishApplication
	ColdCommit          *appsessioncold.CommitApplication
	RestoreRecovery     *appsession.RestoreRecoveryApplication
	RestoringShell      *appsession.RestoringShellApplication
	Closed              func() bool
	SessionUnit         func(string) *sessionstate.SessionUnit
	PublishEvent        func(event.EventKind, uint64, string, string, any) event.Event
	StoredTier          func(string) string
	DisplayInput        func(string) string
	ResetTurnState      func(*sessionstate.SessionUnit)
}

// Runtime owns application-level session history and restore orchestration.
type Runtime struct {
	*kernelstate.Core
	Deps       contract.Dependencies
	components historyComponents

	closedValue    func() bool
	sessionUnit    func(string) *sessionstate.SessionUnit
	publishEvent   func(event.EventKind, uint64, string, string, any) event.Event
	storedTier     func(string) string
	displayInput   func(string) string
	resetTurnState func(*sessionstate.SessionUnit)
}

// NewRuntime constructs the session history runtime.
func NewRuntime(deps RuntimeDeps) *Runtime {
	return &Runtime{
		Core: deps.Core,
		Deps: deps.Deps,
		components: historyComponents{
			conversationStreams:    deps.ConversationStreams,
			workspace:              deps.Workspace,
			residencyRuntime:       deps.Residency,
			sessions:               deps.Sessions,
			view:                   deps.View,
			sessionColdRead:        deps.ColdRead,
			sessionColdHistory:     deps.ColdHistory,
			sessionColdRestore:     deps.ColdRestore,
			sessionColdWorkspace:   deps.ColdWorkspace,
			sessionColdActivation:  deps.ColdActivation,
			sessionColdTask:        deps.ColdTask,
			sessionColdView:        deps.ColdView,
			sessionColdPublish:     deps.ColdPublish,
			sessionColdCommit:      deps.ColdCommit,
			sessionRestoreRecovery: deps.RestoreRecovery,
			sessionRestoringShell:  deps.RestoringShell,
		},
		closedValue:    deps.Closed,
		sessionUnit:    deps.SessionUnit,
		publishEvent:   deps.PublishEvent,
		storedTier:     deps.StoredTier,
		displayInput:   deps.DisplayInput,
		resetTurnState: deps.ResetTurnState,
	}
}

func (runtime *Runtime) closed() bool {
	return runtime != nil && runtime.closedValue != nil && runtime.closedValue()
}

func (runtime *Runtime) sessionUnitLocked(sessionID string) *sessionstate.SessionUnit {
	if runtime == nil || runtime.sessionUnit == nil {
		return nil
	}
	return runtime.sessionUnit(sessionID)
}

func (runtime *Runtime) publishSessionEvent(kind event.EventKind, revision uint64, requestID, sessionID string, payload any) event.Event {
	if runtime == nil || runtime.publishEvent == nil {
		return event.Event{}
	}
	return runtime.publishEvent(kind, revision, requestID, sessionID, payload)
}

func (runtime *Runtime) readStoredPermissionTier(sessionID string) string {
	if runtime == nil || runtime.storedTier == nil {
		return ""
	}
	return runtime.storedTier(sessionID)
}

func (runtime *Runtime) displayUserInput(input string) string {
	if runtime == nil || runtime.displayInput == nil {
		return input
	}
	return runtime.displayInput(input)
}

func (runtime *Runtime) ResetTurnState(unit *sessionstate.SessionUnit) {
	if runtime != nil && runtime.resetTurnState != nil {
		runtime.resetTurnState(unit)
	}
}

func (runtime *Runtime) BeginAsyncRestore(sessionID string) (uint64, error) {
	return runtime.beginAsyncRestore(sessionID)
}

func (runtime *Runtime) PointViewAtRestoringShell(sessionID string) bool {
	return runtime.pointViewAtRestoringShell(sessionID)
}

func (runtime *Runtime) ResumeSessionColdInBackground(sessionID, previousID string, epoch uint64) {
	runtime.resumeSessionColdInBackground(sessionID, previousID, epoch)
}

func (runtime *Runtime) ResumeSessionCold(sessionID string, epoch uint64) error {
	return runtime.resumeSessionCold(sessionID, epoch)
}

func (runtime *Runtime) AttachSessionContextFor(workspaceID, sessionID string) error {
	return runtime.attachSessionContextFor(workspaceID, sessionID)
}

func (runtime *Runtime) VisibleTailServedFromMemory(sessionID string, assembler applicationhistory.WindowAssembler) bool {
	return runtime.visibleTailServedFromMemory(sessionID, assembler)
}

func (runtime *Runtime) LoadConversationTailPage(workspaceID, sessionID string, window int) (applicationhistory.Page, error) {
	return runtime.loadConversationTailPage(workspaceID, sessionID, window)
}

func (runtime *Runtime) LoadEarlierVisiblePage(workspaceID, sessionID string, offset, limit, window int, seam protocol.Message, assembler applicationhistory.WindowAssembler) (applicationhistory.Page, error) {
	return runtime.loadEarlierVisiblePage(workspaceID, sessionID, offset, limit, window, seam, assembler)
}

func (runtime *Runtime) InstallVisibleHistory(sessionID string, page applicationhistory.Page, window int, mode applicationhistory.InstallMode, assembler applicationhistory.WindowAssembler) error {
	return runtime.installVisibleHistory(sessionID, page, window, mode, assembler)
}

func (runtime *Runtime) VisibleWindowFirstRow(sessionID string, assembler applicationhistory.WindowAssembler) protocol.Message {
	return runtime.visibleWindowFirstRow(sessionID, assembler)
}

func (runtime *Runtime) CurrentWorkspaceIDLocked() string {
	return currentWorkspaceIDLocked(runtime)
}
