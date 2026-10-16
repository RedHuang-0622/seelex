# wiring

## 生态位

`wiring` 是 Seelex 的**组合根（composition root）**：进程启动时把各层实现接线成
`contract` 端口，并把门面不再持有的构造函数收在一处。它是依赖注入的落点，不是任何
业务规则的落点。

主要调用方：根 `main.go`（真实进程装配）、根目录与前端测试基座
（`tool_full_chain_test.go`、`gui/tool_full_chain_test.go`、`e2e/scenario/harness.go` 等）。

物理位置：2026-10-16 按
[`../docs/2026-10-15-architecture-reshape/plan.md`](../docs/2026-10-15-architecture-reshape/plan.md)
§1.3 ④ 与 §2.2 的 M1-B 新建。门面 `application` 的
`New` / `NewEventHub` / `NewApprovalBroker` / `NewToolHookBridge` / `NewPromptStack` /
`NewEffortManager` / `WebSearch` 迁到这里，函数体与参数类型逐字不变（类型都是
`type X = Y` 别名，同一类型身份），**行为零变化**。

## 职责与非职责

- 做：构造应用门面对象、事件 hub、审批 broker、工具钩子桥、prompt 栈与 effort 管理器；
  把端口实现塞进 `contract.Dependencies`。
- 不做：不做权限判定、不持有会话事实、不实现工具路由、不渲染 UI、不在运行期改状态。
  这里只出现"谁被造出来"的装配事实。
- 不做：不承载业务判据。若某个构造参数需要默认值或校验规则，判据属于消费它的层
  （如 effort 档位归 `application/prompt`），本包只传参。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| [`constructors.go`](constructors.go) | 七个构造函数，逐一转发到唯一实现。 |

规划（尚未实现）：按 plan §2.5/§2.10，`internal/adapters/` 将搬到 `wiring/adapters/`、
`main.go` 的注册段将搬到 `wiring/registry/`；`WebSearch` 的终局是
`backend/tool/websearch`（plan §2.2，M4）。

## 核心实现

全部函数都是**一行转发**，实现只有一份，位于被转发包：

| 本包函数 | 唯一实现 |
|---|---|
| `New` | `application/core.New` |
| `NewEventHub` | `application/event.NewEventHub` |
| `NewApprovalBroker` | `backend/limit/approval.NewApprovalBroker`（端口形状在 `contract/approval.go`） |
| `NewToolHookBridge` | `application/core.NewToolHookBridge` |
| `NewPromptStack` | `application/prompt.NewPromptStack` |
| `NewEffortManager` | `application/prompt.NewEffortManager` |
| `WebSearch` | `seelebridge/search.WebSearch`（M4 迁 `backend/tool/websearch`） |

`NewApprovalBroker` 的形参是具体类型 `*event.EventHub` 而不是 `event.Hub` 接口：
调用方传 `nil` 时"接口持 nil 指针"与"nil 接口"语义不同，保持与迁移前逐字一致
（见 `permission_employee_panel_test.go` 的注释）。

## 数据流与生命周期

```text
main.go / 测试基座
    │  wiring.NewEventHub → wiring.NewApprovalBroker → wiring.NewToolHookBridge
    │  （先造共享设施）
    ▼
  store / seelebridge.Runtime 等实现就绪
    │  wiring.New(contract.Dependencies{...})
    ▼
*core.Service（门面 application.Service 的同一类型身份）
    ▼
前端只消费 Snapshot / Event / 用例方法面
```

装配顺序本身是契约：`Service` 的四个生命周期消费者在 `New` 里就把信号口读走，
因此编排面必须在 `wiring.New` **之前**注入（见 `tool_full_chain_test.go` 的
`fullChainBackendInstaller`）。

## 依赖方向

- 允许：依赖所有层（`application/*`、`contract`、`seelebridge/*` 等）。
- 禁止：被 `application`、`middle`、`backend`、`store`、`contract` 反向依赖。
  门面"包内无 `func New*`"（不变量 I3）与前端"依赖闭包 ⊆ {门面, 前端子包}"
  （I2）都依赖这条单向边。

## 纪律：装配优先于继承

- **一层只装配它需要的端口**：`Dependencies` 里没配的端口就是没配，实现里不得出现
  "按层/按主体判断"的分支（`if kind == ...`、`if role == ...`）。
- **层差异只允许出现在装配表与策略的调用点上**：例如 e2e 基座用桩实现、真实进程用
  `seelebridge.Runtime`，差异写在装配处，不写进被装配对象。
- **不新造第二份实现**：本包的每个函数都必须是转发（见「核心实现」表），
  出现实现体即为缺陷（`AGENTS.md` §8）。

## 并发与错误语义

- 构造函数本身不启 goroutine、不加锁；并发语义由被构造对象决定（如 `EventHub` 的
  订阅表、`ApprovalBroker` 的待批表）。
- `New` 返回的错误是装配期错误：缺必需端口或 prompt assets 嵌入失败时返回
  `(*core.Service, error)`，调用方（组合根）决定是否终止进程。

## 扩展方式

新增构造入口时：先确认它不属于任何既有层（否则应该放进那层的构造函数里），再在本包
加一行转发，并在上表登记唯一实现。**不得**在本包重写实现体。

## Review 指南

- 本包是否出现了判据、默认值或状态？（应属于被构造对象。）
- 构造函数是否仍是"一行转发 + 同一类型身份"？出现新类型或包装结构即违反了保形要求。
- 调用方是否仍有 `application.New*` 残留？（门面包内不应再出现 `func New*`。）

## 测试与验证

```text
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
go test ./... -count=1 -timeout=900s
```

本包无独立单元测试（全部函数是一行转发），由调用方测试覆盖：
`tool_full_chain_test.go`、`gui/tool_full_chain_test.go`、`gui/bridge_test.go`、
`tui/tui_test.go`、`e2e/scenario/`。文档门禁见 `e2e/layout_test.go`。

## 文件与函数索引

### constructors.go

- `func New(deps contract.Dependencies) (*core.Service, error)` — 构造应用 Service 并接线 `contract.Dependencies` 里的全部端口。
- `func NewEventHub() *event.EventHub` — 构造进程级事件 hub（唯一事件发布点，前端按 Subscription 消费）。
- `func NewApprovalBroker(events *event.EventHub) *limitapproval.ApprovalBroker` — 构造异步审批 broker；events 为 nil 表示不发布审批事件。
- `func NewToolHookBridge() *core.ToolHookBridge` — 构造工具钩子桥（工具调用前后的诊断与观测挂点）。
- `func NewPromptStack() *prompt.PromptStack` — 构造空的 prompt 栈。
- `func NewEffortManager(stack *prompt.PromptStack, engine interface{ SetMaxLoops(int); SetSystemPrompt(string) }) *prompt.EffortManager` — 构造 effort 管理器（推理档位与 ReAct 循环预算）。
- `func WebSearch(ctx context.Context, config search.WebSearchConfig, query string, maxResults int) (string, error)` — 执行一次 Web 搜索。
