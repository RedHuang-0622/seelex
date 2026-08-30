package adapters

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/telemetry"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application/contract"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
)

var _ contract.RuntimePort = RuntimePort{}

// 编译期断言：EnginePort 实现会话路由扩展（多会话并行执行）。
var _ contract.SessionChatEngine = (*EnginePort)(nil)

type EnginePort struct {
	engine    ReactorEngine
	newEngine ReactorEngineFactory
	tracer    *telemetry.MemoryTracer // trace 视图查询源（slice 8：telemetry）
	mu        sync.RWMutex
	sessionID string
	// engines 是会话级引擎注册表（sessionID → ReactorEngine）。活跃会话
	// 的引擎与 engine/sessionID 字段保持一致；M1 起切换会话不再销毁其它
	// 会话引擎，恢复时按需创建。
	engines map[string]ReactorEngine
	// engineCalls 是每会话进行中 ChatStream 计数（会话级锁语义：历史替换
	// 只在该会话无活跃调用时才安装干净引擎）。
	engineCalls    map[string]int
	pendingHistory []types.Message
	pendingSession string
	prepareHistory func(string, []types.Message)
	systemPrompt   string
	maxLoops       int
	sessionBacked  bool
	releaseWorking bool
	// nodeConversations 是子代理会话记录查询（节点详情数据面；Runtime 注入，
	// 只读子代理 actor，安全——不经过主会话锁）。
	nodeConversations func(string) ([]types.Message, bool)
	// nodeContextSnapshot 是子代理结构化上下文查询（详情弹窗"上下文"标签；
	// Runtime 注入，只读子代理 actor，安全）。
	nodeContextSnapshot func(string) (*snapshot.ContextSnapshot, bool)
	// nodeToolResult 是子代理工具结果读回（ref 带 node:<nodeID>: 前缀；
	// Runtime 注入，只读子代理归档器，安全）。
	nodeToolResult func(string, string) (string, bool)
	// nodeWorktree 是节点 worktree 现场查询（失败现场恢复入口；Runtime 注入）。
	nodeWorktree func(string) (seelebridge.NodeWorktreeInfo, bool)
	// subAgentTree 是 fork 子代理树投影查询（GUI 树视图数据源；Runtime
	// 注入，内存态只读 actor，安全）。
	subAgentTree func() []dto.SubAgentTreeNode
	// subagentLive 是 node 第一视角实时流订阅源（Runtime 注入，即时输出面；
	// 返回历史回放 + 实时通道 + 取消）。
	subagentLive func(nodeID string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error)
}

// EnginePortDeps 是 EnginePort 的启动期装配（main.go 装配点一次注入；
// 对应原散装单字段 setter，统一走 Deps 结构——禁止再新增单字段 setter）。
type EnginePortDeps struct {
	// NodeConversations 子代理会话记录查询（节点详情数据面；只读子代理
	// actor，安全——不经过主会话锁）。
	NodeConversations func(string) ([]types.Message, bool)
	// NodeContext 子代理结构化上下文快照查询（详情弹窗"上下文"标签）。
	NodeContext func(string) (*snapshot.ContextSnapshot, bool)
	// NodeToolResult 子代理工具结果读回（ref 带 node:<nodeID>: 前缀）。
	NodeToolResult func(string, string) (string, bool)
	// NodeWorktree 节点 worktree 现场查询（失败现场恢复入口；只读注册表）。
	NodeWorktree func(string) (seelebridge.NodeWorktreeInfo, bool)
	// SubAgentTree fork 子代理树投影查询（GUI 树视图数据源；内存态只读
	// actor，安全）。
	SubAgentTree func() []dto.SubAgentTreeNode
	// SubagentLive node 第一视角实时流订阅源（历史回放 + 即时输出面）。
	SubagentLive func(nodeID string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error)
	// PrepareHistory Runtime-owned one-shot handoff（framework Session 的
	// DurableHistory.Load 使用；启动期配置，必须在并发开始前完成）。
	PrepareHistory func(string, []types.Message)
}

