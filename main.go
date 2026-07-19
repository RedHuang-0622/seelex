// Seelex assembles the Seele agent framework with product-level plugins,
// skills, session storage, and the terminal UI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/agent/core/tool/permission"
	"github.com/RedHuang-0622/Seele/engine"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/plugin"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/skill"
	"github.com/RedHuang-0622/seelex/tui"
)

var (
	configPath     = flag.String("c", "config/account-openai.yaml", "LLM 配置路径")
	storePath      = flag.String("store", ".seelex/sessions", "持久化存储路径")
	pluginsPaths   = flag.String("plugins", "plugins", "Plugin 加载路径（逗号分隔）")
	permissionMode = flag.String("permission", "full_access", "权限模式: full_access(全部放行) | manual(白名单外需审批)")
)

func main() {
	flag.Parse()
	runtime := initRuntime()
	defer runtime.Shutdown()

	runtime.RegisterBuiltins()
	skillRegistry := initSkillSystem()
	pluginManager := initPluginSystem(runtime, skillRegistry)
	store := initStore()
	events := application.NewEventHub()
	approval := application.NewApprovalBroker(events)
	setupPermissionGate(runtime, approval)
	toolHooks := application.NewToolHookBridge()
	eng := initEngine(runtime, store, toolHooks)
	registerProductTools(runtime, pluginManager, eng, approval)
	activateDefaultPlugin(pluginManager, eng)
	sessionManager := initSessionManager(store, eng)
	app := initApplication(eng, runtime, pluginManager, sessionManager, skillRegistry, events, approval)
	defer app.Shutdown()
	toolHooks.Bind(app)
	model := initTUI(app)
	startTUI(model)
}

func initRuntime() *seelebridge.Runtime {
	runtime, err := seelebridge.NewRuntime(seelebridge.RuntimeConfig{
		AccountsPath: *configPath, StorePath: *storePath,
		ToolCallTimeout: 120 * time.Second,
	})
	if err != nil {
		fatalf("初始化 Seele Runtime 失败: %v", err)
	}
	return runtime
}

func initSkillSystem() *skill.Registry {
	// skills are now per-plugin (plugins/<name>/<skill>/SKILL.md).
	// The registry is populated via PublishPluginSkills on plugin Load/Activate.
	return skill.NewRegistry()
}

func initPluginSystem(
	runtime *seelebridge.Runtime,
	skills *skill.Registry,
) *plugin.Manager {
	loader := plugin.NewLoader(splitPaths(*pluginsPaths)...)
	manager := plugin.NewManager(loader, runtime, runtime, skills)
	if err := manager.Load(); err != nil {
		fatalf("加载 Plugin 失败: %v", err)
	}
	return manager
}

func activateDefaultPlugin(manager *plugin.Manager, eng *engine.Engine) {
	if _, err := pluginByName(manager.All(), "default"); err != nil {
		return
	}
	if err := manager.Activate(context.Background(), "default"); err != nil {
		fatalf("激活 default Plugin 失败: %v", err)
	}
	applyPluginPrompt(eng, manager)
}

func registerProductTools(runtime *seelebridge.Runtime, plugins *plugin.Manager, eng *engine.Engine, approval *application.ApprovalBroker) {
	registerTimeTool(runtime)
	registerWebSearchTool(runtime, *configPath)
	registerMCPServers(runtime, *configPath) // from mcpconfig.go — 与 websearch 同一生态位
	registerPluginSwitchTools(runtime, plugins, eng)
	registerAskApprove(runtime, approval)
}

func registerTimeTool(runtime *seelebridge.Runtime) {
	runtime.RegisterTool(
		"get_time",
		"获取当前日期和时间",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		func(context.Context, string) (string, error) {
			return fmt.Sprintf(`"%s"`, time.Now().Format("2006-01-02 15:04:05")), nil
		},
	)
}

