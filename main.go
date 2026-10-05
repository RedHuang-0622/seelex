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

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/console"
	"github.com/RedHuang-0622/seelex/application/contract/dto"
	"github.com/RedHuang-0622/seelex/application/core"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	coretask "github.com/RedHuang-0622/seelex/application/core/task_context"
	"github.com/RedHuang-0622/seelex/gui"
	"github.com/RedHuang-0622/seelex/internal/adapters"
	"github.com/RedHuang-0622/seelex/internal/bootseed"
	"github.com/RedHuang-0622/seelex/internal/buildinfo"
	mcpconfig "github.com/RedHuang-0622/seelex/mcpstack/config"
	"github.com/RedHuang-0622/seelex/plugin"
	"github.com/RedHuang-0622/seelex/seelebridge"
	"github.com/RedHuang-0622/seelex/seelebridge/search"
	seeteamwork "github.com/RedHuang-0622/seelex/seelebridge/teamwork"
	seeltools "github.com/RedHuang-0622/seelex/seelebridge/tools"
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
	pluginsPaths   = flag.String("plugins", "", "Plugin 根目录（逗号分隔，可多根）；留空走责任链：$SEELEX_PLUGINS > <exe>/plugins > <exe>/../plugins > plugins(CWD)")
	permissionMode = flag.String("permission", "manual", "权限档位: manual(默认) | edit(自动改文件) | auto(自动执行) | full(全权，旧别名 full_access)")
	frontendMode   = flag.String("frontend", DefaultFrontend, "前端模式: tui | gui | headless | backend")
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

// runtimeConfigChain 返回配置文件在责任链上的候选（按优先级）与允许初始化的落盘根：
//
//  1. config/<name>        CWD 相对（仓库里那份 / 开发场景；用户改过的就是它）
//  2. <name>               根目录回退（历史兼容）
//  3. <exe>/config/<name>  包内配置（正式部署：二进制旁边自带一份）
//
// 落盘根**固定取 <exe>/config**：CWD 可能是用户的项目目录，不能在那里凭空造出
// config/ 来；二进制所在目录才是这个应用自己的地盘。落盘失败（只读安装目录等）
// 由调用方回退代码默认值，不阻断启动。
func runtimeConfigChain(name string) (candidates []string, seedRoot string) {
	candidates = []string{filepath.Join("config", name), name}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(exeDir, "config", name))
		seedRoot = filepath.Join(exeDir, "config")
	}
	return candidates, seedRoot
}

// pluginRootChain 返回 Plugin 根在责任链上的候选（按优先级、去重保序）：
//
//  1. -plugins <paths>      显式旗标（最高优先；逗号分隔可多根，先出现的根胜出）
//  2. $SEELEX_PLUGINS       环境变量（容器/CI/多份发行包并存时的注入点）
//  3. <exe>/plugins         交付树自带（发行包：二进制旁边就有一份 plugins/）
//  4. <exe>/../plugins      交付树上一级（stage 布局：exe 落在子目录里）
//  5. plugins               CWD 相对（仓库 / go run 开发场景）
//
// 语义是**顺序覆盖**而不是"择一"：加载器本身是多根 first-wins（plugin/loader.go
// 的 LoadAll：先出现的同名插件胜出、不存在的根静默跳过），所以这条链只决定"去哪里找"。
// 显式旗标排在最前，因此 `-plugins <开发目录>` 能覆盖发行包里那份同名插件；
// 而"到底哪个根真的供上了插件"由启动期的根报告（logPluginRoots）写明，不靠猜。
func pluginRootChain(flagValue, envValue, exePath string) []string {
	roots := make([]string, 0, 6)
	roots = append(roots, splitPaths(flagValue)...)
	roots = append(roots, splitPaths(envValue)...)
	if exePath != "" {
		exeDir := filepath.Dir(exePath)
		roots = append(roots, filepath.Join(exeDir, "plugins"), filepath.Join(exeDir, "..", "plugins"))
	}
	roots = append(roots, "plugins")
	return dedupRoots(roots)
}

// pluginRoots 用真实环境（旗标 / SEELEX_PLUGINS / 可执行文件位置）解析责任链。
func pluginRoots() []string {
	exe, err := os.Executable()
	if err != nil {
		exe = ""
	}
	return pluginRootChain(*pluginsPaths, os.Getenv("SEELEX_PLUGINS"), exe)
}

// dedupRoots 去空、去重（保序）。Windows 上路径大小写不敏感，同一份目录可能以两种
// 写法出现在链上，重复项会让启动期的根报告出现"两个 0 个插件"的噪声行。
func dedupRoots(roots []string) []string {
	seen := make(map[string]bool, len(roots))
	result := make([]string, 0, len(roots))
	for _, root := range roots {
		if root = strings.TrimSpace(root); root == "" {
			continue
		}
		cleaned := filepath.Clean(root)
		key := cleaned
		if os.PathSeparator == '\\' {
			key = strings.ToLower(cleaned)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, cleaned)
	}
	return result
}

// requirePlugins 把"零插件"从静默降级变成一次显式失败。
//
// 没有 Plugin 的启动不是"功能少一点"：default 插件不存在 ⇒ 无 skill 目录、`#` 切换
// 为空、模型拿不到任何领域纪律，而旧口径下日志里一个字都没有（loader 跳过不存在的根
// → 空表不报错 → 找不到 default 就 `return nil`）。这条错误把"试过哪些根"和"怎么救"
// 一起说出来，让故障在启动期可见，而不是以"模型什么都不会"的形式显现。
func requirePlugins(roots []string, loaded []plugin.Plugin) error {
	if len(loaded) > 0 {
		return nil
	}
	tried := "（责任链为空）"
	if len(roots) > 0 {
		tried = strings.Join(roots, ", ")
	}
	return fmt.Errorf(
		"零插件：责任链上 %d 个根都没有加载到任何 Plugin（试过: %s）。"+
			"用 -plugins <路径> 或 $SEELEX_PLUGINS 显式指定插件根；发行包应把 plugins/ 放在二进制旁边（<exe>/plugins）",
		len(roots), tried)
}

// pluginRootReport 生成"插件到底从哪来"的**一条读数**：责任链上每个根各供上了几个
// 插件，以及每个插件最终来自哪个根（多根 first-wins，同名插件只算链上先出现的那个）。
//
// 这条读数**两处共写**：终端日志（logPluginRoots）与 UI 面（run() 里进启动通知）。
// 旧口径只有前者，于是同一条事实在 GUI 里根本看不到——所以"广播"那个词名不副实；
// 现在它是"启动读数"，两处同源（改一处两处一起变，不会出现两份对不上的读数）。
func pluginRootReport(roots []string, loaded []plugin.Plugin) string {
	absLoaded := make([]string, 0, len(loaded))
	for _, p := range loaded {
		if abs, err := filepath.Abs(p.RootDir); err == nil {
			absLoaded = append(absLoaded, abs)
		}
	}
	parts := make([]string, 0, len(roots))
	for _, root := range roots {
		count := 0
		if abs, err := filepath.Abs(root); err == nil {
			prefix := abs + string(os.PathSeparator)
			for _, dir := range absLoaded {
				if dir == abs || strings.HasPrefix(dir, prefix) {
					count++
				}
			}
		}
		parts = append(parts, fmt.Sprintf("%s=%d", root, count))
	}
	origins := make([]string, 0, len(loaded))
	for _, p := range loaded {
		origins = append(origins, fmt.Sprintf("%s←%s", p.Name, p.RootDir))
	}
	report := fmt.Sprintf("根 %s（共加载 %d 个插件）", strings.Join(parts, ", "), len(loaded))
	if len(origins) > 0 {
		report += "；来源 " + strings.Join(origins, ", ")
	}
	return report
}

