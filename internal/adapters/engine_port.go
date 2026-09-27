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
	engineCalls map[string]int
	// pendingHistory 是「目标会话此刻有回合在飞，等它收尾再装」的历史登记表，
	// 按会话键控。为什么不能是单槽（一份历史 + 一个目标会话号）：S3b 之后，
	// **任何**会话（含非活跃会话）在飞时折叠都要登记待安装，单槽既装不下多个
	// 会话，也让 legacy ChatStream 把别的会话的待安装装到自己头上。
	pendingHistory map[string][]types.Message
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
	// nodeStageLogs 是 node 第一视角阶段日志历史查询（详情弹窗"第一视角"
	// 历史回放源；Runtime 注入，只读子代理 actor，安全）。
	nodeStageLogs func(nodeID string) []dto.NodeStageLog
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
	// NodeStageLogs node 第一视角阶段日志历史查询（详情弹窗"第一视角"
	// 历史回放源；只读子代理 actor，安全）。
	NodeStageLogs func(nodeID string) []dto.NodeStageLog
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
	port.nodeStageLogs = deps.NodeStageLogs
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

// historyQueuer 是「能当场接收一次历史替换」的引擎能力（Seele session.Session 实现）。
//
// 为什么它是宿主迁移的支点：回合进行中，引擎把替换**排队到下一个检查点**（模型调用
// 前 / assistant 落历史后 / tool 结果 append 前后），空闲时立即应用；因此宿主不再需要
// 「等这个会话的回合收尾，再换一台新引擎把历史装上去」那套登记表。折叠在下一次模型
// 请求前就生效，这也正是自动压缩要的语义（它本就要在下一次请求才生效）。
//
// 没有实现它的引擎（只支持 Clear/Append 的旧替身与 legacy 引擎）仍走 pendingHistory
// 登记路径，行为与之前一致。
type historyQueuer interface {
	ReplaceHistory([]types.Message) error
}

// queueSessionHistory 把一次历史替换交给在飞会话的引擎自己排队。返回 false = 该引擎
// 没有这个能力，调用方回退登记路径。
//
// 为什么可以持着 port.mu 调用：新引擎的 ReplaceHistory 只做两件短事——按当前工作
// 历史校验替换是否丢掉在飞 tool_call 单元、把替换挂进检查点队列，**不等任何回合**。
// 旧实现里"改在飞会话的历史"会撞上整轮持有不放的会话锁（S3b 的三处引信），所以当时
// 只能登记到收尾再装。
func queueSessionHistory(engine ReactorEngine, history []types.Message) (bool, error) {
	queuer, ok := engine.(historyQueuer)
	if !ok {
		return false, nil
	}
	return true, queuer.ReplaceHistory(history)
}

type ReactorEngineFactory func(sessionID string) ReactorEngine