func registerPluginSwitchTools(
	runtime *seelebridge.Runtime,
	plugins *plugin.Manager,
	eng *engine.Engine,
) {
	names := make([]interface{}, 0, len(plugins.All())+1)
	for _, p := range plugins.All() {
		names = append(names, p.Name)
	}
	names = append(names, "off")
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"plugin": map[string]interface{}{
				"type": "string", "enum": names, "description": "目标插件",
			},
		},
		"required": []string{"plugin"},
	}
	handler := func(ctx context.Context, argsJSON string) (string, error) {
		var input struct {
			Plugin string `json:"plugin"`
			Mode   string `json:"mode"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
			return "", fmt.Errorf("switch_plugin: %w", err)
		}
		name := strings.ToLower(strings.TrimSpace(input.Plugin))
		if name == "" {
			name = strings.ToLower(strings.TrimSpace(input.Mode))
		}
		if name == "off" || name == "none" || name == "" {
			if err := plugins.Deactivate(ctx); err != nil {
				return "", err
			}
		} else if err := plugins.Activate(ctx, name); err != nil {
			return "", err
		}
		applyPluginPrompt(eng, plugins)
		result := map[string]interface{}{
			"plugin":        runtime.ActivePlugin(),
			"visible_tools": len(runtime.VisibleTools(ctx)),
			"total_tools":   len(runtime.AllTools()),
		}
		encoded, err := json.Marshal(result)
		return string(encoded), err
	}
	runtime.RegisterTool("switch_plugin", "切换 Seelex Plugin 及其工具、Skill 和 MCP", schema, handler)

	legacySchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"mode": map[string]interface{}{
				"type": "string", "enum": names, "description": "目标插件（兼容 mode 名称）",
			},
		},
		"required": []string{"mode"},
	}
	runtime.RegisterTool("switch_mode", "兼容工具：等价于 switch_plugin", legacySchema, handler)
}

func applyPluginPrompt(eng *engine.Engine, plugins *plugin.Manager) {
	current, ok := plugins.Current()
	if !ok {
		eng.SetSystemPrompt("")
		return
	}
	eng.SetSystemPrompt(strings.TrimSpace(current.Prompt))
}

func registerAskApprove(runtime *seelebridge.Runtime, approval *application.ApprovalBroker) {
	runtime.RegisterTool(
		"ask_approve",
		"向用户请求操作确认。当需要执行高风险操作时调用此工具。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"question": map[string]interface{}{"type": "string"},
				"choices": map[string]interface{}{
					"type": "array", "items": map[string]interface{}{"type": "string"},
				},
			},
			"required": []string{"question"},
		},
		func(ctx context.Context, argsJSON string) (string, error) {
			var input struct {
				Question string   `json:"question"`
				Choices  []string `json:"choices,omitempty"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
				return "", fmt.Errorf("ask_approve: %w", err)
			}
			choices := input.Choices
			if len(choices) == 0 {
				choices = []string{"Yes", "No"}
			}
			options := make([]application.InteractionOption, len(choices))
			for i, choice := range choices {
				options[i] = approvalOption(choice)
			}
			decision, err := approval.Request(ctx, application.ApprovalRequest{
				ID: fmt.Sprintf("ask_%d", time.Now().UnixNano()), Question: input.Question,
				Options: options, Risk: "low", ToolName: "ask_approve",
			})
			if err != nil || !approvalAccepted(decision.OptionID) {
				return `{"approved":false,"reason":"cancelled"}`, nil
			}
			encoded, err := json.Marshal(map[string]interface{}{"approved": true, "choice": decision.OptionID})
			return string(encoded), err
		},
	)
}

func initStore() *seelebridge.SessionStore {
	store, err := seelebridge.NewSessionStore(*storePath)
	if err != nil {
		fatalf("初始化存储失败: %v", err)
	}
	return store
}

func initEngine(runtime *seelebridge.Runtime, store *seelebridge.SessionStore, hooks *application.ToolHookBridge) *engine.Engine {
	return engine.New(
		runtime.Agent(),
		engine.WithStore(store.FrameworkStore()),
		engine.WithTracer(seelebridge.NewTracer()),
		engine.WithHooks(hooks.Hooks()),
	)
}

func initSessionManager(store *seelebridge.SessionStore, eng *engine.Engine) *session.Manager {
	manager := session.NewManager(store)
	manager.InjectSaveLoad(
		func(sessionID string) error { return store.Save(sessionID, eng.History()) },
		func(sessionID string) error {
			if _, err := store.Load(sessionID); err != nil {
				return err
			}
			return fmt.Errorf("session resume requires engine history replacement support")
		},
	)
	return manager
}

