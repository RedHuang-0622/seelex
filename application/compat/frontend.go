// Package compat hosts temporary frontend compatibility adapters that forward
// to already-extracted application components without owning state.
package compat

import (
	"context"

	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	applicationscheduler "github.com/RedHuang-0622/seelex/application/admin/scheduler"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	conversationcontext "github.com/RedHuang-0622/seelex/application/conversation/context"
	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	conversationsearch "github.com/RedHuang-0622/seelex/application/conversation/search"
	"github.com/RedHuang-0622/seelex/application/core"
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	subagentview "github.com/RedHuang-0622/seelex/application/execution/subagentview"
	applicationtaskcontext "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationgoal "github.com/RedHuang-0622/seelex/application/goal"
	applicationhistory "github.com/RedHuang-0622/seelex/application/history"
	applicationinput "github.com/RedHuang-0622/seelex/application/input"
	applicationlifecycle "github.com/RedHuang-0622/seelex/application/lifecycle"
	applicationobservability "github.com/RedHuang-0622/seelex/application/observability"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	applicationprompt "github.com/RedHuang-0622/seelex/application/prompt"
	applicationreference "github.com/RedHuang-0622/seelex/application/reference"
	applicationreview "github.com/RedHuang-0622/seelex/application/review"
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	applicationsessionquery "github.com/RedHuang-0622/seelex/application/session/query"
	view_state "github.com/RedHuang-0622/seelex/application/session/viewstate"
	applicationworkspace "github.com/RedHuang-0622/seelex/application/workspace"
	switchworkspace "github.com/RedHuang-0622/seelex/application/workspace/switch"
	task_context "github.com/RedHuang-0622/seelex/backend/engine/task"
	goaldomain "github.com/RedHuang-0622/seelex/backend/multiagent/goal"
	seelsession "github.com/RedHuang-0622/seelex/backend/multiagent/subagent"
	"github.com/RedHuang-0622/seelex/backend/multiagent/team"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/contract/protocol"
	"github.com/RedHuang-0622/seelex/kernel/board"
	"github.com/RedHuang-0622/seelex/kernel/event"
)

// Frontend keeps the existing frontend method surface while the consumers
// migrate to narrow capability interfaces. State and lifecycle stay in core.
type Frontend struct {
	*core.Frontend
	ports core.FrontendPorts
}

// New builds the compatibility adapter from an assembled core service.
func New(service *core.Service) *Frontend {
	frontend := &Frontend{Frontend: &core.Frontend{Service: service}}
	if service != nil {
		frontend.ports = core.NewFrontendPorts(service)
	}
	return frontend
}

func (frontend *Frontend) admin() *applicationadmin.AccountApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Admin
}

func (frontend *Frontend) completion() *applicationinput.Application {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Completion
}

func (frontend *Frontend) contextCompaction() *conversationcontext.Application {
	if frontend == nil {
		return nil
	}
	return frontend.ports.ContextCompaction
}

func (frontend *Frontend) conversationQueue() *applicationconversation.QueueApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.ConversationQueue
}

func (frontend *Frontend) conversationSession() *applicationconversation.UnifiedApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.ConversationSession
}

func (frontend *Frontend) ConversationView(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationViewRequest) (dto.SessionRecordPage, error) {
	return frontend.conversationSession().View(ctx, ref, request)
}

func (frontend *Frontend) ConversationChat(ctx context.Context, ref dto.ConversationSessionRef, request dto.ConversationChatRequest) (dto.ChatReceipt, error) {
	return frontend.conversationSession().Chat(ctx, ref, request)
}

func (frontend *Frontend) RecoverConversation(ctx context.Context, ref dto.ConversationSessionRef) (dto.SessionRecovery, error) {
	return frontend.conversationSession().Recover(ctx, ref)
}

func (frontend *Frontend) history() *applicationhistory.Application {
	if frontend == nil {
		return nil
	}
	return frontend.ports.History
}

func (frontend *Frontend) execution() *applicationexecution.WorkItemApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Execution
}

func (frontend *Frontend) roleActivity() *applicationexecution.RoleActivityApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.RoleActivity
}

func (frontend *Frontend) HandleRoleToolActivity(activity dto.RoleToolActivity) {
	frontend.roleActivity().Handle(activity)
}

func (frontend *Frontend) historySearch() *conversationsearch.Application {
	if frontend == nil {
		return nil
	}
	return frontend.ports.HistorySearch
}

func (frontend *Frontend) interaction() *applicationsession.InteractionApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Interaction
}

func (frontend *Frontend) fork() *applicationsession.ForkApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Fork
}

func (frontend *Frontend) sessionSubmit() *applicationsession.SubmitApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Submit
}

func (frontend *Frontend) SubmitToSession(ctx context.Context, sessionID, text string) error {
	return frontend.sessionSubmit().SubmitToSession(ctx, sessionID, text)
}

func (frontend *Frontend) ForkSession(parentID string, request model.ForkRequest) (string, error) {
	return frontend.fork().Fork(parentID, request)
}

func (frontend *Frontend) ForkSessionLatest(parentID string) (string, error) {
	return frontend.fork().ForkLatest(parentID)
}

