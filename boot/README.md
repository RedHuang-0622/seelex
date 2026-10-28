# Boot：进程组合根

## 生态位

`boot` 是 Seelex 唯一的进程级组合根。它把已解析的启动参数、构建期版本、运行时、
存储、Application 组件、工具注册和前端宿主装配成一台可运行实例；主要调用方是
根目录 `main.go`，集成测试通过同一入口验证真实装配。

## 职责与非职责

职责：

- 固定装配顺序，并把关机动作登记为逆序清理栈。
- 构造 Seelex Runtime、Store、Workspace、Application 与前端依赖。
- 注册产品工具、权限门、Plugin/Skill/MCP、项目看板、定时任务等宿主能力。
- 把启动警告与插件根读数送入用户可见面。
- 选择并启动 `tui`、`gui`、`headless` 或 `backend` 前端。

非职责：

- 不解析 flag、不定义默认参数、不解析配置责任链；这些归 `internal/startup`。
- 不实现会话、聊天、上下文、工具、权限或存储业务规则。
- 不渲染 UI；前端只消费 Application owner 的窄依赖。
- 不持有可在多入口复制的第二份装配流程。

## 文件结构

| 文件 | 职责 |
|---|---|
| `app.go` | `Options`、`App`、`Run`、`Serve`、逆序清理与失败收尾。 |
| `runtime.go` | Engine/Runtime、Store、Workspace、会话 key 与运行参数装配。 |
| `application.go` | `SessionManager` 与 Application assembly 的构造。 |
| `service_ports.go` | 把 Runtime 的窄端口交给 Application。 |
| `engine_plugins.go` | Skill/Plugin 系统初始化与默认插件激活。 |
| `permission.go` | 权限档位、规则合并、配置装载与审批桥。 |
| `tools.go` | 产品工具与宿主能力注册。 |
| `frontend*.go` | 前端模式分派及各宿主依赖装配。 |

编译标签只决定某个前端实现是否可用；启动语义仍以本包唯一装配流程为准。

## 核心实现

- `Options` 只携带 `*startup.Flags` 与构建期版本。
- `Run` 依次完成模式校验、配置/提示词目录解析、Runtime 与数据根闸门、Store、
  权限、工具、Plugin、Application、派生投影和前端依赖装配。
- `App.addCleanup` 记录清理动作；`Shutdown` 按逆序执行且幂等。
- 启动中任一阶段失败都经 `App.fail` 回收已装配资源，不把半初始化实例交给入口。
- `Serve` 只做前端分派；进程生命周期不写进各前端。

## 数据流与生命周期

```text
main.go
  -> internal/startup.BindFlags / flag.Parse
  -> boot.Run(Options)
     -> startup mode + path chain
     -> Runtime / data-root guard / Store / Workspace
     -> permission / tools / plugins / Application
     -> immutable projections + frontend dependencies
  -> App.Serve()
  -> defer App.Shutdown()（逆序、幂等）
```

装配顺序是行为：例如提示词目录必须先于提示词消费者，Store 必须先于会话持久化，
权限门必须先于工具暴露，Plugin 激活必须先于技能可见性读取。新增步骤必须在
`Run` 的明确位置解释顺序理由，并登记需要的清理动作。

## 依赖方向

允许依赖：

- `internal/startup` 的启动参数、路径和插件根解析。
- `application/**`、`internal/host`、`backend/**`、`kernel/**`、`store/**`、
  `contract/**`、`console` 与四种前端包。

禁止：

- 被 Application、Backend、Kernel、Store 反向 import。
- 在业务域内复制 flag 名、路径责任链或工具注册表。
- 按前端模式在业务实现内部增加分支；差异只放在本装配层。

## 并发、存储与错误语义

- `Shutdown` 必须可重复调用；清理错误不得阻止后续清理。
- 数据根只允许本进程作为默认写者；启动闸门和 `store/root` 锁是两层防线。
- 装配失败要先回收已获得资源，错误返回入口，不能 `os.Exit`。
- 后台 goroutine、MCP、数据库或文件句柄必须在 `App` 关闭路径中可终止。

