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

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	tea "github.com/charmbracelet/bubbletea"
	"gopkg.in/yaml.v3"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/core"
	"github.com/RedHuang-0622/seelex/application/search"
	"github.com/RedHuang-0622/seelex/gui"
	"github.com/RedHuang-0622/seelex/plugin"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/seelexctx"
	"github.com/RedHuang-0622/seelex/seelexctx/provider"
	seelexctxsnapshot "github.com/RedHuang-0622/seelex/seelexctx/snapshot"
	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/sessionstore"
	"github.com/RedHuang-0622/seelex/skill"
	"github.com/RedHuang-0622/seelex/tui"
	"github.com/RedHuang-0622/seelex/workspace"
)

var (
	storePath      = flag.String("store", ".seelex/sessions", "持久化存储路径")
	pluginsPaths   = flag.String("plugins", "plugins", "Plugin 加载路径（逗号分隔）")
	permissionMode = flag.String("permission", "manual", "权限模式: manual(白名单外需审批) | full_access(全部放行)")
	frontendMode   = flag.String("frontend", DefaultFrontend, "前端模式: tui | gui")
	showVersion    = flag.Bool("version", false, "显示版本号并退出")
	runtimeLimits  seelexctx.Limits // initRuntime 加载的 limits（后续初始化消费）
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
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "✖ %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()
	if *showVersion {
		fmt.Println(Version)
		return nil
	}
	frontend, err := parseFrontendMode(*frontendMode)
	if err != nil {
		return fmt.Errorf("前端模式无效: %w", err)
	}
	*frontendMode = frontend
	if frontend == "gui" && !gui.Available() {
		return fmt.Errorf(`当前二进制未包含 GUI；请使用 go run -tags "gui,desktop,production" . -frontend gui`)
	}
	mode, err := parsePermissionMode(*permissionMode)
	if err != nil {
		return fmt.Errorf("权限模式无效: %w", err)
	}
	*permissionMode = string(mode)
	*storePath = resolveStorePath(*storePath)

	runtime, err := initRuntime()
	if err != nil {
		return err
	}
	defer runtime.Shutdown()

	runtime.RegisterBuiltins()
	skillRegistry := initSkillSystem()
	pluginManager, err := initPluginSystem(runtime, skillRegistry)
	if err != nil {
		return err
	}
	store, err := initStore()
	if err != nil {
		return err
	}
	defer store.Close()
	events := application.NewEventHub()
	approval := application.NewApprovalBroker(events)
	runtime.SetPlanApprovalGate(&planApprovalGate{broker: approval})
	// 双轨事件（slice 8）：执行事实 → sessionstore 事件库（事实轨），
	// EventHub 继续前端快照（快照轨）。Sink 失败经 ErrorHandler 隔离，
	// 不破坏 WorkPlan 控制流（见 Seele event/README.md）。
	eventStore := sessionstore.NewEventStore(store)
	runtime.SetEventPersister(eventStore.Append)
	if err := setupPermissionGate(runtime, approval); err != nil {
		return fmt.Errorf("权限模式无效: %w", err)
	}
	toolHooks := application.NewToolHookBridge()
	frameworkEngine, err := initEngine(runtime, toolHooks)
	if err != nil {
		return err
	}
	registerProductTools(runtime, pluginManager, frameworkEngine, approval)
	if err := activateDefaultPlugin(pluginManager, frameworkEngine); err != nil {
		return err
	}
	appEngine := newEnginePort(frameworkEngine, func() reactorEngine {
		fresh, createErr := initEngine(runtime, toolHooks)
		if createErr != nil {
			return nil
		}
		return fresh
	}, runtime.Tracer())
	sessionManager := initSessionManager(store, appEngine)
	wsRepo, err := initWorkspaceRepo()
	if err != nil {
		return err
	}
	app, err := initApplication(appEngine, runtime, pluginManager, sessionManager, skillRegistry, wsRepo, events, approval)
	if err != nil {
		return err
	}
	defer app.Shutdown()
	registerTaskTerminalTools(runtime, app)
	registerContextReadTools(runtime, app, sessionManager)
	registerProjectRefreshTool(runtime, store)
	// 项目级模块语义提供者：Assembler 会话前预读 project 块（内容 hash
	// 版本化复用；重建失败保留上一版本）。
	runtime.SetProjectKnowledgeProvider(func() *sessionstore.ProjectRecord {
		record, readErr := store.LoadProjectRecord(store.Workspace())
		if readErr != nil {
			return nil
		}
		return &record
	})
	toolHooks.Bind(app)
	runtime.SetPlanNodeCallback(app.HandlePlanNodeComplete)
	// 子代理上下文闭环（Actor 消息边界，seelebridge/actor.go）：
	// - 消息进（ParentEvidence）：plan_run 期间主会话被 ChatStream 全程持锁，
	//   父证据必须从 application 镜像（service.mu）+ 遥测 trace 构造，
	//   绝不访问主会话（死锁教训见 actor.go 注释）；
	// - 消息出（MergeBack）：merge-back 结果投递 mailbox，下次 ChatStream
	//   开始前注入。
	runtime.SetContextExchanger(&contextExchanger{app: app, tracer: runtime.Tracer()})
	// 子代理 skill 能力（与主代理一致读取 skill 目录）：装配 skill 目录
	// actor（Registry 自带锁，读写经其方法进出；nodeSkillBlocks 消费）。
	runtime.SetSkillRegistry(skillRegistry)
	return startFrontend(app)
}

