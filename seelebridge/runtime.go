// Package seelebridge adapts Seele framework primitives to stable Seelex APIs.
package seelebridge

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/bridge"
	frameworkevent "github.com/RedHuang-0622/Seele/event"
	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/telemetry"
	frameworkmcp "github.com/RedHuang-0622/Seele/tools/mcp"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"

	"github.com/RedHuang-0622/seelex/mcpstack"
	"github.com/RedHuang-0622/seelex/seelebridge/fs"
	"github.com/RedHuang-0622/seelex/seelebridge/security"
	"github.com/RedHuang-0622/seelex/seelebridge/task"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/sessionstore"
	"github.com/RedHuang-0622/seelex/skill"
)

// RuntimeConfig contains the Seelex-facing subset of Agent configuration.
type RuntimeConfig struct {
	MaxReplanProviderRequests int
	MaxConcurrentReplans      int
	MaxReplansPerWindow       int
	ReplanWindow              time.Duration
	PlanDecisionTimeout       time.Duration
	AccountsPath              string                 // LLM 账号配置路径
	StorePath                 string                 // 会话存储目录（空 = 不持久化）
	ToolCallTimeout           time.Duration          // 工具调用超时
	ApprovalTimeout           time.Duration          // 审批等待超时
	HeartbeatInterval         time.Duration          // workplan 心跳间隔
	HubStartupDelay           time.Duration          // Hub 启动等待时间
	SubagentMailboxSize       int                    // 子代理 merge-back 有界邮箱容量
	WindowConfig              seelexctx.WindowConfig // 滑动窗口配置段（seele.yaml；零值 = 默认，plan.md §3.7.3）
	Limits                    seelexctx.Limits       // 运行时上限（seele.yaml limits 段；零值 = 默认）
}

