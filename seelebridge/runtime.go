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
	"github.com/RedHuang-0622/Seele/seelectx"
	"github.com/RedHuang-0622/Seele/session"
	"github.com/RedHuang-0622/Seele/telemetry"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/mcpstack"
	"github.com/RedHuang-0622/seelex/seelebridge/account"
	"github.com/RedHuang-0622/seelex/seelebridge/fork"
	"github.com/RedHuang-0622/seelex/seelebridge/fs"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/config"
	"github.com/RedHuang-0622/seelex/seelebridge/internal/docker"
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
	"github.com/RedHuang-0622/seelex/sessionstore"
	"github.com/RedHuang-0622/seelex/skill"
)

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

	model string

	// MCPStack 记录所有 MCP 调用的 trace（熔断事件 + 调用记录）。
	// AttachMCP 时自动启动熔断事件监听，无需手动装配。
	MCPStack *mcpstack.MCPStack

	mcpManager           *mcp.Manager      // MCP 服务器生命周期（mcp/ 域）
	planExecutor         *plan.Executor    // Plan 执行域组件（plan_executor.go）：策略/绑定/runID/事件/replan/工厂
	accounts             *account.Manager  // 账号路由状态（account/ 域）：选中账号/provider/限额
	visibilityPolicy     *seeltools.Policy // 可见性策略（tools/ 域）：节点作用域 + 插件过滤
	visibilityProjection atomic.Pointer[RuntimeVisibilityProjection]
	// subagentContext 是子代理上下文 actor（装配件拆分第一步）：父证据
	// 的读-合并-写回与 merge-back 队列收进单一 goroutine（channel 命令 +
	// 串行处理），替代原来的 parentEvidenceMu / subagentOverflowMu 两把锁。
	subagentContext *subagentsession.SubagentContextActor
	// skills 是子代理 skill 目录的 actor 资源：skill.Registry 内部自锁
	// （读写即消息进出：All/Get 读、Register/Reload 写），见 skill/skill.go。
	// 装配一次性写入、运行期只读消费，与 filesystem actor 同构，无需外层锁。
	skills       *skill.Registry
	projectScope *security.ProjectScope
	filesystem   fs.FileSystem             // 文件系统 actor（写路径分片串行化，filesystem_actor.go）
	sandbox      security.CommandSandbox   // shell 执行隔离端口（security/sandbox.go；默认 native cwd-gate）
	dockerProbe  docker.Prober             // docker 守护进程探测/启动（零值 → 真实实现；测试注入）
	node         *seenode.Coordinator      // 节点协调器（node/ 域）：会话注册/fork 树/task 打点/plan 阶段/Blocks
	worktreeMgr  *worktree.WorktreeManager // 子代理 worktree 生命周期组件（worktree/ 域）
	forkTool     *fork.Tool                // fork_subagents 执行编排（fork/ 域）
	tasks        *task.TaskRegistry        // task 注册表 actor（task/；todolist 融合为 kind=todo 的 task）
	scheduler    *scheduler.State          // 定时周期任务 actor（scheduler/ 域）
	toolEvents   *subagentsession.ToolEventState
	// live* 是 node 第一视角实时流分发器（runtime_live.go）：阶段+工具事件
	// 统一通道按 nodeID 广播；liveStarted/liveStop 保护启动与停机。
	liveMu         sync.Mutex
	liveStarted    bool
	liveStop       chan struct{}
	liveCh         chan dto.SubagentLiveEvent
	liveSubs       map[string][]chan dto.SubagentLiveEvent
	liveToolCancel func()

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
	lifecycle           []func() // 生命周期登记（NewRuntime 装配序；Shutdown 逆序）

	plugins *plugin.Manager // 插件可见性配置（plugin/ 域）

	permission *seeltools.PermissionGate

	// 上下文控制接线（seelebridge/context_components.go）：
	// 窗口策略（RuntimeConfig.WindowConfig 构造）、会话上下文存储与
	// ProjectKnowledge 提供者为可选注入（会话恢复流程就绪后 Attach）。
	windowMu        sync.RWMutex
	window          seelexctx.WindowPolicy
	bindings        sessionBindings // 会话绑定状态归组（runtime_session.go）
	lazyMCPServerMu sync.RWMutex
	lazyMCPServers  map[string]MCPServer // 已登记未连接的 MCP 服务器（冷启动）
}