// logPluginRoots 把根读数写成启动期一行终端日志。**它不再是唯一出口**：同一条读数由
// run() 送进 UI 面（启动通知），见 pluginRootReport。返回这行读数，供调用方复用同一份
// 字节（两处各生成一次就会出现两份对不上的读数）。
//
// 这是"一处发现、处处可用"的可观察面——静默降级被这条读数替换掉（0 个插件的根会
// 明明白白显示为 0），排障不必再靠猜二进制旁边有没有 plugins/。
func logPluginRoots(roots []string, loaded []plugin.Plugin) string {
	report := pluginRootReport(roots, loaded)
	log.Printf("plugin: %s", report)
	return report
}

// resolveCuratedRead 从**已解析的插件根**（责任链，多根 first-wins）读一次精选目录。
//
// 语义三条：
//   - **first-wins**：链上第一个带 curated.yaml 的根说话（与加载器"同名插件先出现的
//     根胜出"同一姿势）；第一个存在的那份读不动就**当场报**，不悄悄退到下一个根——
//     退让会把"这份目录坏了"藏成"目录里没有这个名字"。
//   - **交叉校验用真实加载集合**：installed 取的是本次启动**真正加载出来**的插件名
//     （多根并集），不是某一个根的 LoadAll。
//   - **差异是回报、不是错误**：目录与这台机器已装集合不一致（本机自装插件、退役插件）
//     不阻断、不报警告，只留成 Drift 供日志回报——否则使用者"自己改下插件"就会永远
//     背着一条他改不动、也不该由他改的发行侧警告。
//   - **一个根都没有 ⇒ Absent**：自建根/用户树可以不带精选目录。Err 仍非 nil（装配面
//     要显式拒绝并说清"没有目录可比对"），但它不是配置缺陷、不进启动警告。
func resolveCuratedRead(roots []string, loaded []plugin.Plugin) plugin.CuratedRead {
	installed := make([]string, 0, len(loaded))
	for _, p := range loaded {
		installed = append(installed, p.Name)
	}
	for _, root := range roots {
		path := filepath.Join(root, plugin.CuratedFileName)
		if _, err := os.Stat(path); err != nil {
			continue
		}
		// 运行期读数：目录**自身**的问题才是错误；"与这台机器已装集合不一致"退成回报。
		catalog, drift, err := plugin.LoadCuratedForRuntime(root, installed)
		if err != nil {
			return plugin.CuratedRead{Roots: roots, Path: path, Err: err}
		}
		return plugin.CuratedRead{Catalog: catalog, Roots: roots, Path: path, Drift: drift}
	}
	return plugin.CuratedRead{Roots: roots, Absent: true, Err: fmt.Errorf(
		"责任链上 %d 个根都没有 %s（根解析见 pluginRootChain；用 -plugins 或 $SEELEX_PLUGINS 指定插件根）",
		len(roots), plugin.CuratedFileName)}
}

// curatedAssemblyJudge 把精选目录读数包成装配面的判决函数（"未定义的名字怎么判/怎么说"）。
//
// 判定的分支与文案全在 `plugin.CuratedRead.Judge`：那里同时有目录（entries / pending /
// 实读页）与"已装名单"，是唯一能一次说清三件事的地方；本函数只负责把**运行时的事实**
// （本进程已定义插件名）递进去，不复制判定。
func curatedAssemblyJudge(read plugin.CuratedRead) func(name string, installed []string) error {
	return func(name string, installed []string) error {
		return read.Judge(name, installed)
	}
}

