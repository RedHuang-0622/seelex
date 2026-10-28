// Package assembly is the application composition root for focused use cases.
package assembly

import (
	"context"
	"errors"
	applicationadmin "github.com/RedHuang-0622/seelex/application/admin"
	applicationconversation "github.com/RedHuang-0622/seelex/application/conversation"
	conversationsearch "github.com/RedHuang-0622/seelex/application/conversation/search"
	"github.com/RedHuang-0622/seelex/application/event"
	applicationexecution "github.com/RedHuang-0622/seelex/application/execution"
	applicationgoal "github.com/RedHuang-0622/seelex/application/goal"
	applicationhistory "github.com/RedHuang-0622/seelex/application/history"
	applicationpolicy "github.com/RedHuang-0622/seelex/application/policy"
	applicationreview "github.com/RedHuang-0622/seelex/application/review"
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"github.com/RedHuang-0622/seelex/contract"
	"github.com/RedHuang-0622/seelex/kernel/board"
	"github.com/RedHuang-0622/seelex/kernel/sessionpolicy"
)

// history_window / max_replans_per_plan_chain 默认值由 limit.DefaultLimits 提供。

var (
	ErrChatRunning         = errors.New("chat is already running")
	ErrApplicationDraining = errors.New("application is finishing active work")
	// ErrSessionRestoring 是目标会话仍在后台冷加载时拒绝新回合的兜底错误。
	// submission owner 负责延后；直接 start host 用它明确拒绝。
	ErrSessionRestoring = errors.New("session is restoring; retry after the restore completes")
	// ErrSessionBusy 是跨会话提交/切换的单飞边界。
	ErrSessionBusy = errors.New("another session is running")
	// ErrSessionSnapshotUnavailable 表示目标会话既无驻留引擎快照、也无持久化
	// record 可拼只读基线（C1：未驻留会话从 record 冷拼装；两者皆无即不可用）。
	ErrSessionSnapshotUnavailable = errors.New("session snapshot unavailable: no resident engine and no persisted record")
	// ErrEmptySearchQuery 是历史检索空查询拒绝（检索必须有关键词）。
	ErrEmptySearchQuery = conversationsearch.ErrEmptyQuery
	// ErrQueueNotRunning / ErrQueueIndexOutOfRange / ErrQueueSessionNotFound 的**判据**
	// 已在 M5-R6-queue-edit 随接收者转移归位 `middle/switch`（plan §2.4 的 `service_queue.go`）；
	// 这三个名字是宿主侧的兼容面，与 `switching.ErrQueue*` 是**同一个错误值**
	// （`errors.Is` 两个名字都成立，对外语义不拆）。
	ErrQueueNotRunning      = switching.ErrQueueNotRunning
	ErrQueueIndexOutOfRange = switching.ErrQueueIndexOutOfRange
	ErrQueueSessionNotFound = switching.ErrQueueSessionNotFound
	// ErrDraftNotInView 是草稿提交的归属门：草稿槽是进程单例，只有视图指针仍停在
	// 那份草稿上时才能就地物化（物化会把共享视图镜像切到该 SID）。渲染层拿着过期
	// 快照把输入发给一份已不在视图里的草稿时，明确失败比"投进别的会话"安全。
	ErrDraftNotInView = errors.New("draft session is no longer the viewed session; resubmit after the switch settles")
)

// Service is the public application facade. Stateful responsibilities are
// assembled from focused components; the facade keeps their lifecycle and
// cross-component workflows behind one stable API.
type Service struct {
	*serviceState
	components serviceComponents
}

// projectBoardSink 在锁内只复制接口值；外部同步器不得在装配锁下执行。
// 注入可发生在两个回合之间，工具完成段只消费这份快照。
func (service *Service) projectBoardSink() board.ProjectBoardSink {
	if service == nil {
		return nil
	}
	return service.components.projectBoard.load()
}

type ExternalPorts struct {
	HistoryWindow          applicationhistory.WindowAssembler
	Residency              applicationsession.ResidencyPort
	ConversationStream     applicationconversation.StreamOutputPort
	AccountWindow          applicationadmin.AccountWindowPolicy
	SkillActivation        applicationpolicy.SkillActivationPort
	SessionRecords         applicationconversation.SessionRecordsPort
	ErrorPresentation      contract.ErrorPresentationPort
	ApprovalFactory        func(event.Hub) contract.ApprovalBroker
	CommandRegistry        func() contract.CommandRegistryPort
	InputRouter            func(contract.InputRouteHandlers) contract.InputDispatcher
	RoleSessions           func(contract.SessionPort) contract.RoleSessionsPort
	TeamworkBoardReader    func(contract.RuntimePort) applicationexecution.BoardReader
	HostSessionForToolCall func(context.Context) string
	Goal                   *applicationgoal.Coordinator
	ReviewProcessor        applicationreview.ProcessorFactory
}

func New(deps contract.Dependencies, ports ExternalPorts) (*Service, error) {
	return serviceAssembler{
		deps:  deps,
		ports: ports,
	}.assemble()
}

// handleSubagentToolEvent 把 Runtime 工具分发投影进权威 Plan 节点快照并
// 发布一次前端增量。
//
// **兼容面**（2026-10-19 方向 C）：这条入口不带 owner 归属，因此**不记读数**
// （记账要求 `(ownerSessionID, unitID)` 两维都在，见 tool_record_stage.go）；
// Plan 投影照旧。生产装配走 HandleSubagentToolEventFor（装配层从执行上下文
// 解析出主会话再调进来）。
// HandleRoleToolActivity 把 Runtime 的**员工回合**工具活动投影成会话级实时事件
// （`teammate.tool.started/completed`）。
//
// 生态位（与 HandleSubagentToolEvent 对称，不是它的第二份实现）：子代理那条把活动
// 落进 Plan 节点的有界 `tool_events`（详情面板有权威投影可读）；员工这条**不落快照**
// ——员工的权威记录在它自己的角色会话里（`AgentTeamRoleSnapshot` 是读面），这里只负责
// "它刚动了"这一件事的实时送达。所以：
//
//   - 载荷 = 这一次工具调用的有界投影（`dto.RoleToolActivity`），按 `Limits().EvidenceChars`
//     截断（与子代理同一把尺子，不另立一份上限）；
//   - `revision = 0`（同 `team.changed` 口径）：载荷不进快照，带 revision 会被"快照比
//     事件新"的陈旧判据吃掉，逐帧的进度就又变成"等这一轮跑完才看得到"；
//   - 路由键 = `MainSessionID`（这位 teammate 所属的主会话）；缺它不发布——宁可丢弃也不
//     回填到别的会话（同 jobs_events.go 的口径）。
//
// 单向只读：它只播报"正在发生什么"，不写任何状态，也不唤醒任何忙会话。
