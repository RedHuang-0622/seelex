# 并发、依赖与模块解耦审查

> 审查日期：2026-07-24
> 代码基线：`b97d48f`（`gui` branch）
> 范围：Go application/runtime/adapters、Wails frontend、现有 GUI 文档与 Agent Workbench 规划

## 1. 结论

当前单会话桌面 alpha **有条件通过**：Go import 图无循环，Application Core 与 TUI/GUI 的主依赖方向合理，现有普通并发测试覆盖了 EventHub、Approval、Chat、PromptStack、Snapshot 和 Plugin 串行切换。

在开启多会话、HTTP 或 generation 自动恢复前，以下问题必须先解决：

1. Service 级 runtime 操作没有统一单写者，Plugin/Session/Effort/Account 与 Chat 可并发交错。
2. history 分页和 session 恢复存在 check/use 分离的逻辑竞态。
3. MCP breaker listener 生命周期会按 attach 重复启动，且首次初始化没有同步边界。
4. 多处锁内执行外部后端、回调或磁盘 IO，放大阻塞和重入死锁面。
5. MCPStack 在解锁后暴露内部元素指针，破坏“immutable/thread-safe”承诺。

因此本轮判断不是“代码已支持并发多会话”，而是“当前分层可作为迁移基线，必须先引入 SessionActor/operation serialization 和明确生命周期”。

## 2. 审查方法与证据

- `go list -f '{{.ImportPath}}|{{.Imports}}' ./...`：检查实际 package DAG；Go 编译通过意味着无直接 import cycle。
- `docs_contract_test.go`：编译全部 JSON Schema、验证示例、检查模块 ID/文档/未知依赖/有向环。
- 静态检查 mutex、goroutine、channel、callback、外部 IO 和状态提交边界。
- 检查 `application/race_test.go`、Plugin/Session/Bridge/MCPStack 测试覆盖。
- 定向 race 命令已尝试；本机先因 `CGO_ENABLED=0` 拒绝，显式开启后因缺少 `gcc` 无法构建。该限制不等于 race 通过，最终质量门禁仍要求在带 C 编译器的 runner 执行。

## 3. 发现的问题

### P0：Service runtime 操作缺少统一串行边界

位置：`application/app.go:193-255`、`application/app.go:392-433`。

`SwitchPlugin` 在 Service 锁外激活/停用 Plugin、清空 Engine History、重置 PromptStack 并替换 `effortManager`；它没有拒绝正在运行的 Chat。`SelectAccount` 和 `SwitchEffort` 也能与 Engine 调用交错。`resumeSession` 只在加载前检查一次 `Chat.Running`，外部存储读取期间另一个 goroutine 可以开始 Chat，然后恢复路径再替换 Engine History。

这不一定立即表现为 Go 内存 race，因为子对象各自可能有锁，但会产生跨对象非原子提交：Conversation、Engine history、Plugin tools、PromptStack 和 Runtime Snapshot 可能来自不同状态时刻。

建议：当前单会话先增加 Service operation serializer 或 actor mailbox，把 Submit、resume、plugin/account/effort、cancel、interaction 和 shutdown 变成命令；不要简单扩大 `Service.mu` 覆盖 Engine/MCP/IO。多会话时每个 SessionActor 独立拥有 Engine、PromptStack、queue、approval 和 runtime。

### P0：分页与恢复存在 TOCTOU

位置：`application/app.go:392-479`。

`LoadMoreHistory` 在锁内读取 `offset/sessionID`，锁外加载，再无条件 prepend。两个并发请求可读取同一 offset 并重复插入；加载期间 resume 到另一 session 时，旧 session 的历史可能提交到新 Snapshot。`resumeSession` 也存在“检查未运行 → 慢加载 → 替换历史”窗口。

建议：读取时捕获 `(session_id, revision, history_offset, generation_id)`，IO 后在同一提交点 compare-and-swap；不匹配则丢弃结果并返回 typed stale error。客户端对同 scope 合并相同分页请求只是优化，Core 仍必须正确。

### P0：MCP breaker listener 生命周期不受控