// ensureConfigFile 按责任链给出配置文件路径（口径见 internal/bootseed）：
//
//	存在即读：候选链上第一份存在的文件就是答案——用户改过的、包内自带的、
//	          仓库里的，都优先于默认数据，本函数一个字节都不写。
//	缺失即初始化：候选链上全都没有 → 用内嵌默认档在 <exe>/config 里落盘，再读它。
//
// 返回"该读哪个路径"；确实没有可用文件时返回链上第一个候选（与老口径一致：让
// 宽容的加载器按"文件不存在"处置，走代码默认值）。
func ensureConfigFile(name string, pack bootseed.Pack) string {
	candidates, seedRoot := runtimeConfigChain(name)
	result, err := bootseed.Resolve(bootseed.Spec{
		Name:       name,
		Candidates: candidates,
		SeedRoot:   seedRoot,
		Entry:      name,
		Pack:       pack,
	})
	if err != nil {
		log.Printf("config: %s 初始化默认档失败（回退代码默认值）: %v", name, err)
		return candidates[0]
	}
	switch result.Kind {
	case bootseed.KindSeeded:
		log.Printf("config: %s 候选链上都没有，已用内嵌默认档初始化到 %s（存在即读：之后直接用这份）", name, result.Path)
	case bootseed.KindMissing:
		log.Printf("config: %s 候选链上都没有，且无处落盘（回退代码默认值）", name)
	}
	if result.Path != "" {
		return result.Path
	}
	return candidates[0]
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "✖ %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	flag.Parse()
	// pprof 构建钩子（-tags pprof）：Go 侧采样端口，默认 127.0.0.1:6060，
	// 与 GUI 前端性能钩子（window.__seelexPerf / PerfStats）配合做内存对照。
	startPprofHook()
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
	*permissionMode = mode
	*storePath = resolveStorePath(*storePath)

	console.LogStageIf(backendTrace, "startup.runtime.begin")
	runtime, err := initRuntime()
	if err != nil {
		return err
	}
	defer runtime.Shutdown()
	console.LogStageIf(backendTrace, "startup.runtime.ready")
	// 启动期多进程闸门（limits.runtime.allow_multi_process，默认单实例）：数据根
	// 是单进程写者，另一个实例在写同一数据根时在这里给出可读拒绝，而不是让
	// ErrDataRootLocked 从装配深处冒出来（`initStore` 真正抢锁时会再兜一次）。
	if err := guardMultiProcess(); err != nil {
		return err
	}

	runtime.RegisterBuiltins()
	console.LogStageIf(backendTrace, "startup.builtins.ready")
	skillRegistry := initSkillSystem()
	console.LogStageIf(backendTrace, "startup.skills.ready")
	pluginManager, pluginStartup, err := initPluginSystem(runtime, skillRegistry)
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
	// 子代理会话记录持久化（运行期落盘 + 结束时结论归主会话 + 记录删除）
	// 与 plan checkpoint 持久化（RunPlan 落最终快照，ResumePlan 续跑）。
	runtime.AttachSubSessionStore(sessionstore.NewNodeSessionStore(store))
	runtime.SetPlanCheckpointStore(sessionstore.NewCheckpointStore(store, store.Workspace()))
	events := application.NewEventHub()
	approval := application.NewApprovalBroker(events)
	// 双轨事件（slice 8）：执行事实 → sessionstore 事件库（事实轨），
	// EventHub 继续前端快照（快照轨）。Sink 失败经 ErrorHandler 隔离，
	// 不破坏 WorkPlan 控制流（见 Seele event/README.md）。
	eventStore := sessionstore.NewEventStore(store)
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
	// The framework Session is intentionally created only by StartSession or
	// ReplaceHistory during resume; startup itself remains a cold draft.
	registerProductTools(runtime, pluginManager, appEngine, approval, skillRegistry)
	if err := activateDefaultPlugin(pluginManager, nil); err != nil {
		return err
	}
	console.LogStageIf(backendTrace, "startup.engine.lazy-ready")
	// 启动期装配一次注入：子代理详情数据面（会话记录/上下文/工具结果/
	// worktree 现场，只读子代理 actor，安全）+ 子代理树投影 + 实时流订阅源
	// + 历史准备器（DurableHistory.Load handoff）。
	appEngine.ApplyDeps(adapters.EnginePortDeps{
		NodeConversations: runtime.NodeSessionConversation,
		NodeContext:       runtime.NodeContextSnapshot,
		NodeToolResult:    runtime.NodeToolResult,
		NodeWorktree:      runtime.NodeWorktreeInfoFor,
		SubAgentTree:      runtime.SubAgentTree,
		SubagentLive:      runtime.SubscribeSubagentLive,
		NodeStageLogs:     runtime.NodeStageLogs,
		PrepareHistory: func(sessionID string, messages []types.Message) {
			runtime.PrepareMainSessionHistory(sessionID, messages)
		},
	})
	sessionManager := initSessionManager(store, appEngine)
	wsRepo, err := initWorkspaceRepo()
	if err != nil {
		return err
	}
	// 执行事实事件库按会话绑定项目落盘（R3 键漂移修复：后台会话事件不再
	// 因 active write scope 复位而拆到默认项目）。
	eventStore.SetWorkspaceResolver(func(sessionID string) string {
		if workspace, ok := wsRepo.SessionWorkspace(sessionID); ok {
			return workspace.ID
		}
		return ""
	})
	// teamwork 编排面：leader 的六件套工具 + jobs_manage。计划/审计落 moduleTeamwork
	// （JSON 后端）；作用域键由"会话 → 所属项目"解析，与执行事实事件库同源。
	if repo, ok := store.TeamworkFor(); ok {
		// 看板存档面（会话粒度元数据 + 重启快照恢复）：与计划/审计同一个 JSON 后端，
		// 落 metadata/board_team.json。非 JSON 后端不实现它 → nil，读侧无从恢复，
		// 活体投影照常工作。
		boardRepo, _ := store.BoardsFor()
		keyFor := func(sessionID string) (sessionstore.Key, bool) {
			workspace, exists := wsRepo.SessionWorkspace(sessionID)
			if !exists || strings.TrimSpace(workspace.ID) == "" {
				return sessionstore.Key{}, false
			}
			return sessionstore.Key{ProjectID: workspace.ID, SessionID: sessionID}, true
		}
		if err := runtime.SetTeamworkBackend(seelebridge.TeamworkBackend{
			Store:  seeteamwork.NewPlanStore(repo),
			Boards: boardRepo,
			// 作业输出归产品（S5）：正文落会话的 teamwork/jobs，销项 / 驱逐 / Close
			// 都不由框架删，整队收口（team_close）时才清——"正文活到 close"。
			JobOutputs:   seelebridge.NewTeamworkJobOutputs(repo, keyFor),
			KeyFor:       keyFor,
			MaxTeammates: runtimeLimits.Team.MaxTeammates,
		}); err != nil {
			return fmt.Errorf("装配 teamwork 编排面失败: %w", err)
		}
	}
	app, err := initApplication(appEngine, runtime, pluginManager, sessionManager, skillRegistry, wsRepo, events, approval)
	if err != nil {
		return err
	}
	defer app.Shutdown()
	console.LogStageIf(backendTrace, "startup.application.ready")
	// 子代理中断恢复的「补历史」步骤：父侧缺失的工具结果（含中断的
	// subagent 派发）在续跑前补齐 provider-only tool 占位。
	runtime.SetSubagentParentRepairer(app.PrepareProviderHistory)
	// 配置容错：启动期非致命警告（如 accounts.yaml 解析失败）以系统通知
	// 进入会话可见区，GUI 另弹原生对话框；应用照常启动，不再闪退。
	startupWarnings := runtime.StartupWarnings()
	for _, warning := range startupWarnings {
		app.AddNotice("⚠ 启动配置警告: " + warning)
	}
	// 插件根读数（加载了几个插件、从哪个根）**进 UI 面**：旧口径里这条只写进终端
	// 日志（logPluginRoots），GUI 里根本看不到——所以"广播"那个词名不副实（它既不是
	// 广播，也没有第二个读者）。现在终端与 UI 两处写的是**同一条读数**（pluginRootReport
	// 生成一次，两处引用同一份字节）。
	if pluginStartup.RootReading != "" {
		app.AddNotice("插件: " + pluginStartup.RootReading)
	}
	if pluginStartup.Curated != "" {
		app.AddNotice("⚠ 启动配置警告: " + pluginStartup.Curated)
	}
	registerTaskTerminalTools(runtime, app)
	registerGoalTools(runtime, app)
	// P1：真实 TL 评估器装配（seelebridge 账号 completer → goal 域
	// TLEvaluator）；注入发生在首次会话启动前，Supervisor 首次 bind 即启用。
	// ADVISOR 的角色提示词来自 Agent Team 的员工登记：装配根先把"读已装配
	// 提示词"的读面注入 Runtime（未登记 → 内置角色设定；输出契约永远追加）。
	// 能力轴（按会话插件装配）的角色自带读面：与提示词同源同姿势。注册表里
	// RoleSpec.Plugins 登记了什么，这个角色的回合就装配什么（空 = 不覆盖：工具面
	// 继承宿主当前装配 + 技能目录不注入）。
	runtime.SetRolePluginsProvider(func(roleName string) []string {
		sessionID := app.Snapshot().Session.ID
		if sessionID == "" {
			return nil
		}
		plugins, err := app.AgentTeamRolePlugins(sessionID, roleName)
		if err != nil {
			return nil
		}
		return plugins
	})
	runtime.SetRolePromptProvider(func(roleName string) string {
		sessionID := app.Snapshot().Session.ID
		if sessionID == "" {
			return ""
		}
		prompt, err := app.AgentTeamRolePrompt(sessionID, roleName)
		if err != nil {
			return ""
		}
		return prompt
	})
	// 员工权责接线：把"角色会话 → ToolsPolicy"的读面注入权限门。员工的工具调用
	// 因此判成 emp_ro / emp_rw 主体：位齐按组默认/规则走，位缺（违权）走执行选择
	// 页面提权（人类的选择页 = sudo 口令）。
	//
	// 归属解析走 application 侧的**反向索引**（角色会话 → 归属主会话 + 权责），
	// 不再锚"当前视图会话"：角色会话可能属于后台会话，按视图会话查会查不到而落回
	// root（= 不拦）。索引只有"读到过该角色会话的注册表"才有事实，所以角色回合
	// 执行体仍应按构造把主体类放进 ctx（见 WithEmployeeSubjectClass）——两条路
	// 给出同一个结论，任何一条可用即拦得住。
	runtime.SetRoleSessionPolicyResolver(func(roleSessionID string) (string, bool) {
		if strings.TrimSpace(roleSessionID) == "" {
			return "", false
		}
		return app.RoleSessionToolsPolicy(roleSessionID)
	})
	// 员工/评审者越权提权的审批归属：角色会话（goal-a2a-pm / advisor:<main>）不在
	// 用户视图里，也没有自己的会话单元，因此它的待批请求不会进视图单格、不会进目录
	// awaiting_approval——对宿主完全不可见，只能等审批超时被拒。折算到宿主主会话后，
	// **现有**审批面板/目录/会话快照三条读面原样复用（不新建面板）。
	//
	// 与主体判定无关：判定仍按角色会话的主体（emp_<角色>）走，这里只改"审批弹在哪"。
	runtime.SetRoleSessionOwnerResolver(func(roleSessionID string) (string, bool) {
		if strings.TrimSpace(roleSessionID) == "" {
			return "", false
		}
		return app.RoleSessionOwner(roleSessionID)
	})
	if tlEvaluator := runtime.GoalTLEvaluator(); tlEvaluator != nil {
		app.SetGoalTLEvaluator(tlEvaluator)
	}
	registerSkillActivateTool(runtime, skillRegistry, func(name string) (skill.Skill, error) {
		info, err := app.ActivateSkill(name)
		if err != nil {
			return skill.Skill{}, err
		}
		return skill.Skill{Name: info.Name, Description: info.Description, Prompt: info.Prompt}, nil
	})
	registerContextReadTools(runtime, app)
	registerProjectRefreshTool(runtime, store)
	registerScheduledTaskCapability(runtime)
	toolHooks.Bind(app)
	// plan 节点事件 / 子代理树生命周期 / task 变更均由 application 内的
	// CSP 消费者经 channel 处理（service_assembler 启动），无需模型调用
	// 任何工具，worktable/task 增量自动发布（被动技能）。
	// Application 在状态迁移后向 Runtime 发布不可变可见性和父证据投影；
	// Runtime 只读自己的缓存，子代理 merge-back 写 Runtime 有界 mailbox，
	// 由主会话在下一次 ChatStream 前锁外消费。
	app.PublishRuntimeProjections()
	// 启动期装配一次注入（RuntimeDeps）：项目知识/子代理工具回调/skill 目录
	// + 计划事件钩子 + 压缩归档 + 调度器执行器/观察者 + 诊断观察者。
	// 对应原散装单字段 setter，装配点统一走 Deps 结构。
	var bashObserver seelebridge.BashDiagnosticObserver
	if backendTrace != nil {
		bashObserver = backendTrace.LogBashEvent
	}
	runtime.ApplyDeps(seelebridge.RuntimeDeps{
		BashDiagnosticObserver: bashObserver,
		TurnArchiver: &core.CompressedTurnArchiver{
			Sessions:          sessionManager,
			SessionIDProvider: func() string { return app.Snapshot().Session.ID },
			// 显式项目作用域：压缩原文的落盘键必须与读面
			// read_compressed_turn（LoadToolResultWorkspace(workspaceID, sessionID, ref)）
			// 一致。先按会话自己的绑定解析（与 eventStore 同一解析器，R3 键漂移
			// 修复同源），未绑定的会话退回视图当前工作区（与读面同源）。
			ProjectIDProvider: func(sessionID string) string {
				if workspace, ok := wsRepo.SessionWorkspace(sessionID); ok {
					return workspace.ID
				}
				return ""
			},
			WorkspaceIDProvider: func() string {
				return session_runtime.WorkspaceID(app.Snapshot().CurrentWorkspace)
			},
		},
		ProjectKnowledge: func() *sessionstore.ProjectRecord {
			record, readErr := store.LoadProjectRecord(store.Workspace())
			if readErr != nil {
				return nil
			}
			return &record
		},
		EventPersister: eventStore.Append,
		PlanApprovalGate: &adapters.PlanApprovalGate{
			Broker: approval,
			// 波 4 approval 会话级归属：plan/manual 审批节点随执行 ctx
			// 归属到目标会话（与 runChat 同一路由键）。
			SessionIDFromContext: coretask.SessionIDFromContext,
		},
		SubagentToolCallback: app.HandleSubagentToolEvent,
		// 员工回合的工具活动：subagent 那条的对称面。装配根只做搬运，投影与事件发布
		// 都在 application（HandleRoleToolActivity）。
		RoleToolCallback: app.HandleRoleToolActivity,
		SkillRegistry:    skillRegistry,
		ScheduledPromptExecutor: func(ctx context.Context, prompt, sessionID string) (string, error) {
			// 会话绑定：显式 sessionID 必须匹配当前主会话（切换后跳过，
			// 不误投递）；空 = 执行时当前 main session。
			current := app.Snapshot().Session.ID
			if sessionID != "" && sessionID != current {
				return "", fmt.Errorf("任务绑定会话 %s，当前会话 %s（已切换），本次跳过", sessionID, current)
			}
			if err := app.Submit(ctx, prompt); err != nil {
				return "", err
			}
			return "已提交到当前会话执行（异步输出见会话记录）", nil
		},
		SchedulerObserver: app.RefreshRuntimeSnapshot,
	})
	if frontend == "backend" && strings.TrimSpace(*backendProject) != "" {
		if err := console.BindProject(app, *backendProject); err != nil {
			return err
		}
		console.LogStageIf(backendTrace, "startup.workspace.ready")
	}
	console.LogStageIf(backendTrace, "startup.frontend.ready")
	return startFrontend(app, backendOutput, strings.Join(startupWarnings, "\n"))
}

