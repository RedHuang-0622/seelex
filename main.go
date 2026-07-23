// Seelex assembles the Seele agent framework with product-level plugins,
// skills, session storage, and the terminal UI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/agent/core/tool/permission"
	"github.com/RedHuang-0622/Seele/engine"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/gui"
	"github.com/RedHuang-0622/seelex/plugin"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/skill"
	"github.com/RedHuang-0622/seelex/tui"
)

var (
	storePath      = flag.String("store", ".seelex/sessions", "持久化存储路径")
	pluginsPaths   = flag.String("plugins", "plugins", "Plugin 加载路径（逗号分隔）")
	permissionMode = flag.String("permission", "manual", "权限模式: manual(白名单外需审批) | full_access(全部放行)")
	frontendMode   = flag.String("frontend", DefaultFrontend, "前端模式: tui | gui")
	showVersion    = flag.Bool("version", false, "显示版本号并退出")
)

// accountsPath 返回 accounts.yaml 的路径。
// 优先使用二进制所在目录（正式部署），回退到当前工作目录（go run / 开发场景）。
func accountsPath() string {
	exe, err := os.Executable()
	if err == nil {
		p := filepath.Join(filepath.Dir(exe), "config", "accounts.yaml")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return filepath.Join("config", "accounts.yaml")
}

func main() {
	flag.Parse()
	if *showVersion {
		fmt.Println(Version)
		return
	}
	frontend, err := parseFrontendMode(*frontendMode)
	if err != nil {
		fatalf("前端模式无效: %v", err)
	}
	*frontendMode = frontend
	if frontend == "gui" && !gui.Available() {
		fatalf(`当前二进制未包含 GUI；请使用 go run -tags "gui,desktop,production" . -frontend gui`)
	}
	mode, err := parsePermissionMode(*permissionMode)
	if err != nil {
		fatalf("权限模式无效: %v", err)
	}
	*permissionMode = string(mode)
	*storePath = resolveStorePath(*storePath)

	runtime := initRuntime()
	defer runtime.Shutdown()

	runtime.RegisterBuiltins()
	skillRegistry := initSkillSystem()
	pluginManager := initPluginSystem(runtime, skillRegistry)
	store := initStore()
	events := application.NewEventHub()
	approval := application.NewApprovalBroker(events)
	if err := setupPermissionGate(runtime, approval); err != nil {
		fatalf("权限模式无效: %v", err)
	}
	toolHooks := application.NewToolHookBridge()
	frameworkEngine := initEngine(runtime, toolHooks)
	registerProductTools(runtime, pluginManager, frameworkEngine, approval)
	activateDefaultPlugin(pluginManager, frameworkEngine)
	appEngine := newEnginePort(frameworkEngine)
	sessionManager := initSessionManager(store, appEngine)
	app := initApplication(appEngine, runtime, pluginManager, sessionManager, skillRegistry, events, approval)
	defer app.Shutdown()
	toolHooks.Bind(app)
	runtime.SetPlanNodeCallback(app.HandlePlanNodeComplete)
	startFrontend(app)
}

func initRuntime() *seelebridge.Runtime {
	runtime, err := seelebridge.NewRuntime(seelebridge.RuntimeConfig{
		AccountsPath: accountsPath(), StorePath: *storePath,
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
	// 系统提示词由 application.Service.buildSystemPrompt 在 initApplication 时组装，
	// 不要在启动时直接覆盖 engine 的 system prompt。
	// applyPluginPrompt(eng, manager)
}

func registerProductTools(runtime *seelebridge.Runtime, plugins *plugin.Manager, eng *engine.Engine, approval *application.ApprovalBroker) {
	registerTimeTool(runtime)
	registerWebSearchTool(runtime, accountsPath())
	registerMCPServers(runtime, accountsPath()) // from mcpconfig.go — 与 websearch 同一生态位
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

func initEngine(runtime *seelebridge.Runtime, hooks *application.ToolHookBridge) *engine.Engine {
	return engine.New(
		runtime.Agent(),
		engine.WithTracer(seelebridge.NewTracer()),
		engine.WithHooks(hooks.Hooks()),
	)
}

func initSessionManager(store *seelebridge.SessionStore, eng *enginePort) *session.Manager {
	manager := session.NewManager(store)
	manager.InjectSaveLoad(
		func(sessionID string) error { return store.Save(sessionID, eng.rawHistory()) },
		func(sessionID string) error {
			history, err := store.Load(sessionID)
			if err != nil {
				return err
			}
			return eng.replaceRawHistory(sessionID, history)
		},
	)
	return manager
}

func initApplication(
	eng *enginePort, runtime *seelebridge.Runtime, plugins *plugin.Manager,
	sessions *session.Manager, skills *skill.Registry,
	events *application.EventHub, approval *application.ApprovalBroker,
) *application.Service {
	return application.New(application.Dependencies{
		Engine: eng, Runtime: runtimePort{runtime: runtime},
		Plugins: pluginPort{manager: plugins}, Skills: skillPort{registry: skills},
		Sessions: sessionPort{manager: sessions}, Events: events, Approval: approval,
	})
}

func initTUI(app *application.Service) tui.Model { return tui.NewModel(app) }

func startTUI(model tui.Model) {
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		fatalf("TUI 错误: %v", err)
	}
}

func startFrontend(app *application.Service) {
	switch *frontendMode {
	case "gui":
		if err := gui.Run(app, gui.Options{Title: "Seelex", Version: Version}); err != nil {
			fatalf("GUI 错误: %v", err)
		}
	default:
		startTUI(initTUI(app))
	}
}

func parseFrontendMode(value string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "tui", "gui":
		return mode, nil
	default:
		return "", fmt.Errorf("%q，允许值为 tui 或 gui", value)
	}
}

// setupPermissionGate 根据 -permission 标志安装权限门控。
// manual：白名单内自动放行，白名单外弹审批框（默认）。
// full_access：所有工具直接放行，仅在用户显式选择时启用。
func setupPermissionGate(runtime *seelebridge.Runtime, approval *application.ApprovalBroker) error {
	mode, err := parsePermissionMode(*permissionMode)
	if err != nil {
		return err
	}
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
	case permission.ModeFullAccess:
		cfg := permission.PermissionConfig{Mode: permission.ModeFullAccess}
		runtime.SetPermissionConfig(cfg, nil)
	}
	return nil
}

func parsePermissionMode(value string) (permission.Mode, error) {
	mode := permission.Mode(strings.ToLower(strings.TrimSpace(value)))
	switch mode {
	case permission.ModeManual, permission.ModeFullAccess:
		return mode, nil
	default:
		return "", fmt.Errorf("%q，允许值为 manual 或 full_access", value)
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

// resolveStorePath 确保多实例不冲突：检测 .lock 文件中的 PID，
// 若该 PID 还活着则自动递增路径后缀（sessions → sessions_1 → sessions_2…）。
func resolveStorePath(basePath string) string {
	for i := 0; i < 100; i++ {
		path := basePath
		if i > 0 {
			path = basePath + "_" + strconv.Itoa(i)
		}
		lockFile := filepath.Join(path, ".lock")
		if tryAcquireLock(lockFile) {
			return path
		}
	}
	// 理论上不会到这里（100 个实例够多了）
	path := basePath + "_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	os.MkdirAll(path, 0755)
	return path
}

// tryAcquireLock 尝试创建锁文件并写入当前 PID。返回 true 表示获取成功。
// 如果锁文件已存在但持有进程已死（stale lock），则覆盖。
func tryAcquireLock(lockFile string) bool {
	// 检查已有锁
	if data, err := os.ReadFile(lockFile); err == nil {
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if parseErr == nil && pid > 0 && processExists(pid) {
			return false // 锁被活着的进程持有
		}
		// Stale lock — 清理
		os.Remove(lockFile)
	}
	// 创建目录 + 锁文件
	if err := os.MkdirAll(filepath.Dir(lockFile), 0755); err != nil {
		return false
	}
	return os.WriteFile(lockFile, []byte(strconv.Itoa(os.Getpid())), 0644) == nil
}

// processExists 检查指定 PID 的进程是否存在（平台实现见 main_unix.go / main_windows.go）。

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "✖ "+format+"\n", args...)
	os.Exit(1)
}
