// Package seelebridge adapts Seele framework primitives to stable Seelex APIs.
package seelebridge

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/agent/core/tool/builtin"
	"github.com/RedHuang-0622/Seele/agent/core/tool/holder"
	"github.com/RedHuang-0622/Seele/agent/core/tool/permission"
	"github.com/RedHuang-0622/Seele/types"
	workplanTypes "github.com/RedHuang-0622/Seele/workplan/core/types"
	"github.com/RedHuang-0622/Seele/workplan/runtime/forkexec"
	"github.com/RedHuang-0622/Seele/workplan/sugar/approve"

	"github.com/RedHuang-0622/seelex/mcpstack"
)

// RuntimeConfig contains the Seelex-facing subset of Agent configuration.
type RuntimeConfig struct {
	MaxReplanProviderRequests int
	MaxConcurrentReplans      int
	MaxReplansPerWindow       int
	ReplanWindow              time.Duration
	AccountsPath              string        // LLM 账号配置路径
	StorePath                 string        // 会话存储目录（空 = 不持久化）
	ToolCallTimeout           time.Duration // 工具调用超时
	HubStartupDelay           time.Duration // Hub 启动等待时间
}

// Runtime owns one Seele Agent and exposes application-oriented facades.
type Runtime struct {
	agent  *agent.Agent
	client *api.ChatClient
	pool   *api.AccountPool
	model  string

	// MCPStack 记录所有 MCP 调用的 trace（熔断事件 + 调用记录）。
	// AttachMCP 时自动启动熔断事件监听，无需手动装配。
	MCPStack *mcpstack.MCPStack

	breaker           *breakerState         // 熔断器事件 channel 状态
	planTool          *builtin.WorkPlanTool // plan 工具，用于设置进度回调
	branchMu          sync.RWMutex
	branchBinding     PlanBranchBinding
	planPolicyMu      sync.RWMutex
	planPolicy        PlanPolicy
	replanGuard       *replanGuard
	selectedAccountID string
	projectScope      *ProjectScope
	toolCallTimeout   time.Duration
	scopedToolsReady  bool
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
	pool, _, err := loadSimplifiedConfig(cfg.AccountsPath)
	if err != nil {
		return nil, fmt.Errorf("seelebridge: load accounts: %w", err)
	}
	accounts := pool.All()
	if len(accounts) == 0 {
		return nil, fmt.Errorf("seelebridge: accounts configuration is empty")
	}
	first := accounts[0]
	llmCfg := types.LLMConfig{
		BaseURL: first.BaseURL, APIKey: first.APIKey, Model: first.Model,
		MaxTokens: 200000, Timeout: 300,
		Temperature: 0.7,
	}
	agt, err := agent.New(agent.Options{
		LLMConfig: llmCfg, ToolCallTimeOut: cfg.ToolCallTimeout,
		HubStartupDelay: cfg.HubStartupDelay,
	})
	if err != nil {
		return nil, fmt.Errorf("seelebridge: create agent: %w", err)
	}
	client, ok := agt.LLM().(*api.ChatClient)
	if !ok {
		agt.Shutdown()
		return nil, fmt.Errorf("seelebridge: unsupported LLM client %T", agt.LLM())
	}
	client.WithAccountPool(pool)
	client.SetProvider("openai")
	agt.Tools().WithPluginManager(holder.NewPluginManager())
	// 把 main.go 配置的 ToolCallTimeout（120s）传给 holder，
	// 否则 holder.New() 默认只有 30s，FreeCAD 复杂操作极易超时熔断。
	agt.Tools().ToolCallTimeout = cfg.ToolCallTimeout

	mcpStackOpts := []mcpstack.Option{
		mcpstack.WithSessionID(fmt.Sprintf("mcp-%d", time.Now().Unix())),
	}
	if cfg.StorePath != "" {
		mcpStackOpts = append(mcpStackOpts,
			mcpstack.WithAutoSave(filepath.Join(cfg.StorePath, "mcp-traces.json")))
	}

	r := &Runtime{
		agent:           agt,
		client:          client,
		pool:            pool,
		model:           first.Model,
		MCPStack:        mcpstack.New(mcpStackOpts...),
		projectScope:    NewProjectScope(),
		toolCallTimeout: cfg.ToolCallTimeout,
		replanGuard:     newReplanGuard(cfg.MaxConcurrentReplans, cfg.MaxReplansPerWindow, cfg.MaxReplanProviderRequests, cfg.ReplanWindow),
	}

	return r, nil
}

