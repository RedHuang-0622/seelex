// Package seelebridge adapts Seele framework primitives to stable Seelex APIs.
package seelebridge

import (
	"context"
	"fmt"
	"hash/fnv"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RedHuang-0622/Seele/accountpool"
	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/bridge"
	frameworkevent "github.com/RedHuang-0622/Seele/event"
	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/telemetry"
	frameworktools "github.com/RedHuang-0622/Seele/tools"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/Seele/workplan/codec"
	"github.com/RedHuang-0622/Seele/workplan/core/node"
	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/mcpstack"
	"github.com/RedHuang-0622/seelex/seelebridge/account"
	"github.com/RedHuang-0622/seelex/seelebridge/fork"
	"github.com/RedHuang-0622/seelex/seelebridge/fs"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/config"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/docker"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/model"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/stream"
	seeletelemetry "github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry"
	"github.com/RedHuang-0622/seelex/seelebridge/mcp"
	seenode "github.com/RedHuang-0622/seelex/seelebridge/node"
	"github.com/RedHuang-0622/seelex/seelebridge/plan"
	"github.com/RedHuang-0622/seelex/seelebridge/plugin"
	"github.com/RedHuang-0622/seelex/seelebridge/scheduler"
	"github.com/RedHuang-0622/seelex/seelebridge/security"
	subagentsession "github.com/RedHuang-0622/seelex/seelebridge/session"
	"github.com/RedHuang-0622/seelex/seelebridge/task"
	seeltools "github.com/RedHuang-0622/seelex/seelebridge/tools"
	"github.com/RedHuang-0622/seelex/seelebridge/worktree"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/compactor"
	"github.com/RedHuang-0622/seelex/seelexctx/memory"
	"github.com/RedHuang-0622/seelex/seelexctx/provider"
	"github.com/RedHuang-0622/seelex/seelexctx/snapshot"
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
	registry  *seeltools.RegistryState
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
	accountLimits    map[string]config.AccountLimits
	accountSpecs     map[string]model.AccountSpec

	// MCPStack 记录所有 MCP 调用的 trace（熔断事件 + 调用记录）。
	// AttachMCP 时自动启动熔断事件监听，无需手动装配。
	MCPStack *mcpstack.MCPStack

	mcpManager           *mcp.Manager // MCP 服务器生命周期（mcp/ 域）
	branchMu             sync.RWMutex
	planExecutor         *plan.Executor // Plan 执行域组件（plan_executor.go）：策略/绑定/runID/事件/replan/工厂
	visibilityProjection atomic.Pointer[RuntimeVisibilityProjection]
	// subagentContext 是子代理上下文 actor（装配件拆分第一步）：父证据
	// 的读-合并-写回与 merge-back 队列收进单一 goroutine（channel 命令 +
	// 串行处理），替代原来的 parentEvidenceMu / subagentOverflowMu 两把锁。
	subagentContext *subagentsession.SubagentContextActor
	// skills 是子代理 skill 目录的 actor 资源：skill.Registry 内部自锁
	// （读写即消息进出：All/Get 读、Register/Reload 写），见 skill/skill.go。
	// 装配一次性写入、运行期只读消费，与 filesystem actor 同构，无需外层锁。
	skills            *skill.Registry
	selectedAccountID string
	providerFilter    string
	projectScope      *security.ProjectScope
	filesystem        fs.FileSystem             // 文件系统 actor（写路径分片串行化，filesystem_actor.go）
	sandbox           security.CommandSandbox   // shell 执行隔离端口（security/sandbox.go；默认 native cwd-gate）
	dockerProbe       docker.Prober             // docker 守护进程探测/启动（零值 → 真实实现；测试注入）
	node              *seenode.Coordinator      // 节点协调器（node/ 域）：会话注册/fork 树/task 打点/plan 阶段/Blocks
	worktreeMgr       *worktree.WorktreeManager // 子代理 worktree 生命周期组件（worktree/ 域）
	forkTool          *fork.Tool                // fork_subagents 执行编排（fork/ 域）
	tasks             *task.TaskRegistry        // task 注册表 actor（task/；todolist 融合为 kind=todo 的 task）
	scheduler         *scheduler.State          // 定时周期任务 actor（scheduler/ 域）
	toolEvents        *subagentsession.ToolEventState

	// 子代理会话注册表组件（actor：channel 命令 + 单 goroutine，subagent_sessions.go）。
	// 运行中读子会话 History（子代理 actor 独立锁，安全）；结束后保留快照；
	// node:<nodeID>: 前缀工具结果归档器由组件托管，运行中/结束后均可读回。
	subagentSessions *subagentsession.SubagentSessions
	// subagentTree 是 fork 子代理树注册表（内存态，不落盘）：fork 创建子代理
	// 时记录 parent/child 链与节点状态/goal/会话摘要，GUI 树视图经
	// Runtime.SubAgentTree() 读取（subagent_tree.go）。
	subagentTree        *subagentsession.SubagentTree
	toolCallTimeout     time.Duration
	bashObserverMu      sync.RWMutex
	bashObserver        BashDiagnosticObserver
	planDecisionTimeout time.Duration
	approvalTimeout     time.Duration
	heartbeatInterval   time.Duration
	limits              seelexctx.Limits // seele.yaml limits 段（含默认；seelebridge 消费点读取）
	scopedToolsReady    bool

	plugins *plugin.Manager // 插件可见性配置（plugin/ 域）

	permission *seeltools.PermissionGate

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
	loaded, err := config.Load(cfg.AccountsPath)
	if err != nil {
		return nil, fmt.Errorf("seelebridge: load accounts: %w", err)
	}
	if len(loaded.Specs) == 0 {
		return nil, fmt.Errorf("seelebridge: accounts configuration is empty")
	}

	// 1. 账号池：accountpool.P2CPool[agent.Completer]（P2C 租约）
	pool := accountpool.New[agent.Completer]()
	if err := account.RegisterAccounts(pool, loaded.Specs); err != nil {
		return nil, err
	}
	specsByName := make(map[string]model.AccountSpec, len(loaded.Specs))
	for _, spec := range loaded.Specs {
		specsByName[spec.Name] = spec
	}
	first := loaded.Specs[0]

	// 遥测装配：内存追踪器 + 生命周期钩子（llm/tool intent-effect 事件）。
	tracer := seeletelemetry.NewTracer()
	hook, err := seeletelemetry.NewLifecycleHook(tracer)
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
		scheduler:           scheduler.NewState(),
		toolEvents:          subagentsession.NewToolEventState(),
		toolCallTimeout:     cfg.ToolCallTimeout,
		planDecisionTimeout: planDecisionTimeout,
		approvalTimeout:     approvalTimeout,
		heartbeatInterval:   heartbeatInterval,
		limits:              cfg.Limits.WithDefaults(),
		plugins:             plugin.NewManager(),
		permission:          &seeltools.PermissionGate{},
		subagentSessions:    subagentsession.NewSubagentSessions(tracer),
		subagentTree:        subagentsession.NewSubagentTree(tracer),
		subagentContext:     subagentsession.NewSubagentContextActor(tracer),

		window: seelexctx.NewDefaultWindowPolicy(cfg.WindowConfig),
		tracer: tracer,
		hook:   hook,
	}
	// MCP 管理器：MCPStack 承接熔断事件 trace，工具注册表经懒解析适配器注入。
	r.mcpManager = mcp.NewManager(r.MCPStack, mcpRegistryAdapter{runtime: r})
	// plan 执行域组件：策略 / 绑定 / runID / 事件通道 / replan 护拦 / 子代理工厂
	// 收进 planExecutor；deps 闭包引用 r 的能力面（账号、注册表、分发、节点工厂），
	// 组件不反向依赖 Runtime（构造放在 r 就绪后，与 worktreeManager 同模式）。
	r.planExecutor = plan.NewExecutor(plan.ExecutorDeps{
		Model:               r.model,
		Heartbeat:           heartbeatInterval,
		Limits:              r.limits,
		PlanDecisionTimeout: planDecisionTimeout,
		Accounts:            r.accountSpecList,
		LoadPlanDefinition: func() (types.Tool, bool) {
			if r.registry == nil || r.registry.Registry == nil {
				return types.Tool{}, false
			}
			for _, tool := range r.registry.Registry.Tools() {
				if tool.Function.Name == "plan_load" {
					return tool, true
				}
			}
			return types.Tool{}, false
		},
		Dispatch:    r.agentDispatch,
		NodeFactory: r.nodeFactory,
	}, cfg.MaxConcurrentReplans, cfg.MaxReplansPerWindow, cfg.MaxReplanProviderRequests, cfg.ReplanWindow)
	// 节点协调器：session/fork/task/plan 经接口与闭包注入，域内不反向依赖
	// Runtime；面向 AgentNode.Deps 与 RuntimePort 互相承接。
	r.node = seenode.NewCoordinator(seenode.CoordinatorDeps{
		Sessions: r.subagentSessions,
		Tree:     r.subagentTree,
		Tasks:    r.tasks,
		Plan:     r.planExecutor,
		Evidence: r.subagentContext.NodeParentEvidence,
		Limits:   r.limits,
		GoalSkillActive: func() bool {
			if projection := r.visibilityProjection.Load(); projection != nil {
				return projection.GoalSkillActive
			}
			return false
		},
		AccountLimits: r.currentAccountLimits,
		InheritedBlocks: func() []seelectx.PromptBlock {
			blocks := make([]seelectx.PromptBlock, 0, 2)
			if project := r.projectBlock(); project != nil {
				blocks = append(blocks, *project)
			}
			blocks = append(blocks, r.stackBlocks()...)
			return blocks
		},
		RelatedMemory: r.relatedMemoryBlocks,
	})
	// worktree 生命周期组件：项目根 / 阶段事件 / 审批门经 deps 注入，组件不反向
	// 依赖 Runtime（构造放在 r 就绪后，因为 deps 引用 r 的方法值）。
	r.worktreeMgr = worktree.NewWorktreeManager(worktree.WorktreeManagerDeps{
		Root:  r.projectScope.Root,
		Phase: r.node.AppendPhase,
		Gate:  r.planExecutor.CurrentApprovalGate,
	})
	r.forkTool = fork.NewTool(r.forkDeps())
	// The wrapper is inert until the backend diagnostic observer is enabled.
	// It brackets telemetry.After, the only framework boundary between a tool
	// registry return and the application ToolHookBridge completion callback.
	r.hook = seeletelemetry.NewDiagnosticHook(r.hook, r.observeBash)

	// 2. 工具注册表：WithCallTimeout 保留工具超时语义；权限门控作为 middleware。
	r.registry = seeltools.NewRegistryState(cfg.ToolCallTimeout, r.permission, approvalTimeout, r.toolEvents.Middleware(), r.bashDiagnosticMiddleware())

	// 3. Completer / StreamCompleter（共享账号选择器，无 api.ChatClient 强转）
	if err := r.assembleCompleters(); err != nil {
		return nil, err
	}

	// 4. Agent 装配：agent.NewWithComponents（不启动 Hub / 账号池 / 网关）
	runtimeAdapter, err := bridge.NewRegistryRuntime(r.registry.Registry,
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
	r.streamer = stream.NewStreamingCompleter(r.pool, r.accountSelector)
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
		r.scheduler.Stop()
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

func (r *Runtime) currentAccountLimits() config.AccountLimits {
	r.branchMu.RLock()
	accountID := r.selectedAccountID
	r.branchMu.RUnlock()
	if accountID == "" {
		accountID = r.defaultAccountID
	}
	if limits, ok := r.accountLimits[accountID]; ok {
		return limits
	}
	return config.AccountLimits{ContextWindow: config.DefaultContextWindow, MaxOutputTokens: config.DefaultMaxOutputTokens}
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
		if err := r.registry.Registry.Register(r.planExecutor.Provider()); err != nil {
			return
		}
	}
}

