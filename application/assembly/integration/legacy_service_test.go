package integration

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	applicationscheduler "github.com/RedHuang-0622/seelex/application/admin/scheduler"
	assembly "github.com/RedHuang-0622/seelex/application/assembly"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	conversationcontext "github.com/RedHuang-0622/seelex/application/conversation/context"
	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	subagentview "github.com/RedHuang-0622/seelex/application/execution/subagentview"
	applicationtable "github.com/RedHuang-0622/seelex/application/execution/table"
	taskcontext "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationtoolhooks "github.com/RedHuang-0622/seelex/application/execution/toolhooks"
	applicationtoolrecord "github.com/RedHuang-0622/seelex/application/execution/toolrecord"
	applicationgoal "github.com/RedHuang-0622/seelex/application/goal"
	applicationhistory "github.com/RedHuang-0622/seelex/application/history"
	applicationinput "github.com/RedHuang-0622/seelex/application/input"
	applicationlifecycle "github.com/RedHuang-0622/seelex/application/lifecycle"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	applicationprompt "github.com/RedHuang-0622/seelex/application/prompt"
	applicationreference "github.com/RedHuang-0622/seelex/application/reference"
	applicationreview "github.com/RedHuang-0622/seelex/application/review"
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	applicationsessionhistory "github.com/RedHuang-0622/seelex/application/session/history"
	applicationsessionquery "github.com/RedHuang-0622/seelex/application/session/query"
	sessionresidency "github.com/RedHuang-0622/seelex/application/session/residency"
	sessionruntime "github.com/RedHuang-0622/seelex/application/session/runtime"
	sessionstate "github.com/RedHuang-0622/seelex/application/session/state"
	viewstate "github.com/RedHuang-0622/seelex/application/session/viewstate"
	applicationworkspace "github.com/RedHuang-0622/seelex/application/workspace"
	switchworkspace "github.com/RedHuang-0622/seelex/application/workspace/switch"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/contract/dto"
	protocol "github.com/RedHuang-0622/seelex/contract/protocol"
	kernelevent "github.com/RedHuang-0622/seelex/kernel/event"
	switching "github.com/RedHuang-0622/seelex/kernel/sessionpolicy"
)

var draftSnapshotInterval = 5 * time.Second

type legacyTestService struct {
	*assembly.Service
	commands      contract.CommandRegistryPort
	draft         *sessionstate.DraftSlot
	effortManager *applicationprompt.EffortManager
	knobs         *applicationpolicy.Knobs
	promptStack   *applicationprompt.PromptStack
	sessions      *sessionstate.Domain
	toolRecords   *applicationtoolrecord.Application
	components    legacyComponents
}

type legacyDraftComponents struct {
	entry           *applicationsession.DraftEntryApplication
	submission      *applicationsession.DraftSubmissionApplication
	materialization *applicationsession.DraftMaterializationApplication
}

type legacyConversationTurnComponents struct {
	projection *applicationtoolhooks.ProjectionApplication
	start      *applicationconversation.TurnStartApplication
}

type legacyInteractionComponents struct {
	resolve *applicationsession.InteractionApplication
	observe *applicationsession.InteractionObserverApplication
}

type legacyEditForkComponents struct {
	application *applicationedit.EditApplication
	annotation  *applicationedit.AnnotationApplication
}

