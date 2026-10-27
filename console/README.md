# Console

## 生态位

`console` 提供无窗口的后端诊断控制台：绑定项目、提交提示、观察事件流并以脱敏格式输出
阶段日志。它是 `-frontend backend` 的入口，只消费本包声明的 consumer-owned 窄端口
（`Dependencies`），不依赖具体 UI，也不经由任何 application 级宽门面取能力。

主要调用方：组合根 `wiring/bootstrap`（`-frontend backend` 分支）——它既装配
`console.Dependencies`，也用 `NewEventLogger` / `LogStageIf` 打印启动阶段轨迹。

## 职责与非职责

- 做：一次 `Submit` 进核、事件流的脱敏阶段投影、可选的项目绑定、stdout / 文件输出选择。
- 不做：不持有业务状态机、不复制 application 判定、不在运行期探测自己的依赖是否装配。

## 目录与文件

| 文件 | 职责 |
|---|---|
| `dependencies.go` | 本包声明的 5 个窄端口与 `Dependencies` 装配形状 |
| `backend_console.go` | 主循环（`Start` / `Run`）、项目绑定、输出选择、脱敏事件日志器 |

## 核心实现

- 端口按 owner 切分，一个字段对应一个拥有该用例的组件：`Submitter`
  （`Conversation.Submission`）、`TurnWaiter`（`Runtime.Lifecycle`）、`EventReader`
  （`Session.View`）、`ViewSnapshot`（`Session.Snapshot`）、`WorkspaceBinder`
  （`Workspace.Switch`）。方法名逐字等于 owner 的方法名。
- 旧形态里 `backendApplication` 把 Submit / WaitForIdle / Subscribe 写在一个接口里，
  只有兼容门面能同时满足它；拆开后 `Dependencies` 的三个字段由装配处交出三个 owner
  指针，绑定语句本身就是编译期结构断言。
- `BindProject` 的读面（`ViewSnapshot`）与写面（`WorkspaceBinder`）是两个独立端口，
  由装配处显式交给用例（`components.Session.Snapshot` 与 `components.Workspace.Switch`），
  因此诊断台没有"读面和写面各配一份"的第二条通路。
- `EventLogger` 是唯一的脱敏投影：只打阶段、工具名、状态、时长与字节数，
  绝不输出用户输入、命令文本、参数、路径或工具结果。

## 数据流

`wiring/bootstrap` 装配 `Dependencies` → `Start` → `observeBackendEvents`（订阅事件流）
→ `Submit(prompt)` →（单轮时）`WaitForIdle` → `EventLogger` 脱敏阶段日志 → stdout / 文件。

## 依赖方向

`console → application（仅 DTO / Event 类型）+ contract/dto`。owner 由 `wiring/bootstrap`
注入；禁止 import `gui`、`tui`、`application/assembly`、`application/compat` 或 `store`。

## 并发与安全

- 事件观察是独立 goroutine，`ctx` 取消或订阅关闭即退出；`Run` 返回前等待它结束。
- `EventLogger` 用自身互斥量保护写出，多来源（业务事件、bash 诊断、工具钩子诊断）可并发调用。
- 诊断日志默认 stdout；`-backend-log` 指定路径时以 `0o600` 追加并同时镜像到 stdout。

## 扩展方式

新增能力：在 `dependencies.go` 声明端口（按 owner 切，方法名等于 owner 方法名）+
在 `wiring/bootstrap/frontend_ports.go` 的 `consoleDependencies` 补一行装配。
没有调用点需要的能力不要提前声明端口。

## Review 指南

- 用例是否只调用 owner 端口，有没有重新长出 `*application.Service` / 宽门面参数；
- 是否出现运行期类型断言或判空降级（依赖全为骨架，缺失即装配错误）；
- 端口方法名是否逐字等于 owner 方法名（改名只发生在转发层，转发层是本次删除对象）；
- 日志脱敏是否仍成立：新增字段前确认它不包含用户输入、命令、路径或工具结果；
- 一次性探针依赖的事件（如 `team.changed`）是否仍有对应 stage 行，删掉等于观测面消失。

## 导出

- `Dependencies`：诊断台事件循环需要的全部应用面（每字段一个 owner）。
- `Start` / `Run`：启动诊断控制台主循环 / 无 UI 的提交-观察循环。
- `BindProject`：将后端会话绑定到项目工作区（读面 + 写面两个端口）。
- `OpenOutput` / `NewEventLogger` / `LogStageIf`：输出与脱敏事件日志器。

## 验证

```text
go test ./console -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### backend_console.go

- `func BindProject(view ViewSnapshot, commands WorkspaceBinder, rootPath string) error`
- `func Start(deps Dependencies, prompt string, timeout time.Duration, output io.Writer) error`
- `func OpenOutput(logPath string) (io.Writer, func() error, error)`
- `func Run(ctx context.Context, deps Dependencies, prompt string, timeout time.Duration, input io.Reader, output io.Writer) error`
- `func submitBackendPrompt(ctx context.Context, deps Dependencies, logger *EventLogger, prompt string, timeout time.Duration) error`
- `func observeBackendEvents(ctx context.Context, deps Dependencies, logger *EventLogger) func()`
- `func NewEventLogger(output io.Writer, now func() time.Time) *EventLogger`
- `func LogStageIf(logger *EventLogger, stage string)`
- `func (logger *EventLogger) LogStage(stage string)`
- `func (logger *EventLogger) LogBashEvent(event dto.BashDiagnosticEvent)` — LogBashEvent records the process-boundary diagnostics emitted by scopedBash.
- `func (logger *EventLogger) LogToolHookEvent(event application.ToolHookDiagnosticEvent)` — LogToolHookEvent records the framework-to-application projection boundary.
- `func (logger *EventLogger) LogSubmit(text string)`
- `func (logger *EventLogger) LogSubmitError(err error)`
- `func (logger *EventLogger) LogIdle(err error)`
- `func (logger *EventLogger) LogEvent(event application.Event)`
- `func (logger *EventLogger) logToolEventLocked(event application.Event)`
- `func (logger *EventLogger) requestStartLocked(requestID string) time.Time`
- `func (logger *EventLogger) elapsedLocked(requestID string) time.Duration`

### backend_console_test.go

- `func (fake *backendWorkspaceFake) Snapshot() application.Snapshot`
- `func (fake *backendWorkspaceFake) CreateWorkspace(name, rootPath, _ string) error`
- `func (fake *backendWorkspaceFake) BindWorkspace(workspaceID string) error`
- `func newBackendConsoleFake() *backendConsoleFake`
- `func (fake *backendConsoleFake) Submit(_ context.Context, text string) error`
- `func (fake *backendConsoleFake) WaitForIdle(context.Context) error`
- `func (fake *backendConsoleFake) Subscribe(buffer int) application.Subscription`
- `func TestBackendEventLoggerLogsToolStagesWithoutPayloadContent(t *testing.T)`
- `func TestBackendEventLoggerLogsTeamChangedStage(t *testing.T)`
- `func TestBackendConsolePromptSubmitsAndWaitsForIdle(t *testing.T)`
- `func TestOpenBackendOutputUsesStdoutWithoutLogPath(t *testing.T)`
- `func TestBackendEventLoggerLogsBashStagesWithoutCommandContent(t *testing.T)`
- `func TestBackendEventLoggerLogsToolHookStagesWithoutPayloadContent(t *testing.T)`
- `func TestBindBackendProjectUsesExistingWorkspaceOrCreatesOne(t *testing.T)`