// ApplyDeps 一次注入 EnginePort 启动期装配（幂等；覆盖旧值）。
// 调用方保证在并发应用工作开始前完成注入。
func (port *EnginePort) ApplyDeps(deps EnginePortDeps) {
	if port == nil {
		return
	}
	port.mu.Lock()
	defer port.mu.Unlock()
	port.nodeConversations = deps.NodeConversations
	port.nodeContextSnapshot = deps.NodeContext
	port.nodeToolResult = deps.NodeToolResult
	port.nodeWorktree = deps.NodeWorktree
	port.subAgentTree = deps.SubAgentTree
	port.subagentLive = deps.SubagentLive
	port.prepareHistory = deps.PrepareHistory
}

// ReactorEngine is the small framework surface the application adapter
// needs. Keeping construction behind a factory makes a new application session
// a new ReAct loop, rather than a logical ID layered over an old loop.
// 基础面（ChatStream/ClearHistory/SessionID/SetSystemPrompt/SetMaxLoops）
// 与 contract.ChatEngine 共享（contract.EngineBase）；History/AppendHistory
// 使用框架消息类型 types.Message，属于适配面的扩展部分。
type ReactorEngine interface {
	contract.EngineBase
	History() []types.Message
	AppendHistory(types.Message)
}

type ReactorEngineFactory func(sessionID string) ReactorEngine

func NewEnginePort(eng ReactorEngine, newEngine ReactorEngineFactory, tracer *telemetry.MemoryTracer) *EnginePort {
	port := &EnginePort{
		engine:      eng,
		newEngine:   newEngine,
		tracer:      tracer,
		engines:     make(map[string]ReactorEngine),
		engineCalls: make(map[string]int),
	}
	if eng == nil {
		return port
	}
	port.sessionID = eng.SessionID()
	port.engines[port.sessionID] = eng
	port.engineCalls[port.sessionID] = 0
	if _, ok := eng.(*frameworkSession.Session); ok {
		port.sessionBacked = true
	}
	return port
}

// SessionBacked 报告底层 reactor 是否为 session.Session。
// 新 Session 装配下 OnIterationComplete 在 Session 锁内同步执行，
// 应用层不得在回调中重入 Engine 历史操作（见 chat.go ToolHookBridge）。
func (port *EnginePort) SessionBacked() bool { return port.sessionBacked }

func (port *EnginePort) ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error) {
	port.mu.Lock()
	current := port.engine
	port.engineCalls[port.sessionID]++
	port.mu.Unlock()
	if current == nil {
		port.mu.Lock()
		port.engineCalls[port.sessionID]--
		port.mu.Unlock()
		return "", fmt.Errorf("engine is unavailable")
	}
	result, err := current.ChatStream(ctx, input, onChunk)

	port.mu.Lock()
	port.engineCalls[port.sessionID]--
	if port.engineCalls[port.sessionID] == 0 && len(port.pendingHistory) > 0 {
		port.installSessionEngineLocked(port.pendingSession, port.pendingHistory)
		port.pendingHistory = nil
		port.pendingSession = ""
	}
	port.mu.Unlock()
	return result, err
}

// ChatStreamFor 是会话路由的 ChatStream：多会话并行执行时，runChat 用
// 显式 sessionID 调用，避免活跃会话切换把请求打到别的会话引擎上。
// 会话引擎未注册时回退到当前活跃引擎（单会话兼容）；两者皆不可用返回
// 错误。
func (port *EnginePort) ChatStreamFor(sessionID string, ctx context.Context, input string, onChunk func(string)) (string, error) {
	port.mu.Lock()
	engine := port.engineForSessionLocked(sessionID)
	if engine == nil {
		port.mu.Unlock()
		return "", fmt.Errorf("engine for session %q is unavailable", sessionID)
	}
	port.engineCalls[sessionID]++
	port.mu.Unlock()

	result, err := engine.ChatStream(ctx, input, onChunk)

	port.mu.Lock()
	port.engineCalls[sessionID]--
	if port.engineCalls[sessionID] == 0 && port.pendingSession == sessionID && len(port.pendingHistory) > 0 {
		port.installSessionEngineLocked(sessionID, port.pendingHistory)
		port.pendingHistory = nil
		port.pendingSession = ""
	}
	port.mu.Unlock()
	return result, err
}

