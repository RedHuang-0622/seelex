// Package core orchestrates application use cases while depending only on contracts.
package core

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
	applicationsession "github.com/RedHuang-0622/seelex/application/session"
	"github.com/RedHuang-0622/seelex/application/session/switchpolicy"
	"github.com/RedHuang-0622/seelex/contract"
)

// defaultHistoryWindow 与 maxReplansPerPlanChain 已收编进 seele.yaml limits 段
// （history_window / max_replans_per_plan_chain）；默认值定义在
// limit.DefaultLimits，消费点经 Limits() 读取。

var (
	ErrChatRunning         = errors.New("chat is already running")
	ErrApplicationDraining = errors.New("application is finishing active work")
	// ErrSessionRestoring 是恢复门的兜底错误：目标会话正在后台冷加载
	// （restoring 空壳，视图指针已切到目标、内容尚未装载完成）时**不启动新回合**。
	// 提交入口（submitConversation / submitConversationFor）命中 restoring 走
	// **延后**——把输入挂到装载完成点再启动（既不开空壳回合，也不丢输入）；
	// 本错误只在更窄的 TOCTOU 窗口出现：提交通过恢复门、释放 ViewMu 之后，切换
	// 才把目标置为 restoring（startChatFor 内二次判定）。届时宁可明确失败，也不
	// 在空壳上开出回合——见 devlog 2026-09-17-submit-during-cold-restore-fix。
	ErrSessionRestoring = errors.New("session is restoring; retry after the restore completes")
	// ErrSessionBusy 是跨会话提交/切换的单飞执行边界（M1：共享组件栈要求
	// 同一时刻只有一个会话在执行；真并行 = M2 会话级组件隔离）。
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

// Frontend is the frontend compatibility adapter.
type Frontend struct{ *Service }

// projectBoardSink 取一份当前订阅口快照（读锁；返回的是一份接口值副本）。
//
// 为什么"取一份"而不是"持锁调用"：订阅口是**外部实现**（生产里嵌的是同步器，它只
// 标脏 + 唤醒、不做 IO）；完成段是回合内高频点，绝不让外部代码在装配锁下运行——一次
// 慢实现就变成整个回合的停顿，且注入与调用会互相等锁（本函数与 SetProjectBoardSink
// 可并发：注入可能发生在两个回合之间，读取在回合内的工具完成段）。
func (service *Service) projectBoardSink() applicationexecution.ProjectBoardSink {
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
