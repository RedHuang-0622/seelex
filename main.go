// Seelex assembles the Seele agent framework with product-level plugins,
// skills, session storage, and the terminal UI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/RedHuang-0622/seelex/backend/limit/policy"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	frameworkSession "github.com/RedHuang-0622/Seele/session"
	toolspermission "github.com/RedHuang-0622/Seele/tools/permission"
	tea "github.com/charmbracelet/bubbletea"
	"gopkg.in/yaml.v3"

	"github.com/RedHuang-0622/Seele/types"
	"github.com/RedHuang-0622/seelex/application"
	"github.com/RedHuang-0622/seelex/application/console"
	"github.com/RedHuang-0622/seelex/application/core"
	"github.com/RedHuang-0622/seelex/application/core/session_runtime"
	coretask "github.com/RedHuang-0622/seelex/application/core/task_context"
	seelebridge "github.com/RedHuang-0622/seelex/backend/engine/assemble"
	"github.com/RedHuang-0622/seelex/backend/limit"
	"github.com/RedHuang-0622/seelex/backend/limit/overlay"
	seeteamwork "github.com/RedHuang-0622/seelex/backend/multiagent/teamwork"
	toolreg "github.com/RedHuang-0622/seelex/backend/tool/registry"
	"github.com/RedHuang-0622/seelex/backend/tool/search"
	"github.com/RedHuang-0622/seelex/contract/dto"
	"github.com/RedHuang-0622/seelex/gui"
	"github.com/RedHuang-0622/seelex/internal/adapters"
	"github.com/RedHuang-0622/seelex/internal/bootseed"
	"github.com/RedHuang-0622/seelex/internal/buildinfo"
	"github.com/RedHuang-0622/seelex/internal/promptassets"
	"github.com/RedHuang-0622/seelex/plugin"
	"github.com/RedHuang-0622/seelex/session"
	"github.com/RedHuang-0622/seelex/skill"
	"github.com/RedHuang-0622/seelex/store"
	"github.com/RedHuang-0622/seelex/store/workspace"
	"github.com/RedHuang-0622/seelex/tui"
	"github.com/RedHuang-0622/seelex/wiring"
	"github.com/RedHuang-0622/seelex/wiring/config"
	"github.com/RedHuang-0622/seelex/wiring/registry"
)

// launchFlags 是本入口的命令行参数：flag 名、默认值与说明文案的权威定义在
// wiring/config（启动项可复用的关键——第二个入口只需 BindFlags 一次）。包级绑定
// 保持「启动参数在任何装配之前就位」的既有语义。
//
// 版本号与默认前端不在这里：它们是构建期 ldflags 注入点，权威值在 internal/buildinfo。
var launchFlags = config.BindFlags(flag.CommandLine)