// contextExchanger 是父子 actor 上下文消息通道实现（Actor 消息边界）：
// 状态私有、消息进出。ParentEvidence 从 application 镜像构造新快照值对象；
// MergeBack 无锁投递 mailbox（application 排队，ChatStream 外注入）。
type contextExchanger struct {
	app    *application.Service
	tracer provider.TraceSource
}

func (ex *contextExchanger) ParentEvidence() *seelexctxsnapshot.ContextSnapshot {
	snap := ex.app.Snapshot()
	goal := ""
	for index := len(snap.Conversation) - 1; index >= 0; index-- {
		message := snap.Conversation[index]
		if message.Role == "user" && strings.TrimSpace(message.Content) != "" {
			goal = truncateSnapshotGoal(message.Content)
			break
		}
	}
	return seelexctx.ExportSnapshotFromData(snap.Session.ID, goal, len(snap.Conversation), ex.tracer)
}

func (ex *contextExchanger) MergeBack(content string) {
	ex.app.AppendSubagentContext(content)
}

// truncateSnapshotGoal 截断父证据目标（与 snapshot.Truncate 同语义的本地
// 实现，避免引入额外依赖）。按 rune 截断：字节截断会在多字节 UTF-8
// （中文）第 200 字节处切断，产生无效 UTF-8 后缀导致快照渲染乱码。
func truncateSnapshotGoal(content string) string {
	const maxGoalRunes = 200
	runes := []rune(content)
	if len(runes) <= maxGoalRunes {
		return content
	}
	return string(runes[:maxGoalRunes]) + "…"
}

func registerContextReadTools(runtime *seelebridge.Runtime, app *application.Service, sessionManager *session.Manager) {
	readResultSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"result_ref": map[string]interface{}{"type": "string"},
			"offset":     map[string]interface{}{"type": "integer", "minimum": 0},
			"limit":      map[string]interface{}{"type": "integer", "minimum": 1, "maximum": core.Limits().MaxReferencePageSize},
			"contains":   map[string]interface{}{"type": "string"},
		},
		"required": []string{"result_ref"},
	}
	readPlanSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"plan_ref": map[string]interface{}{"type": "string"},
			"node_ids": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
		},
	}
	runtime.RegisterTool("read_tool_result", "Read an immutable stored tool result by reference with bounded pagination or line filtering.", readResultSchema, app.ReadToolResultHandler)
	runtime.RegisterTool("read_plan", "Read selected nodes from the durable canonical Plan without changing Plan state.", readPlanSchema, app.ReadPlanHandler)
	readCompressedTurnSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"segment_id": map[string]interface{}{"type": "string"},
			"offset":     map[string]interface{}{"type": "integer", "minimum": 0},
			"limit":      map[string]interface{}{"type": "integer", "minimum": 1, "maximum": core.Limits().MaxReferencePageSize},
			"contains":   map[string]interface{}{"type": "string"},
		},
		"required": []string{"segment_id"},
	}
	runtime.RegisterTool("read_compressed_turn", "Read the original messages of a compressed turn segment by segment_id (from a compact frame Summary/Evidence) with bounded pagination or line filtering. Use it when the compressed summary lacks detail — the original content is durably stored and loss is reversible.", readCompressedTurnSchema, app.ReadCompressedTurnHandler)
	// 压缩轮次原文归档装配：溢出轮次原文经 session 管理器持久化
	// （ref = compressed:<segment_id>），read_compressed_turn 读回。
	runtime.SetTurnArchiver(&core.CompressedTurnArchiver{
		Sessions:          sessionManager,
		SessionIDProvider: func() string { return app.Snapshot().Session.ID },
	})
}

