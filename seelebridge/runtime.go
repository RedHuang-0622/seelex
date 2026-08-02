// Package seelebridge adapts Seele framework primitives to stable Seelex APIs.
package seelebridge

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"sync"
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
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/sessionstore"
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

	mcpProvider         *frameworkmcp.Provider
	breaker             *breakerState // 熔断器事件 channel 状态
	branchMu            sync.RWMutex
	branchBinding       PlanBranchBinding
	planPolicyMu        sync.RWMutex
	planPolicy          PlanPolicy
	planProvider        *planToolProvider
	planEvents          *planEventSink              // plan 执行事实 → 事件库 + 投影订阅
	eventErrorHandler   frameworkevent.ErrorHandler // Sink 失败隔离（不破坏 WorkPlan 控制流）
	replanGuard         *replanGuard
	agentFactoryMu      sync.RWMutex
	agentFactory        node.AgentFactory // bridge.NewAgentFactory 产物（plan 子代理工厂）
	approvalGateMu      sync.RWMutex
	approvalGate        approve.ApprovalGate
	parentEvidence      *parentEvidenceState // 节点子代理父证据（seelexctx snapshot 承袭）
	selectedAccountID   string
	providerFilter      string
	projectScope        *ProjectScope
	toolCallTimeout     time.Duration
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
	projectMu        sync.RWMutex
	projectKnowledge func() *sessionstore.ProjectRecord
	turnArchiverMu   sync.RWMutex
	turnArchiver     seelexctx.TurnArchiver // 压缩轮次原文归档（main 装配注入）
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
		projectScope:        NewProjectScope(),
		toolCallTimeout:     cfg.ToolCallTimeout,
		planDecisionTimeout: planDecisionTimeout,
		approvalTimeout:     approvalTimeout,
		heartbeatInterval:   heartbeatInterval,
		limits:              cfg.Limits.WithDefaults(),
		replanGuard:         newReplanGuard(cfg.MaxConcurrentReplans, cfg.MaxReplansPerWindow, cfg.MaxReplanProviderRequests, cfg.ReplanWindow),
		pluginDefs:          make(map[string]pluginDef),
		permission:          &permissionGateState{},
		planEvents:          newPlanEventSink(),
		parentEvidence:      &parentEvidenceState{},
		window:              seelexctx.NewDefaultWindowPolicy(cfg.WindowConfig),
		tracer:              tracer,
		hook:                hook,
		eventErrorHandler:   func(_ context.Context, err error) { log.Printf("seelebridge: event sink: %v", err) },
	}

	// 2. 工具注册表：WithCallTimeout 保留工具超时语义；权限门控作为 middleware。
	r.registry = &toolsRegistryState{registry: newToolsRegistry(cfg.ToolCallTimeout, r.permission, approvalTimeout)}

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
	r.planProvider = newPlanToolProvider(r)

	// 5. Plan 子代理工厂：bridge.NewAgentFactory（每节点独立 Session，
	//    工作历史隔离；节点会话组件见 branch.go nodeSessionComponents）。
	planAgentFactory, err := bridge.NewAgentFactory(agt,
		bridge.WithSessionComponents(r.nodeSessionComponents()),
		bridge.WithSessionID(r.nodeSessionID),
	)
	if err != nil {
		return nil, fmt.Errorf("seelebridge: create plan agent factory: %w", err)
	}
	r.agentFactory = planAgentFactory
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
// 由框架自动生成。History 为空时会话持久化继续由 application 层（Router
// SaveCommit）负责；DurableHistory 接线在后续切片随会话恢复流程对齐。
func (r *Runtime) NewMainSession(hooks *session.LoopHooks) (*session.Session, error) {
	return r.newMainSession("", hooks)
}

func (r *Runtime) newMainSession(sessionID string, hooks *session.LoopHooks) (*session.Session, error) {
	sess, err := session.NewSession(session.SessionComponents{
		Agent:     r.agt,
		Context:   r.mainContextComponents(),
		Hooks:     hooks,
		Telemetry: r.hook,
		SessionID: sessionID,
		ModelName: r.model,
	})
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
	if r != nil && r.agt != nil {
		r.agt.Shutdown()
	}
}

func (r *Runtime) Model() string { return r.model }

// Tracer 返回遥测内存追踪器（trace 视图查询源，见 seelebridge/trace.go）。
// GUI/TUI 经 enginePort 查询（TraceText/TokenCount）；生命周期事件
// （llm/tool intent-effect）由会话级 hook 写入。
func (r *Runtime) Tracer() *telemetry.MemoryTracer { return r.tracer }

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
	r.scopedToolsReady = true
	// plan 工具（seelex-workplan provider）：plan_load/plan_clear/plan_validate/
	// plan_status/plan_export；plan_run 的执行内核在 seele-v2 slice 4 迁移后恢复。
	if err := r.registry.registry.Register(r.planProvider); err != nil {
		return
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
	r.planEvents.Subscribe(cb)
}

// SetEventPersister 安装执行事实持久化钩子（双轨事件的事实轨：
// event.Sink → sessionstore 事件库）。钩子失败经 ErrorHandler 隔离，
// 不破坏 WorkPlan 控制流（见 Seele event/README.md）。
func (r *Runtime) SetEventPersister(fn func(context.Context, frameworkevent.Event) error) {
	r.planEvents.SetPersister(fn)
}

// SetEventErrorHandler 覆盖 Sink 失败处理器（默认 log.Printf 兜底）。
func (r *Runtime) SetEventErrorHandler(handler frameworkevent.ErrorHandler) {
	if handler == nil {
		return
	}
	r.eventErrorHandler = handler
}

// SetPlanApprovalGate 设置 plan kind:approve/manual 节点的审批门控；
// approvalGateNode 在 Run 时读取当前门（延迟读取，可在 plan_load 之后设置）。
func (r *Runtime) SetPlanApprovalGate(gate approve.ApprovalGate) {
	r.approvalGateMu.Lock()
	r.approvalGate = gate
	r.approvalGateMu.Unlock()
}

// currentApprovalGate 返回当前审批门（approvalGateNode 的读取器）。
func (r *Runtime) currentApprovalGate() approve.ApprovalGate {
	r.approvalGateMu.RLock()
	defer r.approvalGateMu.RUnlock()
	return r.approvalGate
}

// currentAgentFactory 返回当前 plan 子代理工厂（SeelexAgentNode 的读取器）。
func (r *Runtime) currentAgentFactory() node.AgentFactory {
	r.agentFactoryMu.RLock()
	defer r.agentFactoryMu.RUnlock()
	return r.agentFactory
}

// SetPlanBranchBinding freezes context and account-selection inputs for the
// next plan run.
func (r *Runtime) SetPlanBranchBinding(binding PlanBranchBinding) {
	r.setPlanBranchBinding(binding)
}

// SetPlanPolicy updates constraints applied to subsequent plan_load calls.
func (r *Runtime) SetPlanPolicy(policy PlanPolicy) {
	r.planPolicyMu.Lock()
	r.planPolicy = policy
	r.planPolicyMu.Unlock()
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
	r.planPolicyMu.RLock()
	defer r.planPolicyMu.RUnlock()
	return r.planPolicy
}

// ReplanMetrics returns process-wide replan cost and rejection accounting.
func (r *Runtime) ReplanMetrics() ReplanMetrics {
	if r == nil || r.replanGuard == nil {
		return ReplanMetrics{}
	}
	return r.replanGuard.snapshot()
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