// registerTodoTools 注册 todolist 工具族（委托 task.Tools；RegisterBuiltins 内调用）。
func (r *Runtime) registerTodoTools() {
	task.NewTools(r.taskToolsDeps()).RegisterTodoTools()
}

// registerTaskTools 注册主动任务工具 taskadd（同上委托）。
func (r *Runtime) registerTaskTools() {
	task.NewTools(r.taskToolsDeps()).RegisterTaskTools()
}

// BindProjectRoot makes the supplied project the only root used by Seelex
// filesystem tools for the active session.
func (r *Runtime) BindProjectRoot(rootPath string) error { return r.projectScope.Bind(rootPath) }

// UnbindProjectRoot makes filesystem and shell tools fail closed until a
// project is selected.
func (r *Runtime) UnbindProjectRoot() { r.projectScope.Unbind() }

// SetPlanNodeCallback 注册节点/计划状态投影订阅：workplan 执行事实
// 经 plan.planEventSink 投影为 dto.PlanNodeEvent（NodeStatus/PlanStatus）后回调。
// 语义与旧框架 NodeResult 回调等价（plan_gate_test 不变）。
func (r *Runtime) SetPlanNodeCallback(cb func(dto.PlanNodeEvent)) {
	r.planExecutor.SetPlanNodeCallback(cb)
}