// engineForSessionLocked 返回指定会话的引擎；未注册会话返回 nil（阶段 0：
// 取消活跃引擎回退，杜绝后台提交打到活跃会话引擎，对应 P5）。调用方必须
// 持有 port.mu。
func (port *EnginePort) engineForSessionLocked(sessionID string) ReactorEngine {
	if engine, ok := port.engines[sessionID]; ok && engine != nil {
		return engine
	}
	return nil
}

// HistoryFor 返回指定会话引擎的历史（只读拷贝）。
func (port *EnginePort) HistoryFor(sessionID string) []contract.EngineMessage {
	return adaptMessages(port.RawHistoryFor(sessionID))
}

// RawHistoryFor 返回指定会话引擎的原始历史（只读拷贝）。
func (port *EnginePort) RawHistoryFor(sessionID string) []types.Message {
	port.mu.RLock()
	defer port.mu.RUnlock()
	engine := port.engineForSessionLocked(sessionID)
	if engine == nil {
		return nil
	}
	return append([]types.Message(nil), engine.History()...)
}

// AppendHistoryFor 追加消息到指定会话引擎历史。
func (port *EnginePort) AppendHistoryFor(sessionID string, msg types.Message) {
	port.mu.RLock()
	engine := port.engineForSessionLocked(sessionID)
	port.mu.RUnlock()
	if engine != nil {
		engine.AppendHistory(msg)
	}
}

// ClearHistoryFor 清空指定会话引擎历史。
func (port *EnginePort) ClearHistoryFor(sessionID string) {
	port.mu.Lock()
	engine := port.engineForSessionLocked(sessionID)
	port.mu.Unlock()
	if engine != nil {
		engine.ClearHistory()
	}
}

// SetSystemPromptFor 设置指定会话引擎的 system prompt。
func (port *EnginePort) SetSystemPromptFor(sessionID, prompt string) {
	port.mu.RLock()
	engine := port.engineForSessionLocked(sessionID)
	port.mu.RUnlock()
	if engine != nil {
		engine.SetSystemPrompt(prompt)
	}
}

// HasSession 报告目标会话引擎是否已实例化。
func (port *EnginePort) HasSession(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	port.mu.RLock()
	defer port.mu.RUnlock()
	engine, ok := port.engines[sessionID]
	return ok && engine != nil
}

// ReplaceHistoryFor 是会话内历史替换（contract 版）：替换指定会话引擎
// 历史，不切换活跃会话。
func (port *EnginePort) ReplaceHistoryFor(sessionID string, history []contract.EngineMessage) error {
	return port.ReplaceRawHistoryFor(sessionID, restoreMessages(history))
}

// ReplaceRawHistoryFor 是 ReplaceHistoryFor 的原始消息版本。
func (port *EnginePort) ReplaceRawHistoryFor(sessionID string, history []types.Message) error {
	return port.replaceRawHistoryFor(sessionID, history)
}

// NodeSessionConversation 转发子代理会话记录查询（节点详情数据面；
// 查询源经 ApplyDeps 注入，只读子代理 actor，安全）。
func (port *EnginePort) NodeSessionConversation(nodeID string) ([]types.Message, bool) {
	if port == nil || port.nodeConversations == nil {
		return nil, false
	}
	return port.nodeConversations(nodeID)
}

// NodeContextSnapshot 转发子代理结构化上下文查询（详情弹窗"上下文"标签；
// 查询源经 ApplyDeps 注入，只读子代理 actor，安全）。
func (port *EnginePort) NodeContextSnapshot(nodeID string) (*snapshot.ContextSnapshot, bool) {
	if port == nil || port.nodeContextSnapshot == nil {
		return nil, false
	}
	return port.nodeContextSnapshot(nodeID)
}

