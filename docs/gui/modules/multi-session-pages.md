# 多会话页面并行工作模块详细设计

> 状态：拟议方案
> 产品需求：`CAP-MULTISESSION`
> 总体架构：[`../../arch/agent-workbench-architecture.md`](../../arch/agent-workbench-architecture.md)

## 1. 目标与语义

本模块让一个 Seelex 桌面窗口同时打开多个会话页面，并允许多个会话在后台独立运行。P0 使用中心区域页签，不等同于启动多个 WebView 或多个操作系统窗口。

必须区分四种状态：

| 概念 | 语义 |
|------|------|
| persisted session | 已写入 Session store，可关闭且未加载 |
| open session | 已加载为内存 runtime，并在页签栏中有页面 |
| active session | 当前前台可见、接收默认键盘输入的唯一页面 |
| running session | 正在执行 turn、等待工具或等待审批；可以有多个 |

不变量：`active ⊆ open ⊆ persisted-or-new`，但 `running` 不等于 `active`。切走页面不得取消任务、替换 Engine history 或停止事件消费。

## 2. 当前基线与迁移缺口

当前 `application.Service` 持有一个 Engine、一个 `Snapshot.Session`、一个 `ChatState` 和一个 PromptStack：

- `ErrChatRunning` 阻止全局第二个 Chat；
- `/resume` 通过 `Engine.ReplaceHistory` 覆盖当前 Engine；
- GUI 左栏 session button 调用 `/resume <id>`，属于破坏式切换；
- Snapshot 和 Event 没有 session routing key；
- Effort、Plugin、Skill、Plan、Interaction 和 input queue 都隐含为全局当前会话状态。

所以现有列表可以“换会话”，不能“多开并行”。迁移不能只增加页签 DOM，必须先隔离每个会话的 Engine 和业务状态。

## 3. 方案对比

| 维度 | A：单 Service 切换 History | B：进程内 SessionActor + Coordinator | C：每会话子进程 |
|------|--------------------------|--------------------------------------|-----------------|
| 真并行 | 否，仍共享 Engine/Chat | 是，每会话独立 runtime | 是 |
| 状态隔离 | 低，切换容易串 history | 高，actor 单写者 | 最高，进程隔离 |
| 当前改动 | 小但不满足需求 | 中高，可渐进迁移 | 很高，需 IPC/监督/打包 |
| 资源成本 | 低 | 可用 scheduler 有界控制 | 高，每会话进程和连接 |
| 事件路由 | 仍是全局 | session topic + workbench topic | 需跨进程复用协议 |
| 可测试性 | 低，竞态隐式 | 高，fake factory/clock/scheduler | 中，测试启动慢 |
| 崩溃隔离 | 低 | 中，panic 必须 recover/标错 | 高 |
| 回滚 | 表面容易，数据风险高 | 高，可限制并发为 1 | 中 |

推荐 **B：进程内 SessionActor + WorkbenchCoordinator**。最大风险是 Engine、审批和 runtime 配置以前默认为全局；必须通过 factory 为每个 session 显式构造，禁止把旧 Engine 指针共享给多个 actor。

子进程隔离保留为未来高风险插件或远程执行策略，不作为 P0 页签能力的前提。

## 4. 模块结构

```text
gui session tabs / session-store.js
              │ explicit session_id
              ▼
application.WorkbenchService
  ├─ WorkbenchCoordinator
  │    ├─ open registry
  │    ├─ active session id
  │    ├─ SessionScheduler
  │    └─ workbench EventHub
  └─ SessionRuntimeFactory (caller-owned port)
         ├─ SessionActor A ─ Engine A / PromptStack A / EventHub A
         ├─ SessionActor B ─ Engine B / PromptStack B / EventHub B
         └─ SessionActor C ─ Engine C / PromptStack C / EventHub C
                    │
          SessionRepository / ApprovalBroker / WorkspacePort
```

`WorkbenchCoordinator` 管页面生命周期和资源配额，不解释 Conversation、Tool 或 Card。`SessionActor` 复用单会话 Service 的状态机语义，是 session 内事实源。