type legacyComponents struct {
	accountApplication           *applicationadmin.AccountApplication
	completionApplication        *applicationinput.Application
	conversationHistoryQuery     *applicationhistory.Application
	conversationQueue            *applicationconversation.QueueApplication
	conversationStreamRuntime    *applicationconversation.StreamRuntime
	conversationTurn             legacyConversationTurnComponents
	commands                     contract.CommandRegistryPort
	editFork                     legacyEditForkComponents
	frontendSnapshot             *viewstate.SnapshotApplication
	faultGuardApplication        *applicationlifecycle.FaultGuardApplication
	forkApplication              *applicationsession.ForkApplication
	goal                         *applicationgoal.Coordinator
	goalApplication              *applicationgoal.Application
	history                      *conversationcontext.EngineHistoryCoordinator
	input                        contract.InputDispatcher
	interactions                 legacyInteractionComponents
	lifecycleApplication         *applicationlifecycle.Application
	policyApplication            *applicationpolicy.SwitchApplication
	projectBoard                 *assembly.ProjectBoardSinkPort
	prompts                      *applicationprompt.Coordinator
	promptStack                  *applicationprompt.PromptStack
	projectBoardQuery            *applicationexecution.ProjectBoardApplication
	providerRecovery             *conversationcontext.RecoveryApplication
	referenceApplication         *applicationreference.Application
	reviewApplication            *applicationreview.Application
	roleActivity                 *applicationexecution.RoleActivityApplication
	roleApplication              *applicationpolicy.RoleApplication
	residencyRuntime             *sessionresidency.Runtime
	scheduledRunApplication      *applicationscheduler.RunApplication
	scheduledSessionApplication  *applicationscheduler.SessionApplication
	schedulerApplication         *applicationscheduler.SchedulerApplication
	sessionArchive               *applicationsession.ArchiveApplication
	sessionDelete                *applicationsession.DeleteApplication
	sessionDrafts                legacyDraftComponents
	sessionHistoryRuntime        *applicationsessionhistory.Runtime
	sessionLifecycle             *applicationsession.LifecycleApplication
	sessionMetaApplication       *applicationsession.MetaApplication
	sessionQuery                 *applicationsessionquery.Application
	sessionReleaseApplication    *applicationsession.ReleaseApplication
	sessionResume                *applicationsession.ResumeApplication
	sessionSSHApplication        *applicationsession.SSHApplication
	sessions                     *sessionruntime.Coordinator
	sessionsDomain               *sessionstate.Domain
	subagent                     *subagentview.Coordinator
	subagentApplication          *applicationexecution.SubagentApplication
	submission                   *applicationinput.SubmissionApplication
	submitApplication            *applicationsession.SubmitApplication
	tasks                        *taskcontext.Coordinator
	terminalHandler              *taskcontext.TerminalHandlerApplication
	view                         *viewstate.Coordinator
	workTableRuntime             *applicationtable.Runtime
	workspace                    *switchworkspace.UseCase
	workspaceApplication         *applicationworkspace.Application
	context                      *conversationcontext.EngineCoordinator
	contextCompactionApplication *conversationcontext.Application
	contextCompactionGate        *conversationcontext.CompactionGate
}

func newLegacyTestService(t testing.TB, engine contract.ChatEngine, options ...testServiceOption) *legacyTestService {
	t.Helper()
	return legacyServiceFor(newTestService(t, engine, options...))
}

func mustNewLegacy(t testing.TB, deps contract.Dependencies) *legacyTestService {
	t.Helper()
	return legacyServiceFor(mustNew(t, deps))
}

