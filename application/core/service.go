// Package core orchestrates application use cases while depending only on contracts.
package core

import (
	"errors"
	"strings"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/model"
	seelsession "github.com/RedHuang-0622/seelex/seelebridge/session"
)

// defaultHistoryWindow 与 maxReplansPerPlanChain 已收编进 seele.yaml limits 段
// （history_window / max_replans_per_plan_chain）；默认值定义在
// seelexctx.DefaultLimits，消费点经 Limits() 读取。

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
	ErrEmptySearchQuery = errors.New("search_history: query is required")
	// ErrQueueNotRunning 表示目标会话当前没有运行中的回合，因此不存在可
	// 调换/撤回的「排队中」输入（队列只在回合运行期间承接输入）。
	ErrQueueNotRunning = errors.New("no queued input: session is not running")
	// ErrQueueIndexOutOfRange 表示调换/撤回的队列下标越界。
	ErrQueueIndexOutOfRange = errors.New("queued input index out of range")
	// ErrQueueSessionNotFound 表示目标会话不在会话域注册表中（不存在）。
	ErrQueueSessionNotFound = errors.New("queue session not found")
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

func New(deps Dependencies) (*Service, error) {
	return serviceAssembler{deps: deps}.assemble()
}

// ActiveSkillIDs 返回当前任务的激活 skill ID 列表（goal skill 激活判定用，
// 见 Runtime 单向可见性投影；锁内快照，无锁外访问）。
func (service *Service) ActiveSkillIDs() []string {
	return service.components.tasks.ActiveSkillIDs()
}

// PromptLayers 返回当前会话注入的 prompt 前缀层（system/base/effort/
// instructions/skill；GUI 轨迹视图"前缀注入"数据源，经桥接方法单独拉取，
// 不进 Snapshot 序列化——避免把私有指令泄漏进常规快照）。
func (service *Service) PromptLayers() []PromptLayer {
	if service == nil || service.promptStack == nil {
		return nil
	}
	return service.promptStack.Layers()
}

// GoalSkillActive 返回最新的本地投影（诊断与测试用）。Runtime 经
// PublishRuntimeProjections 收到同一值；它不调用本方法。
func (service *Service) GoalSkillActive() bool {
	return service.components.tasks.GoalSkillActive()
}

// PublishRuntimeProjections 刷新 Runtime 的不可变状态副本。供在
// Application.New 返回后完成 Runtime 接线的组合根调用。
func (service *Service) PublishRuntimeProjections() {
	service.publishRuntimeProjections()
}

// HandleSubagentToolEvent 把 Runtime 工具分发投影进权威 Plan 节点快照并
// 发布一次前端增量。
func (service *Service) HandleSubagentToolEvent(event seelsession.SubagentToolEvent) {
	service.components.subagent.HandleSubagentToolEvent(event)
}

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
func (service *Service) HandleRoleToolActivity(event dto.RoleToolActivity) {
	if service == nil || service.Events == nil {
		return
	}
	sessionID := strings.TrimSpace(event.MainSessionID)
	if sessionID == "" || strings.TrimSpace(event.ID) == "" {
		return
	}
	limit := Limits().EvidenceChars
	event.Arguments = truncateWorkEvidence(event.Arguments, limit)
	event.Result = truncateWorkEvidence(event.Result, limit)
	event.Error = truncateWorkEvidence(event.Error, limit)

	kind := EventTeammateToolCompleted
	if strings.TrimSpace(event.Status) == "running" {
		kind = EventTeammateToolStarted
	}
	requestID := ""
	if service.Core != nil {
		// 快照字段统一由 Core.ViewMu 保护：本回调由 Runtime 在工具活动时直接
		// 调用，可能与会话切换并发，裸读构成同族数据竞争。
		service.ViewMu.RLock()
		requestID = service.Core.Snapshot.Chat.RequestID
		service.ViewMu.RUnlock()
	}
	service.publishSessionEvent(kind, 0, requestID, sessionID, event)
}

// SubagentSessionDetail 返回节点子代理的详情数据（截断会话 + 上下文快照 +
// 功能打点）。
func (service *Service) SubagentSessionDetail(nodeID string) (*model.SubagentDetail, error) {
	return service.components.subagent.SubagentDetail(nodeID)
}

// ClearSubagentTree 清空子代理树（GUI「清空」按钮入口：失败节点显式清走；
// 完成后刷新快照投影，前端经 runtime.changed 增量收到空树 → 分区隐藏）。
func (service *Service) ClearSubagentTree() error {
	if err := service.Deps.Runtime.ClearSubagentTree(); err != nil {
		return err
	}
	service.RefreshRuntimeSnapshot()
	return nil
}
