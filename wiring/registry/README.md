# wiring/registry

## 生态位

`wiring/registry` 是 **Seelex 自己生产的那一族工具与能力的登记处**：`read_tool_result` /
`compact_context` / 项目看板读写 / 定时任务能力 / goal 工具族 / `skill_activate` /
`ask_approve` / 插件切换与自省 / MCP 加载 / `web_search` / 时间工具都在这里注册。

主要调用方是组合根（`main.go` 的 `run()`）与 `wiring/bootstrap`；测试基座
（`test/`、`e2e/`）也直接复用这些注册函数——它们就是"装配一台可用的 Seelex"里
**工具面**的那一段。把这一段从组合根搬出来，是为了让第二个入口（不同 build tag /
不同前端形态）复用同一份工具表，而不是各自抄一遍。

## 职责与非职责

**做**：把工具/能力登记进 `assemble.Runtime`（工具名、描述、JSON Schema、handler）、
把 handler 路由到 `appassembly.ComponentsFor(app)` 快照里的领域 owner 与 `assemble.Runtime`
的既有方法面、生成启动期
可复用的执行器（`ScheduledPromptExecutor`）、把看板写盘的 sink 与关闭钩子交给调用方。

**不做**：不装配 Runtime/Application（见 `wiring/bootstrap`）；不解析配置与路径（见
`wiring/config`）；不持有状态、不起后台循环、不缓存快照——每次调用都是一次登记动作；
也不决定"这个工具该不该被模型看见"（可见性门控在 `backend/tool/registry` 与
`backend/limit/policy`，本包只登记名字）。

## 文件结构

| 文件 | 职责 |
|---|---|
| `registry.go` | 全部注册入口（19 个）：`RegisterContextReadTools` / `RegisterProjectBoardReadTool` / `RegisterProjectRefreshTool` / `ScheduledPromptExecutor` / `RegisterScheduledTaskCapability` / `RegisterTaskTerminalTools` / `RegisterProductTools` / `RegisterReviewFilesTool` / `RegisterMCPServers` / `RegisterMCPLoadTool` / `RegisterTimeTool` / `RegisterPluginSwitchTools` / `RegisterPluginSelfTools` / `ApplyPluginPrompt` / `RegisterAskApprove` / `ProjectBoardSinkFor` / `PluginByName` / `RegisterGoalTools` / `RegisterSkillActivateTool`；四个私有 helper（`argsOnly` 把 owner handler 提升到注册面形状 / `safePluginCapabilities` / `projectBoardReadRows` / `projectBoardHookMode`）；`pluginPromptEngine` 是本包定义的消费方接口。 |

## 核心实现

- **登记顺序即行为**：`RegisterProductTools` 是工具面的总装配表（MCP → MCP 加载 →
  时间 → 插件切换 → 插件自省 → 审批 → 审阅文件 → 自省注册），顺序与该表在组合根里
  逐字一致；改动顺序属于行为变更。
- **handler 只做路由**：每个 handler 都从 `appassembly.ComponentsFor(app)` 这份分组
  只读快照里取 owner 指针（`components.Conversation.Reference.*`、
  `components.Conversation.HistorySearch.SearchHistoryHandler`、
  `components.Conversation.Compaction.CompactContextHandler`、
  `components.Execution.ProjectBoard.*`、`components.Admin.Review`、…），
  本包不复制业务判定，也不再经 `application.Service` 门面方法。
- **能力面与工具面分开**：`RegisterTaskTerminalTools` 只转发
  `components.Execution.TerminalHandler.Handler`；goal 工具族的可视性由
  `backend/tool/registry` 的 `isGoalTool` + `GoalActive` 门控。
- **看板 sink 的开关是装配期的**：`ProjectBoardSinkFor` 在关时返回 nil sink 与空
  close，工具完成段判空后一个字节都不写（关就是关，不是"注册了但报错"）。
- **接口放消费方**：`pluginPromptEngine`（只要求 `SetSystemPrompt(string)`）定义在本包，
  具体 Session 由组合根注入。
