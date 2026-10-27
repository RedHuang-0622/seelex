# Application 未完成项开发计划

日期：2026-10-27

状态：**执行中。已完成 AP-001～AP-107、AP-108A、AP-109～AP-111、AP-112、
AP-113A～AP-113C、AP-114A/114B、AP-115A/115B/115C1/115C2a/115C2b1/115C2b2/115C2c、
AP-115D1/115D2、AP-108B1/B2a/B2b、AP-113D1/D2、AP-116A～AP-116F2g、
EP-001～EP-090；`newService*` 构造适配审计、`Service` 公开业务方法退场与
Application 具体依赖归零均已完成。**

本文件只记录“尚未完成且下一步会实际开发”的工作包。当前代码与测试是实现状态的最终事实
来源；本文件不把规划写成已实现能力。

## 1. 当前基线

| 指标 | 当前 |
|---|---:|
| `Service` 公开方法 | 0 |
| `application/core` 直接内部依赖 | 0 |
| `application/**` 具体 Backend/Middle 依赖包 | 0，Store 为 0 |
| `application/core` 生产文件 | 55 |
| `application/core` 生产代码 | 7,178 行 |

本轮包提取起点为 126 个文件、18,723 行；当前净减 59 个文件和 8,770 行。

## 裁决已冻结

长期依赖与状态边界见
[`docs/arch/application-middle-backend-boundaries.md`](../../docs/arch/application-middle-backend-boundaries.md)。

- Session 聚合最终归 `application/session`；Middle 只保留纯策略/投影，
  Backend Engine 保留执行运行时；
- AP-003 分两条验收：入站 API 只用 Contract；整个 `application/**` 的具体
  Backend/Middle/Store 依赖另设预算并最终归零；
- `TeamworkPlan` 系列已完成完整 DTO 化，Store/Backend/Middle 保留 alias；
  `application/execution` 迁移类型门面已删除；
- Fork lineage 只消费 `forked_from` 私有窄 JSON 投影，
  `application/session` 不再 import Store 载荷类型；
- SessionState/ViewState 已迁入 `application/session/state` 与
  `application/session/viewstate`，`application/core` 直接内部依赖已归零。
- Workspace 切换用例已从 `middle/switch/workspace` 物理迁入
  `application/workspace/switch`，旧 Middle owner 已删除。
- Subagent View 已从 `middle/view/subagent` 物理迁入
  `application/execution/subagentview`，identity alias 与旧 Middle owner 已删除。
- WorkTable 纯投影已从 `middle/view/worktable` 物理迁入 `kernel/worktable`；
  Middle Board 与 Application 都消费同一中立 owner，避免反向依赖或重复实现。
- Error Presentation 已从 `middle/view/errors` 物理迁入
  `application/errorpresentation`，旧 Middle owner 已删除。
- PerfStats 计算已从 `middle/view` 物理迁入 `application/observability`。
- Session Switch 纯策略已从 `middle/switch` 物理迁入 `kernel/sessionpolicy`；
  Application、Backend 与 Wiring 共用同一中立 owner。
- Project Board 已从 `middle/view/board` 物理迁入 `kernel/board`；
  ProjectBoard row/scope/options 与 SessionKey 已下沉 Contract，Store 保留 alias。
- Limit 根包与 effort/loop/tier 已从 `backend/limit` 物理迁入 `kernel/limit`；
  Backend 只保留 approval/gate/overlay/policy/accountwindow 等宿主能力子域。
- Prompt runtime 与 assets 已从 `backend/engine/prompt` 物理迁入
  `kernel/prompt`。
- Context Window Policy 已从 `backend/engine/input/contextcontrol` 物理迁入
  `kernel/contextcontrol`。
- React stream surface 已从 `backend/engine/react` 物理迁入 `kernel/react`。
- Compat 调度入口已直接使用 Contract DTO，退出 Engine Assemble 具体类型。
- Compat Subagent 事件入口已直接使用 Contract DTO，退出 Backend subagent alias。
- RoleSessions 组合门面已从 Backend team 物理迁入 `kernel/rolesessions`。
- Goal Controller 已从 `backend/multiagent/goal` 物理迁入 `kernel/goal`。
- Context Coordinator 根包已从 `backend/engine/context` 物理迁入
  `kernel/context`。
- Session Coordinator 根包已从 `backend/engine/session` 物理迁入
  `kernel/session`；Application 具体依赖归零。
