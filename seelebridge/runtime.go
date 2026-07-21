// Package seelebridge adapts Seele framework primitives to stable Seelex APIs.
package seelebridge

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/RedHuang-0622/Seele/agent"
	"github.com/RedHuang-0622/Seele/agent/core/api"
	"github.com/RedHuang-0622/Seele/agent/core/tool/builtin"
	"github.com/RedHuang-0622/Seele/agent/core/tool/holder"
	"github.com/RedHuang-0622/Seele/agent/core/tool/permission"
	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/mcpstack"
)

// RuntimeConfig contains the Seelex-facing subset of Agent configuration.
type RuntimeConfig struct {
	AccountsPath    string        // LLM 账号配置路径
	StorePath       string        // 会话存储目录（空 = 不持久化）
	ToolCallTimeout time.Duration // 工具调用超时
	HubStartupDelay time.Duration // Hub 启动等待时间
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

	breaker *breakerState // 熔断器事件 channel 状态
	planTool *builtin.WorkPlanTool // plan 工具，用于设置进度回调
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
	loaded, err := api.LoadFullAccountsConfig(cfg.AccountsPath)
	if err != nil {
		return nil, fmt.Errorf("seelebridge: load accounts: %w", err)
	}
	accounts := loaded.Pool.All()
	if len(accounts) == 0 {
		return nil, fmt.Errorf("seelebridge: accounts configuration is empty")
	}
	first := accounts[0]
	defaults := loaded.LLMDefaults
	llmCfg := types.LLMConfig{
		BaseURL: first.BaseURL, APIKey: first.APIKey, Model: first.Model,
		MaxTokens: defaults.MaxTokens, Timeout: defaults.Timeout,
		Temperature: defaults.Temperature,
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
	client.WithAccountPool(loaded.Pool)
	if defaults.Provider != "" {
		client.SetProvider(defaults.Provider)
	}
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
		agent:    agt,
		client:   client,
		pool:     loaded.Pool,
		model:    first.Model,
		MCPStack: mcpstack.New(mcpStackOpts...),
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
	r.planTool = builtin.NewWorkPlanTool(builtin.NewChatAgentFactory(r.agent.LLM()))
	r.agent.Tools().Register(r.planTool)
}

// SetPlanNodeCallback 设置 plan 每节点完成回调（seelex plan visualization）。
// 可在 runtime 创建后、服务初始化完成后调用。
func (r *Runtime) SetPlanNodeCallback(cb func(nodeID, kind, status string, elapsed time.Duration)) {
	if r.planTool != nil {
		r.planTool.ProgressCallback = cb
	}
}

func (r *Runtime) RegisterTool(
	name, description string,
	inputSchema map[string]interface{},
	handler func(context.Context, string) (string, error),
) {
	r.agent.RegisterTool(name, description, inputSchema, handler)
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