- **show_files 注册归本包**：BM-005 首个切片起，本包直接调用
  `backend/tool/review.RegisterShowFilesTool`；旧 assemble wrapper 只保留 D1 兼容。

## 数据流

组合根（或 `wiring/bootstrap`）→ `Registry*` 一次调用 → `assemble.Runtime.RegisterTool` /
`RegisterCapability` → 运行期模型调用工具 → handler → `ComponentsFor(app)` 里的 owner 组件。
本包是**单向漏斗**：没有从 Runtime 回到本包的读路径，也不订阅事件。

## 依赖方向

允许：标准库、`application`（含 `application/assembly`、`assembly/task_context`）、
`backend/engine/assemble`（Runtime 类型）、`kernel/limit`、`backend/tool/{registry,websearch}`、
`backend/engine/context/seelectx/search`（原 `seelexctx/search`，M5-R7 归位）、`contract/dto`、`wiring/adapters`、`internal/bootseed`、
`mcpstack/config`、`plugin`、`skill`、`store`、`wiring/config`。

禁止：`gui`、`tui`（前端只能经 Application），也不得反向被 `application/*` 依赖。

## 错误语义

- 没有返回值里的"软失败"：注册要么成功要么 panic（与 `Runtime.RegisterTool` 同）。
- 需要外部资源的注册点显式报错/跳过并留日志：`RegisterScheduledTaskCapability` 在
  找不到脚本目录或 python 解释器时**跳过登记**并写明原因（登记即信任、argv 固定直传，
  宁可不登记也不登记半个）。
- `ProjectBoardSinkFor` 的 `closeProjectBoard` 必须被调用方 `defer`：它是唯一收口点。

## 扩展方式

新增工具：加一个 `Register<Name>` 函数 + 在 `RegisterProductTools`（或组合根）里接线 +
在 `backend/limit/policy` 的工具组表里分封（否则 `TestEveryRegisteredToolDeclaresGroups`
会点名它）+ 补本 README 与用例。工具名要用字面量或包级常量：拼出来的名字绕过了那条
门禁的静态扫描。

## Review 指南

- 登记顺序与工具表内容是否与改动前逐字一致（这一族是对外行为面）。
- 是否把业务判定搬进了 handler（应当只有路由）。
- 新增工具名是否分封（权限组 + 并发分类），否则它拿不到任何策略也没有报错面。
- 看板开关的"关 ⇒ 零写盘"是否被改坏。
- 是否有第二份"工具表"（另一处也注册同名工具）。

## 测试与验证

```text
go test ./wiring/registry/ -count=1     # 本包无独立用例（注册是纯转发）
go build ./wiring/registry
go test ./test/ -run 'TestEveryRegisteredToolDeclaresGroups|TestRegistrationScannerSeesEveryFamily' -count=1
go test ./test/ -run 'TestTool|TestProjectBoard|TestPlugin' -count=1
```

关键跨包用例（随 M5 收尾搬进 `test/`）：`tool_meta_declaration_test.go`（每个注册的工具
都必须分封）、`project_board_*_test.go`（看板读/写/刷新与开关两臂）、
`plugin_*_test.go`（插件切换与自省工具）、`skill_activate_tool_test.go`、
`tool_full_chain_test.go`（上下文读工具与定时任务执行器）。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### registry.go

- `func argsOnly(handler func(argsJSON string) (string, error)) func(context.Context, string) (string, error)` — argsOnly 把 owner 的 handler 提升到注册面要求的 func(context.Context, string) 形状。
- `func RegisterContextReadTools(runtime *assemble.Runtime, app *application.Service)`
- `func RegisterProjectBoardReadTool(runtime *assemble.Runtime, st *store.Router, app *application.Service)` — RegisterProjectRefreshTool 注册 project_refresh 产品工具：扫描模块文档目录 +
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
- `func projectBoardReadRows(rows []store.ProjectBoardRow) []map[string]interface` — projectBoardReadRows 把投影行渲染成给模型看的紧凑载荷：只带判定所需的那几格，
- `func projectBoardHookMode(hook string) (string, error)` — projectBoardHookMode 归一化并校验项目看板的触发面取值。

