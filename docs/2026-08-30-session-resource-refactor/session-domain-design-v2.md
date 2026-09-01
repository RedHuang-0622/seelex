# 会话域重构详细设计 v2（Session Domain Refactor — Detailed Design）

> 状态：红队独立草案。已被 [session-domain-design.md](./session-domain-design.md)
> 的对抗性修订版（v2.1 + §8 审查记录 + §9 实施记录）取代；本文件仅作参考保留。

> 日期: 2026-09-01
> 状态: 详细设计（待对抗性审查；审查通过后按 A–E 阶段实施）
> 前置: [niche-redesign.md](./niche-redesign.md)（生态位重划）、
>       [session-domain-design.md](./session-domain-design.md)（v1 定稿）、
>       [design-model.md](./design-model.md)（六元组与不变量 Ⅰ–Ⅳ）、
>       [test-cases.md](./test-cases.md)（既有用例规格）
> 本文替代 v1 中与「会话完全独立出 core」冲突的部分，其余沿用。

## 0. 一句话目标

**`session/` 成为会话的唯一所有者**：容器、生命周期、线程、事件、持久化编排全部
收进会话域；`application/core` 只保留执行算法（chat/tool/task/plan）与指向当前
会话的**视图指针 V**（以及为热加载回看持有的、只读的其它运行中会话视图引用）。
会话之间零共享；继承只走深拷贝；跨会话污染在类型层被禁止。

## 1. 现状事实清单（证据，全部来自当前代码）

### 1.1 会话容器目前散落在 core 的五处

| 资产 | 当前位置 | 关键符号 |
|---|---|---|
| 每会话可见投影（conversation/chat/readFiles） | `application/core/internal/state` | `state.Core.SessionViews map[string]*SessionView` |
| 每会话聊天运行态（running/queue/cancel/stream） | `application/core` 根包 | `serviceState.sessionChat map[string]*sessionChatRuntime`、`inputQueue`、`streamOutput`、`streamBatcher`、`cancelChat` |
| 每会话 task/plan 运行态分片 | `application/core/task_context` | `Coordinator.sessionStates map[string]*sessionTaskRuntime` |
| 每会话标题/名称 | `application/core/session_runtime` | `sessionTitles` / `sessionNames` |
| 每会话引擎实例 | `seelebridge`（已部分会话化） | `Runtime.bundles map[string]*sessionBundle`、`activeSessionID` |
| 全局 task 注册表 | `seelebridge` | `Runtime.tasks *task.TaskRegistry`、`sessionTaskSnapshots`、`currentTaskSessionID` |
| 全局 plan executor / 子代理树 | `seelebridge` | `Runtime.planExecutor`、`subagentTree`、`PlanNodeEventChannel` |

### 1.2 写路径没有单一入口

当前会话写回分散在多个入口，且部分**不带 sid**：

- `view_state.Coordinator.AppendMessageLockedFor`（带 sid，但被 `AppendMessageLocked`
  便捷包装成「活跃会话」）——`chat.go` 的 `appendMessageLocked` 走活跃会话；
- `chat.go` 的 `appendVisibleDelta` 先按 requestID 反查会话，再写 view；
- `work_table.go` 的 `syncTasksFromSources` 经 `TaskAdd`/`TaskSetStatus` 直写
  **全局注册表**（`currentTaskSessionID` 指向谁就写谁）——这是 task 跨会话的直接
  根因；
- `HandlePlanNodeComplete`（`plan_tools.go`）把 plan 事件投影到 `Snapshot.Runtime.Plan`
  **全局槽**，且 `dto.PlanNodeEvent` 没有 `SessionID` 字段；
- `hotAttachSession`（`session_lifecycle.go`）执行 `BindProjectRoot`（全局 project scope
  绑定）与 `SwitchSessionTasks`（全局注册表换血），这些是「视图操作写执行态」的
  典型违反 Ⅱ 的行为。

### 1.3 事件发件侧与前端消费侧均不按会话过滤