// MainSessionID 返回当前主会话 ID（压缩帧 SegmentID 溯源；空 = 未创建）。
func (r *Runtime) MainSessionID() string {
	if r == nil {
		return ""
	}
	return r.bindings.sessionID()
}

// SetTurnArchiver 注入压缩轮次原文归档实现（application 层持久化通道）。
// 注入后窗口外压缩会把溢出轮次原文持久化，帧 Evidence 携带读回句柄，
// 模型可经 read_compressed_turn 工具读回（压缩丢失可逆）。
func (r *Runtime) SetTurnArchiver(archiver seelexctx.TurnArchiver) {
	r.bindings.setTurnArchiver(archiver)
}

// Account 账号路由摘要（域本体在 account/）。
type Account = account.Account
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
	// 账号路由状态收敛为 account.Manager：选中账号/provider 过滤/限额。
	r.accounts = account.NewManager(loaded.Specs, loaded.Limits, first.Name, pool)
	// ── 拓扑序装配链（config→account→registry→completer→agent→plan→node→…）──
	// 2. 工具注册表：WithCallTimeout 保留工具超时语义；权限门控作为 middleware。
	r.registry = seeltools.NewRegistryState(cfg.ToolCallTimeout, r.permission, approvalTimeout, r.toolEvents.Middleware(), r.bashDiagnosticMiddleware())

	// 3. Completer / StreamCompleter（共享账号选择器，无 api.ChatClient 强转）
	if err := r.assembleCompleters(); err != nil {
		return nil, err
	}

	// 4. 可见性策略：goal skill 激活判定 + 插件过滤，经闭包注入 tools.Policy。
	// （GoalSkillActive 是运行期状态，闭包在 Dispatch 期求值；node 可能尚未
	// 装配，nil 防护回退 false。）
	r.visibilityPolicy = seeltools.NewPolicy(seeltools.PolicyDeps{
		GoalSkillActive: func() bool {
			if r.node == nil {
				return false
			}
			return r.node.GoalSkillActive()
		},
		PluginFilter: r.plugins.Filter,
	})

	// 5. Agent 装配：agent.NewWithComponents（不启动 Hub / 账号池 / 网关）
	runtimeAdapter, err := bridge.NewRegistryRuntime(r.registry.Registry,
		bridge.WithVisibilityPolicy(r.visibilityPolicy.Filter),
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

	// 6. plan 执行域组件：策略 / 绑定 / runID / 事件通道 / replan 护拦 / 子代理工厂。
	//    依赖面此时已全部就绪（registry/accounts/agt），无前向闭包；
	//    NodeFactory 经 SetNodeFactory 后置注入（plan→node 构造环）。
	r.planExecutor = plan.NewExecutor(plan.ExecutorDeps{
		Model:               r.model,
		Heartbeat:           heartbeatInterval,
		Limits:              r.limits,
		PlanDecisionTimeout: planDecisionTimeout,
		Accounts:            r.accountSpecList,
		LoadPlanDefinition:  func() (types.Tool, bool) { return r.registry.FindTool("plan_load") },
		Dispatch:            r.agentDispatch,
	}, cfg.MaxConcurrentReplans, cfg.MaxReplansPerWindow, cfg.MaxReplanProviderRequests, cfg.ReplanWindow)

	// 7. 节点协调器：session/fork/task/plan 经接口与闭包注入，域内不反向依赖 Runtime。
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
	// 8. 回填节点工厂（node 已就绪；plan_load 在运行期才消费）。
	r.planExecutor.SetNodeFactory(r.nodeFactory)

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
	// node 第一视角：把 node 会话的 llm/tool telemetry 边界投影为同节点
	// 分阶段日志（NodeScope 已在 AgentNode.Run 注入 ctx；主会话无 NodeScope
	// 被过滤）。best-effort，绝不改变执行路径。
	r.hook = seeletelemetry.NewStageHook(r.hook, r.node)

	// 9. MCP 管理器：MCPStack 承接熔断事件 trace，注册表此时已装配。
	r.mcpManager = mcp.NewManager(r.MCPStack, mcpRegistryAdapter{runtime: r})

	// 10. Plan 子代理工厂：bridge.NewAgentFactory（每节点独立 Session，
	//    工作历史隔离；节点会话组件见 branch.go nodeSessionComponents）。
	planAgentFactory, err := bridge.NewAgentFactory(agt,
		bridge.WithSessionComponents(r.nodeSessionComponents()),
		bridge.WithSessionID(r.nodeSessionID),
	)
	if err != nil {
		return nil, fmt.Errorf("seelebridge: create plan agent factory: %w", err)
	}
	r.planExecutor.SetAgentFactory(planAgentFactory)
	// 生命周期登记（装配序；Shutdown 逆序关停）。只登记真实资源持有者；
	// planExecutor/forkTool 无自有 goroutine，不登记。
	r.lifecycle = nil
	if r.tasks != nil {
		r.lifecycle = append(r.lifecycle, r.tasks.Close)
	}
	if r.subagentContext != nil {
		r.lifecycle = append(r.lifecycle, r.subagentContext.Close)
	}
	if r.subagentSessions != nil {
		r.lifecycle = append(r.lifecycle, r.subagentSessions.Close)
	}
	if r.scheduler != nil {
		r.lifecycle = append(r.lifecycle, r.scheduler.Stop)
	}
	if r.worktreeMgr != nil {
		r.lifecycle = append(r.lifecycle, r.worktreeMgr.Close)
	}
	if r.node != nil {
		r.lifecycle = append(r.lifecycle, r.node.Close)
	}
	if r.mcpManager != nil {
		r.lifecycle = append(r.lifecycle, r.mcpManager.Close)
	}
	if r.agt != nil {
		r.lifecycle = append(r.lifecycle, r.agt.Shutdown)
	}
	return r, nil
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
	r.bindings.attachHistoryRouter(router)
}
func (r *Runtime) durableHistoryRouter() *sessionstore.Router {
	return r.bindings.getHistoryRouter()
}

