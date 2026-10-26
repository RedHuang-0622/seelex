# Seelex `store` 领域驱动拆分方案

日期：2026-10-27
状态：**方案已确认；实施前见 [`milestones.md`](milestones.md)、[`external-change-protocol.md`](external-change-protocol.md) 与 [`next-session-prompt.md`](next-session-prompt.md)**
范围：`store` 根包的类型分解、限界上下文划包、消费方端口改造与兼容面收敛。
不在范围：JSON v8 落盘格式变更、用户数据迁移、业务语义变更、当前正在迁移的 teamwork 契约改写。

事实来源：

- 当前源码。
- [`../2026-10-15-architecture-reshape/post-migration-proposals.md`](../2026-10-15-architecture-reshape/post-migration-proposals.md) P4。
- [`../2026-10-15-architecture-reshape/m3-store-design.md`](../2026-10-15-architecture-reshape/m3-store-design.md)。
- [`../../store/README.md`](../../store/README.md)。
- [`../../AGENTS.md`](../../AGENTS.md) §8、§9。

## 1. 设计目标

把“文件很多”与“职责很散”的单包，收敛成：

1. 一个只负责持久化机制、不解释业务的存储内核。
2. 若干按聚合与生命周期划分的领域存储包。
3. 消费者自己声明窄端口，领域实现通过结构满足端口。
4. 组合根只负责装配具体实现与窄端口。
5. 根包 `store` 暂时保留兼容门面，最终退化为零业务方法或删除。

本次拆分的硬约束：

- 物理路径、JSON 字段、head 水位、原子发布、崩溃恢复与锁粒度保持逐字兼容。
- 不把 `if kind == ...`、`if role == ...` 等层间差异搬进领域实现。
- 不为了拆包复制第二份判定；每个判据只保留一个函数。
- 每次迁移必须列出“原位置 → 新唯一位置”的删除清单。
- 契约变更先写红测，再实现。

## 2. 当前结构证据

当前耦合链为：

```text
Router
  → Repository / withJSONRepositoryAt 的具体类型断言
  → jsonRepository
  → storeEngine
  → sessionModuleLocks + 各领域文件
```

| 类型 | 实测规模 | 结构问题 |
|---|---:|---|
| `storeEngine` | 183 个生产方法 / 23 个文件 | 所有领域的隐式 receiver，事实上的实现层上帝类型 |
| `Router` | 130 个方法 / 11 个文件 | 公共上帝门面，混合作用域、生命周期、业务读写和能力发现 |
| `jsonRepository` | 90 个方法 / 15 个文件 | 中央复合仓储，继续依赖具体 `storeEngine` |
| `Commit` | 5 个字段 | 把 transcript、artifact、旧 provider cache、旧 state、compaction 焊成一个事务载体 |
| `sessionModuleLocks` | 18 把锁 + 3 个跨域缓存 | 新模块必须同步改枚举、锁字段、`mutexFor` 与 head 修复分支 |
| `Repository` | 23 个方法 | 一个接口同时服务历史、事件、context、项目记录、栈与删除生命周期 |
| `SessionGranularStore` | 46 个方法 | 在 `Router` 外再加一层全领域转发 |

真正的耦合不只是“方法多”：

- `writeCommitLayout` 在一次调用内协调工具结果、message head 与压缩记录。
- `createRoleSession` 同时协调布局、message、EVENT、lifecycle 与失败清理。
- `newSubagent` 同时读取 retention、父消息，写子消息、事件与索引。
- `compactCommit`、`lRUDelete` 跨 compact/retention/message 取锁。
- `roleStore` 会再创建一个同型 `storeEngine`，形成“存储中再套存储”。

因此不能直接把文件移到子目录。Go 要求方法与类型同包，必须先拆类型和职责。

## 3. 限界上下文与聚合

### 3.1 目标模块

```text
store/
├── root/                 现有纯标准库机制：原子写、共享读、数据根锁、残尾处理
├── internal/
│   └── layout/           会话路径、模块 head 信封、按模块分片锁、guide
├── session/              会话记录、message/event、draft/queue、retention、compaction、fork
├── context/              system prompt、Plan/Task/Goal/Skill/Compact 栈、checkpoint
├── artifact/             tool result、big blob、media、search index
├── project/              ProjectRecord 与项目知识
├── board/                项目看板投影存储
├── teamwork/             team plan、team event、WorkItem binding
├── role/                 角色会话、role draft、角色权限
├── workunit/             node/subagent 会话记录与进度
├── workspace/            现有工作区实现
├── schedule/             现有全局定时任务定义态
├── record/               现有工具记录累计器
└── (root package store)  仅保留迁移期兼容 alias / Router 薄转发
```