// Runtime is the Seelex composition root: it owns the account pool, tool
// registry, assembled Agent and the main Session, and exposes
// application-oriented facades.
type Runtime struct {
	registry  *toolsRegistryState
	pool      *accountpool.P2CPool[agent.Completer]
	completer agent.Completer
	streamer  agent.StreamCompleter
	agt       *agent.Agent

	sessionMu    sync.Mutex
	session      *session.Session // 主会话（每逻辑会话经 NewMainSession 重建）
	sessionHooks *session.LoopHooks

	// 遥测（slice 8）：内存追踪器 + 生命周期钩子。会话级 llm/tool
	// intent-effect 事件经 hook 写入 tracer；GUI/TUI 的 trace 视图经
	// tracer.Query 读取（enginePort.TraceText / TokenCount）。
	tracer *telemetry.MemoryTracer
	hook   telemetry.Hook

	model            string
	defaultAccountID string
	accountLimits    map[string]accountLimits
	accountSpecs     map[string]accountSpec

	// MCPStack 记录所有 MCP 调用的 trace（熔断事件 + 调用记录）。
	// AttachMCP 时自动启动熔断事件监听，无需手动装配。
	MCPStack *mcpstack.MCPStack

	mcpProvider          *frameworkmcp.Provider
	breaker              *breakerState // 熔断器事件 channel 状态
	branchMu             sync.RWMutex
	planExecutor         *planExecutor // Plan 执行域组件（plan_executor.go）：策略/绑定/runID/事件/replan/工厂
	visibilityProjection atomic.Pointer[RuntimeVisibilityProjection]
	// subagentContext 是子代理上下文 actor（装配件拆分第一步）：父证据
	// 的读-合并-写回与 merge-back 队列收进单一 goroutine（channel 命令 +
	// 串行处理），替代原来的 parentEvidenceMu / subagentOverflowMu 两把锁。
	subagentContext *subagentContextActor
	nodeStartedMu   sync.Mutex
	nodeStarted     map[string]struct{}
	// skills 是子代理 skill 目录的 actor 资源：skill.Registry 内部自锁
	// （读写即消息进出：All/Get 读、Register/Reload 写），见 skill/skill.go。
	// 装配一次性写入、运行期只读消费，与 filesystem actor 同构，无需外层锁。
	skills            *skill.Registry
	selectedAccountID string
	providerFilter    string
	projectScope      *security.ProjectScope
	filesystem        fs.FileSystem    // 文件系统 actor（写路径分片串行化，filesystem_actor.go）
	sandbox           security.CommandSandbox // shell 执行隔离端口（security/sandbox.go；默认 native cwd-gate）
	dockerProbe       dockerProber     // docker 守护进程探测/启动（nil → 真实实现；测试注入）
	worktreeMgr       *worktreeManager // 子代理 worktree 生命周期组件（worktree_manager.go）
	tasks             *task.TaskRegistry // task 注册表 actor（task/；todolist 融合为 kind=todo 的 task）
	scheduler         *schedulerState  // 定时周期任务 actor（scheduler.go）
	toolEvents        *subagentToolEventState

	// 子代理会话注册表组件（actor：channel 命令 + 单 goroutine，subagent_sessions.go）。
	// 运行中读子会话 History（子代理 actor 独立锁，安全）；结束后保留快照；
	// node:<nodeID>: 前缀工具结果归档器由组件托管，运行中/结束后均可读回。
	subagentSessions *subagentSessions
	// subagentTree 是 fork 子代理树注册表（内存态，不落盘）：fork 创建子代理
	// 时记录 parent/child 链与节点状态/goal/会话摘要，GUI 树视图经
	// Runtime.SubAgentTree() 读取（subagent_tree.go）。
	subagentTree        *subagentTreeState
	toolCallTimeout     time.Duration
	bashObserverMu      sync.RWMutex
	bashObserver        BashDiagnosticObserver
	planDecisionTimeout time.Duration
	approvalTimeout     time.Duration
	heartbeatInterval   time.Duration
	limits              seelexctx.Limits // seele.yaml limits 段（含默认；seelebridge 消费点读取）
	scopedToolsReady    bool

	pluginMu     sync.RWMutex
	pluginDefs   map[string]pluginDef
	activePlugin string

	permission *permissionGateState

	// 上下文控制接线（seelebridge/context_components.go）：
	// 窗口策略（RuntimeConfig.WindowConfig 构造）、会话上下文存储与
	// ProjectKnowledge 提供者为可选注入（会话恢复流程就绪后 Attach）。
	windowMu         sync.RWMutex
	window           seelexctx.WindowPolicy
	ctxStoreMu       sync.RWMutex
	ctxStore         *sessionstore.SessionContextStore
	historyRouterMu  sync.RWMutex
	historyRouter    *sessionstore.Router
	mainHistoryMu    sync.RWMutex
	mainHistory      *sessionstore.DurableHistory
	projectMu        sync.RWMutex
	projectKnowledge func() *sessionstore.ProjectRecord
	turnArchiverMu   sync.RWMutex
	turnArchiver     seelexctx.TurnArchiver // 压缩轮次原文归档（main 装配注入）
	mainSessionMu    sync.RWMutex
	mainSessionID    string // 当前主会话 ID（压缩帧 SegmentID 溯源）
	lazyMCPServerMu  sync.RWMutex
	lazyMCPServers   map[string]MCPServer // 已登记未连接的 MCP 服务器（冷启动）
}

// MainSessionID 返回当前主会话 ID（压缩帧 SegmentID 溯源；空 = 未创建）。
func (r *Runtime) MainSessionID() string {
	if r == nil {
		return ""
	}
	r.mainSessionMu.RLock()
	defer r.mainSessionMu.RUnlock()
	return r.mainSessionID
}

// SetTurnArchiver 注入压缩轮次原文归档实现（application 层持久化通道）。
// 注入后窗口外压缩会把溢出轮次原文持久化，帧 Evidence 携带读回句柄，
// 模型可经 read_compressed_turn 工具读回（压缩丢失可逆）。
func (r *Runtime) SetTurnArchiver(archiver seelexctx.TurnArchiver) {
	r.turnArchiverMu.Lock()
	defer r.turnArchiverMu.Unlock()
	r.turnArchiver = archiver
}

