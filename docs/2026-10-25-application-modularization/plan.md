# Application 模块化修正方案与里程碑

日期：2026-10-25

状态：**方案，尚未开始实施**。本文记录 Application 层的目标边界、DDD 聚合、
契约驱动迁移方式、兼容层退场规则和分阶段实施周期；当前代码仍是事实来源。

## 1. 背景与基线

`backend`、`middle` 与 `contract` 已经形成基本单向依赖，局部能力可以按契约替换。
`application/core` 仍然同时承担应用用例、运行时状态机、读模型、事件投影、
工具钩子和兼容转调，是当前最明显的大包。

2026-10-25 实测基线：

- `application/core` 共 262 个 Go 文件，其中 76 个生产文件、186 个测试文件。
- 生产代码约 15,274 行，测试代码约 36,399 行。
- `Service` 有 171 个公开 `*Service` 方法。
- `application/core` 直接 import 38 个 `backend/*`、`middle/*` 或 `store` 内部包。
- 主要大型文件：
  - `chat.go`：1,157 行；
  - `session_history.go`：1,012 行；
  - `session_scope.go`：754 行；
  - `work_table.go`：682 行；
  - `service_snapshot.go`：482 行。

问题不是单纯的“文件多”，而是多个变化原因共享同一包、同一门面和同一组锁：

- 聊天、恢复、持久化、工具事件、Goal、Team、工作区与读模型相互影响；
- 前端为了一个能力必须依赖完整 `Service`；
- 验证一次跨域行为通常需要启动大量共享状态；
- 兼容方法与新路径长期并存，缺少删除条件；
- 高冲突文件集中在 `service*.go`、`session*.go` 与 `chat.go`。

## 2. 目标与非目标

### 2.1 目标

1. 保留稳定的 Application 入站 API，但不再使用完整的 171 方法门面作为内部依赖。
2. 用 DDD 聚合定义状态一致性边界，用契约驱动定义可替换缝。
3. 将应用用例按业务能力拆为独立小包；每个包只使用自己需要的端口。
4. Application 不得直接依赖 `backend/engine/assemble` 等具体实现。
5. 查询模型与命令路径分离：查询不参与共享命令锁和长事务。
6. 建立兼容层登记、调用方归零、废弃和删除的完整生命周期。
7. 最终删除 `application/core` 大型包，而不是把它换名。

### 2.2 非目标

- 不在本轮重新设计 Seele Engine。
- 不把 `backend`、`middle` 已有领域全部迁入 Application。
- 不采用全量事件溯源。
- 不一次性重写聊天状态机。
- 不为拆分而拆分，不按技术词汇创建 `managers/`、`helpers/`、`models/` 大杂烩。
- 不在迁移批次中顺带修改产品行为；行为修改必须独立提交。

## 3. 领域划分原则

### 3.1 DDD 负责“哪里必须一致”

DDD 在这里主要回答：

- 哪个状态必须原子变化；
- 哪个不变式只能由一处判断；
- 哪些变化可以最终一致；
- 哪个流程负责跨聚合协调。

聚合应是业务一致性边界，不是缓存容器，也不是把所有相关字段放进一个 struct。

### 3.2 契约驱动负责“怎么替换”

- 入站契约：Application API，只依赖 `contract/dto` 与协议模型。
- 出站契约：Engine、Session、Store、Workspace、Jobs 等宿主能力，继续由
  `contract` 或消费方窄端口表达。
- 具体实现：只在 `wiring/bootstrap` 中装配。
- UI 只能消费入站 API 和版本化 DTO，不直接识别具体 backend 类型。

### 3.3 依赖方向

```text
gui / tui / headless
        ↓
application/api
        ↓
application use cases / process managers
        ↓
contract + contract/dto + middle/protocol/model
        ↑
具体 backend / middle / store 实现
        ↑
wiring/bootstrap
```

Application 内部可以依赖领域端口，不得依赖具体执行实现。

