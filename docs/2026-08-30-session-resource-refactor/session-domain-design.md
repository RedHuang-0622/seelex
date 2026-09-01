# 会话域独立重构详细设计（Session Domain Independence — Detailed Design）

> **取代说明（2026-09-01）**：本文档中「自造生命周期状态机 / 并行 ChatRuntime /
> 项目粒度存储」部分已被 [thin-wrapper-session-design.md](../2026-09-01-session-thin-wrapper/thin-wrapper-session-design.md)
> 取代——会话性归 Seele（引擎+loop），seelex 只做薄封装；存储改会话粒度；
> 主/子代理会话同构。本文档保留的仍有价值部分：事件按会话路由、前端过滤、
> 对抗性审查记录（§8）与实施记录（§9）。

> 日期: 2026-09-01
> 状态: **设计稿 v2.1（已并入主代理对抗性审查结论；子代理审查并入第 8 节）**
> 前置: [niche-redesign.md](./niche-redesign.md)（生态位重划）、
>       [design-model.md](./design-model.md)（六元组与不变量）、
>       [plan.md](./plan.md)（资源/竞争/污染清单）、
>       [session-async-chain.md](./session-async-chain.md)（当前链路）

## 0. 总纲

### 0.1 一句话目标

**会话是一等资源、完全独立于 core**：`session/` 拥有每个会话的全部状态、生命周期、
线程与事件；`application/core` 只保留执行算法与「多会话视图指针注册表 V」；
会话之间零共享（继承只走深拷贝）；跨会话污染在类型层被禁止。

### 0.2 三条硬约束（对应三类测试）

| 约束 | 含义 | 测试档位 |
|---|---|---|
| C1 域不相交 | 会话 i 的任何状态写必须经会话域端口；core 不直接持有会话容器字段 | 边界测试 |
| C2 线程隔离 | 会话执行互不串行、互不污染；`-race` 全绿；切换只换视图指针 | 暴力测试 |
| C3 功能不回归 | 单会话全链路、多会话切换、前端视图与事件语义不受扰动 | 冒烟测试 |

### 0.3 验收总纲

- 每个迁移阶段结束时：`go test ./... -p 1` 全绿（允许的例外仅 S0 红靶场）+
  相关包 `-race` 全绿 + 前端 `node --test` 全绿。
- 已知两个症状（运行中会话视图切不过去、task 跨会话）在 S0 靶场里
  **先复现为红**，重构完成后转绿；禁止「症状消失但无测试」。
- 基线（2026-09-01 实测）：全量 `go test ./... -p 1` 唯一红 = S0 靶场
  `TestS0BackgroundSessionTaskWriteMustNotPolluteActiveRegistry`；其余包全绿。

## 1. 现状证据（重构前）

### 1.1 会话容器仍在 core

| 资产 | 当前位置 | 目标 |
|---|---|---|
| SessionViews（conversation/chat/readFiles 投影） | `application/core/internal/state.Core` | `session/` Unit.View |
| sessionChat 注册表（Running/queue/cancel/stream） | `application/core/serviceState.sessionChat` | `session/` Unit.Chat |
| inputQueue / streamOutput / streamBatcher / cancelChat / idle / draining | `application/core/serviceState`（全局镜像） | `session/` Unit（每会话一份） |
| 生命周期（cold_load/hot_attach/unload） | `application/core/session_lifecycle.go`、`session_history.go` | `session/` registry |
| 持久化编排/目录/标题/TransitionLock | `application/core/session_runtime` | `session/` |
| fork 深拷贝 | `application/core/session_runtime/fork.go` | `session/` |
| 可见快照镜像（view_state） | `application/core/view_state` | core 只留指针镜像（缩小） |

### 1.2 全局单槽（跨会话污染/竞争源）

| # | 全局槽 | 位置 | 危害 |
|---|---|---|---|
| G1 | `Snapshot.Conversation/Chat/Task`（当前会话镜像） | `state.Core.Snapshot` | 单槽，切换重建 |
| G2 | `Snapshot.Runtime.Plan / SubAgentTree / WorkTable` | `model.RuntimeState` | 后台会话写全局投影（P6） |
| G3 | seelebridge `tasks` 注册表 + SwitchSessionTasks 整体换血 | `seelebridge/ports.go` | **task 跨会话根因（S0 红）** |
| G4 | `Router.projectID` 写作用域 | `sessionstore` | 键漂移（阶段 0 已部分收敛） |
| G5 | `BindProjectRoot` 全局项目根 | `seelebridge.Runtime` | 路径作用域串写 |
| G6 | `inputQueue/streamOutput/cancelChat` 全局镜像 | `serviceState` | 切换期间串写 |

### 1.3 事件 sid 现状

- 已有 `publishSessionEvent(kind, revision, requestID, sessionID, payload)`，chat/
  tool/snapshot 事件多数已带 sid。