// runtimeLimits 是 initRuntime 加载的 limits（后续初始化消费）。
var runtimeLimits limit.Limits

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
	if launchFlags.ShowVersion {
		fmt.Println(buildinfo.Version)
		return nil
	}
	frontend, err := config.ParseFrontendMode(launchFlags.FrontendMode)
	if err != nil {
		return fmt.Errorf("前端模式无效: %w", err)
	}
	launchFlags.FrontendMode = frontend
	var backendOutput io.Writer
	var backendTrace *console.EventLogger
	if frontend == "backend" {
		output, closeOutput, outputErr := console.OpenOutput(launchFlags.BackendLogPath)
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
	mode, err := config.ParsePermissionMode(launchFlags.PermissionMode)
	if err != nil {
		return fmt.Errorf("权限模式无效: %w", err)
	}
	launchFlags.PermissionMode = mode
	launchFlags.StorePath = config.ResolveStorePath(launchFlags.StorePath)

	// 提示词资产目录（config/prompt）：先按责任链定目录（候选链全缺则用内嵌
	// 默认词初始化），再让 promptassets 按"外部优先、内嵌逐文件兜底"解析。
	// 必须早于任何提示词消费——装配期的 promptassets.Validate 与运行期的
	// system prompt 组装都读它。
	promptassets.SetDir(config.ResolvePromptDir())

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
	st, err := initStore()
	if err != nil {
		return err
	}
	defer st.Close()
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
	if err := setupPermissionGate(runtime, approval); err != nil {
		return fmt.Errorf("权限模式无效: %w", err)
	}
	console.LogStageIf(backendTrace, "startup.permissions.ready")
	toolHooks := wiring.NewToolHookBridge()
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
	registry.RegisterProductTools(runtime, pluginManager, appEngine, approval, skillRegistry)
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
	sessionManager := initSessionManager(st, appEngine)
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
	if repo, ok := st.TeamworkFor(); ok {
		// 看板存档面（会话粒度元数据 + 重启快照恢复）：与计划/审计同一个 JSON 后端，
		// 落 metadata/board_team.json。非 JSON 后端不实现它 → nil，读侧无从恢复，
		// 活体投影照常工作。
		boardRepo, _ := st.BoardsFor()
		// 作用域键解析只有一份（sessionKeyFor，见文件尾部）：团队作业与工具记录
		// 都用它，免得两处各写一遍"会话 → (项目, 会话)"的解析。
		keyFor := func(sessionID string) (store.Key, bool) { return sessionKeyFor(wsRepo, sessionID) }
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
	// P1：真实 TL 评估器装配（seelebridge 账号 completer → goal 域
	// TLEvaluator）；注入发生在首次会话启动前，Supervisor 首次 bind 即启用。
	//
	// **J-1a 之后这里不再注入两条"角色自带"读面**（2026-10-12，Agent Team 用户面
	// 退场）：
	//
	//   - `SetRolePluginsProvider`（RoleSpec.Plugins 的"角色自带插件装配"）：它的
	//     唯一事实来源是团队角色注册表，注册表随本条目删除 ⇒ 读面没有来源。运行时
	//     对"读面未注入"的口径就是**空集 = 不覆盖**（backend/engine/assemble/runtime_role_plugins.go
	//     的 `rolePluginsFor`），因此退场后的行为与"注册表里没登记"逐字相同；能力轴
	//     仍在：`team_plan members[].plugins` 的显式声明照旧生效（那条面在
	//     backend/multiagent/teamwork，未动）。
	//   - `SetRolePromptProvider`（已登记员工提示词 → ADVISOR 回合）：同上，读面未注入
	//     时 ADVISOR 回退内置角色设定（backend/engine/assemble/runtime_role_prompt.go），输出契约
	//     不受影响。
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
	if tlEvaluator := runtime.GoalTLEvaluator(); tlEvaluator != nil {
		app.SetGoalTLEvaluator(tlEvaluator)
	}
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
		return err
	}
	app.SetProjectBoardSink(projectBoardSink)
	defer closeProjectBoard()
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
	toolRecordOverlay := overlay.Read(config.RuntimeLimitsPath(launchFlags.StorePath))
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
	var bashObserver seelebridge.BashDiagnosticObserver
	if backendTrace != nil {
		bashObserver = backendTrace.LogBashEvent
	}
	// 命令面的成败也走这条观察口：工具层已经在 `bash.process.exited` 上报了每一次同步
	// 命令的退出码（那条 stage 只由同步 bash 链发出；bash_bg 走另一条链、不经这里），
	// 命令面因此不需要第二个上报口。"退出码非零 = 一次命令失败"的判据在 core
	// （RecordToolCommandOutcome），这里只做搬运；诊断日志照旧转发给下游。
	bashObserver = toolCommandOutcomeObserver(bashObserver, app)
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
	if frontend == "backend" && strings.TrimSpace(launchFlags.BackendProject) != "" {
		if err := console.BindProject(app, launchFlags.BackendProject); err != nil {
			return err
		}
		console.LogStageIf(backendTrace, "startup.workspace.ready")
	}
	console.LogStageIf(backendTrace, "startup.frontend.ready")
	return startFrontend(app, backendOutput, strings.Join(startupWarnings, "\n"))
}