// PlanNodeEventChannel 返回 plan 节点事件 channel（CSP：application 消费者
// 串行处理，保序；非阻塞投递，满则丢事件由 Snapshot resync 兜底）。
func (r *Runtime) PlanNodeEventChannel() <-chan dto.PlanNodeEvent {
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
func (r *Runtime) SetPlanBranchBinding(binding dto.PlanBranchBinding) {
	r.branchMu.RLock()
	selectedAccountID := r.selectedAccountID
	r.branchMu.RUnlock()
	if binding.AccountID == "" {
		binding.AccountID = selectedAccountID
	}
	if binding.PrimaryRole == "" {
		binding.PrimaryRole = model.RoleAgent
	}
	if binding.PlanID == "" {
		binding.PlanID = binding.EntryNodeID
	}
	r.planExecutor.SetBinding(binding)
}

// SetPlanPolicy updates constraints applied to subsequent plan_load calls.
func (r *Runtime) SetPlanPolicy(policy dto.PlanPolicy) {
	r.planExecutor.SetPolicy(policy)
}

// RestorePlan reloads a canonical persisted plan into the runtime plan store.
func (r *Runtime) RestorePlan(ctx context.Context, arguments string) error {
	canonical, err := plan.NormalizePlanLoadArguments(arguments)
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

func (r *Runtime) currentPlanPolicy() dto.PlanPolicy {
	if r == nil || r.planExecutor == nil {
		return dto.PlanPolicy{}
	}
	return r.planExecutor.Policy()
}

// dto.ReplanMetrics returns process-wide replan cost and rejection accounting.
func (r *Runtime) ReplanMetrics() dto.ReplanMetrics {
	if r == nil || r.planExecutor == nil {
		return dto.ReplanMetrics{}
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
	r.registry.AddInline(name, description, inputSchema, handler)
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
	return summarizeTools(r.registry.Registry.Tools())
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
	spec := account.ByName(r.accountSpecList(), name)
	if spec == nil {
		return false
	}
	r.branchMu.Lock()
	r.selectedAccountID = spec.Name
	r.providerFilter = spec.Provider
	r.branchMu.Unlock()
	return true
}

func (r *Runtime) accountSpecList() []model.AccountSpec {
	specs := make([]model.AccountSpec, 0, len(r.accountSpecs))
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
		if spec := account.ByName(r.accountSpecList(), r.defaultAccountID); spec != nil {
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
		r.permission.Set(cfg, handler)
	}
}

func (r *Runtime) SetFullAccess(on bool) {
	if r.permission != nil {
		r.permission.SetFullAccess(on)
	}
}

func (r *Runtime) FullAccess() bool {
	return r.permission != nil && r.permission.FullAccess()
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

// runtimeBudgetProvider 把 Runtime 账号限额适配为 seelexctx.BudgetProvider
// （Controller 软/硬阈值输入）。
type runtimeBudgetProvider struct{ runtime *Runtime }

func (p runtimeBudgetProvider) ContextTokens() int   { return p.runtime.ContextWindow() }
func (p runtimeBudgetProvider) MaxOutputTokens() int { return p.runtime.MaxOutputTokens() }

// runtimeCompactStacks 把可选 SessionContextStore 适配为 CompactStackStore：
// 已绑定存储 → 栈操作落盘（state blob）；未绑定 → 内存态（会话内可审计）。
type runtimeCompactStacks struct {
	runtime *Runtime
	memory  seelexctx.CompactStackStore
}

func (s runtimeCompactStacks) Snapshot() sessionstore.SessionContextRecord {
	if store := s.runtime.sessionContextStore(); store != nil {
		return store.Snapshot()
	}
	return s.memory.Snapshot()
}

func (s runtimeCompactStacks) PushCompact(frame sessionstore.CompactFrame) error {
	if store := s.runtime.sessionContextStore(); store != nil {
		return store.PushCompact(frame)
	}
	return s.memory.PushCompact(frame)
}

// mainContextComponents 构造主会话的 ContextComponents（plan.md §3.1 步骤 4）。
// SystemPrompt 置空：会话级提示由 application 经 SetSystemPrompt 注入，
// 保持既有行为；Assembler 的 system provider 在应用层迁移后接管。
func (r *Runtime) mainContextComponents() session.ContextComponents {
	return session.ContextComponents{
		Assembler:           r.seelexAssembler(),
		ToolResultProcessor: seelexctx.NewToolResultProcessor(0, nil),
		Compressor:          r.seelexCompressor(),
		Controller:          r.seelexController(),
	}
}

// nodeContextComponents 构造节点子代理会话的 ContextComponents
// （bridge.WithSessionComponents 输入）。节点级 PromptBlocks 由
// SeelexAgentNode.Run 注入 ctx（ScopeAssembler 合并），本组件与
// 主会话共享同一套适配器依赖（预算按节点账号限额推导）。
func (r *Runtime) nodeContextComponents() session.ContextComponents {
	return session.ContextComponents{
		Assembler: r.node.Assembler(),
		ToolResultProcessor: seelexctx.NewToolResultProcessor(0, seenode.ToolResultArchiver{
			ArchiverFor: r.node.ToolResultArchiverFor,
			Shared:      seelexctx.NewInMemoryToolResultArchiver(),
		}),
		Compressor: r.seelexCompressor(),
		Controller: r.seelexController(),
	}
}

// seelexAssembler 构造 seelex 装配器：栈块（now using = 栈顶）来自
// SessionContextStore（未注入 → 无块）；project 块来自 ProjectKnowledge
// 提供者（未注入 → 无块）；记忆块按当前查询从历史压缩段选取（无存储 →
// 无块）；占位符解析委托 seelexctx。
func (r *Runtime) seelexAssembler() seelectx.RequestAssembler {
	return seelexctx.NewAssembler(seelexctx.AssemblerOptions{
		SystemPrompt: nil, // 会话级提示由 application 侧注入（迁移后经此渲染）
		ProjectBlock: r.projectBlock,
		StackBlocks:  r.stackBlocks,
		Memories:     r.relatedMemoryBlocks,
		Resolver: seelectx.PlaceholderResolverFunc(func(_ context.Context, name string) (string, error) {
			return r.resolvePlaceholder(name)
		}),
	})
}

// relatedMemoryBlocks 按当前查询从 CompactStack 全部帧选取相关记忆块
// （不止栈顶：超长会话的久远相关段经 seelexctx/memory 词法选取器召回）。
// 无绑定存储 / 空查询 / 无命中 → 不注入（请求内容不变）。
func (r *Runtime) relatedMemoryBlocks(_ context.Context, query string) []seelectx.PromptBlock {
	store := r.sessionContextStore()
	if store == nil {
		return nil
	}
	record := store.Snapshot()
	if len(record.CompactStack) == 0 {
		return nil
	}
	candidates := make([]memory.Candidate, 0, len(record.CompactStack))
	for _, frame := range record.CompactStack {
		candidates = append(candidates, memory.Candidate{
			SegmentID: frame.SegmentID, Summary: frame.Summary,
			Evidence: frame.Evidence, From: frame.From, To: frame.To,
		})
	}
	opts := memory.DefaultOptions()
	selected := memory.Select(query, candidates, opts)
	if len(selected) == 0 {
		return nil
	}
	if block := memory.RenderMemoryBlock(selected, opts.MaxTokens); block != nil {
		return []seelectx.PromptBlock{*block}
	}
	return nil
}

// coverHistoryGap 把滑动窗口与压缩内容之间的真空区轮次压缩为合并帧
// （seelexctx.CoverHistoryGap）：完整事件流 vs 压缩栈顶 To vs 尾窗装载量
// 三者对齐，未覆盖区间压入会话压缩栈（state blob / 内存兜底），原文经
// TurnArchiver 归档（read_compressed_turn 可读回）。无真空区 → 无副作用。
func (r *Runtime) coverHistoryGap(ctx context.Context, allEvents, tailEvents []sessionstore.Event) error {
	if len(allEvents) == 0 {
		return nil
	}
	stacks := runtimeCompactStacks{runtime: r, memory: seelexctx.NewMemoryCompactStack()}
	_, err := seelexctx.CoverHistoryGap(ctx, seelexctx.GapCoverageOptions{
		AllEvents:  allEvents,
		TailEvents: tailEvents,
		Record:     stacks.Snapshot(),
		Stacks:     stacks,
		Turns:      r.turnArchiver,
		SessionID:  r.MainSessionID(),
	})
	return err
}

// seelexCompressor 构造压缩器：短历史快速路径 + QuickChat 结构化 checkpoint
// （共享账号 completer 的隔离调用，无工具、独立 history）。
func (r *Runtime) seelexCompressor() seelectx.Compressor {
	quickChat, err := seelectx.NewQuickChat(r.completer)
	if err != nil {
		quickChat = nil // 装配失败 → 仅短历史/快照路径可用
	}
	return seelexctx.NewCompressor(seelexctx.CompressorOptions{
		QuickChat: quickChat,
		Compactor: r.compactorInstance(),
		SnapshotFor: func(_ context.Context, request seelectx.CompressionRequest) *snapshot.ContextSnapshot {
			return r.compressionSnapshot(request.SessionID)
		},
	})
}

// seelexController 构造控制器：窗口策略来自 RuntimeConfig.WindowConfig
// （DefaultWindowPolicy，plan.md §3.7.3），阈值预算来自账号限额。
func (r *Runtime) seelexController() seelectx.ContextController {
	policy := seelexctx.NewContextWindowPolicy(r.ContextWindow(), r.MaxOutputTokens())
	return seelexctx.NewContextController(seelexctx.ControllerOptions{
		Policy: policy,
		Window: r.windowPolicy(),
		Budget: runtimeBudgetProvider{runtime: r},
		Stacks: runtimeCompactStacks{runtime: r, memory: seelexctx.NewMemoryCompactStack()},
		Turns:  r.turnArchiver,
		// 压缩帧 SegmentID 溯源到当前会话：每次压缩动态取值，会话切换后
		// 仍指向正确会话（compact-<sessionID>-<ms>）。
		SessionIDProvider: r.MainSessionID,
	})
}

// windowPolicy 返回当前窗口策略（NewRuntime 时按配置构造）。
func (r *Runtime) windowPolicy() seelexctx.WindowPolicy {
	r.windowMu.RLock()
	defer r.windowMu.RUnlock()
	return r.window
}

// windowTailBudget 从窗口策略推导 Load 的读尾预算（D1，plan.md §9）：
// maxUnits = 窗口轮数（策略推导；输入不足时保守回退 min_rounds）；
// tokenBudget = 账号上下文窗口（上限保护，LoadEventTail 双上限取先到者）。
func (r *Runtime) windowTailBudget() (tokenBudget, maxUnits int) {
	tokenBudget = r.ContextWindow()
	info := seelexctx.ProviderContextInfo{ContextTokens: tokenBudget}
	rounds, _ := r.windowPolicy().WindowRounds(context.Background(), info)
	if rounds <= 0 {
		rounds = 4 // DefaultWindowPolicy 的 min_rounds（输入不足时同样回退）
	}
	return tokenBudget, rounds
}

// stackBlocks 渲染会话级使用栈块（now using = 栈顶；未绑定存储 → 无块）。
func (r *Runtime) stackBlocks() []seelectx.PromptBlock {
	store := r.sessionContextStore()
	if store == nil {
		return nil
	}
	return seelexctx.RenderStackBlocks(store.Snapshot())
}

// projectBlock 渲染项目级模块语义块（ProjectKnowledge，会话前预读缓存；
// 提供者未注入 → 无块）。
func (r *Runtime) projectBlock() *seelectx.PromptBlock {
	r.projectMu.RLock()
	provider := r.projectKnowledge
	r.projectMu.RUnlock()
	if provider == nil {
		return nil
	}
	record := provider()
	if record == nil {
		return nil
	}
	return seelexctx.RenderProjectBlock(*record)
}

// resolvePlaceholder 解析 {{name}} 占位符（当前无内置变量，未知占位符
// 原样保留）。
func (r *Runtime) resolvePlaceholder(name string) (string, error) {
	return "", nil
}

// sessionContextStore 返回绑定的会话上下文存储（nil = 未绑定）。
func (r *Runtime) sessionContextStore() *sessionstore.SessionContextStore {
	r.ctxStoreMu.RLock()
	defer r.ctxStoreMu.RUnlock()
	return r.ctxStore
}

// AttachSessionContextStore 绑定会话上下文存储（state blob，plan.md §3.7.2）。
// 会话恢复流程接线时由调用方注入（router + sessionID 就绪后）。
func (r *Runtime) AttachSessionContextStore(store *sessionstore.SessionContextStore) {
	r.ctxStoreMu.Lock()
	r.ctxStore = store
	r.ctxStoreMu.Unlock()
}

// SetProjectKnowledgeProvider 注入项目级模块语义提供者（ProjectKnowledge
// 会话前预读；nil 关闭 project 块）。
func (r *Runtime) SetProjectKnowledgeProvider(provider func() *sessionstore.ProjectRecord) {
	r.projectMu.Lock()
	r.projectKnowledge = provider
	r.projectMu.Unlock()
}

// compressionSnapshot 跨会话快照压缩输入：当前节点/会话的父证据快照
// （compactor 路径；无 → 走 QuickChat 路径）。
func (r *Runtime) compressionSnapshot(_ string) *snapshot.ContextSnapshot {
	return r.node.ParentEvidence()
}

// ── 编译期检查 ────────────────────────────────────────────────────

var (
	_ seelexctx.BudgetProvider  = runtimeBudgetProvider{}
	_ session.ContextComponents = session.ContextComponents{}
)

// compactorInstance 返回跨会话快照压缩器（构造一次，会话间复用）。
func (r *Runtime) compactorInstance() *compactor.Compactor {
	return compactor.NewCompactor()
}

// accountSelector 是主链路共享的账号选择器闭包：读取 Runtime 当前的
// provider 过滤与选中账号（不持有任何 api.ChatClient 引用），把请求条件
// 转换为 accountpool.AcquireRequest。P2C 池负责实际选择与租赁。
// Plan 子代理请求（ctx 含 NodeScope）优先走节点账号解析：显式 pin 优先，
// 否则按角色 + branchID 走确定性 hash（account.ResolveForBranch 逻辑）。
func (r *Runtime) accountSelector(ctx context.Context, messages []types.Message, tools []types.Tool) accountpool.AcquireRequest {
	if scope := model.NodeScopeFromContextOrEmpty(ctx); scope.NodeID != "" {
		return r.nodeAccountRequest(scope)
	}
	r.branchMu.RLock()
	selected := r.selectedAccountID
	provider := r.providerFilter
	r.branchMu.RUnlock()
	request := accountpool.AcquireRequest{}
	if selected != "" {
		request.AccountID = selected
	}
	if provider != "" {
		if request.Metadata == nil {
			request.Metadata = make(map[string]string)
		}
		request.Metadata["provider"] = provider
	}
	return request
}

// nodeAccountRequest 按节点作用域解析账号租赁请求：binding 显式 AccountID
// 直接 pin；否则按 role + planID:branchID 确定性 hash 选择。
func (r *Runtime) nodeAccountRequest(scope seenode.NodeScope) accountpool.AcquireRequest {
	request := accountpool.AcquireRequest{}
	binding := r.currentPlanBranchBinding()
	if binding.AccountID != "" {
		request.AccountID = binding.AccountID
		return request
	}
	seed := scope.BranchID
	if seed == "" {
		seed = scope.NodeID
	}
	accountID, err := account.ResolveForBranch(r.pool, scope.Role, binding.PlanID+":"+seed)
	if err == nil {
		request.AccountID = accountID
	}
	return request
}

// bridgeAccountCompleter 构造同步 Completer（每次 Complete 恰好一次租赁）。
func (r *Runtime) bridgeAccountCompleter() (agent.Completer, error) {
	completer, err := bridge.NewAccountCompleter(r.pool,
		bridge.WithAccountRequestSelector(r.accountSelector),
	)
	if err != nil {
		return nil, fmt.Errorf("seelebridge: assemble account completer: %w", err)
	}
	return completer, nil
}

// ensureDockerForRuntime 是 tools 域的接线面：按 limits 配置执行自动恢复
// （disable_docker_auto_start 关闭时返回 nil 表示"不处理"）。
func (r *Runtime) ensureDockerForRuntime(ctx context.Context) error {
	if r == nil {
		return nil
	}
	return docker.EnsureForRuntime(ctx, r.limits.DisableDockerAutoStart, r.limits.DockerStartTimeoutSec, r.dockerProbe)
}

// dockerProbe 保留兼容名（测试注入点；内部使用 docker.Prober）。
type dockerProbe = docker.Prober

// forkDeps 把 Runtime 能力面注入 fork 域（Deps 全部为闭包，域内不依赖根包）。
func (r *Runtime) forkDeps() fork.Deps {
	return fork.Deps{
		CurrentPlanPolicy:        r.currentPlanPolicy,
		NodeFactory:              r.nodeFactory,
		TaskResolveByKey:         r.ResolveTaskByKey,
		TaskAdd:                  r.TaskAdd,
		TaskSetStatus:            r.TaskSetStatus,
		TaskAttachParticipant:    r.TaskAttachParticipant,
		SubagentTreeRegisterFork: r.subagentTree.RegisterFork,
		SubagentTreeSummaryFor:   r.subagentTree.SummaryFor,
		RunPlan:                  r.planExecutor.RunPlan,
		ForkTimeoutSec:           r.limits.ForkTimeoutSec,
		PlanNodeMaxLoops:         r.limits.PlanNodeMaxLoops,
	}
}

// registerForkTool 注册 fork_subagents（RegisterBuiltins 内调用）。
func (r *Runtime) registerForkTool() {
	r.RegisterTool("fork_subagents",
		"Fork N isolated subagents in parallel (worktree-isolated) and return their structured outputs."+fork.SubagentsContractDescription,
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"subagents": map[string]interface{}{
					"type":        "array",
					"description": "Subagent specs: unique id + natural-language goal.",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"id":   map[string]interface{}{"type": "string"},
							"goal": map[string]interface{}{"type": "string"},
						},
						"required": []string{"id", "goal"},
					},
				},
				"max_concurrency": map[string]interface{}{"type": "integer", "minimum": 1},
			},
			"required": []string{"subagents"},
		},
		r.forkSubagentsHandler)
}