- **缺口**：`worktable.changed` / `task.changed` 发布走 `Events.Publish`（无 sid，
  且 requestID 取全局 `Snapshot.Chat.RequestID`）；`dto.PlanNodeEvent` 无 SessionID；
  `runtime.changed` 无 sid（正确，但前端需区分全局/会话事件）。
- 前端 `protocol.js applyEvent` **不校验 `event.session_id`** → 后台会话事件直接
  upsert 当前快照（用户可见症状 1）。

## 2. 目标架构

### 2.1 包布局与文件迁移映射

```text
session/                                  ← 会话域（升级为唯一所有者）
  ports.go       会话域对外端口（ViewWriter/EventBus/Lifecycle/Query/Runtime）
                  + 消费方声明的依赖端口（ExecutorPort/RenderPort/TaskPersistencePort/…）
  unit.go        Unit 容器（身份/视图/聊天运行态/绑定/上下文/task scope/引擎句柄）
  view.go        View 投影（自 state.SessionView 迁移；自带 RWMutex 与 Clone）
  chat.go        ChatRuntime（自 core.sessionChatRuntime 迁移）+ 每会话 worker/队列
  registry.go    Registry（sid→Unit 目录、当前视图指针、生命周期状态机、TransitionLock）
  lifecycle.go   cold_load / hot_attach / unload / switch 实现（自 core 迁移）
  persist.go     持久化编排 + 目录 + 标题（自 application/core/session_runtime 迁移）
  fork.go        深拷贝 fork（自 session_runtime/fork.go 迁移，写口 For 化）
  events.go      事件路由辅助（sid 校验、全局/会话事件分类）
  manager.go     legacy 存储桥保留（阶段 E 决定去留；不再承担会话用例）

application/core/                         ← 执行内核（收窄）
  service.go / service_assembler.go      门面与装配（组合根）
  chat.go                                runChat 执行算法（经 session.Ports 写回）
  tool_hooks.go / plan_tools.go          工具分发/plan 执行算法
  work_table.go                          工作台同步（For 端口，带 sid）
  task_context/                          每会话 task/plan 权威状态（实现 session 声明的端口）
  context_runtime/ prompt_layer/         上下文与提示词装配（执行算法）
  internal/state/state.go                **收窄**：Deps/Events/Approval + 视图指针注册表
                                          + Snapshot 当前镜像；实现 session.RenderPort
  view_state/coordinator.go              **收窄**：快照镜像/修订号（不再直接改视图内容）
  session_history.go / session_lifecycle.go / session_scope.go
  session_fork.go / session_draft.go    迁移后删除或瘦身为薄委托
  session_runtime/                       迁移后删除
```

### 2.2 所有权表

| 资产 | 所有者 |
|---|---|
| 会话身份/标题/状态 I | session |
| 持久记录 R | sessionstore（不变）+ session 编排 |
| 工作内存 M（View/Chat/TaskScope/Plan 投影） | session（容器）+ seelebridge（per-session 实例） |
| 执行 X（chat/tool/task/plan 算法） | core（算法）；session 持有队列/worker/取消 |
| 绑定 B | workspace（不变）+ session 查询 |
| 上下文 C（四栈） | session 容器持有引用；core 装配内容 |
| 视图指针注册表 V | core（`state.Core.Views map[sid]*session.View`，只读指针） |
| 目录/订阅 | session + event.Hub |
| 工具能力/引擎实例 | seelebridge（per-session 实例，session 组装） |

### 2.3 依赖方向（禁止循环）

```text
gui/tui ──→ application.Service（组合根门面）
application/core ──→ session.Ports（ViewWriter/EventBus/Lifecycle/Query）
session ──→ sessionstore / workspace / seelebridge 类型
session ──→ core.ExecutorPort（接口，由 core 实现，装配注入）
session ──→ core.RenderPort（接口，由 core 的 state.Core 实现）
```

规则：
1. `session` 永不 import `application/core` 的实现包；依赖全部为消费方声明的接口。
2. core 永不持有会话容器字段（`sessionChat` map、`SessionViews` map 迁出）；
   只保留 `Views map[sid]*session.View` 指针注册表与 Snapshot 镜像。
3. 端口定义所在包 = 消费方（core 需要的端口定义在 session；session 需要的端口
   定义在 session 的 ports.go，由 core 实现）。组合根负责装配。

### 2.4 接口契约

#### 2.4.1 session.Ports（core 依赖的会话域端口）