- `gui/bridge.go` `Start()` 用 `app.Subscribe(256)` 订阅**全量事件**并原样 emit，
  不区分会话；
- `gui/frontend/dist/protocol.js` `applyIncremental` 完全不看 `event.session_id`，
  后台会话的 `message.added` / `worktable.changed` / `task.changed` /
  `subagent.changed` 会直接改写当前快照——这是「运行中会话切不过去 / 恢复会话被
  污染」的直接根因；
- `event.Hub.SubscribeSession` 已有按会话过滤能力，但前端 bridge 未使用。

### 1.4 既有生命周期阶段（2c79333 已交付）是正确骨架，但容器仍留在 core

`session_lifecycle.go` 已有 `hotAttachSession` / `UnloadSession`、`session_scope.go`
已有 `SubmitToSession` / `ActivateSession` / `SnapshotOf` / `SubscribeSession`、
`session_runtime` 已有标题/目录/切换锁。**问题不是缺机制，而是机制长在 core 身上**：
`Service` 同时持有 `sessionChat`、`SessionViews`、`inputQueue` 等会话容器字段，
`state.Core` 是全局状态内核。重构 = 把这些字段连同它们的写路径整体搬到
`session/`，core 只留 V。

## 2. 目标架构

### 2.1 包与依赖方向（禁止循环）

```text
gui/tui ──→ application.Service（composition root，装配 session + core + seelebridge）

application/core（执行内核）──→ session.Ports（ViewWriter/EventBus/Lifecycle/Query）
                               └ 只经端口写状态、发事件；不 import session 实现细节
session（会话域）──→ sessionstore / workspace
session（会话域）──→ seelebridge（按会话持有 per-session 能力实例）
session（会话域）──→ core.ExecutorPort（接口，由 core 实现；session 只依赖接口）
```

规则：
1. `session/` 永远不 import `application/core` 及其子包的**实现**；只允许依赖
   `application/contract` / `application/model` / `application/event` /
   `sessionstore` / `workspace` / `seelebridge`（能力层）与
   `session.ExecutorPort`（接口定义在 session 包内）。
2. `application/core` 不持有任何会话容器字段（`sessionChat`、`SessionViews`、
   `inputQueue`、`streamOutput`、`sessionStates`、`sessionTitles` 全部搬走）；
   只保留：
   - `V *session.Unit`（当前会话视图指针，由会话域 bump 时通知更新）；
   - `Snapshot`（V 的只读镜像，协议对外形态）；
   - 执行算法与 CSP 消费者。
3. 若实现需要共享 `chat.VisibleOutputStream` / `chat.StreamBatcher` 等类型，把
   这些类型下沉到 `application/contract` 或会话域内的 `session/stream.go`
   （DTO 纯化 + 自由函数），禁止 session → core/chat 的实现依赖。

### 2.2 所有权表（目标）

| 资产 | 所有者 |
|---|---|
| 会话身份/标题/状态（I） | session.Unit |
| 持久记录 R | sessionstore（不变） |
| 工作内存 M（可见投影、聊天运行态、task/plan 运行态） | session.Unit（View/Chat/TaskScope） |
| 执行 X（chat loop、队列、取消、流） | session.Unit.Chat 持有容器，core 提供算法（ExecutorPort） |
| 绑定 B | workspace（不变）+ session.Unit.Binding |
| 上下文 C（四栈/checkpoint） | session.Unit.Context，core 装配 |
| 当前视图指针 V | core 只读引用（会话域 bump 时重建） |
| 目录/订阅（registry/bus） | session.Registry + event.Hub |
| 工具能力/引擎实例 | seelebridge（per-session bundle，会话域组装注入） |
| 执行算法 | application/core（chat/tool/task/plan） |

### 2.3 session 包内部结构（新骨架）