## 4. 目标目录

```text
application/
  api/
    conversation.go
    session.go
    execution.go
    workspace.go
    goal.go
    policy.go
  conversation/
    aggregate.go
    turn_process.go
    restore.go
    query.go
  session/
    descriptor.go
    lifecycle.go
    switch.go
    query.go
  execution/
    workitem_query.go
    teammate_query.go
  workspace/
  goal/
  teamview/
  compat/
```

约束：

- `api/` 只放入站命令/查询接口及回执类型，不放状态机。
- `compat/` 只放在迁移期存在，最终删除。
- `conversation/` 与 `session/` 不能互相持有完整对象，只能通过窄端口协作。
- `execution/` 默认只做读模型；执行事务仍归拥有该状态的领域模块。
- 根 `application` 门面只做稳定导出和组装，不承载业务判断。

## 5. 聚合与应用流程

| 聚合 / 流程 | 负责的不变式 | 明确不负责 |
|---|---|---|
| `Conversation` | 每 Session 最多一个活动回合；输入队列冻结；conversation revision 单调；edit/fork CAS；restoring 状态 | 标题、workspace binding、团队计划 |
| `SessionDescriptor` | 预分配 SessionID；draft 可无引擎；归档状态；单一 workspace binding；fork lineage | 聊天正文、工具循环 |
| `WorkspaceProject` | 项目根、环境、Git/SSH 配置和安全边界 | 会话历史与 UI 投影 |
| `WorkItemExecution` | WorkItem、Session、workspace mode、job、清理中状态的对应关系 | mainagent 全局任务编排 |
| `Goal` / `TeamPlan` | 既有领域状态机，继续留在 `backend/multiagent` | 前端页面状态 |

Application 层只保留少量流程管理器：

1. `ChatTurnProcess`
   - 协调一次用户输入从受理、Engine 流、context、task、tool hook 到持久化和事件发布。
2. `SessionSwitchProcess`
   - 协调保存旧会话、冷恢复、workspace binding、视图 epoch 和恢复完成。
3. `WorkItemExecutionQuery`
   - 一次组装 WorkItem、Progress 和完整 SessionRecordPage。

流程管理器允许拥有短生命周期事务状态，不允许成为新的共享状态中心。

## 6. 兼容层退场政策

### 6.1 兼容层准入

新增兼容层必须同时登记：

- 旧接口；
- 新接口；
- 当前调用方；
- 删除里程碑；
- 归零验证；
- 回退方式。

兼容实现只允许一行转发。出现状态、分支、写盘或事件发布，说明迁移没有完成。

### 6.2 退场阶段

| 阶段 | 状态 |
|---|---|
| D0 | 已登记，新旧接口同时可用 |
| D1 | 新接口接入，旧接口变为薄转发 |
| D2 | 生产调用方归零，只保留兼容测试 |
| D3 | 标记 deprecated，停止新增调用 |
| D4 | 删除接口、DTO 别名与测试 |

### 6.3 首批退场项

| 旧面 | 新面 | 退场阶段 |
|---|---|---|
| active-session 隐式 `Submit/Cancel/Snapshot` | 显式 SessionID 的 Conversation API | D1-D4 |
| 171 方法 `Service` 使用面 | `ConversationAPI/SessionAPI/ExecutionQueryAPI/...` | D1-D4 |
| `assemble.*`、`goaldomain.*` 出现在 Application API | `contract/dto` | D1-D3 |
| `TeammateSessionLive*` | `ConversationSession` / `WorkItemExecutionView` | D1-D4 |
| `application` 的 DTO 再导出 | `contract/dto` / `middle/protocol/model` | D2-D4 |
| `application/core` | 小型 Application use-case 包 | D3-D4 |

## 7. 里程碑与改动周期

以下周期按“一名主实现者 + 一名评审者、每个批次独立合入”的节奏估算。
它是排期基线，不是强制发布日期；外部 API 验证或并行高冲突改动可能造成顺延。