```go
package session

// ViewWriter 可见投影写口（core 执行算法唯一写回通道）。
type ViewWriter interface {
	AppendMessageFor(sid, role, content string, tool *model.ToolCall) (*model.Message, error)
	AppendDeltaFor(requestID, messageID, chunk string)
	AttachReasoningFor(sid, requestID, reasoning string)
	SetChatStateFor(sid string, chat model.ChatState)
	SetReadFilesFor(sid string, readFiles []model.ReadFileRef)
	SetTitleFor(sid string, title model.SessionTitle)
}

// EventBus 会话事件发口（会话负载事件 sid 必填；空 sid = 全局/目录事件）。
type EventBus interface {
	PublishFor(kind event.EventKind, revision uint64, requestID, sid string, payload any) event.Event
	BumpRevisionFor(sid string) uint64
}

// Lifecycle 生命周期与切换（全部走状态机，非法迁移返回错误）。
type Lifecycle interface {
	ColdLoad(sid string) error
	HotAttach(sid string) error
	Unload(sid string) error
	SwitchTo(sid string) error
	Transition() sync.Locker
}

// Query 查询与订阅。
type Query interface {
	CurrentSessionID() string
	ViewFor(sid string) *View
	SnapshotOf(sid string) (model.Snapshot, error)
	SubscribeSession(sid string, buffer int) (event.Subscription, error)
	Catalog() []model.SessionInfo
}

> **v2.1 修订（F5）**：`SnapshotOf(sid)` 对**任意 LIVE 会话**生效（不再只有当前
> 会话）：从该会话 Unit.View（Clone）+ per-session task/plan scope 组装。这是
> 「运行中会话视图切不过去」修复的前端数据面。

// Runtime 是会话域门面（组合根/桥接层消费；core 只依赖上面窄端口）。
type Runtime interface {
	ViewWriter
	EventBus
	Lifecycle
	Query
	Submit(sid, text string) error          // 入队 + 触发 worker
	LoadMoreHistory(sid string, limit int) error
	TaskSnapshotFor(sid string) []dto.TaskRecord
	PlanStackFor(sid string) []model.SessionPlanFrame
}
```

#### 2.4.2 session 声明的消费方端口（core 实现）

```go
// ExecutorPort 由 application/core 实现；session 的每会话 worker 调用。
type ExecutorPort interface {
	// RunTurn 执行一轮 ReAct（输入→流式→工具→收尾）；写回经 ViewWriter/EventBus。
	RunTurn(ctx context.Context, sid string, request TurnRequest) error
	// Abort 取消指定会话的进行中 turn。
	Abort(sid string) bool
	// OnSessionLoaded 冷加载完成后 core 侧装配（引擎/context 挂接）。
	OnSessionLoaded(sid string) error
	// OnSessionActivated 切换视图后 core 侧投影刷新（system prompt/task/plan 投影）。
	OnSessionActivated(sid string)
	// OnSessionUnloaded 卸载前 core 侧释放（引擎卸载/context 解绑）。
	OnSessionUnloaded(sid string) error
}

// RenderPort 由 core 的 state.Core 实现；session 发布视图指针与修订号。
type RenderPort interface {
	SetViewPointer(sid string, view *View)
	RemoveViewPointer(sid string)
	BumpRevision() uint64
	PublishSnapshotChanged(revision uint64, sid string)
}

// TaskPersistencePort 由 core 的 task_context 实现（自 session_runtime/ports.go 原样迁移）。
type TaskPersistencePort interface { /* 全部 For 变体，见现文件 */ }
```

#### 2.4.3 接口隔离原则

- 禁止单一 `session.Runtime` 上帝接口被 core 持有：core 只依赖
  `ViewWriter + EventBus + Query.ViewFor/CurrentSessionID`（组合注入）。
- `Lifecycle`/`Submit` 只在组合根/桥接层可见。
- 会话域内部实现可自由组合，对外只暴露端口。

### 2.5 会话单元 Unit

```go
type Unit struct {
	ID        string
	Draft     bool
	State     LifecycleState      // COLD/PREPARED/LIVE
	Running   bool                // 派生：worker 是否在跑 turn
	Identity  Identity            // title/status/display
	View      *View               // 可见投影（自带 RWMutex + Clone）
	Chat      *ChatRuntime        // queue/cancel/stream（每会话）
	Binding   *workspace.Binding  // 工作区绑定
	Context   *ContextHandle      // 四栈/checkpoint 引用（session 容器，core 装配）
	Tasks     *task.Scope         // per-session task/plan scope（seelebridge 实例句柄）
	Engine    contract.SessionChatEngine // per-session 引擎句柄
	mu        sync.RWMutex        // 会话私有锁（短临界区）
	nextMsgID uint64
}
```

> **v2.1 修订（主代理审查发现 F1）**：`nextMsgID` 只作展示/诊断，**消息 ID 的
> 分配必须进程全局唯一**（前端 upsert 以 message ID 为键，跨会话复用会撞键）。
> 实现为 `session` 包级 `atomic.Uint64` 单调计数器，Unit 不持有独立计数。

### 2.6 生命周期状态机

