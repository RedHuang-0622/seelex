# TUI 前端与 Application Core 拆分详细方案

## 设计目标

1. `tui` 只负责 Bubble Tea 输入事件、组件局部状态、布局和渲染。
2. 聊天、命令、Skill、Plugin、Session、账号、审批、补全等用例进入无界面的 `application` 包。
3. TUI 可在进程内直接调用 application；未来 Electron 可通过 IPC adapter 调用相同能力。
4. 状态和事件 DTO 可 JSON 序列化，不携带 Bubble Tea、Lipgloss、Engine 或 Seele 深层类型。
5. 去除所有 TUI 包级可变状态，允许测试、多实例和多客户端订阅。
6. 不修改 Seele；除允许直接使用的 `engine` 外，其余框架能力继续通过 `seelebridge`。

## 非目标

- 本轮不实现 Electron 页面。
- 本轮不修改或发布 Seele。
- 本轮不借拆分之机重写 Session 持久化格式。
- 本轮不解决 Seele 当前缺少 Engine 历史替换 API 导致的 Resume 限制。
- 本轮不把终端布局、Lipgloss Style 或 Bubble Tea KeyMsg 抽象成跨前端协议。

## 方案对比

| 维度 | 方案 A：Headless Application Service（推荐） | 方案 B：共享 MVU/Reducer | 方案 C：仅抽 Service helper |
|---|---|---|---|
| 核心思路 | application 持有业务状态和副作用；UI 是 adapter | 把 Update reducer 抽成通用包，各 UI 消费 action/state | 从 TUI 搬出 Chat/command 函数，Model 仍持有主要状态 |
| TUI 耦合 | 低 | 中 | 中高 |
| Electron 复用 | 高，可通过 RPC 直接使用 | 中；TS 不能直接运行 Go reducer，仍需 RPC 或重复 reducer | 低，Electron 仍需重写状态编排 |
| 副作用管理 | 集中清晰 | reducer 外还需 effect runtime，复杂 | 分散在 helper 和 UI |
| 可测试性 | 高，application 可无 UI 单测 | 高，但模型和 effect 体系成本大 | 中 |
| 改动量 | 中高 | 高 | 低中 |
| 长期扩展 | 适合 TUI/Electron/Web/API | 适合多个 Go UI，不天然适合 JS UI | 容易再次耦合 |
| 回滚性 | 可按 use case 垂直迁移 | 需要较大一次性替换 | 高 |

## 推荐：方案 A

Electron 与 Go 的语言边界决定了必须存在 transport。与其把 Bubble Tea 的 Elm/MVU 模型强行泛化，不如建立稳定的 application service：Go TUI 直接调用，Electron 通过 sidecar RPC 调用。

最大风险是流式事件、工具 hook 和同步审批三类异步路径。方案通过实例化 EventHub、单调事件序号、context cancel 和 ApprovalBroker 解决，不使用包级 channel。

## 目标架构

```text
┌───────────────────────────┐       ┌───────────────────────────┐
│ Bubble Tea TUI            │       │ Electron Renderer         │
│                           │       │                           │
│ Key/Mouse/Resize          │       │ DOM/Input/Modal           │
│ Local UI State            │       │ Local UI State            │
│ Components + Render       │       │ Components + Render       │
└─────────────┬─────────────┘       └─────────────┬─────────────┘
              │ Go interface                     │ JSON-RPC/stdio
              ▼                                  ▼
        ┌─────────────────────────────────────────────────┐
        │ Seelex application                              │
        │                                                 │
        │ Input Router / Command Registry / Completion    │
        │ Chat Coordinator / EventHub / ApprovalBroker    │
        │ Session / Plugin / Skill / Account Use Cases    │
        │ Snapshot Store                                  │
        └──────────────────────┬──────────────────────────┘
                               │ ports
                  ┌────────────┼────────────┐
                  ▼            ▼            ▼
               engine     seelebridge   plugin/skill/session
                  │
                  ▼
                Seele（保持纯框架库，不修改）
```

## 包结构