// forkSubagentsHandler 是 fork_subagents 的执行入口（委托 fork.Tool.Handle）。
func (r *Runtime) forkSubagentsHandler(ctx context.Context, argsJSON string) (string, error) {
	if r == nil || r.forkTool == nil {
		return "", fmt.Errorf("fork_subagents: fork tool is not configured")
	}
	return r.forkTool.Handle(ctx, argsJSON)
}

// nodeDeps 把 Runtime 能力面注入 node 域（Deps 全部为闭包，域内不依赖根包）。
func (r *Runtime) nodeDeps() seenode.Deps {
	return seenode.Deps{
		CurrentAgentFactory:      r.currentAgentFactory,
		CurrentPlanBranchBinding: r.currentPlanBranchBinding,
		AppendNodePhase:          r.node.AppendPhase,
		BeginNodeWorktree:        r.beginNodeWorktree,
		FinishNodeWorktree:       r.finishNodeWorktree,
		ReleaseNodeWorktree:      r.releaseNodeWorktree,
		RegisterNodeSession:      r.node.RegisterSession,
		UnregisterNodeSession:    r.node.UnregisterSession,
		CompleteSubagentNode:     r.node.CompleteSubagentNode,
		NodeParentEvidence:       r.node.ParentEvidence,
		MergeBackIntoParent:      r.mergeBackIntoParent,
		EnqueueSubagentContext:   r.enqueueSubagentContext,
		NodeBudget:               r.node.Budget,
		NodePromptBlocks:         r.node.PromptBlocks,
		Tracer: func() provider.TraceSource {
			return r.Tracer()
		},
	}
}