// Account is the non-secret account information exposed to Seelex UI.
type Account struct {
	Name     string
	Provider string
	Model    string
	Disabled bool
}

// Tool is the Seelex-facing tool summary.
type Tool struct {
	Name        string
	Description string
}

func NewRuntime(cfg RuntimeConfig) (*Runtime, error) {
	planDecisionTimeout := cfg.PlanDecisionTimeout
	if planDecisionTimeout <= 0 {
		planDecisionTimeout = time.Duration(cfg.Limits.WithDefaults().PlanDecisionTimeoutSec) * time.Second
	}
	loaded, err := loadSimplifiedConfig(cfg.AccountsPath)
	if err != nil {
		return nil, fmt.Errorf("seelebridge: load accounts: %w", err)
	}
	if len(loaded.Specs) == 0 {
		return nil, fmt.Errorf("seelebridge: accounts configuration is empty")
	}

	// 1. 账号池：accountpool.P2CPool[agent.Completer]（P2C 租约）
	pool := accountpool.New[agent.Completer]()
	if err := registerAccounts(pool, loaded.Specs); err != nil {
		return nil, err
	}
	specsByName := make(map[string]accountSpec, len(loaded.Specs))
	for _, spec := range loaded.Specs {
		specsByName[spec.Name] = spec
	}
	first := loaded.Specs[0]

	// 遥测装配：内存追踪器 + 生命周期钩子（llm/tool intent-effect 事件）。
	tracer := NewTracer()
	hook, err := NewLifecycleHook(tracer)
	if err != nil {
		return nil, fmt.Errorf("seelebridge: create lifecycle hook: %w", err)
	}

	mcpStackOpts := []mcpstack.Option{
		mcpstack.WithSessionID(fmt.Sprintf("mcp-%d", time.Now().Unix())),
	}
	if cfg.StorePath != "" {
		mcpStackOpts = append(mcpStackOpts,
			mcpstack.WithAutoSave(filepath.Join(cfg.StorePath, "mcp-traces.json")))
	}

	approvalTimeout := cfg.ApprovalTimeout
	if approvalTimeout <= 0 {
		approvalTimeout = time.Duration(cfg.Limits.WithDefaults().ApprovalTimeoutSec) * time.Second
	}
	heartbeatInterval := cfg.HeartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = time.Duration(cfg.Limits.WithDefaults().HeartbeatIntervalSec) * time.Second
	}
	r := &Runtime{
		pool:                pool,
		model:               first.Model,
		defaultAccountID:    first.Name,
		accountLimits:       loaded.Limits,
		accountSpecs:        specsByName,
		MCPStack:            mcpstack.New(mcpStackOpts...),
		projectScope:        security.NewProjectScope(),
		filesystem:          fs.NewFileSystemActor(),
		sandbox:             security.NewNativeProjectCWD(),
		tasks:               task.NewTaskRegistry(),
		scheduler:           newSchedulerState(),
		toolEvents:          newSubagentToolEventState(),
		toolCallTimeout:     cfg.ToolCallTimeout,
		planDecisionTimeout: planDecisionTimeout,
		approvalTimeout:     approvalTimeout,
		heartbeatInterval:   heartbeatInterval,
		limits:              cfg.Limits.WithDefaults(),
		pluginDefs:          make(map[string]pluginDef),
		permission:          &permissionGateState{},
		subagentSessions:    newSubagentSessions(tracer),
		subagentTree:        newSubagentTreeState(),
		subagentContext:     newSubagentContextActor(tracer),
		nodeStarted:         make(map[string]struct{}),

		window: seelexctx.NewDefaultWindowPolicy(cfg.WindowConfig),
		tracer: tracer,
		hook:   hook,
	}
	// plan 执行域组件：策略 / 绑定 / runID / 事件通道 / replan 护拦 / 子代理工厂
	// 收进 planExecutor；deps 闭包引用 r 的能力面（账号、注册表、分发、节点工厂），
	// 组件不反向依赖 Runtime（构造放在 r 就绪后，与 worktreeManager 同模式）。
	r.planExecutor = newPlanExecutor(planExecutorDeps{
		Model:               r.model,
		Heartbeat:           heartbeatInterval,
		Limits:              r.limits,
		PlanDecisionTimeout: planDecisionTimeout,
		Accounts:            r.accountSpecList,
		LoadPlanDefinition: func() (types.Tool, bool) {
			if r.registry == nil || r.registry.registry == nil {
				return types.Tool{}, false
			}
			for _, tool := range r.registry.registry.Tools() {
				if tool.Function.Name == "plan_load" {
					return tool, true
				}
			}
			return types.Tool{}, false
		},
		Dispatch:    r.agentDispatch,
		NodeFactory: r.nodeFactory,
	}, cfg.MaxConcurrentReplans, cfg.MaxReplansPerWindow, cfg.MaxReplanProviderRequests, cfg.ReplanWindow)
	// worktree 生命周期组件：项目根 / 阶段事件 / 审批门经 deps 注入，组件不反向
	// 依赖 Runtime（构造放在 r 就绪后，因为 deps 引用 r 的方法值）。
	r.worktreeMgr = newWorktreeManager(worktreeManagerDeps{
		Root:  r.projectScope.Root,
		Phase: r.appendNodePhase,
		Gate:  r.planExecutor.CurrentApprovalGate,
	})
	// The wrapper is inert until the backend diagnostic observer is enabled.
	// It brackets telemetry.After, the only framework boundary between a tool
	// registry return and the application ToolHookBridge completion callback.
	r.hook = newDiagnosticTelemetryHook(r.hook, r)

	// 2. 工具注册表：WithCallTimeout 保留工具超时语义；权限门控作为 middleware。
	r.registry = &toolsRegistryState{registry: newToolsRegistry(cfg.ToolCallTimeout, r.permission, approvalTimeout, r.subagentToolMiddleware(), r.bashDiagnosticMiddleware())}

	// 3. Completer / StreamCompleter（共享账号选择器，无 api.ChatClient 强转）
	if err := r.assembleCompleters(); err != nil {
		return nil, err
	}

	// 4. Agent 装配：agent.NewWithComponents（不启动 Hub / 账号池 / 网关）
	runtimeAdapter, err := bridge.NewRegistryRuntime(r.registry.registry,
		bridge.WithVisibilityPolicy(r.seelexVisibilityPolicy),
	)
	if err != nil {
		return nil, fmt.Errorf("seelebridge: assemble tool runtime: %w", err)
	}
	agt, err := agent.NewWithComponents(agent.Components{
		Completer:       r.completer,
		StreamCompleter: r.streamer,
		Tools:           runtimeAdapter,
	})
	if err != nil {
		return nil, fmt.Errorf("seelebridge: create agent: %w", err)
	}
	r.agt = agt

	// 5. Plan 子代理工厂：bridge.NewAgentFactory（每节点独立 Session，
	//    工作历史隔离；节点会话组件见 branch.go nodeSessionComponents）。
	planAgentFactory, err := bridge.NewAgentFactory(agt,
		bridge.WithSessionComponents(r.nodeSessionComponents()),
		bridge.WithSessionID(r.nodeSessionID),
	)
	if err != nil {
		return nil, fmt.Errorf("seelebridge: create plan agent factory: %w", err)
	}
	r.planExecutor.SetAgentFactory(planAgentFactory)
	return r, nil
}