func (frontend *Frontend) ResolveInteraction(ctx context.Context, id, optionID string) error {
	return frontend.interaction().Resolve(ctx, id, optionID)
}

func (frontend *Frontend) submission() *applicationinput.SubmissionApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Submission
}

func (frontend *Frontend) Submit(ctx context.Context, text string) error {
	return frontend.submission().Submit(ctx, text)
}

func (frontend *Frontend) goal() *applicationgoal.Application {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Goal
}

func (frontend *Frontend) lifecycle() *applicationlifecycle.Application {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Lifecycle
}

func (frontend *Frontend) perf() *applicationobservability.PerfApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Perf
}

func (frontend *Frontend) policy() *applicationpolicy.SwitchApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Policy
}

func (frontend *Frontend) projectBoard() *applicationexecution.ProjectBoardApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.ProjectBoard
}

func (frontend *Frontend) reference() *applicationreference.Application {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Reference
}

func (frontend *Frontend) review() *applicationreview.Application {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Review
}

func (frontend *Frontend) scheduler() *applicationscheduler.SchedulerApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Scheduler
}

func (frontend *Frontend) scheduledRun() *applicationscheduler.RunApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.ScheduledRun
}

func (frontend *Frontend) scheduledSession() *applicationscheduler.SessionApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.ScheduledSession
}

func (frontend *Frontend) runtime() *applicationadmin.RuntimeApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Runtime
}

func (frontend *Frontend) tasks() *task_context.Coordinator {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Tasks
}

func (frontend *Frontend) terminalHandler() *applicationtaskcontext.TerminalHandlerApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.TerminalHandler
}

func (frontend *Frontend) TaskTerminalHandler(kind string) func(context.Context, string) (string, error) {
	return frontend.terminalHandler().Handler(kind)
}

func (frontend *Frontend) view() *view_state.Coordinator {
	if frontend == nil {
		return nil
	}
	return frontend.ports.View
}

func (frontend *Frontend) frontendSnapshot() *view_state.SnapshotApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.FrontendSnapshot
}

func (frontend *Frontend) Snapshot() model.Snapshot {
	return frontend.frontendSnapshot().Snapshot()
}

func (frontend *Frontend) subagentView() *subagentview.Coordinator {
	if frontend == nil {
		return nil
	}
	return frontend.ports.SubagentView
}

func (frontend *Frontend) role() *applicationpolicy.RoleApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Role
}

func (frontend *Frontend) roleSessions() contract.RoleSessionsPort {
	if frontend == nil {
		return team.RoleSessions{}
	}
	return frontend.ports.RoleSessions
}

func (frontend *Frontend) workspace() *applicationworkspace.Application {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Workspace
}

func (frontend *Frontend) workspaceUseCase() *switchworkspace.UseCase {
	if frontend == nil {
		return nil
	}
	return frontend.ports.WorkspaceUseCase
}

func (frontend *Frontend) sessionDelete() *applicationsession.DeleteApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.SessionDelete
}

func (frontend *Frontend) sessionArchive() *applicationsession.ArchiveApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.SessionArchive
}

func (frontend *Frontend) sessionDraftEntry() *applicationsession.DraftEntryApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.SessionDraftEntry
}

func (frontend *Frontend) sessionLifecycle() *applicationsession.LifecycleApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.SessionLifecycle
}

func (frontend *Frontend) sessionResume() *applicationsession.ResumeApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.SessionResume
}

func (frontend *Frontend) sessionSSH() *applicationsession.SSHApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.SessionSSH
}

func (frontend *Frontend) editFork() *applicationedit.EditApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.EditFork
}

func (frontend *Frontend) subagent() *applicationexecution.SubagentApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.Subagent
}

func (frontend *Frontend) sessionMeta() *applicationsession.MetaApplication {
	if frontend == nil {
		return nil
	}
	return frontend.ports.SessionMeta
}

func (frontend *Frontend) sessionQuery() *applicationsessionquery.Application {
	if frontend == nil {
		return nil
	}
	return frontend.ports.SessionQuery
}

// ListSessions returns the durable session catalog.
func (frontend *Frontend) ListSessions() []model.SessionInfo {
	return frontend.sessionQuery().ListSessions()
}

// WaitCatalogRefresh waits for the session catalog projection to settle.
func (frontend *Frontend) WaitCatalogRefresh(ctx context.Context) error {
	return frontend.sessionQuery().WaitCatalogRefresh(ctx)
}

// GetSessionTranscript returns a bounded transcript range.
func (frontend *Frontend) GetSessionTranscript(sessionID string, fromSeq, toSeq uint64) ([]model.TranscriptEvent, error) {
	return frontend.sessionQuery().GetSessionTranscript(sessionID, fromSeq, toSeq)
}

// SessionInputIndex returns the durable user-input index.
func (frontend *Frontend) SessionInputIndex(sessionID string) (model.SessionInputIndex, error) {
	return frontend.sessionQuery().SessionInputIndex(sessionID)
}

// SnapshotOf returns one session's durable or resident snapshot.
func (frontend *Frontend) SnapshotOf(sessionID string) (model.SessionSnapshot, error) {
	return frontend.sessionQuery().SnapshotOf(sessionID)
}