- 全局具体依赖预算见
  [`application/testdata/application_wide_dependency_budget.json`](../../application/testdata/application_wide_dependency_budget.json)，
  当前 0 个唯一 Backend/Middle 包；具体 Store 包亦为 0。

已迁出的领域包：

- `application/admin`
- `application/execution`
- `application/goal`
- `application/history`
- `application/input`
- `application/lifecycle`
- `application/observability`
- `application/policy`
- `application/reference`
- `application/review`
- `application/session`
- `application/session/query`
- `application/workspace`
- `application/conversation/search`

已落地结构门禁：

- `Service` 方法冻结与递减预算；
- core backend/middle/store 直接依赖白名单双向一致；
- core 生产文件/行数 ratchet；
- 已提取领域包禁止 import `application/core`、禁止持有完整 `Service`；
- 已迁移的兼容 Service 入口必须保持单行转发；
- facade 依赖闭包只能登记真实使用的新领域包。

## 2. 每个批次都必须满足

1. 先写会失败的结构/行为测试，再实现。
2. 纯搬运与语义修改分开。
3. 新包不 import `application/core`，不持有 `*Service`。
4. 尚未迁出的旧 `Service` 方法只保留一行转发；已迁移入口由 `Frontend` 同签名
   承接并直接调用唯一组件。
5. 查询路径不得进入共享写锁或执行慢 I/O。
6. 每批必须让 core 生产代码净下降；出现 adapter 增长时必须由下一子批偿还。
7. 每批同步目标包 README、主 Application README 和 `implementation.md`。
8. 不改 GUI/TUI wire；前端迁移前保留旧兼容入口。
9. 为迁移依赖方可以修改 `gui/**`、`tui/**`、`headless/**`、`wiring/**` 等调用点；
   共享 DTO、Event 或 wire 变更仍须保持兼容，并登记对应 Backend/Middle 责任批次。
10. 每批运行受影响包、结构门禁、E2E facade 和差异检查。

## 3. 后续工作包总览

| 批次 | 范围 | 目标包 | 风险 |
|---|---|---|---|
| AP-106 | Session Draft Entry / Materialization | `application/session` | 中 |
| AP-107 | Cold Restore 组件链 | `application/session/cold` | 高 |
| AP-108 | Fork lineage、Restoring shell、Session scope 查询 | `application/session` | 中高 |
| AP-109 | Scheduled Task CRUD / Run / Session | `application/admin/scheduler` | 高 |
| AP-110 | Policy 切换应用 | `application/policy` | 中高 |
| AP-111 | Context Compaction | `application/conversation/context` | 高 |
| AP-112 | Queue / ChatState / ChatTurn 请求工厂 | `application/conversation` | 高 |
| AP-113 | ChatTurn 执行流水线 | `application/conversation` | 很高 |
| AP-113D1 | Chat reasoning 投影（已完成） | `application/conversation` | 中高 |
| AP-113D2 | ChatTurn Runner（已完成） | `application/conversation` | 高 |
| AP-114 | Edit/Fork 应用链 | `application/conversation/edit` | 高 |
| AP-115A | Tool Hook Bridge | `application/execution/toolhooks` | 高 |
| AP-115B | Tool Record（已完成） | `application/execution/toolrecord` | 高 |
| AP-115C1 | WorkTable publisher（已完成） | `application/execution/table` | 高 |
| AP-115C2a | WorkTable TraceBlock 输入投影（已完成） | `application/execution/table` | 中高 |
| AP-115C2b1 | async/teamwork consumer（已完成） | `application/execution/table` | 高 |
| AP-115C2b2 | subagent/task consumer（已完成） | `application/execution/table` | 高 |
| AP-115C2c | WorkItem/批次输入投影（已完成） | `application/execution/table` | 高 |
| AP-115D1 | 基础命令注册（已完成） | `application/input/command` | 中高 |
| AP-115D2 | 会话 mutation 命令 / bridge（已完成） | `application/input/command` | 高 |
| AP-108B1 | Session Runtime 投影（已完成） | `application/session/query` | 中高 |
| AP-108B2a | Snapshot 冷/热路径（已完成） | `application/session/query` | 高 |
| AP-108B2b | Snapshot 驻留组装（已完成） | `application/session/query` | 高 |
| AP-116E | 8 个共用入口迁移到 `Frontend`（已完成） | `application` / 前端依赖方 | 高 |
| AP-116F1 | Admin/Role/Workspace 50 个转发壳迁出 core（已完成） | `application/compat` | 高 |
| AP-116F2a | 15 个只读会话/执行投影入口迁出 core，history 端口落地（已完成） | `application/compat` / `history` | 高 |
| AP-116F2b | Context/Reference/Review/Goal/Scheduler/Lifecycle 19 个方法退场（已完成） | `application/compat` | 高 |
| AP-116F2c | Session/Subagent 16 个入口迁出 core（已完成） | `application/compat` | 高 |
| AP-116F2d | Policy/Lifecycle/State 14 个入口迁出 core（已完成） | `application/compat` | 高 |
| AP-116F2e | 剩余 34 方法退场与旧 API 收回 | `application` / `compat` | 很高 |
| EP-046A | Core 组件 getter 转发退场 | `application/core` 内部调用点 | 中 |
| EP-046B | `newService*` 构造适配归位 | 已迁出的 Application 领域包 | 中高 |
| EP-046C | Core 入站接口定义退场 | `application` / `contract` | 中高 |
| EP-046D | Core 前端状态壳退场 | `application/compat` / `viewstate` | 高 |
| EP-046E | Core 组件单表达式转发退场 | `application/core` 内部调用点 | 高 |

