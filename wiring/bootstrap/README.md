# wiring/bootstrap

## 生态位

mwiring/bootstrapm 是组合根的**装配段**：把 Seelex 的全部部件（Runtime、插件/技能系统、存储、
Application、权限门、工具与能力、看板 sink、前端）按固定顺序接成一台能跑的 Seelex。

主要调用方是入口 mmain.gom（mRunm → mServem → mShutdownm 三步）与测试基座
（mtest/m、me2e/m 里的 full-chain harness 逐段复用 mInit*m）。它存在的理由只有一条：
**同一套装配要能被多个入口复用**（mgo run .m、发布二进制、m-frontend backendm 诊断入口、
headless 宿主、测试基座），第二份"看起来差不多"的装配正是历史事故（测试基座缺一句
mAttachSubSessionStorem，端到端断言恒为空集）的来源。

## 职责与非职责

**做**：装配顺序；装配期的一次性注入（mRuntimeDepsm / mEnginePortDepsm / 看板 sink / 工具记录
存储）；**逆序关机**；启动期多进程闸门；数据根解析后的 limits 与覆盖层叠加；权限门安装；
前端选择与启动。

**不做**：不声明启动参数（mwiring/configm）；不登记具体工具（mwiring/registrym）；不实现任何
业务判定（一律调用被装部件的既有方法面）；不解析配置段语义（mbackend/limitm、
mmcpstack/configm 各自拥有自己的段）。

## 文件结构

| 文件 | 职责 |
|---|---|
| mbootstrap.gom | mOptionsm / mAppm / mRunm / mServem / mShutdownm：装配主流程与逆序关机栈。 |
| mruntime.gom | mInitRuntimem（配置责任链 → limits → 覆盖层 → 账号池）、mInitStorem、mInitWorkspaceRepom、mInitEnginem、会话→存储键解析、工具记录存储适配、多进程闸门。 |
| mplugins.gom | mInitSkillSystemm、mPluginStartupm、mInitPluginSystemm、mActivateDefaultPluginm。 |
| mapplication.gom | mInitSessionManagerm、mInitApplicationm（mwiring.New(Dependencies{...})m 的唯一装配点）。 |
| mpermission.gom | mSetupPermissionGatem、mMergePermissionConfigm、mDefaultManualRulesm、mLoadPermissionConfigm、mNewPermissionBridgem。 |
| mfrontend.gom | mInitTUIm、mStartTUIm、mStartFrontendm（tui / gui / headless / backend 四态分派）。 |

## 核心实现

- **顺序就是行为面**：mRunm 的每一步顺序与 M5 之前 mmain.gom 的 mrun()m 逐条一致
  （runtime → 闸门 → builtins → skills → plugins → store → 权限门 → 工具注册 → EnginePort
  deps → teamwork → Application → 各类回调注入 → 看板 sink → 工具记录 → RuntimeDeps →
  定时任务恢复 → 前端）。改动顺序属于行为变更，必须配套用例。
- **逆序关机栈**：mRunm 里原本的 mdeferm 全部改写成 mApp.addCleanupm，mShutdownm 反向执行，
  顺序与改动前逐条相同（mcloseProjectBoardm → mapp.Shutdownm → mst.Closem → mruntime.Shutdownm
  → backend 诊断输出最后关）。装配中途失败时 mfailm 走同一套清理再返回原错误。
- **显式输入**：数据根（mstorePathm）与插件根（mpluginsPathsm）作为参数传入，装配段不读包级
  flag——第二个入口因此可以把它们换成别的值（不同 build 的不同配置）。
- **权限门**：先装 manual 权责基线（分组 + 主体 + 缺位口径 + 默认规则），mconfig/seele.yamlm
  的 mpermissionm 段按字段覆盖；免打断白名单按**装配期解析出的实际壳**分派
  （mpolicy.PermissionConfigForShellm）。
- **前端分派**：mStartFrontendm 是四态 switch；GUI 不可用时（未带 mgui,desktop,productionm
  标签）在装配早期就给出可读错误，而不是等到建窗。

## 数据流

