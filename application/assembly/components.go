package assembly

import (
	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	applicationscheduler "github.com/RedHuang-0622/seelex/application/admin/scheduler"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	conversationcontext "github.com/RedHuang-0622/seelex/application/conversation/context"
	applicationedit "github.com/RedHuang-0622/seelex/application/conversation/edit"
	conversationsearch "github.com/RedHuang-0622/seelex/application/conversation/search"
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	subagentview "github.com/RedHuang-0622/seelex/application/execution/subagentview"
	task_context "github.com/RedHuang-0622/seelex/application/execution/taskcontext"
	applicationtoolrecord "github.com/RedHuang-0622/seelex/application/execution/toolrecord"
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
	"github.com/RedHuang-0622/seelex/contract"
)

// Components 是组合根读取的只读装配快照：只有 owner 指针，按限界上下文分组。
//
// 为什么字段里不许出现 func：一份 `func(main, role string)` 闭包能在"纯数据"面里
// 藏住判据（角色单元键怎么拼、投影发布的加锁顺序），那正是第二份实现的增长点。
// 判据一律回到 owner 方法（如 toolrecord.Application.CloseRoleUnit）或 assembly
// 自己的协调函数（PublishInitialRuntimeProjections）。
type Components struct {
	Admin        AdminComponents
	Conversation ConversationComponents
	Session      SessionComponents
	Execution    ExecutionComponents
	Workspace    WorkspaceComponents
	Policy       PolicyComponents
	Runtime      RuntimeComponents
}

// AdminComponents 是跨会话的运营读数 owner（账号、目标、审阅、诊断）。
type AdminComponents struct {
	Account *applicationadmin.AccountApplication
	Goal    *applicationgoal.Application
	Review  *applicationreview.Application
	Perf    *applicationobservability.PerfApplication
}

// ConversationComponents 是"一条会话内容"的 owner：回合、队列、输入、压缩、历史与检索。
type ConversationComponents struct {
	Unified        *applicationconversation.UnifiedApplication
	Queue          *applicationconversation.QueueApplication
	Submission     *applicationinput.SubmissionApplication
	Completion     *applicationinput.Application
	Compaction     *conversationcontext.Application
	ContextHistory *conversationcontext.EngineHistoryCoordinator
	History        *applicationhistory.Application
	HistorySearch  *conversationsearch.Application
	EditFork       *applicationedit.EditApplication
	PromptStack    *applicationprompt.PromptStack
	Reference      *applicationreference.Application
}

// SessionComponents 是会话驻留与视图 owner：单个会话的生命周期动作加上视图会话读数。
type SessionComponents struct {
	Query       *applicationsessionquery.Application
	Interaction *applicationsession.InteractionApplication
	Fork        *applicationsession.ForkApplication
	Submit      *applicationsession.SubmitApplication
	Archive     *applicationsession.ArchiveApplication
	Delete      *applicationsession.DeleteApplication
	DraftEntry  *applicationsession.DraftEntryApplication
	Lifecycle   *applicationsession.LifecycleApplication
	Meta        *applicationsession.MetaApplication
	Resume      *applicationsession.ResumeApplication
	SSH         *applicationsession.SSHApplication
	View        *view_state.Coordinator
	Snapshot    *view_state.SnapshotApplication
}

// ExecutionComponents 是执行面 owner：工作项/角色/子代理投影、项目看板与工具台账。
type ExecutionComponents struct {
	WorkItem         *applicationexecution.WorkItemApplication
	RoleActivity     *applicationexecution.RoleActivityApplication
	ProjectBoard     *applicationexecution.ProjectBoardApplication
	ProjectBoardSink *ProjectBoardSinkPort
	Subagent         *applicationexecution.SubagentApplication
	SubagentView     *subagentview.Coordinator
	Tasks            *task_context.Coordinator
	TerminalHandler  *task_context.TerminalHandlerApplication
	ToolRecords      *applicationtoolrecord.Application
}

// WorkspaceComponents 是项目/工作区 owner：项目登记与工作区切换用例。
type WorkspaceComponents struct {
	Projects *applicationworkspace.Application
	Switch   *switchworkspace.UseCase
}

// PolicyComponents 是策略 owner：技能/插件开关、角色档位与角色会话。
type PolicyComponents struct {
	Switch       *applicationpolicy.SwitchApplication
	Role         *applicationpolicy.RoleApplication
	RoleSessions contract.RoleSessionsPort
}

// RuntimeComponents 是运行时读数与排期 owner。
type RuntimeComponents struct {
	Runtime          *applicationadmin.RuntimeApplication
	Scheduler        *applicationscheduler.SchedulerApplication
	ScheduledRun     *applicationscheduler.RunApplication
	ScheduledSession *applicationscheduler.SessionApplication
	Lifecycle        *applicationlifecycle.Application
}

// ComponentsFor returns a read-only snapshot of assembled component owners.
// It must not be used by core state machines.
func ComponentsFor(service *Service) Components {
	if service == nil {
		return Components{}
	}
	return Components{
		Admin: AdminComponents{
			Account: service.components.adminApplication,
			Goal:    service.components.goalApplication,
			Review:  service.components.reviewApplication,
			Perf:    service.components.perfStatsApplication,
		},
		Conversation: ConversationComponents{
			Unified:        service.components.conversationSession,
			Queue:          service.components.conversationQueue,
			Submission:     service.components.submission,
			Completion:     service.components.completionApplication,
			Compaction:     service.components.contextCompactionApplication,
			ContextHistory: service.components.history,
			History:        service.components.conversationHistoryQuery,
			HistorySearch:  service.components.historySearchApplication,
			EditFork:       service.components.editForkApplication,
			PromptStack:    service.promptStack,
			Reference:      service.components.referenceApplication,
		},
		Session: SessionComponents{
			Query:       service.components.sessionQuery,
			Interaction: service.components.interactionApplication,
			Fork:        service.components.forkApplication,
			Submit:      service.components.submitApplication,
			Archive:     service.components.sessionArchive,
			Delete:      service.components.sessionDelete,
			DraftEntry:  service.components.sessionDraftEntry,
			Lifecycle:   service.components.sessionLifecycle,
			Meta:        service.components.sessionMetaApplication,
			Resume:      service.components.sessionResume,
			SSH:         service.components.sessionSSHApplication,
			View:        service.components.view,
			Snapshot:    service.components.frontendSnapshot,
		},
		Execution: ExecutionComponents{
			WorkItem:         service.components.executionApplication,
			RoleActivity:     service.components.roleActivity,
			ProjectBoard:     service.components.projectBoardQuery,
			ProjectBoardSink: service.components.projectBoard,
			Subagent:         service.components.subagentApplication,
			SubagentView:     service.components.subagent,
			Tasks:            service.components.tasks,
			TerminalHandler:  service.components.terminalHandler,
			ToolRecords:      service.toolRecords,
		},
		Workspace: WorkspaceComponents{
			Projects: service.components.workspaceApplication,
			Switch:   service.components.workspace,
		},
		Policy: PolicyComponents{
			Switch:       service.components.policyApplication,
			Role:         service.components.roleApplication,
			RoleSessions: service.components.roleSessions,
		},
		Runtime: RuntimeComponents{
			Runtime:          service.components.runtimeApplication,
			Scheduler:        service.components.schedulerApplication,
			ScheduledRun:     service.components.scheduledRunApplication,
			ScheduledSession: service.components.scheduledSessionApplication,
			Lifecycle:        service.components.lifecycleApplication,
		},
	}
}