func legacyServiceFor(service *assembly.Service) *legacyTestService {
	components := assembly.ComponentsFor(service)
	return &legacyTestService{
		Service:       service,
		commands:      components.Runtime.Commands,
		effortManager: components.Policy.EffortManager,
		knobs:         components.Policy.Knobs,
		promptStack:   components.Conversation.PromptStack,
		sessions:      components.Session.Domain,
		toolRecords:   components.Execution.ToolRecords,
		components: legacyComponents{
			accountApplication:        components.Admin.Account,
			completionApplication:     components.Conversation.Completion,
			conversationHistoryQuery:  components.Conversation.History,
			conversationQueue:         components.Conversation.Queue,
			conversationStreamRuntime: components.Conversation.Stream,
			conversationTurn: legacyConversationTurnComponents{
				projection: components.Conversation.TurnProjection,
				start:      components.Conversation.TurnStart,
			},
			commands:                     components.Runtime.Commands,
			context:                      components.Conversation.Context,
			contextCompactionApplication: components.Conversation.Compaction,
			contextCompactionGate:        components.Conversation.CompactionGate,
			editFork: legacyEditForkComponents{
				application: components.Conversation.EditFork,
				annotation:  components.Conversation.Annotation,
			},
			faultGuardApplication:       components.Runtime.FaultGuard,
			forkApplication:             components.Session.Fork,
			frontendSnapshot:            components.Session.Snapshot,
			goal:                        components.Admin.GoalRuntime,
			goalApplication:             components.Admin.Goal,
			history:                     components.Conversation.ContextHistory,
			input:                       components.Conversation.Input,
			interactions:                legacyInteractionComponents{resolve: components.Session.Interaction, observe: components.Session.InteractionObserver},
			lifecycleApplication:        components.Runtime.Lifecycle,
			policyApplication:           components.Policy.Switch,
			projectBoard:                components.Execution.ProjectBoardSink,
			prompts:                     components.Conversation.Prompts,
			promptStack:                 components.Conversation.PromptStack,
			projectBoardQuery:           components.Execution.ProjectBoard,
			providerRecovery:            components.Conversation.ProviderRecovery,
			referenceApplication:        components.Conversation.Reference,
			reviewApplication:           components.Admin.Review,
			roleActivity:                components.Execution.RoleActivity,
			roleApplication:             components.Policy.Role,
			residencyRuntime:            components.Session.Residency,
			scheduledRunApplication:     components.Runtime.ScheduledRun,
			scheduledSessionApplication: components.Runtime.ScheduledSession,
			schedulerApplication:        components.Runtime.Scheduler,
			sessionArchive:              components.Session.Archive,
			sessionDelete:               components.Session.Delete,
			sessionDrafts: legacyDraftComponents{
				entry:           components.Session.DraftEntry,
				submission:      components.Session.DraftSubmission,
				materialization: components.Session.DraftMaterialization,
			},
			sessionHistoryRuntime:     components.Session.HistoryRuntime,
			sessionLifecycle:          components.Session.Lifecycle,
			sessionMetaApplication:    components.Session.Meta,
			sessionQuery:              components.Session.Query,
			sessionReleaseApplication: components.Session.Release,
			sessionResume:             components.Session.Resume,
			sessionSSHApplication:     components.Session.SSH,
			sessions:                  components.Session.Runtime,
			sessionsDomain:            components.Session.Domain,
			subagent:                  components.Execution.SubagentView,
			subagentApplication:       components.Execution.Subagent,
			submission:                components.Conversation.Submission,
			submitApplication:         components.Session.Submit,
			tasks:                     components.Execution.Tasks,
			terminalHandler:           components.Execution.TerminalHandler,
			view:                      components.Session.View,
			workTableRuntime:          components.Execution.WorkTable,
			workspace:                 components.Workspace.Switch,
			workspaceApplication:      components.Workspace.Projects,
		},
	}
}

func (service *legacyTestService) replaceEngineHistory(sessionID string, history []contract.EngineMessage) error {
	if routed, ok := contract.RoutedSessionEngine(service.Deps.Engine); ok {
		return routed.ReplaceHistoryFor(sessionID, history)
	}
	return service.Deps.Engine.ReplaceHistory(sessionID, history)
}

func (service *legacyTestService) reapplyEffortAfterPluginSwitch() {
	if service.effortManager == nil {
		return
	}
	_ = service.effortManager.Apply(service.effortManager.Current())
	service.knobs.ApplyLoopLimit(service.knobs.CurrentLoopLimit())
}