```text
application/
├── app.go              # Service 生命周期与公开用例
├── ports.go            # application 消费的窄接口
├── state.go            # Snapshot/read model/DTO
├── event.go            # EventHub、订阅、Seq/Revision
├── chat.go             # ChatStream、取消、tool event
├── input.go            # 普通输入、/command、#skill 路由
├── command.go          # 实例化 CommandRegistry
├── commands_builtin.go # 内置业务命令
├── completion.go       # command/tool/skill 建议
└── approval.go         # ApprovalBroker

tui/
├── model.go            # Bubble Tea adapter；只持有 AppController + UI state
├── event.go            # application.Event -> tea.Msg
├── input.go            # KeyMsg/MouseMsg -> application 调用
├── view.go             # 页面组合
├── component_*.go      # status/conversation/input/suggest/selector/approval
├── state.go            # 仅尺寸、焦点、滚动、选中项等 UI state
└── styles.go

transport/jsonrpc/      # 第二阶段
├── server.go           # JSON-RPC/stdio 请求循环
├── methods.go          # application 方法映射
├── notifications.go    # application.Event 通知
└── protocol.go         # version/capabilities/error DTO
```

不建议一开始把每个 TUI 组件都拆成独立 Go package。先用 `component_*.go` 保持一个 `tui` package，避免 Style、布局和组件之间制造大量小接口；只有审批等确实能独立测试/复用的组件再下沉子包。

## 状态边界

### Application 持有

- 当前 Session ID 和会话消息 read model。
- Chat 是否运行、开始时间、请求 ID、取消函数。
- assistant 文本增量。
- tool call 开始、完成、结果、错误和耗时。
- 当前 provider/account/plugin/model、可见工具和 Skills。
- command catalog 和 completion 数据。
- 待处理 approval/selection interaction。
- capability，例如 `session_resume=false` 及原因。

### TUI 持有

- 终端 width/height。
- textarea、viewport、focus。
- splash/logo 是否显示。
- suggestion 面板是否展开、选中 index、scroll offset。
- selector/approval 的当前高亮项。
- 颜色、边框、动画帧、光标闪烁。
- 鼠标滚动和快捷键映射。

### Electron 持有

- DOM 组件状态、路由、窗口布局。
- modal 是否展开和前端选中项。
- 本地主题、快捷键、窗口持久化。
- application snapshot/event 的前端缓存。

## 核心 DTO

```go
package application

type Snapshot struct {
	Revision    uint64           `json:"revision"`
	Session     SessionState     `json:"session"`
	Conversation []Message       `json:"conversation"`
	Chat        ChatState        `json:"chat"`
	Runtime     RuntimeState     `json:"runtime"`
	Interaction *Interaction     `json:"interaction,omitempty"`
	Capabilities Capabilities    `json:"capabilities"`
}

type Message struct {
	ID        string      `json:"id"`
	Role      string      `json:"role"`
	Content   string      `json:"content,omitempty"`
	Tool      *ToolCall   `json:"tool,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
	Status    string `json:"status"`
}

