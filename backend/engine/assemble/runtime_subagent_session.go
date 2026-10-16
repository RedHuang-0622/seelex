package assemble

// runtime_subagent_session.go 是**子代理独立会话工厂**的宿主转发面。
//
// 落点沿革（P21-5，plan §2.5 的 `runtime_subagent_session.go` → `backend/multiagent/subagent/`）：
// 工厂与它的四条判据（显式 ID / 非空 / 重名拒绝 / 有无 history router 两条装配支路）搬进域包
// [`backend/multiagent/subagent/session_factory.go`](../multiagent/subagent/session_factory.go)
// 的 `SessionFactory`；这里只留一行薄转发——`Runtime` 的公开方法名与签名逐字不变。
//
// 组件模板（Agent / Context / Telemetry）由本层装配后经函数值端口交进域包：域包因此不必
// 认识 agent 与 telemetry 的类型，只管本次会话专属的四格（Hooks / SessionID / ModelName / History）。

import (
	"errors"

	"github.com/RedHuang-0622/Seele/session"

	subagentsession "github.com/RedHuang-0622/seelex/backend/multiagent/subagent"
)

// NewSubagentSessionWithID 创建独立子代理会话单元（域包 `SessionFactory.NewWithID`）。
func (r *Runtime) NewSubagentSessionWithID(sessionID string, hooks *session.LoopHooks) (*session.Session, error) {
	if r == nil || r.subagentSessionFactory == nil {
		return nil, errors.New("seelex: runtime is nil")
	}
	return r.subagentSessionFactory.NewWithID(sessionID, hooks)
}

// SubagentSessionCount 返回当前注册的子代理会话数（监控/测试）。
func (r *Runtime) SubagentSessionCount() int {
	if r == nil || r.subagentSessionFactory == nil {
		return 0
	}
	return r.subagentSessionFactory.Count()
}

// subagentSessionFactory 句柄的类型声明（装配见 runtime.go 的构造器）。
var _ = subagentsession.NewSessionFactory