func (service *legacyTestService) submitCommand(ctx context.Context, input string) error {
	trimmed := strings.TrimSpace(strings.TrimPrefix(input, "/"))
	if trimmed == "" {
		return nil
	}
	parts := strings.Fields(trimmed)
	if _, ok := service.Deps.Skills.Get(parts[0]); ok {
		return nil
	}
	command, ok := service.commands.Get(parts[0])
	if !ok {
		service.components.view.AddNotice(service.components.completionApplication.UnknownCommandNotice(parts[0]))
		return nil
	}
	result, err := command.Execute(ctx, parts[1:])
	if err != nil {
		service.components.view.AddNotice(err.Error())
		return err
	}
	if result.Notice != "" {
		service.components.view.AddNotice(result.Notice)
	}
	return nil
}

func (service *legacyTestService) prepareCompletedTaskBoundary() {
	service.ViewMu.RLock()
	terminal := false
	if !service.Core.Snapshot.Chat.Running {
		terminal = service.Core.Snapshot.Task != nil &&
			(service.Core.Snapshot.Task.Status == protocol.TurnCompleted || service.Core.Snapshot.Task.Status == protocol.TurnFailed)
		if state := service.components.tasks.CurrentTaskExecution(); state != nil {
			terminal = terminal || state.Status == protocol.TurnCompleted || state.Status == protocol.TurnFailed
		}
	}
	service.ViewMu.RUnlock()
	if terminal {
		service.promptStack.ClearKind("skill")
		service.knobs.ApplyLoopLimit(service.knobs.CurrentLoopLimit())
	}
}

func selectedSkillLayers(layers []applicationprompt.PromptLayer) []applicationprompt.PromptLayer {
	return applicationconversation.SelectedSkillLayers(layers)
}

func (service *legacyTestService) appendHistoryLocked(history []contract.EngineMessage) {
	service.appendHistoryLockedFor(service.Core.Snapshot.Session.ID, history)
}

func (service *legacyTestService) appendHistoryLockedFor(sessionID string, history []contract.EngineMessage) {
	for _, historyMessage := range history {
		if !service.components.sessionHistoryRuntime.IsVisibleHistoryMessage(historyMessage) {
			continue
		}
		origin := protocol.MessageOrigin{RoleName: protocol.RoleNameMain, RoleSessionID: sessionID}
		if historyMessage.Role == "user" {
			origin.RoleName = protocol.RoleNameUser
		}
		if historyMessage.Role != "tool" && historyMessage.Content != "" && !conversationcontext.IsProviderOnlyHistoryContent(historyMessage.Content) {
			content := historyMessage.Content
			if historyMessage.Role == "user" {
				content = assembly.DisplayUserInput(content)
			}
			appended := service.components.view.AppendMessageWithOriginLockedFor(sessionID, historyMessage.Role, content, nil, origin)
			if historyMessage.ReasoningContent != "" && appended != nil {
				appended.ReasoningContent = historyMessage.ReasoningContent
			}
		}
		for _, call := range historyMessage.ToolCalls {
			service.components.view.AppendMessageWithOriginLockedFor(sessionID, "tool", "", &protocol.ToolCall{
				ID: call.ID, Name: call.Name, Arguments: call.Arguments, Status: dto.ToolEventSuccess,
			}, origin)
		}
		if historyMessage.Role == "tool" {
			visible, ref, truncated, totalChars := service.components.frontendSnapshot.ToolResultBudget().Bound(historyMessage.Name, historyMessage.Content)
			service.components.view.AppendMessageWithOriginLockedFor(sessionID, "tool_result", visible, &protocol.ToolCall{
				ID: historyMessage.ToolCallID, Name: historyMessage.Name,
				Result: visible, Status: dto.ToolEventSuccess,
				ResultRef: ref, Truncated: truncated, TotalChars: totalChars,
			}, origin)
		}
	}
}

func (service *legacyTestService) startDraftSnapshotLoop(ctx context.Context, sessionID, requestID string) func() {
	if requestID == "" {
		return func() {}
	}
	left, found := service.draftSnapshotBaseSeq(sessionID, requestID)
	if !found {
		return func() {}
	}
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(draftSnapshotInterval)
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-ticker.C:
				next, supported, err := service.persistDraftSnapshotSuffix(sessionID, left)
				if err != nil {
					log.Printf("[draft-snapshot] session=%s request=%s: %v", sessionID, requestID, err)
					continue
				}
				if !supported {
					return
				}
				left = next
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
}

