// Package core orchestrates application use cases while depending only on contracts.
package core

import (
	"errors"

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
	// ErrSessionBusy 是跨会话提交/切换的单飞执行边界（M1：共享组件栈要求
	// 同一时刻只有一个会话在执行；真并行 = M2 会话级组件隔离）。
	ErrSessionBusy = errors.New("another session is running")
	// ErrSessionSnapshotUnavailable 表示目标会话无驻留快照（M1 只有活跃
	// 会话持有快照，其它会话需先 ActivateSession）。
	ErrSessionSnapshotUnavailable = errors.New("session snapshot is unavailable until activated")
	// ErrEmptySearchQuery 是历史检索空查询拒绝（检索必须有关键词）。
	ErrEmptySearchQuery = errors.New("search_history: query is required")
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

// SubscribeSubagentLive 订阅 node 第一视角实时流（历史回放 + 只读事件通道
// + 取消函数，取消幂等）。
func (service *Service) SubscribeSubagentLive(nodeID string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error) {
	return service.components.subagent.SubscribeSubagentLive(nodeID)
}

// HandleSubagentToolEvent 把 Runtime 工具分发投影进权威 Plan 节点快照并
// 发布一次前端增量。
func (service *Service) HandleSubagentToolEvent(event seelsession.SubagentToolEvent) {
	service.components.subagent.HandleSubagentToolEvent(event)
}

// SubagentSessionDetail 返回节点子代理的详情数据（截断会话 + 上下文快照 +
// worktree 现场）。
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
