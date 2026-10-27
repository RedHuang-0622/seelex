package assembly

import (
	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	applicationscheduler "github.com/RedHuang-0622/seelex/application/admin/scheduler"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	conversationcontext "github.com/RedHuang-0622/seelex/application/conversation/context"
	conversationsearch "github.com/RedHuang-0622/seelex/application/conversation/search"
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	subagentview "github.com/RedHuang-0622/seelex/application/execution/subagentview"
	applicationtable "github.com/RedHuang-0622/seelex/application/execution/table"
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
	applicationsessionfork "github.com/RedHuang-0622/seelex/application/session/fork"
	applicationsessionhistory "github.com/RedHuang-0622/seelex/application/session/history"
	applicationsessionquery "github.com/RedHuang-0622/seelex/application/session/query"
	applicationsessionresidency "github.com/RedHuang-0622/seelex/application/session/residency"
	"github.com/RedHuang-0622/seelex/application/session/runtime"
	"github.com/RedHuang-0622/seelex/application/session/viewstate"
	applicationworkspace "github.com/RedHuang-0622/seelex/application/workspace"
	switchworkspace "github.com/RedHuang-0622/seelex/application/workspace/switch"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/kernel/board"
	"sync"
)

// serviceComponents is the application composition graph. Service coordinates
// these parts but does not own their implementation details.
type serviceComponents struct {
	prompts               *applicationprompt.Coordinator
	context               *conversationcontext.EngineCoordinator
	contextCompactionGate *conversationcontext.CompactionGate
	providerRecovery      *conversationcontext.RecoveryApplication
	history               *conversationcontext.EngineHistoryCoordinator
	sessions              *session_runtime.Coordinator
	sessionHistoryRuntime *applicationsessionhistory.Runtime
	sessionForkRuntime    *applicationsessionfork.Runtime
	tasks                 *task_context.Coordinator
	terminalHandler       *task_context.TerminalHandlerApplication
	// goal 是会话级 goal 治理协调器（P1：每会话 Controller+Supervisor；
	// 视图投影/工具路由经 Service 面暴露）。
	goal             *applicationgoal.Coordinator
	view             *view_state.Coordinator
	frontendSnapshot *view_state.SnapshotApplication
	subagent         *subagentview.Coordinator
	input            contract.InputDispatcher
	submission       *applicationinput.SubmissionApplication

	// sessionQuery 与 conversationHistoryQuery 是 Application 查询面：
	// 只持窄读依赖，不持完整 Service，也不承担会话生命周期状态机。
	sessionQuery                 *applicationsessionquery.Application
	conversationSession          *applicationconversation.UnifiedApplication
	conversationHistoryQuery     *applicationhistory.Application
	residency                    applicationsession.ResidencyPort
	residencyRuntime             *applicationsessionresidency.Runtime
	conversationStreams          applicationconversation.StreamOutputPort
	errorPresentation            contract.ErrorPresentationPort
	roleSessions                 contract.RoleSessionsPort
	conversationQueue            *applicationconversation.QueueApplication
	conversationTurnState        *conversationTurnState
	conversationStreamRuntime    *applicationconversation.StreamRuntime
	sessionLifecycle             *applicationsession.LifecycleApplication
	interactionApplication       *applicationsession.InteractionApplication
	forkApplication              *applicationsession.ForkApplication
	submitApplication            *applicationsession.SubmitApplication
	sessionArchive               *applicationsession.ArchiveApplication
	sessionDelete                *applicationsession.DeleteApplication
	sessionDrafts                *sessionDraftComponents
	sessionResume                *applicationsession.ResumeApplication
	editFork                     *editForkComponents
	conversationTurn             *conversationTurnComponents
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
	workTableRuntime             *applicationtable.Runtime
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
	projectBoard *ProjectBoardSinkPort

	// workspace 是工作区用例的装配载体（application/workspace/switch）：创建/绑定/
	// 解绑、当前工作区读面与投影收集。视图锁与发布面经 workspaceHost 注入。
	workspace *switchworkspace.UseCase
}

type chatTurnStartOptions = applicationconversation.StartOptions

// ProjectBoardSinkPort 是项目看板订阅口（board.ProjectBoardSink）的**可变槽位**：
// 装配期交接一次、此后每回合读一份（读多写一，单独一把锁，不碰 Core.ViewMu）。
// 注入走组件自己的 Store 方法，不再由装配快照携带一份闭包。
type ProjectBoardSinkPort struct {
	mu   sync.RWMutex
	sink board.ProjectBoardSink
}

func (port *ProjectBoardSinkPort) Store(sink board.ProjectBoardSink) {
	port.mu.Lock()
	port.sink = sink
	port.mu.Unlock()
}

func (port *ProjectBoardSinkPort) load() board.ProjectBoardSink {
	port.mu.RLock()
	defer port.mu.RUnlock()
	return port.sink
}