```text
COLD ──cold_load(sid)──▶ PREPARED ──首次渲染──▶ LIVE(IDLE)
LIVE(IDLE) ──Submit──▶ LIVE(RUNNING) ──turn 完成──▶ LIVE(IDLE)
LIVE(任意) ──SwitchTo(仅换 V)──▶ LIVE(任意)         （目标会话视图成为当前）
LIVE(IDLE) ──unload(flush+evict)──▶ COLD
```

非法迁移（必须拒绝并返回明确错误，边界测试覆盖）：

| 迁移 | 拒绝原因 |
|---|---|
| 双 cold_load 同一 sid | 已驻留 |
| RUNNING 中 unload | 执行态未结束 |
| COLD 中 hot_attach | 未驻留 |
| RUNNING 中再 Submit（同会话） | 入队而非启动（合法路径是入队） |
| draft 参与 ColdLoad/HotAttach | 草稿非持久会话 |

### 2.7 线程隔离协议

1. **每会话一把私有锁**（`Unit.mu`），只保护该会话容器的短写；provider I/O、
   工具回调、落盘全部在锁外。
2. **registry 全局锁**只保护 `map[sid]*Unit` 与当前指针；临界区仅指针级操作。
3. **切换协议**：`TransitionLock`（全局互斥，仅 cold_load/hot_attach/unload/
   switch/bind 使用）+ 目标 Unit 私有锁；**加锁顺序固定 TransitionLock →
   Unit.mu → View.mu**，禁止反序，杜绝 ABBA。
4. **写路径单一入口**：所有会话状态修改经 session 端口；core 的 runChat 只调用
   端口，不触碰任何容器字段。
5. **执行不持锁**：runChat/tool 回调/落盘 I/O 全程不持任何锁；锁临界区微秒级。
6. **每会话一个 worker**：`ChatRuntime.queue` 由该会话专属 goroutine 消费；
   worker 空闲时阻塞在自己的 wake channel 上；取消走 `Unit.Chat.cancel`。
7. **移除 M1 单飞闸门**：`anyChatRunningLocked` 全局串行语义删除（当前代码已
   只在 runChat 尾部用于 idle 判定；切换路径已允许运行中回看）。切换 = 只换
   视图指针，绝不等待其它会话执行完成。
8. **锁纪律（v2.1 修订，主代理审查发现 F2/F3）**：
   - 所有 View 内容变更必须持 `View.mu`（写）；`Unit.mu` 保护容器字段，
     `View.mu` 保护投影数据，二者可嵌套（Unit.mu → View.mu）。
   - **禁止持任何会话锁时调用 core 端口**（RenderPort.SetViewPointer /
     BumpRevision / PublishFor）：会话域先改完（锁内），释放锁，再发布。
   - 快照镜像锁序固定 **Core.Mu → View.mu(RLock)**：core 组装 Snapshot 时
     先取 Core.Mu（镜像会话状态/修订号），再对当前 View 做 Clone（RLock）。
     会话域从不取 Core.Mu，因此不存在 Core.Mu ↔ Unit.mu/View.mu 的 ABBA。
   - `Snapshot()` 与 `SnapshotOf(sid)` 的 View 克隆走 `View.Clone()`（自带
     RLock），禁止直接读共享 slice 头。

### 2.8 继承只走深拷贝

- fork / cold_load 拷贝面：`P_i`（项目元数据）、`R_i` 前缀（对话/事件/
  tool-results/checkpoint）、`C_i`（四栈）→ 全部深拷贝；`M_i` 从 `R_j` 重派生
  （引擎工作历史重建，不复用父引用）；`X_j` 全新空执行态。
- 唯一共享面：只读服务快照与不可变常量（system 模板、limit 配置）。
- 测试断言：fork 后改子会话的 view/chat/task 不得影响父会话（引用不相交）。

## 3. 事件路由与前端收口

### 3.1 事件清单

| kind | sid 要求 | 路由 | 现状 |
|---|---|---|---|
| snapshot.changed | 会话负载带 sid；目录刷新空 sid | 会话订阅/全部 | 多数已带 |
| message.added / message.delta | 必填 | 会话 | 已带 |
| tool.started / tool.completed | 必填 | 会话 | 已带 |
| subagent.changed / subagent.tool.* | 必填 | 会话 | 需补（PlanNodeEvent 无 sid） |
| worktable.changed | 必填 | 会话 | **缺口：无 sid** |
| task.changed | 必填 | 会话 | **缺口：无 sid** |
| interaction.opened / closed | 必填 | 会话 | 需补 |
| error | 必填 | 会话 | 已带 |
| runtime.changed | **会话 scoped（携带收集时 sid）** | 会话（v2.1 修订） | **现状错误：载荷含 Plan/WorkTable/SubAgentTree，却按全局发布 → 后台会话污染当前快照** |
| resync.required / exit_requested | 空 | 全部 | 正确 |