## 3.1 EP-046：Core 转发面退场

状态：**EP-046A/B/C/D/E 均已完成。**

### 目标

`application/core` 只保留三类职责：

1. 跨领域会话命令协调、共享锁与原子状态转换；
2. 尚不能离开的宿主适配；
3. `Service` 组装、关闭和兼容入口。

已经由 `application/<domain>` 完成的工作，不得再通过 `Service` getter 或
`newService*` 壳转一圈才可使用。包内消费者直接读 `service.components`；
跨包消费者依赖领域组件或装配期注入的窄端口。

### EP-046A：组件 getter 退场

状态：**已完成。**

结构判据：

- `core` 生产文件不得出现无参、单返回值、名字以 `Component` 结尾的
  `*Service` 方法；
- 包内调用点直接使用 `service.components.<field>`；
- 跨包调用只走现有 `FrontendPorts` 或领域包，不新增万能接口；
- 测试属于 core 集成测试时保留在 `application/core`，但不得为了测试恢复
  生产 getter。

红测：

```text
go test ./application/core -run TestCoreDoesNotExposeComponentGetters -count=1
```

删除门禁：

- `adminApplicationComponent`、`completionApplicationComponent`、
  `policyApplicationComponent`、`workspaceApplicationComponent` 等
  `*ApplicationComponent` getter 全部删除；
- 只承载 getter 的 `core` 文件删除；
- core 生产文件数和行数下降；
- `Service` 公开方法集合不变，GUI/TUI wire 不变。

回退方式：恢复被删除的 getter，并把包内调用点改回 getter；不涉及状态或持久化
迁移。

落点与证据：

- 删除 26 个无参、单返回值的 `*ApplicationComponent` getter，包内改读
  `service.components.<field>`；
- 删除只承载 `adminApplicationComponent` 的 `service_account.go`；
- `HasSigilPrefix`、`SigilOf`、输入 sigil 常量和 `Suggestion` 类型身份回归
  `application/input`，根门面直接引用该包；
- 新增 `TestCoreDoesNotExposeComponentGetters` 与输入 facade 结构门禁；
- core 生产文件 114 → 111，生产代码 11,602 → 10,634。

### EP-046B：构造适配归位

状态：**已完成。**

- 删除 68 个自由 `newService*` 构造器；Conversation Turn、Session
  lifecycle/draft/restore/query、Edit/Fork、Cold Restore 和其余 Application
  能力改由 `service_assembler_*.go` 中的组合阶段一次性装配；
- 构造器不再分散为一个能力一个文件；原本只承载构造器的 58 个文件已删除；
- `TestNoFreeServiceConstructors` 把自由 `newService*` 构造器预算收紧为 0，
  防止新增能力继续通过“包一层构造器”伪装拆分；
- 真实状态与判据仍留在原 owner：Session release 的受保护集、ProjectBoard 的
  任务读选择、ToolRecord host、Chat/Session 状态机均未复制；
- core 生产文件 111 → 68，生产代码 10,634 → 10,243 行；`Service` 仍为 11 个
  共享状态/生命周期方法；
- GUI/TUI wire、事件 revision/seq、snapshot 与持久化 JSON 均未改变。

### EP-046C：入站接口定义退场

状态：**已完成。**

`application/faces_api.go` 的接口与类型清单属于入站契约，不属于
协作实现。迁移前必须先证明：

- 根 `application` 不再通过 `core` 再导出接口身份；
- 编译期断言仍能证明实现满足能力面；
- E2E narrow-face 清单、`FaceTypeNames` 与文档路径同步；
- 不改变任何前端方法签名。