// NodeToolResult 转发子代理工具结果读回（ref 带 node:<nodeID>: 前缀；
// 查询源经 ApplyDeps 注入，只读子代理归档器，安全）。
func (port *EnginePort) NodeToolResult(nodeID, ref string) (string, bool) {
	if port == nil || port.nodeToolResult == nil {
		return "", false
	}
	return port.nodeToolResult(nodeID, ref)
}

// NodeWorktreeInfoFor 转发节点 worktree 现场查询（失败现场恢复入口；
// 查询源经 ApplyDeps 注入，只读注册表，安全）。
func (port *EnginePort) NodeWorktreeInfoFor(nodeID string) (seelebridge.NodeWorktreeInfo, bool) {
	if port == nil || port.nodeWorktree == nil {
		return seelebridge.NodeWorktreeInfo{}, false
	}
	return port.nodeWorktree(nodeID)
}

// SubAgentTree 转发 fork 子代理树投影查询（GUI 树视图数据源；内存态
// 只读 actor，安全——不触碰主会话锁）。
func (port *EnginePort) SubAgentTree() []dto.SubAgentTreeNode {
	if port == nil || port.subAgentTree == nil {
		return nil
	}
	return port.subAgentTree()
}

// SubscribeSubagentLive 订阅 node 第一视角实时流（历史回放 + 即时输出面）。
func (port *EnginePort) SubscribeSubagentLive(nodeID string) ([]dto.SubagentLiveEvent, <-chan dto.SubagentLiveEvent, func(), error) {
	if port == nil || port.subagentLive == nil {
		return nil, nil, func() {}, fmt.Errorf("subagent live stream is not configured")
	}
	return port.subagentLive(nodeID)
}

// AppendHistory 追加消息到引擎内部对话历史。
// 由 OnIterationComplete 在 ChatStream 同 goroutine 中调用，无需加锁。
func (port *EnginePort) AppendHistory(msg types.Message) {
	port.mu.RLock()
	engine := port.engine
	port.mu.RUnlock()
	if engine != nil {
		engine.AppendHistory(msg)
	}
}

func (port *EnginePort) ClearHistory() {
	port.mu.Lock()
	if port.engine != nil {
		port.engine.ClearHistory()
	}
	port.mu.Unlock()
}
func (port *EnginePort) ReplaceHistory(sessionID string, history []contract.EngineMessage) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("engine: session ID is required")
	}
	return port.ReplaceRawHistory(sessionID, restoreMessages(history))
}

func (port *EnginePort) ReplaceRawHistory(sessionID string, history []types.Message) error {
	desired := canonicalEngineHistory(history)
	port.mu.Lock()
	defer port.mu.Unlock()
	if port.engine == nil && port.newEngine == nil {
		return fmt.Errorf("engine is unavailable")
	}
	if port.engineCalls[port.sessionID] > 0 {
		// A running ReActLoop owns its in-memory slice. Keep it valid for the
		// current turn, then install a genuinely clean reactor before the next
		// request. ClearHistory deliberately retains system messages upstream,
		// so appending them again here would duplicate the prompt on every
		// compaction or recovery.
		port.replaceActiveHistoryLocked(desired)
		port.pendingHistory = append([]types.Message(nil), desired...)
		port.pendingSession = sessionID
	} else {
		port.installSessionEngineLocked(sessionID, desired)
	}
	port.sessionID = sessionID
	return nil
}