// SessionMeta returns display metadata for one session.
func (frontend *Frontend) SessionMeta(sessionID string) (model.SessionMeta, error) {
	return frontend.sessionMeta().SessionMeta(sessionID)
}

// PerfStats returns the current process performance projection.
func (frontend *Frontend) PerfStats() model.PerfStats {
	return frontend.perf().PerfStats()
}

// SearchHistory searches durable conversation history.
func (frontend *Frontend) SearchHistory(ctx context.Context, query string, limit int) (dto.SearchResult, error) {
	return frontend.historySearch().SearchHistory(ctx, query, limit)
}

// ProjectRootFor resolves the project root for a session.
func (frontend *Frontend) ProjectRootFor(sessionID string) string {
	return frontend.projectBoard().ProjectRootFor(sessionID)
}

// WorkTableRowsFor returns the current projection rows for one session.
func (frontend *Frontend) WorkTableRowsFor(sessionID string) []model.WorkItem {
	return frontend.projectBoard().WorkTableRowsFor(sessionID)
}

// TeammateSessionLiveFor returns teammate live rows.
func (frontend *Frontend) TeammateSessionLiveFor(sessionID string) dto.TeammateSessionLiveView {
	return frontend.execution().SessionLive(sessionID)
}

// TeammateSessionLivePageFor returns one bounded teammate live page.
func (frontend *Frontend) TeammateSessionLivePageFor(sessionID string, offset, limit int) dto.TeammateSessionLiveView {
	return frontend.execution().SessionPage(sessionID, offset, limit)
}

// WorkItemExecutionFor returns one work item's execution view.
func (frontend *Frontend) WorkItemExecutionFor(ctx context.Context, mainSessionID, itemID string, page dto.ConversationViewRequest) (dto.WorkItemExecutionView, error) {
	return frontend.execution().WorkItemExecution(ctx, mainSessionID, itemID, page)
}

// TeammateInspectionFor returns a teammate inspection projection.
func (frontend *Frontend) TeammateInspectionFor(ctx context.Context, mainSessionID, role string) (dto.TeammateInspectionView, error) {
	return frontend.execution().TeammateInspection(ctx, mainSessionID, role)
}

// ToolResultContent returns a bounded stored tool result page.
func (frontend *Frontend) ToolResultContent(_ context.Context, resultRef string, offset, limit int) (model.ToolResultPage, error) {
	return frontend.reference().ToolResultContent(resultRef, offset, limit)
}

// CompactContextNow runs one synchronous context compaction.
func (frontend *Frontend) CompactContextNow(ctx context.Context) (conversationcontext.Result, error) {
	return frontend.contextCompaction().CompactContextNow(ctx)
}

// CompactContextHandler adapts the compact_context model tool.
func (frontend *Frontend) CompactContextHandler(ctx context.Context, argsJSON string) (string, error) {
	return frontend.contextCompaction().CompactContextHandler(ctx, argsJSON)
}

// ReadToolResultHandler adapts the read_tool_result model tool.
func (frontend *Frontend) ReadToolResultHandler(_ context.Context, argsJSON string) (string, error) {
	return frontend.reference().ReadToolResultHandler(argsJSON)
}

// ReadPlanHandler adapts the read_plan model tool.
func (frontend *Frontend) ReadPlanHandler(_ context.Context, argsJSON string) (string, error) {
	return frontend.reference().ReadPlanHandler(argsJSON)
}

// ReadCompressedTurnHandler adapts the read_compressed_turn model tool.
func (frontend *Frontend) ReadCompressedTurnHandler(_ context.Context, argsJSON string) (string, error) {
	return frontend.reference().ReadCompressedTurnHandler(argsJSON)
}

// RequestFileReview opens one bounded review batch.
func (frontend *Frontend) RequestFileReview(ctx context.Context, files []dto.ReviewFileRef) (dto.ReviewRequest, error) {
	return frontend.review().RequestFileReview(ctx, files)
}

// GoalGovernanceViewFor returns one session's goal governance projection.
func (frontend *Frontend) GoalGovernanceViewFor(sessionID string) *dto.GoalGovernanceView {
	return frontend.goal().GovernanceViewFor(sessionID)
}

// GoalBeginFor begins a session-scoped goal.
func (frontend *Frontend) GoalBeginFor(ctx context.Context, sessionID string, request goaldomain.BeginRequest) (*goaldomain.GoalRecord, error) {
	return frontend.goal().BeginFor(ctx, sessionID, request)
}

// GoalUpdateFor updates a session-scoped goal.
func (frontend *Frontend) GoalUpdateFor(ctx context.Context, sessionID string, request goaldomain.UpdateRequest) (*goaldomain.GoalRecord, error) {
	return frontend.goal().UpdateFor(ctx, sessionID, request)
}

// GoalDoneFor finishes or aborts a session-scoped goal.
func (frontend *Frontend) GoalDoneFor(ctx context.Context, sessionID string, request goaldomain.FinishRequest, abort bool) (*goaldomain.GoalRecord, error) {
	return frontend.goal().DoneFor(ctx, sessionID, request, abort)
}