落地结果：

- 删除 `application/core/service_api_faces.go` 与旧 `application/faces.go`
  再导出层，接口事实源改为 `application/faces_api.go`；
- `Frontend` 的结构断言继续固定在
  `application/frontend_assertions.go`；
- `api_contract_test.go` 直接扫描根 Application 入站文件；
- E2E 允许根门面拥有这些入站契约类型，同时继续要求其余导出类型为 alias。

### EP-046D：Core 前端状态壳退场

状态：**已完成。**

- `SubscribeSession`、`SubscribeSessionWithReplay` 唯一判据迁入
  `application/session/viewstate`；过滤使用 `session.Domain.ActiveID()` 与
  EventHub 的可选过滤/重放能力；
- `PrepareProviderHistory`、`PromptLayers`、`SetProjectBoardSink` 从
  `core.Service` 移到 `compat.Frontend`，直接调用 history、prompt stack 与
  project-board 装配端口；
- 生产调用方 `wiring/bootstrap`、GUI/TUI/headless 与项目看板测试改用
  `Frontend`；core 旧集成测试只在 `_test.go` 保留同名 shim；
- `Service` 公开方法 23 → 11；GUI/TUI wire、事件 revision/seq 与持久化格式
  不变。

## 4. AP-106：Session Draft Entry / Materialization（已完成）

### 目标

把 `session_draft_entry.go` 与 `session_draft_materialization.go` 的应用编排迁入
`application/session`，core 只保留 draft slot、engine、snapshot 和 publication 的
Host adapter。

### 新类型

```go
type DraftEntryState struct { ... }
type DraftSlot struct { ... }
type DraftEntryDeps struct { ... }
type DraftEntryApplication struct { ... }

type DraftMaterializationSource struct { ... }
type DraftMaterializationDeps struct { ... }
type DraftMaterializationApplication struct { ... }
```

### 必写测试

- 红测：`application/session/draft_entry.go`、`draft_materialization.go` 不存在时结构门禁失败。
- 行为 parity：
  - `/new` 在已关闭/排空时短路；
  - 运行中旧会话不执行持久化与上下文 detach；
  - 空闲旧会话先保存、清历史、清 Skill、detach context；
  - draft shell 安装顺序与 revision 发布一次；
  - 首次提交只物化一次，草稿槽消费、标题写入、workspace projection 和发布顺序不变。

### 删除门禁

- 删除 core `sessionDraftEntryDeps/sessionDraftEntry`；
- 删除 core `sessionDraftMaterializationDeps/sessionDraftMaterialization`；
- `Service.BeginNewSession`、`materializeDraftSession` 保留单行转发；
- core 行数必须继续下降。

## 5. AP-107：Cold Restore 组件链

### 目标

把冷恢复的已拆分组件迁入 `application/session/cold`：

- `sessionColdRead`
- `sessionColdHistory`
- `sessionColdRestore`
- `sessionColdWorkspace`
- `sessionColdActivation`
- `sessionColdTask`
- `sessionColdView`
- `sessionColdCommit`
- `sessionColdPublish`

`session_history.go` 的 `resumeSessionCold` 主链本批不直接搬走，只允许改为顺序调用新
包组件。

### 契约

- `ReadDeps`：record/transcript/history 并行读取。
- `HistoryDeps`：record/transcript 重建、renumber 与 fallback。
- `RestoreDeps`：workspace 显式键、Engine resume/replace、队列回填。
- `ActivationDeps`：epoch、active、restoring、tier、title。
- `TaskDeps`、`ViewDeps`、`CommitDeps`、`PublishDeps`：各自现有读/写端口。

### 必写测试

- 红测：包文件不存在、core 仍定义旧组件类型。
- 冷快照、transcript range、compaction facts、Task/Plan、workspace binding、权限 tier
  等既有回归全部保持通过。
- 重点 parity：锁内准备/安装/commit 与锁外磁盘读、context 挂接、runtime/snapshot 发布。

### 风险

- 这是 Conversation/Session reshape 前风险最高的一批；
- 必须先完成 AP-106，再单车道迁移；
- 任何需要改 `contract/**` 的 DTO 变更停止并记录跨会话需求。

## 6. AP-108：Fork / Restoring / Scope 查询

### 目标

- `sessionForkLineage` → `application/session`；
- `sessionRestoringShell` → `application/session`；
- 只读 Session scope/catalog 判定中可独立部分 → `application/session`；
- `SnapshotOf` 的冷/热读取逻辑暂不从 core 移走，只抽可测试的纯查询。