// assembleCompleters 构造同步与流式 completer，两者共享同一个账号选择器闭包。
func (r *Runtime) assembleCompleters() error {
	completer, err := r.bridgeAccountCompleter()
	if err != nil {
		return err
	}
	r.completer = completer
	r.streamer = &streamingAccountCompleter{pool: r.pool, selector: r.accountSelector}
	return nil
}

// Agent returns the framework object used by session assembly.
func (r *Runtime) Agent() *agent.Agent { return r.agt }

// Session 返回当前主会话（尚未创建时返回 nil）。
func (r *Runtime) Session() *session.Session {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	return r.session
}

// NewMainSession 按新装配模型（session.NewSession）构造主会话。
// 每个逻辑会话（StartSession / resume）重建一个 Session，SessionID 为空时
// 由 Seelex 生成。AttachHistoryRouter 已装配时，框架 working history 使用
// 同一 SessionID 的 DurableHistory；完整 SessionRecord 仍由 Application
// 的 Router SaveCommit 负责。
func (r *Runtime) NewMainSession(hooks *session.LoopHooks) (*session.Session, error) {
	return r.newMainSession("", hooks)
}

// NewMainSessionWithID keeps the framework Session identity aligned with the
// application session key used by resume and durable storage.
func (r *Runtime) NewMainSessionWithID(sessionID string, hooks *session.LoopHooks) (*session.Session, error) {
	return r.newMainSession(sessionID, hooks)
}

