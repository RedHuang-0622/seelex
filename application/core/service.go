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

func New(deps Dependencies) *Service {
	return serviceAssembler{deps: deps}.assemble()
}