## 扩展方式

1. 新启动参数放 `internal/startup.Flags` 和 `BindFlags`。
2. 新前置资源在 `boot/runtime.go` 构造，并登记逆序清理。
3. 新工具/能力注册放本包现有注册文件；跨进程服务协议先放 `contract/`。
4. 新前端只实现 `StartFrontend` 分支和该前端自己的窄依赖装配。
5. 任何新步骤都要同步 `README.md`、`contract`/协议测试和仓库级启动用例。

## Review 指南

- 新代码是否把业务判据塞进 `Run`，而不是放入拥有该规则的领域包。
- 失败路径是否漏掉清理、重复关闭或吞掉首个错误。
- 是否绕开 `internal/startup` 另写 flag/路径/插件根解析。
- 是否绕开 `internal/host` 把 Store/Engine 具体类型直接泄漏给 Application。
- 前端是否直接 import Backend、Middle 或 Store。
- 新常驻 goroutine、Watch、MCP、子进程和文件句柄是否进入关闭栈。

## 测试与验证

```text
go test ./e2e/ -run 'Boot|Session|Plugin' -count=1
go test ./test/ -run 'Smoke|Boot|Permission|Plugin' -count=1
go build ./...
go build -tags "gui,desktop,production" ./...
```

真实装配主要覆盖在 `e2e/` 与 `test/`；本包当前没有独立测试文件，不能用局部
单测替代仓库级启动链路验证。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### app.go

- `func (a *App) addCleanup(fn func())` — addCleanup 登记一个关机动作。顺序语义与 `defer` 一致：逆序执行。
- `func (a *App) Shutdown()` — Shutdown 按装配逆序释放全部部件（幂等：重复调用只会跑一次清理）。
- `func (a *App) close()` — close 执行清理栈；backend 诊断输出**最后**关（它的 defer 最先注册）。
- `func (a *App) fail(err error) error` — fail 是 Run 的错误返回姿态：先按已登记的清理栈收拾干净，再把原错误交给入口。
- `func (a *App) Serve() error` — Serve 跑前端（tui/gui/headless/backend）——前端形态是入口选择，装配与它无关。
- `func Run(opts Options) (*App, error)` — Run 装配一台完整的 Seelex。返回的 App 必须在 Serve 之后 Shutdown。

### application.go

- `func InitSessionManager(router *store.Router, eng *host.EnginePort) *host.SessionManager`
- `func InitApplication( eng *host.EnginePort, runtime *assemble.Runtime, plugins *plugin.Manager, sessions *host.SessionManager, skills *skill.Registry, workspaces *workspace.Repo, events *application.EventHub, approval application.ApprovalBroker, boards *storeboard.SessionStore, // storePath 是数据根（`-store` 解析后的值）：运行期上限覆盖层与工具记录 // 计数开关都落在它下面的 runtime-limits.json，所以它是**显式输入**而不是 // 包级变量的隐含读取。 storePath string, ) (*application.Service, error)`

### engine_plugins.go

- `func InitSkillSystem() *skill.Registry`
- `func InitPluginSystem( runtime *assemble.Runtime, skills *skill.Registry, // pluginsPaths 是 `-plugins` 的原始值（责任链的链首）；环境变量与可执行文件 // 位置由 startup.PluginRoots 自己解析。 pluginsPaths string, ) (*plugin.Manager, PluginStartup, error)`
- `func ActivateDefaultPlugin(manager *plugin.Manager, eng *frameworkSession.Session) error`

### frontend.go

- `func StartFrontend(flags *startup.Flags, app *application.Service, backendOutput io.Writer, startupWarning string, version string) error` — StartFrontend 按 `-frontend` 选择形态，并把装配表交出的 owner 直接给该形态。

### frontend_gui.go