// GoalStatusFor returns one session's goal stack and active goal.
func (frontend *Frontend) GoalStatusFor(sessionID string) (goaldomain.StatusView, error) {
	return frontend.goal().StatusFor(sessionID)
}

// ScheduleTask creates one scheduled task.
func (frontend *Frontend) ScheduleTask(ctx context.Context, spec dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error) {
	return frontend.scheduler().ScheduleTask(ctx, spec)
}

// UpdateScheduledTask edits one scheduled task.
func (frontend *Frontend) UpdateScheduledTask(ctx context.Context, id string, spec dto.ScheduledTaskSpec) (*dto.ScheduledTaskStatus, error) {
	return frontend.scheduler().UpdateScheduledTask(ctx, id, spec)
}

// CancelScheduledTask cancels one scheduled task.
func (frontend *Frontend) CancelScheduledTask(id string) error {
	return frontend.scheduler().CancelScheduledTask(id)
}

// AssembleScheduledRun assembles the execution context for a scheduled run.
func (frontend *Frontend) AssembleScheduledRun(ctx context.Context, sessionID string, task dto.ScheduledTaskSpec) (context.Context, error) {
	return frontend.scheduledRun().AssembleScheduledRun(ctx, sessionID, task)
}

// StartScheduledSession starts the session owned by a scheduled task.
func (frontend *Frontend) StartScheduledSession(ctx context.Context, task dto.ScheduledTaskSpec) (string, error) {
	return frontend.scheduledSession().StartScheduledSession(ctx, task)
}

// RefreshRuntimeSnapshot publishes the latest runtime projection.
func (frontend *Frontend) RefreshRuntimeSnapshot() {
	frontend.runtime().Refresh()
}

// AnyChatRunning reports whether any session is running.
func (frontend *Frontend) AnyChatRunning() bool {
	return frontend.lifecycle().AnyChatRunning()
}

// WaitForIdle waits until all active work settles.
func (frontend *Frontend) WaitForIdle(ctx context.Context) error {
	return frontend.lifecycle().WaitForIdle(ctx)
}

// ArchiveSession archives one durable session.
func (frontend *Frontend) ArchiveSession(sessionID string) error {
	return frontend.sessionArchive().Archive(sessionID)
}

// BeginNewSession creates a new draft session.
func (frontend *Frontend) BeginNewSession() error {
	return frontend.sessionDraftEntry().Begin()
}

// UnloadSession releases one resident session.
func (frontend *Frontend) UnloadSession(sessionID string) error {
	return frontend.sessionLifecycle().Unload(sessionID)
}

// ResumeSession resumes or cold-loads one session.
func (frontend *Frontend) ResumeSession(sessionID string) error {
	return frontend.sessionResume().Resume(sessionID)
}

// ActivateSession is the explicit-session alias for ResumeSession.
func (frontend *Frontend) ActivateSession(sessionID string) error {
	return frontend.sessionResume().Resume(sessionID)
}

// SetSessionMeta updates session display metadata.
func (frontend *Frontend) SetSessionMeta(sessionID string, meta model.SessionMeta) error {
	return frontend.sessionMeta().SetSessionMeta(sessionID, meta)
}

// EditUserMessageAndFork atomically edits a user message and forks the session.
func (frontend *Frontend) EditUserMessageAndFork(ctx context.Context, parentID string, request model.EditUserMessageForkRequest) (model.EditUserMessageForkResult, error) {
	return frontend.editFork().EditUserMessageAndFork(ctx, parentID, request)
}

// SessionSSHState returns one session's SSH execution state.
func (frontend *Frontend) SessionSSHState(sessionID string) (model.SessionSSHInfo, error) {
	return frontend.sessionSSH().State(sessionID)
}

// SetSessionSSHAllowed updates the session's allowed SSH environments.
func (frontend *Frontend) SetSessionSSHAllowed(sessionID string, envs []string) (model.SessionSSHInfo, error) {
	return frontend.sessionSSH().SetAllowed(sessionID, envs)
}

// SetSessionSSHEnv updates the session's fixed SSH environment.
func (frontend *Frontend) SetSessionSSHEnv(sessionID, projectID, env string) (model.SessionSSHInfo, error) {
	return frontend.sessionSSH().SetEnv(sessionID, projectID, env)
}

// ActivateSkill activates one skill for the current session.
func (frontend *Frontend) ActivateSkill(name string) (model.SkillInfo, error) {
	return frontend.policy().ActivateSkill(name)
}

// UpdateWorkItemStatus updates a WorkItem's user-visible status.
func (frontend *Frontend) UpdateWorkItemStatus(id, status string) error {
	return frontend.execution().UpdateWorkItemStatus(id, status)
}

// ListSubagentRecovery lists recoverable subagents for a session.
func (frontend *Frontend) ListSubagentRecovery(sessionID string) ([]dto.SubagentRecoveryView, error) {
	return frontend.subagent().ListRecovery(sessionID)
}

// ResumeInterruptedSubagents resumes all interrupted subagents.
func (frontend *Frontend) ResumeInterruptedSubagents(ctx context.Context, sessionID string) (dto.SubagentResumeReport, error) {
	return frontend.subagent().ResumeInterrupted(ctx, sessionID)
}