func registerContextReadTools(runtime *seelebridge.Runtime, app *application.Service) {
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
	// compact_context：把 /compact 这条手动压缩入口同样交给模型——它可以在
	// 上下文逼近上限、或开始一段长任务前主动收拢上下文。落点与引擎自动压缩、
	// 命令 /compact 完全同一条（context_runtime.CompactContextNow），原始轮次
	// 仍完整留在会话存储，细节按引用回读。
	compactContextSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"reason": map[string]interface{}{"type": "string", "description": "为什么现在压缩（写入压缩记录，供审计与回看）"},
		},
	}
	runtime.RegisterTool("compact_context", "Compact this session's context now: fold the variable transcript into a bounded checkpoint frame (stable prefix + task evidence summary + plan + current request) while every original turn stays readable by reference. Call it before a long multi-step task or when the context is close to the provider limit; it is not a substitute for finishing the current step.", compactContextSchema, app.CompactContextHandler)
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

// registerScheduledTaskCapability 装配定时周期任务白名单命令：
// 登记 auto_get_jobs（脚本目录或 python 不可用时跳过并告警）。prompt
// 任务执行器与状态 observer 由主流程经 Runtime.ApplyDeps 一次注入
// （对应 RuntimeDeps.ScheduledPromptExecutor / SchedulerObserver）。
func registerScheduledTaskCapability(runtime *seelebridge.Runtime) {
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
}

// resolveAutoGetJobsDir 定位 auto_get_jobs 脚本目录（责任链 + 缺失即初始化，
// 口径见 internal/bootseed）：
//
//	存在即读：CWD 相对的 local/tools/auto_get_jobs/main.py、二进制旁边那份，
//	          谁先存在就用谁（用户自己放好的脚本永远优先）。
//	缺失即初始化：两处都没有 main.py → 把**骨架**（README.md + .env.example）
//	          写到二进制旁边的 local/tools/auto_get_jobs/，让"该放什么、放哪儿"
//	          在文件系统上可见；脚本本体是第三方项目，不随包分发。
//
// 命令登记的安全口径不变：没有 main.py 就不登记（登记即信任、argv 固定直传）。
func resolveAutoGetJobsDir() (string, bool) {
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(exe)
	}
	return resolveToolDir(filepath.Join("local", "tools", "auto_get_jobs"), exeDir, bootseed.AutoGetJobsPack())
}