// ReplaceHistoryFor 是会话内历史替换（M2 后台并行）：替换指定会话引擎的
// 历史，但不切换活跃会话。context 装配/恢复路径（PrepareExecutionContextFor、
// removeProviderContextRecovery 等）按执行会话替换其自身历史；目标会话
// 未注册时按需创建（工厂可用）。
func (port *EnginePort) replaceRawHistoryFor(sessionID string, history []types.Message) error {
	desired := canonicalEngineHistory(history)
	port.mu.Lock()
	defer port.mu.Unlock()
	if sessionID == port.sessionID {
		// 目标是当前活跃会话：与 ReplaceRawHistory 相同语义（可能延迟安装）。
		if port.engine == nil && port.newEngine == nil {
			return fmt.Errorf("engine is unavailable")
		}
		if port.engineCalls[port.sessionID] > 0 {
			port.replaceActiveHistoryLocked(desired)
			port.pendingHistory = append([]types.Message(nil), desired...)
			port.pendingSession = sessionID
		} else {
			port.installSessionEngineLocked(sessionID, desired)
		}
		return nil
	}
	// 非活跃目标会话：会话内替换，不切换活跃。
	engine, ok := port.engines[sessionID]
	if !ok || engine == nil {
		if port.newEngine == nil {
			return fmt.Errorf("engine for session %q is unavailable", sessionID)
		}
		fresh := port.newEngine(sessionID)
		if fresh == nil {
			return fmt.Errorf("engine for session %q is unavailable", sessionID)
		}
		port.engines[sessionID] = fresh
		port.engineCalls[sessionID] = 0
		engine = fresh
	}
	engine.ClearHistory()
	for _, message := range desired {
		engine.AppendHistory(message)
	}
	if port.prepareHistory != nil {
		port.prepareHistory(sessionID, desired)
	}
	return nil
}

func (port *EnginePort) replaceActiveHistoryLocked(history []types.Message) {
	port.engine.ClearHistory()
	hasSystem := false
	for _, message := range port.engine.History() {
		hasSystem = hasSystem || message.Role == "system"
	}
	for _, message := range history {
		if message.Role == "system" {
			if !hasSystem {
				port.engine.AppendHistory(message)
				hasSystem = true
			}
			continue
		}
		port.engine.AppendHistory(message)
	}
}

// installSessionEngineLocked 为目标会话安装权威历史：优先创建全新引擎并
// 注册到会话注册表（ReplaceHistory 语义 = 干净 reactor）；工厂不可用时
// 回退为就地替换当前引擎。调用方必须持有 port.mu。
func (port *EnginePort) installSessionEngineLocked(sessionID string, history []types.Message) {
	if port.newEngine == nil {
		port.replaceActiveHistoryLocked(history)
		if port.prepareHistory != nil {
			port.prepareHistory(sessionID, history)
		}
		return
	}
	fresh := port.newEngine(sessionID)
	if fresh == nil {
		port.replaceActiveHistoryLocked(history)
		if port.prepareHistory != nil {
			port.prepareHistory(sessionID, history)
		}
		return
	}
	for _, message := range history {
		fresh.AppendHistory(message)
	}
	port.engines[sessionID] = fresh
	port.engineCalls[sessionID] = 0
	port.engine = fresh
	_, port.sessionBacked = fresh.(*frameworkSession.Session)
	if port.systemPrompt != "" {
		fresh.SetSystemPrompt(port.systemPrompt)
	}
	if port.maxLoops > 0 {
		fresh.SetMaxLoops(port.maxLoops)
	}
	if port.prepareHistory != nil {
		port.prepareHistory(sessionID, history)
	}
}

func canonicalEngineHistory(history []types.Message) []types.Message {
	canonical := make([]types.Message, 0, len(history))
	hasSystem := false
	for _, message := range history {
		if message.Role == "system" {
			if hasSystem {
				continue
			}
			hasSystem = true
		}
		canonical = append(canonical, message)
	}
	return canonical
}
func (port *EnginePort) StartSession() string {
	port.mu.Lock()
	defer port.mu.Unlock()
	if port.newEngine == nil {
		return ""
	}
	fresh := port.newEngine("")
	if fresh == nil {
		return ""
	}
	port.engines[fresh.SessionID()] = fresh
	port.engineCalls[fresh.SessionID()] = 0
	port.engine = fresh
	port.sessionID = fresh.SessionID()
	_, port.sessionBacked = fresh.(*frameworkSession.Session)
	if port.systemPrompt != "" {
		fresh.SetSystemPrompt(port.systemPrompt)
	}
	if port.maxLoops > 0 {
		fresh.SetMaxLoops(port.maxLoops)
	}
	return port.sessionID
}