// ResumeSubagent resumes one interrupted subagent.
func (frontend *Frontend) ResumeSubagent(ctx context.Context, sessionID, nodeID string) (dto.SubagentResumeResult, error) {
	return frontend.subagent().Resume(ctx, sessionID, nodeID)
}

// ForkSubagents forks a bounded subagent set.
func (frontend *Frontend) ForkSubagents(ctx context.Context, sessionID string, specs []dto.SubagentForkSpec) (string, error) {
	return frontend.subagent().Fork(ctx, sessionID, specs)
}

// SubagentSessionDetail returns one subagent detail projection.
func (frontend *Frontend) SubagentSessionDetail(nodeID string) (*model.SubagentDetail, error) {
	return frontend.subagent().Detail(nodeID)
}

// ClearSubagentTree clears failed/finished subagent nodes.
func (frontend *Frontend) ClearSubagentTree() error {
	return frontend.subagent().ClearTree()
}

// SearchHistoryHandler adapts the search_history model tool.
func (frontend *Frontend) SearchHistoryHandler(_ context.Context, argsJSON string) (string, error) {
	return frontend.historySearch().SearchHistoryHandler(argsJSON)
}

// BeginGracefulShutdown stops accepting new work.
func (frontend *Frontend) BeginGracefulShutdown() {
	frontend.lifecycle().BeginGracefulShutdown()
}

// CancelAllChats cancels every running chat.
func (frontend *Frontend) CancelAllChats() {
	frontend.lifecycle().CancelAllChats()
}

// CancelChat cancels one chat request.
func (frontend *Frontend) CancelChat(requestID string) bool {
	return frontend.lifecycle().CancelChat(requestID)
}

// Shutdown closes the application lifecycle.
func (frontend *Frontend) Shutdown() {
	frontend.lifecycle().Shutdown()
}

// SetFullAccess applies the legacy full-access compatibility switch.
func (frontend *Frontend) SetFullAccess(on bool) bool {
	return frontend.policy().SetFullAccess(on)
}

// SetPermissionTier changes the current session permission tier.
func (frontend *Frontend) SetPermissionTier(tier string) (string, error) {
	return frontend.policy().SetPermissionTier(tier)
}

// SetLoopLimit changes the current session loop limit.
func (frontend *Frontend) SetLoopLimit(_ context.Context, limit int) error {
	return frontend.policy().SetLoopLimit(limit)
}

// SetReasoningEffort changes the current session reasoning effort.
func (frontend *Frontend) SetReasoningEffort(_ context.Context, effort string) error {
	return frontend.policy().SetReasoningEffort(effort)
}

// SessionReleaseWindow returns the current release window.
func (frontend *Frontend) SessionReleaseWindow() int {
	return frontend.policy().SessionReleaseWindow()
}

// SetSessionReleaseWindow changes the release window.
func (frontend *Frontend) SetSessionReleaseWindow(_ context.Context, window int) (int, error) {
	return frontend.policy().SetSessionReleaseWindow(window)
}

// Subscribe subscribes to process-level events.
func (frontend *Frontend) Subscribe(buffer int) event.Subscription {
	return frontend.view().Subscribe(buffer)
}

// SubscribeSession subscribes to one session, or to the current view when the
// session ID is empty.
func (frontend *Frontend) SubscribeSession(sessionID string, buffer int) (event.Subscription, error) {
	return frontend.view().SubscribeSession(sessionID, buffer)
}

// SubscribeSessionWithReplay subscribes with a bounded replay window when the
// assembled hub supports replay.
func (frontend *Frontend) SubscribeSessionWithReplay(sessionID string, buffer, replayWindow int) (event.Subscription, error) {
	return frontend.view().SubscribeSessionWithReplay(sessionID, buffer, replayWindow)
}

// PrepareProviderHistory repairs missing provider tool pairs for one session.
func (frontend *Frontend) PrepareProviderHistory(sessionID string) error {
	return frontend.ports.ContextHistory.PrepareProviderHistoryFor(sessionID)
}

// PromptLayers returns the current prompt prefix layers.
func (frontend *Frontend) PromptLayers() []applicationprompt.PromptLayer {
	return frontend.ports.PromptStack.Layers()
}

// SetProjectBoardSink installs the optional project-board write projection.
func (frontend *Frontend) SetProjectBoardSink(sink board.ProjectBoardSink) {
	frontend.ports.SetProjectBoardSink(sink)
}

// SetToolRecordEnabled switches the ledger after persisting the setting.
func (frontend *Frontend) SetToolRecordEnabled(enabled bool) (bool, error) {
	return frontend.ports.ToolRecords.SetEnabled(enabled)
}

// ApplyToolRecordEnabled installs the startup value without rewriting storage.
func (frontend *Frontend) ApplyToolRecordEnabled(enabled bool) {
	frontend.ports.ToolRecords.ApplyEnabled(enabled)
}

// ToolRecordEnabled reports the live ledger switch.
func (frontend *Frontend) ToolRecordEnabled() bool {
	return frontend.ports.ToolRecords.Enabled()
}

// FlushToolRecord writes one dirty session after draining leaf stages.
func (frontend *Frontend) FlushToolRecord(sessionID string) {
	frontend.ports.ToolRecords.Flush(sessionID)
}

