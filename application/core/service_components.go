package core

import (
	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	applicationscheduler "github.com/RedHuang-0622/seelex/application/admin/scheduler"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	conversationcontext "github.com/RedHuang-0622/seelex/application/conversation/context"
	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	conversationsearch "github.com/RedHuang-0622/seelex/application/conversation/search"
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	"github.com/RedHuang-0622/seelex/application/execution/taskcontext"
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
	applicationsessioncold "github.com/RedHuang-0622/seelex/application/session/cold"
	applicationsessionquery "github.com/RedHuang-0622/seelex/application/session/query"
	"github.com/RedHuang-0622/seelex/application/session/runtime"
	"github.com/RedHuang-0622/seelex/application/session/viewstate"
	applicationworkspace "github.com/RedHuang-0622/seelex/application/workspace"
	"github.com/RedHuang-0622/seelex/contract"
	"sync"
)

// serviceComponents is the application composition graph. Service coordinates
// these parts but does not own their implementation details.
type serviceComponents struct {
	prompts         *applicationprompt.Coordinator
	context         *conversationcontext.EngineCoordinator
	history         *conversationcontext.EngineHistoryCoordinator
	sessions        *session_runtime.Coordinator
	tasks           *task_context.Coordinator
	terminalHandler *task_context.TerminalHandlerApplication
	// goal 是会话级 goal 治理协调器（P1：每会话 Controller+Supervisor；
	// 视图投影/工具路由经 Service 面暴露）。
	goal             *applicationgoal.Coordinator
	view             *view_state.Coordinator
	frontendSnapshot *view_state.SnapshotApplication
	subagent         *applicationexecution.SubagentViewCoordinator
	input            contract.InputDispatcher
	submission       *applicationinput.SubmissionApplication

	// sessionQuery 与 conversationHistoryQuery 是 Application 查询面：
	// 只持窄读依赖，不持完整 Service，也不承担会话生命周期状态机。
	sessionQuery                 *applicationsessionquery.Application
	conversationSession          *applicationconversation.UnifiedApplication
	conversationHistoryQuery     *applicationhistory.Application
	historyWindowAssembler       applicationhistory.WindowAssembler
	residency                    applicationsession.ResidencyPort
	conversationStreams          applicationconversation.StreamOutputPort
	accountWindowPolicy          applicationadmin.AccountWindowPolicy
	skillActivation              applicationpolicy.SkillActivationPort
	sessionRecords               applicationconversation.SessionRecordsPort
	errorPresentation            contract.ErrorPresentationPort
	roleSessions                 contract.RoleSessionsPort
	conversationQueue            *applicationconversation.QueueApplication
	conversationTurnState        *conversationTurnState
	sessionLifecycle             *applicationsession.LifecycleApplication
	interactionApplication       *applicationsession.InteractionApplication
	forkApplication              *applicationsession.ForkApplication
	submitApplication            *applicationsession.SubmitApplication
	sessionArchive               *applicationsession.ArchiveApplication
	sessionDelete                *applicationsession.DeleteApplication
	sessionDraftSubmission       *applicationsession.DraftSubmissionApplication
	sessionDraftMaterialization  *applicationsession.DraftMaterializationApplication
	sessionDraftEntry            *applicationsession.DraftEntryApplication
	sessionResume                *applicationsession.ResumeApplication
	editForkOperationRegistry    *applicationedit.OperationRegistry
	editForkApplication          *applicationedit.EditApplication
	editForkValidator            *applicationedit.Validator
	editForkTargetResolver       *applicationedit.TargetResolver
	editForkRollback             *applicationedit.RollbackApplication
	editForkProcess              *applicationedit.ProcessApplication
	editForkAnnotation           *applicationedit.AnnotationApplication
	sessionForkLineage           *applicationsession.ForkLineageApplication
	sessionRestoringShell        *applicationsession.RestoringShellApplication
	sessionRestoreRecovery       *applicationsession.RestoreRecoveryApplication
	sessionRestoreDraftReset     *applicationsession.RestoreDraftResetApplication
	sessionColdRead              *applicationsessioncold.ReadApplication
	sessionColdHistory           *applicationsessioncold.HistoryApplication
	sessionColdRestore           *applicationsessioncold.RestoreApplication
	sessionColdWorkspace         *applicationsessioncold.WorkspaceApplication
	sessionColdActivation        *applicationsessioncold.ActivationApplication
	sessionColdTask              *applicationsessioncold.TaskApplication
	sessionColdView              *applicationsessioncold.ViewApplication
	sessionColdPublish           *applicationsessioncold.PublishApplication
	sessionColdCommit            *applicationsessioncold.CommitApplication
	conversationTurnProcess      *applicationconversation.TurnProcessApplication
	conversationTurnCompletion   *applicationconversation.TurnCompletionApplication
	conversationTurnOutcome      *applicationconversation.TurnOutcomeApplication
	conversationTurnPublisher    *applicationconversation.TurnPublisherApplication
	conversationTurnTail         *applicationconversation.TurnTailApplication
	conversationTurnInput        *applicationconversation.TurnInputApplication
	conversationTurnResponse     *applicationconversation.TurnResponseApplication
	conversationTurnRecovery     *applicationconversation.TurnRecoveryApplication
	conversationTurnCleanup      *applicationconversation.TurnCleanupApplication
	conversationTurnCommit       *applicationconversation.TurnCommitApplication
	conversationTurnFinalize     *applicationconversation.TurnFinalizeApplication
	conversationTurnExecution    *applicationconversation.TurnExecutionApplication
	conversationTurnRunner       *applicationconversation.TurnRunnerApplication
	policyApplication            *applicationpolicy.SwitchApplication
	adminApplication             *applicationadmin.AccountApplication
	workspaceApplication         *applicationworkspace.Application
	sessionSSHApplication        *applicationsession.SSHApplication
	roleApplication              *applicationpolicy.RoleApplication
	schedulerApplication         *applicationscheduler.SchedulerApplication
	scheduledRunApplication      *applicationscheduler.RunApplication
	scheduledSessionApplication  *applicationscheduler.SessionApplication
	runtimeApplication           *applicationadmin.RuntimeApplication
	reviewApplication            *applicationreview.Application
	lifecycleApplication         *applicationlifecycle.Application
	historySearchApplication     *conversationsearch.Application
	completionApplication        *applicationinput.Application
	referenceApplication         *applicationreference.Application
	sessionMetaApplication       *applicationsession.MetaApplication
	projectBoardQuery            *applicationexecution.ProjectBoardApplication
	sessionReleaseApplication    *applicationsession.ReleaseApplication
	contextCompactionApplication *conversationcontext.Application
	faultGuardApplication        *applicationlifecycle.FaultGuardApplication
	perfStatsApplication         *applicationobservability.PerfApplication
	asyncCompletionApplication   *applicationexecution.CompletionApplication
	goalApplication              *applicationgoal.Application
	subagentApplication          *applicationexecution.SubagentApplication
	executionApplication         *applicationexecution.WorkItemApplication
	roleActivity                 *applicationexecution.RoleActivityApplication

	// projectBoard 是装配期注入的**可选写侧端口**（项目看板投影的订阅口，M3）。
	//
	// 为什么落在这里而不是 facade 的第三个字段：facade 只允许 state + components
	// （TestServiceFacadeContainsOnlyAssembly 就是这条结构断言的守门人），而它是
	// 装配期交接的外部端口——与其它组件同一生态位（注入一次、此后只读）。
	//
	// 为什么不落进某个 serviceState 状态组：状态组是**用例会读改的会话状态**，
	// 放进去等于给"第二份判定/第二份事实"开口子（本能力只需要一个转发口）。
	//
	// 为什么是**指针**槽位：serviceComponents 是可复制/可 `%+v` 的纯装配图
	// （既有用例与 `go vet` 都这么用它）；内联 mutex 会让整张图变成不可复制
	// （vet: "copies lock value"），指针让"可变性"只留在槽位自己身上。
	projectBoard *projectBoardPort

	// workspace 是工作区用例的装配载体（middle/switch/workspace）：创建/绑定/
	// 解绑、当前工作区读面与投影收集。视图锁与发布面经 workspaceHost 注入。
	workspace *applicationworkspace.SwitchUseCase
}