AP-108B1/B2a/B2b 均已完成：Runtime 投影、冷/热路径选择和驻留 Snapshot 组装
进入 `application/session/query`；现有锁序与 wire 未改。AP-108 的只读范围已收口。

### 必写测试

- fork lineage 缺失/空 payload/损坏 record/ref/空 parent 的降级；
- restoring shell 的 Session、Runtime、ChatState、Active、revision；
- 会话删除/释放后 scope 不应复活；
- 查询不进入共享写锁。

## 7. AP-109：Scheduled Task

### 目标包

`application/admin/scheduler`

迁出：

- `schedulerApplication`
- `scheduledRunApplication`
- `scheduledSessionApplication`
- `withScheduledRunDefaults` / `scheduledRunParams` 的应用部分
- workspace/plugin/run-param 校验编排

### 契约

- `Create/Update/Cancel` 回调；
- workspace lookup；
- plugin catalog names；
- session unit、permission tier、reasoning lease、engine loop pin；
- plugin assembly carrier；
- runtime projection refresh。

### 必写测试

- 默认参数只落一次；
- 创建/编辑共用校验顺序；
- 空 workspace 合法、未知 workspace 拒绝；
- 未知插件显式拒绝；
- 忙碌/关闭/排空时的调度会话行为；
- 新会话、workspace 绑定、tier/run params/plugin 装配、提交的顺序。

## 8. AP-110：Policy 切换

### 目标

把剩余 `policyApplication` 迁入 `application/policy`：

- skill 激活；
- reasoning effort / loop limit；
- release window；
- permission tier；
- effort 切换；
- plugin 切换；
- runtime projection 提交。

### 约束

- 规则仍在 `backend/limit/*`、`backend/engine/skill`、`middle/switch`；
- 包只固定应用编排和锁外/锁内顺序；
- UI 设置行为与 wire 不变。

### 必写测试

- effort 与 loop 互不影响；
- plugin 切换仅在全部会话空闲时允许；
- tier 持久化失败不回写 runtime；
- full access 自动审批只作用于当前会话；
- runtime event 每次切换只发布一次。

## 9. AP-111：Context Compaction

### 目标包

`application/conversation/context`

迁出：

- `contextCompactionApplication`
- `compact_context` tool JSON adapter
- 同会话压缩 gate 的 acquire/release 顺序
- CompactDone / Unrecorded / Failed / Scheduled / NotNeeded 回执映射

### 非职责

- 压缩算法、摘要、预算和持久化仍在 `backend/engine/context`；
- 不在本包复制 provider fallback 或压缩重试判断。

### 必写测试

- 压缩成功、局部 fallback、失败痕、未记录、不需要压缩；
- gate 忙时 Scheduled 回执；
- `compact_context` 参数错误；
- 压缩路径不持 ViewMu 执行慢调用。

## 10. AP-112：Queue / ChatState / ChatTurn 请求工厂

### 目标包

`application/conversation`

迁出：

- `conversationQueue`
- `conversationTurnState`
- `chat_turn_request` 工厂
- `chat_turn_process.BeginLocked` 的请求起点部分

### 唯一状态所有者

- `SessionUnit.Queue`
- `SessionUnit.Chat`
- Engine stream
- Task coordinator

### 必写测试

- 队列 reorder/recall；
- `requestID` 冻结与被替换回合拒绝；
- 队列提升批次分隔与顺序；
- Skill 层冻结；
- user transcript 只写一次；
- 产线不在组件外直接调用 `SetChatState/UpdateChat`。

## 11. AP-113：ChatTurn 执行流水线

### 目标

把已收拢的 `chat_turn_*` 组件分批迁入 `application/conversation`：

1. input/response；
2. recovery/cleanup；
3. commit/finalize；
4. publication/tail；
5. 最后处理 `runChat` 主循环。

AP-113D1/D2 已完成 reasoning 投影和 `runChat` application runner 提取；Engine
ReAct loop、SessionUnit 状态所有权和持久化实现仍由既有模块拥有，未复制状态机。

### 分批理由

这批同时涉及 Engine 流、持久化、事件、Task、queue、LRU 和 Session release，不能一次全搬。

### 必写测试

- provider recovery / bounded retry；
- ReAct budget finalize；
- persistence failure 回退草稿；
- stale turn 拒绝；
- message/tool event/revision/seq 顺序；
- tail 在 ViewMu 外完成；
- 后台会话完成不污染当前视图。