// AttachHistoryRouter installs the provider-history plane independently from
// SessionContextStore, whose state blob has a different owner.
func (r *Runtime) AttachHistoryRouter(router *sessionstore.Router) {
	r.historyRouterMu.Lock()
	r.historyRouter = router
	r.historyRouterMu.Unlock()
}

func (r *Runtime) durableHistoryRouter() *sessionstore.Router {
	r.historyRouterMu.RLock()
	defer r.historyRouterMu.RUnlock()
	return r.historyRouter
}

// PrepareMainSessionHistory arms the Runtime-owned DurableHistory to hand the
// application-assembled provider history to the next framework ChatStream.
// Runtime owns only this cache; it never calls back into Application.
func (r *Runtime) PrepareMainSessionHistory(sessionID string, messages []types.Message) bool {
	if r == nil {
		return false
	}
	r.mainHistoryMu.RLock()
	history := r.mainHistory
	r.mainHistoryMu.RUnlock()
	if history == nil || history.SessionID() != sessionID {
		return false
	}
	history.PrepareNextLoad(messages)
	return true
}

func (r *Runtime) newMainSession(sessionID string, hooks *session.LoopHooks) (*session.Session, error) {
	if sessionID == "" {
		sessionID = fmt.Sprintf("sess_%d", time.Now().UnixNano())
	}
	r.mainSessionMu.Lock()
	r.mainSessionID = sessionID
	r.mainSessionMu.Unlock()
	components := session.SessionComponents{
		Agent:     r.agt,
		Context:   r.mainContextComponents(),
		Hooks:     hooks,
		Telemetry: r.hook,
		SessionID: sessionID,
		ModelName: r.model,
	}
	// 滑动窗口加载区间（D1，plan.md §9）：装配 DurableHistory 并经
	// SetTailBudget 注入窗口读尾预算——Session 每次 Chat 前 Load 只装载
	// 窗口区间（token + 轮数），窗口外由 CompactStack 摘要承接。
	// 未装配 Router 时保持框架内存 history，供测试和兼容调用方使用。
	if router := r.durableHistoryRouter(); router != nil {
		history := sessionstore.NewDurableHistory(router, sessionID)
		history.SetTailBudget(r.windowTailBudget())
		// 真空区覆盖：滑动窗口与压缩内容之间未压缩轮次的断档填补
		// （seelexctx/gap.go；压缩只随活跃期事件触发，会话结束后窗口外的
		// 未压缩轮次会被尾窗装载丢弃——Load 时以完整事件流补压合并帧）。
		history.SetGapCoverer(func(ctx context.Context, allEvents, tailEvents []sessionstore.Event) error {
			return r.coverHistoryGap(ctx, allEvents, tailEvents)
		})
		components.History = history
		r.mainHistoryMu.Lock()
		r.mainHistory = history
		r.mainHistoryMu.Unlock()
	}
	sess, err := session.NewSession(components)
	if err != nil {
		return nil, fmt.Errorf("seelebridge: create main session: %w", err)
	}
	r.sessionMu.Lock()
	r.session = sess
	r.sessionHooks = hooks
	r.sessionMu.Unlock()
	return sess, nil
}