`wire_assembler.go` 不进入新的 store 领域包。它是 provider 输入装配规则，
最终应回到 `backend/engine/context` 或该域的读侧应用服务；迁移前先以读侧端口保留兼容入口。

### 3.2 领域归属

| 上下文 | 聚合 / 事实 | 必须维持的不变式 | 当前主要文件 |
|---|---|---|---|
| Session | 会话 transcript、draft tail、queue、retention、compaction 指针 | message head 是发布点；未发布 tail 不可见；draft/queue 原子迁移；LRU 不能越过 watermark；同一 commit 幂等 | `sessionstore`、`message_rows`、`pending_tail`、`lifecycle`、`queue_consume`、`retention`、`compact_frames`、`compaction_records`、`structural_events`、`fork_store`、`session_meta` |
| Context | system prompt、四类上下文栈、Goal 栈、checkpoint | 每个栈只由一个写者推进；system prompt 不压缩；compact 帧链有序；head 只装水位 | `session_context`、`context_state`、`stack_channel`、`stack_journal_json`、`checkpoint_store` |
| Artifact | tool result、blob、media、search index | 内容寻址；message 可见前 artifact 已落盘；引用不可变；GC 只回收无引用对象 | `big_tool_result`、`media`、`search_index`、`tool_record` |
| Project | ProjectRecord、项目知识 | 项目记录是项目级事实；project-known 是可重建人读视图，不是第二事实源 | `project_record`、`project_known` |
| Board | 项目看板追加投影、BOARD.md | 投影不是领域事实源；只追加/折叠；同一会话文件单写者；可删可重建 | `project_board`、`project_board_markdown` |
| Teamwork | team plan、team event、WorkItem binding | plan 整份替换；event 只追加；binding 按 work item 折叠最后一行 | `teamwork`、`teamwork_items`、`team_registry` |
| Role | 角色会话、role draft、权限 | sequencer 是 main message 唯一写者；sync 成功才删 draft；权限记录原子替换 | `role_session`、`role_session_router`、`role_permission` |
| WorkUnit | node/subagent 会话记录、进度 | 主会话能索引全部子会话；终态结论回主会话；记录删除与恢复语义固定 | `node_session_store`、`unit_record_writer` |

### 3.3 刻意不拆的边界

- `message/event/draft/retention/compaction` 暂留同一 `session` 包。
  它们共享 message head、seq、watermark 和发布顺序，拆成不同包会制造双向端口。
- `board.go` 是会话看板元数据，依赖会话模块锁，归 `session`；
  `project_board.go` 是项目目录派生投影，归 `board`。两者名称接近但生命周期不同。
- `project_known.go` 是对 `ProjectRecord` 的渲染视图，不能先于 `ProjectRecord`
  的领域归属迁移。
- `store/root` 不承载会话或模块语义；新 `store/internal/layout` 才承载会话布局。

## 4. 设计模式选择

| 模式 | Go 实现 | 应用位置 | 理由 |
|---|---|---|---|
| Adapter | 领域存储结构满足消费方窄接口 | application/middle/backend 的端口 | 上层不依赖具体 store 包，也不需要为每个消费者写转发文件 |
| Facade | 根包 `store` 暂时提供 alias/薄转发 | 迁移期兼容面 | 先缩小调用面，再删除；避免一次性修改大量调用点 |
| Factory / DI | `New...` 显式接收 `root`、`layout` 与依赖 | `wiring/bootstrap` | 存储实现不读取全局状态，构造顺序可测试 |
| Strategy | 模块 ID / head payload codec / rebuilder 由领域提供 | `store/internal/layout` | 内核不写领域 switch；领域差异只出现在装配表和调用点 |
| Unit of Work | application 层编排 turn commit | `application/core/session_runtime` | 跨 transcript/artifact/compaction 的写序属于用例，不属于某个聚合 |

## 5. 方案对比

### 方案 A：一次性按目标包整体搬迁

一次定义全部限界上下文，直接把 130+ 生产文件拆成子包，同时替换所有调用方。

### 方案 B：契约与端口先行，按领域逐批 strangler 迁移