type RuntimeState struct {
	Model        string       `json:"model"`
	Provider     string       `json:"provider"`
	Account      string       `json:"account,omitempty"`
	Plugin       string       `json:"plugin,omitempty"`
	VisibleTools []Tool       `json:"visible_tools"`
	Skills       []SkillInfo  `json:"skills"`
}
```

DTO 不直接复用 `engine.Message`、`plugin.Plugin`、`skill.Skill` 或 `seelebridge.Account`，避免未来 Electron 协议被 Go 内部类型变化牵动。

## 事件模型

```go
type Event struct {
	Seq       uint64          `json:"seq"`
	Revision  uint64          `json:"revision"`
	RequestID string          `json:"request_id,omitempty"`
	Kind      EventKind       `json:"kind"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

const (
	EventSnapshotChanged   EventKind = "snapshot.changed"
	EventMessageAdded      EventKind = "message.added"
	EventMessageDelta      EventKind = "message.delta"
	EventToolStarted       EventKind = "tool.started"
	EventToolCompleted     EventKind = "tool.completed"
	EventRuntimeChanged    EventKind = "runtime.changed"
	EventInteractionOpened EventKind = "interaction.opened"
	EventInteractionClosed EventKind = "interaction.closed"
	EventError             EventKind = "error"
	EventResyncRequired    EventKind = "resync.required"
)
```

规则：

1. `Seq` 在一个 application 实例内严格递增。
2. 每次可观察状态更新同时递增 `Revision`。
3. 订阅者发现 `Seq` 跳号，调用 `Snapshot()` 重建，不猜测丢失内容。
4. EventHub 不因慢订阅者阻塞 ChatStream；缓冲满时发送/标记 `resync.required`。
5. Snapshot 返回 copy，不暴露内部 slice/map。

第一版 TUI 可以在每个事件后读取 `Snapshot()`，先保证正确性；Electron adapter 再利用 delta 减少序列化成本。

## application ports

接口定义在消费方 `application` 包，main 负责注入具体实现。

```go
type ChatEngine interface {
	ChatStream(ctx context.Context, input string, onChunk func(string)) (string, error)
	History() []seelebridge.Message
	ClearHistory()
	SessionID() string
	SetSystemPrompt(string)
	ExportTrace() *seelebridge.TraceTree
}

type RuntimePort interface {
	Provider() string
	Accounts() []seelebridge.Account
	SelectAccount(name string) bool
	VisibleTools(ctx context.Context) []seelebridge.Tool
	ActivePlugin() string
}

type PluginPort interface {
	All() []plugin.Plugin
	Activate(ctx context.Context, name string) error
	Deactivate(ctx context.Context) error
	Current() (plugin.Plugin, bool)
}

type SkillPort interface {
	All() []skill.Skill
	Get(name string) (skill.Skill, bool)
}

type SessionPort interface {
	SaveCurrent(sessionID string) error
	Resume(sessionID string) error
	List() []seelebridge.SessionMeta
	LoadHistory(sessionID string) ([]seelebridge.Message, error)
}
```

若为了让 application 完全不 import `plugin/skill/seelebridge` DTO，可在装配层增加 adapter 转换为 application 自己的 DTO。这是推荐终态；迁移第一阶段可暂时保留 DTO 依赖，再在稳定后收窄。

## TUI 消费接口

接口属于调用方 `tui`：

```go
type AppController interface {
	Snapshot() application.Snapshot
	Subscribe(buffer int) application.Subscription
	Submit(ctx context.Context, text string) error
	CancelChat(requestID string) bool
	Suggestions(input string) []application.Suggestion
	ResolveInteraction(ctx context.Context, id, optionID string) error
	SelectAccount(ctx context.Context, name string) error
	SwitchPlugin(ctx context.Context, name string) error
}
```

`Model` 不再持有 `*engine.Engine`、`*session.Manager`、`*skill.Registry`、PluginController 或 RuntimeView。

## 输入路由

application 统一解析用户输入，避免 TUI 和 Electron 各实现一套：

```text
空输入        -> no-op
/command ...  -> CommandRegistry.Execute
#skill ...    -> SkillPort.Get -> 生成带 Skill prompt 的 Chat request
普通文本      -> ChatCoordinator.Send
```

同时提供显式方法供 Electron 菜单调用，例如 `SwitchPlugin`、`SelectAccount`。显式方法最终复用同一 use case，不复制逻辑。

## Command Registry

当前全局 `commands.registry` 改成实例：

```go
type Command interface {
	Name() string
	Description() string
	Execute(ctx context.Context, args []string) (CommandResult, error)
}

type CommandResult struct {
	Notice      string
	Exit        bool
	Interaction *Interaction
	Refresh     RefreshMask
}

type CommandRegistry struct {
	commands map[string]Command
}
```

`/sessions`、`/pool` 不应把终端格式字符串作为唯一结果；它们返回 `Interaction` 或结构化 rows，TUI 和 Electron 分别渲染。`/help` 等纯文本命令可以继续返回 Notice。

## ApprovalBroker

当前 `tui/approve.Ask` 是同步阻塞入口，必须从 TUI 移出：

```go
type ApprovalBroker struct {
	mu      sync.Mutex
	pending map[string]chan ApprovalDecision
	events  *EventHub
}

func (b *ApprovalBroker) Request(
	ctx context.Context,
	req ApprovalRequest,
) (ApprovalDecision, error)

func (b *ApprovalBroker) Resolve(
	id string,
	decision ApprovalDecision,
) error
```

`Request` 发布 `interaction.opened` 后等待对应 request ID；context 取消、超时或 application Shutdown 时必须解除等待。TUI 和 Electron 都只负责显示请求并调用 Resolve。

`main.registerAskApprove` 改为注入 `ApprovalBroker`，不再 import `tui/approve`。

## Chat 与 Engine hook

1. application 为每次 Chat 生成 `requestID` 和可取消 context。
2. ChatStream 的文本回调写入 application state 并发布 `message.delta`。
3. Engine tool hooks 写入实例 EventSink，不访问包级 `streamEventCh`。
4. tool complete 后 application 刷新 RuntimeState，覆盖插件被 Agent 自主切换的情况。
5. Chat 结束后发布最终状态；错误转换为稳定的 application error code 和用户消息。
6. 默认同一 Session 只允许一个活动 Chat；后续多 Session 并发时按 session ID 建 coordinator。

装配顺序：

```go
hub := application.NewEventHub()
broker := application.NewApprovalBroker(hub)
hooks := application.NewToolHooks(hub)
eng := engine.New(runtime.Agent(), engine.WithHooks(hooks), ...)
app := application.New(application.Dependencies{
	Engine: eng, Runtime: runtime, Plugins: plugins,
	Skills: skills, Sessions: sessions,
	Events: hub, Approval: broker,
})
model := tui.NewModel(app)
```

## Electron transport

推荐第一版采用 **Go sidecar + JSON-RPC 2.0 over stdio**：

- Electron main process `spawn()` Seelex backend。
- stdin 发送请求，stdout 只输出 JSON-RPC response/notification。
- 日志严格写 stderr，避免破坏协议。
- 不监听本地 TCP 端口，部署简单且默认不暴露网络入口。
- Chat/tool/approval 事件使用 JSON-RPC notification。
- 请求方法包括 `app.snapshot`、`chat.submit`、`chat.cancel`、`command.execute`、`completion.list`、`interaction.resolve`、`plugin.switch`、`account.select`。

如果未来需要多窗口共享或远程前端，再增加 WebSocket adapter；application core 不变化。

协议必须包含：

- `protocol_version`
- `capabilities`
- request ID / application request ID
- 稳定 error code，不只传 Go error string
- shutdown/handshake
- snapshot resync

## 实现步骤

| # | 步骤 | 主要文件 | 验收标准 |
|---:|---|---|---|
| 1 | 为当前 TUI 建 characterization tests | `tui/*_test.go` | 普通输入、command、Skill、stream、tool、selector、approval 行为被锁定 |
| 2 | 定义 application DTO、ports、Snapshot、EventHub | `application/{ports,state,event}.go` | 无 Bubble Tea/Lipgloss import；race-safe；事件顺序测试通过 |
| 3 | 抽取 Completion 和实例化 CommandRegistry | `application/{completion,command}.go` | 删除全局 registry/cache；命令与建议可独立测试 |
| 4 | 抽取 Input Router 和 Skill 执行 | `application/input.go` | TUI 只传 raw input；解析行为与当前一致 |
| 5 | 抽取 ChatCoordinator 和 tool hook | `application/chat.go` | 无全局 stream channel；增量、完成、错误、取消顺序正确 |
| 6 | 抽取 ApprovalBroker | `application/approval.go`, `main.go` | `main` 不再 import `tui/approve`；超时/取消/重复 resolve 有测试 |
| 7 | 抽取 Session、Account、Plugin use cases | `application/app.go` | 操作后统一更新 Snapshot 并发布事件 |
| 8 | 将 TUI Model 改为 AppController adapter | `tui/{model,event,input}.go` | Model 不再依赖 engine/plugin/skill/session/seelebridge |
| 9 | 将 Cell/Conversation 拆成 DTO renderer | `tui/state.go`, `tui/component_*.go` | application DTO 无 Render；TUI snapshot 可重建所有画面 |
| 10 | 删除旧全局状态与兼容桥 | `tui/stream.go`, `tui/command.go`, `tui/dialog.go` | `rg` 找不到五个包级可变状态；race test 通过 |
| 11 | 更新 main 装配和 README/架构文档 | `main.go`, `README.md`, `docs/*` | 生命周期顺序清晰；Seele 未修改 |
| 12 | 可选：实现 Electron JSON-RPC sidecar | `transport/jsonrpc/*`, `cmd/seelex-backend/*` | Node/Electron 测试进程可收 snapshot、stream 和 approval event |

## 垂直迁移顺序

不要一次性重写整个 Model。按以下顺序逐块迁移，每块都可回滚：

1. Completion/command catalog（低风险、纯逻辑）。
2. Input router/Skill（低中风险）。
3. Runtime/Plugin/Session use cases（中风险）。
4. Chat stream/tool event（高风险）。
5. Approval broker（高风险）。
6. TUI Snapshot renderer 收口。
7. 删除旧桥接和全局变量。

迁移期可让 `Model` 同时持有旧字段和 `AppController`，但每完成一个 use case 就删除对应旧字段，禁止长期双轨。

## 测试策略

### Application 单元测试

- `Submit` 对空输入、普通文本、slash command、Skill 的路由。
- CommandRegistry 重名、未知命令、类型化结果。
- Completion 的 command/tool/skill 合并、优先级、大小写和空前缀。
- Chat 文本增量、错误、取消、重复提交。
- tool start/complete 匹配和乱序容错。
- Plugin/Account/Session 操作后的 Snapshot 与 Event。
- Approval resolve、取消、超时、重复 ID、Shutdown。
- EventHub 多订阅者、慢订阅者、跳号重同步。

### TUI 单元测试

- KeyMsg 到 application 方法调用。
- WindowSize/scroll/focus/selection 只修改 UI state。
- Snapshot 到 Conversation/status/approval/selector 的渲染。
- application event 到 `tea.Msg` 的订阅桥。
- TUI 不直接启动 goroutine 调 Engine。

### 集成测试

- fake Engine 发三段文本 + tool start/complete，TUI 最终视图一致。
- Plugin 切换后 RuntimeState、工具和 Skill 建议同时更新。
- ask_approve 从 tool handler 阻塞，TUI resolve 后继续。
- application Shutdown 能取消活动 Chat 和所有 pending approval。
- `go test -race ./application ./tui` 无 data race。

### Electron transport 测试

- 启动 sidecar，handshake 后读取 snapshot。
- 发送 chat.submit，按 seq 收到 delta/complete。
- approval notification -> interaction.resolve -> Chat 继续。
- 非法 JSON、未知 method、协议版本不兼容返回稳定错误。
- stdout 无非协议日志。

## 质量门槛

- `application` 覆盖率目标不低于 85%。
- `tui` 关键 reducer/input adapter 覆盖率不低于 70%。
- `go vet ./...`、`go build ./...`、`go test ./...` 通过。
- `go test -race ./application ./tui ./plugin ./skill` 通过。
- `application` 和 `transport` 不 import Bubble Tea/Lipgloss。
- `tui` 不 import `engine`、`plugin`、`skill`、`session`、`seelebridge`。
- 无 application/TUI 包级可变 registry、channel 或 pending request。

## 回滚方案

1. 每个 use case 独立提交，保留旧 TUI 路径直到对应 application 测试和 TUI adapter 测试通过。
2. `tui.NewModel` 在迁移期可保留旧构造器，新建 `NewModelWithApp`；最终切换后删除旧构造器。
3. Electron adapter 是 application 的独立消费者，可单独回滚，不影响 TUI。
4. Event DTO 在 Electron 接入前仍可修改；接入后通过 `protocol_version` 管理兼容性。
5. 不对 Seele 做 fork、replace 或本地 patch，回滚只涉及 Seelex 仓库。

## 最终验收

```text
tui imports:
  application + Bubble Tea/Lipgloss only

application imports:
  standard library + Seelex domain ports/DTO adapters
  engine direct use allowed
  no tui imports

Electron:
  only protocol DTO/JSON-RPC
  no Go/TUI implementation knowledge
```

完成后，新增任意前端的成本应主要集中在输入映射和组件渲染，而不是重新实现 Chat、Plugin、Skill、Session 和审批流程。