```text
session/
  ports.go        # ViewWriter / EventBus / Lifecycle / Query / ExecutorPort 接口
  registry.go     # Registry：sid → *Unit、TransitionLock、目录/订阅
  unit.go         # Unit 容器（六元组内存形态）
  view.go         # View：conversation/chat/readFiles 投影 + 有界尾窗
  chat_runtime.go # ChatRuntime：running/queue/cancel/stream 容器
  lifecycle.go    # COLD→PREPARED→LIVE(FG/BG)→COLD 状态机 + cold_load/hot_attach/unload
  task_scope.go   # TaskScope：per-session task/plan/subagent 实例（收口 seelebridge）
  persist.go      # 持久化编排（三读一写，全部显式 (projectID, sid) 键）
  engine.go       # per-session 引擎句柄（seelebridge bundle 的会话域视图）
  deepcopy.go     # 深拷贝边界：clone(P/R/C) 自由函数 + 测试钩子
  README.md       # 模块说明（readme-spec）
```

## 3. 会话单元（SessionUnit）与线程隔离协议

### 3.1 Unit 结构

```go
type Unit struct {
    ID       string
    State    LifecycleState          // COLD / PREPARED / LIVE(IDLE|FG-RUN|BG-RUN)
    Identity Identity                // title / status / display
    View     *View                   // 可见投影（M 的展示面）
    Chat     *ChatRuntime            // X 容器（running/queue/cancel/stream sink）
    Binding  *Binding                // workspace binding（B）
    Context  *ContextStack           // C 四栈 + checkpoint
    Tasks    *TaskScope              // per-session task/plan/subagent 实例
    Engine   engineHandle            // seelebridge per-session 引擎实例（句柄）
    mu       sync.Mutex              // 会话私有锁（短临界区）
    revision uint64
}
```

### 3.2 锁协议（防 ABBA、防跨会话串行）

1. **每会话一把私有锁** `Unit.mu`：只保护该会话容器字段的短写
   （append message / set chat state / task upsert）；provider/IO/落盘在锁外。
2. **会话域全局锁** `Registry.mu`：只保护 sid → Unit 映射、目录元数据；
   临界区仅指针级操作。
3. **切换协议**：`TransitionLock`（全局互斥，仅 cold_load / hot_attach / unload /
   SwitchTo / BindWorkspace 使用）+ 目标 Unit.mu；**加锁顺序固定为
   TransitionLock → Unit.mu**，禁止反序；测试断言（锁序静态检查 + 死锁超时护栏）。
4. **写路径单一入口**：core 的 runChat / tool 回调 / CSP 消费者只调用
   `session.Ports`（`AppendMessageFor` / `PublishEventFor` / `CommitTurn`），
   不触碰任何容器字段；编译期通过「core 无容器字段」+ 测试期通过「直接改容器
   字段的代码不存在」（`rg` 断言 + 集成测试）。
5. **执行不持锁**：`ChatStream` / tool 回调 / 落盘 I/O 全部在锁外；会话锁临界区
   与当前 core 的 `Core.Mu` 临界区同量级（微秒级）。
6. **互不串行**：每个会话的执行 goroutine 独立（`go service.runChat(ctx, sid, ...)`），
   队列按会话隔离；**禁止全局单飞门控**（当前 `anyChatRunningLocked` 的 M1 语义
   在重构后移除或降级为仅同会话防重入）。

### 3.3 状态机与非法迁移

```text
COLD ──cold_load(sid)──▶ PREPARED ──publish──▶ LIVE(IDLE)
LIVE(IDLE) ──submit──▶ LIVE(FG-RUN) ──switch(仅移 V)──▶ LIVE(BG-RUN)
LIVE(BG-RUN) ──hot_attach(仅移 V)──▶ LIVE(FG-RUN)
LIVE(任何) ──turn 完成──▶ LIVE(IDLE)
LIVE(IDLE) ──unload(flush+evict)──▶ COLD
```

非法迁移（必须被状态机拒绝并返回明确错误，边界测试覆盖）：
- RUN 中 unload → `ErrChatRunning`；
- PREPARED 中 hot_attach / switch → `ErrSessionNotPublished`；
- 同一 sid 双 cold_load（PREPARED 中再 cold_load）→ `ErrSessionPreparing`；
- 不存在的 sid switch/unload → `ErrSessionNotFound`；
- RUN 中 fork 父会话 → 既有 `ErrChatRunning` 契约保留。