| 里程碑 | 建议周期 | 目标 | 退出条件 |
|---|---|---|---|
| M0 基线与契约冻结 | 2026-10-26 至 2026-10-30 | 建立债务看板、API 分组和兼容台账 | 依赖、方法数、文件大小预算门禁可运行；无行为变化 |
| M1 入站 API 收窄 | 2026-11-02 至 2026-11-06 | 171 方法按能力拆为小型 API | GUI/TUI 至少一组改用子接口；旧门面仍可编译 |
| M2 契约净化 | 2026-11-09 至 2026-11-13 | 清除 API 中的 backend 具体类型 | Application API 只依赖 contract/protocol DTO；依赖数下降 |
| M3 查询模型抽出 | 2026-11-16 至 2026-11-27 | 抽出 Session/History/WorkItem/Teammate/Subagent Query | 查询不进入写锁；旧查询方法均为一层转发 |
| M4 Conversation 聚合 | 2026-11-30 至 2026-12-11 | 抽 chat state/queue、turn process、restore、edit/fork | 会话状态规则集中；聊天行为 parity 通过 |
| M5 Session 生命周期 | 2026-12-14 至 2026-12-25 | 抽 descriptor/catalog/draft、switch/resume、archive/delete | SessionID、binding、draft、归档不变式集中 |
| M6 Goal / Team / Subagent 用例 | 2026-12-28 至 2027-01-08 | 迁出目标、团队、子代理读模型与事件适配 | Application 不再直接识别具体 backend 实现 |
| M7 Policy / Workspace / Admin 收尾 | 2027-01-11 至 2027-01-22 | 收尾账号、权限、工作区、调度和装配入口 | `application/core` 生产规模显著下降 |
| M8 兼容退场 | 2027-01-25 至 2027-02-05 | 删除旧接口、旧 DTO、旧 active-session 路径 | 零生产调用；旧兼容测试删除；门面只剩窄 API |
| M9 终验与 `core` 删除 | 2027-02-08 至 2027-02-19 | 删除 `application/core`，验证目标依赖图 | 构建、race、E2E、前端门禁全部通过 |

## 8. 分阶段任务看板

### M0：基线与契约冻结

范围：

- 新增 Application 依赖预算测试；
- 冻结现有 171 个方法及其归属；
- 建立兼容层台账；
- 标出高冲突文件与串行修改窗口。

效果：

- 后续每个批次都能量化“债务减少”；
- 防止拆包过程中新增第二套实现。

回退：

- 仅删除新增测试与文档，不影响生产代码。

### M1：入站 API 收窄

范围：

- `application/faces.go` 拆分为按能力的接口；
- 保留现有五组接口作为兼容别名；
- 选择 GUI/TUI 的一个低风险调用方改用子接口。

效果：

- 新调用方不再依赖完整 `Service`；
- 接口成为后续拆包的真正目标边界。

回退：

- 恢复单一接口消费点；兼容接口不从 `Service` 删除。

### M2：契约净化

范围：

- 将 `assemble.ScheduledTaskSpec`、`goaldomain.*` 等移入 `contract/dto`；
- 修正 Application API 与 Bridge 的字段来源；
- 增加后端类型泄漏扫描。

效果：

- 入站 API 只依赖纯数据契约；
- 替换 scheduler、goal 或 team 实现不再改动前端契约。

回退：

- 保留旧类型别名一轮，只回退调用点。

### M3：查询模型抽出

子批次：

1. `SessionQuery`
2. `ConversationHistoryQuery`
3. `WorkItemExecutionQuery`
4. `TeammateInspectionQuery`
5. `SubagentDetailQuery`

效果：

- 只读页面不再通过写模型组件；
- 查询不持有共享命令锁；
- 前端详情页只消费查询接口。

回退：

- 每个 Query 独立回退到旧 `Service` 转发。