// resolveToolDir 是 resolveAutoGetJobsDir 的可测内核（exeDir 由调用方给，测试才好
// 摆现场）：按责任链找工具目录，两个候选是 CWD 相对的那份与 <exeDir>/ 下那份。
// 命中即用；全缺则把骨架（pack）初始化到 <exeDir>/<relative>，但没有 main.py 就
// 不认账——返回 false，调用方跳过白名单命令登记。
func resolveToolDir(relative, exeDir string, pack bootseed.Pack) (string, bool) {
	candidates := []string{filepath.Join(relative, "main.py")}
	seedRoot := ""
	if exeDir != "" {
		candidates = append(candidates, filepath.Join(exeDir, relative, "main.py"))
		seedRoot = filepath.Join(exeDir, relative)
	}
	result, err := bootseed.Resolve(bootseed.Spec{
		Name:       relative,
		Candidates: candidates,
		SeedRoot:   seedRoot,
		Pack:       pack,
	})
	if err != nil {
		log.Printf("scheduled tasks: 初始化 %s 目录骨架失败: %v", relative, err)
	}
	if result.Kind == bootseed.KindHit {
		return filepath.Dir(result.Path), true
	}
	if len(result.Written) > 0 {
		log.Printf("scheduled tasks: %s 脚本目录未找到（期望 %s），已初始化骨架 %v；仍缺 main.py，跳过该白名单命令登记", pack.Name, relative, result.Written)
		return "", false
	}
	log.Printf("scheduled tasks: %s 脚本目录未找到（期望 %s），跳过该白名单命令登记", pack.Name, relative)
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
	// 责任链 = CWD 的 config/ → CWD 根目录 → 二进制目录的 config/（见
	// runtimeConfigChain）。命中就按它读（存在即读）；全都没有 → 用内嵌默认档在
	// 二进制旁边初始化，再读它（internal/bootseed）。滑动窗口段缺失 → 零值走默认；
	// limits 缺失字段 → 默认值。
	runtimeConfigPath := ensureConfigFile(bootseed.RuntimeConfigName, bootseed.RuntimeConfigPack())
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
	toolCallTimeout, _, planDecision, heartbeat, replanWindow, searchTimeout := limits.Durations()
	core.ApplyLimits(limits)
	// window 段同时注入应用侧压缩保留窗口决策（压缩前缀 = min(retain_tokens,
	// ratio × 全量上下文)，硬压缩阈值 force_compact_tokens）。
	core.ApplyWindowConfig(windowConfig)
	search.ApplyLimits(int(searchTimeout / time.Second))
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
	// 保留窗口配置校验（《压缩四区模型》边界判定）：保护区下限（比例 × 预算）
	// 高于保留上限 window.retain_tokens 是非法组合，必须在这里拒绝启动 ——
	// 静默取小会把它吞成"配置看起来生效"，直到保护区被压到失忆才以"模型忘了"
	// 的形式显现。预算口径与压缩判据同源（task_context.ContextBudgetFor）。
	if err := core.ValidateRetainWindow(windowConfig, limits, runtime.ContextWindow()); err != nil {
		runtime.Shutdown()
		return nil, fmt.Errorf("加载 window 配置失败: %w", err)
	}
	return runtime, nil
}

func initSkillSystem() *skill.Registry {
	// skills are now per-plugin (plugins/<name>/<skill>/SKILL.md).
	// The registry is populated via PublishPluginSkills on plugin Load/Activate.
	return skill.NewRegistry()
}

// pluginStartup 是一次插件系统启动的读数（终端日志与 UI 面**同源**，见 pluginRootReport）：
//
//	RootReading —— 根读数（加载了几个插件、从哪个根；进终端日志 + UI 启动通知）
//	Curated     —— 精选目录读不到时的出声（空 = 目录已读到，无声；非空 = 启动期警告）
type pluginStartup struct {
	RootReading string
	Curated     string
}

func initPluginSystem(
	runtime *seelebridge.Runtime,
	skills *skill.Registry,
) (*plugin.Manager, pluginStartup, error) {
	// 根解析 = 责任链（-plugins > $SEELEX_PLUGINS > <exe>/plugins > <exe>/../plugins
	// > plugins(CWD)），见 pluginRootChain；加载器是多根 first-wins。
	roots := pluginRoots()
	loader := plugin.NewLoader(roots...)
	manager := plugin.NewManager(loader, runtime, runtime, skills)
	if err := manager.Load(); err != nil {
		return nil, pluginStartup{}, fmt.Errorf("加载 Plugin 失败: %w", err)
	}
	loaded := manager.All()
	if err := requirePlugins(roots, loaded); err != nil {
		return nil, pluginStartup{}, err
	}
	startup := pluginStartup{RootReading: logPluginRoots(roots, loaded)}
	// 精选目录（A：从"声明面"变成"装配面"）：**启动时**从已解析的插件根读一次，
	// 读到的判决函数接进运行期的装配校验（team_plan members[].plugins）——
	// pending 候选（路线图）要能点名上游来源、认不出的名字要说清两边都不在。
	//
	// 三档出声（2026-10-05 重排：使用者自己改插件不该被自己的 app 警告）：
	//   - 目录**缺席**（自建根/用户树没这份文件）⇒ 只写终端一行：合法现场，不是警告；
	//   - 目录**存在却读不动**（解析/结构失败）⇒ 终端 + UI 启动警告（这是配置缺陷）；
	//   - 目录能读、但与这台机器**已装集合有差异** ⇒ 只写终端一行（本机自装/退役插件）。
	// 无论哪一档，判决函数都会被注入：它对每个未定义名显式拒绝并说清"用哪个根找过"，
	// 所以"没有目录/目录坏了"在装配面上同样是显式失败，而不是"没有 pending"。
	// 发现 → 读回 → 落进 yaml：磁盘是事实，精选目录是事实的读数。运行树里的插件可能是
	// agent 自己长出来的（plugin_create）、也可能是手工/上一次会话放进来的——先登记，再读
	// 判决面；否则它们只会被读成"漂移"，使用者只能自己把机器该做的簿记抄进 YAML。
	if registered, err := manager.RegisterDiscoveredPlugins(); err != nil {
		log.Printf("plugin: 精选目录登记失败（目录保持原样，不影响启动）: %v", err)
	} else if len(registered) > 0 {
		log.Printf("plugin: 已发现并登记本机自建插件进 %s: %s",
			plugin.CuratedFileName, strings.Join(registered, ", "))
	}
	curated := resolveCuratedRead(roots, loaded)
	runtime.SetPluginUnassembledReason(curatedAssemblyJudge(curated))
	switch {
	case curated.Absent:
		log.Printf("plugin: 没有精选目录——%v（找过: %s）；自建根/用户树可以不带它，装配面对未定义名仍显式拒绝",
			curated.Err, curated.Page())
	case curated.Err != nil:
		message := fmt.Sprintf(
			"%s 读不到（%v；责任链上找过的根: %s）——装配面的名字判定会显式拒绝未定义的名字，不会静默当空目录",
			plugin.CuratedFileName, curated.Err, curated.Page())
		log.Printf("plugin: %s", message)
		startup.Curated = message
	case len(curated.Drift) > 0:
		log.Printf("plugin: %s 与本次已装集合有差异（不是错误，是回报）：%s",
			curated.Path, strings.Join(curated.Drift, "；"))
	}
	return manager, startup, nil
}