## 5. 核心数据模型

```go
type SessionLifecycle string

const (
    SessionLoading         SessionLifecycle = "loading"
    SessionIdle            SessionLifecycle = "idle"
    SessionQueued          SessionLifecycle = "queued"
    SessionRunning         SessionLifecycle = "running"
    SessionAwaitingApproval SessionLifecycle = "awaiting_approval"
    SessionError           SessionLifecycle = "error"
    SessionClosing         SessionLifecycle = "closing"
)

type SessionPageSummary struct {
    SessionID       string           `json:"session_id"`
    Title           string           `json:"title"`
    Lifecycle       SessionLifecycle `json:"lifecycle"`
    WorkspaceID     string           `json:"workspace_id,omitempty"`
    Active          bool             `json:"active"`
    UnreadCount     int              `json:"unread_count"`
    PendingApproval int              `json:"pending_approval"`
    LastActivityAt  time.Time        `json:"last_activity_at"`
    Revision        uint64           `json:"revision"`
}

type WorkbenchSnapshot struct {
    ProtocolVersion int                  `json:"protocol_version"`
    Revision        uint64               `json:"revision"`
    ActiveSessionID string               `json:"active_session_id"`
    OpenSessions    []SessionPageSummary `json:"open_sessions"`
    Limits          SessionLimits        `json:"limits"`
}
```

Workbench Snapshot 只含页签摘要，不复制各会话 Conversation。每个页面通过 `SessionSnapshot(sessionID, window)` 独立获取内容。

## 6. 调用方接口与构造注入

接口定义在 `application` 调用方：

```go
type SessionRuntime interface {
    ID() string
    Snapshot(context.Context, TranscriptWindow) (SessionSnapshot, error)
    Submit(context.Context, string) error
    Cancel(context.Context, string) error
    Resolve(context.Context, InteractionResolution) error
    Subscribe(context.Context) (<-chan SessionEvent, func(), error)
    Close(context.Context) error
}

type SessionRuntimeFactory interface {
    New(context.Context, NewSessionRequest) (SessionRuntime, error)
    Open(context.Context, string) (SessionRuntime, error)
}

type SessionRepository interface {
    List(context.Context, SessionListQuery) (SessionPage, error)
    SaveCheckpoint(context.Context, SessionCheckpoint) error
    MarkOpen(context.Context, string, bool) error
}
```

`session` 包实现 repository，composition root 注入 EngineFactory、Workspace binding、clock、ID generator 和 scheduler。不得使用包级 `map[sessionID]*Engine` 单例。

计划 Application API：

| 方法 | 语义 |
|------|------|
| `WorkbenchSnapshot()` | 获取页签摘要和全局 limits |
| `OpenSession(id)` | 幂等加载并返回 page summary |
| `NewSession(options)` | 创建、加载并打开页面 |
| `ActivateSession(id)` | 只改变前台选择，不影响运行 |
| `CloseSession(id, mode)` | idle 关闭；running 必须显式 cancel |
| `SessionSnapshot(id, window)` | 获取该会话权威状态 |
| `SubmitToSession(id, text)` | 向明确会话提交 |
| `CancelSession(id, requestID)` | 只取消目标会话请求 |
| `ResolveSessionInteraction(id, resolution)` | 只解决目标会话审批 |

旧 `Submit/Cancel/Resolve/Snapshot` 在迁移期委托 active session；新前端一律使用显式 session ID。

## 7. SessionActor 状态机

每个 actor 使用 command channel 或等价串行 mailbox 保证单写者：

```text
Loading ──loaded──► Idle
Idle ──submit──► Queued ──permit──► Running
Running ──tool approval──► AwaitingApproval ──resolve──► Running
Running ──complete/error/cancel──► Idle/Error
Idle/Error ──close──► Closing ──checkpoint──► Unloaded
```

规则：

1. 同一 session 内仍保持一次一个 active turn；连续输入进入该 session 自己的 queue。
2. 不同 session 可以并行 Running，request ID 只需在 session 内唯一，但 Event 总是同时携带 session ID。
3. actor 不持有全局 coordinator 锁调用 Engine、Session store、Workspace 或 EventHub。
4. Engine callback 先转换为 actor command，再修改 session state，避免回调并发写 Snapshot。
5. actor panic/Engine fatal 只把目标 session 标为 error，Coordinator 和其他 session 继续工作。