func (r *Runtime) Shutdown() {
	if r == nil {
		return
	}
	// todolist mailbox actor 优雅停机（Stop 消费者 goroutine，避免泄漏）。
	if r.tasks != nil {
		r.tasks.Close()
	}
	// 子代理上下文 actor 优雅停机（父证据/merge-back 队列；幂等）。
	if r.subagentContext != nil {
		r.subagentContext.Close()
	}
	if r.subagentSessions != nil {
		r.subagentSessions.Close()
	}
	if r.worktreeMgr != nil {
		r.worktreeMgr.Close()
	}
	// 定时周期任务优雅停机：停 ticker + 取消运行中任务并等待退出。
	if r.scheduler != nil {
		r.scheduler.stop()
	}
	if r.agt != nil {
		r.agt.Shutdown()
	}
}

// SetBashDiagnosticObserver installs an optional, best-effort diagnostic
// observer for the project-scoped bash tool. Events contain no command text,
// arguments, workdir, output, or account data, so a console logger can be
// enabled while investigating a stalled tool call without exposing payloads.
// Passing nil disables the observer.
func (r *Runtime) SetBashDiagnosticObserver(observer BashDiagnosticObserver) {
	if r == nil {
		return
	}
	r.bashObserverMu.Lock()
	r.bashObserver = observer
	r.bashObserverMu.Unlock()
}

func (r *Runtime) Model() string { return r.model }

// Tracer 返回遥测内存追踪器（trace 视图查询源，见 seelebridge/trace.go）。
// GUI/TUI 经 enginePort 查询（TraceText/TokenCount）；生命周期事件
// （llm/tool intent-effect）由会话级 hook 写入。
func (r *Runtime) Tracer() *telemetry.MemoryTracer { return r.tracer }

// CurrentSession 返回当前主会话（会话切换重建后依然是最新实例）。
// 注意：仅在 ChatStream 之外读取（会话切换/装配）；运行中的子代理上下文
// 访问会与主会话锁死锁，父证据/回传均走无锁旁路。
func (r *Runtime) CurrentSession() *session.Session {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	return r.session
}

// ContextWindow returns the total context available to the selected account.
// It is distinct from MaxOutputTokens, which caps a single response.
func (r *Runtime) ContextWindow() int { return r.currentAccountLimits().ContextWindow }

// MaxOutputTokens returns the configured maximum output for one model call.
func (r *Runtime) MaxOutputTokens() int { return r.currentAccountLimits().MaxOutputTokens }

func (r *Runtime) currentAccountLimits() accountLimits {
	r.branchMu.RLock()
	accountID := r.selectedAccountID
	r.branchMu.RUnlock()
	if accountID == "" {
		accountID = r.defaultAccountID
	}
	if limits, ok := r.accountLimits[accountID]; ok {
		return limits
	}
	return accountLimits{ContextWindow: defaultContextWindow, MaxOutputTokens: defaultMaxOutputTokens}
}

func (r *Runtime) RegisterBuiltins() {
	r.registerProjectScopedTools()
	r.registerForkTool()
	r.registerTodoTools()
	r.registerTaskTools()
	r.scopedToolsReady = true
	// plan 工具（seelex-workplan provider）：plan_load/plan_clear/plan_validate/
	// plan_status/plan_export；plan_run 的执行内核在 seele-v2 slice 4 迁移后恢复。
	if r.planExecutor != nil {
		if err := r.registry.registry.Register(r.planExecutor.Provider()); err != nil {
			return
		}
	}
}

// BindProjectRoot makes the supplied project the only root used by Seelex
// filesystem tools for the active session.
func (r *Runtime) BindProjectRoot(rootPath string) error { return r.projectScope.Bind(rootPath) }