func activateDefaultPlugin(manager *plugin.Manager, eng *frameworkSession.Session) error {
	if _, err := pluginByName(manager.All(), "default"); err != nil {
		// 同一条静默降级链的另一半：插件加载到了，但没有 default（启动基线）。
		// 这里不阻断启动（自定义根可以只带一个垂直插件），但不许无声无息。
		log.Printf("plugin: 没有 default 插件——启动基线未激活（skill 目录与 `#` 切换为空）；可用插件见 plugins/curated.yaml 的 entries")
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

func registerProductTools(runtime *seelebridge.Runtime, plugins *plugin.Manager, eng pluginPromptEngine, approval *application.ApprovalBroker, skills *skill.Registry) {
	registerTimeTool(runtime)
	websearch.Register(runtime, accountsPath())
	registerMCPServers(runtime, accountsPath()) // mcpstack/config 加载 + Runtime 冷启动登记
	registerMCPLoadTool(runtime)
	registerPluginSwitchTools(runtime, plugins, eng)
	registerPluginSelfTools(runtime, plugins)
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
			Args: s.Args, Env: s.Env, URL: s.URL, ToolNotes: s.ToolNotes,
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

// registerPluginSelfTools 注册插件/技能/MCP 的自迭代工具族：
// 创建（写盘）→ 加载（reload）→ 列表查看，闭环让 main agent 可以自己扩展
// 运行时能力。plugin 与 skill 的写盘都**不改变运行时状态**，由 plugins_reload
// 事务式应用；MCP 走冷启动登记 + mcp_load 连接。
func registerPluginSelfTools(runtime *seelebridge.Runtime, plugins *plugin.Manager) {
	// ── plugin_create ──────────────────────────────────────────────
	runtime.RegisterTool(
		"plugin_create",
		"在 plugins 目录脚手架一个插件（plugin.md manifest + README + 可选 skills），写入磁盘但不立即生效；调用后请执行 plugins_reload 加载。插件名须匹配 ^[a-z0-9][a-z0-9_-]*$ 且目录不存在。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":        map[string]interface{}{"type": "string", "description": "插件名（小写字母/数字/_-）"},
				"description": map[string]interface{}{"type": "string", "description": "插件职责一句话"},
				"include":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "允许暴露的工具前缀"},
				"exclude":     map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "排除的工具前缀"},
				"prompt":      map[string]interface{}{"type": "string", "description": "插件系统提示词正文"},
				"skills": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"name":        map[string]interface{}{"type": "string"},
							"description": map[string]interface{}{"type": "string"},
							"prompt":      map[string]interface{}{"type": "string", "description": "skill 指令正文"},
						},
						"required": []string{"name"},
					},
				},
			},
			"required": []string{"name"},
		},
		func(ctx context.Context, argsJSON string) (string, error) {
			var input plugin.CreateSpec
			if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
				return "", fmt.Errorf("plugin_create: %w", err)
			}
			paths, err := plugins.Create(input)
			if err != nil {
				return "", fmt.Errorf("plugin_create: %w", err)
			}
			encoded, _ := json.Marshal(map[string]interface{}{
				"status": "created", "plugin": input.Name, "files": paths,
				"next": "调用 plugins_reload 使插件生效",
			})
			return string(encoded), nil
		},
	)

	// ── plugins_reload ─────────────────────────────────────────────
	runtime.RegisterTool(
		"plugins_reload",
		"重新扫描 plugins 目录并事务式应用差异（新增/删除/修改）：新增插件立即可见但不激活，修改中的当前激活插件先停用再按新定义重新激活；任一步失败回滚到上一个可用状态。用于 plugin_create / skill_create 之后的加载。",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		func(ctx context.Context, _ string) (string, error) {
			report, err := plugins.Reload(ctx)
			if err != nil {
				return "", fmt.Errorf("plugins_reload: %w", err)
			}
			// 发现 → 读回 → 落进 yaml：刚写盘的插件（plugin_create / skill_create / 别的
			// 工具放进去的）在这一步被登记进精选目录——"创建 → 加载 → 目录跟上"是同一个闭环，
			// 别让使用者手抄插件清单。登记失败不改判定：回执里留一条读数，启动期还会重试。
			registered, rerr := plugins.RegisterDiscoveredPlugins()
			if rerr != nil {
				log.Printf("plugin: 精选目录登记失败（目录保持原样）: %v", rerr)
			}
			report.CatalogRegistered = registered
			encoded, _ := json.Marshal(report)
			return string(encoded), nil
		},
	)

	// ── plugins_list ───────────────────────────────────────────────
	runtime.RegisterTool(
		"plugins_list",
		"列出全部已加载插件及其描述、激活态、include/exclude、skills 与 MCP server 清单。",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		func(ctx context.Context, _ string) (string, error) {
			type pluginView struct {
				Name        string   `json:"name"`
				Description string   `json:"description,omitempty"`
				Active      bool     `json:"active"`
				Include     []string `json:"include,omitempty"`
				Exclude     []string `json:"exclude,omitempty"`
				Skills      []string `json:"skills,omitempty"`
				MCPServers  []string `json:"mcp_servers,omitempty"`
			}
			active, _ := plugins.Current()
			views := make([]pluginView, 0)
			for _, p := range plugins.All() {
				view := pluginView{
					Name: p.Name, Description: p.Description, Active: active.Name == p.Name,
					Include: p.Include, Exclude: p.Exclude,
				}
				for _, s := range p.Skills {
					view.Skills = append(view.Skills, s.Name)
				}
				for _, server := range p.MCPServers {
					view.MCPServers = append(view.MCPServers, server.Name)
				}
				views = append(views, view)
			}
			encoded, err := json.Marshal(views)
			return string(encoded), err
		},
	)

	// ── skill_create ───────────────────────────────────────────────
	runtime.RegisterTool(
		"skill_create",
		"在指定插件目录下脚手架一个 skill（<plugin>/<skill>/SKILL.md，含 name/description frontmatter 与指令正文），写入磁盘但不立即生效；调用后请执行 plugins_reload 加载。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"plugin":      map[string]interface{}{"type": "string", "description": "目标插件名（须已存在）"},
				"name":        map[string]interface{}{"type": "string", "description": "skill 名"},
				"description": map[string]interface{}{"type": "string"},
				"prompt":      map[string]interface{}{"type": "string", "description": "skill 指令正文"},
			},
			"required": []string{"plugin", "name"},
		},
		func(ctx context.Context, argsJSON string) (string, error) {
			var input plugin.SkillSpec
			var meta struct {
				Plugin string `json:"plugin"`
				plugin.SkillSpec
			}
			if err := json.Unmarshal([]byte(argsJSON), &meta); err != nil {
				return "", fmt.Errorf("skill_create: %w", err)
			}
			input = meta.SkillSpec
			path, err := plugins.CreateSkill(meta.Plugin, input)
			if err != nil {
				return "", fmt.Errorf("skill_create: %w", err)
			}
			encoded, _ := json.Marshal(map[string]interface{}{
				"status": "created", "plugin": meta.Plugin, "skill": input.Name, "file": path,
				"next": "调用 plugins_reload 使 skill 生效",
			})
			return string(encoded), nil
		},
	)

	// ── skills_list ────────────────────────────────────────────────
	runtime.RegisterTool(
		"skills_list",
		"列出全部已加载插件内的 skill（无论是否激活）及其描述与来源路径。",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		func(ctx context.Context, _ string) (string, error) {
			type skillView struct {
				Name        string `json:"name"`
				Description string `json:"description,omitempty"`
				Plugin      string `json:"plugin,omitempty"`
				Path        string `json:"path,omitempty"`
			}
			views := make([]skillView, 0)
			for _, p := range plugins.All() {
				for _, s := range p.Skills {
					views = append(views, skillView{
						Name: s.Name, Description: s.Description, Plugin: p.Name, Path: s.FilePath,
					})
				}
			}
			encoded, err := json.Marshal(views)
			return string(encoded), err
		},
	)

	// ── mcp_create ─────────────────────────────────────────────────
	runtime.RegisterTool(
		"mcp_create",
		"冷启动登记一个 MCP server（内存态，本次会话有效）：登记后调用 mcp_load 连接并注册其工具。注意：不写入 accounts.yaml，重启后需重新登记。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":      map[string]interface{}{"type": "string", "description": "MCP server 名"},
				"transport": map[string]interface{}{"type": "string", "enum": []string{"stdio", "sse"}, "description": "stdio（本地命令）或 sse（远程 URL）"},
				"command":   map[string]interface{}{"type": "string", "description": "stdio 启动命令"},
				"args":      map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"env":       map[string]interface{}{"type": "object", "description": "环境变量（不含机密）"},
				"url":       map[string]interface{}{"type": "string", "description": "sse 端点 URL"},
			},
			"required": []string{"name", "transport"},
		},
		func(ctx context.Context, argsJSON string) (string, error) {
			var input struct {
				Name      string            `json:"name"`
				Transport string            `json:"transport"`
				Command   string            `json:"command,omitempty"`
				Args      []string          `json:"args,omitempty"`
				Env       map[string]string `json:"env,omitempty"`
				URL       string            `json:"url,omitempty"`
			}
			if err := json.Unmarshal([]byte(argsJSON), &input); err != nil {
				return "", fmt.Errorf("mcp_create: %w", err)
			}
			env := make([]string, 0, len(input.Env))
			for key, value := range input.Env {
				env = append(env, key+"="+value)
			}
			cfg := seelebridge.MCPServer{
				Name: input.Name, Transport: input.Transport, Command: input.Command,
				Args: input.Args, Env: env, URL: input.URL,
			}
			if err := runtime.RegisterLazyMCP(input.Name, cfg); err != nil {
				return "", fmt.Errorf("mcp_create: %w", err)
			}
			encoded, _ := json.Marshal(map[string]interface{}{
				"status": "registered", "server": input.Name,
				"next": "调用 mcp_load 连接并注册工具",
			})
			return string(encoded), nil
		},
	)

	// ── mcp_list ───────────────────────────────────────────────────
	runtime.RegisterTool(
		"mcp_list",
		"列出已登记的 MCP server（冷启动未连接 + 已连接）及其存活状态与工具数。",
		map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		func(ctx context.Context, _ string) (string, error) {
			type mcpView struct {
				Name   string `json:"name"`
				Loaded bool   `json:"loaded"`
				Alive  bool   `json:"alive,omitempty"`
				Tools  int    `json:"tools,omitempty"`
			}
			names := append([]string(nil), runtime.LazyMCPServerNames()...)
			seen := make(map[string]bool, len(names))
			views := make([]mcpView, 0, len(names))
			for _, name := range names {
				seen[name] = true
				alive, tools, _ := runtime.MCPServerStatus(name)
				views = append(views, mcpView{Name: name, Loaded: alive, Alive: alive, Tools: tools})
			}
			for _, name := range runtime.MCPServerNames() {
				if seen[name] {
					continue
				}
				alive, tools, _ := runtime.MCPServerStatus(name)
				views = append(views, mcpView{Name: name, Loaded: alive, Alive: alive, Tools: tools})
			}
			encoded, err := json.Marshal(views)
			return string(encoded), err
		},
	)
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
				// 波 4 approval 会话级归属：ask_approve 随工具 ctx 归属到
				// 发起会话（不再以进程级空归属进入视图单格）。
				SessionID: coretask.SessionIDFromContext(ctx),
				Options:   options, Risk: "low", ToolName: "ask_approve",
			})
			if err != nil || !adapters.ApprovalAccepted(decision.OptionID) {
				return `{"approved":false,"reason":"cancelled"}`, nil
			}
			encoded, err := json.Marshal(map[string]interface{}{"approved": true, "choice": decision.OptionID})
			return string(encoded), err
		},
	)
}