// PrepareMainSessionHistory arms the Runtime-owned DurableHistory to hand the
// application-assembled provider history to the next framework ChatStream.
// Runtime owns only this cache; it never calls back into Application.
func (r *Runtime) PrepareMainSessionHistory(sessionID string, messages []types.Message) bool {
	if r == nil {
		return false
	}
	return r.bindings.prepareMainSessionHistory(sessionID, messages)
}
func (r *Runtime) newMainSession(sessionID string, hooks *session.LoopHooks) (*session.Session, error) {
	if sessionID == "" {
		sessionID = fmt.Sprintf("sess_%d", time.Now().UnixNano())
	}
	r.bindings.setSessionID(sessionID)
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
		r.bindings.setMainHistory(history)
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
	r.stopLiveDispatcher()
	// 逆装配序统一关停（NewRuntime 登记）；幂等由各实现保证。
	for index := len(r.lifecycle) - 1; index >= 0; index-- {
		if r.lifecycle[index] != nil {
			r.lifecycle[index]()
		}
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

// BindProjectRoot makes the supplied project the only root used by Seelex
// filesystem tools for the active session.
func (r *Runtime) BindProjectRoot(rootPath string) error { return r.projectScope.Bind(rootPath) }

// UnbindProjectRoot makes filesystem and shell tools fail closed until a
// project is selected.
func (r *Runtime) UnbindProjectRoot() { r.projectScope.Unbind() }
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

type runtimeBudgetProvider struct{ runtime *Runtime }

func (p runtimeBudgetProvider) ContextTokens() int   { return p.runtime.ContextWindow() }
func (p runtimeBudgetProvider) MaxOutputTokens() int { return p.runtime.MaxOutputTokens() }

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

var (
	_ seelexctx.BudgetProvider  = runtimeBudgetProvider{}
	_ session.ContextComponents = session.ContextComponents{}
)

type dockerProbe = docker.Prober
type (
	BashDiagnosticEvent    = seeltools.BashDiagnosticEvent
	BashDiagnosticObserver = seeltools.BashDiagnosticObserver
)
type NodeWorktree = worktree.NodeWorktree