先做特征测试与领域建模；每批只迁移一个可解释边界，根包保留 alias/薄转发；
上层逐步改用契约 DTO 和消费者窄端口；每批完成后收紧结构门禁。

| 维度 | 方案 A | 方案 B |
|---|---|---|
| 耦合度 | 目标低，但迁移期难判断真实边界 | 逐步下降，每批可验证 |
| 内聚性 | 依赖一次建模完全正确 | 可在 Board/Artifact 试点后修正 |
| 可测试性 | 一次性出现大量编译与行为变化，定位困难 | 每批都有局部红绿与结构证据 |
| 实现成本 | 前期集中，后期大量修调用点 | 总成本相近，但分布可控 |
| 改动面 | 跨约 110 个外部调用点同时变化 | 每批只改一个域及其调用方 |
| 可回滚性 | 差，单批难以局部回退 | 好，可按域回退 |
| 数据风险 | 高，难以区分搬迁缺陷与行为回归 | 低，落盘兼容测试可逐批固定 |

## 6. 推荐方案

采用 **方案 B：端口先行、类型先行、逐域迁包的 strangler 迁移**。

理由：

- 当前边界已经由仓库 P4 裁决，但 `Commit`、消息锁、团队/角色事务仍有未决行为。
- Board 域已经存在消费方窄端口，可以作为第一个低风险垂直切片。
- `store/root`、`store/workspace`、`store/schedule`、`store/record` 已证明
  “机制下沉 + 领域包”可行。
- 每批保留兼容面，调用方可以逐步迁移，不需要一次性修改全部 import。

最大风险：

`Commit` 与 `sessionModuleLocks` 的拆分会触及消息可见性和失败语义。
在补足写序、半失败和重试特征测试之前，不得进入 Session 主聚合迁移。

### 6.1 外部依赖批次变更协议

任何里程碑只要会修改当前领域目录之外的文件，或会改变被外部包消费的导出类型、
方法、常量、接口或 JSON 形状，都必须先执行
[`external-change-protocol.md`](external-change-protocol.md)。

固定顺序：

1. 为整个外部批次创建专用 branch/worktree；脚本禁止在主工作区运行。
2. 在专用 worktree 中生成并审查外部影响清单，逐文件记录
   “依赖项 / 被依赖项 / 使用符号 / 验证包”。
3. 确认目标文件没有任务外的未提交内容；有冲突时停止，不在脏目标上执行。
4. 在脚本执行前提交 checkpoint commit，并记录 commit hash；不 push。
5. 使用落盘的版本化脚本，先 dry-run，再一次性应用清单内全部文件；
   脚本必须校验当前工作区根和 branch。
6. 脚本任一步失败必须自动恢复本次触碰文件；进程中断时用 checkpoint 和运行日志兜底。
7. 脚本成功但编译/测试/结构门禁失败时，提交批次变更后对批次执行 `git revert`，
   不覆盖 checkpoint。

影响清单、脚本和回滚日志是该批次交付物的一部分；没有它们的外部依赖改动不进入 review。

### 6.2 Token、工时与成本预估

拆分预算按“累计消耗”估算，包含源码阅读、工具输出、实现、测试和 review，
不是单次模型上下文窗口的驻留量。已验证基准：`application` 拆包约 1 天 1 夜、
成本约 150（用户口径，币种未注明），**但应用层仍未拆完**。

因此 `150` 只能视为“尚未收敛工作”的已投入下限，不能当作完整拆包的成交价。
`store` 还涉及发布顺序、锁、崩溃恢复和多领域外部 importer，预算必须上调，
并保留因未知耦合导致返工的空间。

`store` 的方法量更大、直接外部 importer 更多且涉及持久化发布顺序，预计：

| 指标 | 预估 |
|---|---:|
| Token | 约 230 万–440 万 |
| 有效工时 | 约 86–152 小时，约 3.6–6.3 个 24 小时开发周期 |
| 开发会话 | 约 8–12 个可验收会话 |
| 校准成本 | 约 600–1100，与参考 150 同单位 |

分里程碑预算见 [`milestones.md`](milestones.md) §2.1。
预算不是交付承诺；若预计超过上限 1.5 倍，暂停并重新拆分，不得通过减少测试、
跳过红测或省略回滚演练压预算。

## 7. 循环依赖检查

目标依赖方向：

