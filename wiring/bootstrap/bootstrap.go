package bootstrap

import (
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/RedHuang-0622/Seele/types"

	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/core"
	assemble "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	"github.com/RedHuang-0622/seelex/backend/engine/prompt/assets"
	"github.com/RedHuang-0622/seelex/backend/engine/session"
	"github.com/RedHuang-0622/seelex/backend/engine/skill"
	coretask "github.com/RedHuang-0622/seelex/backend/engine/task"
	"github.com/RedHuang-0622/seelex/backend/limit/overlay"
	seeteamwork "github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	toolreg "github.com/RedHuang-0622/seelex/backend/tool/registry"
	"github.com/RedHuang-0622/seelex/console"
	"github.com/RedHuang-0622/seelex/gui"
	"github.com/RedHuang-0622/seelex/store"
	"github.com/RedHuang-0622/seelex/wiring"
	"github.com/RedHuang-0622/seelex/wiring/adapters"
	"github.com/RedHuang-0622/seelex/wiring/config"
	"github.com/RedHuang-0622/seelex/wiring/registry"
)

// Options 是一次装配的全部输入：入口只把「参数 + 构建期版本」装进来，其余判断都在
// 这里做（这样第二个入口不会各自复述一遍启动顺序）。
type Options struct {
	// Flags 是已解析的启动参数（`wiring/config.BindFlags` 的产物，非空）。
	Flags *config.Flags
	// Version 是入口的版本号（`internal/buildinfo.Version`，可由 ldflags 注入）。
	Version string
}

// App 是一次装配的产物：Application + Runtime + 关机材料。
//
// **关机顺序是行为面**：Shutdown 按装配的逆序执行（后装的先关），与 M5 之前的
// `defer` 链逐条相同；入口必须在 Serve 返回后（或 defer）调用它一次。
type App struct {
	Application *application.Service
	Runtime     *assemble.Runtime
	Store       *store.Router

	Flags          *config.Flags
	Frontend       string
	Version        string
	BackendOutput  io.Writer
	StartupWarning string
	Trace          *console.EventLogger

	closeOutput func() error
	cleanup     []func()
}

// addCleanup 登记一个关机动作。顺序语义与 `defer` 一致：逆序执行。
func (a *App) addCleanup(fn func()) { a.cleanup = append(a.cleanup, fn) }

// Shutdown 按装配逆序释放全部部件（幂等：重复调用只会跑一次清理）。
func (a *App) Shutdown() { a.close() }

// close 执行清理栈；backend 诊断输出**最后**关（它的 defer 最先注册）。
func (a *App) close() {
	for i := len(a.cleanup) - 1; i >= 0; i-- {
		a.cleanup[i]()
	}
	a.cleanup = nil
	if a.closeOutput != nil {
		_ = a.closeOutput()
		a.closeOutput = nil
	}
}

// fail 是 Run 的错误返回姿态：先按已登记的清理栈收拾干净，再把原错误交给入口。
func (a *App) fail(err error) error {
	a.close()
	return err
}

// Serve 跑前端（tui/gui/headless/backend）——前端形态是入口选择，装配与它无关。
func (a *App) Serve() error {
	return StartFrontend(a.Flags, a.Application, a.BackendOutput, a.StartupWarning, a.Version)
}