位置：`seelebridge/mcp.go:32-61`。

每次 `AttachMCP` 都执行 `go mcpstack.ListenBreaker(...)`，多个长期 goroutine 竞争消费同一个 channel；detach 不停止 listener，Runtime shutdown 也没有显式 close/wait。`BreakerEvents` 对 `r.breaker == nil` 的首次初始化本身没有 mutex/once 包裹整个指针创建，若被并发调用会产生 data race 风险。

建议：Runtime 构造时创建 breaker state；使用单一 `sync.Once` 启动 listener，并由 Runtime context/cancel/waitgroup 管生命周期。一个 channel 只能有一个 trace consumer；shutdown 明确停止并等待。

### P1：锁内执行外部调用与 IO

位置：

- `application/app.go:296-308`：持 `Service.mu` 调用 Runtime/Engine/Plugin/Skill ports；
- `plugin/manager.go:88-139`：持 Manager 锁完成 MCP attach/detach、Tool/Skill backend 事务；
- `session/manager.go:40-57`：持锁执行注入的 save/load callback；
- `mcpstack/stack.go:149-208`：持写锁执行 auto-save 磁盘 IO；
- `mcpstack/persist.go:39-44`：持读锁覆盖整个文件保存。

这些路径当前用锁换取顺序一致性，但未知 backend callback、慢 MCP/磁盘和锁顺序会形成长临界区。Session callback 若重入 Manager 会自锁；Plugin backend 若反向查询 Manager 也会重入。

建议：锁内只快照状态/函数指针和提交内存结果，外部调用放到锁外；需要原子副作用时使用显式 prepare/commit/rollback 状态机和 operation token，而不是把互斥锁当事务。

### P1：MCPStack 暴露内部可变指针

位置：`mcpstack/stack.go:174-238`。

`Undo`、`Redo`、`Current`、`Peek` 返回 `&s.Calls[index]`，锁释放后调用方可修改内部 slice 元素，并与 Record/查询并发。`WithTags` 也直接保存调用方 map。该行为与包注释中的 immutable/thread-safe 承诺冲突。

建议：返回 `MCPCall` 值或深拷贝；输入 `RawMessage`、Tags 等可变引用在边界复制。需要 mutation 时提供受锁保护的专用方法。

### P1：Application Core 仍依赖具体 Engine hook 类型

位置：`application/chat.go:10`、`application/chat.go:356-425`。

`ToolHookBridge` 让 `application` import Seele `engine` 具体包。运行回调路径是 Engine → ToolHookBridge → Service，而 Application 又依赖 Engine hook DTO，形成概念回边，削弱了“无框架核心”的边界。

建议：把 Seele hook adapter 移到 `seelebridge` 或 composition root；Application 只暴露 `OnToolStarted/Completed` 等本地 port/command DTO。

### P1：Context/MCP 存在被代码注释承认的概念回环

位置：`mcpstack/provider.go:6-12`。

实际 import DAG 为 `seelexctx/provider → seelebridge → mcpstack → seelexctx/snapshot`。它没有形成 Go cycle，但基础 trace stack 反向依赖 context snapshot DTO；源码注释明确说明若直接 import provider 会成环。这是模块职责未完全解开的信号。

建议：把 MCP→Context 转换移入 `seelexctx/provider` adapter，或提取只含稳定 Context DTO 的中立 contract 包；`mcpstack` 只暴露自己的不可变 Snapshot/summary。

### P1：Approval 是单槽 UI 模型，不支持规划中的并发 session

位置：`application/approval.go`、`application/app.go:274-293`。

Broker map 可以保存多个 pending，但 Service Snapshot 只有一个 `Interaction`，observer 也只有一个。任一 timeout/remove 调用 `observer(nil)` 会清空当前展示，即使其他 pending 仍存在。当前单 Engine 下可受控，多 SessionActor 下会串审批。

建议：Interaction 属于 session scope；以 `(session_id, interaction_id, expected_revision)` 路由。Workbench 只保存摘要/badge，不共享全局 pending slot。