// Agent returns the framework object required by engine.New.
func (r *Runtime) Agent() *agent.Agent { return r.agent }

func (r *Runtime) Shutdown() {
	if r != nil && r.agent != nil {
		r.agent.Shutdown()
	}
}

func (r *Runtime) Model() string { return r.model }

func (r *Runtime) RegisterBuiltins() {
	builtin.RegisterAll(r.agent.Tools())
	r.registerProjectScopedTools()
	r.scopedToolsReady = true
	r.planTool = builtin.NewWorkPlanTool(builtin.NewChatAgentFactory(r.agent.LLM()))
	r.planTool.SetBranchRuntimeResolver(r.resolvePlanBranchRuntime)
	r.agent.Tools().Register(&planToolProvider{tool: r.planTool, policy: r.currentPlanPolicy})
}

// BindProjectRoot makes the supplied project the only root used by Seelex
// filesystem tools for the active session.
func (r *Runtime) BindProjectRoot(rootPath string) error { return r.projectScope.Bind(rootPath) }

// UnbindProjectRoot makes filesystem and shell tools fail closed until a
// project is selected.
func (r *Runtime) UnbindProjectRoot() { r.projectScope.Unbind() }

// SetPlanNodeCallback 设置 plan 每节点完成回调（seelex plan visualization）。
// cb 直接接收框架原生 *types.NodeResult，不做签名变换。
func (r *Runtime) SetPlanNodeCallback(cb func(nr *workplanTypes.NodeResult)) {
	if r.planTool != nil {
		r.planTool.ProgressCallback = cb
	}
}

// SetPlanApprovalGate 设置 plan kind:manual 节点的审批门控。
// gate 连接 ApprovalBroker，human-in-the-loop 审批通过时继续执行节点。
func (r *Runtime) SetPlanApprovalGate(gate approve.ApprovalGate) {
	if r.planTool != nil {
		r.planTool.SetGate(gate)
	}
}

// SetPlanBranchCallback registers a Seelex-facing branch lifecycle callback.
func (r *Runtime) SetPlanBranchCallback(callback func(PlanBranchEvent)) {
	if r.planTool == nil {
		return
	}
	r.planTool.SetBranchEventHook(func(event forkexec.Event) {
		if callback == nil {
			return
		}
		branchEvent := PlanBranchEvent{
			Type: string(event.Type), BranchID: event.BranchID, NodeID: event.NodeID,
			At: event.At,
		}
		if event.Err != nil {
			branchEvent.Error = event.Err.Error()
		}
		callback(branchEvent)
	})
}

// SetPlanBranchBinding freezes context and account-selection inputs for the
// next plan run. Branches receive private clients created from this binding.
func (r *Runtime) SetPlanBranchBinding(binding PlanBranchBinding) {
	r.setPlanBranchBinding(binding)
}

// SetPlanPolicy updates constraints applied to subsequent plan_load calls.
// A loaded WorkPlan retains the concurrency selected when it was created.
func (r *Runtime) SetPlanPolicy(policy PlanPolicy) {
	r.planPolicyMu.Lock()
	r.planPolicy = policy
	r.planPolicyMu.Unlock()
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
	r.agent.RegisterTool(name, description, inputSchema, handler)
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
	return summarizeTools(r.agent.Tools().Tools())
}

func (r *Runtime) VisibleTools(ctx context.Context) []Tool {
	return summarizeTools(r.agent.VisibleTools(ctx))
}

func (r *Runtime) Accounts() []Account {
	accounts := r.pool.All()
	result := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		result = append(result, Account{
			Name: account.Name, Provider: string(account.Provider), Model: account.Model,
			Disabled: account.Disabled,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func (r *Runtime) SelectAccount(name string) bool {
	account := r.pool.Select(name)
	if account == nil {
		return false
	}
	r.client.SetProviderFilter(account.Provider)
	r.setSelectedAccount(account.Name)
	return true
}

func (r *Runtime) Provider() string { return string(r.client.ProviderFilter()) }

func (r *Runtime) SetProvider(provider string) {
	r.client.SetProviderFilter(api.ProviderType(provider))
}

// SetPermissionConfig 安装权限门控：Mode + Rules + ApprovalHandler。
func (r *Runtime) SetPermissionConfig(cfg permission.PermissionConfig, handler permission.ApprovalHandler) {
	r.agent.SetPermissionConfig(cfg, handler)
}

func (r *Runtime) SetFullAccess(on bool) {
	if on {
		r.agent.SetPermissionConfig(permission.PermissionConfig{Mode: permission.ModeFullAccess}, nil)
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
