package session

// session_factory.go 是**子代理独立会话的工厂**（P21-5「域方法随类型走」：从
// `backend/engine/assemble/runtime_subagent_session.go` 搬进本域；plan §2.5 的
// `runtime_subagent_session.go` → `backend/multiagent/subagent/`）。
//
// 口径（thin-wrapper-session-design.md §3.3，UC7）：plan 节点 = 一个 SubagentSession
// （own loop/视图/历史）。工厂以**显式会话 ID**（= 节点 ID）创建它并注册进
// `SubagentSessions` registry；结束经 Unregister 回收（导出快照 → merge-back 主会话）。
//
// 与主会话同构（框架 Session + DurableHistory），但用**节点上下文组件**（own context/loop）：
// 组件模板（Agent / Context / Telemetry）由宿主装配后经函数值端口交进来，本域只补本次会话
// 专属的四格（`Hooks` / `SessionID` / `ModelName` / `History`）——这样域包不必认识
// agent 与 telemetry 的类型。

import (
	"errors"
	"fmt"
	"strings"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/backend/multiagent/agentsession"
	"github.com/RedHuang-0622/seelex/store"
)

// SessionFactoryDeps 是工厂需要的宿主面。
type SessionFactoryDeps struct {
	// Components 返回会话组件模板（宿主装配 Agent / Context / Telemetry）；
	// 本次会话专属的四格由工厂补上。必须非 nil。
	Components func() frameworkSession.SessionComponents
	// ModelName 取当前模型名（写进会话组件）。
	ModelName func() string
	// HistoryRouter 取历史路由（nil = 不挂 durable history，引擎侧退化为无历史）。
	HistoryRouter func() *store.Router
	// WorkspaceResolver 取该会话的"工作区解析器"（写进 durable history）。
	WorkspaceResolver func(sessionID string) func() string
	// InitialHistory supplies a persisted snapshot when a cold subagent chat
	// resumes after interruption.
	InitialHistory func(sessionID string) []types.Message
	// Registry 是子代理会话注册表（**同域**的 actor）；nil = 未装配，只建不注册。
	Registry *SubagentSessions
}

// SessionFactory 是会话工厂（无状态：状态在宿主装配的组件与同域的注册表上）。
type SessionFactory struct {
	deps SessionFactoryDeps
	base *agentsession.Factory
}

// NewSessionFactory 装配一例（装配点是组合根 `assemble.Runtime` 的构造器）。
func NewSessionFactory(deps SessionFactoryDeps) *SessionFactory {
	return &SessionFactory{
		deps: deps,
		base: agentsession.New(agentsession.Deps{
			Components:        deps.Components,
			ModelName:         deps.ModelName,
			HistoryRouter:     deps.HistoryRouter,
			WorkspaceResolver: deps.WorkspaceResolver,
			InitialHistory:    deps.InitialHistory,
		}),
	}
}

// NewWithID 创建独立子代理会话单元：与主会话同构（框架 Session + DurableHistory），
// 但使用节点上下文组件（own context/loop）。会话 ID 即节点 ID；注册后可从
// `SubagentSessions` 读取运行中 History。
func (factory *SessionFactory) NewWithID(sessionID string, hooks *frameworkSession.LoopHooks) (*frameworkSession.Session, error) {
	return factory.NewWithIDFor("", sessionID, hooks)
}

// NewWithIDFor creates an independent session in an explicit main-session
// scope. It is used by the unified conversation adapter when a cold subagent
// window receives a new chat turn.
func (factory *SessionFactory) NewWithIDFor(mainSessionID, sessionID string, hooks *frameworkSession.LoopHooks) (*frameworkSession.Session, error) {
	return factory.NewWithIDForWithHistory(mainSessionID, sessionID, hooks, nil)
}

// NewWithIDForWithHistory creates a cold subagent session seeded from its last
// durable snapshot when a conversation resumes after interruption.
func (factory *SessionFactory) NewWithIDForWithHistory(mainSessionID, sessionID string, hooks *frameworkSession.LoopHooks, initial []types.Message) (*frameworkSession.Session, error) {
	if factory == nil || factory.deps.Components == nil {
		return nil, errors.New("seelex: runtime is nil")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, errors.New("seelex: subagent session ID is required")
	}
	registry := factory.deps.Registry
	if registry != nil && registry.Session(sessionID) != nil {
		return nil, fmt.Errorf("seelex: subagent session %q already exists", sessionID)
	}
	sess, err := factory.base.NewWithIDWithHistory(sessionID, hooks, initial)
	if err != nil {
		return nil, fmt.Errorf("seelex: create subagent session: %w", err)
	}
	if registry != nil {
		registry.RegisterFor(mainSessionID, sessionID, sess, "")
	}
	return sess, nil
}

// Count 返回当前注册的子代理会话数（监控/测试）。
func (factory *SessionFactory) Count() int {
	if factory == nil {
		return 0
	}
	return factory.deps.Registry.Count()
}