## 8. 调度、背压与资源配额

```go
type SessionLimits struct {
    MaxOpenSessions       int `json:"max_open_sessions"`
    MaxConcurrentRunning  int `json:"max_concurrent_running"`
    MaxQueuedTurnsTotal   int `json:"max_queued_turns_total"`
    MaxQueuedTurnsSession int `json:"max_queued_turns_session"`
}
```

建议默认值：open 8、running 3、global queued 20、per-session queued 5；管理员可降低或通过构造 options 覆盖，Agent 不能修改。

Scheduler 使用 FIFO + round-robin session fairness：同一会话不能靠连续排队长期占满全部 permit。审批等待默认释放纯模型运行 permit，但仍保留 interaction 与 tool lease；具体 provider 并发上限通过 Strategy 注入。

资源不足时提交进入 `queued`，UI 显示队列位置。超过硬上限返回 typed error，不静默丢弃或无限创建 goroutine。

## 9. Protocol 与事件路由

事件拆成两个 scope：

| scope | seq/revision | 内容 |
|-------|--------------|------|
| `workbench` | 全局独立序列 | page opened/closed/activated、summary changed、limits changed |
| `session:<id>` | 每会话独立序列 | conversation、runtime、tool、card、interaction、workspace binding |

```go
type EventEnvelope struct {
    ProtocolVersion int             `json:"protocol_version"`
    Scope           string          `json:"scope"`
    SessionID       string          `json:"session_id,omitempty"`
    Seq             uint64          `json:"seq"`
    Revision        uint64          `json:"revision"`
    Kind            string          `json:"kind"`
    Payload         json.RawMessage `json:"payload"`
}
```

采用 per-scope seq，而不是一个全局高频 seq：后台 A 丢事件只重拉 A 的 SessionSnapshot，不刷新 B、页签 shell 或 Workspace 查询。workbench gap 只重拉 WorkbenchSnapshot。

Bridge 订阅可以复用一个 Wails transport，但必须保留 envelope scope。GUI 不能按“当前 active session”猜事件归属。

## 10. 前端页面模型

```text
center-column
  ├─ session-tablist
  │    └─ session-tab × N (title, status, unread, approval, close)
  └─ active-session-page
       ├─ conversation viewport
       ├─ history anchor
       └─ composer
```

计划 `session-store.js` 保存：

```js
Map<sessionID, {
  snapshot,
  seq,
  revisionFloor,
  loading,
  unread,
  viewState: { scrollAnchor, draft, focusedItem, expandedCards }
}>
```

只把 active page 挂到主 Conversation DOM，后台 session 继续 reducer 归并但不渲染完整消息树。切回时使用缓存 snapshot + viewState 立即恢复，再异步校验权威 revision。这样 open 8 个页面不会常驻 8 份大 DOM。

页签操作：

- 新建会话默认打开并激活；
- 点击已持久化但未打开的会话先 `OpenSession`，成功后加入 tab；
- 切换只调用 `ActivateSession`，不调用 `/resume`；
- 中键/关闭按钮关闭 idle page；running/approval page 返回确认交互，不能单击静默取消；
- `Ctrl+Tab` / `Ctrl+Shift+Tab` 切换，`Ctrl+W` 遵守关闭保护；
- 小屏页签可横向滚动或进入 session switcher，不压缩 Conversation 到不可用。

## 11. 后台活动与通知

后台 session 的 summary badge 区分：running、queued、approval、error、done/unread。只在状态边沿增加 unread，不按每个 token delta 计数。

- 完成、错误和等待审批可触发应用内 toast；
- 系统通知必须由用户显式开启，内容默认只含会话标题和状态，不含 prompt、工具参数或文件内容；
- 点击通知通过 session ID 激活页面并聚焦对应 interaction/item；
- active page 可见时不重复系统通知；
- 页签标题来自用户命名或本地生成摘要，Agent 不得注入 HTML。