- `func guiAvailable() bool`
- `func guiDependencies(components assembly.Components) gui.Dependencies` — guiDependencies 是桌面宿主的装配表：一行一个端口，右值是拥有该用例的组件指针。
- `func startGUI(service *application.Service, startupWarning, version string) error` — startGUI 拥有 GUI 形态的构造与关闭顺序：headless 冒烟控制面（只在
- `func startTUI(*application.Service) error` — GUI 产物专注桌面形态；TUI 使用不带 gui 标签的独立二进制。

### frontend_ports.go

- `func headlessDependencies(components assembly.Components) headless.Dependencies`
- `func consoleDependencies(components assembly.Components) console.Dependencies`

### frontend_tui.go

- `func guiAvailable() bool`
- `func startGUI(*application.Service, string, string) error`
- `func tuiDependencies(components assembly.Components) tui.Dependencies` — tuiDependencies 是 TUI 的装配表：一行一个端口，右值是拥有该用例的组件指针。
- `func InitTUI(service *application.Service) tui.Model`
- `func startTUI(service *application.Service) error`
- `func StartTUI(model tui.Model) error`

### permission.go

- `func SetupPermissionGate(runtime permissionRuntime, approval application.ApprovalBroker, mode string) error` — SetupPermissionGate 根据 -permission 标志安装权限门控与起始档位。
- `func MergePermissionConfig(base, override toolspermission.PermissionConfig) toolspermission.PermissionConfig` — MergePermissionConfig 把 seele.yaml 的 permission 段叠加到默认权责配置上：只覆盖
- `func DefaultManualRules() []toolspermission.PermissionRule` — DefaultManualRules 是 manual 模式的默认白名单（seele.yaml 未配置规则时回退）。
- `func LoadPermissionConfig(path string) (toolspermission.PermissionConfig, error)` — LoadPermissionConfig 读取 seele.yaml 的 permission 段（权限专用文件）：rules /
- `func NewPermissionBridge(broker application.ApprovalBroker) toolspermission.ApprovalHandler` — NewPermissionBridge 创建连接 permission.ApprovalHandler → ApprovalBroker 的桥接器。

### runtime.go

- `func initRuntime(storePath string) (*assemble.Runtime, error)` — initRuntime 装配 Runtime：配置责任链 → limits（含数据根覆盖层）→ 账号池。
- `func guardMultiProcess(storePath string) error`
- `func InitStore(storePath string) (*Store, error)`
- `func sessionStorageLimits() storeroot.Settings` — sessionStorageLimits 把 seelex.yaml limits.session_storage 映射成存储覆盖层
- `func InitWorkspaceRepo(storePath string) (*workspace.Repo, error)`
- `func InitEngine(runtime *assemble.Runtime, hooks *application.ToolHookBridge, sessionID string) (*frameworkSession.Session, error)` — InitEngine 按新装配模型创建主会话（session.NewSession）。
- `func toolCommandOutcomeObserver(next toolreg.BashDiagnosticObserver, recorder commandOutcomeRecorder) toolreg.BashDiagnosticObserver` — toolCommandOutcomeObserver 把命令面的退出码折成一次工具记录（成功/失败），再把事件
- `func sessionKeyFor(workspaces *workspace.Repo, sessionID string) (store.Key, bool)` — sessionKeyFor 把会话 ID 解析成存储作用域键（项目 + 会话）：**按会话自己的项目绑定**解析，
- `func (st sessionToolRecordStore) SaveToolRecordSnapshot(sessionID string, snapshot application.ToolRecordSnapshot) error`
- `func (st sessionToolRecordStore) LoadToolRecordSnapshot(sessionID string) (application.ToolRecordSnapshot, bool, error)`
- `func (st sessionToolRecordStore) key(sessionID string) (store.Key, bool)`

### service_ports.go

- `func ServicePorts(runtime contract.RuntimePort) assembly.ExternalPorts`

### tools.go