> **v2.1 修订说明（F4）**：`runtime.changed` 的载荷（model.RuntimeState）包含
> Plan/SubAgentTree/WorkTable 等会话专属字段。现状 `CollectRuntimeProjection`
> 读全局活跃引擎/注册表。重构后 runtime 投影按「收集时当前会话」发布并携带
> sid；前端只对当前会话应用。切换后基线 resync 重新装载新会话的 runtime。
> 目录/全局类快照变化继续走空 sid 的 snapshot.changed。

### 3.2 DTO 变更

- `dto.PlanNodeEvent` 增加 `SessionID string` 字段。
- `dto.SubagentEvent` / `SubagentTreeEvents` 信号按会话归属（channel 保持 CSP，
  消费者从 ctx/事件带 sid 路由）。
- 新增 `dto.SessionScopedEvent` 辅助类型（可选，便于前端校验）。

### 3.3 前端协议变更

`gui/frontend/dist/protocol.js`：

```js
export function applyEvent(snapshot, event, lastSeq = 0, snapshotRevisionFloor = 0) {
  // …现有 seq/协议校验…
  // 新增：会话负载事件必须与当前快照会话一致，否则忽略（绝不 upsert）。
  if (event.session_id && snapshot?.session?.id && event.session_id !== snapshot.session.id) {
    return { snapshot, lastSeq: seq, needsRefresh: false }; // 后台会话事件丢弃
  }
  // …现有 applyIncremental…
}
```

`gui/frontend/dist/client-state.js`：
- `acceptSnapshot` 记录当前 `session.id`。
- 切换会话（`ActivateSession` 成功）后：先 `acceptSnapshot(SnapshotOf(sid))` 重置
  基线，再继续增量；禁止只靠增量补历史。
- `handleEvent` 收到 `resync.required` 时走基线重置。

`gui/bridge.go`：
- `Start()` 增加第一道过滤：事件为会话负载且 `session_id != 当前快照会话` 时
  丢弃（协议层第二道防御，双保险）。

> **v2.1 修订（F6）**：Bridge 维护 `currentSessionID` 字段（初始来自
> `Snapshot().Session.ID`；在 ResumeSession/ActivateSession/BeginNewSession/
> SubmitToSession 成功返回后更新）。事件过滤用该字段，**禁止在事件热路径调用
> `app.Snapshot()`**（克隆整份快照代价过高）。

### 3.5 草稿会话（v2.1 补充，F7）

- draft 是会话域内的特殊 Unit（`Draft=true, ID=""`），拥有自己的 View/Chat
  容器与工作区绑定；`materializeDraftSession` 时转为真实 Unit。
- draft 事件发布 sid 为空（视为全局/当前），前端 sid 过滤对 draft（当前
  快照 session.id 为空）放行。
- BeginNewSession = session 域创建/切换 draft Unit；首次提交 = 物化。

### 3.4 切换协议（前端）

```text
用户切页 → Bridge.ActivateSession(sid)
  → application.Runtime.SwitchTo(sid)（只换视图指针）
  → 前端 acceptSnapshot(SnapshotOf(sid))（权威基线，revision/seq 重置）
  → 之后跟随会话事件增量
```

## 4. seelebridge 收口（阶段 C）

### 4.1 per-session task scope

`Runtime` 内部：

```go
type Runtime struct {
	// …既有字段…
	sessionTasks map[string]*task.Registry   // per-session 注册表（替代单一 r.tasks）
	sessionTaskMu sync.RWMutex
	currentTaskSessionID string               // 兼容层：默认路由目标
}
```

- 新增 For 端口：`TaskAddFor(sid, spec)`、`ResolveTaskByKeyFor(sid, key)`、
  `TaskSetStatusFor(sid, id, status, evidence)`、`TaskAttachParticipantFor(...)`、
  `TaskSnapshotFor(sid)`（改读 per-session 注册表实时快照，不再读切换保存快照）。
- `TaskAdd/TaskSetStatus/...`（无 For）退化为「当前会话路由」兼容层，内部实现
  即 `TaskXxxFor(currentTaskSessionID, …)`；`SwitchSessionTasks` 改为仅记录
  `currentTaskSessionID`（不再 ReplaceAll 换血）——**S0 靶场转绿的关键**。
- `SetCurrentTaskBatch` 保持按会话存储；per-session 注册表创建时应用默认批次。
- `TaskChangedChannel` 保持 CSP，但事件发布按归属会话（consumer 按 sid 过滤）。

### 4.2 plan/subagent per-session 实例

- `PlanNodeEventChannel` 输出事件补 `SessionID`；`consumePlanNodeEvents` 按 sid
  路由到目标会话的投影（不再改全局 `Snapshot.Runtime.Plan`）。