## 12. 会话级 Runtime、Effort 与 Skill

以下状态属于 SessionActor，不再是 Workbench 全局可变状态：

- Engine history 与 request context；
- input queue、Conversation、Plan、Tool lifecycle；
- PromptStack、活动 Skill、Effort/MaxLoops；
- pending Interaction 与 approval；
- Card presentation 与 transcript window；
- Workspace binding 和 artifact provenance。

全局 runtime 配置只作为新会话默认模板。顶部 Effort 控件展示并修改 active session；切页后由该 session 的 runtime snapshot 覆盖 committed 状态。后台会话运行期间修改另一页 Effort 不影响它。

## 13. 审批与动作归属

Interaction ID 采用 `session_id + interaction_id` 复合身份，所有 resolve 请求必须同时携带两者及 expected revision。

- 后台会话弹出审批时页签显示高优先级 badge，不抢占当前输入焦点；
- 用户点击 badge 后激活目标页并打开该 session 的审批 UI；
- 审批文案必须展示来源 session、workspace、tool 和目标资源；
- resolve 后只唤醒对应 actor；重复、过期或跨 session resolution 返回 typed conflict；
- 全局审批中心可作为 P1，但必须复用相同 Core API，不能在前端重写权限逻辑。

## 14. Workspace 并发与绑定

每个 SessionRuntime 保存稳定 `workspace_id`。P0 可以让多个 session 绑定同一个本地 Workspace，但右栏始终显示 active session 的 binding 和选中状态。

并发读取可共享 Workspace index/cache；写、删和命令继续经过 Permission Gate。多个 session 可能修改同一资源时：

1. approval preview 记录 resource revision/hash；
2. 执行前重新 PathGuard 并比较 precondition；
3. 不一致返回 `WORKSPACE_REVISION_CONFLICT`，不得 last-write-wins；
4. 成功后发布 Workspace revision，所有绑定该 Workspace 的 session summary/cache 失效相关 scope；
5. artifact provenance 始终记录 producer session/turn/tool/approval。

## 15. 持久化、恢复与关闭

每个 session bundle 独立 generation/checkpoint。Workbench 另存轻量 layout：

```json
{
  "open_session_ids": ["s-a", "s-b"],
  "active_session_id": "s-b",
  "tab_order": ["s-a", "s-b"],
  "saved_at": "2026-07-24T10:00:00Z"
}
```

启动时先恢复 shell 和 page summaries，再按 active-first、有界并发懒加载 runtime。某个 bundle 损坏只让该页进入 error/read-only，不阻塞其他页。

关闭规则：

| 状态 | close 行为 |
|------|------------|
| idle/error | checkpoint 后卸载，保留持久化会话 |
| queued | 要求确认取消 queue，再关闭 |
| running | 返回 `SESSION_CLOSE_REQUIRES_CANCEL`，显式 cancel-and-close |
| awaiting approval | 要求 reject/cancel 或继续保持页面 |

应用退出时进入 draining：停止接收新 turn，给 actor 有界 checkpoint 时间；未完成任务按 policy 取消并记录 interrupted，下一次恢复不能伪装成 running。

## 16. 错误模型

| code | 场景 | UI |
|------|------|----|
| `SESSION_NOT_OPEN` | action 指向未加载页 | 提供重新打开 |
| `SESSION_LIMIT_EXCEEDED` | open 超限 | 提示关闭 idle 页 |
| `SESSION_RUN_QUEUE_FULL` | 调度队列超限 | 保留输入草稿，不提交 |
| `SESSION_RUNTIME_FAILED` | Engine/runtime 构造失败 | 单页 error + retry |
| `SESSION_CLOSE_REQUIRES_CANCEL` | running/approval 直接关闭 | 确认 cancel-and-close |
| `SESSION_INTERACTION_STALE` | 跨页或过期审批 | 刷新目标 session |
| `SESSION_EVENT_GAP` | 单 scope seq 缺口 | 只 resync 该 session |
| `SESSION_CHECKPOINT_FAILED` | 持久化失败 | 保持页面打开，禁止假成功关闭 |
| `SESSION_WORKSPACE_CONFLICT` | 共享资源 revision 变化 | 展示 diff/重新审批 |