### M4：Conversation 聚合

子批次：

1. ChatState、queue、revision 和单会话 active-turn 规则；
2. `ChatTurnProcess` 的跨域顺序；
3. 冷恢复与历史窗口；
4. message edit/fork CAS。

效果：

- `chat.go`、`session_scope.go` 不再是应用总调度器；
- 对话状态只在一个聚合内变化；
- 查询和持久化继续通过端口。

回退：

- 保留旧 `Service` 转发方法，按子批次独立回退。

### M5：Session 生命周期

子批次：

1. `SessionDescriptor`：ID、标题、meta、binding、archived；
2. `SessionLifecycle`：draft、activate、archive、delete；
3. `SessionSwitchProcess`：保存、恢复、切换和 epoch。

效果：

- Session 身份与 Conversation 内容彻底分层；
- 项目名只用于展示，ID 是唯一操作键；
- 一次 binding 一个 workspace 的不变式有唯一守卫。

回退：

- 三个阶段分别迁移、分别回退。

### M6：Goal / Team / Subagent

范围：

- Goal 用例；
- Team 看板与 WorkItem 查询；
- Teammate inspection；
- Subagent detail 与事件 hook 适配。

效果：

- Application 不直接 import 具体多代理实现；
- 多代理能力通过消费方端口接入。

回退：

- 保留旧事件入口作为薄适配器。

### M7：Policy / Workspace / Admin

范围：

- 账号、权限档位、limits；
- Workspace 用例；
- 调度；
- 工具注册入口和运行期策略。

效果：

- Application 只保留用户意图校验、调用顺序和回执；
- 领域规则继续留在拥有它们的模块。

回退：

- 按能力恢复旧转发表。

### M8：兼容退场

范围：

- 删除旧 active-session API；
- 删除旧 DTO 再导出；
- 删除 `TeammateSessionLive` 等被新读面替代的入口；
- 删除只服务旧接口的测试。

效果：

- 新调用方只依赖窄 API；
- 兼容层不再产生状态或业务行为。

回退：

- 每项独立 commit，可按接口恢复 shim，不恢复旧状态实现。

### M9：终验与 `core` 删除

范围：

- 删除 `application/core` 目录；
- 更新所有 README 与依赖图；
- 固化最终依赖闭包；
- 执行全量回归。

退出条件：

- 所有生产调用走新 API；
- `application/core` 不存在；
- 没有新增上帝包；
- 全仓测试、race 目标包、构建、vet、前端测试通过。

## 9. 并行车道

只有互不共享根 facade 的包可以并行：

| 车道 | 内容 | 并行条件 |
|---|---|---|
| API 车道 | `application/api`、DTO、接口冻结 | 必须串行修改根 facade |
| Query 车道 | Session/History/WorkItem/Teammate/Subagent Query | 独立包，可并行 |
| Conversation 车道 | Conversation 聚合与 Turn Process | 串行 |
| Session 车道 | SessionDescriptor/Lifecycle/Switch | 串行 |
| Policy 车道 | Goal/Team/Workspace/Admin 用例 | 独立包，可并行 |
| 兼容车道 | 旧方法转发、调用方迁移、删除 | 最后统一修改 |

高冲突文件：

- `application/application.go`
- `application/faces.go`
- `application/core/service.go`
- `application/core/service_components.go`
- `application/core/service_snapshot.go`

这些文件同一时刻只允许一个车道修改。

## 10. 每个小批次的门禁

所有迁移批次必须满足：

1. 一个批次只处理一个聚合或一个查询面。
2. 生产代码迁移建议不超过 2,000 行，公开方法不超过 20 个。
3. “纯搬运”和“语义修改”不得放在同一 commit。
4. 迁移前补 characterization test，迁移后补新旧 parity test。
5. 旧实现只能保留一行转发，不得保留第二份状态机。
6. 新增直接 backend 依赖必须被依赖预算测试拒绝。
7. 目标包新增 README，更新文件与函数索引。
8. 删除旧实现时同步修改门面冻结清单、Bridge 和 E2E。