## 12. AP-114：Edit/Fork

### 目标包

`application/conversation/edit`

迁出：

- operation registry；
- validator；
- target resolver；
- child process；
- rollback；
- visible-message annotation；
- fork lineage 的写侧编排。

### 必写测试

- operation_id 幂等；
- running/queued/compression 前缀禁编；
- fork cut 计算；
- child fork → resume → submit → rollback 原子顺序；
- 可见消息 EventSeq 回填；
- 父会话回退失败的错误合成。

## 13. AP-115：WorkTable / Tool hooks / Tool record / Command

该工作包拆成四个可独立回退的切片。每片都必须先新增结构/行为测试，再迁移实现；
新包只能通过 `Deps` 或消费者定义的 port 依赖宿主，不得 import `application/core`。

### AP-115A Tool hooks（已完成）

目标包：[`application/execution/toolhooks`](../../application/execution/toolhooks/README.md)。

已迁出：

- LLM complete、tool start/complete、iteration complete 的顺序编排；
- `(turn, name, arguments)` FIFO 配对与并发工具稳定唯一 ID；
- metadata-only 诊断观察者及 panic 容错；
- session-backed 排队输入闸门、非 session-backed history repair 与失败回执。

core 只保留工具消息/transcript/Runtime/EventHub 的 Host projection。以下内容明确仍由
core 或后续切片拥有：工具结果截断、`recordToolOutcome`、worktable 同步、工具权限判据。

### AP-115B Tool record（已完成）

目标包：`application/execution/toolrecord`

迁出范围：

- `toolRecordAccumulators` 的会话/单元累计与 `lastSeq` 幂等合并；
- `toolStatusFailed`、`toolRecordReason`、`recordToolOutcome` 的唯一判据；
- 批量 flush、每会话懒加载、开关、错误看板投影；
- 主子代理/teammate 完成路径共用同一记账入口。

已落地 struct/ports：

```go
type Deps struct {
    SaveEnabled     func(bool) error
    WritableSession func(string) bool
    ViewSessionID   func() string
}

type Application struct { /* accumulators, stages, switch, snapshot store */ }
```

实际实现补齐了 `SaveEnabled`、`WritableSession`、`ViewSessionID` 三个宿主回调；
`SessionView` 与 `Diagnostics` 只服务看板和测试读面。无 owner session 的读数丢弃并
计数；`seq <= lastSeq` 幂等忽略；开关关闭不写内存/磁盘；flush 不在 `ViewMu` 或工具
投影临界区执行。`core.Service` 的 `RecordToolCommandOutcome`、
`SetToolRecordEnabled`、`FlushToolRecord`、stage/commit/close 等保留一行转发。

行为 parity 测试已覆盖：成功/失败/终态、两会话隔离、无 session 丢弃、stage drain、
seq 幂等、落盘失败看板错误、开关关闭和 release 回收。

删除门禁：core 的 `tool_record.go` / `tool_record_stage.go` 已不再定义
`toolRecordAccumulators`、`toolRecordStages`、原因词表或 flush 状态机；结构测试
`TestToolRecordApplicationIsExtractedFromCore` 扫描并钉住这一点。

### AP-115C1 WorkTable publisher（已完成）

目标包：`application/execution/table`

已迁出：

- `WorkTablePublisher` 的 latest-wins 汇聚、关闭尾态与关闭后 `Send` 安全；
- 子代理树 payload 签名去重；
- `WorkTableEvent` 的 rows/batches/tree 组装。

core 只保留树快照、session ID 与 EventHub 发布回调；行投影仍只在
`kernel/worktable`。

### AP-115C2a WorkTable 输入投影（已完成）

已迁出：

- 请求尾部 TraceBlock 的未终态 task 过滤、稳定排序与 retry 文案；
- async/teamwork 行与提示；
- 整块行预算、截断提示与空块；
- session/view 作用域归一。

core 的 `workTableTraceBlockFor` 只采集不可变输入并调用 `table.BuildTraceBlock`。

### AP-115C2b1 async/teamwork consumer（已完成）

已迁出：

- async 与 teamwork 两个容量 1 信号口的单消费者循环；
- pending completion 补扫、timer 重置与 stop 收尾；
- refresh / trigger 的 safe-call 顺序。

### AP-115C2b2 subagent/task consumer（已完成）

已迁出：

- subagent lifecycle signal consumer；
- task change consumer 与 task context 采样；
- `sync.Once` 三路 consumer 启动、统一 stop 与 safe-call。