// SetToolRecordSnapshotStore injects the per-session snapshot port.
func (frontend *Frontend) SetToolRecordSnapshotStore(store contract.ToolRecordSnapshotStore) {
	frontend.ports.ToolRecords.SetSnapshotStore(store)
}

// ToolRecordBoard returns the current-view session projection.
func (frontend *Frontend) ToolRecordBoard() dto.ToolRecordBoard {
	return frontend.ports.ToolRecords.Board()
}

// RecordToolCommandOutcome records the command-exit-code success criterion.
func (frontend *Frontend) RecordToolCommandOutcome(sessionID, tool string, exitCode int) {
	frontend.ports.ToolRecords.RecordCommandOutcome(sessionID, tool, exitCode)
}

// GoalSkillActive reports whether the latest goal has active skill state.
func (frontend *Frontend) GoalSkillActive() bool {
	return frontend.tasks().GoalSkillActive()
}

// HandleSubagentToolEventFor projects an attributed subagent tool event.
func (frontend *Frontend) HandleSubagentToolEventFor(ownerSessionID string, toolEvent seelsession.SubagentToolEvent) {
	frontend.subagentView().HandleSubagentToolEventFor(ownerSessionID, toolEvent)
}

// AddNotice appends one user-visible system notice.
func (frontend *Frontend) AddNotice(notice string) {
	if frontend != nil && frontend.ports.AddNotice != nil {
		frontend.ports.AddNotice(notice)
	}
}

// PublishRuntimeProjections refreshes runtime projections.
func (frontend *Frontend) PublishRuntimeProjections() {
	if frontend != nil && frontend.ports.PublishRuntime != nil {
		frontend.ports.PublishRuntime()
	}
}

// CloseRoleToolRecordUnit closes one role tool record unit.
func (frontend *Frontend) CloseRoleToolRecordUnit(mainSessionID, roleSessionID string) {
	if frontend != nil && frontend.ports.CloseRoleToolRecord != nil {
		frontend.ports.CloseRoleToolRecord(mainSessionID, roleSessionID)
	}
}

// AddAccount forwards to the account application.
func (frontend *Frontend) AddAccount(ctx context.Context, draft dto.AccountDraft) (dto.AccountAddResult, error) {
	return frontend.admin().AddAccount(ctx, draft)
}

// AccountDetails forwards to the account application.
func (frontend *Frontend) AccountDetails(name string) (dto.AccountDetails, error) {
	return frontend.admin().AccountDetails(name)
}

// UpdateAccount forwards to the account application.
func (frontend *Frontend) UpdateAccount(update dto.AccountUpdate) (dto.AccountDetails, error) {
	return frontend.admin().UpdateAccount(update)
}

// DeleteAccount forwards to the account application.
func (frontend *Frontend) DeleteAccount(name string) (dto.AccountDetails, error) {
	return frontend.admin().DeleteAccount(name)
}

// AccountBalance forwards to the account application.
func (frontend *Frontend) AccountBalance(ctx context.Context, name string) (dto.AccountBalance, error) {
	return frontend.admin().AccountBalance(ctx, name)
}

// SetAccountWindow forwards to the account application.
func (frontend *Frontend) SetAccountWindow(_ context.Context, name string, window int) (int, error) {
	return frontend.admin().SetAccountWindow(context.Background(), name, window)
}

// Suggestions returns command, skill, and plugin suggestions.
func (frontend *Frontend) Suggestions(input string) []applicationinput.Suggestion {
	return frontend.completion().Suggestions(input)
}

// SelectAccount switches the active account.
func (frontend *Frontend) SelectAccount(ctx context.Context, name string) error {
	return frontend.admin().SelectAccount(context.Background(), name)
}

// SwitchEffort changes the current effort level.
func (frontend *Frontend) SwitchEffort(_ context.Context, level string) error {
	return frontend.policy().SwitchEffort(level)
}

// SwitchPlugin activates or disables a plugin.
func (frontend *Frontend) SwitchPlugin(ctx context.Context, name string) error {
	return frontend.policy().SwitchPlugin(ctx, name)
}

// LoadMoreHistory prepends an older history window.
func (frontend *Frontend) LoadMoreHistory(limit int) error {
	return frontend.history().LoadMoreHistory(limit)
}

// LoadLatestHistory returns the visible window to the tail.
func (frontend *Frontend) LoadLatestHistory() error {
	return frontend.history().LoadLatestHistory()
}

// ReorderQueuedInput moves one queued input.
func (frontend *Frontend) ReorderQueuedInput(sessionID string, from, to int) error {
	return frontend.conversationQueue().ReorderQueuedInput(sessionID, from, to)
}

// RecallQueuedInput removes one queued input and returns its text.
func (frontend *Frontend) RecallQueuedInput(sessionID string, index int) (string, error) {
	return frontend.conversationQueue().RecallQueuedInput(sessionID, index)
}

// RoleDefaultVector reads a role's explicit default permission vector.
func (frontend *Frontend) RoleDefaultVector(sessionID, role string) ([]string, bool, error) {
	return frontend.role().RoleDefaultVector(sessionID, role)
}