```text
application / middle / backend
        ↓ 只依赖 contract DTO 与自己的消费端口
contract/                   零依赖共享形状
        ↑
store/<domain>  →  store/internal/layout  →  store/root
        ↑
wiring                       只负责构造与注入

store 根兼容包  →  store/<domain>     仅迁移期允许
store/<domain>  -X→  store 根包      领域包禁止反向 import
```

具体规则：

1. 领域包禁止 import 根 `store`、application、middle、backend。
2. 领域包之间禁止直接 import；跨域协作用例由 application 层端口编排。
3. `store/internal/layout` 只依赖 `store/root`、`contract` 与标准库。
4. 根 `store` 可以依赖领域包，只用于兼容转发。
5. 消费方端口定义在消费方包；跨层值类型放 `contract/dto` 或 `contract/protocol`。
6. `e2e/store_boundary_test.go` 的 scope 扩到 `./store/...`，每新增子包必须自动纳入 I6。

## 8. 核心接口与类型草图

以下仅为设计草图，最终签名以先写的契约测试为准。

### 8.1 Layout 内核

```go
package layout

type Scope struct {
    ProjectID string
    SessionID string
}

type ModuleID string

type Locks interface {
    For(scope Scope, module ModuleID) (*sync.Mutex, error)
}

type Layout struct {
    root   string
    locks  Locks
}

func (l *Layout) SessionRoot(scope Scope) (string, error)
func (l *Layout) ReadHead(scope Scope, module ModuleID) (Head, error)
func (l *Layout) PublishHead(scope Scope, module ModuleID, payload any) error
```

约束：

- `ModuleID` 由领域包拥有并使用命名空间，例如 `session.message`、`artifact.media`、
  `teamwork.plan`；内核不再维护 18 case 的 `mutexFor` switch。
- 同一模块 ID 必须映射到同一把锁；不同模块 ID 不得静默别名。
- head payload 的校验、重建由领域包负责；内核不按模块 switch。
- 现有 `storageModule` 枚举和 `sessionModuleLocks` 在兼容期保留，内部逐步委托新内核。

### 8.2 消费方端口示例

```go
// middle/view/board：消费方定义
type ProjectBoardStore interface {
    AppendProjectBoardRows(
        projectRoot, sessionID, file string,
        rows []dto.ProjectBoardRow,
    ) (uint64, error)
}

// application/core/session_runtime：消费方定义
type TranscriptWriter interface {
    Append(ctx context.Context, scope dto.SessionScope, events []dto.TranscriptEvent) error
}

type ArtifactWriter interface {
    PutToolResults(ctx context.Context, scope dto.SessionScope, refs []dto.ArtifactResult) error
}
```

接口不放进 `store`，避免实现方拥有消费者契约。

### 8.3 Commit 拆分

当前 `store.Commit` 拆成三个聚合提交：

```text
SessionCommit         { Events, optional floor }
ArtifactCommit        { ToolResults }
CompactionRecordCommit{ Records }
```

应用层 `CommitTurn` 负责保持原写序：

1. 写不可变 artifact 并发布 refs 索引；
2. 发布 message 事件行与 message head；
3. 写 compaction records。

兼容期保留 `store.Commit` 作为 adapter DTO，并把它转成上述三个提交。

必须先确认现状：

- message 已发布后 compaction 失败，是否允许 message 继续可见。
- 重试同一次 `Commit` 的幂等边界是什么。
- artifact 已写但 message 未发布时，孤儿 artifact 如何回收。

若产品要求三个通道真正原子，应另立数据格式设计；本次拆分只固定并保持现有语义。

### 8.4 Facade 收敛

迁移期根包保留：

- 类型 alias，例如 `Event = protocol.TranscriptEvent`。
- `Router` 方法改为薄转发到领域实现。
- 能力出口 `BoardsFor`、`TeamworkFor`、`ToolRecordFor` 暂时保留，
  但调用方优先改用装配期注入的窄端口。

终态判据：

- `Router` 不再包含业务读写实现，只保留数据根/关闭/装配期通用能力。
- 外部直接 import 根 `store` 的生产文件归零，或只剩兼容测试。
- `SessionGranularStore` 不再聚合 context/artifact/queue 等全部能力；
  各消费方只注入所需端口。

## 9. 分批迁移

### 批次 0：特征测试与结构门禁

先写会红的测试或生成基线，不改业务实现：

1. 代表性会话的物理布局 golden：
   message shard、event shard、compact、retention、lifecycle、tool result、media。
2. `Commit` 写序特征测试：
   artifact、message、compaction 的可见性与 reopen 结果。