// UnbindProjectRoot makes filesystem and shell tools fail closed until a
// project is selected.
func (r *Runtime) UnbindProjectRoot() { r.projectScope.Unbind() }

// SetPlanNodeCallback 注册节点/计划状态投影订阅：workplan 执行事实
// 经 planEventSink 投影为 PlanNodeEvent（NodeStatus/PlanStatus）后回调。
// 语义与旧框架 NodeResult 回调等价（plan_gate_test 不变）。
func (r *Runtime) SetPlanNodeCallback(cb func(PlanNodeEvent)) {
	r.planExecutor.SetPlanNodeCallback(cb)
}

// PlanNodeEventChannel 返回 plan 节点事件 channel（CSP：application 消费者
// 串行处理，保序；非阻塞投递，满则丢事件由 Snapshot resync 兜底）。
func (r *Runtime) PlanNodeEventChannel() <-chan PlanNodeEvent {
	if r == nil || r.planExecutor == nil {
		return nil
	}
	return r.planExecutor.PlanNodeEventChannel()
}

// SetEventPersister 安装执行事实持久化钩子（双轨事件的事实轨：
// event.Sink → sessionstore 事件库）。钩子失败经 ErrorHandler 隔离，
// 不破坏 WorkPlan 控制流（见 Seele event/README.md）。
func (r *Runtime) SetEventPersister(fn func(context.Context, frameworkevent.Event) error) {
	r.planExecutor.SetEventPersister(fn)
}

// SetEventErrorHandler 覆盖 Sink 失败处理器（默认 log.Printf 兜底）。
func (r *Runtime) SetEventErrorHandler(handler frameworkevent.ErrorHandler) {
	r.planExecutor.SetEventErrorHandler(handler)
}

// SetPlanApprovalGate 设置 plan kind:approve/manual 节点的审批门控；
// approvalGateNode 在 Run 时读取当前门（延迟读取，可在 plan_load 之后设置）。
func (r *Runtime) SetPlanApprovalGate(gate approve.ApprovalGate) {
	r.planExecutor.SetApprovalGate(gate)
}

// currentApprovalGate 返回当前审批门（approvalGateNode 的读取器）。
func (r *Runtime) currentApprovalGate() approve.ApprovalGate {
	if r == nil || r.planExecutor == nil {
		return nil
	}
	return r.planExecutor.CurrentApprovalGate()
}

// currentAgentFactory 返回当前 plan 子代理工厂（SeelexAgentNode 的读取器）。
func (r *Runtime) currentAgentFactory() node.AgentFactory {
	if r == nil || r.planExecutor == nil {
		return nil
	}
	return r.planExecutor.CurrentAgentFactory()
}

// SetPlanBranchBinding freezes context and account-selection inputs for the
// next plan run.
func (r *Runtime) SetPlanBranchBinding(binding PlanBranchBinding) {
	r.branchMu.RLock()
	selectedAccountID := r.selectedAccountID
	r.branchMu.RUnlock()
	if binding.AccountID == "" {
		binding.AccountID = selectedAccountID
	}
	if binding.PrimaryRole == "" {
		binding.PrimaryRole = RoleAgent
	}
	if binding.PlanID == "" {
		binding.PlanID = binding.EntryNodeID
	}
	r.planExecutor.SetBinding(binding)
}

// SetPlanPolicy updates constraints applied to subsequent plan_load calls.
func (r *Runtime) SetPlanPolicy(policy PlanPolicy) {
	r.planExecutor.SetPolicy(policy)
}

// RestorePlan reloads a canonical persisted plan into the runtime plan store.
func (r *Runtime) RestorePlan(ctx context.Context, arguments string) error {
	canonical, err := NormalizePlanLoadArguments(arguments)
	if err != nil {
		return fmt.Errorf("restore plan: normalize persisted plan: %w", err)
	}
	if _, err := r.agentDispatch(ctx, "plan_load", canonical); err != nil {
		return fmt.Errorf("restore plan: %w", err)
	}
	return nil
}