## 11. 验收指标

### 11.1 结构指标

- Application 直接依赖的具体 backend 实现数：38 → 10 以下。
- `application/core` 生产代码：约 15,274 行 → 3,000 行以下，最终 0。
- 单个入站 API：建议不超过 30 个方法。
- 单个应用聚合生产文件：原则上不超过 500 行。
- 单个应用子包：不以“其他/杂项”为落点。
- 不存在子包反向持有完整 `Service`。
- 兼容包生产方法只允许一行转发。

### 11.2 行为指标

- Chat turn、切换、恢复、fork、WorkItem 执行的 DTO/Event 序列 parity；
- 冷恢复后不混入其他 Session 的历史；
- 同一 Session 不出现双 active turn；
- 查询路径不阻塞命令写路径；
- GUI/TUI 的 wire 字段在迁移期间保持兼容。

### 11.3 验证命令

```text
go test ./application/... -count=1
go test ./e2e -run 'Facade|Document' -count=1
go test ./backend/multiagent/teamwork ./backend/engine/assemble ./middle/view/sessionaccess -count=1
go test -race ./application/... -count=1
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
go test ./... -count=1 -timeout=120s
node --test gui/frontend/dist/*.test.mjs
git diff --check
```

## 12. 主要风险与缓解

| 风险 | 缓解 |
|---|---|
| 拆包后出现多个共享状态副本 | 每个聚合只有一个 owner；其他包只持窄端口 |
| 旧 `Service` 门面继续膨胀 | 方法数和依赖数只许下降；新方法必须进入小 API |
| Query 重新持有写锁 | Query 独立门禁：不得调用命令服务和聚合写方法 |
| Conversation 迁移造成长流阻塞 | 锁只保护状态切换；Engine 流和 I/O 在锁外 |
| 兼容层永久化 | 每项登记删除里程碑与调用方归零测试 |
| 并行开发冲突 | 根 facade 与高冲突文件单车道，领域包多车道 |
| 只搬文件不减少耦合 | 以依赖预算、方法数、锁跨度和 parity 测试作为验收 |

## 13. 决策记录

实施前需要明确并记录以下决策：

1. `Conversation` 是 Application 状态聚合，还是下沉到独立会话运行域。
2. `SessionDescriptor` 是否拥有 workspace binding，还是只保存 binding reference。
3. 旧 Wails Bridge 方法的冻结期和删除里程碑。
4. `application/core` 删除后，根 `application` 是否只保留 facade 和 API 别名。
5. 查询模型是否采用独立 Query service，还是在各用例包内提供只读接口。

这些决策应在 M0 冻结；M0 后不得在迁移批次中静默改变。

## 14. 推荐首批执行包

### AP-001：依赖与方法预算

- 新增 Application 直接依赖预算；
- 新增 171 方法冻结和兼容台账；
- 输出高冲突文件列表。

### AP-002：入站 API 子集

- 新增 `ConversationAPI`、`SessionAPI`、`ExecutionQueryAPI`；
- `Service` 继续实现全部接口；
- 不改生产调用路径。

### AP-003：API 契约净化

- 清除 Application API 中的 `assemble.*`、`goaldomain.*`；
- 改用 `contract/dto`；
- 增加后端类型泄漏测试。

### AP-004：SessionQuery 抽出

- 迁移会话目录、标题、binding、归档只读查询；
- 旧 `Service` 方法改为一行转发；
- 运行查询 parity 与锁行为测试。

### AP-005：ConversationHistoryQuery 抽出

- 迁移历史分页、冷读和 record 映射；
- 保持 offset/limit/total/has_more/truncated 契约不变；
- 禁止查询持有共享写锁。

首批不迁移 `chat.go`。聊天回合是最高风险路径，必须等 API、查询和契约边界稳定后再进入。
