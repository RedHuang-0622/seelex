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

| 文件 | 主要函数 |
|---|---|
| `app.go` | `Run`：装配完整实例；`App.Serve`：启动前端；`App.Shutdown`：执行逆序幂等清理；`App.fail`：失败回收。 |
| `application.go` | `InitSessionManager`：接 Store 与 Engine；`InitApplication`：构造 Application assembly。 |
| `engine_plugins.go` | `InitSkillSystem`：创建技能注册表；`InitPluginSystem`：加载插件并生成根读数；`ActivateDefaultPlugin`：激活默认插件。 |
| `permission.go` | `SetupPermissionGate`：装配权限中间件；`MergePermissionConfig`：合并规则；`LoadPermissionConfig`：读取配置；`NewPermissionBridge`：连接审批。 |
| `runtime.go` | `initRuntime`、`guardMultiProcess`、`InitStore`、`InitWorkspaceRepo`、`InitEngine`、`sessionKeyFor`。 |
| `service_ports.go` | `ServicePorts`：构造 Application 的外部窄端口组。 |
| `tools.go` | `RegisterProductTools`、`RegisterContextReadTools`、`RegisterProjectBoardReadTool`、`RegisterProjectRefreshTool`、`RegisterScheduledTaskCapability`、`RegisterTaskTerminalTools`、`RegisterReviewFilesTool`、`RegisterMCPServers`、`RegisterMCPLoadTool`、`RegisterTimeTool`、`RegisterPluginSwitchTools`、`RegisterPluginSelfTools`、`RegisterAskApprove`、`RegisterGoalTools`、`RegisterSkillActivateTool`、`ProjectBoardSinkFor`。 |
| `frontend.go` | `StartFrontend`：按已校验模式启动 TUI/GUI/headless/backend。 |
| `frontend_tui.go` | `InitTUI`、`StartTUI`、`tuiDependencies` 与无 GUI 构建下的占位实现。 |
| `frontend_gui.go` | GUI 可用性、依赖与启动适配。 |
| `frontend_ports.go` | headless 与控制台前端的依赖装配。 |