3. `Commit` 半失败特征测试：
   用可控失败点证明当前错误语义，并登记为兼容要求。
4. 锁身份测试：
   同模块同锁、不同模块不同锁、未知模块显式失败。
5. 方法面 ratchet：
   根包 God 类型的方法数只许下降，不允许新增领域方法。
6. import 边界：
   `./store/...` 全部纳入 I6；领域包禁止 import 根 `store` 或上层。

完成条件：

- 所有现状语义有可执行证据。
- 未决行为若与“整次提交失败”文案不一致，先报冲突，不自行改产品语义。

### 批次 1：Board 垂直试点

范围：

- `project_board.go`
- `project_board_markdown.go`
- 对应测试

步骤：

1. 在 `contract/dto` 增加最小 Board 行/读选项类型，JSON tag 逐字保持。
2. 根包增加同身份 alias，先切消费方类型引用。
3. 新建 `store/board`，迁移纯函数、路径校验、追加/折叠/压实实现。
4. `middle/view/board` 的端口改用 contract DTO。
5. 根 `Router` 只保留薄转发。
6. 删除根包中的重复常量、纯函数和私有实现。

完成条件：

- `go test ./store ./store/board ./middle/view/board ./e2e -count=1`
- 物理目录、文件白名单、行数上限、tombstone 折叠、压实结果 byte-for-byte 不变。
- 根包 Board 实现只剩转发，不存在第二份折叠/追加判据。

明确不做：

- 不搬 `board.go`。
- 不搬 `project_known.go`，等待 `ProjectRecord` 契约归属。

### 批次 2：Layout 内核

步骤：

1. 提取会话路径、guide、module head 信封与锁注册。
2. `ModuleID` 由领域包声明，移除中央模块 switch 的新增入口。
3. 将 head 重建回调移到各领域，layout 只提供读写原语和重试。
4. 根 `storeEngine` 的路径/head 方法委托 `layout`。

完成条件：

- 现有 head 自愈、Windows rename、共享读、锁热点与原测试全绿。
- 新模块无需修改 `sessionModuleLocks` 的字段集合。
- `go test ./store/... -run 'Head|Lock|Atomic|Pending' -count=1`

### 批次 3：Artifact

迁移：

- `big_tool_result.go`
- `media.go`
- `search_index.go`
- tool result 读写面

步骤：

1. 定义 artifact 引用 DTO；`ToolResult` 兼容 alias 到 contract。
2. 新建 `store/artifact`，按 blob/media/search 分文件。
3. application/backend 的消费方端口改为 artifact 窄接口。
4. 根 `jsonRepository` 的 media/tool-result 方法改为转调。

完成条件：

- 内容寻址、媒体配额、引用 GC、超大结果回读测试全绿。
- artifact 包不知道 transcript 或 board。
- message 可见前 artifact 已落盘的特征测试保持。

### 批次 4：Session 主聚合

最高风险批次，必须单独提交。

步骤：

1. 拆 `Commit` 为 Session/Artifact/Compaction 提交。
2. 在 application 层实现 `CommitTurn` 写序编排。
3. 新建 `store/session`，迁 message、event、draft、queue、retention、compact、
   fork、metadata。
4. `Router` 与 `SessionGranularStore` 仅转发。
5. 删除根包中的跨领域 `writeCommitLayout` 具体实现。

完成条件：

- 轮次提交、草稿不可见、publish tail、event 水位、LRU、compact 边界、
  session fork、subagent 副本测试全绿。
- 半失败行为与批次 0 的基线一致；任何行为变化必须先单独裁决。
- 根包不再持有 message/queue/compact 领域判据。

### 批次 5：Context

步骤：

1. 拆 `SessionContextRecord`：system prompt、栈记录、fork 血缘、审计各自由消费方 DTO 表达。
2. 新建 `store/context`，迁 stack journal、context state、checkpoint。
3. `wire_assembler` 迁往 engine context 域，store 只保留读端口兼容。
4. `backend/engine/context` 不再依赖完整 `SessionContextRecord`，只取所需快照。

完成条件：

- Plan/Task/Goal/Skill/Compact 栈语义不变。
- 冷恢复和 provider 前缀稳定性测试全绿。
- context 包不知道 media、teamwork、board。

### 批次 6：Teamwork / Role / WorkUnit

必须在当前 teamwork 契约迁移落定后开始。

步骤：