// guardMultiProcess 是启动期多进程闸门：数据根是单进程写者
// （sessionstore/data_root_lock.go 的 lock.owner）。默认单实例——另一个实例正在写
// 同一数据根时，这里给出**可读**的拒绝（对齐 ErrDataRootLocked 的口径，但更早、
// 更可操作）。只有 limits.runtime.allow_multi_process: true 才放行；代价见
// config/seelex.yaml 的 runtime 块（放行后失去跨进程写者串行化，一致性由使用者负责）。
//
// 这是**早期**闸门（发生在抢锁之前）：真正的不变量仍由 initStore → sessionstore
// 的数据根锁兜底，所以这里只做「能早就早」的可读拒绝，不替代锁本身。
func guardMultiProcess() error {
	if runtimeLimits.Runtime.AllowMultiProcess {
		return nil
	}
	dataRoot := filepath.Dir(*storePath)
	staleAfter := time.Duration(runtimeLimits.SessionStorage.LockStaleAfterSeconds) * time.Second
	if owner, held := sessionstore.DataRootLockedByWith(dataRoot, staleAfter); held {
		return fmt.Errorf(
			"数据根 %s 正被另一个 Seelex 实例独占（pid=%d program=%s host=%s）：默认单实例。"+
				"要允许并行启动，在 config/seelex.yaml 设 limits.runtime.allow_multi_process: true"+
				"（代价：多个进程共用同一数据根，失去跨进程写者串行化，一致性由使用者负责）",
			dataRoot, owner.PID, owner.Program, owner.Hostname)
	}
	return nil
}

func initStore() (*sessionstore.Router, error) {
	// NestedSessionStore 的 baseDir 与 workspace_index.json 同级
	baseDir := filepath.Dir(*storePath)
	sessionstore.ApplyLimits(runtimeLimits.SummaryChars)
	router, err := sessionstore.NewRouter(
		filepath.Join(baseDir, "session-storage.json"), baseDir, sessionStorageLimits())
	if err != nil {
		return nil, fmt.Errorf("初始化嵌套存储失败: %w", err)
	}
	return router, nil
}