// Temporary aliases keep the ChatTurn migration compatible while the
// Conversation aggregate still consumes these values.
type conversationTurnProcess = applicationconversation.TurnProcessApplication
type conversationTurnCompletion = applicationconversation.TurnCompletionApplication
type conversationTurnOutcome = applicationconversation.TurnOutcomeApplication
type conversationTurnPublisher = applicationconversation.TurnPublisherApplication
type conversationTurnTail = applicationconversation.TurnTailApplication
type conversationTurnCommit = applicationconversation.TurnCommitApplication
type conversationTurnFinalize = applicationconversation.TurnFinalizeApplication
type conversationTurnExecution = applicationconversation.TurnExecutionApplication

type chatTurnStartOptions = applicationconversation.StartOptions
type chatTurnPromotion = applicationconversation.Promotion
type chatTurnCommitResult = applicationconversation.CommitResult
type chatTurnPublication = applicationconversation.Publication

type editForkOperation = applicationedit.Operation

// projectBoardPort 是项目看板订阅口（board.ProjectBoardSink）的**可变槽位**：
// 装配期交接一次、此后每回合读一份（读多写一，单独一把锁，不碰 Core.ViewMu）。
type projectBoardPort struct {
	mu   sync.RWMutex
	sink applicationexecution.ProjectBoardSink
}

func (port *projectBoardPort) store(sink applicationexecution.ProjectBoardSink) {
	port.mu.Lock()
	port.sink = sink
	port.mu.Unlock()
}

func (port *projectBoardPort) load() applicationexecution.ProjectBoardSink {
	port.mu.RLock()
	defer port.mu.RUnlock()
	return port.sink
}