func (service *legacyTestService) draftSnapshotBaseSeq(sessionID, requestID string) (uint64, bool) {
	for _, event := range service.transcriptSnapshot(sessionID) {
		if event.TaskID == requestID && event.Seq > 0 {
			return event.Seq - 1, true
		}
	}
	return 0, false
}

func (service *legacyTestService) persistDraftSnapshotSuffix(sessionID string, left uint64) (uint64, bool, error) {
	events := service.transcriptSnapshot(sessionID)
	suffix := make([]protocol.TranscriptEvent, 0)
	right := left
	for _, event := range events {
		if event.Seq <= left {
			continue
		}
		suffix = append(suffix, event)
		if event.Seq > right {
			right = event.Seq
		}
	}
	if len(suffix) == 0 {
		return left, true, nil
	}
	location := service.components.sessions.LocateSession(sessionID)
	_, supported, err := service.components.sessions.AppendDraftSnapshot(location, sessionID, suffix)
	if err != nil || !supported {
		return left, supported, err
	}
	return right, true, nil
}

func (service *legacyTestService) transcriptSnapshot(sessionID string) []protocol.TranscriptEvent {
	events := service.components.tasks.TranscriptFor(sessionID)
	return append([]protocol.TranscriptEvent(nil), events...)
}

func (service *legacyTestService) currentViewSessionID() string {
	service.ViewMu.RLock()
	defer service.ViewMu.RUnlock()
	return service.Core.Snapshot.Session.ID
}

func (service *legacyTestService) effortForSession(sessionID string) string {
	if unit := service.sessions.Unit(sessionID); unit != nil {
		if level := unit.EffortLevel(); level != "" {
			return level
		}
	}
	return service.effortManager.Current()
}

func (service *legacyTestService) syncSubagentPolicyFor(sessionID string) {
	if service.Deps.Runtime == nil {
		return
	}
	service.Deps.Runtime.SetSubagentPolicyFor(sessionID, applicationprompt.SubagentPolicyFor(service.effortForSession(sessionID)))
}

func (service *legacyTestService) permissionTierForSession(sessionID string) string {
	if unit := service.sessions.Unit(sessionID); unit != nil {
		if tier, ok := unit.PermissionTier(); ok && tier != "" {
			return tier
		}
	}
	if tier := service.Deps.Runtime.PermissionTier(); tier != "" {
		return tier
	}
	return dto.PermissionTierManual
}

func (service *legacyTestService) fullAccessForSession(sessionID string) bool {
	return service.permissionTierForSession(sessionID) == dto.PermissionTierFull
}

func (service *legacyTestService) syncFullAccessFor(sessionID string) {
	if service.Deps.Runtime != nil {
		service.Deps.Runtime.SetFullAccessFor(sessionID, service.fullAccessForSession(sessionID))
	}
}

func (service *legacyTestService) readStoredPermissionTier(sessionID string) string {
	if port, ok := service.Deps.Sessions.(interface {
		SessionPermissionTier(string) (string, error)
	}); ok {
		tier, err := port.SessionPermissionTier(sessionID)
		if err == nil {
			return tier
		}
	}
	return ""
}

func (service *legacyTestService) applyStoredPermissionTier(sessionID, tier string) {
	if unit := service.sessions.Unit(sessionID); unit != nil {
		unit.SetPermissionTier(tier)
	}
	if service.Deps.Runtime != nil {
		_ = service.Deps.Runtime.SetPermissionTierFor(sessionID, tier)
	}
	if service.Core.Snapshot.Session.ID == sessionID {
		service.Core.Snapshot.Runtime.PermissionTier = tier
		service.Core.Snapshot.Runtime.FullAccess = tier == dto.PermissionTierFull
	}
}

