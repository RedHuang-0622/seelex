// Seelex assembles the Seele agent framework with product-level plugins,
// skills, session storage, and the terminal UI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	tea "github.com/charmbracelet/bubbletea"
	"gopkg.in/yaml.v3"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/adapters"
	"github.com/RedHuang-0622/seelex/application/console"
	"github.com/RedHuang-0622/seelex/application/core"
	"github.com/RedHuang-0622/seelex/application/search"
	"github.com/RedHuang-0622/seelex/gui"
	"github.com/RedHuang-0622/seelex/internal/buildinfo"
	mcpconfig "github.com/RedHuang-0622/seelex/mcpstack/config"
	"github.com/RedHuang-0622/seelex/plugin"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/seelebridge/tools/websearch"
	"github.com/RedHuang-0622/seelex/seelexctx"
	seelexctxsearch "github.com/RedHuang-0622/seelex/seelexctx/search"
	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/sessionstore"
	"github.com/RedHuang-0622/seelex/skill"
	"github.com/RedHuang-0622/seelex/tui"
	"github.com/RedHuang-0622/seelex/workspace"
)

var (
	// Version / DefaultFrontend 是构建期注入点：默认值来自 internal/buildinfo，
	// 发布构建通过 ldflags "-X main.Version=<tag>" / "-X main.DefaultFrontend=gui" 覆盖。
	Version         = buildinfo.Version
	DefaultFrontend = buildinfo.DefaultFrontend

	storePath      = flag.String("store", ".seelex/sessions", "持久化存储路径")
	pluginsPaths   = flag.String("plugins", "plugins", "Plugin 加载路径（逗号分隔）")
	permissionMode = flag.String("permission", "manual", "权限模式: manual(白名单外需审批) | full_access(全部放行)")
	frontendMode   = flag.String("frontend", DefaultFrontend, "前端模式: tui | gui | backend")
	backendPrompt  = flag.String("backend-prompt", "", "后端诊断请求（仅 -frontend backend；为空时从标准输入逐行读取）")
	backendTimeout = flag.Duration("backend-timeout", 2*time.Minute, "后端单次诊断请求的最大等待时间")
	backendLogPath = flag.String("backend-log", "", "后端诊断日志文件（仅 -frontend backend；仍同步输出到标准输出）")
	backendProject = flag.String("backend-project", "", "后端诊断绑定的项目根目录（仅 -frontend backend；显式提供才会绑定）")
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

// firstExisting 返回第一个存在的路径（配置兼容：优先 config/，回退根目录）。
func firstExisting(paths ...string) string {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if len(paths) > 0 {
		return paths[0]
	}
	return ""
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
	var backendOutput io.Writer
	var backendTrace *console.EventLogger
	if frontend == "backend" {
		output, closeOutput, outputErr := console.OpenOutput(*backendLogPath)
		if outputErr != nil {
			return outputErr
		}
		defer func() { _ = closeOutput() }()
		backendOutput = output
		backendTrace = console.NewEventLogger(output, time.Now)
		backendTrace.LogStage("startup.flags.parsed")
	}
	if frontend == "gui" && !gui.Available() {
		return fmt.Errorf(`当前二进制未包含 GUI；请使用 go run -tags "gui,desktop,production" . -frontend gui`)
	}
	mode, err := parsePermissionMode(*permissionMode)
	if err != nil {
		return fmt.Errorf("权限模式无效: %w", err)
	}
	*permissionMode = string(mode)
	*storePath = resolveStorePath(*storePath)

	console.LogStageIf(backendTrace, "startup.runtime.begin")
	runtime, err := initRuntime()
	if err != nil {
		return err
	}
	defer runtime.Shutdown()
	if backendTrace != nil {
		runtime.SetBashDiagnosticObserver(backendTrace.LogBashEvent)
	}
	console.LogStageIf(backendTrace, "startup.runtime.ready")

	runtime.RegisterBuiltins()
	console.LogStageIf(backendTrace, "startup.builtins.ready")
	skillRegistry := initSkillSystem()
	console.LogStageIf(backendTrace, "startup.skills.ready")
	pluginManager, err := initPluginSystem(runtime, skillRegistry)
	if err != nil {
		return err
	}
	console.LogStageIf(backendTrace, "startup.plugins.ready")
	store, err := initStore()
	if err != nil {
		return err
	}
	defer store.Close()
	console.LogStageIf(backendTrace, "startup.store.ready")
	runtime.AttachHistoryRouter(store)
	events := application.NewEventHub()
	approval := application.NewApprovalBroker(events)
	runtime.SetPlanApprovalGate(&adapters.PlanApprovalGate{Broker: approval})
	// 双轨事件（slice 8）：执行事实 → sessionstore 事件库（事实轨），
	// EventHub 继续前端快照（快照轨）。Sink 失败经 ErrorHandler 隔离，
	// 不破坏 WorkPlan 控制流（见 Seele event/README.md）。
	eventStore := sessionstore.NewEventStore(store)
	runtime.SetEventPersister(eventStore.Append)
	if err := setupPermissionGate(runtime, approval); err != nil {
		return fmt.Errorf("权限模式无效: %w", err)
	}
	console.LogStageIf(backendTrace, "startup.permissions.ready")
	toolHooks := application.NewToolHookBridge()
	if backendTrace != nil {
		toolHooks.SetDiagnosticObserver(backendTrace.LogToolHookEvent)
	}
	appEngine := adapters.NewEnginePort(nil, func(sessionID string) adapters.ReactorEngine {
		fresh, createErr := initEngine(runtime, toolHooks, sessionID)
		if createErr != nil {
			return nil
		}
		return fresh
	}, runtime.Tracer())
	appEngine.EnableWorkingHistoryRelease()
	appEngine.SetHistoryPreparer(func(sessionID string, messages []seelebridge.Message) {
		runtime.PrepareMainSessionHistory(sessionID, messages)
	})
	// The framework Session is intentionally created only by StartSession or
	// ReplaceHistory during resume; startup itself remains a cold draft.
	registerProductTools(runtime, pluginManager, appEngine, approval)
	if err := activateDefaultPlugin(pluginManager, nil); err != nil {
		return err
	}
	console.LogStageIf(backendTrace, "startup.engine.lazy-ready")
	// 子代理详情数据面：节点会话记录 + 结构化上下文查询 + 工具结果读回
	// （只读子代理 actor，安全——运行中实时导出、结束后快照）。
	appEngine.SetNodeConversationsProvider(runtime.NodeSessionConversation)
	appEngine.SetNodeContextProvider(runtime.NodeContextSnapshot)
	appEngine.SetNodeToolResultProvider(runtime.NodeToolResult)
	appEngine.SetNodeWorktreeProvider(runtime.NodeWorktreeInfoFor)
	// 子代理树投影（fork 内存态，GUI 树视图数据源；权威 Snapshot 增量携带）。
	appEngine.SetSubAgentTreeProvider(runtime.SubAgentTree)
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
	console.LogStageIf(backendTrace, "startup.application.ready")
	registerTaskTerminalTools(runtime, app)
	registerContextReadTools(runtime, app, sessionManager)
	registerProjectRefreshTool(runtime, store)
	registerScheduledTaskCapability(runtime, app)
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
	runtime.SetSubagentToolCallback(app.HandleSubagentToolEvent)
	// plan 节点事件 / 子代理树生命周期 / task 变更均由 application 内的
	// CSP 消费者经 channel 处理（service_assembler 启动），无需模型调用
	// 任何工具，worktable/task 增量自动发布（被动技能）。
	// Application 在状态迁移后向 Runtime 发布不可变可见性和父证据投影；
	// Runtime 只读自己的缓存，子代理 merge-back 写 Runtime 有界 mailbox，
	// 由主会话在下一次 ChatStream 前锁外消费。
	app.PublishRuntimeProjections()
	// 子代理 skill 能力（与主代理一致读取 skill 目录）：装配 skill 目录
	// actor（Registry 自带锁，读写经其方法进出；nodeSkillBlocks 消费）。
	runtime.SetSkillRegistry(skillRegistry)
	if frontend == "backend" && strings.TrimSpace(*backendProject) != "" {
		if err := console.BindProject(app, *backendProject); err != nil {
			return err
		}
		console.LogStageIf(backendTrace, "startup.workspace.ready")
	}
	console.LogStageIf(backendTrace, "startup.frontend.ready")
	return startFrontend(app, backendOutput)
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
	// search_history：压缩栈帧是语义索引，检索在其范围内查真实聊天记录
	// （seelexctx/search：memory.Select 选相关帧 → 帧 [From..To] 单元范围
	// 从事件库读回记录 → token 预算内内联返回）。
	searchHistorySchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "检索关键词（与压缩段摘要词法匹配，可中英文）"},
			"limit": map[string]interface{}{"type": "integer", "minimum": 1, "maximum": seelexctxsearch.MaxLimit},
		},
		"required": []string{"query"},
	}
	runtime.RegisterTool("search_history", "Search the session's long-term history: select relevant compressed segments (compact stack index) and read back the real chat records in their unit ranges, bounded by a token budget. Use it when the current context lacks relevant history the user mentioned earlier (past decisions, requirements, tool outputs).", searchHistorySchema, app.SearchHistoryHandler)
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

