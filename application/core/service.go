// Package core orchestrates application use cases while depending only on contracts.
package core

import (
	"errors"
)

// defaultHistoryWindow 与 maxReplansPerPlanChain 已收编进 seele.yaml limits 段
// （history_window / max_replans_per_plan_chain）；默认值定义在
// seelexctx.DefaultLimits，消费点经 Limits() 读取。

var (
	ErrChatRunning         = errors.New("chat is already running")
	ErrApplicationDraining = errors.New("application is finishing active work")
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
	service.mu.RLock()
	defer service.mu.RUnlock()
	state := service.components.tasks.taskExecution
	if state == nil {
		return nil
	}
	ids := make([]string, 0, len(state.activeSkills))
	for _, active := range state.activeSkills {
		ids = append(ids, active.SkillID)
	}
	return ids
}

// GoalSkillActive returns the latest local projection for diagnostics and
// tests. Runtime receives the same value through PublishRuntimeProjections;
// it does not call this method.
func (service *Service) GoalSkillActive() bool {
	return service.goalSkillActive.Load()
}

// PublishRuntimeProjections refreshes Runtime's immutable state copies. It is
// exposed for composition roots that complete their Runtime wiring after
// Application.New returns.
func (service *Service) PublishRuntimeProjections() {
	service.publishRuntimeProjections()
}