## 4. 深拷贝继承语义

### 4.1 拷贝面（fork / cold_load 适用）

| 面 | 语义 |
|---|---|
| P（项目元数据） | `clone(workspace meta)` 深拷贝（id/name/root/git/project 知识） |
| R 前缀（对话/事件/tool-results/checkpoint） | `clone(R_i^prefix)`（seedLength 边界） |
| C（四栈 + checkpoint） | `clone(C_i)` 深拷贝 |
| M（引擎工作历史） | 从 `R_j` **重派生**（绝不复用父句柄/切片） |
| X（执行态） | 全新空运行态（queue/cancel/stream 零共享） |
| 共享面 | 只读服务快照、不可变常量（system 模板、limit 配置） |

### 4.2 测试断言

- fork 后改子会话的 view/chat/task 不得影响父会话（引用不相交）；
- `clone` 后对来源的修改不影响克隆体（深拷贝指纹：改后父/子哈希不变式）；
- cold_load 重建的 Unit 与旧 Unit 无共享可变引用。

## 5. 接口契约（ports）

### 5.1 session.Ports（会话域暴露给执行内核）

```go
package session

type ViewWriter interface {
    AppendMessageFor(sid, role, content string, tool *model.ToolCall) *model.Message
    AppendDeltaFor(requestID, messageID, chunk string)
    SetChatStateFor(sid string, chat model.ChatState)
    SetTitleFor(sid string, title model.SessionTitle)
}

type EventBus interface {
    PublishFor(sid, kind string, revision uint64, requestID string, payload any)
}

type Lifecycle interface {
    ColdLoad(sid string) error
    HotAttach(sid string) error
    Unload(sid string) error
    SwitchTo(sid string) error
    Transition() sync.Locker
}

type Query interface {
    SnapshotOf(sid string) (model.Snapshot, error)
    Subscribe(sid string, buffer int) (event.Subscription, error)
    Catalog() []model.SessionInfo
}
```

接口隔离：禁止单个 `session.Runtime` 上帝接口；core 只依赖其需要的子集
（ViewWriter + EventBus + 执行所需的 Query）。装配在 composition root 完成。

### 5.2 core.ExecutorPort（执行内核提供给会话域）

```go
type ExecutorPort interface {
    PrepareExecutionContext(ctx, sid, requestID string, input string) error
    ChatStream(ctx, sid string, onChunk func(string)) (string, error)
    Abort(sid string) bool
    ToolHooks(sid string) ToolHookSet
}
```

会话域在生命周期/执行事件驱动时经 ExecutorPort 调用 core；core 在 runChat 里经
session.Ports 回写。**双向都只依赖接口，装配只在 composition root。**

## 6. 事件路由与前端收口

### 6.1 事件分类

| 类别 | 示例 | session_id | 路由 |
|---|---|---|---|
| 会话负载 | message/tool/task/worktable/plan/subagent | 必填 | 目标会话订阅者 |
| 全局/目录 | catalog/runtime.changed/approval/suggestion/error | 空 | 全部订阅者 |

### 6.2 发件侧补齐 sid

- `dto.PlanNodeEvent` 增加 `SessionID string` 字段（`application/contract/dto/plan.go`）；
- `TaskChangedChannel` / `SubagentTreeEvents` / `PlanNodeEventChannel` 的 CSP 消费者
  在 `PublishFor` 时带上事件归属会话（C 阶段 per-session 实例天然携带）；
- `publishTaskChanged` / `publishWorkTable` / `publishRuntimeProjections` 全部改为
  `PublishFor(sid, ...)`。

### 6.3 前端消费侧

- `gui/bridge.go`：改为 `SubscribeSession(currentSID, 256)`（切换时重新订阅），
  第一道过滤；