// nodeSessionComponents 构造 Plan 节点子代理会话的公共组件
// （bridge.WithSessionComponents 输入，plan.md §3.1 步骤 5）。
// Agent 由 bridge 强制覆盖为 runtime 的 agent；每节点新建独立 Session
// （工作历史默认隔离）。节点级 PromptBlocks 由 SeelexAgentNode.Run 注入
// ctx，装配器 ScopeAssembler 在每次请求时合并。
func (r *Runtime) nodeSessionComponents() session.SessionComponents {
	return session.SessionComponents{
		Context:   r.nodeContextComponents(),
		Config:    session.SessionConfig{MaxLoops: r.limits.PlanNodeMaxLoops},
		Telemetry: r.hook,
		ModelName: r.model,
	}
}

// nodeSessionID 派生节点会话 ID：以系统提示（节点目标）为种子做稳定 hash；
// 同一节点路径 plan_run 可复现（供未来 checkpoints 定位）；空提示返回空串，
// 让 Session 自动生成不透明 ID。
func (r *Runtime) nodeSessionID(systemPrompt string) string {
	if systemPrompt == "" {
		return ""
	}
	return fmt.Sprintf("node-%x", stableHash(systemPrompt))
}

// stableHash 返回 seed 的 FNV-1a 32 位稳定哈希。
func stableHash(seed string) uint32 {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(seed))
	return hash.Sum32()
}