## 17. 性能、可靠性与可观测性

目标：

- 8 个 open、3 个 running 的 fixture 下，页签切换可见反馈 P95 <100ms；
- background token delta 不触发 active Conversation 重绘；
- WorkbenchSnapshot 默认 <32 KiB，单 SessionSnapshot 保持独立窗口上限；
- 任一 actor/event subscriber 背压不阻塞其他 session；
- 并发 session 数、queue wait、turn duration、approval wait、actor crash、scope resync 可度量；
- 日志必须携带 hashed/session-safe correlation ID，不能记录完整 prompt 或 secret。

## 18. 计划改动位置

| 层 | 文件/目录 | 变更 |
|----|-----------|------|
| Contracts | `application/workbench.go`、`state.go`、`event.go` | Workbench/Session snapshots、scoped event、API |
| Runtime | `application/session_actor.go`、`session_scheduler.go` | actor mailbox、registry、fair permits |
| Ports | `application/ports.go` | SessionRuntimeFactory、Repository、Workspace binding |
| Composition | `main.go` | per-session Engine/runtime factory 注入 |
| Session | `session/` | 独立 bundle generation、layout、checkpoint |
| Approval | `application/approval*.go` | session-scoped interaction identity |
| Bridge | `gui/bridge.go` | explicit session page methods |
| Frontend state | `session-store.js`、`client-state.js` | per-scope reducer、缓存、viewState |
| Frontend UI | `session-tabs.js`、`index.html`、`styles.css` | tablist、badges、快捷键、响应式 |
| E2E | `e2e/fixtures/`、`gui/e2e/` | parallel session journeys |

## 19. 测试矩阵

| 层 | 必测项 |
|----|--------|
| Actor unit | 独立 history/queue/effort/skill、状态转换、panic isolation |
| Scheduler | max running、fairness、cancel queued、permit leak、provider limit |
| Application | open 幂等、activate 无副作用、close protection、explicit routing |
| Event | per-scope seq/gap/resync、后台 delta、workbench summary ordering |
| Persistence | 多 bundle checkpoint、active-first restore、partial corruption |
| Approval | background badge、跨 session reject、stale revision、focus routing |
| Workspace | shared reads、write precondition conflict、cross-session invalidation |
| Node | tab keyboard、draft/scroll restore、unread edge、active Effort |
| Playwright | A/B 并行、切页、后台完成、审批归属、关闭保护、重启恢复 |
| Race | actor registry、Snapshot、event publish、close/cancel、shutdown drain |
| Load | 8 open/3 running、长 delta、慢 subscriber、bounded memory/goroutine |

## 20. 实施与回滚

1. 先引入 explicit session ID API 和 per-scope Event，保持 scheduler concurrency=1；
2. 把现有单 Service 状态迁入 SessionActor，验证单页行为无回归；
3. 引入页签 store/UI，但仍只允许一个 running；
4. EngineFactory、Approval 和 Workspace precondition 完成后开启多个 running；
5. 最后增加恢复 layout、系统通知和负载门禁。

Feature flag `capabilities.multi_session_pages` 可关闭页签并把 max open/running 固定为 1。数据仍使用独立 bundle，因此降级不会合并或覆盖多个会话。

## 21. 验收追溯

| PRD | 设计落点 |
|-----|----------|
| MS-001 | persisted/open/active/running 四态语义 |
| MS-002 | SessionActor + 独立 Engine/runtime |
| MS-003 | WorkbenchCoordinator + page APIs |
| MS-004 | scoped Event 与局部 resync |
| MS-005 | tablist、后台 badge、viewState 恢复 |
| MS-006 | session-scoped approval routing |
| MS-007 | scheduler、fairness 与硬 limits |
| MS-008 | workspace binding、revision precondition 与 provenance |
| MS-009 | layout/checkpoint、关闭与重启恢复 |
| MS-010 | session 级 Effort/Skill/Plan/input queue 隔离 |