mwiring/config.Flagsm + minternal/buildinfo.Versionm（入口）→ mRunm → mApp{Application, Runtime, Store}m
→ mServem 跑前端 → mShutdownm 逆序释放。运行期数据流由各被装部件负责，本包只在装配期
**一次性**接线（mRuntimeDepsm / mEnginePortDepsm / mSet*m 注入），不做运行期转发。

## 依赖方向

允许：标准库、mapplicationm（含 core、console、mcore/task_contextm、mcore/session_runtimem）、
mbackend/engine/assemblem（Runtime）、mbackend/limit{,overlay,policy}m、
mbackend/multiagent/teamworkm、mbackend/tool/{registry,search}m、mguim、mtuim、
minternal/bootseedm、mbackend/engine/prompt/assetsm、mpluginm、msessionm、mskillm、mstore{,/workspace}m、
mwiringm、mwiring/configm、mwiring/registrym、Seele 的 msessionm/mtypesm/mtools/permissionm。

禁止：mwiring/bootstrapm 不得被 mapplication/*m、mbackend/*m、mguim、mtuim 反向依赖；它只被
入口与测试装配。

## 错误语义

- 装配期错误**不吞**：逐层向上返回原错误（mInitStorem/mInitRuntimem/mInitPluginSystemm 各自
  带上下文），mRunm 在返回前按已登记的清理栈释放已装部件——半装配状态不会留在进程里。
- 启动期的**非致命**配置问题走 mRuntime.StartupWarnings()m：以系统通知进 UI 面，不阻断启动
  （"配置读不到就回退代码默认值"的既有口径）。
- 零插件、GUI 不可用、数据根被活进程占用：都是显式错误（分别由 mwiring/configm、
  mStartFrontendm 的可用性检查、mlimits.runtime.allow_multi_processm 闸门给出）。

## 扩展方式

新增一个装配步骤：加在 mRunm 里**正确的位置**并说明为什么在那里（顺序即行为），需要关机的
就 mboot.addCleanupm；新增一个入口：mconfig.BindFlags(flag.CommandLine)m → mbootstrap.Runm →
mServem → mShutdownm，不要在入口里复制装配。新增前端形态：改 mParseFrontendModem
（mwiring/configm）与 mStartFrontendm 的 switch，并补两处文档。

## Review 指南

- 装配顺序是否被改动（尤其 mSetTeamworkBackendm 必须在 mInitApplicationm 之前、
  mRestoreScheduledTasksm 必须在执行器注入之后）。
- 新增的 mdeferm 是否必须——mRunm 内部用清理栈，不要混用两种关机路径。
- 是否把包级 flag 又读回来了（应当只剩参数）。
- 是否出现了第二份装配（测试基座自己拼一遍）——那正是漂移的来源。
- 关机顺序是否仍是逆序，且每一格只会被关一次（幂等）。

## 测试与验证

mmmtext
go test ./wiring/bootstrap/ -count=1     # 本包随装配被根包/测试基座用例覆盖
go build ./wiring/bootstrap
go test ./test/ -count=1                 # full-chain harness 逐段复用 Init*
go test ./e2e/ -count=1
mmm

关键跨包用例（随 M5 收尾搬进 mtest/m）：mtool_full_chain_test.gom（同序装配的测试基座）、
mmanual_smoke_test.gom、mrepro_*_test.gom（会话/工具链回归都建在这套装配上）、
mpermission_config_test.gom / mpermission_employee_panel_test.gom（权限门与审批桥）、
mrelease_test.gom（档位与默认值的契约）。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### application.go

- `func InitSessionManager(router *store.Router, eng *adapters.EnginePort) *adapters.SessionManager`
- `func InitApplication( eng *adapters.EnginePort, runtime *assemble.Runtime, plugins *plugin.Manager, sessions *adapters.SessionManager, skills *skill.Registry, workspaces *workspace.Repo, events *application.EventHub, approval *application.ApprovalBroker, // storePath 是数据根（`-store` 解析后的值）：运行期上限覆盖层与工具记录 // 计数开关都落在它下面的 runtime-limits.json，所以它是**显式输入**而不是 // 包级变量的隐含读取。 storePath string, ) (*application.Service, error)`

### bootstrap.go

- `func (a *App) addCleanup(fn func())` — addCleanup 登记一个关机动作。顺序语义与 `defer` 一致：逆序执行。
- `func (a *App) Shutdown()` — Shutdown 按装配逆序释放全部部件（幂等：重复调用只会跑一次清理）。
- `func (a *App) close()` — close 执行清理栈；backend 诊断输出**最后**关（它的 defer 最先注册）。
- `func (a *App) fail(err error) error` — fail 是 Run 的错误返回姿态：先按已登记的清理栈收拾干净，再把原错误交给入口。
- `func (a *App) Serve() error` — Serve 跑前端（tui/gui/headless/backend）——前端形态是入口选择，装配与它无关。
- `func Run(opts Options) (*App, error)` — Run 装配一台完整的 Seelex。返回的 App 必须在 Serve 之后 Shutdown。

### frontend.go

- `func InitTUI(app *application.Service) tui.Model`
- `func StartTUI(model tui.Model) error`
- `func StartFrontend(flags *config.Flags, app *application.Service, backendOutput io.Writer, startupWarning string, version string) error`

### permission.go

- `func SetupPermissionGate(runtime permissionRuntime, approval *application.ApprovalBroker, mode string) error` — SetupPermissionGate 根据 -permission 标志安装权限门控与起始档位。
- `func MergePermissionConfig(base, override toolspermission.PermissionConfig) toolspermission.PermissionConfig` — MergePermissionConfig 把 seele.yaml 的 permission 段叠加到默认权责配置上：只覆盖
- `func DefaultManualRules() []toolspermission.PermissionRule` — DefaultManualRules 是 manual 模式的默认白名单（seele.yaml 未配置规则时回退）。
- `func LoadPermissionConfig(path string) (toolspermission.PermissionConfig, error)` — LoadPermissionConfig 读取 seele.yaml 的 permission 段（权限专用文件）：rules /
- `func NewPermissionBridge(broker *application.ApprovalBroker) toolspermission.ApprovalHandler` — NewPermissionBridge 创建连接 permission.ApprovalHandler → ApprovalBroker 的桥接器。

### plugins.go

- `func InitSkillSystem() *skill.Registry`
- `func InitPluginSystem( runtime *assemble.Runtime, skills *skill.Registry, // pluginsPaths 是 `-plugins` 的原始值（责任链的链首）；环境变量与可执行文件 // 位置由 config.PluginRoots 自己解析。 pluginsPaths string, ) (*plugin.Manager, PluginStartup, error)`
- `func ActivateDefaultPlugin(manager *plugin.Manager, eng *frameworkSession.Session) error`

### runtime.go

- `func initRuntime(storePath string) (*assemble.Runtime, error)` — initRuntime 装配 Runtime：配置责任链 → limits（含数据根覆盖层）→ 账号池。
- `func guardMultiProcess(storePath string) error`
- `func InitStore(storePath string) (*store.Router, error)`
- `func sessionStorageLimits() storeroot.Settings` — sessionStorageLimits 把 seele.yaml limits.session_storage 映射成存储覆盖层
- `func InitWorkspaceRepo(storePath string) (*workspace.Repo, error)`
- `func InitEngine(runtime *assemble.Runtime, hooks *application.ToolHookBridge, sessionID string) (*frameworkSession.Session, error)` — InitEngine 按新装配模型创建主会话（session.NewSession）。
- `func toolCommandOutcomeObserver(next toolreg.BashDiagnosticObserver, app *application.Service) toolreg.BashDiagnosticObserver` — toolCommandOutcomeObserver 把命令面的退出码折成一次工具记录（成功/失败），再把事件
- `func sessionKeyFor(workspaces *workspace.Repo, sessionID string) (store.Key, bool)` — sessionKeyFor 把会话 ID 解析成存储作用域键（项目 + 会话）：**按会话自己的项目绑定**解析，
- `func (st sessionToolRecordStore) SaveToolRecordSnapshot(sessionID string, snapshot application.ToolRecordSnapshot) error`
- `func (st sessionToolRecordStore) LoadToolRecordSnapshot(sessionID string) (application.ToolRecordSnapshot, bool, error)`
- `func (st sessionToolRecordStore) repository() (store.ToolRecordRepository, bool)`
- `func (st sessionToolRecordStore) key(sessionID string) (store.Key, bool)`

