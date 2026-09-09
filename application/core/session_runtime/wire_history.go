package session_runtime

import (
	"github.com/RedHuang-0622/seelex/application/contract"
)

// SessionWireAssemblerPort 是 R2 wire 装配的可选能力：实现方对 v8 会话
// 直接产出 compact 摘要 + 尾窗 + 最近 K 条尝试的 provider 历史（旧链路
// 回退 ok=false）。
type SessionWireAssemblerPort interface {
	AssembleWireHistoryWorkspace(projectID, sessionID string, budget, k int) ([]contract.EngineMessage, bool, error)
}

// AssembleWireHistoryWorkspace 通过 Core.Deps.Sessions 的可选能力执行 R2
// 装配；未装配/非 v8 返回 ok=false。
func (c *Coordinator) AssembleWireHistoryWorkspace(location Location, sessionID string, budget, k int) ([]contract.EngineMessage, bool, error) {
	if c == nil || c.Core == nil {
		return nil, false, nil
	}
	store, ok := c.Core.Deps.Sessions.(SessionWireAssemblerPort)
	if !ok {
		return nil, false, nil
	}
	return store.AssembleWireHistoryWorkspace(location.WorkspaceID, sessionID, budget, k)
}

// SessionLifecycleRecoverPort 是 v8 lifecycle 队列恢复的可选能力。
type SessionLifecycleRecoverPort interface {
	LifecycleRecoverWorkspace(projectID, sessionID string) (int, bool, error)
}

// LifecycleRecover 重启后恢复 v8 lifecycle 队列（发送未确认项回 queued；
// message 已发布项出队）。返回恢复条数；非 v8/未装配 ok=false。
func (c *Coordinator) LifecycleRecover(location Location, sessionID string) (int, bool, error) {
	if c == nil || c.Core == nil {
		return 0, false, nil
	}
	store, ok := c.Core.Deps.Sessions.(SessionLifecycleRecoverPort)
	if !ok {
		return 0, false, nil
	}
	return store.LifecycleRecoverWorkspace(location.WorkspaceID, sessionID)
}