### AP-115C2c WorkItem/批次输入投影（已完成）

已迁出：

- `BuildRows`：Plan/Task/SubAgent/Async 输入到行与批次；
- `WorkItemForRecord`：task-change 单行与 plan 依赖补齐；
- 实际排序、去重、证据截断与身份算法继续只在 `kernel/worktable`。

AP-115C/D 至此完成；剩余跨会话项是 AP-116。

保留：`kernel/worktable` 是可复用的纯投影判据与有界背压实现，不复制其排序、
去重或容量算法；具体 Runtime/Task/SubAgent 仍由宿主 port 提供。建议新增
`Source`、`Consumer`、`Query` 窄接口，由 core 装配时注入，禁止把完整 `Service`
作为字段。`publisher` 已在上一切片独立，consumer 只向它发送不可变快照。

必须验证：查询不持共享写锁、慢输入采集在锁外；同一 revision 的增量不会乱序；
关闭时 consumer 可终止且不泄漏 goroutine；snapshot resync 与增量 reducer 的
wire 形状不变。

### AP-115D1 基础命令（已完成）

目标包：`application/input/command`

已迁出：

- help、clear、model、history、trace、sessions、diag、exit；
- `Registry` 窄接口与 `contract.CommandResult` 适配；
- 重复注册错误、历史角色统计和 help/sigil 文本。

### AP-115D2 mutation 命令 / bridge（已完成）

已迁出：

- compact/resume/fork/plugin/effort/permission 的应用编排；
- command token 参数归一与 handler 结果包装；
- command → conversation/Session API 的桥接；
- `/new`、resume、fork 的行为 parity；core 只保留 host callback 装配。

不得迁出：`backend/engine/input` 的 Router 规则、工具权限或 Skill/Plugin 能力判定。
core 的 `registerBuiltinCommands` 只保留 host callback 装配；结果
继续用 `contract.CommandResult`，禁止用字符串分支模拟第二套命令状态机。
AP-115D 至此完成。

必须验证：重复注册和空命令拒绝；handler error 保留原始 cause；`/new` 在运行中、
草稿和空闲会话的行为与现有一致；命令事件与 GUI/TUI wire 不变；registry 读取不需要
共享写锁。

## 14. AP-116：34 方法退场

这是最终验收批，不提前做“空接口”或“未来接口”。

### 前置条件

- GUI/TUI/headless 已迁移到能力型 API；
- `faces_api.go` 旧四组接口的生产调用为零；
- 每个旧 Service 方法有 ledger 首调用方和删除门；
- facade 闭包白名单已收紧。

### 执行顺序

1. 删除生产调用为零的旧方法；
2. 更新 `service_methods.txt` 与预算；
3. 每删一批运行全量、race、E2E、前端测试；
4. 最后的 `Service` 只保留组合根需要的装配/生命周期方法；
5. core 生产代码接近 0 后再删除 core 包。

### 禁止

- 为减少方法数把多个方法塞成 `map[string]any` 或字符串命令；
- 删除仍被 GUI/TUI 使用的 wire 方法；
- 以“测试全绿”替代 ledger 调用方归零证据。

### AP-116A 已完成

通过 AST/调用方审计，将仅服务 core 内部和测试的
`StageUnitToolOutcome`、`CommitUnitToolRecord`、`CloseUnitToolRecord`、
`CloseSubagentToolRecordUnit`、`ToolRecordDroppedReadings`、
`RestoreRoleAccounts`、`RestoreRolePermissions` 收为包内方法。Service 公开方法从
171 降至 164。

AP-116B 继续收回了 `ActiveSkillIDs`、`GoalBegin`、`GoalUpdate`、`GoalDone`、
`HandleSubagentToolEvent`、`RefreshWorkTableSnapshot`：它们只服务 core 内部
handler/lifecycle；Service 公开方法进一步降至 157。`TeamworkBoardViewFor` 的
view port 结构由 `teamworkBoardViewPort` 内部适配器满足，不再暴露到 Service。

### AP-116C/D 已完成

- `TeamworkBoardViewFor` 改为内部 `teamworkBoardViewPort`，Service 方法降至 157；
- `ClearReviewRequest` 改为 review 组件内部调用，Service 方法降至 156。

### AP-116E 已完成

按“先迁移依赖方、再删除旧入口”的顺序新增 `application.Frontend` 过渡适配器，
把 GUI、TUI、headless、wiring 与 e2e harness 切到该类型。随后 `Service` 公开面
迁出 8 个共用入口：