func NewEnginePort(eng ReactorEngine, newEngine ReactorEngineFactory, tracer *telemetry.MemoryTracer) *EnginePort {
	port := &EnginePort{
		engine:         eng,
		newEngine:      newEngine,
		tracer:         tracer,
		engines:        make(map[string]ReactorEngine),
		engineCalls:    make(map[string]int),
		pendingHistory: make(map[string][]types.Message),
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
	sessionID := port.sessionID
	port.engineCalls[sessionID]++
	port.mu.Unlock()
	if current == nil {
		port.mu.Lock()
		port.engineCalls[sessionID]--
		port.mu.Unlock()
		return "", fmt.Errorf("engine is unavailable")
	}
	// 会话标签注入（G1/M8）：legacy 活跃别名路径也要让 telemetry 事件带上
	// 会话 ID，否则 SessionTagHook 在生产侧无标签可打，per-session trace
	// 查询（TokenCountFor/TraceText 按会话）退回全局求和。
	ctx = seelebridge.WithTelemetrySessionID(ctx, sessionID)
	result, err := current.ChatStream(ctx, input, onChunk)

	port.mu.Lock()
	port.engineCalls[sessionID]--
	if port.engineCalls[sessionID] == 0 {
		port.installPendingLocked(sessionID)
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

	// 会话入口 ctx 注入（G1/M8）：runChat 的会话 ID 在这里转写为 telemetry
	// 路由键。Seele loop 原样把该 ctx 传给 hooks 与工具 handler，因此 llm/tool
	// 的 intent-effect 事件都带 session_id，可按会话查询（INV-T1/T2 生产成立）。
	ctx = seelebridge.WithTelemetrySessionID(ctx, sessionID)
	result, err := engine.ChatStream(ctx, input, onChunk)

	port.mu.Lock()
	port.engineCalls[sessionID]--
	if port.engineCalls[sessionID] == 0 {
		port.installPendingLocked(sessionID)
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
//
// 锁纪律（S3b）：port.mu 只做查表，engine.History() 一定在锁外调。Session.mu
// 由 ChatStream 从进函数持到出函数，锁内读它等于「持着进程级 RLock 等一把整轮
// 不放手的锁」——Go 的 RWMutex 在有写者排队后连新读者也停，于是别的会话连开回合
// 都开不了。挪到锁外后，卡住的只有本次调用自己（语义仍是权威历史），进程不冻结。
// 代价与 AppendHistoryFor/ClearHistoryFor 同口径：查表后引擎可能已被替换，读到的是
// 拿到的那一份实例。
func (port *EnginePort) RawHistoryFor(sessionID string) []types.Message {
	port.mu.RLock()
	engine := port.engineForSessionLocked(sessionID)
	port.mu.RUnlock()
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
	port.mu.Lock()
	port.systemPrompt = prompt
	engine := port.engineForSessionLocked(sessionID)
	port.mu.Unlock()
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

// NodeStageLogs 转发 node 第一视角阶段日志历史查询（详情弹窗"第一视角"
// 历史回放源；查询源经 ApplyDeps 注入，只读子代理 actor，安全）。
func (port *EnginePort) NodeStageLogs(nodeID string) []dto.NodeStageLog {
	if port == nil || port.nodeStageLogs == nil {
		return nil
	}
	return port.nodeStageLogs(nodeID)
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

// ClearHistory 清空活跃会话引擎历史。锁纪律同 ClearHistoryFor：port.mu 只解析
// 别名，engine 调用在锁外（锁内等 Session.mu 会把全进程排在 port.mu 上）。
func (port *EnginePort) ClearHistory() {
	port.mu.RLock()
	engine := port.engine
	port.mu.RUnlock()
	if engine != nil {
		engine.ClearHistory()
	}
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
	// A running ReActLoop used to own its in-memory slice and overwrite the session
	// view at turn exit, so touching that engine then bought nothing and would block
	// right here on a lock held for the whole round. Seele no longer works that way:
	// the engine queues a replacement submitted during a turn and applies it at its
	// next checkpoint (before the next model request), so handing the history to the
	// running session is now both safe and useful — it is what makes an in-turn fold
	// visible to the *next* request of that same turn. Engines without that capability
	// still take the old route (register and install once the turn releases
	// port.engineCalls, see installPendingLocked).
	if port.engineCalls[sessionID] > 0 {
		if queued, err := queueSessionHistory(port.engineForSessionLocked(sessionID), desired); queued {
			return err
		}
		port.armPendingLocked(sessionID, desired)
		port.activateLocked(sessionID)
		return nil
	}
	// 先装后切：install 已把新引擎登记在该会话号下，别名随后指过去即可。反过来
	// （先 activate 再 install）会让工厂白造一台——install 又造一个新的换上。
	port.installSessionEngineLocked(sessionID, desired)
	port.activateLocked(sessionID)
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
		// 目标是当前活跃会话：与 ReplaceRawHistory 相同语义（可能排队到检查点，
		// 也可能换一台干净引擎）。
		if port.engine == nil && port.newEngine == nil {
			return fmt.Errorf("engine is unavailable")
		}
		if port.engineCalls[sessionID] > 0 {
			if queued, err := queueSessionHistory(port.engineForSessionLocked(sessionID), desired); queued {
				return err
			}
			port.armPendingLocked(sessionID, desired)
			return nil
		}
		port.installSessionEngineLocked(sessionID, desired)
		return nil
	}
	// 非活跃目标会话：会话内替换，不切换活跃。这里原本没有「目标在飞就先登记」
	// 这一步，等于持着进程级 port.mu 去等一把整轮不放手的会话锁——目标会话恰好
	// 开着回合时，其它会话连开回合都要排在 port.mu 后面（S3b 写面引信）。
	engine := port.engineForSessionLocked(sessionID)
	if engine == nil {
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
	if port.engineCalls[sessionID] > 0 {
		if queued, err := queueSessionHistory(engine, desired); queued {
			return err
		}
		port.armPendingLocked(sessionID, desired)
		return nil
	}
	installHistoryInPlace(engine, desired)
	if port.prepareHistory != nil {
		port.prepareHistory(sessionID, desired)
	}
	return nil
}

// replaceTargetHistoryLocked 就地重建目标会话引擎的历史（工厂不可用时的退路）。
// 单引擎装配下注册表里最多只有一台引擎，它就是目标会话的承载者：顺手登记到目标
// 会话号下，后续别名解析才找得到，重建也不会被悄悄丢掉。调用方必须持 port.mu，
// 且已确认目标会话无回合在飞。
func (port *EnginePort) replaceTargetHistoryLocked(sessionID string, history []types.Message) {
	engine := port.engineForSessionLocked(sessionID)
	if engine == nil {
		if port.engine == nil {
			return
		}
		engine = port.engine
		port.engines[sessionID] = engine
		port.engineCalls[sessionID] = 0
	}
	installHistoryInPlace(engine, history)
}

// installHistoryInPlace 用 history 重建 engine 的 provider 历史，保留该会话的引擎
// 实例。调用方必须保证这个引擎此刻没有回合在飞（这正是"空闲会话"的落点）。
//
// 优先让引擎自己装（Seele session.Session.ReplaceHistory）：一次短临界区完成整份替换，
// 语义是"工作历史就是这份"。引擎拒绝时（自定义 Loop 不支持替换 / 替换会丢掉在飞单元）
// 退回 Clear + 逐条 Append 的 legacy 形状——本函数的契约已保证没有回合在飞，兼容路径
// 不会撞上在飞校验。
//
// system 行只补缺、不重加：上游 ClearHistory 刻意保留 system 消息，再把 desired 里
// 的 system 追加一遍等于每次压缩/恢复都复制一份 prompt。
func installHistoryInPlace(engine ReactorEngine, history []types.Message) {
	if queuer, ok := engine.(historyQueuer); ok && queuer.ReplaceHistory(history) == nil {
		return
	}
	engine.ClearHistory()
	hasSystem := false
	for _, message := range engine.History() {
		hasSystem = hasSystem || message.Role == "system"
	}
	for _, message := range history {
		if message.Role == "system" {
			if !hasSystem {
				engine.AppendHistory(message)
				hasSystem = true
			}
			continue
		}
		engine.AppendHistory(message)
	}
}

// armPendingLocked 登记「该会话这次回合收尾之后要装的历史」。调用方必须持 port.mu
// 且已确认该会话有回合在飞（engineCalls > 0）——登记之后绝不能碰它的引擎。
func (port *EnginePort) armPendingLocked(sessionID string, history []types.Message) {
	if port.pendingHistory == nil {
		port.pendingHistory = make(map[string][]types.Message)
	}
	port.pendingHistory[sessionID] = append([]types.Message(nil), history...)
}

// installPendingLocked 在该会话的最后一个在飞回合收尾时兑现登记的历史。调用方必须
// 持 port.mu 且 engineCalls[sessionID] 已归零，此刻装历史不会排在会话锁后面，而
// port.mu 又挡住了新回合进入（新回合要先 Lock 才能给 engineCalls 加一），因此这一
// 次安装对该会话是原子的。
func (port *EnginePort) installPendingLocked(sessionID string) {
	history, ok := port.pendingHistory[sessionID]
	if !ok {
		return
	}
	delete(port.pendingHistory, sessionID)
	port.installSessionEngineLocked(sessionID, history)
}

// activateLocked 把活跃别名（port.engine / port.sessionID）成对指向目标会话。
// 它**只查表不建引擎**：需要新引擎的调用（ReplaceRawHistory）先经 install 把引擎
// 登记在该会话号下，再指别名——反过来会白造一台。目标未注册时返回 false 且不动
// 别名。整个过程不碰任何引擎的历史方法，因此可以在持 port.mu 时安全调用。
func (port *EnginePort) activateLocked(sessionID string) bool {
	engine := port.engineForSessionLocked(sessionID)
	if engine == nil {
		return false
	}
	port.engine = engine
	port.sessionID = sessionID
	_, port.sessionBacked = engine.(*frameworkSession.Session)
	return true
}

// installSessionEngineLocked 为目标会话安装权威历史：优先创建全新引擎并
// 注册到会话注册表（ReplaceHistory 语义 = 干净 reactor）；工厂不可用时回退为
// 就地重建该会话的引擎。两处都只在目标会话此刻无回合在飞时才会被调到（判据在
// 调用方），所以就地重建不会排在会话锁后面。调用方必须持有 port.mu。
func (port *EnginePort) installSessionEngineLocked(sessionID string, history []types.Message) {
	if port.newEngine == nil {
		port.replaceTargetHistoryLocked(sessionID, history)
		if port.prepareHistory != nil {
			port.prepareHistory(sessionID, history)
		}
		return
	}
	fresh := port.newEngine(sessionID)
	if fresh == nil {
		port.replaceTargetHistoryLocked(sessionID, history)
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
	if port.sessionID == sessionID {
		// 只有目标就是活跃会话时才换别名；后台会话的折叠不得把活跃会话切走。
		port.engine = fresh
		_, port.sessionBacked = fresh.(*frameworkSession.Session)
	}
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
		port.armPendingLocked(sessionID, desired)
		return nil
	}
	engine, ok := port.engines[sessionID]
	if !ok || engine == nil {
		if port.newEngine == nil {
			// 单引擎装配（legacy/测试桩）：没有工厂就没有第二台引擎，只能就地重建
			// 这一台。它承载的是当前活跃会话，活跃会话有回合在飞时这一步会排到它的
			// 会话锁后面，所以按同一口径先登记、等那次回合收尾再装。
			if port.engine == nil {
				return fmt.Errorf("engine is unavailable")
			}
			if port.engineCalls[port.sessionID] > 0 {
				port.armPendingLocked(port.sessionID, desired)
			} else {
				installHistoryInPlace(port.engine, desired)
				if port.prepareHistory != nil {
					port.prepareHistory(sessionID, desired)
				}
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

// UnloadSession 释放指定会话的引擎实例（阶段 2 生命周期：unload 后重开走
// cold_load；活跃会话卸载时清空活跃别名）。
func (port *EnginePort) UnloadSession(sessionID string) error {
	port.mu.Lock()
	defer port.mu.Unlock()
	if port.engineCalls[sessionID] > 0 {
		return fmt.Errorf("engine: session %q is busy", sessionID)
	}
	delete(port.engines, sessionID)
	delete(port.engineCalls, sessionID)
	if port.sessionID == sessionID {
		port.engine = nil
		port.sessionID = ""
	}
	return nil
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

// TokenCountFor 返回指定会话的 token 计数（G1/M8）：只累加带该会话
// session_id 标签的 LLM usage 事件。会话标签由 ChatStreamFor/ChatStream
// 的 ctx 注入 + SessionTagHook 在生产路径打上；空会话 ID 或未命中返回 "0"。
// 未实现会话标签的旧事件（升级前存量）不计入任何会话。
func (port *EnginePort) TokenCountFor(sessionID string) string {
	if port == nil || port.tracer == nil {
		return "0"
	}
	return strconv.Itoa(seelebridge.SessionTokenCount(port.tracer, sessionID))
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

// RawHistory 返回**活跃引擎**的原始历史（只读拷贝）。
//
// 锁纪律（S3b 口径，与 RawHistoryFor / ClearHistory 同）：port.mu 只用来取出
// 引擎实例，engine.History() 一律在锁外调。port.mu 是跨会话的进程级锁，而
// engine.History() 要取那把被整个回合持有的 Session.mu——锁内调它就等于「持着
// 全进程读锁等一把整轮不放手的锁」，Go 的 RWMutex 在有写者排队后连新读者也停，
// 于是本该只卡住本次调用的等待会把所有会话的开回合/历史读一起冻住。挪到锁外后，
// 卡住的仍然只有本次调用自己（语义仍是权威历史）。
//
// 这条路径的用户可达入口：`/history` 命令、工作区切换（运行中会话的历史读）。
// 代价与 RawHistoryFor 相同：查表之后引擎可能已被替换，读到的是拿到的那一份实例。
func (port *EnginePort) RawHistory() []types.Message {
	port.mu.RLock()
	engine := port.engine
	port.mu.RUnlock()
	if engine == nil {
		return nil
	}
	return append([]types.Message(nil), engine.History()...)
}