- `gui/frontend/dist/protocol.js`：`applyEvent` 增加一致性校验：
  `event.session_id` 非空且 ≠ `snapshot.session.id` → 忽略（绝不 upsert）；
  `worktable.changed` / `task.changed` 同规则；payload 无 sid 时按事件 sid 推断；
- 切换即 resync：`SwitchTo(sid)` 成功后前端先取权威基线快照再跟随增量；
- 运行中会话视图 = 其自身事件流（后台事件不得进当前快照）。

## 7. 迁移阶段（每阶段独立验证 + 独立提交）

### S0 靶场先行（红 → 重构中保持红 → 完成转绿）

在 `application/core/session_pollution_s0_test.go` 基础上补齐五个用例
（当前应失败，重构完成后转绿）：

1. `TestS0BackgroundSessionEventsDoNotPolluteActiveSnapshot`
   （前端 protocol + 后端事件路由：后台 A 流式/task 变更不得进入当前 B 快照）；
2. `TestS0TaskAddRoutesToOwningSessionScope`
   （A 后台运行期间切到 B，A 的 plan/subagent 生命周期写 A 自己的 scope；B
   工作台不变；`TaskSnapshotFor(A)` 实时一致）；
3. `TestS0SwitchToRunningSessionResyncsBaseline`
   （切到运行中会话先基线后增量；不再 `ErrChatRunning`）；
4. `TestS0ForkDeepCopyIsolation`
   （fork 后父/子容器引用不相交）；
5. `TestS0SessionStateMachineRejectsIllegalTransitions`
   （双 cold_load / RUN 中 unload / PREPARED hot_attach 均拒绝）。

### A. 契约抽取（纯重构，行为不变）

- 新建 `session/` 包：`ports.go`、`registry.go`、`unit.go`、`view.go`、
  `chat_runtime.go`、`lifecycle.go`、`task_scope.go`、`persist.go`、`engine.go`、
  `deepcopy.go`；
- `session/ports.go` 定义 5.1/5.2 接口；`application/core` 提供实现（搬家不换
  行为：先用「适配器持 core 引用」的方式实现 ports，随后阶段再搬容器）；
- 编译期断言：`var _ session.ViewWriter = (*adapter)(nil)` 等；
- 事件指纹回归：相同输入序列 → 相同事件序列（新契约测试）。

### B. 容器上移

- `state.Core.SessionViews` → `session.Registry.Units[sid].View`；
- `serviceState.sessionChat` / `inputQueue` / `streamOutput` / `streamBatcher` /
  `cancelChat` → `session.Unit.Chat`；
- `task_context.sessionStates` → `session.Unit.Context` / `Tasks`；
- `session_runtime.sessionTitles` → `session.Unit.Identity`；
- core 只保留 `V *session.Unit` + `Snapshot` 镜像；所有写路径改端口调用；
- 编译期禁止：core 直接引用会话容器字段（删字段即编译错）。

### C. seelebridge 收口

- task registry：`Runtime.tasks` 从全局单例改为 `map[sid]*task.TaskRegistry`
  （或等价 per-session Scope），`TaskAddFor(sid, spec)` / `TaskSetStatusFor(sid, ...)`
  / `TaskSnapshotFor(sid)` 全部路由自身 scope；删除 `SwitchSessionTasks` 换血逻辑；
- plan executor / subagent tree 收进 per-session 实例；
- `PlanNodeEvent.SessionID` 字段 + 事件 channel 按 sid 分拣；
- `BindProjectRoot` 改为 per-session binding（或经会话域 Binding 解析后注入），
  删除 `hotAttachSession` 里的全局 `BindProjectRoot`。

### D. 前端收口

- bridge 第一道过滤 + protocol 一致性校验 + 切换 resync；
- 前端测试：`node --test` 161 例全绿 + 新增 sid 过滤用例。

### E. 清理遗留