// ActivateSession 把活跃引擎切换到目标会话（首次激活按需经工厂创建并
// 注册）。只切换活跃别名，不销毁其它会话引擎。会话 ID 为空返回错误。
func (port *EnginePort) ActivateSession(sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("engine: session ID is required")
	}
	port.mu.Lock()
	defer port.mu.Unlock()
	engine, ok := port.engines[sessionID]
	if !ok || engine == nil {
		if port.newEngine == nil {
			return fmt.Errorf("engine: session %q is not materialized and no factory is configured", sessionID)
		}
		engine = port.newEngine(sessionID)
		if engine == nil {
			return fmt.Errorf("engine: factory returned nil for session %q", sessionID)
		}
		port.engines[sessionID] = engine
		port.engineCalls[sessionID] = 0
		if port.systemPrompt != "" {
			engine.SetSystemPrompt(port.systemPrompt)
		}
		if port.maxLoops > 0 {
			engine.SetMaxLoops(port.maxLoops)
		}
	}
	port.engine = engine
	port.sessionID = sessionID
	_, port.sessionBacked = engine.(*frameworkSession.Session)
	return nil
}

// ResumeSession 为目标会话恢复引擎历史并切换为活跃引擎：会话已有引擎时
// 就地重置安装（保留会话级引擎实例），否则经工厂创建后注册。
func (port *EnginePort) ResumeSession(sessionID string, history []contract.EngineMessage) error {
	return port.ResumeRawSession(sessionID, restoreMessages(history))
}

// ResumeRawSession 是 ResumeSession 的原始消息版本（框架消息类型）。
func (port *EnginePort) ResumeRawSession(sessionID string, history []types.Message) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("engine: session ID is required")
	}
	desired := canonicalEngineHistory(history)
	port.mu.Lock()
	defer port.mu.Unlock()
	if port.newEngine == nil && port.engine == nil {
		return fmt.Errorf("engine is unavailable")
	}
	if port.engineCalls[sessionID] > 0 {
		// 目标会话自身忙时才延迟安装；其它会话运行中不阻塞本会话恢复
		// （M2 并行语义：空闲目标可立即安装，避免触碰运行中会话的引擎锁）。
		port.pendingHistory = append([]types.Message(nil), desired...)
		port.pendingSession = sessionID
		return nil
	}
	engine, ok := port.engines[sessionID]
	if !ok || engine == nil {
		if port.newEngine == nil {
			port.replaceActiveHistoryLocked(desired)
			if port.prepareHistory != nil {
				port.prepareHistory(sessionID, desired)
			}
			port.sessionID = sessionID
			return nil
		}
		engine = port.newEngine(sessionID)
		if engine == nil {
			return fmt.Errorf("engine: factory returned nil for session %q", sessionID)
		}
		port.engines[sessionID] = engine
		port.engineCalls[sessionID] = 0
	}
	engine.ClearHistory()
	for _, message := range desired {
		engine.AppendHistory(message)
	}
	port.engine = engine
	port.sessionID = sessionID
	_, port.sessionBacked = engine.(*frameworkSession.Session)
	if port.systemPrompt != "" {
		engine.SetSystemPrompt(port.systemPrompt)
	}
	if port.maxLoops > 0 {
		engine.SetMaxLoops(port.maxLoops)
	}
	if port.prepareHistory != nil {
		port.prepareHistory(sessionID, desired)
	}
	return nil
}

// EnableWorkingHistoryRelease marks this adapter as backed by DurableHistory.
func (port *EnginePort) EnableWorkingHistoryRelease() {
	port.mu.Lock()
	port.releaseWorking = true
	port.mu.Unlock()
}