// SetRoleDefaultVector writes a role's default permission vector.
func (frontend *Frontend) SetRoleDefaultVector(sessionID, role string, vector []string) error {
	return frontend.role().SetRoleDefaultVector(sessionID, role, vector)
}

// RoleAccount reads a role's explicit default account.
func (frontend *Frontend) RoleAccount(sessionID, role string) (string, bool, error) {
	return frontend.role().RoleAccount(sessionID, role)
}

// SetRoleAccount applies and persists a role's default account.
func (frontend *Frontend) SetRoleAccount(sessionID, role, account string) error {
	return frontend.role().SetRoleAccount(sessionID, role, account)
}

// RoleInheritMainAgent reports whether a role inherits main-agent settings.
func (frontend *Frontend) RoleInheritMainAgent(sessionID, role string) (bool, error) {
	return frontend.role().RoleInheritMainAgent(sessionID, role)
}

// SetRoleInheritMainAgent updates role inheritance.
func (frontend *Frontend) SetRoleInheritMainAgent(sessionID, role string, inherit bool) error {
	return frontend.role().SetRoleInheritMainAgent(sessionID, role, inherit)
}

// RoleSSHUserSpaces reads a role's effective SSH path scope.
func (frontend *Frontend) RoleSSHUserSpaces(sessionID, role string) ([]string, error) {
	return frontend.role().RoleSSHUserSpaces(sessionID, role)
}

// RoleDefaultSSHUserSpaces reads a role's default SSH path scope.
func (frontend *Frontend) RoleDefaultSSHUserSpaces(sessionID, role string) ([]string, error) {
	return frontend.role().RoleDefaultSSHUserSpaces(sessionID, role)
}

// SetRoleSSHUserSpaces writes a role's default SSH path scope.
func (frontend *Frontend) SetRoleSSHUserSpaces(sessionID, role string, paths []string) error {
	return frontend.role().SetRoleSSHUserSpaces(sessionID, role, paths)
}

// SetRoleSessionSSHUserSpaces writes a session-local SSH path scope.
func (frontend *Frontend) SetRoleSessionSSHUserSpaces(sessionID, role string, paths []string) error {
	return frontend.role().SetRoleSessionSSHUserSpaces(sessionID, role, paths)
}

// RolePermissionVector reads one effective role permission vector.
func (frontend *Frontend) RolePermissionVector(sessionID, role string) ([]string, error) {
	return frontend.role().RolePermissionVector(sessionID, role)
}

// SetRolePermissionVector writes a session-local role permission vector.
func (frontend *Frontend) SetRolePermissionVector(sessionID, role string, vector []string) error {
	return frontend.role().SetRolePermissionVector(sessionID, role, vector)
}

func (frontend *Frontend) CreateRoleSession(mainSessionID, roleName, roleSessionID string, joinSeq uint64) (dto.RoleSessionInfo, error) {
	return frontend.roleSessions().CreateRoleSession(mainSessionID, roleName, roleSessionID, joinSeq)
}

func (frontend *Frontend) AppendRoleDraft(mainSessionID, roleName, roleSessionID string, rows []dto.RoleDraftRow) error {
	return frontend.roleSessions().AppendRoleDraft(mainSessionID, roleName, roleSessionID, rows)
}

func (frontend *Frontend) ReadRoleDraft(mainSessionID, roleName, roleSessionID string) ([]dto.RoleDraftRow, error) {
	return frontend.roleSessions().ReadRoleDraft(mainSessionID, roleName, roleSessionID)
}

func (frontend *Frontend) SyncRoleDraft(mainSessionID, roleName, roleSessionID string, order []string) (dto.RoleDraftSyncResult, error) {
	return frontend.roleSessions().SyncRoleDraft(mainSessionID, roleName, roleSessionID, order)
}

func (frontend *Frontend) AppendRoleSessionRows(mainSessionID, roleName, roleSessionID string, rows []dto.RoleRow) error {
	return frontend.roleSessions().AppendRoleSessionRows(mainSessionID, roleName, roleSessionID, rows)
}

func (frontend *Frontend) ReadRoleSessionRows(mainSessionID, roleName, roleSessionID string) ([]dto.RoleRow, error) {
	return frontend.roleSessions().ReadRoleSessionRows(mainSessionID, roleName, roleSessionID)
}

func (frontend *Frontend) RoleSnapshot(mainSessionID, roleName, roleSessionID string) (dto.RoleSnapshot, error) {
	return frontend.roleSessions().RoleSnapshot(mainSessionID, roleName, roleSessionID)
}

func (frontend *Frontend) AssembleRoleWire(mainSessionID, roleName, roleSessionID string, budget, k int) (dto.RoleWireSnapshot, error) {
	return frontend.roleSessions().AssembleRoleWire(mainSessionID, roleName, roleSessionID, budget, k)
}

func (frontend *Frontend) SetLifecycleOrder(sessionID, policy string, roles []string) error {
	return frontend.roleSessions().SetLifecycleOrder(sessionID, policy, roles)
}

func (frontend *Frontend) SetRoleLifecycle(mainSessionID, roleName, roleSessionID string, joinSeq uint64, ref *dto.CompactFrameRef) error {
	return frontend.roleSessions().SetRoleLifecycle(mainSessionID, roleName, roleSessionID, joinSeq, ref)
}