// Run 装配一台完整的 Seelex。返回的 App 必须在 Serve 之后 Shutdown。
func Run(opts Options) (*App, error) {
	flags := opts.Flags
	if flags == nil {
		return nil, fmt.Errorf("bootstrap: Options.Flags 不能为空")
	}
	boot := &App{Flags: flags, Version: opts.Version}

	frontend, err := config.ParseFrontendMode(flags.FrontendMode)
	if err != nil {
		return nil, fmt.Errorf("前端模式无效: %w", err)
	}
	flags.FrontendMode = frontend
	boot.Frontend = frontend
	if frontend == "backend" {
		output, closeOutput, outputErr := console.OpenOutput(flags.BackendLogPath)
		if outputErr != nil {
			return nil, outputErr
		}
		boot.closeOutput = closeOutput
		boot.BackendOutput = output
		boot.Trace = console.NewEventLogger(output, time.Now)
		boot.Trace.LogStage("startup.flags.parsed")
	}
	if frontend == "gui" && !gui.Available() {
		return nil, boot.fail(fmt.Errorf(`当前二进制未包含 GUI；请使用 go run -tags "gui,desktop,production" . -frontend gui`))
	}
	mode, err := config.ParsePermissionMode(flags.PermissionMode)
	if err != nil {
		return nil, boot.fail(fmt.Errorf("权限模式无效: %w", err))
	}
	flags.PermissionMode = mode
	flags.StorePath = config.ResolveStorePath(flags.StorePath)

	// 提示词资产目录（config/prompt）：先按责任链定目录（候选链全缺则用内嵌
	// 默认词初始化），再让 promptassets 按"外部优先、内嵌逐文件兜底"解析。
	// 必须早于任何提示词消费——装配期的 promptassets.Validate 与运行期的
	// system prompt 组装都读它。
	promptassets.SetDir(config.ResolvePromptDir())

	backendTrace := boot.Trace
	console.LogStageIf(backendTrace, "startup.runtime.begin")
	runtime, err := initRuntime(flags.StorePath)
	if err != nil {
		return nil, boot.fail(err)
	}
	boot.addCleanup(runtime.Shutdown)
	console.LogStageIf(backendTrace, "startup.runtime.ready")
	// 启动期多进程闸门（limits.runtime.allow_multi_process，默认单实例）：数据根
	// 是单进程写者，另一个实例在写同一数据根时在这里给出可读拒绝，而不是让
	// ErrDataRootLocked 从装配深处冒出来（`InitStore` 真正抢锁时会再兜一次）。
	if err := guardMultiProcess(flags.StorePath); err != nil {
		return nil, boot.fail(err)
	}

	runtime.RegisterBuiltins()
	console.LogStageIf(backendTrace, "startup.builtins.ready")
	skillRegistry := InitSkillSystem()
	console.LogStageIf(backendTrace, "startup.skills.ready")
	pluginManager, pluginStartup, err := InitPluginSystem(runtime, skillRegistry, flags.PluginsPaths)
	if err != nil {
		return nil, boot.fail(err)
	}
	console.LogStageIf(backendTrace, "startup.plugins.ready")
	st, err := InitStore(flags.StorePath)
	if err != nil {
		return nil, boot.fail(err)
	}
	boot.addCleanup(func() { _ = st.Close() })
	console.LogStageIf(backendTrace, "startup.store.ready")
	runtime.AttachHistoryRouter(st)
	// 子代理会话记录持久化（运行期落盘 + 结束时结论归主会话 + 记录删除）
	// 与 plan checkpoint 持久化（RunPlan 落最终快照，ResumePlan 续跑）。
	runtime.AttachSubSessionStore(store.NewNodeSessionStore(st))
	runtime.SetPlanCheckpointStore(store.NewCheckpointStore(st, st.Workspace()))
	events := wiring.NewEventHub()
	approval := wiring.NewApprovalBroker(events)
	// 双轨事件（slice 8）：执行事实 → store 事件库（事实轨），
	// EventHub 继续前端快照（快照轨）。Sink 失败经 ErrorHandler 隔离，
	// 不破坏 WorkPlan 控制流（见 Seele event/README.md）。
	eventStore := store.NewEventStore(st)
	if err := SetupPermissionGate(runtime, approval, flags.PermissionMode); err != nil {
		return nil, boot.fail(fmt.Errorf("权限模式无效: %w", err))
	}
	console.LogStageIf(backendTrace, "startup.permissions.ready")
	toolHooks := wiring.NewToolHookBridge()
	if backendTrace != nil {
		toolHooks.SetDiagnosticObserver(backendTrace.LogToolHookEvent)
	}
	appEngine := adapters.NewEnginePort(nil, func(sessionID string) adapters.ReactorEngine {
		fresh, createErr := InitEngine(runtime, toolHooks, sessionID)
		if createErr != nil {
			return nil
		}
		return fresh
	}, runtime.Tracer())
	appEngine.EnableWorkingHistoryRelease()
	// The framework Session is intentionally created only by StartSession or
	// ReplaceHistory during resume; startup itself remains a cold draft.
	registry.RegisterProductTools(runtime, pluginManager, appEngine, approval, skillRegistry)
	if err := ActivateDefaultPlugin(pluginManager, nil); err != nil {
		return nil, boot.fail(err)
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
		SubagentLivePage:  runtime.SubagentLiveHistoryPage,
		NodeStageLogs:     runtime.NodeStageLogs,
		PrepareHistory: func(sessionID string, messages []types.Message) {
			runtime.PrepareMainSessionHistory(sessionID, messages)
		},
		// 会话释放面（2026-10-14）：LRU 驱逐/卸载/归档/删除时把桥侧会话本体
		// （框架 Session + 上下文存储 + 主历史）一起放掉——只丢引擎端口引用
		// 等于把"驱逐"做成空操作，见 docs/2026-10-14-memory-cpu-accumulation/。
		ReleaseSessionBundle: runtime.ReleaseSessionBundle,
	})
	sessionManager := InitSessionManager(st, appEngine)
	wsRepo, err := InitWorkspaceRepo(flags.StorePath)
	if err != nil {
		return nil, boot.fail(err)
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
	if repo, ok := st.TeamworkFor(); ok {
		// 看板存档面（会话粒度元数据 + 重启快照恢复）：与计划/审计同一个 JSON 后端，
		// 落 metadata/board_team.json。非 JSON 后端不实现它 → nil，读侧无从恢复，
		// 活体投影照常工作。
		boardRepo, _ := st.BoardsFor()
		// 作用域键解析只有一份（sessionKeyFor，见文件尾部）：团队作业与工具记录
		// 都用它，免得两处各写一遍"会话 → (项目, 会话)"的解析。
		keyFor := func(sessionID string) (store.Key, bool) { return sessionKeyFor(wsRepo, sessionID) }
		if err := runtime.SetTeamworkBackend(assemble.TeamworkBackend{
			Store:  seeteamwork.NewPlanStore(repo),
			Boards: boardRepo,
			// 作业输出归产品（S5）：正文落会话的 teamwork/jobs，销项 / 驱逐 / Close
			// 都不由框架删，整队收口（team_close）时才清——"正文活到 close"。
			JobOutputs:   seeteamwork.NewOutputStore(repo, keyFor),
			KeyFor:       keyFor,
			MaxTeammates: runtimeLimits.Team.MaxTeammates,
		}); err != nil {
			return nil, boot.fail(fmt.Errorf("装配 teamwork 编排面失败: %w", err))
		}
	}
	app, err := InitApplication(appEngine, runtime, pluginManager, sessionManager, skillRegistry, wsRepo, events, approval, flags.StorePath)
	if err != nil {
		return nil, boot.fail(err)
	}
	boot.addCleanup(app.Shutdown)
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
	// 日志（wiring/config 的 LogPluginRoots），GUI 里根本看不到——所以"广播"那个词名不副实（它既不是
	// 广播，也没有第二个读者）。现在终端与 UI 两处写的是**同一条读数**（config.PluginRootReport
	// 生成一次，两处引用同一份字节）。
	if pluginStartup.RootReading != "" {
		app.AddNotice("插件: " + pluginStartup.RootReading)
	}
	if pluginStartup.Curated != "" {
		app.AddNotice("⚠ 启动配置警告: " + pluginStartup.Curated)
	}
	registry.RegisterTaskTerminalTools(runtime, app)
	registry.RegisterGoalTools(runtime, app)
	// **J-1a 之后这里不再注入两条"角色自带"读面**（2026-10-12，Agent Team 用户面
	// 退场）：
	//
	//   - `SetRolePluginsProvider`（RoleSpec.Plugins 的"角色自带插件装配"）：它的
	//     唯一事实来源是团队角色注册表，注册表随本条目删除 ⇒ 读面没有来源。运行时
	//     对"读面未注入"的口径就是**空集 = 不覆盖**（backend/engine/assemble/runtime_role_plugins.go
	//     的 `rolePluginsFor`），因此退场后的行为与"注册表里没登记"逐字相同；能力轴
	//     仍在：`team_plan members[].plugins` 的显式声明照旧生效（那条面在
	//     backend/multiagent/teamwork，未动）。
	//   - `SetRolePromptProvider`（已登记员工提示词 → 角色回合）：同上，读面未注入
	//     时角色回合回退内置角色设定（backend/engine/assemble/runtime_role_prompt.go）。
	//
	// 两条注入点保留在 seelebridge 侧（它们是运行时的公开读面，不属于本条目要删的
	// Agent Team 用户面），只是不再有装配根来源。
	//
	// **权限门的"员工权责反查"接线也随本条目退场**（2026-10-12）：`SetRoleSessionPolicyResolver`
	// （角色会话 → ToolsPolicy）与 `SetRoleSessionOwnerResolver`（角色会话 → 宿主主会话）
	// 的对象 `app.RoleSessionToolsPolicy` / `app.RoleSessionOwner` 只有一个事实来源——
	// `application/core/agentteam_role_index.go` 的反向索引，而它的**唯一登记点**是
	// 团队注册表的读面（`agentTeamRawView`）。注册表随 Agent Team 用户面删除 ⇒ 索引
	// 恒空 ⇒ 两条 resolver 退场前后的返回值**逐字相同**（恒 `("", false)`，权限门按
	// "不是角色会话"回退）。因此这里不再接线，而不是留一个恒 false 的闭包。
	//
	// 员工权限的**权威判定**在"按构造"那条路（角色回合起手用
	// `backend/tool/registry.WithEmployeeGrant` / `WithEmployeeSubjectClass` 把员工主体放进
	// ctx），那条路不依赖反查索引，本条目未改 seelebridge（见
	// docs/arch/agent-permission-subjects.md）。`seelebridge` 侧的
	// `SetRoleSessionPolicyResolver` / `SetRoleSessionOwnerResolver` 仍在（运行时公开
	// 读面，测试与 headless 宿主可用），只是不再有装配根来源。
	registry.RegisterSkillActivateTool(runtime, skillRegistry, func(name string) (skill.Skill, error) {
		info, err := app.ActivateSkill(name)
		if err != nil {
			return skill.Skill{}, err
		}
		return skill.Skill{Name: info.Name, Description: info.Description, Prompt: info.Prompt}, nil
	})
	registry.RegisterContextReadTools(runtime, app)
	// J-2：agent 请求 GUI 打开待审阅文件（一次批处理召唤，上限 10 份）。
	registry.RegisterReviewFilesTool(runtime, app)
	registry.RegisterProjectRefreshTool(runtime, st, app)
	registry.RegisterScheduledTaskCapability(runtime)
	toolHooks.Bind(app)
	// 项目看板（可选能力，M3 装配门）：装配期**一次**交接——开关关 ⇒ 不注入订阅口
	// （工具完成段判空后什么都不做，一个文件都不写）；开 ⇒ 唯一写盘路径挂上。
	// 关就是关的口径见 docs/arch/seelex-project-board.md §5.3。
	projectBoardSink, closeProjectBoard, err := registry.ProjectBoardSinkFor(
		runtimeLimits.ProjectBoard, st,
		app.WorkTableRowsFor, app.ProjectBoardTeamPlanFor,
		app.ProjectBoardTeamStateFor, app.ProjectRootFor,
	)
	if err != nil {
		return nil, boot.fail(err)
	}
	app.SetProjectBoardSink(projectBoardSink)
	boot.addCleanup(closeProjectBoard)
	// 工具记录（成功/失败计数 + 失败原因）：读数**按会话**存在会话自己的存储里，落盘是
	// **批次**（真相源提交时刻统一更新一次，见 core.FlushToolRecord）——不做"每次工具调用
	// 写一次盘"（回合内高频调用会把磁盘写忙，2026-10-15 用户口径）。
	app.SetToolRecordSnapshotStore(sessionToolRecordStore{
		router:     st,
		resolveKey: func(sessionID string) (store.Key, bool) { return sessionKeyFor(wsRepo, sessionID) },
	})
	// 计数开关：默认**开**（先收数据），界面上改过的值落在数据根 runtime-limits.json 的
	// tool_record_enabled 格（指针：nil = 没改过 ⇒ 用默认）。
	// 开关的权威值在数据根覆盖层（同一份 runtime-limits.json：设置里拨过就写它）。
	// 这里自己读一次而不是复用 initRuntime 里的那份：两处装配点不同（这个在更早的
	// 前端装配段），读一次文件的代价换来"装配顺序不构成隐式依赖"。
	toolRecordOverlay := overlay.Read(config.RuntimeLimitsPath(flags.StorePath))
	toolRecordEnabled := true
	if toolRecordOverlay.ToolRecordEnabled != nil {
		toolRecordEnabled = *toolRecordOverlay.ToolRecordEnabled
	}
	// 装配期只装内存（值本来就从覆盖层来）：用户拨开关才走 SetToolRecordEnabled 的
	// "先落盘、再改内存"。这样覆盖层文件只在用户真改过之后才出现这一格。
	app.ApplyToolRecordEnabled(toolRecordEnabled)
	// 会话删除时同批回收项目目录里的投影：项目根只有装配根知道（存储层不认识工作区），
	// 所以在这里把解析器交给 Router 一次（见 store.SetProjectBoardRootFor）。
	st.SetProjectBoardRootFor(func(sessionID string) string {
		if workspace, ok := wsRepo.SessionWorkspace(sessionID); ok {
			return strings.TrimSpace(workspace.RootPath)
		}
		return ""
	})
	// 读取面：**只在这一档注册**（关 ⇒ 工具根本不存在 = 零读取面，而不是"注册了但报错"）。
	if runtimeLimits.ProjectBoard.Enabled {
		registry.RegisterProjectBoardReadTool(runtime, st, app)
	}
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
	var bashObserver toolreg.BashDiagnosticObserver
	if backendTrace != nil {
		bashObserver = backendTrace.LogBashEvent
	}
	// 命令面的成败也走这条观察口：工具层已经在 `bash.process.exited` 上报了每一次同步
	// 命令的退出码（那条 stage 只由同步 bash 链发出；bash_bg 走另一条链、不经这里），
	// 命令面因此不需要第二个上报口。"退出码非零 = 一次命令失败"的判据在 core
	// （RecordToolCommandOutcome），这里只做搬运；诊断日志照旧转发给下游。
	bashObserver = toolCommandOutcomeObserver(bashObserver, app)
	runtime.ApplyDeps(assemble.RuntimeDeps{
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
		ProjectKnowledge: func() *store.ProjectRecord {
			record, readErr := st.LoadProjectRecord(st.Workspace())
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
		// 定时提示词任务的执行器：默认**新建会话**发起（可选装配工作区），
		// 显式绑定了会话的任务才投递到那个会话。
		// 单独一个函数是为了让测试基座（tool_full_chain_test.go 的 newFullChainHarness）
		// 装的是同一份口径，而不是各写一份"看起来差不多"的闭包。
		ScheduledPromptExecutor: registry.ScheduledPromptExecutor(app),
		SchedulerObserver:       app.RefreshRuntimeSnapshot,
	})
	// 定时任务定义恢复（全局 JSONL → 内存排期）：必须在**执行器与观察者注入之后**，
	// 恢复出来的任务才可能立刻按排期触发。副作用只有"多出几条已排期任务"与一次
	// runtime.changed 投影发布，不触碰任何会话记录。
	if restored, skipped, restoreErr := runtime.RestoreScheduledTasks(); restoreErr != nil {
		log.Printf("定时任务恢复失败（任务未排期）：%v", restoreErr)
	} else if restored > 0 || skipped > 0 {
		log.Printf("定时任务恢复：%d 条已排期，%d 条跳过（已过期/已失效）", restored, skipped)
	}
	if frontend == "backend" && strings.TrimSpace(flags.BackendProject) != "" {
		if err := console.BindProject(app, flags.BackendProject); err != nil {
			return nil, boot.fail(err)
		}
		console.LogStageIf(backendTrace, "startup.workspace.ready")
	}
	console.LogStageIf(backendTrace, "startup.frontend.ready")
	boot.Application = app
	boot.Runtime = runtime
	boot.Store = st
	boot.StartupWarning = strings.Join(startupWarnings, "\n")
	return boot, nil
}