// registerScheduledTaskCapability 装配定时周期任务（seelebridge/scheduler.go）：
//   - 登记 auto_get_jobs 白名单命令（脚本目录或 python 不可用时跳过并告警）；
//   - 注入 prompt 任务执行器：复用当前主会话 Submit（带会话绑定校验，切换
//     后跳过而不是打到错误的会话）；
//   - 注入状态变化 observer：调度器开始/完成/失败 → 快照投影 →
//     runtime.changed 增量（GUI 定时任务面板实时更新）。
func registerScheduledTaskCapability(runtime *seelebridge.Runtime, app *application.Service) {
	if scriptDir, ok := resolveAutoGetJobsDir(); ok {
		if python := resolvePythonCommand(); python != "" {
			if err := runtime.RegisterScheduledCommand(seelebridge.ScheduledCommand{
				Key:         "auto_get_jobs",
				Label:       "BOSS直聘自动投简历",
				Description: "周期抓取招聘职位并自动筛选投递（local/tools/auto_get_jobs/main.py；需先按脚本 README 配置 .env 与 user_requirements.txt）",
				WorkingDir:  scriptDir,
				Argv:        []string{python, "main.py"},
				TimeoutSec:  30 * 60,
			}); err != nil {
				log.Printf("scheduled tasks: register auto_get_jobs: %v", err)
			}
		}
	}
	runtime.SetScheduledPromptExecutor(func(ctx context.Context, prompt, sessionID string) (string, error) {
		// 会话绑定：显式 sessionID 必须匹配当前主会话（切换后跳过，不误投递）；
		// 空 = 执行时当前 main session。
		current := app.Snapshot().Session.ID
		if sessionID != "" && sessionID != current {
			return "", fmt.Errorf("任务绑定会话 %s，当前会话 %s（已切换），本次跳过", sessionID, current)
		}
		if err := app.Submit(ctx, prompt); err != nil {
			return "", err
		}
		return "已提交到当前会话执行（异步输出见会话记录）", nil
	})
	runtime.SetSchedulerObserver(app.RefreshRuntimeSnapshot)
}

