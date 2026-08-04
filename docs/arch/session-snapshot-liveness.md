# Session、Snapshot 与无死锁数据流

本文描述当前实现（2026-08-04）中，主会话、Application `Snapshot`、`Runtime` 与子代理之间的数据归属和并发边界。目标是让前端能及时获得一致的可见状态，同时避免 Session 锁、`service.mu` 和外部组件之间形成等待环。

## 1. 事实源与职责

| 层 | 拥有的数据 | 约束 |
|---|---|---|
| Engine Session | Provider 执行历史、框架的主会话执行锁 | 不作为前端可见状态的事实源；调用期间可能长期持有框架锁。 |
| Application `Service.snapshot` | 前端可见会话、对话尾部、运行状态、目录缓存和 revision | 只在 `service.mu` 下读写自身内存；锁内不得调用 Engine、Runtime、Workspace、Storage、Skills 或 Plugins。 |
| Runtime | 工具可见性投影、父证据快照、子代理回流邮箱 | 不反向调用 Application，也不直接读写主 Session。 |
| SessionPort / WorkspacePort | 持久化会话和工作区目录 | 只在锁外访问；目录结果异步复制到 `Snapshot` 缓存。 |
| 前端 | `Snapshot` 的当前视图及 Event 的增量 reducer | 不拥有后端业务事实；可在丢失增量事件后用 `Snapshot` 重新同步。 |

这里的“Session”有两层含义：Engine Session 是模型执行所需的工作历史；Application `Snapshot` 是 GUI/API 所需的可见状态。二者会在聊天、工具事件和持久化边界同步，但不能互相替代。

## 2. 主聊天：Session 到 Snapshot，再到前端

```text
Frontend Submit
  -> Application.Submit / startChat
  -> [service.mu] 写入用户消息、assistant 占位、Chat.Running、revision
  -> 解锁
  -> 发布 Runtime 值投影
  -> 消费已有的子代理回流（锁外写 Engine history）
  -> Engine.ChatStream
       -> chunk / tool hook 更新 Application Snapshot，并发布 Event
  -> ChatStream 返回（框架主 Session 锁已释放）
  -> 再次消费 Runtime mailbox
  -> 锁外持久化当前 Session
  -> [service.mu] 写入结束状态、运行时投影与 revision
  -> Event / Snapshot 发送给前端
```

`startChat` 在启动 `Engine.ChatStream` 前先写入用户消息和空的 assistant 消息，因此前端无需等待 Provider 首个 chunk 就能看到已接受的请求。流式输出、工具开始/结束等事件继续增量更新 `Service.snapshot` 并携带 revision。

前端有两种互补的读法：

- Event 用于低延迟增量呈现，例如新增消息、工具状态和 `snapshot.changed`。
- `Snapshot()` 用于读取完整的当前视图以及在事件遗漏、重连或顺序不确定时重新对齐。

`Snapshot()` 的实现仅在 `service.mu.RLock()` 内执行 `cloneSnapshot(service.snapshot)`，随后立刻释放锁。它不会读取 Engine history、调用 Runtime，也不会触发 Session/Workspace/Storage I/O。复制本身的成本随当前 Snapshot 大小增长，但热路径没有外部等待。

## 3. Session 目录不再阻塞 Snapshot

会话目录、工作区绑定和标题恢复来自 `SessionPort`、`WorkspacePort`，属于潜在阻塞的外部读取。启动时 `startSessionCatalogRefresh()` 创建独立 worker；`requestSessionCatalogRefresh()` 通过容量为 1 的 wake channel 合并重复刷新请求。

```text
catalog refresh request
  -> session catalog worker
  -> [锁外] SessionPort.List / WorkspacePort.AllBindings
  -> 得到本地不可变结果
  -> [service.mu] 复制 sessions、bindings、缺失标题到 Snapshot
  -> 解锁 -> 发布 snapshot.changed
```

关闭时先将 Application 标记为 closed，再停止该 worker。历史 `SessionPort` 目录接口尚未提供 `context`，因此关闭最多等待 100 ms；超时后 worker 可在外部调用返回时自行退出，并因 `closed` 不再发布新状态。这是一个明确的可用性优先取舍：关闭不会因为不受控的目录 I/O 卡住 GUI。

## 4. Application 到 Runtime：单向不可变投影

Runtime 的工具钩子和子代理路径过去需要查询 Application 状态，形成了 Runtime -> Application 的同步反向依赖。现在 Application 每次状态迁移后只发送最小值对象：

```text
Application 状态迁移
  -> [service.mu.RLock] 复制 goal-skill、session ID、最近用户目标、消息数
  -> 解锁
  -> Runtime.SetRuntimeVisibilityProjection(value copy)
  -> Runtime.SetParentEvidenceProjection(value copy)
  -> Runtime 的 atomic 缓存
```