// registerProjectRefreshTool 注册 project_refresh 产品工具：扫描模块文档目录 +
// 模块元数据（module_dotting.json）+ 可选手工说明 seelex.project.md，构建
// 项目级模块语义知识（ProjectKnowledge，plan.md §3.7.1）。来源 hash 未变时
// 直接复用（内容版本化）；重建失败保留上一版本（可回退）。
func registerProjectRefreshTool(runtime *seelebridge.Runtime, store *sessionstore.Router) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"project_root": map[string]interface{}{"type": "string", "description": "项目根目录（默认当前工作目录）"},
			"force":        map[string]interface{}{"type": "boolean", "description": "强制重建，忽略来源 hash 复用"},
		},
	}
	handler := func(ctx context.Context, argsJSON string) (string, error) {
		var input struct {
			ProjectRoot string `json:"project_root"`
			Force       bool   `json:"force"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
			return "", fmt.Errorf("project_refresh: %w", err)
		}
		builder := sessionstore.NewProjectKnowledgeBuilder(sessionstore.ProjectKnowledgeSources{Root: input.ProjectRoot})
		result, err := sessionstore.RefreshProjectKnowledge(ctx, store, builder, input.Force)
		if err != nil {
			return "", fmt.Errorf("project_refresh: %w", err)
		}
		payload := map[string]interface{}{
			"version":  result.Record.Version,
			"modules":  len(result.Record.Modules),
			"reused":   result.Reused,
			"fallback": result.Fallback,
			"note":     result.Note,
			"built_at": result.Record.BuiltAt.Format(time.RFC3339),
		}
		encoded, err := json.Marshal(payload)
		return string(encoded), err
	}
	runtime.RegisterTool("project_refresh", "扫描项目模块文档与元数据，重建项目级模块语义知识；来源未变化时直接复用", schema, handler)
}

// registerTaskTerminalTools 把 task_complete/task_failed/task_needs_user_decision
// 注册进 tools.Registry（taskTerminalProvider，见 seelebridge/task_terminal.go）；
// handler 内调用 TaskService.VerifyAndApply（投影 flush + 终态校验）。
func registerTaskTerminalTools(runtime *seelebridge.Runtime, app *application.Service) {
	runtime.RegisterTaskTerminalTools(app.TaskTerminalHandler)
}

func initRuntime() (*seelebridge.Runtime, error) {
	// 运行参数在 seelex.yaml（配置参数文件；权限在 seele.yaml）：
	// 滑动窗口段缺失 → 零值走默认；limits 缺失字段 → 默认值。
	windowConfig, err := core.LoadWindowConfig("seelex.yaml")
	if err != nil {
		return nil, fmt.Errorf("加载 window 配置失败: %w", err)
	}
	limits, err := seelexctx.LoadLimits("seelex.yaml")
	if err != nil {
		return nil, fmt.Errorf("加载 limits 配置失败: %w", err)
	}
	limits = limits.WithDefaults()
	runtimeLimits = limits // initStore/initEngine 等后续初始化消费
	toolCallTimeout, _, planDecision, heartbeat, replanWindow, tavily := limits.Durations()
	core.ApplyLimits(limits)
	search.ApplyLimits(int(tavily / time.Second))
	runtime, err := seelebridge.NewRuntime(seelebridge.RuntimeConfig{
		AccountsPath: accountsPath(), StorePath: *storePath,
		ToolCallTimeout:           toolCallTimeout,
		PlanDecisionTimeout:       planDecision,
		HeartbeatInterval:         heartbeat,
		ReplanWindow:              replanWindow,
		MaxConcurrentReplans:      limits.MaxConcurrentReplans,
		MaxReplansPerWindow:       limits.MaxReplansPerWindow,
		MaxReplanProviderRequests: limits.MaxReplanProviderReqs,
		WindowConfig:              windowConfig,
		Limits:                    limits,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 Seele Runtime 失败: %w", err)
	}
	return runtime, nil
}

func initSkillSystem() *skill.Registry {
	// skills are now per-plugin (plugins/<name>/<skill>/SKILL.md).
	// The registry is populated via PublishPluginSkills on plugin Load/Activate.
	return skill.NewRegistry()
}

func initPluginSystem(
	runtime *seelebridge.Runtime,
	skills *skill.Registry,
) (*plugin.Manager, error) {
	loader := plugin.NewLoader(splitPaths(*pluginsPaths)...)
	manager := plugin.NewManager(loader, runtime, runtime, skills)
	if err := manager.Load(); err != nil {
		return nil, fmt.Errorf("加载 Plugin 失败: %w", err)
	}
	return manager, nil
}

func activateDefaultPlugin(manager *plugin.Manager, eng *frameworkSession.Session) error {
	if _, err := pluginByName(manager.All(), "default"); err != nil {
		return nil
	}
	if err := manager.Activate(context.Background(), "default"); err != nil {
		return fmt.Errorf("激活 default Plugin 失败: %w", err)
	}
	// 系统提示词由 application.Service.buildSystemPrompt 在 initApplication 时组装，
	// 不要在启动时直接覆盖 session 的 system prompt。
	// applyPluginPrompt(eng, manager)
	return nil
}

func registerProductTools(runtime *seelebridge.Runtime, plugins *plugin.Manager, eng *frameworkSession.Session, approval *application.ApprovalBroker) {
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
	eng *frameworkSession.Session,
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

func applyPluginPrompt(eng *frameworkSession.Session, plugins *plugin.Manager) {
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

func initStore() (*sessionstore.Router, error) {
	// NestedSessionStore 的 baseDir 与 workspace_index.json 同级
	baseDir := filepath.Dir(*storePath)
	sessionstore.ApplyLimits(runtimeLimits.SummaryChars)
	router, err := sessionstore.NewRouter(filepath.Join(baseDir, "session-storage.json"), baseDir)
	if err != nil {
		return nil, fmt.Errorf("初始化嵌套存储失败: %w", err)
	}
	return router, nil
}

func initWorkspaceRepo() (*workspace.Repo, error) {
	baseDir := filepath.Dir(*storePath)
	repo, err := workspace.NewRepoWithStore(baseDir)
	if err != nil {
		return nil, fmt.Errorf("初始化工作区存储失败: %w", err)
	}
	return repo, nil
}

// initEngine 按新装配模型创建主会话（session.NewSession）。
// enginePort 的 reactorEngine 接口由 *session.Session 直接满足。
func initEngine(runtime *seelebridge.Runtime, hooks *application.ToolHookBridge) (*frameworkSession.Session, error) {
	sess, err := runtime.NewMainSession(hooks.Hooks())
	if err != nil {
		return nil, fmt.Errorf("初始化主会话失败: %w", err)
	}
	return sess, nil
}

func initSessionManager(router *sessionstore.Router, eng *enginePort) *session.Manager {
	manager := session.NewManager(router)
	manager.WithRouter(router)
	manager.InjectSaveLoad(
		func(sessionID string) error { return router.Save(sessionID, eng.rawHistory()) },
		func(sessionID string) error {
			history, err := router.Load(sessionID)
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
	workspaces *workspace.Repo,
	events *application.EventHub, approval *application.ApprovalBroker,
) (*application.Service, error) {
	return application.New(application.Dependencies{
		Engine: eng, Runtime: runtimePort{runtime: runtime},
		Plugins: pluginPort{manager: plugins}, Skills: skillPort{registry: skills},
		Sessions: sessionPort{manager: sessions}, Workspace: workspacePort{repo: workspaces},
		Events: events, Approval: approval,
	})
}

func initTUI(app *application.Service) tui.Model { return tui.NewModel(app) }

func startTUI(model tui.Model) error {
	program := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("TUI 错误: %w", err)
	}
	return nil
}

func startFrontend(app *application.Service) error {
	switch *frontendMode {
	case "gui":
		if err := gui.Run(app, gui.Options{Title: "Seelex", Version: Version}); err != nil {
			return fmt.Errorf("GUI 错误: %w", err)
		}
		return nil
	default:
		return startTUI(initTUI(app))
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
	case toolspermission.ModeManual:
		cfg := toolspermission.PermissionConfig{Mode: toolspermission.ModeManual, Rules: defaultManualRules()}
		// seele.yaml 的 permission 段（权限专用文件）：存在有效规则时覆盖
		// 内置白名单；缺失/为空回退默认白名单。
		if fileRules, loadErr := loadPermissionRules("seele.yaml"); loadErr != nil {
			return loadErr
		} else if len(fileRules) > 0 {
			cfg.Rules = fileRules
		}
		runtime.SetPermissionConfig(cfg, newPermissionBridge(approval))
	case toolspermission.ModeFullAccess:
		cfg := toolspermission.PermissionConfig{Mode: toolspermission.ModeFullAccess}
		runtime.SetPermissionConfig(cfg, nil)
	}
	return nil
}

// defaultManualRules 是 manual 模式的默认白名单（seele.yaml 未配置规则时回退）。
func defaultManualRules() []toolspermission.PermissionRule {
	return []toolspermission.PermissionRule{
		{ToolName: "grep_search", Action: toolspermission.ActionAllow},
		{ToolName: "read_file", Action: toolspermission.ActionAllow},
		{ToolName: "glob", Action: toolspermission.ActionAllow},
		{ToolName: "git_status", Action: toolspermission.ActionAllow},
		{ToolName: "git_log", Action: toolspermission.ActionAllow},
		{ToolName: "git_diff", Action: toolspermission.ActionAllow},
		{ToolName: "get_time", Action: toolspermission.ActionAllow},
		{ToolName: "switch_plugin", Action: toolspermission.ActionAllow},
		{ToolName: "switch_mode", Action: toolspermission.ActionAllow},
		{ToolName: "ask_approve", Action: toolspermission.ActionAllow},
		{ToolName: "plan_load", Action: toolspermission.ActionAllow},
		{ToolName: "plan_run", Action: toolspermission.ActionAllow},
		{ToolName: "plan_status", Action: toolspermission.ActionAllow},
		{ToolName: "plan_validate", Action: toolspermission.ActionAllow},
		{ToolName: "plan_export", Action: toolspermission.ActionAllow},
		{ToolName: "plan_clear", Action: toolspermission.ActionAllow},
	}
}

// loadPermissionRules 读取 seele.yaml 的 permission.rules（权限专用文件）。
// 文件缺失或 permission 段缺失 → 空规则列表（回退默认白名单）；解析失败显式报错。
func loadPermissionRules(path string) ([]toolspermission.PermissionRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []toolspermission.PermissionRule{}, nil
		}
		return nil, fmt.Errorf("permission: read config: %w", err)
	}
	var file struct {
		Permission struct {
			Rules []toolspermission.PermissionRule `yaml:"rules"`
		} `yaml:"permission"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("permission: parse config: %w", err)
	}
	return file.Permission.Rules, nil
}

func parsePermissionMode(value string) (toolspermission.Mode, error) {
	mode := toolspermission.Mode(strings.ToLower(strings.TrimSpace(value)))
	switch mode {
	case toolspermission.ModeManual, toolspermission.ModeFullAccess:
		return mode, nil
	default:
		return "", fmt.Errorf("%q，允许值为 manual 或 full_access", value)
	}
}

// newPermissionBridge 创建连接 permission.ApprovalHandler → ApprovalBroker 的桥接器。
// 每次工具触发审批时，阻塞等待用户在 TUI 交互面板中作出选择。
func newPermissionBridge(broker *application.ApprovalBroker) toolspermission.ApprovalHandler {
	return func(ctx *toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
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
		return &toolspermission.ApprovalResponse{
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