// currentPlanBranchBinding 返回当前 plan 执行分支绑定（planExecutor 持有）。
func (r *Runtime) currentPlanBranchBinding() dto.PlanBranchBinding {
	if r == nil || r.planExecutor == nil {
		return dto.PlanBranchBinding{}
	}
	return r.planExecutor.Binding()
}

// setSelectedAccount 切换主链路选中账号（provider 过滤跟随账号规格）。
func (r *Runtime) setSelectedAccount(name string) {
	r.branchMu.Lock()
	r.selectedAccountID = name
	r.branchMu.Unlock()
}

// resolvePlanBranchAccount 按 role + planID:branchID 稳定解析分支账号。
func (r *Runtime) resolvePlanBranchAccount(binding dto.PlanBranchBinding, role model.AccountRole, branchID string) (string, error) {
	if binding.AccountID != "" {
		if spec := account.ByName(r.accountSpecList(), binding.AccountID); spec == nil {
			return "", fmt.Errorf("branch %q pins unknown account %q", branchID, binding.AccountID)
		}
		return binding.AccountID, nil
	}
	return account.ResolveForBranch(r.pool, role, binding.PlanID+":"+branchID)
}

// roleForPlanBranch 解析分支账号角色（main/entry → 主账号，其余 → 子代理）。
func roleForPlanBranch(binding dto.PlanBranchBinding, branchID string) model.AccountRole {
	return seenode.RoleForPlanBranch(binding, branchID)
}

