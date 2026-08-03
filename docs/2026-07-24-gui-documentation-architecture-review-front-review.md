# 前置审查报告：GUI/Agent Workbench 文档治理与架构审查

## 需求摘要

把现有 GUI/Agent Workbench 设计资料整理为稳定、可校验、可运行的文档交付包，并审查当前代码与规划架构的并发安全、依赖方向、模块边界和解耦程度。

## 当前基线

- 长期 GUI 文档位于 `docs/gui/`，已有 9 份模块详设、决策记录、CI 说明和旧版代码审查。
- Agent Workbench 总体架构位于 `docs/arch/agent-workbench-architecture.md`，其中已经描述 protocol v2、Workspace、DSL Card、多会话、持久化、恢复和依赖方向，但与 `docs/gui/` 的交付入口分离。
- 当前仓库没有 `docs/gui/architecture.md`、`module_dotting.json`、`schemas/`、`api/`、`examples/`、`recipes/` 和 GUI 子系统级 `CHANGELOG.md`。
- `docs/gui/modules/` 同时包含“已实现 v1”与“规划 v2”模块，但缺少统一状态字段、契约引用和依赖登记，读者容易把规划能力误认为当前实现。
- Go 包 import 图能够通过 `go list ./...`，不存在编译级 import cycle；架构级回环仍需结合接口、回调和运行时装配审查。

## 影响文件清单

| 文件路径 | 修改类型 | 具体位置 | 修改原因 |
|---|---|---|---|
| `docs/gui/architecture.md` | 新增 | 全文 | 收口总体架构、数据流、generation 发布模型、恢复语义和通用数据结构 |
| `docs/gui/module_dotting.json` | 新增 | 全文 | 机器可读登记模块职责、状态、接口、计划路径、输入输出和依赖 |
| `docs/gui/modules/*.md` | 修改 | 文档头部、契约与依赖章节 | 统一模块状态、边界、依赖方向、实现路径和验收链接 |
| `docs/gui/schemas/*.schema.json` | 新增 | 全部 Schema | 冻结对外及跨模块 JSON 契约，不以示意代码替代 |
| `docs/gui/api/*.md` | 新增 | 全文 | 定义 HTTP API、安全、分页、错误、幂等和 Snapshot/Generation 语义 |
| `docs/gui/examples/*.json` | 新增 | 全部示例 | 提供与 Schema 一一对应并可自动校验的 payload |
| `docs/gui/recipes/*.md` | 新增 | 全文 | 提交、回滚、重建、故障恢复和兼容迁移流程 |
| `docs/gui/CHANGELOG.md` | 新增 | 全文 | 记录设计契约的重要变更及兼容性影响 |
| `docs/gui/README.md` | 修改 | 索引、状态说明、维护规则 | 将新目录设为权威入口，区分已实现与规划能力 |
| `docs/gui/code-review.md` | 修改或拆分 | 五轴审查、问题清单 | 更新为本轮并发、依赖、边界和解耦审查结论 |
| `docs/arch/agent-workbench-architecture.md` | 修改 | 入口说明/链接 | 保留历史推演，避免与新权威架构文档形成双重事实源 |
| `docs_contract_test.go` | 计划新增 | GUI 文档契约测试 | 编译所有 JSON Schema，并验证 `examples/` 的对应 payload |
| `go.mod` / `go.sum` | 可能修改 | JSON Schema 校验依赖 | 若复用仓库现有间接依赖，`go mod tidy` 会调整直接/间接标记 |

## 依赖分析

### 当前代码依赖方向

```text
gui ───────────────► application
tui ───────────────► application
application ───────► Seele engine（通过本地 ports 隔离大部分能力）
plugin ────────────► skill
session ───────────► seelebridge
seelebridge ───────► mcpstack ─────► seelexctx/snapshot
seelexctx/provider ─► seelebridge + seelexctx/snapshot
main ──────────────► 所有实现包（composition root）
```

Go 编译器已证明上述 import 图无直接环。规划架构必须继续遵守：接口定义在调用方，`main` 负责注入，`application` 不反向 import `gui`、`workspace`、`presentation` 或具体持久化实现。

### 文档依赖方向

```text
architecture.md
  ├─► module_dotting.json
  ├─► modules/*.md
  ├─► api/*.md
  └─► schemas/*.schema.json ◄── examples/*.json
                                  ▲
recipes/*.md ─────────────────────┘
```

Schema 是契约事实源；模块文档和 API 文档引用 Schema，不复制另一套字段定义。示例只承载演示，不承担契约定义。

## 循环依赖检查

- [x] 当前 Go import 图无循环依赖。
- [x] `gui.Application` 接口定义在调用方，Bridge 未依赖 `application.Service` 具体类型。
- [x] 当前 `application` 未 import `gui`、`tui`、`plugin`、`session` 或 `seelebridge` 具体实现。
- [ ] 规划的 Workspace/Card/SessionActor 必须通过 application-owned ports 接入，禁止实现包反向调用 application service。
- [ ] Schema 引用图需要自动检查 `$ref` 可解析及递归边界，防止契约级引用回环。
- [ ] generation 发布/恢复流程需要保证 manifest 只指向已完整落盘的不可变 generation，避免“当前指针 ↔ 写入流程”状态回环。

## 初步并发审查发现