- `func argsOnly(handler func(argsJSON string) (string, error)) func(context.Context, string) (string, error)` — argsOnly 把 owner 的 handler 提升到注册面要求的 func(context.Context, string) 形状。
- `func RegisterContextReadTools(runtime *assemble.Runtime, app *application.Service)`
- `func RegisterProjectBoardReadTool(runtime *assemble.Runtime, st board.ProjectBoardReader, app *application.Service)` — RegisterProjectRefreshTool 注册 project_refresh 产品工具：扫描模块文档目录 +
- `func RegisterProjectRefreshTool(runtime *assemble.Runtime, st *store.Router, app *application.Service)`
- `func ScheduledPromptExecutor(app *application.Service) assemble.ScheduledPromptExecutor` — ScheduledPromptExecutor 是定时提示词任务的执行器。落点与装配都从任务定义读
- `func RegisterScheduledTaskCapability(runtime *assemble.Runtime)` — RegisterScheduledTaskCapability 装配定时周期任务白名单命令：
- `func RegisterTaskTerminalTools(runtime *assemble.Runtime, app *application.Service)` — RegisterTaskTerminalTools 把 task_complete/task_failed/task_needs_user_decision
- `func RegisterProductTools(runtime *assemble.Runtime, plugins *plugin.Manager, eng pluginPromptEngine, approval application.ApprovalBroker, skills *skill.Registry)`
- `func safePluginCapabilities(plugins *plugin.Manager, names []string) []dto.PluginCapabilityView`
- `func RegisterReviewFilesTool(runtime *assemble.Runtime, app *application.Service)` — RegisterReviewFilesTool 注册 show_files：agent 请求 GUI 在侧边资源管理器打开
- `func RegisterMCPServers(runtime *assemble.Runtime, mcpPath string)` — RegisterMCPServers 将 mcp.yaml 配置的 MCP 服务器全部登记到 Runtime
- `func RegisterMCPLoadTool(runtime *assemble.Runtime)` — RegisterMCPLoadTool 注册按需加载工具：连接已登记但未连接的 MCP 服务器
- `func RegisterTimeTool(runtime *assemble.Runtime)`
- `func RegisterPluginSwitchTools( runtime *assemble.Runtime, plugins *plugin.Manager, eng pluginPromptEngine, )`
- `func RegisterPluginSelfTools(runtime *assemble.Runtime, plugins *plugin.Manager)` — RegisterPluginSelfTools 注册插件/技能/MCP 的自迭代工具族：
- `func ApplyPluginPrompt(eng pluginPromptEngine, plugins *plugin.Manager)`
- `func RegisterAskApprove(runtime *assemble.Runtime, approval application.ApprovalBroker)`
- `func ProjectBoardSinkFor( limits limit.ProjectBoardLimits, st board.ProjectBoardStore, workTableRows func(sessionID string) []model.WorkItem, teamPlanFor func(sessionID string) *store.TeamworkPlan, teamStateFor func(sessionID string) *store.TeamworkState, projectRootFor func(sessionID string) string, evidenceChars func() int, ) (board.ProjectBoardSink, func(), error)` — guardMultiProcess 是启动期多进程闸门：数据根是单进程写者
- `func PluginByName(plugins []plugin.Plugin, name string) (plugin.Plugin, error)`
- `func RegisterGoalTools(runtime *assemble.Runtime, handlers goalToolHandlers)`
- `func RegisterSkillActivateTool(runtime *assemble.Runtime, skills *skill.Registry, activate func(name string) (skill.Skill, error))` — RegisterSkillActivateTool 注册模型侧的技能激活工具（Codex 式 "调用 skill"
- `func projectBoardReadRows(rows []dto.ProjectBoardRow) []map[string]interface` — projectBoardReadRows 把投影行渲染成给模型看的紧凑载荷：只带判定所需的那几格，
- `func projectBoardHookMode(hook string) (string, error)` — projectBoardHookMode 归一化并校验项目看板的触发面取值。
