package seelebridge

import (
	"errors"
	"fmt"
	"strings"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/seelex/sessionstore"
)

// 子代理独立会话（thin-wrapper-session-design.md §3.3，UC7）：
// plan 节点 = 一个 SubagentSession（own loop/视图/历史），经
// NewSubagentSessionWithID 以显式会话 ID 创建并注册进 subagentSessions
// registry；结束经 Unregister 回收（导出快照 → merge-back 主会话，UC8）。

// NewSubagentSessionWithID 创建独立子代理会话单元：与主会话同构（框架
// Session + DurableHistory），但使用节点上下文组件（own context/loop）。
// 会话 ID 即节点 ID；注册后可从 SubagentSessions 读取运行中 History。
func (r *Runtime) NewSubagentSessionWithID(sessionID string, hooks *frameworkSession.LoopHooks) (*frameworkSession.Session, error) {
	if r == nil {
		return nil, errors.New("seelebridge: runtime is nil")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("seelebridge: subagent session ID is required")
	}
	if r.subagentSessions != nil && r.subagentSessions.Session(sessionID) != nil {
		return nil, fmt.Errorf("seelebridge: subagent session %q already exists", sessionID)
	}
	components := frameworkSession.SessionComponents{
		Agent:     r.agt,
		Context:   r.nodeContextComponents(),
		Hooks:     hooks,
		Telemetry: r.hook,
		SessionID: sessionID,
		ModelName: r.model,
	}
	if router := r.durableHistoryRouter(); router != nil {
		history := sessionstore.NewDurableHistory(router, sessionID)
		history.SetWorkspaceResolver(r.sessionWorkspaceFor(sessionID))
		components.History = history
	}
	sess, err := frameworkSession.NewSession(components)
	if err != nil {
		return nil, fmt.Errorf("seelebridge: create subagent session: %w", err)
	}
	if r.subagentSessions != nil {
		r.subagentSessions.Register(sessionID, sess, "")
	}
	return sess, nil
}

// SubagentSessionCount 返回当前注册的子代理会话数（监控/测试）。
func (r *Runtime) SubagentSessionCount() int {
	if r == nil || r.subagentSessions == nil {
		return 0
	}
	return r.subagentSessions.Count()
}