func (frontend *Frontend) ListRoleSessions(mainSessionID string) ([]string, error) {
	return frontend.roleSessions().ListRoleSessions(mainSessionID)
}

func (frontend *Frontend) ScheduleRegister(sessionID string, payload dto.ScheduleEventPayload) error {
	return frontend.roleSessions().ScheduleRegister(sessionID, payload)
}

func (frontend *Frontend) ScheduleCancel(sessionID string, payload dto.ScheduleEventPayload) error {
	return frontend.roleSessions().ScheduleCancel(sessionID, payload)
}

func (frontend *Frontend) ScheduleFire(sessionID string, payload dto.ScheduleEventPayload) error {
	return frontend.roleSessions().ScheduleFire(sessionID, payload)
}

// WorkspaceEnvironments reads a project's environment table and default name.
func (frontend *Frontend) WorkspaceEnvironments(projectID string) ([]model.WorkspaceEnvironmentInfo, string, error) {
	return frontend.workspace().WorkspaceEnvironments(projectID)
}

// SetWorkspaceEnvironment writes one project environment.
func (frontend *Frontend) SetWorkspaceEnvironment(projectID, name string, change model.WorkspaceEnvironmentChange) (model.WorkspaceEnvironmentInfo, error) {
	return frontend.workspace().SetWorkspaceEnvironment(projectID, name, change)
}

// DeleteWorkspaceEnvironment removes one project environment.
func (frontend *Frontend) DeleteWorkspaceEnvironment(projectID, name string) error {
	return frontend.workspace().DeleteWorkspaceEnvironment(projectID, name)
}

// SetDefaultWorkspaceEnvironment selects or clears a project's default.
func (frontend *Frontend) SetDefaultWorkspaceEnvironment(projectID, name string) error {
	return frontend.workspace().SetDefaultWorkspaceEnvironment(projectID, name)
}

// WorkspaceGitRepos discovers git repositories under the current workspace.
func (frontend *Frontend) WorkspaceGitRepos() (dto.GitReposResult, error) {
	return frontend.workspace().WorkspaceGitRepos()
}

// DeleteSession deletes a session record and runs release.
func (frontend *Frontend) DeleteSession(sessionID string) error {
	return frontend.sessionDelete().Delete(sessionID)
}

// CreateWorkspace creates a workspace and binds it to the current view.
func (frontend *Frontend) CreateWorkspace(name, rootPath, gitRemote string) error {
	return frontend.workspaceUseCase().CreateWorkspace(name, rootPath, gitRemote)
}

// CreateWorkspaces registers projects without binding the current session.
func (frontend *Frontend) CreateWorkspaces(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error) {
	return frontend.workspace().CreateWorkspaces(requests)
}

// BindWorkspace binds a workspace to the current view session or draft.
func (frontend *Frontend) BindWorkspace(workspaceID string) error {
	return frontend.workspaceUseCase().BindWorkspace(workspaceID)
}

// UnbindWorkspace removes the current view session's workspace binding.
func (frontend *Frontend) UnbindWorkspace() {
	frontend.workspaceUseCase().UnbindWorkspace()
}

// WorkspaceTree lists children of one directory.
func (frontend *Frontend) WorkspaceTree(relPath string, depth int) (dto.TreeListing, error) {
	return frontend.workspaceUseCase().Tree(relPath, depth)
}

// WorkspaceFileCount counts visible files and directories.
func (frontend *Frontend) WorkspaceFileCount() (dto.TreeCount, error) {
	return frontend.workspaceUseCase().FileCount()
}

// WorkspaceGitLog returns recent commits for one repository.
func (frontend *Frontend) WorkspaceGitLog(repo string, limit int) (dto.GitLogResult, error) {
	return frontend.workspace().GitLog(repo, limit)
}

// WorkspaceChanges returns uncommitted changes for one repository.
func (frontend *Frontend) WorkspaceChanges(repo string, limit int) (dto.WorkspaceChangesResult, error) {
	return frontend.workspace().Changes(repo, limit)
}

// WorkspaceGitCommitDetail returns the changed-file summary of one commit.
func (frontend *Frontend) WorkspaceGitCommitDetail(repo, hash string, limit int) (dto.GitCommitDetail, error) {
	return frontend.workspace().CommitDetail(repo, hash, limit)
}

// WorkspaceFileContent reads controlled bytes from the current workspace.
func (frontend *Frontend) WorkspaceFileContent(relPath string, limit int64) (dto.FileContent, error) {
	return frontend.workspaceUseCase().FileContent(relPath, limit)
}

// WorkspaceGitCommitFileContent reads one file from a commit.
func (frontend *Frontend) WorkspaceGitCommitFileContent(repo, hash, relPath string, limit int64) (dto.FileContent, error) {
	return frontend.workspace().CommitFileContent(repo, hash, relPath, limit)
}

// WorkspaceWriteFile overwrites one workspace file.
func (frontend *Frontend) WorkspaceWriteFile(relPath, content string) (dto.FileWriteResult, error) {
	return frontend.workspaceUseCase().WriteFile(relPath, content)
}