### P2：EventHub 与 Bridge 的背压/关闭边界可改进

位置：`application/event.go:81-102`、`gui/bridge.go:109-161`。

EventHub 的 channel send 使用 non-blocking 分支，当前不会因普通满 buffer 永久阻塞；但它在全局锁内遍历、清空和投递所有 subscriber，高频 delta 下锁持有时间随订阅数增长。Bridge `stop` 无界等待 emitter goroutine；若外部 emitter 阻塞且忽略 context，关闭可能卡住。

建议：EventHub 使用受控 subscription slot/单写 fan-out，在不引入 close/send race 的前提下缩短全局锁；Bridge 规定 emitter 必须 context-aware，并为 shutdown 增加诊断和有界策略。

### P2：错误处理与配置可预测性

- `EventHub.Publish` 忽略 `json.Marshal` 错误，可能发布 kind 正确但 payload 为空的 Event。
- `effortPrompts`、`effortLoops` 和 `orderedLevels` 是包级可变 map/slice；当前只读但无法由类型系统保证，且两份 map 可能漂移。
- `app.go` 481 行、`app.js` 500 行已到工程上限；继续加入 Workspace/Session/Card 会让 composition 与领域协调混合。

建议：不可序列化 payload 返回错误或发布明确 internal error；Effort 使用不可变表/单一 slice；按 use case/controller 拆文件与对象，但不要为了行数创建相互持有 Service 的小包。

## 4. 循环依赖结论

### 编译依赖

当前 Go package import 图无环：

```text
gui ─────────► application
tui ─────────► application
plugin ──────► skill ─────► internal/frontmatter
session ─────► seelebridge ─────► mcpstack ─────► seelexctx/snapshot
seelexctx/provider ──────────────► seelebridge
main ────────► composition implementations
```

规划模块登记图也通过自动 DAG 检查。

### 运行时/概念回边

| 回边 | 是否合理 | 处理 |
|---|:---:|---|
| Engine callback → Application，同时 Application import Engine hook | 否 | adapter 移到 `seelebridge` |
| Session Manager → injected Engine callbacks | 有条件 | 锁外调用，改为 repository/runtime ports |
| Approval Broker → Service observer | 当前可用 | 多 session 改 scoped event/actor command |
| mcpstack → context snapshot，context provider → seelebridge → mcpstack | 否 | 转换 adapter 上移到 context 层 |
| GUI event → reducer → Snapshot refresh → Bridge | 合理反馈环 | 这是协议恢复流程，不是依赖环；用 seq/revision 限制 |

## 5. 代码模块分块审查

| 模块 | 内聚/边界 | 解耦结论 | 建议 |
|---|---|---|---|
| `main` + root adapters | 装配职责正确，但文件承担大量工具/adapter 映射 | 基本合理 | 保持唯一 composition root；按 adapter 文件拆，不下沉业务规则 |
| `application` | 状态机、commands/chat/approval/event 集中，业务内聚 | 方向正确，仍被 Seele hook 泄漏 | 引入单写 command 边界；Hook adapter 外移；按 use case 拆内部组件 |
| `gui` | caller-owned `Application` 接口窄，Wails 生命周期集中 | 良好 | 加 emitter shutdown contract；v2 使用 explicit session methods |
| `tui` | Bubble Tea 单线程渲染，只消费 Application Snapshot/Event | 良好 | 保持 UI 局部状态，不重新持有 Engine/runtime |
| GUI frontend | reducer、client state、render、markdown、effort 已拆 | 基本合理，`app.js` composition 过重 | 拆 session/runtime/interaction controller；视图不调用彼此领域逻辑 |
| `plugin` | Tool/MCP/Skill 事务聚合职责清楚 | 领域内聚，但锁和外部副作用耦合 | 两阶段 operation/状态机；backend 必须可取消、可恢复 |
| `skill` | Registry/Loader 独立，依赖 frontmatter | 合理 | 保持 immutable Skill DTO；与 Plugin 通过接口组合 |
| `session` | 当前是 Store + Engine callback 薄封装 | 解耦不足 | 演进为 Session/Generation Repository；DTO 不直接依赖 `seelebridge` |
| `seelebridge` | 对 Seele 的 anti-corruption facade 有价值 | 基本合理，生命周期同步不足 | Runtime 显式 mutex/once/cancel/wg；不让上层拿 concrete Agent/Engine |
| `mcpstack` | trace、查询、持久化集中 | 边界被 Context DTO 穿透，immutable 承诺不完整 | 返回深拷贝；Context adapter 外移；IO 锁缩短 |
| `seelexctx` | snapshot/provider/compactor/merger 有领域分块 | root re-export 与 concrete Engine 依赖偏重 | 保留有产品语义部分，逐步移除纯别名；Provider 依赖窄 Engine port |
| `internal/frontmatter` | 通用解析，无业务反向依赖 | 良好 | 保持内部叶子模块 |