func initRuntime() (*seelebridge.Runtime, error) {
	// 运行参数在 config/seelex.yaml（配置参数文件；权限在 config/seele.yaml）：
	// 责任链 = CWD 的 config/ → CWD 根目录 → 二进制目录的 config/（见
	// config.RuntimeConfigChain）。命中就按它读（存在即读）；全都没有 → 用内嵌默认档在
	// 二进制旁边初始化，再读它（internal/bootseed）。滑动窗口段缺失 → 零值走默认；
	// limits 缺失字段 → 默认值。
	runtimeConfigPath := config.EnsureConfigFile(bootseed.RuntimeConfigName, bootseed.RuntimeConfigPack())
	windowConfig, err := core.LoadWindowConfig(runtimeConfigPath)
	if err != nil {
		return nil, fmt.Errorf("加载 window 配置失败: %w", err)
	}
	limits, err := limit.LoadLimits(runtimeConfigPath)
	if err != nil {
		return nil, fmt.Errorf("加载 limits 配置失败: %w", err)
	}
	limits = limits.WithDefaults()
	// 界面上的覆盖层优先于配置：用户在设置弹窗里改过的运行时参数落盘在数据根，
	// 启动时叠在 config/seelex.yaml 的 limits 段之上（内置默认 ← 配置 ← 覆盖层），
	// 见 backend/limit/overlay（M2-C2 归位前在仓库根的 runtime_limits_overlay.go）。
	limitsOverlay := overlay.Read(config.RuntimeLimitsPath(launchFlags.StorePath))
	limits = overlay.Apply(limits, limitsOverlay)
	runtimeLimits = limits // initStore/initEngine 等后续初始化消费
	toolCallTimeout, _, planDecision, heartbeat, replanWindow, searchTimeout := limits.Durations()
	core.ApplyLimits(limits)
	// window 段同时注入应用侧压缩保留窗口决策（压缩前缀 = min(retain_tokens,
	// ratio × 全量上下文)，硬压缩阈值 force_compact_tokens）。
	core.ApplyWindowConfig(windowConfig)
	search.ApplyLimits(int(searchTimeout / time.Second))
	runtime, err := seelebridge.NewRuntime(seelebridge.RuntimeConfig{
		AccountsPath: config.AccountsPath(), StorePath: launchFlags.StorePath,
		// 定时任务定义落**全局** JSONL：与 workspace_index.json 同级（store 的
		// 基目录），不按项目分区、不按会话分片。会话记录仍走 store。
		ScheduledTasksPath:        filepath.Join(filepath.Dir(launchFlags.StorePath), "scheduled-tasks.jsonl"),
		ToolCallTimeout:           toolCallTimeout,
		PlanDecisionTimeout:       planDecision,
		HeartbeatInterval:         heartbeat,
		ReplanWindow:              replanWindow,
		MaxConcurrentReplans:      limits.MaxConcurrentReplans,
		MaxReplansPerWindow:       limits.MaxReplansPerWindow,
		MaxReplanProviderRequests: limits.MaxReplanProviderReqs,
		WindowConfig:              windowConfig,
		Limits:                    limits,
		// 界面上按账号设过的窗口（数据根覆盖层的 account_windows 表）：优先级在
		// Runtime.ContextWindowFor 一处判定（账号级 > limits.context_window > 账号档）。
		AccountWindows: limitsOverlay.AccountWindows,
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

// pluginStartup 是一次插件系统启动的读数（终端日志与 UI 面**同源**，见 wiring/config 的
// PluginRootReport）：
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
	// > plugins(CWD)），见 config.PluginRootChain；加载器是多根 first-wins。
	roots := config.PluginRoots(launchFlags.PluginsPaths)
	loader := plugin.NewLoader(roots...)
	manager := plugin.NewManager(loader, runtime, runtime, skills)
	if err := manager.Load(); err != nil {
		return nil, pluginStartup{}, fmt.Errorf("加载 Plugin 失败: %w", err)
	}
	loaded := manager.All()
	if err := config.RequirePlugins(roots, loaded); err != nil {
		return nil, pluginStartup{}, err
	}
	startup := pluginStartup{RootReading: config.LogPluginRoots(roots, loaded)}
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
	curated := config.ResolveCuratedRead(roots, loaded)
	// 来源读数（「这个插件从哪来」）的判定源挂到 manager 上：运行期视图经 adapters 的
	// PluginPort 读它，把 source_kind/source_url 补进每个插件——判定与文案全在
	// plugin.CuratedCatalog.ReadSource，前端不复制判定、组合根也不复制。
	// 目录缺席/损坏时传的是零值 ⇒ 来源面留空（缺失来源不编默认值）。
	manager.SetCuratedCatalog(curated.Catalog)
	runtime.SetPluginUnassembledReason(config.CuratedAssemblyJudge(curated))
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
	if _, err := registry.PluginByName(manager.All(), "default"); err != nil {
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
	// registry.ApplyPluginPrompt(eng, manager)
	return nil
}

func guardMultiProcess() error {
	if runtimeLimits.Runtime.AllowMultiProcess {
		return nil
	}
	dataRoot := filepath.Dir(launchFlags.StorePath)
	staleAfter := time.Duration(runtimeLimits.SessionStorage.LockStaleAfterSeconds) * time.Second
	if owner, held := store.DataRootLockedByWith(dataRoot, staleAfter); held {
		return fmt.Errorf(
			"数据根 %s 正被另一个 Seelex 实例独占（pid=%d program=%s host=%s）：默认单实例。"+
				"要允许并行启动，在 config/seelex.yaml 设 limits.runtime.allow_multi_process: true"+
				"（代价：多个进程共用同一数据根，失去跨进程写者串行化，一致性由使用者负责）",
			dataRoot, owner.PID, owner.Program, owner.Hostname)
	}
	return nil
}

func initStore() (*store.Router, error) {
	// NestedSessionStore 的 baseDir 与 workspace_index.json 同级
	baseDir := filepath.Dir(launchFlags.StorePath)
	store.ApplyLimits(runtimeLimits.SummaryChars)
	router, err := store.NewRouter(
		filepath.Join(baseDir, "session-storage.json"), baseDir, sessionStorageLimits())
	if err != nil {
		return nil, fmt.Errorf("初始化嵌套存储失败: %w", err)
	}
	return router, nil
}

// sessionStorageLimits 把 seele.yaml limits.session_storage 映射成存储覆盖层
// （my_design §11 覆盖链的中间一层：默认值 ← limits ← session-storage.json）。
// 分片行数沿用既有顶层键 limits.message_shard_size。
func sessionStorageLimits() store.Settings {
	limits := runtimeLimits.SessionStorage
	// 指针：把 limits.runtime.allow_multi_process 原样传进 §9 数据根锁（存储侧默认
	// 也是 false，这里显式带过去，让配置一眼看得出生效）。
	allowMultiProcess := runtimeLimits.Runtime.AllowMultiProcess
	return store.Settings{
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
	baseDir := filepath.Dir(launchFlags.StorePath)
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

// 编译期钉子（落盘口的结构满足）在 store 那一侧：`var _ ToolRecordRepository =
// (*jsonRepository)(nil)`——具体类型不导出，装配根写不出这条断言。

// toolCommandOutcomeObserver 把命令面的退出码折成一次工具记录（成功/失败），再把事件
// 原样转发给下游观察者（诊断日志）。
//
// 为什么挂在既有的诊断口上：工具层已经在 `bash.process.exited` 里上报了每一次同步命令的
// 退出码（同步主路径与 docker 重试同源于 executeScopedBash），命令面因此不必新增第二个
// 上报口；"退出码非零 = 失败"这一格判据落在 core 的 `RecordToolCommandOutcome`，
// 本函数只做搬运与转发。
//
// 边界：这条 stage 目前**只由同步 `bash` 链发出**（bash_bg 走 async_run 的另一条链，
// 不经 executeScopedBash）。将来若有第二个工具复用那条同步链，必须先把工具名带进事件
// （BashDiagnosticEvent.Tool），否则命令结果会被记到 bash 名下。
func toolCommandOutcomeObserver(next seelebridge.BashDiagnosticObserver, app *application.Service) seelebridge.BashDiagnosticObserver {
	return func(event seelebridge.BashDiagnosticEvent) {
		if event.Stage == "bash.process.exited" {
			// 会话号由工具层带在事件上（BashDiagnosticEvent.Session，只填 ID 不带正文）：
			// 后台会话/teammate 跑的命令因此记在**它自己的**会话读数里，不会被归到视图会话。
			app.RecordToolCommandOutcome(event.Session, "bash", event.ExitCode)
		}
		if next != nil {
			next(event)
		}
	}
}

// sessionKeyFor 把会话 ID 解析成存储作用域键（项目 + 会话）：**按会话自己的项目绑定**解析，
// 未绑定会话返回 false（不落到别人的项目目录里）。装配期的三处（teamwork backend、
// 工具记录）共用这一份解析，不各写一遍。
func sessionKeyFor(workspaces *workspace.Repo, sessionID string) (store.Key, bool) {
	if workspaces == nil {
		return store.Key{}, false
	}
	bound, ok := workspaces.SessionWorkspace(sessionID)
	if !ok || strings.TrimSpace(bound.ID) == "" {
		return store.Key{}, false
	}
	return store.Key{ProjectID: bound.ID, SessionID: sessionID}, true
}

// sessionToolRecordStore 是 contract.ToolRecordSnapshotStore 在本宿主上的实现：
// 把读数写进**会话自己的**存储（`<sessionRoot>/tool-record.jsonl`，一行一批整份快照）。
//
// 未装配 JSON 后端（或键解析不出来）⇒ 空操作不报错：读数是派生读数，落不下去不该
// 让会话提交失败（错误由调用方按 LastError 的同一口径处理，这里选择"静默跳过"是因为
// 键解析失败通常意味着会话还没绑项目——那时也还没有可写的位置）。
type sessionToolRecordStore struct {
	router     *store.Router
	resolveKey func(sessionID string) (store.Key, bool)
}

func (st sessionToolRecordStore) SaveToolRecordSnapshot(sessionID string, snapshot application.ToolRecordSnapshot) error {
	repository, ok := st.repository()
	if !ok {
		return nil
	}
	key, ok := st.key(sessionID)
	if !ok {
		return nil
	}
	return repository.AppendToolRecordSnapshot(context.Background(), key, snapshot)
}

func (st sessionToolRecordStore) LoadToolRecordSnapshot(sessionID string) (application.ToolRecordSnapshot, bool, error) {
	repository, ok := st.repository()
	if !ok {
		return application.ToolRecordSnapshot{}, false, nil
	}
	key, ok := st.key(sessionID)
	if !ok {
		return application.ToolRecordSnapshot{}, false, nil
	}
	raw, found, err := repository.ReadToolRecordSnapshot(context.Background(), key)
	if err != nil || !found {
		return application.ToolRecordSnapshot{}, false, err
	}
	var snapshot application.ToolRecordSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return application.ToolRecordSnapshot{}, false, nil // 坏行 = 当没记过（不阻塞读面）
	}
	return snapshot, true, nil
}

func (st sessionToolRecordStore) repository() (store.ToolRecordRepository, bool) {
	if st.router == nil {
		return nil, false
	}
	return st.router.ToolRecordFor()
}

func (st sessionToolRecordStore) key(sessionID string) (store.Key, bool) {
	if st.resolveKey == nil {
		return store.Key{}, false
	}
	return st.resolveKey(sessionID)
}

func initSessionManager(router *store.Router, eng *adapters.EnginePort) *session.Manager {
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
	sessionPort.Meta = store.NewSessionMetaStore(sessions.Router())
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
	return wiring.New(application.Dependencies{
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
		// 会话驻留窗口的落盘口：设置弹窗改的值写进数据根 runtime-limits.json，
		// 下次启动由 applyRuntimeLimitsOverlay 叠回 limits 之上。
		ReleaseWindowStore: overlay.StoreFile{Path: config.RuntimeLimitsPath(launchFlags.StorePath)},
		// 每账号上下文窗口的落盘口：同一个覆盖层文件的 account_windows 表
		// （设置 → 账号的挡位/自定义值）。
		AccountWindowStore: overlay.StoreFile{Path: config.RuntimeLimitsPath(launchFlags.StorePath)},
		// 工具记录计数开关的落盘口：同一个覆盖层文件的 tool_record_enabled 格。
		ToolRecordStore: overlay.StoreFile{Path: config.RuntimeLimitsPath(launchFlags.StorePath)},
		// 系统提示词的**环境段**：装配期解析一次"这台机器上 bash 工具实际落到哪个壳"，
		// 写进提示词（模型写命令前唯一的方言事实；2026-10-15 审计 D4）。解析结果进程内
		// 缓存，因此前缀逐轮稳定。
		EnvironmentSection: toolreg.ResolveShellFact().PromptSection(),
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
	switch launchFlags.FrontendMode {
	case "gui":
		if err := gui.Run(app, gui.Options{
			Title: "Seelex", Version: buildinfo.Version, StartupWarning: startupWarning,
			// 外观设置（皮肤 → 窗口材质）的落盘路径：建窗期读它决定窗口要不要走
			// 透明/亚克力（毛玻璃皮肤），换肤期由 Bridge.SetWindowAppearance 写它。
			AppearancePath: config.GUIAppearancePath(),
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
		return console.Start(app, launchFlags.BackendPrompt, launchFlags.BackendTimeout, backendOutput)
	default:
		return startTUI(initTUI(app))
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
	tier, err := config.ParsePermissionMode(launchFlags.PermissionMode)
	if err != nil {
		return err
	}
	// 免打断的读命令白名单按**装配期解析出的实际壳**分派（2026-10-15 审计 D4）：
	// 白名单是按命令文本匹配的，而同一个字符串在 POSIX 壳与 PowerShell 下是两回事——
	// 写死 POSIX 形态会让 PowerShell 宿主上的命令"过闸即死"（放行 → 执行必失败）。
	cfg := policy.PermissionConfigForShell(toolreg.ResolveShellFact().Kind)
	fileCfg, err := loadPermissionConfig(config.EnsureConfigFile(bootseed.PermissionConfigName, bootseed.PermissionConfigPack()))
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