1. Teamwork 迁 plan/event/binding，复用现有 `contract/dto/teamwork_plan.go`。
2. Role 迁 role draft/权限。
3. WorkUnit 迁 node/subagent 记录与进度。
4. 跨域“整队收口”留在 application/teamwork 用例，不在 store 包建跨域事务方法。

完成条件：

- plan/event/binding 的折叠、校验、并发测试全绿。
- role sync 成功后才删 draft，floor 与 message 同步发布。
- workunit 恢复和主会话索引测试全绿。

### 批次 7：Facade 清零

步骤：

1. 统计并迁完根 `store.Router`、`Repository`、`SessionGranularStore` 的剩余调用方。
2. 删除能力发现出口和无生产调用方的兼容方法。
3. 根包只保留稳定 alias 或完全删除。
4. 更新 root README、模块 README、架构文档与 import 图。

完成条件：

- 根包 God 类型方法数归零或只保留真正的组合根职责。
- `Repository` 不再混合多个聚合；每个消费方只依赖自己的端口。
- `go list -deps ./store/...` 无向上依赖。
- 不再存在 `BoardsFor`/`TeamworkFor`/`ToolRecordFor` 这类泛化能力发现方法。

## 10. 测试策略

### 单元与特征测试

- 每个领域包保留现有行为测试，只改 import，不改断言。
- 新领域包的测试不得 import 根 `store`；用 contract DTO 与本包 API。
- 跨域一致性只测用例层，不通过一个包直接调用另一个领域的私有实现。

### 结构测试

- `e2e/store_boundary_test.go` 扩展所有 `store/*` 子包。
- 增加 AST/import 检查：
  - `store/<domain>` 不得 import 根 `store`；
  - 领域包之间不得直接 import；
  - 根兼容包的新增方法必须说明迁移去向。
- 增加锁身份基线：模块常量唯一、锁不静默别名。
- 增加方法面 ratchet：`Router`、`jsonRepository`、`storeEngine` 只许收缩。

### 集成与回归

每批至少执行：

```text
gofmt -l .
go build ./...
go vet ./...
go test ./store/... ./middle/view/board/... ./application/core/... ./wiring/... ./e2e/ -count=1
```

Session/Commit 批次额外执行：

```text
go test ./store/... ./backend/engine/session/... ./backend/engine/assemble/... ./test/ -count=1 -timeout=1200s
```

最终执行仓库门禁：

```text
go build -tags "gui,desktop,production" ./...
go test ./... -count=1 -timeout=1200s
node --test gui/frontend/dist/*.test.mjs
```

## 11. 数据与兼容策略

- 不改任何落盘路径、文件名、JSON tag、schema version 或 head payload。
- Board 迁移前后使用同一 fixture 做 byte/语义对照。
- 根包 alias 是同身份 alias，不做 JSON 往返转换。
- 兼容薄转发必须登记：旧面、新面、调用方、删除里程碑、归零验证。
- 用户数据不迁移、不清理；出现旧格式只保留读兼容。
- 不执行 `clean`、`reset`、目录移动等破坏性操作；文件移动必须先用补丁逐文件添加/删除。

## 12. 回滚方案

每批可独立回滚：

1. 停止把调用方切到新端口。
2. 恢复根包薄转发为原实现，或让组合根回指旧实现。
3. 保留新领域包但不装配，避免数据层回滚。
4. 删除新包只能在整个批次回滚且确认无用户数据依赖后进行。

禁止用 `git reset --hard`、`git checkout --` 或删除目录回滚；
回滚应通过反向补丁和测试证据完成。

## 13. 首两个提交建议

### Commit 1：只补方案门禁

- 新增本计划文档。
- 新增特征测试基线或方法/import ratchet，不改生产行为。
- 更新相关测试资料索引。

### Commit 2：Board 垂直切片

- Board DTO 进 `contract/dto`。
- 新建 `store/board`。
- `middle/view/board` 端口切到 contract DTO。
- 根包保留 alias/薄转发。
- 删除根包 Board 重复实现并在提交说明列出删除清单。

## 14. 需要用户确认的设计点

1. 是否接受“先 Board 试点，再 Session 主聚合”的顺序。
2. 是否接受本次拆分只锁定并保持现有 `Commit` 半失败语义，不顺手引入跨通道原子事务。
3. 是否接受迁移期根 `store` 保留兼容 alias/薄转发，而不是一次性删除 God 类型。
4. 是否同意 `wire_assembler` 最终迁出 store，归 engine context 域。
