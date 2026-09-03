package plan

import (
	"sync"
	"time"

	"github.com/RedHuang-0622/seelex/seelexctx"
)

// ReplanGuards 是按会话的 ReplanGuard 注册表（G1/M5）：每个 sid 一个额度
// 槽，后台会话的重规划不消耗视图会话的并发/窗口预算；空会话 ID 归默认槽
// （legacy 无 sid 路径兜底，不构成事实源）。
type ReplanGuards struct {
	mu                  sync.Mutex
	maxConcurrent       int
	maxWindowAttempts   int
	maxProviderRequests int
	window              time.Duration
	guards              map[string]*ReplanGuard
}

// NewReplanGuards 构造按会话 replan 额度注册表（默认值与单 guard 一致）。
func NewReplanGuards(maxConcurrent, maxWindowAttempts, maxProviderRequests int, window time.Duration) *ReplanGuards {
	defaults := seelexctx.DefaultLimits()
	if maxConcurrent <= 0 {
		maxConcurrent = defaults.MaxConcurrentReplans
	}
	if maxWindowAttempts <= 0 {
		maxWindowAttempts = defaults.MaxReplansPerWindow
	}
	if maxProviderRequests <= 0 {
		maxProviderRequests = defaults.MaxReplanProviderReqs
	}
	if window <= 0 {
		window = time.Duration(defaults.ReplanWindowSec) * time.Second
	}
	return &ReplanGuards{
		maxConcurrent:       maxConcurrent,
		maxWindowAttempts:   maxWindowAttempts,
		maxProviderRequests: maxProviderRequests,
		window:              window,
		guards:              map[string]*ReplanGuard{},
	}
}

// For 返回目标会话的额度槽（空会话 ID 返回默认槽；惰性创建）。
func (guards *ReplanGuards) For(sessionID string) *ReplanGuard {
	if guards == nil {
		return nil
	}
	guards.mu.Lock()
	defer guards.mu.Unlock()
	guard := guards.guards[sessionID]
	if guard == nil {
		guard = NewReplanGuard(guards.maxConcurrent, guards.maxWindowAttempts, guards.maxProviderRequests, guards.window)
		guards.guards[sessionID] = guard
	}
	return guard
}

// MetricsFor 返回指定会话的 replan 统计快照（空会话 ID = 默认槽）。
func (guards *ReplanGuards) MetricsFor(sessionID string) ReplanMetrics {
	if guard := guards.For(sessionID); guard != nil {
		return guard.snapshot()
	}
	return ReplanMetrics{}
}