- `Suggestions`
- `SelectAccount`
- `SwitchEffort`
- `SwitchPlugin`
- `LoadMoreHistory`
- `LoadLatestHistory`
- `ReorderQueuedInput`
- `RecallQueuedInput`

`Frontend` 直接调用唯一领域组件；旧 `Service` 内层壳已删除。core 测试只通过
测试辅助函数构造 `Frontend`。Service 公开方法 156 → 148，core 生产代码
12,818 → 12,810 行。

### AP-116F 下一步

继续按“调用方迁移 → 结构红测 → 删除旧入口”的顺序推进会话、订阅与生命周期
只读面，首批候选：

- `Snapshot`
- `Subscribe`
- `SnapshotOf`
- `ListSessions`
- `GetSessionTranscript`
- `SessionInputIndex`
- `WaitCatalogRefresh`
- `WaitForIdle`
- `PerfStats`

每个子批新增 `Frontend` 方法不超过 20 个；wiring-only 入口继续使用装配期窄
adapter，不无条件塞入门面。

### AP-116F1 已完成

新增 `application/compat`，把 50 个纯转发入口从 core 移到兼容适配器：

- 账号 6 个；
- 角色权限与角色会话 26 个；
- Workspace 18 个；
- 另有此前的 8 个入口同步收口到 `compat`。

core 只保留 `FrontendPorts` 装配快照。删除
`service_account_window.go`、`workspace_environments.go`、`workspace_gitrepos.go`，
并把账号、角色、Workspace 转发从其余 core 文件中移除。

指标：Service 公开方法 148 → 98；core 文件 125 → 123；core 生产代码
12,810 → 12,538 行。

### AP-116F2a 已完成

- 新增 `application/history.WindowAssembler`，middle 提供唯一 adapter，
  core 删除 `middle/switch/history` 直接依赖；
- 把 `ListSessions`、`WaitCatalogRefresh`、`GetSessionTranscript`、
  `SessionInputIndex`、`SnapshotOf`、`SessionMeta`、`PerfStats`、`SearchHistory`、
  `ProjectRootFor`、`WorkTableRowsFor`、`TeammateSessionLiveFor`、
  `TeammateSessionLivePageFor`、`WorkItemExecutionFor`、`TeammateInspectionFor`、
  `ToolResultContent` 共 15 个只读/查询入口迁入 `application/compat`。

指标：Service 公开方法 98 → 83；core 直接内部依赖 34 → 33；core 文件
123 → 121；core 生产代码 12,538 → 12,463 行。

当前剩余 0 个方法：

外部依赖拆除的端口设计见 [`port-migration-plan.md`](port-migration-plan.md)。

- 纯 host/assembly 转发、订阅、Prompt、ProviderHistory 与 ProjectBoardSink
  已全部退场；`Service` 只剩仍在共享状态临界区内的工作：

| 能力 | core 方法 | 暂留原因 |
|---|---|---|
| 无 | — | 公开业务入口均已迁到 capability package 或 compat facade |

ToolRecord 公开入口已由 EP-046D 移到 `application/compat.Frontend`，core 只保留
草稿可写判据、主代理 outcome 和 shutdown 全量 flush 三个内部 host adapter。

## 15. 跨会话阻塞项

- `contract/**` 共享 DTO 变更必须由 Backend/Middle 会话完成。
- GUI/TUI 窄 API 迁移必须与前端会话同步。
- `account-window.test.mjs` 仍依赖 Backend 旧路径，属于共享 DTO 迁移项。
- 若 AP-107/111/113 需要新 DTO 或 Event 字段，本批停止并记录：
  - 所需 DTO/字段；
  - 消费方；
  - wire 兼容策略；
  - 对应 Backend/Middle 责任批次。

## 16. 每批汇报模板

每个 AP 完成后必须报告：

1. 本批实际完成项；
2. 新增/修改接口与 struct；
3. core 文件/行数、Service 方法、直接依赖前后值；
4. 删除/合并的重复实现；
5. 红测命令与失败证据；
6. 绿测命令与通过结果；
7. 未完成项和下一批；
8. 跨会话阻塞；
9. 是否修改越界目录。

## 17. 最终退出条件

- 原 171 个兼容方法完成调用方迁移并清零（当前剩余 34）；
- core 直接内部依赖接近零；
- core 生产代码完成删除；
- GUI/TUI/headless 只消费稳定由 Application/contract 提供的窄 API；
- 全量、race、E2E、前端、README 和 diff 门禁全部通过；
- 不再存在第二份状态机、第二份判据或无人使用的未来接口。