- `session.Manager` legacy 桥降级为「存储适配」注释或删除（视 main.go 装配依赖）；
- 删除 `Snapshot` 全局槽字段（Conversation/Plan/WorkTable/Task 收进 SessionView）；
- `state.Core` 收窄为：Mu + Snapshot（V 镜像）+ Deps/Events/Approval + V 指针；
- core 只保留执行态。

## 8. 测试矩阵（三档）

### 8.1 边界测试（约束 C1：域不相交 + 状态机）

| 用例 | 断言 |
|---|---|
| 会话写必须经端口 | 编译期（core 无容器字段）+ `rg` 断言 + 集成测试 |
| 状态机非法迁移 | 双 cold_load / RUN 中 unload / PREPARED hot_attach 报错 |
| hot_attach 零写入 | 切换前后目标会话 X/M/R 指纹不变 |
| 深拷贝边界 | fork 后父/子 view/chat/task 引用不相交 |
| 事件 sid 一致性 | 非当前会话负载事件被忽略；目录事件放行 |
| 持久化键 | persist(i) 每路来源带 sid；(projectID, sid) 键不串 |

### 8.2 暴力测试（约束 C2：线程隔离 + 竞争）

| 用例 | 内容 |
|---|---|
| 并发 2–10 会话并行 | 同时 submit/流式/工具/切换/persist，`-race` 全绿 |
| 切换风暴 | 高频 SwitchTo 与后台完成交错；事件序列不串、无死锁 |
| 持久化并发 | 多会话同时收尾写同一 workspace 不同 key；原子性 |
| 内存压力 | 100 个 LIVE 会话 + 连续切换，`runtime.NumGoroutine` 有界 |
| 事件指纹稳定 | 相同脚本重复 20 次，事件序列一致（无乱序污染） |

> 注意：Windows 本地 `CGO_ENABLED=0` 时 `-race` 不可用；竞态用例在 Linux CI
> （`-race -covermode=atomic -coverpkg=./...`）上算数，本地用
> `go test -race` 仅在 CGO 可用时执行。

### 8.3 冒烟测试（约束 C3：功能不回归）

| 用例 | 内容 |
|---|---|
| 单会话全链路 | submit → 流式 → 工具 → persist → resume 全通 |
| 多会话基本切换 | 空闲会话互切、运行中会话 hot_attach 回看 |
| 前端协议 | 既有 `node --test` 161 例全绿 + 新增 sid 过滤用例 |
| GUI 启动冒烟 | `-frontend backend /help` 启动链路 + `seelex-flow` 冒烟脚本 |
| 发布/构建 | `go build ./...`、`-tags gui,desktop,production`、跨平台编译 |

## 9. 风险与决策点

- **范围风险**：结构性重构，坚持 A→E 每阶段独立提交；S0 靶场保持红直至完成。
- **决策点 1**：`session/` 留在仓库根（与会话域地位匹配，且不改变 core 包路径）。
- **决策点 2**：seelebridge 的 per-session task/plan 实例由**会话域组装并注入**
  （单一所有者），不由 seelebridge 内部按 sid 索引。
- **决策点 3**：legacy `session.Manager` 在 E 阶段**降级为存储适配**，删除前先
  确认 main.go 无直接依赖。
- **决策点 4**：前端过滤 **bridge 过滤 + protocol 防御**（双保险）。
- **既有文件**：`session_pollution_s0_test.go` 已存在（未跟踪），纳入 S0 靶场；
  `protocol.test.mjs` / `session-async-chain.md` 的用户改动保留。

## 10. 验收总纲

- 每阶段结束：`go test ./... -p 1` 全绿 + 相关包 `-race` 全绿 +
  前端 `node --test` 全绿 + S0 靶场（该阶段应转绿的用例）全绿；
- 已知两个症状（运行中会话视图切不过去、task 跨会话）在 S0 靶场里
  **先复现为红**，重构完成后转绿；
- E 阶段结束后：`rg -n "sessionChat|SessionViews|inputQueue|currentTaskSessionID"` 
  在 `application/core` 与 `seelebridge` 根包内零命中（仅 session/ 保留）。