// branchTraceID 返回分支追踪 ID（planID:branchID 或 traceID:branchID）。
func branchTraceID(binding dto.PlanBranchBinding, branchID string) string {
	if binding.TraceID == "" {
		return branchID
	}
	return binding.TraceID + ":" + branchID
}

// nodeFactoryDeps 返回绑定到 Runtime 的跨域构造回调（测试与装配共用）。
func (r *Runtime) nodeFactoryDeps() plan.NodeFactoryDeps {
	return plan.NodeFactoryDeps{
		NewAgentNode: func(spec codec.NodeSpec[plan.SeelexNodeInput]) (node.Node, error) {
			return seenode.NewAgentNode(spec, r.nodeDeps()), nil
		},
		CurrentApprovalGate: r.currentApprovalGate,
		NewSummaryNode: func(spec codec.NodeSpec[plan.SeelexNodeInput]) (node.Node, error) {
			return fork.NewSummaryNode(spec), nil
		},
	}
}

// nodeFactory 返回绑定到 Runtime 的 codec.NodeFactory，供 codec.Import/Render 使用。
func (r *Runtime) nodeFactory() codec.NodeFactory[plan.SeelexNodeInput] {
	return plan.NodeFactory(r.nodeFactoryDeps())
}

// bashDiagnosticMiddleware marks entry to and exit from the framework tool
// registry. Together with scopedBash's process stages, it distinguishes a
// stalled handler from a stall in the registry/framework after the handler
// has already returned. It is no-op unless a diagnostic observer is installed.
func (r *Runtime) bashDiagnosticMiddleware() frameworktools.Middleware {
	return func(name string, next frameworktools.ToolHandler) frameworktools.ToolHandler {
		if name != "bash" {
			return next
		}
		return frameworktools.HandlerFunc(func(ctx context.Context, argsJSON string) (string, error) {
			r.observeBash(BashDiagnosticEvent{Stage: "bash.registry.dispatch.start"})
			result, err := next.Execute(ctx, argsJSON)
			if err != nil {
				r.observeBash(BashDiagnosticEvent{Stage: "bash.registry.dispatch.error", Err: err})
				return result, err
			}
			r.observeBash(BashDiagnosticEvent{Stage: "bash.registry.dispatch.done"})
			return result, nil
		})
	}
}