func (service *legacyTestService) perSessionExecution() bool {
	return contract.PerSessionExecution(service.Deps.Runtime)
}

func (service *legacyTestService) bindGlobalProjectRoot(rootPath string) error {
	return service.Deps.Runtime.BindProjectRoot(rootPath)
}

func (service *legacyTestService) setWorkspaceWriteScope(workspaceID string) {
	service.Deps.Sessions.SetWorkspace(workspaceID)
}

func (service *legacyTestService) newGeneratedSessionID(prefix string) string {
	seq := service.SessionIDSeq.Add(1)
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), seq)
}

func (service *legacyTestService) bindProjectRootIfSafe(_ string, rootPath string) bool {
	if service.components.lifecycleApplication.AnyChatRunning() || service.Deps.Runtime == nil {
		return false
	}
	return service.Deps.Runtime.BindProjectRoot(rootPath) == nil
}

func (service *legacyTestService) recoverPendingMessageTailAtLoad(sessionID string) string {
	return switching.RecoverPendingTail(switching.PendingTailDeps{
		Running: func(sessionID string) bool {
			unit := service.sessions.Unit(sessionID)
			return unit != nil && unit.ChatState().Running
		},
		Probe: func(sessionID string) (dto.PendingMessageTailReport, bool, error) {
			location := service.components.sessions.LocateSession(sessionID)
			return service.components.sessions.PendingMessageTail(location, sessionID)
		},
		Recover: func(sessionID string) (dto.PendingMessageTailReport, bool, error) {
			location := service.components.sessions.LocateSession(sessionID)
			return service.components.sessions.RecoverPendingMessageTail(location, sessionID)
		},
	}, sessionID)
}

func (service *legacyTestService) publishChatStateFor(sessionID string) {
	unit := service.sessions.Unit(sessionID)
	if unit == nil || service.Core.Events == nil {
		return
	}
	chat := unit.ChatState()
	if hub, ok := service.Core.Events.(kernelevent.SessionAwareHub); ok {
		hub.PublishSession(kernelevent.EventChatChanged, 0, chat.RequestID, sessionID, chat)
	} else {
		service.Core.Events.Publish(kernelevent.EventChatChanged, 0, chat.RequestID, chat)
	}
	service.ViewMu.RLock()
	active := service.Core.Snapshot.Session.ID == sessionID
	service.ViewMu.RUnlock()
	if !active {
		service.Core.Events.Publish(kernelevent.EventSessionsChanged, 0, "", nil)
	}
}

func (service *legacyTestService) finalizeTaskExecution(requestID string) error {
	service.ViewMu.RLock()
	sessionID := service.Core.Snapshot.Session.ID
	plan := service.Core.Snapshot.Runtime.Plan
	service.ViewMu.RUnlock()
	if sessionID != "" {
		service.components.tasks.EnsurePlanProjection(sessionID, plan)
	}
	err := service.components.tasks.FinalizeTask(context.Background(), taskcontext.ChatEndSummary{RequestID: requestID})
	if err == nil && sessionID != "" {
		if task := service.components.tasks.VisibleTaskStateFor(sessionID); task != nil {
			service.ViewMu.Lock()
			service.Core.Snapshot.Task = task
			service.ViewMu.Unlock()
		}
	}
	return err
}

func (service *legacyTestService) resetViewToDraftAfterRestoreFailure() {
	service.ViewMu.Lock()
	defer service.ViewMu.Unlock()
	service.enterFreshDraftShellLocked(true, false)
}

func (service *legacyTestService) switchViewToFreshDraftLocked() {
	service.enterFreshDraftShellLocked(true, true)
}