// resolveAutoGetJobsDir 定位 auto_get_jobs 脚本目录：优先相对当前工作目录
// （go run / 开发场景），回退二进制所在目录（正式部署）；目录内必须存在 main.py。
func resolveAutoGetJobsDir() (string, bool) {
	relative := filepath.Join("local", "tools", "auto_get_jobs")
	candidates := []string{relative}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), relative))
	}
	for _, dir := range candidates {
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if info, statErr := os.Stat(filepath.Join(abs, "main.py")); statErr == nil && !info.IsDir() {
			return abs, true
		}
	}
	log.Printf("scheduled tasks: auto_get_jobs 脚本目录未找到（期望 %s），跳过该白名单命令登记", relative)
	return "", false
}

// resolvePythonCommand 探测可用的 python 解释器（python → py；找不到时返回空）。
func resolvePythonCommand() string {
	for _, candidate := range []string{"python", "py"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	log.Printf("scheduled tasks: 未找到 python 解释器，跳过 auto_get_jobs 白名单命令登记")
	return ""
}

// registerTaskTerminalTools 把 task_complete/task_failed/task_needs_user_decision
// 注册进 tools.Registry（taskTerminalProvider，见 seelebridge/task_terminal.go）；
// handler 内调用 TaskService.VerifyAndApply（投影 flush + 终态校验）。
func registerTaskTerminalTools(runtime *seelebridge.Runtime, app *application.Service) {
	runtime.RegisterTaskTerminalTools(app.TaskTerminalHandler)
}

func initRuntime() (*seelebridge.Runtime, error) {
	// 运行参数在 config/seelex.yaml（配置参数文件；权限在 config/seele.yaml）：
	// 优先 config/，回退根目录（开发习惯兼容）；滑动窗口段缺失 → 零值走默认；limits 缺失字段 → 默认值。
	runtimeConfigPath := firstExisting("config/seelex.yaml", "seelex.yaml")
	windowConfig, err := core.LoadWindowConfig(runtimeConfigPath)
	if err != nil {
		return nil, fmt.Errorf("加载 window 配置失败: %w", err)
	}
	limits, err := seelexctx.LoadLimits(runtimeConfigPath)
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

type pluginPromptEngine interface {
	SetSystemPrompt(string)
}

func registerProductTools(runtime *seelebridge.Runtime, plugins *plugin.Manager, eng pluginPromptEngine, approval *application.ApprovalBroker) {
	registerTimeTool(runtime)
	websearch.Register(runtime, accountsPath())
	registerMCPServers(runtime, accountsPath()) // mcpstack/config 加载 + Runtime 冷启动登记
	registerMCPLoadTool(runtime)
	registerPluginSwitchTools(runtime, plugins, eng)
	registerAskApprove(runtime, approval)
}

// registerMCPServers 将账号池配置中配置的 MCP 服务器全部登记到 Runtime
// （冷启动：只存配置不连接，启动路径零 MCP 进程）。配置加载在 mcpstack/config。
// 首次需要时经内置 mcp_load 工具按名加载（spawn + initialize + tools/list），
// 加载后的 MCP 工具自动通过 mcpstack 中间件记录调用 trace。
func registerMCPServers(runtime *seelebridge.Runtime, accountsPath string) {
	servers := mcpconfig.Load(accountsPath)
	if len(servers) == 0 {
		return
	}

	for _, s := range servers {
		transport := s.Transport
		if transport == "" {
			if s.Command != "" {
				transport = "stdio"
			} else if s.URL != "" {
				transport = "sse"
			} else {
				fmt.Fprintf(os.Stderr, "⚠ MCP 服务器 %q：transport 未知（command 和 URL 均为空），跳过\n", s.Name)
				continue
			}
		}

		cfg := seelebridge.MCPServer{
			Name: s.Name, Transport: transport, Command: s.Command,
			Args: s.Args, Env: s.Env, URL: s.URL,
		}
		if err := runtime.RegisterLazyMCP(s.Name, cfg); err != nil {
			fmt.Fprintf(os.Stderr, "⚠ MCP 服务器 %q 配置无效: %v\n", s.Name, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "✓ MCP 服务器 %q 已登记（冷启动；需要时调用 mcp_load 连接）\n", s.Name)
	}
}

// registerMCPLoadTool 注册按需加载工具：连接已登记但未连接的 MCP 服务器
// （冷启动加载点），加载后其工具立即可用（下一轮调用）。
func registerMCPLoadTool(runtime *seelebridge.Runtime) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"server_name": map[string]interface{}{
				"type": "string", "description": "要加载的 MCP 服务器名（accounts.yaml mcp_servers 段）",
			},
		},
		"required": []string{"server_name"},
	}
	runtime.RegisterTool(
		"mcp_load",
		"Load a registered but disconnected MCP server (cold start): connect, initialize and register its tools. Call this once before using tools from that server; loaded servers stay connected for the session. Loaded tool names become available from the next turn.",
		schema,
		func(ctx context.Context, argsJSON string) (string, error) {
			var args struct {
				ServerName string `json:"server_name"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
				return "", fmt.Errorf("mcp_load: invalid arguments: %w", err)
			}
			// 冷启动握手有固定开销（spawn + initialize + tools/list），带超时保护。
			loadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			tools, err := runtime.LoadMCP(loadCtx, strings.TrimSpace(args.ServerName))
			if err != nil {
				return "", fmt.Errorf("mcp_load: %w", err)
			}
			return fmt.Sprintf("MCP 服务器 %q 已加载，工具 %d 个。可用 MCP 服务器：%v。请重新发起需要这些工具的任务。",
				args.ServerName, tools, runtime.MCPServerNames()), nil
		},
	)
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
	eng pluginPromptEngine,
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

func applyPluginPrompt(eng pluginPromptEngine, plugins *plugin.Manager) {
	if eng == nil {
		return
	}
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
				options[i] = adapters.ApprovalOption(choice)
			}
			decision, err := approval.Request(ctx, application.ApprovalRequest{
				ID: fmt.Sprintf("ask_%d", time.Now().UnixNano()), Question: input.Question,
				Options: options, Risk: "low", ToolName: "ask_approve",
			})
			if err != nil || !adapters.ApprovalAccepted(decision.OptionID) {
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
// EnginePort 的 ReactorEngine 接口由 *session.Session 直接满足。
func initEngine(runtime *seelebridge.Runtime, hooks *application.ToolHookBridge, sessionID string) (*frameworkSession.Session, error) {
	sess, err := runtime.NewMainSessionWithID(sessionID, hooks.Hooks())
	if err != nil {
		return nil, fmt.Errorf("初始化主会话失败: %w", err)
	}
	return sess, nil
}

func initSessionManager(router *sessionstore.Router, eng *adapters.EnginePort) *session.Manager {
	manager := session.NewManager(router)
	manager.WithRouter(router)
	manager.InjectSaveLoad(
		func(sessionID string) error { return router.Save(sessionID, eng.RawHistory()) },
		func(sessionID string) error {
			history, err := router.Load(sessionID)
			if err != nil {
				return err
			}
			return eng.ReplaceRawHistory(sessionID, history)
		},
	)
	return manager
}

func initApplication(
	eng *adapters.EnginePort, runtime *seelebridge.Runtime, plugins *plugin.Manager,
	sessions *session.Manager, skills *skill.Registry,
	workspaces *workspace.Repo,
	events *application.EventHub, approval *application.ApprovalBroker,
) (*application.Service, error) {
	return application.New(application.Dependencies{
		Engine: eng, Runtime: adapters.RuntimePort{Runtime: runtime},
		Plugins: adapters.PluginPort{Manager: plugins}, Skills: adapters.SkillPort{Registry: skills},
		Sessions: adapters.SessionPort{Manager: sessions, Runtime: runtime}, Workspace: adapters.WorkspacePort{Repo: workspaces},
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

func startFrontend(app *application.Service, backendOutput io.Writer) error {
	switch *frontendMode {
	case "gui":
		if err := gui.Run(app, gui.Options{Title: "Seelex", Version: Version}); err != nil {
			return fmt.Errorf("GUI 错误: %w", err)
		}
		return nil
	case "backend":
		return console.Start(app, *backendPrompt, *backendTimeout, backendOutput)
	default:
		return startTUI(initTUI(app))
	}
}

func parseFrontendMode(value string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(value))
	switch mode {
	case "tui", "gui", "backend":
		return mode, nil
	default:
		return "", fmt.Errorf("%q，允许值为 tui、gui 或 backend", value)
	}
}

type permissionRuntime interface {
	SetPermissionConfig(toolspermission.PermissionConfig, toolspermission.ApprovalHandler)
	SetFullAccess(bool)
}

// setupPermissionGate 根据 -permission 标志安装权限门控。
// 始终先安装 manual 基线，保证从 full_access 切回时能够恢复白名单与审批桥；
// full_access 仅作为运行时覆盖层启用。
func setupPermissionGate(runtime permissionRuntime, approval *application.ApprovalBroker) error {
	mode, err := parsePermissionMode(*permissionMode)
	if err != nil {
		return err
	}
	cfg := toolspermission.PermissionConfig{Mode: toolspermission.ModeManual, Rules: defaultManualRules()}
	// config/seele.yaml 的 permission 段（权限专用文件）：存在有效规则时覆盖
	// 内置白名单；缺失/为空回退默认白名单。
	if fileRules, loadErr := loadPermissionRules(firstExisting("config/seele.yaml", "seele.yaml")); loadErr != nil {
		return loadErr
	} else if len(fileRules) > 0 {
		cfg.Rules = fileRules
	}
	runtime.SetPermissionConfig(cfg, newPermissionBridge(approval))
	if mode == toolspermission.ModeFullAccess {
		runtime.SetFullAccess(true)
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
		{ToolName: "mcp_load", Action: toolspermission.ActionAllow},
		{ToolName: "switch_plugin", Action: toolspermission.ActionAllow},
		{ToolName: "switch_mode", Action: toolspermission.ActionAllow},
		{ToolName: "ask_approve", Action: toolspermission.ActionAllow},
		{ToolName: "todolist_init", Action: toolspermission.ActionAllow},
		{ToolName: "todolist_add", Action: toolspermission.ActionAllow},
		{ToolName: "todolist_done", Action: toolspermission.ActionAllow},
		{ToolName: "todolist_status", Action: toolspermission.ActionAllow},
		{ToolName: "task_complete", Action: toolspermission.ActionAllow},
		{ToolName: "task_failed", Action: toolspermission.ActionAllow},
		{ToolName: "task_needs_user_decision", Action: toolspermission.ActionAllow},
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
			ID:                req.ID,
			Question:          req.Preview,
			Options:           adapters.ConvertPermissionOptions(req.Options),
			Risk:              req.Risk,
			ToolName:          req.ToolName,
			Preview:           req.Preview,
			Timeout:           req.Timeout,
			PermissionRequest: true,
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