| 风险点 | 位置 | 初步判断 | 后续验证 |
|---|---|---|---|
| PromptStack、EffortManager 与 Engine 变更未统一受 `Service.mu` 保护 | `application/app.go` 的 `submitConversation`、`SwitchEffort`、`SwitchPlugin` | 高风险；GUI Bridge 方法可能并发进入，存在数据竞争和跨对象非原子提交窗口 | `go test -race ./...`、并发切换/提交定向测试 |
| Plugin Manager 持 `mu` 调用 MCP/Tool/Skill 外部后端 | `plugin/manager.go` | 中风险；可保证串行事务，但慢调用、回调重入或未知后端锁顺序会扩大阻塞/死锁面 | 后端调用图、超时/重入测试、两阶段状态机评估 |
| Session Manager 持锁执行注入回调 | `session/manager.go` | 中风险；回调若重入 Manager 会自锁，且慢存储会阻塞回调更新 | 将函数指针锁内快照、锁外调用的可行性验证 |
| EventHub 在全局锁内遍历、清空并投递订阅 channel | `application/event.go` | 中风险；当前非阻塞分支避免常规阻塞，但订阅者数量和高频流事件会拉长全局临界区 | 压测、关闭/发布竞争、锁外 fan-out 方案比较 |
| Bridge `stop` 等待 goroutine，而 emitter 属于外部调用 | `gui/bridge.go` | 中风险；若 emitter 长阻塞且忽略 context，关闭等待无上界 | 生命周期超时策略与阻塞 emitter 测试 |
| 单 Service/单 Engine 与规划多 SessionActor 并存 | `application/` 与 `modules/multi-session-pages.md` | 架构风险；不能只在 UI 增加页签后并发调用当前 Service | per-session actor、scheduler、approval scope 与持久化隔离验收 |

## 模块边界初判

| 模块 | 初判 | 说明 |
|---|---|---|
| `application` | 边界基本合理，但偏大 | 已形成无界面核心；聊天、命令、会话、审批、运行时切换继续增长时需要按 use case 拆文件/内部组件，不应拆成互相持有 Service 的小包 |
| `gui` Bridge | 合理 | 调用方窄接口与生命周期集中，需补外部 emitter 阻塞语义 |
| GUI frontend | 需要继续拆分 | 现有 reducer/view/controller 方向正确；`app.js` 仍承担较多 composition、session、runtime、command 协调 |
| `plugin` | 职责清晰，事务边界过重 | 把 Tool/MCP/Skill 切换聚合为事务是合理的，但锁覆盖外部副作用，需要显式状态机或准备/提交阶段 |
| `session` | 适配层过薄且回调耦合 | 直接依赖 `seelebridge` DTO，并通过注入回调控制 Engine，未来 generation repository 应隔离存储模型与运行时模型 |
| `seelexctx` | 存在过度封装迹象 | 多处为 Seele 类型别名/薄转发；应以产品语义和稳定契约判断是否保留，而非只为隐藏 import |
| Workbench 规划模块 | 划分方向合理 | Presentation、Workspace、SessionActor、Scheduler 具备独立变化轴；必须以 ports、opaque IDs、scoped events 和 generation repository 解耦 |

## 风险预估

- 高概率 / 高影响：文档中的 protocol v2、HTTP API 与当前 v1 Wails Bridge 混淆，导致实现按错误契约推进。
- 中概率 / 高影响：多会话只扩 UI，不隔离 Engine、审批、取消和 Workspace 写入，产生跨会话污染。
- 中概率 / 高影响：generation manifest 在内容未完全落盘前发布，恢复到半成品状态。
- 中概率 / 中影响：JSON Schema 与 Go/JS DTO 漂移，示例“看起来正确”但实际不可消费。
- 中概率 / 中影响：锁内外部调用造成长尾阻塞或重入死锁，现有 race 测试不一定覆盖锁顺序问题。
- 低概率 / 高影响：Workspace 路径校验与实际打开之间发生 TOCTOU，越过项目根边界。

## 建议实施方案

1. 以 `docs/gui/` 作为 Agent Workbench 设计包的唯一权威入口；旧架构文档保留推演属性并链接到新入口。
2. 先冻结通用 envelope、error、pagination、snapshot、generation manifest、module registry 等 Schema，再写 API 和模块详设。
3. `module_dotting.json` 为每个模块登记 `status`（implemented/planned）、`owner_boundary`、`interfaces`、`inputs`、`outputs`、`depends_on` 和 `planned_paths`，并自动检查未知依赖与有向环。
4. generation 使用不可变目录 + 内容哈希 + 原子 manifest/current 指针；发布与恢复均以完整性校验为门槛。
5. HTTP API 与当前 Wails Bridge 明确为不同 adapter，共享 application contracts，不让 Bridge DTO 直接成为网络协议。
6. 新增文档契约测试，校验 Schema 本身、全部示例、`$ref`、模块依赖图及文档引用。
7. 完成文档后执行 `go test ./...`、`go test -race ./...` 和定向并发测试；最终审查按正确性、并发、架构、安全、测试五轴输出阻断项和建议项。

## 进入实施前的确认点

默认按以下口径实施：目标目录为 `docs/gui/`；HTTP API 作为规划中的远程 adapter 契约，当前 Wails Bridge 保持现状；本轮以文档、Schema、示例和验证测试为主，不主动重构已发现的并发代码问题，只在审查报告中给出证据与修复优先级。