- subagent tree 锚点按会话（`RestoreSubagentAnchors(sid)` 已存在；后台会话树
  事件按 sid 投递）。
- `BindProjectRoot` 收口：per-session 根绑定（新增 `BindProjectRootFor(sid)`）或
  运行中禁止改根；阶段 C 先实现 For 变体。

### 4.3 Runtime 全局槽移除清单

- 删除 `SwitchSessionTasks` 的 ReplaceAll 语义（保留方法签名，行为=记录当前 sid）。
- `TaskSnapshotFor(sid)` 不再读 `sessionTaskSnapshots` 切换快照（改为实时注册表）。
- `sessionTaskSnapshots` 字段删除（无换血即无需快照）。

## 5. 迁移阶段（每阶段独立验证，不做大爆炸）

### S0 靶场先行（先写红，重构期间保持绿）

新增/固化测试（当前应红）：

| 测试 | 断言 | 位置 |
|---|---|---|
| TestS0BackgroundSessionTaskWriteMustNotPolluteActiveRegistry | A 后台 plan 任务不得进 B 注册表；A scope 实时一致 | application/core（已红） |
| TestS0BackgroundEventsDoNotPolluteActiveSnapshot | 后台会话 message/task/worktable 事件被前端丢弃 | gui/frontend/dist/*.test.mjs |
| TestS0SwitchResyncsBaseline | 切换后先基线后增量（前端协议） | gui/frontend/dist/*.test.mjs |
| TestS0ForkDeepCopyIsolation | fork 后父/子容器引用不相交 | application/core |
| TestS0StateMachineRejectsIllegalTransitions | 非法迁移报错 | session（新包） |

### A. 契约抽取（纯重构，行为不变）

- 建立 `session/` 包骨架：ports.go/unit.go/view.go/chat.go/registry.go（先空壳
  或仅类型）；把 `session_runtime/ports.go` 的接口定义迁到 `session/ports.go`。
- core 侧 `state.Core` 增加 `Views map[sid]*session.View` 指针注册表并实现
  `session.RenderPort`（先只登记不改变写路径）。
- 装配根注入 ExecutorPort/RenderPort；`go build ./...` 全绿；事件指纹回归：
  相同输入序列 → 相同事件序列。

### B. 容器上移（核心阶段）

- `SessionViews` + `sessionChat` + inputQueue + streamOutput/streamBatcher +
  cancelChat/idle/draining 迁入 session 域 Unit/Registry。
- core 写路径全部改端口调用（`service.sessionChatLocked` → `session.ViewWriter/
  EventBus`；`service.appendMessageLocked` → `session.AppendMessageFor`）。
- `view_state.Coordinator` 收窄为「读当前 Unit.View → 镜像 Snapshot」；
  messageSeq/窗口裁剪随 View 进入 session 域。
- `session_history.go`（cold_load）/ `session_lifecycle.go`（hot_attach/unload）/
  `session_draft.go` 迁移为 registry 状态机方法；core 只保留薄委托或删除。
- 门禁：S0 靶场保持红（task 问题在 C 修复）；`go test ./... -p 1` 全绿（除
  S0 红）；相关包 `-race` 全绿；新增边界测试转绿。

### C. seelebridge 收口

- 4.1/4.2 全部落地；`work_table.go` 同步函数改 For 端口并携带
  `withSessionID(ctx)` 中取出的 sid；`worktable.changed`/`task.changed` 事件带
  sid；`PlanNodeEvent.SessionID` 补齐。
- 门禁：**S0 靶场转绿**；新增 task 路由边界测试全绿；`-race` 全绿。

### D. 前端收口

- protocol.js sid 过滤 + client-state resync + bridge 第一道过滤；
  `node --test` 全绿（含新增 S0 前端红测试转绿）。
- 门禁：前端测试全绿；`go test ./... -p 1` 全绿。

### E. 清理遗留

- 删除 `session.Manager` legacy 桥（或降级为存储适配器并注明）；
- 删除 `Snapshot` 全局槽字段（Plan/SubAgentTree/WorkTable 收进 per-session
  投影；`Snapshot.Conversation/Chat/Task` 保留为当前会话镜像）；
- 删除 `sessionTaskSnapshots`、`SwitchSessionTasks` ReplaceAll、`anyChatRunning
  Locked` 等遗留；core 只保留执行态与 V 指针。
- 门禁：全量验证（见 §6.3 冒烟）。

## 6. 测试计划（三档）

### 6.1 边界测试（约束 C1）

| 用例 | 断言 |
|---|---|
| 会话写必须经端口 | 编译期（core 无容器字段）+ 测试期（core 包内不允许出现 sessionChat 字段） |
| 状态机非法迁移 | 双 cold_load / RUNNING 中 unload / COLD 中 hot_attach 报错 |
| hot_attach 零写入 | 切换前后目标会话 X/M/R 字节级不变（快照指纹） |
| 深拷贝边界 | fork 后父/子 view/chat/task 引用不相交，改子不影响父 |
| 事件 sid 一致性 | 非当前会话负载事件被忽略；目录/全局事件放行 |
| task 写自有域 | `TaskAddFor(sid)` 写自身 scope；`TaskSnapshotFor(sid)` 实时一致 |
| plan 事件归属 | PlanNodeEvent 带 sid 且只更新自身投影 |
| 持久化键 | persist(i) 每路来源带 sid；(workspaceID, sid) 键不串 |

### 6.2 暴力测试（约束 C2：竞争）

| 用例 | 内容 |
|---|---|
| TestConcurrentSessionsParallel | 2–10 会话并行 submit/流式/工具/切换/persist，`-race` 全绿 |
| TestSwitchStorm | 高频 SwitchTo 与后台完成交错；事件序列不串、无死锁（带超时） |
| TestPersistConcurrency | 多会话同时收尾写同一 workspace 不同 key；原子性 |
| TestGoroutineBound | 100 个 LIVE 会话 + 连续切换，`runtime.NumGoroutine` 有界 |
| TestEventFingerprintStable | 相同脚本重复 20 次，事件序列一致（无乱序污染） |

### 6.3 冒烟测试（约束 C3：功能不回归）

| 用例 | 内容 |
|---|---|
| 单会话全链路 | submit → 流式 → 工具 → persist → resume 全通 |
| 多会话基本切换 | 空闲会话互切、运行中会话 hot_attach 回看 |
| 前端协议 | 既有 `node --test` 全量 + 新增 sid 过滤用例 |
| GUI 启动冒烟 | `-frontend backend /help` 启动链路 + seelex-flow 冒烟脚本 |
| 发布/构建 | `go build ./...`、`-tags gui,desktop,production`、跨平台编译 |

靶场素材：`dist/seelex-gui-dev/.seelex/` 下 dev 会话（sess_1788089895720214200
等）做真实场景输入（仅结构，不含密钥）。

## 7. 风险与决策

| 决策点 | 决策 | 理由 |
|---|---|---|
| `session/` 位置 | 留在仓库根 | 与会话域地位匹配，不改 core 包路径 |
| per-session worker 归属 | session 域 | 队列/取消/流是会话资源 |
| 执行算法归属 | core | chat/tool/task/plan 算法留在执行内核 |
| 锁粒度 | TransitionLock → Unit.mu → View.mu | 固定序杜绝 ABBA |
| Snapshot 镜像 | core 持 V 指针 + 按需克隆 | 满足「core 只留视图指针」 |
| legacy Manager | 阶段 E 前保留，E 决定降级 | 避免大爆炸 |
| 前端过滤 | bridge + protocol 双保险 | 后端再出错也不静默串写 |

风险登记：
- **行为漂移**（重构改变事件顺序/持久化内容）→ 事件指纹回归 + S0 靶场。
- **锁序错误** → 固定锁序 + 暴力测试 -race。
- **runChat 迁移遗漏**（某写路径绕过端口）→ 编译期断言 + 代码审查清单。
- **前端 resync 竞态**（切换瞬间旧事件到达）→ bridge 第一道过滤 + protocol 校验 +
  acceptSnapshot 重置基线。
- **测试面大**（core 约 50 个测试文件）→ 每阶段全量门禁，先保绿再前进。

## 8. 对抗性审查记录

### 8.1 主代理对抗性审查（v2 → v2.1，已并入设计）

| # | 严重级 | 发现 | 处置 |
|---|---|---|---|
| F1 | blocker | 消息 ID 若按会话独立计数会跨会话撞键（前端 upsert 以 ID 为键） | 修订 §2.5：进程全局 atomic 计数器 |
| F2 | blocker | 锁序未闭合：core 镜像（Core.Mu→读 View）与会话域发布（Unit.mu→调 core 端口）可能 ABBA | 修订 §2.7.8：禁止持会话锁调 core 端口；快照锁序 Core.Mu→View.mu(RLock) |
| F3 | blocker | View 内容共享 slice 头无同步 → 扩容/镜像竞争 | 修订 §2.7.8：View 变更必须持 View.mu；Clone 走 RLock |
| F4 | blocker | runtime.changed 载荷含会话专属 Plan/WorkTable/SubAgentTree 却按全局发布 | 修订 §3.1：runtime.changed 改会话 scoped（携带收集时 sid） |
| F5 | major | SnapshotOf 仅当前会话可读，无法支撑「运行中会话回看」 | 修订 §2.4：SnapshotOf 对任意 LIVE 会话生效 |
| F6 | minor | bridge 过滤若每次事件调 app.Snapshot() 代价过高 | 修订 §3.3：Bridge 维护 currentSessionID 字段 |
| F7 | minor | draft 会话在迁移映射中未明确定位 | 修订 §3.5：draft = 会话域特殊 Unit |

### 8.2 子代理审查（待并入）

> **结论（2026-09-01）**：子代理通道在本环境不可用（代理线程卡死/消息未送达，
> 三次尝试均未产出报告，已中断释放）。对抗性审查改由主代理按三视角（架构边界
> / 并发隔离 / 兼容测试）独立完成，结论并入 §8.1（F1–F7）与 §5 实施门禁。
> 实施过程中的对抗性发现（均由测试抓到后修复）：
>
> | # | 严重级 | 发现 | 处置 |
> |---|---|---|---|
> | R1 | blocker | `SubmitToSession` 先读 current 再委托 `Submit`（内部重读 current），切换落在两次读之间 → A 的输入路由进 B 的队列（压力测试抓到 queued-2 进 sess-4） | 路由只认显式 sid，删除 current 重读（session_scope.go） |
> | R2 | major | 测试桩 `ChatStreamFor` 的 `close(started)` 无锁竞争 → double-close/nil channel 抖动 | 锁内建通道 + 守卫 close（session_parallel_test.go） |
> | R3 | major | 测试桩 debugLog 参数在锁外读 map → -race 竞争 | 锁内取值再日志 |
> | R4 | major | hot_attach/cold_load 全局 `BindProjectRoot` + `SetWorkspace`：后台会话运行中切换会改根（P3/G5） | `bindProjectRootIfSafe`：有其它会话运行中时跳过全局重绑（session_scope.go） |
> | R5 | minor | `planProjections` 缓存未随 unload 清理 | unload 时 delete |

## 9. 实施记录（2026-09-01）

### 9.1 阶段状态

| 阶段 | 内容 | 状态 |
|---|---|---|
| S0 | 靶场红测试（core 污染 / 前端 sid / task 分区 / 生命周期 / 深拷贝 / 切换 resync） | 完成，全部转绿 |
| A | 契约抽取：`session/domain.go`（Domain/Unit/View/ChatRuntime + 状态机）、`contract.RuntimePort` For 端口、装配接线 | 完成 |
| B | 容器上移：SessionViews/sessionChat/inputQueue/stream/cancel 迁出 core；`chatRuntimeLocked` 委托会话域；全局镜像字段删除 | 完成 |
| C | seelebridge per-session task 分区（TaskAddFor 等）+ SwitchSessionTasks 换血修正 + PlanNodeEvent.SessionID | 完成 |
| D | 前端：protocol.js sid 过滤 + client-state 切换基线 + bridge 第一道过滤（currentSessionID） | 完成 |
| E | 清理：core 容器字段清零、planProjections 收口、BindProjectRoot 安全守卫、README 同步 | 完成 |

### 9.2 与设计的偏差（均为有意的范围收敛，测试已验证）

1. **View 未加独立锁**：所有 View 写仍经 Core.Mu 单锁（当前实现 race 干净，
   `-race` 全绿）；每会话锁已落在 ChatRuntime（队列/cancel/stream）与 Domain 目录。
   per-session View 锁留待后续（当前临界区为微秒级，不构成会话间串行瓶颈）。
2. **task 分区采用快照分区**（`sessionTaskSnapshots map[sid][]TaskRecord`）而非
   完整 per-session `task.Registry`：隔离语义（写自有域、实时一致、切换不换血）
   与 S0 靶场全部满足；per-session Registry 的全功能（批次/todo/事件通道）留待
   seelebridge 能力层后续演进。
3. **BindProjectRoot 用安全守卫**（运行中不改根）替代 per-session 根绑定：
   避免后台会话路径工具被切根；per-session project scope 属 seelebridge 能力层
   后续项。
4. **bridge 过滤用 currentSessionID 跟踪**（方法内更新）替代 SubscribeSession
   重订阅：效果等价、无需切换时重建订阅 goroutine。
5. **消息 ID 全局唯一**经 view_state 单一 messageSeq 计数器保持（跨会话不撞键）。

### 9.3 验证结果

- `go test ./... -p 1 -count=1`：全绿（含 S0 靶场、压力、分区、生命周期测试）。
- `go test -race ./application/core ./session ./seelebridge ./gui`：全绿。
- 前端 `node --test gui/frontend/dist/*.test.mjs`：165/165 通过。
- `go build -tags "gui,desktop,production" ./...`：通过。
- 跨平台 CLI 编译（CGO_ENABLED=0）：linux/amd64、darwin/amd64 通过。
- 新增测试：`session/domain_test.go`（生命周期/线程/视图/V 指针/深拷贝）、
  `application/core/session_stress_test.go`（6 会话并行 + 切换风暴 + 队列并发）、
  `seelebridge/task_partition_test.go`、`client-state.test.mjs`（切换 resync）。