// seelexVisibilityPolicy 是 bridge.WithVisibilityPolicy 要求的函数类型策略。
// 子代理（Plan kind:agent 节点）与主代理能力一致：完整工具面 + 插件
// include/exclude 过滤同等生效。唯一例外是操作全局状态的工具（plan 工具族、
// task 终态工具）——并发子代理调用会污染主代理的计划状态 / 错误终结任务。
// Dispatch 侧由 agent/bridge.RegistryRuntime 复核同一策略，隐藏工具返回
// ErrToolNotVisible。
func (r *Runtime) seelexVisibilityPolicy(ctx context.Context, tools []types.Tool) []types.Tool {
	scope := model.NodeScopeFromContextOrEmpty(ctx)
	filtered := make([]types.Tool, 0, len(tools))
	for _, tool := range tools {
		name := tool.Function.Name
		if scope.NodeID != "" && scope.Role == model.RoleSubAgent && nodeScopeExcludedTool(name) {
			continue
		}
		// plan 工具面归位（plan.md §6）：主代理与 entry 节点的 plan 工具族
		// 仅在 goal skill 激活时可见（模型自由层默认面 = todolist + fork，
		// 不暴露 plan DAG；entry 节点同主代理语义，避免 DAG 内递归 plan）。
		if scope.Role != model.RoleSubAgent && isPlanTool(name) && !r.node.GoalSkillActive() {
			continue
		}
		filtered = append(filtered, tool)
	}
	return r.plugins.Filter(filtered)
}

// isPlanTool 判断 plan 工具族（goal skill 激活时对主代理可见）。
func isPlanTool(name string) bool {
	switch name {
	case "plan_load", "plan_clear", "plan_run", "plan_status", "plan_export", "plan_validate":
		return true
	default:
		return false
	}
}

// nodeScopeExcludedTool 判断子代理不可见的全局状态工具：这些工具操作
// runtime/会话级单例状态，并行子代理调用会造成语义冲突。其余工具与主代理
// 一致可见。
func nodeScopeExcludedTool(name string) bool {
	switch name {
	case "plan_load", "plan_clear", "plan_run", "plan_status", "plan_export", "plan_validate",
		"task_complete", "task_failed", "task_needs_user_decision",
		"fork_subagents": // fork 会递归派生子代理（无深度控制），同 plan 工具族理由
		return true
	default:
		return false
	}
}

// BashDiagnosticEvent / BashDiagnosticObserver 兼容别名（实现下沉 tools/ 域）。
type (
	BashDiagnosticEvent    = seeltools.BashDiagnosticEvent
	BashDiagnosticObserver = seeltools.BashDiagnosticObserver
)

// registerProjectScopedTools overrides the Seele builtin filesystem tools
// （委托 tools.Router；RegisterBuiltins 内调用）。
func (r *Runtime) registerProjectScopedTools() {
	seeltools.NewRouter(r.scopedToolsDeps()).Register()
}

// observeBash 投递 scoped bash 诊断事件（工具调用不可被诊断改变；观察者
// 意外 panic 也不影响工具调用）。
func (r *Runtime) observeBash(event BashDiagnosticEvent) {
	if r == nil {
		return
	}
	r.bashObserverMu.RLock()
	observer := r.bashObserver
	r.bashObserverMu.RUnlock()
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer(event)
}

// taskToolsDeps 把 Runtime 能力面注入 task 工具族（Deps 全部为闭包）。
func (r *Runtime) taskToolsDeps() task.Deps {
	return task.Deps{
		RegisterTool:         r.RegisterTool,
		ReplaceTodo:          r.tasks.ReplaceTodo,
		AppendTodo:           r.tasks.AppendTodo,
		SetTodoStatusByIndex: r.tasks.SetTodoStatusByIndex,
		TodoSnapshot:         r.TodoSnapshot,
		TaskAdd:              r.TaskAdd,
		TodoMaxItems:         r.limits.TodoMaxItems,
	}
}

// scopedToolsDeps 把 Runtime 能力面注入 tools 域（Deps 全部为闭包）。
func (r *Runtime) scopedToolsDeps() seeltools.Deps {
	return seeltools.Deps{
		RegisterTool:           r.RegisterTool,
		ProjectScope:           r.projectScope,
		FileSystem:             r.filesystem,
		GrepMaxResults:         r.limits.GrepMaxResults,
		WalkTimeoutSec:         r.limits.WalkTimeoutSec,
		ToolCallTimeout:        r.toolCallTimeout,
		ToolCallTimeoutSec:     r.limits.ToolCallTimeoutSec,
		DisableDockerAutoStart: r.limits.DisableDockerAutoStart,
		ObserveBash:            r.observeBash,
		EnsureDocker:           r.ensureDockerForRuntime,
		DockerDaemonDown:       docker.IsDaemonDown,
		DockerCLIPath:          docker.CLIPath,
		DockerHint:             docker.Hint,
	}
}

// NodeWorktree 是单个节点的 worktree 现场（plan_run 生命周期内有效）。
type NodeWorktree = worktree.NodeWorktree

// beginNodeWorktree 为节点创建 worktree（降级返回 nil；语义见
// worktree.WorktreeManager.Begin）。
func (r *Runtime) beginNodeWorktree(scope seenode.NodeScope, nodeID string) *worktree.NodeWorktree {
	if r == nil || r.worktreeMgr == nil {
		return nil
	}
	return r.worktreeMgr.Begin(scope, nodeID)
}

// finishNodeWorktree 收尾：变基仓库 → 提交判定 → 合并审批 → merge → 清理。
func (r *Runtime) finishNodeWorktree(ctx context.Context, nodeID string, wt *worktree.NodeWorktree) error {
	if r == nil || r.worktreeMgr == nil {
		return nil
	}
	return r.worktreeMgr.Finish(ctx, nodeID, wt)
}

// releaseNodeWorktree 在节点结束时从注册表移除（成功路径已清理；失败路径
// 保留现场）。
func (r *Runtime) releaseNodeWorktree(nodeID string) {
	if r == nil || r.worktreeMgr == nil {
		return
	}
	r.worktreeMgr.Release(nodeID)
}