func (service *legacyTestService) enterFreshDraftShellLocked(keepWorkspace, keepProcessInteraction bool) {
	draftID := service.newGeneratedSessionID("draft")
	service.Core.Snapshot.Session = protocol.SessionState{
		ID: draftID, Name: "new session", Draft: true, Status: protocol.SessionStatusDraft,
	}
	service.sessions.SetActive(draftID)
	if !keepWorkspace {
		service.Core.Snapshot.CurrentWorkspace = nil
	}
	service.Core.Snapshot.Conversation = nil
	service.Core.Snapshot.Task = nil
	service.Core.Snapshot.ReadFiles = nil
	service.Core.Snapshot.Runtime.Plan = nil
	if !keepProcessInteraction {
		service.Core.Snapshot.Interaction = nil
	} else if interaction := service.Core.Snapshot.Interaction; interaction != nil && interaction.SessionID != "" {
		service.Core.Snapshot.Interaction = nil
	}
	unit := service.sessionUnitLocked(draftID)
	unit.SetCancel(nil)
	unit.SetRequests(nil)
	service.Core.Snapshot.Chat = unit.ChatState()
	service.components.tasks.ResetForNewSessionLocked()
}

func (service *legacyTestService) sessionUnitLocked(sessionID string) *sessionstate.SessionUnit {
	unit := service.components.sessionsDomain.Unit(sessionID)
	if unit == nil {
		var err error
		if sessionID == "" {
			unit = sessionstate.NewDraftUnit()
		} else {
			unit, err = sessionstate.NewSessionUnit(sessionID)
			if err != nil {
				return nil
			}
		}
		service.components.sessionsDomain.Register(unit)
	}
	return unit
}

func LegacyCompactionRangeLabel(result LegacyContextCompactionResult) string {
	return conversationcontext.CompactionRangeLabel(result)
}

type LegacyContextCompactionResult = conversationcontext.Result

const (
	LegacyEventChatChanged       = assembly.EventChatChanged
	LegacyEventSnapshotChanged   = assembly.EventSnapshotChanged
	LegacyEventMessageDelta      = assembly.EventMessageDelta
	LegacyEventMessageAdded      = assembly.EventMessageAdded
	LegacyEventToolStarted       = assembly.EventToolStarted
	LegacyEventToolCompleted     = assembly.EventToolCompleted
	LegacyEventRuntimeChanged    = assembly.EventRuntimeChanged
	LegacyEventWorkTableChanged  = assembly.EventWorkTableChanged
	LegacyEventInteractionOpened = assembly.EventInteractionOpened
	LegacyEventInteractionClosed = assembly.EventInteractionClosed
	LegacyEventSessionsChanged   = kernelevent.EventSessionsChanged
	LegacySessionStatusDraft     = assembly.SessionStatusDraft
	LegacySessionStatusIdle      = assembly.SessionStatusIdle
	LegacySessionStatusRunning   = assembly.SessionStatusRunning
	LegacySessionStatusQueued    = assembly.SessionStatusQueued
	LegacySessionStatusArchived  = assembly.SessionStatusArchived
	LegacySessionStatusRestoring = assembly.SessionStatusRestoring
	LegacySessionStatusUnknown   = assembly.SessionStatusUnknown
	LegacyTurnProgressing        = assembly.TurnProgressing
	LegacyTurnCompleted          = assembly.TurnCompleted
	LegacyTurnNeedsUserDecision  = assembly.TurnNeedsUserDecision
	LegacyTurnBlocked            = assembly.TurnBlocked
	LegacyTurnInterrupted        = assembly.TurnInterrupted
	LegacyTurnFailed             = assembly.TurnFailed
	LegacyTurnUnknown            = assembly.TurnUnknown
	LegacyTurnIdle               = assembly.TurnIdle
)

var (
	LegacyErrChatRunning                = assembly.ErrChatRunning
	LegacyErrSessionBusy                = assembly.ErrSessionBusy
	LegacyErrSessionNotFoundArchive     = assembly.ErrSessionNotFoundArchive
	LegacyErrSessionSnapshotUnavailable = assembly.ErrSessionSnapshotUnavailable
)