// sessionStorageLimits 把 seele.yaml limits.session_storage 映射成存储覆盖层
// （my_design §11 覆盖链的中间一层：默认值 ← limits ← session-storage.json）。
// 分片行数沿用既有顶层键 limits.message_shard_size。
func sessionStorageLimits() sessionstore.Settings {
	limits := runtimeLimits.SessionStorage
	// 指针：把 limits.runtime.allow_multi_process 原样传进 §9 数据根锁（存储侧默认
	// 也是 false，这里显式带过去，让配置一眼看得出生效）。
	allowMultiProcess := runtimeLimits.Runtime.AllowMultiProcess
	return sessionstore.Settings{
		MessageShardRows:        runtimeLimits.MessageShardSize,
		RetryCacheMaxItems:      limits.RetryCacheMaxItems,
		RetryCacheMaxChars:      limits.RetryCacheMaxChars,
		WireRecentErrors:        limits.RetryCacheWireRecentErrors,
		CompactFrameThreshold:   limits.RetentionCompactFrameThreshold,
		RawBytesAlert:           uint64(limits.RetentionRawBytesAlert),
		RetentionMode:           limits.RetentionMode,
		QueuePersistPending:     limits.QueuePersistPending,
		StaleAfterSeconds:       limits.LockStaleAfterSeconds,
		AutoRecover:             limits.LockAutoRecover,
		AllowMultiProcess:       &allowMultiProcess,
		BlobSoftLimitChars:      limits.BlobSoftLimitChars,
		BlobHardLimitBytes:      limits.BlobHardLimitBytes,
		BlobSessionQuotaBytes:   limits.BlobSessionQuotaBytes,
		MediaMaxItemBytes:       limits.MediaMaxItemBytes,
		MediaSessionQuotaBytes:  limits.MediaSessionQuotaBytes,
		MediaMaxItemsPerSession: limits.MediaMaxItemsPerSession,
		MediaMaxLongSide:        limits.MediaMaxLongSide,
		WireBudgetTokens:        limits.WireBudgetTokens,
		WireSoftRatio:           limits.WireSoftRatio,
		WireTargetRatio:         limits.WireTargetRatio,
	}
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
	manager := session.NewManager().WithRouter(router)
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
	sessionPort := adapters.SessionPort{Manager: sessions, Runtime: runtime}
	// 会话展示元数据（置顶/别名/排序位）按项目存一份 blob：与 SessionRecord 的
	// 落盘路径完全隔离，不会被回合结束的记录重建覆盖。
	sessionPort.Meta = sessionstore.NewSessionMetaStore(sessions.Router())
	// 会话级操作（删除/读历史）按会话绑定项目解析，避免视图切换后活跃
	// 写作用域变化导致删错/读错项目（R3 键漂移 + 列表污染根因）。
	sessionPort.SetWorkspaceResolver(func(sessionID string) string {
		if workspace, ok := workspaces.SessionWorkspace(sessionID); ok {
			return workspace.ID
		}
		return ""
	})
	// 已知项目来源：绑定缺失时归属解析按"数据实际所在"定位，都没有 = 未关联
	// （默认项目），绝不按活跃写作用域猜（视图切换会改变它）。
	sessionPort.SetProjectSource(func() []string {
		items := workspaces.List()
		ids := make([]string, 0, len(items))
		for _, item := range items {
			ids = append(ids, item.ID)
		}
		return ids
	})
	return application.New(application.Dependencies{
		Engine: eng, Runtime: adapters.RuntimePort{Runtime: runtime},
		Plugins: adapters.PluginPort{Manager: plugins}, Skills: adapters.SkillPort{Registry: skills},
		Sessions: sessionPort, Workspace: adapters.WorkspacePort{Repo: workspaces},
		Events: events, Approval: approval,
		// 员工提示词的一次有界优化（Agent Team 入职面板）：实现方是 Runtime
		// 主 completer；未配置账号/completer 时应用层返回可展示错误。
		RolePrompt: runtime,
		// 装配期分配员工权限（与用户权限同一张权责表）：装配团队时把在编员工落成
		// 各自的主体条目 emp_<角色名>，员工回合的工具判定与工具面都按它生效。
		EmployeePermissions: runtime,
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

func startFrontend(app *application.Service, backendOutput io.Writer, startupWarning string) error {
	switch *frontendMode {
	case "gui":
		if err := gui.Run(app, gui.Options{
			Title: "Seelex", Version: Version, StartupWarning: startupWarning,
		}); err != nil {
			return fmt.Errorf("GUI 错误: %w", err)
		}
		return nil
	case "headless":
		// headless 调试入口：无窗口装配同一 Application，经
		// SEELEX_HEADLESS_PORT 回环 RPC + 事件流驱动（gui/headless.go）。
		if err := gui.RunHeadless(app); err != nil {
			return fmt.Errorf("headless 错误: %w", err)
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
	case "tui", "gui", "headless", "backend":
		return mode, nil
	default:
		return "", fmt.Errorf("%q，允许值为 tui、gui、headless 或 backend", value)
	}
}

type permissionRuntime interface {
	SetPermissionConfig(toolspermission.PermissionConfig, toolspermission.ApprovalHandler)
	SetFullAccess(bool)
	SetPermissionTierFor(sessionID, tier string) error
}

// setupPermissionGate 根据 -permission 标志安装权限门控与起始档位。
// 起始先装 manual 权责基线（分组 + 主体 + 缺位口径 + 默认规则），config/seele.yaml
// 的 permission 段按字段覆盖它（缺失/为空的字段保持默认，不再整体替换规则集）。
// 权限档位（manual/edit/auto/full）只作为进程级默认启用；运行期由 GUI/CLI 按会话切换。
func setupPermissionGate(runtime permissionRuntime, approval *application.ApprovalBroker) error {
	tier, err := parsePermissionMode(*permissionMode)
	if err != nil {
		return err
	}
	cfg := seeltools.DefaultPermissionConfig()
	fileCfg, err := loadPermissionConfig(ensureConfigFile(bootseed.PermissionConfigName, bootseed.PermissionConfigPack()))
	if err != nil {
		return err
	}
	runtime.SetPermissionConfig(mergePermissionConfig(cfg, fileCfg), newPermissionBridge(approval))
	if tier != "" && tier != dto.PermissionTierManual {
		if err := runtime.SetPermissionTierFor("", tier); err != nil {
			return err
		}
	}
	return nil
}

// mergePermissionConfig 把 seele.yaml 的 permission 段叠加到默认权责配置上：只覆盖
// 显式给出的字段，其余保持默认——分组表与主体授权表是产品口径（谁能用哪一族工具），
// 不该因为一份只写了规则的旧配置就整块消失（那会让每个主体都变成"无授权"）。
func mergePermissionConfig(base, override toolspermission.PermissionConfig) toolspermission.PermissionConfig {
	merged := base
	if len(override.Rules) > 0 {
		merged.Rules = override.Rules
	}
	if len(override.Groups) > 0 {
		merged.Groups = override.Groups
	}
	if len(override.Subjects) > 0 {
		merged.Subjects = override.Subjects
	}
	if override.MissingBit != "" {
		merged.MissingBit = override.MissingBit
	}
	if override.Mode != "" {
		merged.Mode = override.Mode
	}
	return merged
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
		{ToolName: "todo_init", Action: toolspermission.ActionAllow},
		{ToolName: "todo_add", Action: toolspermission.ActionAllow},
		{ToolName: "todo_done", Action: toolspermission.ActionAllow},
		{ToolName: "todo_status", Action: toolspermission.ActionAllow},
		{ToolName: "task_add", Action: toolspermission.ActionAllow},
		{ToolName: "taskadd", Action: toolspermission.ActionAllow},
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

// loadPermissionConfig 读取 seele.yaml 的 permission 段（权限专用文件）：rules /
// groups / subjects / missing_bit / mode。文件缺失或 permission 段缺失 → 零值配置
// （mergePermissionConfig 保持默认）；解析失败显式报错。
func loadPermissionConfig(path string) (toolspermission.PermissionConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return toolspermission.PermissionConfig{}, nil
		}
		return toolspermission.PermissionConfig{}, fmt.Errorf("permission: read config: %w", err)
	}
	var file struct {
		Permission toolspermission.PermissionConfig `yaml:"permission"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		return toolspermission.PermissionConfig{}, fmt.Errorf("permission: parse config: %w", err)
	}
	return file.Permission, nil
}

// parsePermissionMode 解析 -permission 标志为**权限档位 id**（manual/edit/auto/
// full；兼容旧别名 full_access → full）。空/缺省 → manual。
//
// 档位取代旧的二元 manual/full_access：manual/edit/auto 是对 root 规则表的声明式
// 覆盖，full 是执行门短路放行（= 旧 full_access）。
func parsePermissionMode(value string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return dto.PermissionTierManual, nil
	}
	if trimmed == string(toolspermission.ModeFullAccess) {
		return dto.PermissionTierFull, nil
	}
	tier, err := dto.NormalizePermissionTier(trimmed)
	if err != nil {
		return "", fmt.Errorf("%q，允许值为 manual、edit、auto、full（旧别名 full_access）", value)
	}
	return tier, nil
}

// newPermissionBridge 创建连接 permission.ApprovalHandler → ApprovalBroker 的桥接器。
// 每次工具触发审批时，阻塞等待用户在 TUI 交互面板中作出选择。
func newPermissionBridge(broker *application.ApprovalBroker) toolspermission.ApprovalHandler {
	return func(ctx *toolspermission.ApprovalContext) (*toolspermission.ApprovalResponse, error) {
		req := ctx.Request
		appReq := application.ApprovalRequest{
			ID: req.ID,
			// 波 4 approval 会话级归属：权限审批的会话 ID 由 seelebridge
			// 权限中间件随调度 ctx 填充（registry_state.go）。
			SessionID:         req.SessionID,
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
