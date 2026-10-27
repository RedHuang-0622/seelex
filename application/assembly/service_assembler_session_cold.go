package assembly

import (
	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationsessioncold "github.com/RedHuang-0622/seelex/application/session/cold"
	session_runtime "github.com/RedHuang-0622/seelex/application/session/runtime"
	sessioncase "github.com/RedHuang-0622/seelex/application/session/state"
	applicationwindow "github.com/RedHuang-0622/seelex/application/window"
	switchworkspace "github.com/RedHuang-0622/seelex/application/workspace/switch"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
)

// assembleSessionColdComponents owns the cold-restore dependency graph. The
// nine domain stages remain independently testable; only their core callback
// wiring is centralized here in the composition root.
func (service *Service) assembleSessionColdComponents() {
	service.components.sessionColdRead = applicationsessioncold.NewRead(applicationsessioncold.ReadDeps{
		RecoverTail: service.recoverPendingMessageTailAtLoad,
		Locate: func(sessionID string) session_runtime.Location {
			return service.components.sessions.LocateSession(sessionID)
		},
		LoadRecord: func(location session_runtime.Location, sessionID string) (model.SessionRecord, int, bool, error) {
			return service.components.sessions.LoadSessionResumeRecord(location, sessionID)
		},
		LoadTranscript: func(location session_runtime.Location, sessionID string) ([]model.TranscriptEvent, error) {
			return service.components.sessions.LoadSessionTranscript(location, sessionID)
		},
		LoadHistory: func(location session_runtime.Location) ([]contract.EngineMessage, int, error) {
			return service.components.sessions.LoadHistoryTailWindow(location)
		},
	})

	service.components.sessionColdHistory = applicationsessioncold.NewHistory(applicationsessioncold.HistoryDeps{
		TailBudget: func() int { return applicationwindow.RetainedReadTailBudget(service.Deps.Runtime) },
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

	service.components.sessionColdRestore = applicationsessioncold.NewRestore(applicationsessioncold.RestoreDeps{
		SetWorkspace: service.Deps.Runtime.SetSessionWorkspace,
		ResumeEngine: func(sessionID string, history []contract.EngineMessage) (bool, error) {
			if enginePort, ok := service.Deps.Engine.(interface {
				ResumeSession(string, []contract.EngineMessage) error
			}); ok {
				return true, enginePort.ResumeSession(sessionID, history)
			}
			return false, nil
		},
		ReplaceHistory: service.Deps.Engine.ReplaceHistory,
		RecoverQueue: func(location session_runtime.Location, sessionID string) error {
			resent, lifecycleOK, err := service.components.sessions.QueueRecoverInputs(location, sessionID)
			if err != nil {
				return err
			}
			if lifecycleOK && len(resent) > 0 {
				service.reEnqueueRecoveredInputs(sessionID, resent)
			}
			return nil
		},
		WritePrompt: service.writeSessionSystemPrompt,
	})

	service.components.sessionColdWorkspace = applicationsessioncold.NewWorkspace(applicationsessioncold.WorkspaceDeps{
		WorkspaceAvailable:    func() bool { return service.Deps.Workspace != nil },
		BindProjectRootIfSafe: service.bindProjectRootIfSafe,
		SetWriteScope:         service.setWorkspaceWriteScope,
		BindSession: func(sessionID, workspaceID string) {
			service.Deps.Workspace.BindSession(sessionID, workspaceID)
		},
		AnyChatRunning:   service.anyChatRunningLocked,
		UnbindGlobalRoot: service.unbindGlobalProjectRoot,
		UnbindSession: func(sessionID string) {
			service.Deps.Workspace.UnbindSession(sessionID)
		},
		UnbindRuntimeRoot: func(sessionID string) {
			if service.Deps.Runtime != nil {
				service.Deps.Runtime.UnbindProjectRootFor(sessionID)
			}
		},
	})

	service.components.sessionColdActivation = applicationsessioncold.NewActivation(applicationsessioncold.ActivationDeps{
		ClearRestoring: func(sessionID string) {
			service.components.view.ClearRestoringLocked(sessionID)
		},
		ViewIntent: func(sessionID string) uint64 {
			return service.components.view.SessionViewIntentLocked(sessionID)
		},
		TitleFromHistory: func(history []contract.EngineMessage) string {
			return session_runtime.SessionTitleFromHistory(history, displayUserInput)
		},
		SetActive:       service.sessions.SetActive,
		UnitFor:         service.sessionUnitLocked,
		ApplyStoredTier: service.applyStoredPermissionTier,
		SetTitle: func(sessionID string, title model.SessionTitle) {
			service.components.sessions.SetSessionTitleLocked(sessionID, title)
		},
	})

	service.components.sessionColdTask = applicationsessioncold.NewTask(applicationsessioncold.TaskDeps{
		LoadCompactions: func(location session_runtime.Location, sessionID string) ([]model.ContextCompaction, bool, error) {
			return service.components.sessions.LoadSessionCompactionRecords(location, sessionID)
		},
		LatestUser: func(messages []model.Message) string {
			return service.components.sessions.LatestUserContent(messages)
		},
		RetainedFrom: task_context.RetainedFromForCompactions,
		Restore: func(sessionID string, state task_context.RestoredTaskState) {
			service.components.tasks.RestoreSessionTaskLockedFor(sessionID, state)
		},
		Reset: func() {
			service.components.tasks.ResetForNewSessionLocked()
		},
	})

	service.components.sessionColdView = applicationsessioncold.NewView(applicationsessioncold.ViewDeps{
		LoadForkRef: func(location session_runtime.Location, sessionID string) *model.SessionForkRef {
			return service.components.sessionForkLineage.Load(location, sessionID)
		},
		AdvanceSeq: func(sessionID string, messages []model.Message) {
			service.components.view.AdvanceMessageSeqForLocked(sessionID, messages)
		},
		RecordTail: func(record model.SessionRecord, window int) []model.Message {
			return service.components.sessions.RecordConversationTail(record, window)
		},
		ViewEmpty: func(sessionID string) bool {
			return service.components.view.SessionViewEmptyLocked(sessionID)
		},
		SetView: func(sessionID string, view *sessioncase.View) {
			service.components.view.SetSessionViewLocked(sessionID, view)
		},
		AppendHistory: service.appendHistoryLockedFor,
		MutateView: func(sessionID string, update func(*sessioncase.View)) {
			service.components.view.SessionViewMutateLocked(sessionID, update)
		},
		MirrorActiveView: func() {
			service.components.view.MirrorActiveViewLocked()
		},
	})

	service.components.sessionColdPublish = applicationsessioncold.NewPublish(applicationsessioncold.PublishDeps{
		SetGlobalPrompt: func(prompt string) {
			if _, ok := service.Deps.Engine.(interface{ SetSystemPromptFor(string, string) }); !ok {
				service.Deps.Engine.SetSystemPrompt(prompt)
			}
		},
		AttachContext: func(location session_runtime.Location, sessionID string) error {
			return service.components.sessionHistoryRuntime.AttachSessionContextFor(location.WorkspaceID, sessionID)
		},
		SwitchTasks: func(sessionID string, tasks []dto.TaskRecord) {
			service.Deps.Runtime.SwitchSessionTasks(sessionID, tasks)
		},
		ClearSubagentTree: func() {
			_ = service.Deps.Runtime.ClearSubagentTree()
		},
		RestoreAnchors: func(sessionID string) {
			_ = service.Deps.Runtime.RestoreSubagentAnchors(sessionID)
		},
		RefreshRuntime: func(sessionID string) {
			service.components.view.RefreshRuntimeProjectionForSession(sessionID)
		},
		PublishSnapshot: func(revision uint64, sessionID string) {
			service.publishSessionEvent(EventSnapshotChanged, revision, "", sessionID, nil)
		},
		PublishRuntime: service.publishRuntimeProjections,
		RefreshCatalog: func() { service.components.sessions.RequestCatalogRefresh() },
		TouchResident: func(sessionID string) {
			service.components.residencyRuntime.TouchResident(sessionID)
		},
		TouchContent: func(sessionID string) {
			service.components.residencyRuntime.TouchContent(sessionID)
		},
	})

	service.components.sessionColdCommit = applicationsessioncold.NewCommit(applicationsessioncold.CommitDeps{
		AppendPendingTail: service.appendPendingTailNoticeLocked,
		SetSessionChat: func(sessionID string, chat model.ChatState) {
			service.components.view.SetSessionChatLockedFor(sessionID, chat)
		},
		MirrorPlan: func(sessionID string, plan *model.PlanState) {
			service.mirrorPlanProjectionForSessionLocked(sessionID, func() *model.PlanState { return plan })
		},
		SystemPrompt: func() string {
			return service.components.prompts.SystemPromptForActiveTaskLocked()
		},
		ApplyWorkspace: func(_ *model.WorkspaceInfo, projection switchworkspace.Projection) {
			service.applyWorkspaceProjectionLocked(projection)
		},
		BumpRevision: func() uint64 {
			return service.components.view.BumpLocked()
		},
	})
}