## 6. 规划模块分块审查

| 模块 | 判断 | 必须保持的边界 |
|---|---|---|
| Presentation/Card | 合理独立变化轴 | 不 import GUI；Card action 不直接调用 Workspace 实现 |
| Workspace sandbox | 合理独立安全边界 | PathGuard/Core permission 是事实源；GUI 只持 opaque ID |
| SessionActor | 是多会话正确性的必要边界 | 单 session 单写；Engine/Prompt/Approval 不跨 actor 共享 |
| WorkbenchCoordinator | 合理的页面/资源协调层 | 不解释 conversation/tool/card；锁内不调用 actor/IO |
| SessionScheduler | 应独立于 Actor 状态机 | 只管理 permit/fairness/cancel，不持 session 业务状态 |
| Generation Repository | 合理且应与 Engine 分离 | immutable/CAS/manifest；不解释 Conversation 内容 |
| HTTP adapter | 应与 Wails 并列 | transport DTO、auth、cursor、idempotency 在 adapter；业务规则留 Core |
| E2E runner | 合理的验证层 | fake/scripted Engine，不让生产代码依赖测试模块 |
| Evidence-gated Dev loop | 合理的跨阶段编排层 | Retriever/Assessor/Policy/Review/Executor 仅通过 ports；不让 RAG 分数直接控制代码写入 |

该分块整体合理且无计划依赖环。最容易出现的错误是把 SessionActor、Scheduler、Repository 全塞回一个更大的 `application.Service`，或反过来拆成互相持有的微包。推荐先在 `application` 内形成窄接口和可测试组件，稳定后再按真实变化频率决定物理 package。

## 7. 修复优先级

| 优先级 | 事项 | 放行条件 |
|---|---|---|
| P0 | Service operation serialization + resume/page CAS | 并发 submit/switch/resume/page/shutdown 定向测试通过 |
| P0 | MCP breaker 单 listener 生命周期 | attach/detach/shutdown 无 goroutine 泄漏，race 通过 |
| P0 | SessionActor 隔离后再开启 `max_running > 1` | Engine/Prompt/Approval/Workspace scope 不串状态 |
| P1 | 锁外 external call 与 MCPStack defensive copy | 重入、慢 backend、pointer mutation 测试通过 |
| P1 | Hook/Context 概念回环解耦 | Application 不 import concrete Engine hook；mcpstack 不依赖 context DTO |
| P1 | generation crash consistency | 每个崩溃点、坏 hash/current/父链恢复测试通过 |
| P2 | Event fan-out、Bridge shutdown、文件拆分 | 压测/关闭诊断与工程上限满足 |

## 8. 最终判断

- 当前单会话 alpha：**有条件通过**，可继续维护；不能宣称支持并发多 session。
- import/模块登记循环依赖：**通过**，未发现直接环。
- 架构解耦：**B+**，Application/adapter 主方向正确，但 Engine Hook、Context/MCP 和 callback lock 存在概念回边。
- 并发性：**B-**，已有锁与 race 用例基础较好，但跨对象逻辑竞态和生命周期问题是多会话前阻断项。
- 规划模块分块：**A-**，边界总体合理，前提是严格执行 caller-owned ports、SessionActor 单写和 generation repository 隔离。