// ReleaseWorkingHistoryFor clears only the target session's provider working
// view. The next turn cold-loads a bounded tail from the durable owner.
// 会话参数化：后台会话收尾只清自己的引擎，不清活跃会话（对应 P4）。
func (port *EnginePort) ReleaseWorkingHistoryFor(sessionID string) {
	port.mu.Lock()
	defer port.mu.Unlock()
	if !port.releaseWorking {
		return
	}
	engine := port.engines[sessionID]
	if engine == nil || port.engineCalls[sessionID] > 0 {
		return
	}
	engine.ClearHistory()
}
func (port *EnginePort) SessionID() string {
	port.mu.RLock()
	defer port.mu.RUnlock()
	return port.sessionID
}
func (port *EnginePort) SetSystemPrompt(prompt string) {
	port.mu.Lock()
	port.systemPrompt = prompt
	engine := port.engine
	port.mu.Unlock()
	if engine != nil {
		engine.SetSystemPrompt(prompt)
	}
}
func (port *EnginePort) SetMaxLoops(n int) {
	port.mu.Lock()
	port.maxLoops = n
	engine := port.engine
	port.mu.Unlock()
	if engine != nil {
		engine.SetMaxLoops(n)
	}
}
func (port *EnginePort) TraceText() string {
	if port.tracer == nil {
		return ""
	}
	view, err := port.tracer.Query(context.Background(), telemetry.Query{Limit: 200})
	if err != nil {
		return ""
	}
	var builder strings.Builder
	for _, trace := range view.Traces {
		builder.WriteString(fmt.Sprintf("追踪 %s\n", trace.TraceID))
		writeSpanSnapshot(&builder, trace.Root, 0)
	}
	if len(view.Events) > 0 {
		builder.WriteString(fmt.Sprintf("\n生命周期事件 %d 条\n", len(view.Events)))
		for _, event := range view.Events {
			builder.WriteString(fmt.Sprintf("  %s %s %s\n", event.Timestamp.Format("15:04:05"), event.Type, event.Status))
		}
	}
	return builder.String()
}
func (port *EnginePort) TokenCount() string {
	if port.tracer == nil {
		return "0"
	}
	view, err := port.tracer.Query(context.Background(), telemetry.Query{Limit: 200})
	if err != nil {
		return "0"
	}
	total := 0
	for _, event := range view.Events {
		if event.Type != telemetry.EventLLMAfter {
			continue
		}
		total += attrTelemetryInt(event.Attributes, telemetry.AttributeGenAIUsageInput)
		total += attrTelemetryInt(event.Attributes, telemetry.AttributeGenAIUsageOutput)
	}
	return strconv.Itoa(total)
}

// writeSpanSnapshot 递归渲染遥测 span 树（trace 视图文本）。
func writeSpanSnapshot(builder *strings.Builder, span telemetry.SpanSnapshot, depth int) {
	if span.Name == "" {
		return
	}
	indent := strings.Repeat("  ", depth)
	status := string(span.Status)
	if status == "" {
		status = "unset"
	}
	model := span.Attributes[telemetry.AttributeGenAIRequestModel]
	if model == nil {
		model = ""
	}
	builder.WriteString(fmt.Sprintf("%s%s %s [%s] %v\n", indent, span.Name, status, span.Kind, model))
	for _, child := range span.Children {
		writeSpanSnapshot(builder, child, depth+1)
	}
}

func attrTelemetryInt(attributes telemetry.Attributes, key string) int {
	if attributes == nil {
		return 0
	}
	value, ok := attributes[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		n, err := strconv.Atoi(typed)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}
func (port *EnginePort) History() []contract.EngineMessage {
	return adaptMessages(port.RawHistory())
}
func (port *EnginePort) RawHistory() []types.Message {
	port.mu.RLock()
	defer port.mu.RUnlock()
	if port.engine == nil {
		return nil
	}
	return append([]types.Message(nil), port.engine.History()...)
}