// agentDispatch 统一工具分发入口（agent.DirectDispatch 语义等价）。
func (r *Runtime) agentDispatch(ctx context.Context, name, argsJSON string) (string, error) {
	if r.agt == nil {
		return "", fmt.Errorf("seelebridge: agent is unavailable")
	}
	return r.agt.DirectDispatch(ctx, name, argsJSON)
}

func (r *Runtime) currentPlanPolicy() PlanPolicy {
	if r == nil || r.planExecutor == nil {
		return PlanPolicy{}
	}
	return r.planExecutor.Policy()
}

// ReplanMetrics returns process-wide replan cost and rejection accounting.
func (r *Runtime) ReplanMetrics() ReplanMetrics {
	if r == nil || r.planExecutor == nil {
		return ReplanMetrics{}
	}
	return r.planExecutor.ReplanMetrics()
}

func (r *Runtime) RegisterTool(
	name, description string,
	inputSchema map[string]interface{},
	handler func(context.Context, string) (string, error),
) {
	if r.scopedToolsReady && isProjectScopedTool(name) {
		return
	}
	r.addInlineTool(name, description, inputSchema, handler)
}

func isProjectScopedTool(name string) bool {
	switch name {
	case "read_file", "grep_search", "glob", "write_file", "edit_file", "bash":
		return true
	default:
		return false
	}
}

func (r *Runtime) AllTools() []Tool {
	return summarizeTools(r.registry.registry.Tools())
}

func (r *Runtime) VisibleTools(ctx context.Context) []Tool {
	return summarizeTools(r.agt.VisibleTools(ctx))
}

func (r *Runtime) Accounts() []Account {
	entries := r.pool.Entries()
	result := make([]Account, 0, len(entries))
	for _, entry := range entries {
		result = append(result, Account{
			Name:     entry.Snapshot.ID,
			Provider: entry.Snapshot.Metadata["provider"],
			Model:    entry.Snapshot.Metadata["model"],
			Disabled: entry.Snapshot.Disabled,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (r *Runtime) SelectAccount(name string) bool {
	spec := accountByName(r.accountSpecList(), name)
	if spec == nil {
		return false
	}
	r.branchMu.Lock()
	r.selectedAccountID = spec.Name
	r.providerFilter = spec.Provider
	r.branchMu.Unlock()
	return true
}

func (r *Runtime) accountSpecList() []accountSpec {
	specs := make([]accountSpec, 0, len(r.accountSpecs))
	for _, spec := range r.accountSpecs {
		specs = append(specs, spec)
	}
	return specs
}

func (r *Runtime) Provider() string {
	r.branchMu.RLock()
	provider := r.providerFilter
	r.branchMu.RUnlock()
	if provider == "" {
		if spec := accountByName(r.accountSpecList(), r.defaultAccountID); spec != nil {
			provider = spec.Provider
		}
	}
	return provider
}

func (r *Runtime) SetProvider(provider string) {
	r.branchMu.Lock()
	r.providerFilter = provider
	if provider != "" {
		// 切换 provider 时清除固定账号，让 P2C 在过滤集内选择。
		r.selectedAccountID = ""
	}
	r.branchMu.Unlock()
}

// SetPermissionConfig 安装权限门控：Mode + Rules + ApprovalHandler。
// 门控作为 tools.Registry middleware 在每次工具调度前生效。
func (r *Runtime) SetPermissionConfig(cfg toolspermission.PermissionConfig, handler toolspermission.ApprovalHandler) {
	if r.permission != nil {
		r.permission.set(cfg, handler)
	}
}

func (r *Runtime) SetFullAccess(on bool) {
	if r.permission != nil {
		r.permission.setFullAccess(on)
	}
}

func (r *Runtime) FullAccess() bool {
	return r.permission != nil && r.permission.fullAccess()
}

func summarizeTools(tools []types.Tool) []Tool {
	result := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		result = append(result, Tool{
			Name: tool.Function.Name, Description: tool.Function.Description,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