func initApplication(
	eng *engine.Engine, runtime *seelebridge.Runtime, plugins *plugin.Manager,
	sessions *session.Manager, skills *skill.Registry,
	events *application.EventHub, approval *application.ApprovalBroker,
) *application.Service {
	return application.New(application.Dependencies{
		Engine: enginePort{engine: eng}, Runtime: runtimePort{runtime: runtime},
		Plugins: pluginPort{manager: plugins}, Skills: skillPort{registry: skills},
		Sessions: sessionPort{manager: sessions}, Events: events, Approval: approval,
	})
}

func initTUI(app *application.Service) tui.Model { return tui.NewModel(app) }

func startTUI(model tui.Model) {
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := program.Run(); err != nil {
		fatalf("TUI 错误: %v", err)
	}
}

// setupPermissionGate 根据 -permission 标志安装权限门控。
// full_access：所有工具直接放行（默认）。
// manual：白名单内自动放行，白名单外弹审批框。
func setupPermissionGate(runtime *seelebridge.Runtime, approval *application.ApprovalBroker) {
	mode := permission.Mode(strings.ToLower(strings.TrimSpace(*permissionMode)))
	switch mode {
	case permission.ModeManual:
		cfg := permission.PermissionConfig{
			Mode: permission.ModeManual,
			Rules: []permission.PermissionRule{
				{ToolName: "grep_search", Action: permission.ActionAllow},
				{ToolName: "read_file", Action: permission.ActionAllow},
				{ToolName: "glob", Action: permission.ActionAllow},
				{ToolName: "git_status", Action: permission.ActionAllow},
				{ToolName: "git_log", Action: permission.ActionAllow},
				{ToolName: "git_diff", Action: permission.ActionAllow},
				{ToolName: "get_time", Action: permission.ActionAllow},
				{ToolName: "switch_plugin", Action: permission.ActionAllow},
				{ToolName: "switch_mode", Action: permission.ActionAllow},
				{ToolName: "ask_approve", Action: permission.ActionAllow},
				{ToolName: "plan_load", Action: permission.ActionAllow},
				{ToolName: "plan_run", Action: permission.ActionAllow},
				{ToolName: "plan_status", Action: permission.ActionAllow},
				{ToolName: "plan_export", Action: permission.ActionAllow},
				{ToolName: "plan_clear", Action: permission.ActionAllow},
			},
		}
		runtime.SetPermissionConfig(cfg, newPermissionBridge(approval))
	default:
		cfg := permission.PermissionConfig{Mode: permission.ModeFullAccess}
		runtime.SetPermissionConfig(cfg, nil)
	}
}

// newPermissionBridge 创建连接 permission.ApprovalHandler → ApprovalBroker 的桥接器。
// 每次工具触发审批时，阻塞等待用户在 TUI 交互面板中作出选择。
func newPermissionBridge(broker *application.ApprovalBroker) permission.ApprovalHandler {
	return func(ctx *permission.ApprovalContext) (*permission.ApprovalResponse, error) {
		req := ctx.Request
		appReq := application.ApprovalRequest{
			ID:       req.ID,
			Question: req.Preview,
			Options:  convertPermissionOptions(req.Options),
			Risk:     req.Risk,
			ToolName: req.ToolName,
			Preview:  req.Preview,
			Timeout:  req.Timeout,
		}
		decision, err := broker.Request(context.Background(), appReq)
		if err != nil {
			return nil, err
		}
		remember := decision.OptionID == "always"
		return &permission.ApprovalResponse{
			RequestID: req.ID,
			Choice:    decision.OptionID,
			Remember:  remember,
		}, nil
	}
}

func splitPaths(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func pluginByName(plugins []plugin.Plugin, name string) (plugin.Plugin, error) {
	for _, p := range plugins {
		if p.Name == name {
			return p, nil
		}
	}
	return plugin.Plugin{}, fmt.Errorf("plugin %q not found", name)
}

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "✖ "+format+"\n", args...)
	os.Exit(1)
}