`RuntimeVisibilityProjection` 仅包含工具可见性所需的 `GoalSkillActive`。`ParentEvidenceProjection` 包含 session ID、最近的普通用户目标和消息数量；Runtime 在本地结合自身 tracer 生成父证据 `ContextSnapshot`，并以原子指针保存。子代理读取的也是这个 Runtime 本地副本，不会碰 Application 或主 Session。

这个边界的关键是“复制后发布”：Runtime 收到的是不可变值，而不是可调用回 Application 的闭包、接口或锁保护对象。

## 5. 子代理回流：有界邮箱而不是同步写主会话

子代理结束时会导出自身 Session 的结构化上下文，并与 Runtime 保存的父证据合并。合并结果不能直接写入主 Session：`plan_run` 是主 Session 内的工具调用，`Engine.ChatStream` 执行时框架会持有主 Session 锁；子代理若在此时调用主会话的 `History` 或 `AppendHistory`，主会话会等待子代理，而子代理又等待主会话锁。

当前回流路径如下：

```text
subagent completes
  -> 读取 Runtime 缓存的 parent evidence
  -> merger.MergeBack
  -> Runtime.enqueueSubagentContext
  -> 有界、非阻塞 subagentMailbox
  -> 主聊天开始前或 ChatStream 返回后
  -> Application.DrainSubagentContexts（不持 service.mu）
  -> Engine.AppendHistory（锁外）
  -> [service.mu] 写入可见 Conversation 和 revision
  -> EventMessageAdded -> 前端
```

邮箱满时会丢弃最新回流并累加诊断计数，而不是阻塞子代理或无限占用内存。主会话在下一次 `ChatStream` 开始前、以及当前 `ChatStream` 返回后分别 drain；因此回流既不会争抢正在执行的框架锁，也能在下一轮模型请求前进入 Engine history 和前端 Snapshot。

子会话详情读取也遵守相同原则：先在 `nodeSessionsMu` 下取出 Session 指针并释放注册表锁，再调用子会话的 `History()`；绝不在注册表锁内调用另一个 actor。

## 6. 被消除的等待环

旧的工具可见性路径可能形成以下自锁：

```text
service.mu
  -> Runtime.VisibleTools()
    -> GoalSkillProvider callback
      -> Application.GoalSkillActive()
        -> service.mu.RLock()
```

新的路径只有锁内复制和锁外发布：

```text
service.mu -> copy projection -> unlock -> Runtime atomic store
Runtime tool hook -> atomic projection read
```

子代理的旧路径则可能形成：

```text
main Session.ChatStream 持主 Session 锁
  -> plan_run 等待 child
    -> child 直接访问主 Session History / AppendHistory
      -> 等待主 Session 锁
```

新的路径改为非阻塞消息交接：

```text
child -> Runtime mailbox (nonblocking) -> returns
main chat boundary -> drain mailbox -> Engine.AppendHistory
```

两种修复共同遵守一条规则：跨 actor 不共享可变状态；同步锁临界区只修改本 actor 的内存，跨边界数据以值或有界消息传递。

## 7. 维护不变量

1. `service.mu` 只保护 Application 自身内存，不包裹任何外部 Port 调用。
2. `Snapshot()` 必须是内存读取和复制，不能在该路径新增 I/O 或反向回调。
3. Runtime 只能读取自己的投影缓存；新增 Runtime 功能不得通过 callback 查询 Application。
4. 子代理不得直接访问主 Session；所有回流经过 Runtime 的有界 mailbox，并由主会话边界锁外消费。
5. Event 是增量通知，`Snapshot` 是重同步依据；消费者不得假定 Event 只由用户业务操作产生，异步 catalog 刷新也会发布 `snapshot.changed`。
6. 关闭顺序是先 cancel/closed、再停止接收新工作、再有界 drain；每个可控的外部阻塞调用应接受 context 和超时。

## 8. 关联实现

- `application/core/runtime_projection.go`：Application 复制并发布 Runtime 投影。
- `application/core/service_snapshot.go`：纯内存 Snapshot 读取及运行时状态的两阶段收集/应用。
- `application/core/session_scope.go`：异步 session catalog 缓存与有界关闭等待。
- `application/core/service_input.go`：回流邮箱 drain、Engine history 注入和可见消息更新。
- `application/core/chat.go`：主聊天的开始前与结束后 drain 边界。
- `seelebridge/actor.go`：Runtime 原子投影和非阻塞有界邮箱。
- `seelebridge/agent_node.go`：子代理父证据读取、merge-back 和子会话锁边界。
