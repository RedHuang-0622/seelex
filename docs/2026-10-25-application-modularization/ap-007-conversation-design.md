# AP-007 Conversation 聚合设计门禁

日期：2026-10-26

状态：**设计已冻结，第一个切片已实施**。本文只记录目标边界、当前事实、
契约决策和实施门禁；未完成项明确标注，不把规划写成既成事实。

## 1. 领域边界

| 概念 | 所属领域 | 负责 | 明确不负责 |
|---|---|---|---|
| `Conversation` | Application 对话应用域 | 单个 Session 的回合运行态、输入队列、可见对话与历史窗口的协作边界 | Session 身份、workspace binding、归档、团队计划、工具执行 |
| `ChatTurn` | Conversation 内的一次活动回合 | 一个 requestID 从受理、执行中、持久化到下一队列批次提升的过程状态 | Engine ReAct 循环本身、Task/Plan 领域状态机 |
| `Queue` | Conversation 内的输入集合 | 已受理但未开始执行的输入、稳定顺序、下标投影与撤回 | 模型历史、回合完成判定、durable queue 发布协议 |
| `HistoryWindow` | Conversation 的可见投影 | 已加载消息窗口、offset/total/has_more、分页安装与热尾拼接 | 权威会话正文、provider context、回合是否运行 |
| `Session` | Session 生命周期域 | SessionID、draft/binding/归档/unload、引擎与 context bundle 的资源生命周期 | 单个回合的聊天状态、输入队列内容、历史分页 |

`ChatState` 是 `ChatTurn` 及其队列计数的**前端可见运行投影**，不是执行内核或
durable record 的所有者。`Snapshot` 只是活跃 Session 的镜像，不是第二个 owner。

本阶段不迁入 Conversation 的内容：

- Workspace binding、Session catalog、archive/delete；
- Goal、Team、Subagent、WorkItem；
- Engine ReAct 循环、provider context 压缩和工具路由。

## 2. 状态所有权、锁与恢复

| 可变状态 | 唯一所有者 | 保护 | 生命周期 | 恢复来源 |
|---|---|---|---|---|
| `SessionUnit.Chat` | `middle/switch/sessionusecase.SessionUnit` | `SessionUnit.mu` | 进程内回合状态 | 不持久化；恢复后为 idle |
| 输入队列 `SessionUnit.Queue` | `SessionUnit` / `InputQueue` | `InputQueue.mu`；应用编排另持 `Core.ViewMu` | 运行期输入；durable queue 镜像另行落盘 | durable queue 回填 + record 发布状态 |
| `Cancel` / `Stream` / `Batcher` | `SessionUnit` | `SessionUnit.mu` | 仅当前进程和当前 request | 不恢复；重跑时重建 |
| 可见 `View` / `HistoryWindow` | `SessionUnit.View` | `View.mu`；镜像时持 `Core.ViewMu` | 可冷回读、可 LRU 卸载 | `SessionRecord` + transcript |
| 活跃 `Snapshot` | `kernel/state.Core` | `Core.ViewMu` | 活跃视图镜像 | 由 SessionUnit/View/任务状态重建 |
| `restoring` / `compacting` | `serviceState` | `Core.ViewMu` | 进程内过渡状态 | 不持久化；重启后重新判定 |
| `TaskExecutionState` | `task_context.Coordinator` | 任务协调器自有锁 | 单请求 | record/task checkpoint + transcript |
| draft slot | `serviceState.draft` | `Core.ViewMu` | 跨切换保留，物化后消费 | draft record/composer |
| durable `SessionRecord` | `session_runtime.Coordinator` / store | 存储原子提交 | 跨进程 | 会话三读 |

锁顺序保持现状：

```text
Core.ViewMu -> session Domain.mu -> SessionUnit.mu -> InputQueue.mu
                                  -> View.mu
```

不得反向取锁。事件发布、durable queue I/O、持久化和引擎调用都在释放
`Core.ViewMu` 后进行。

## 3. 聚合不变量

1. 一个 Session 最多有一个活动 `ChatTurn`；`ChatState.Running=true` 时
   `RequestID` 必须非空且属于该 Session。
2. 活动回合可与排队输入同时存在；`Running`、`QueuedCount`、`InputQueue`
   必须从同一 `SessionUnit` 读取。
3. 队列展示下标就是队列编辑下标。投影逐项、按顺序生成，不按 payload 类型过滤。
4. 当前回合持久化成功后才提升下一批排队输入；下一批 user 行必须先于新回合
   goroutine 发布。
5. `HistoryWindow` 只决定可见正文；回看历史不得改变活动回合、队列或持久化正文。
6. Session 只有在非 running、非 queued、无待批、非 restoring、非 compacting
   且满足驻留保护规则时才可 unload/release。
7. 中断或恢复不伪造 assistant 结果；恢复说明使用 system 可见行，durable
   事实仍以 record/transcript 为准。
8. 活跃进程 revision 只由 `Service.bumpLocked` 在 `Core.ViewMu` 内推进；
   后台 Session 使用 `View.Revision` 的会话本地空间。事件在锁外发布。
   AP-007 不统一这两套 revision，避免改变现有 wire 语义。
9. Message/transcript 的 `seq` 仍由 task/transcript 所有者分配；Conversation
   只消费，不生成第二套序号。

## 4. 契约与结构

### 4.1 第一个切片实际新增

不新增 `contract/dto`，不改 JSON、事件或持久化格式。

在 `application/core` 声明消费方私有端口：

```go
type conversationQueueUnit interface {
    QueueProjection() ([]string, int)
    RecallRequest(index int) (session.QueuedRequest, bool)
    ReorderRequests(from, to int) bool
    ChatState() model.ChatState
    UpdateChat(func(*model.ChatState), *session.View)
}

var _ conversationQueueUnit = (*session.SessionUnit)(nil)
```

真实实现方：`*session.SessionUnit` 结构满足，不写适配器。
真实调用方：新的 `conversationQueue` 组件，仅由
`Service.ReorderQueuedInput` / `RecallQueuedInput` 使用。

新增私有 struct：

```go
type conversationQueue struct {
    deps conversationQueueDeps
}
```

依赖只包含：`ViewMu`、解析队列单元、关闭/排空 guard、活跃会话判定、
ChatState 镜像、revision bump、snapshot/chat.changed 发布回调。组件不持完整
`*Service`，不拥有队列内容。

### 4.2 后续切片候选（尚未创建）

| 契约/结构 | 真实未来调用方 | 状态 |
|---|---|---|
| `ConversationDescriptor` DTO | GUI 会话详情、Session 切换流程 | 规划，尚未创建 |
| `ChatTurnCommandPort` | `ChatTurnProcess` | 规划，尚未创建 |
| `ConversationQueryPort` | Snapshot/历史读面 | 规划，尚未创建 |
| `SessionReleasePort` | Session 生命周期流程 | 规划，尚未创建 |

这些接口在对应流程落地前不写代码，避免“未来接口”。

## 5. 方案对比

| 维度 | A：立即新建 `application/conversation` 聚合包 | B：先在 `application/core` 收拢 Queue 应用组件 |
|---|---|---|
| 耦合度 | 低，但需同时处理新包、依赖闭包和跨包状态访问 | 中低；复用现有 SessionUnit 所有权 |
| 内聚性 | 高，目标形态最清晰 | 中高；先集中 Queue 编排，后续可平移 |
| 可测试性 | 高，但需新增大量 port/adapter | 高；现有队列回归可直接 parity |
| 实现成本 | 高，涉及高冲突文件、装配和 facade allowlist | 低，单个能力面 |
| 改动面 | 大，可能迫使 Runtime/SessionUnit 同改 | 小，仅 core 内部 |
| 可回滚性 | 较差，多文件同时迁移 | 好，删除组件并恢复方法体即可 |

**推荐 B**。现有仓库的 facade 依赖闭包只允许收缩，立即新增 Application 子包会
触发 I1 门禁；且 Conversation 主循环尚未准备好。先把 Queue 的锁内编排与发布
顺序收拢到一个不持完整 Service 的组件，既形成真实结构边界，又可独立回退。

**最大风险**：组件仍通过回调访问现有快照/发布面。门禁必须证明它不持
`*Service`、不复制队列状态、不改变锁顺序和事件顺序。

## 6. 下游影响矩阵

| 下游 | 第一个切片影响 | 兼容承诺 |
|---|---|---|
| GUI/TUI | 无调用变化 | `QueueAPI` / `QueueFace` 方法签名不变 |
| Runtime | 无 | 不调用 Runtime |
| SessionStore | 无 | 不在队列编辑中写盘 |
| EventHub | 由组件调用原发布回调，顺序不变 | kind、payload、revision、sessionID 不变 |
| Snapshot reducer | 无字段或语义变化 | `chat.changed` + `snapshot.changed` 顺序不变 |
| 恢复路径 | 无 | durable queue 回填/确认逻辑不动 |
| Session release | 无 | 不修改保护集合 |
| 测试 | 新增结构门禁；现有队列行为用例作为 parity | 错误值、下标空间、事件顺序保持 |

## 7. 兼容层台账

| 旧接口 | 新接口/实现 | 首调用方 | 删除里程碑 | 归零验证 | 回退方式 |
|---|---|---|---|---|---|
| `Service.ReorderQueuedInput` | `conversationQueue.ReorderQueuedInput` | GUI/TUI `QueueAPI` | M8 | 方法只一行转发，生产调用方迁到 `QueueAPI` | 恢复 `service_queue.go` 方法体 |
| `Service.RecallQueuedInput` | `conversationQueue.RecallQueuedInput` | GUI/TUI `QueueAPI` | M8 | 方法只一行转发，生产调用方迁到 `QueueAPI` | 恢复 `service_queue.go` 方法体 |
| `Service.queueEditGuardLocked` / `queuedInputUnitLocked` / `applyQueueEditLocked` | `conversationQueue` 私有方法 | 仅本包两个公开队列方法 | AP-007 后续切片 | `rg` 旧 helper 零命中 | 恢复原私有 helper |
| `SessionUnit.QueueProjection` | 保持唯一实现 | `conversationQueue` | 不删除 | 不允许出现第二套投影 | 无 |

## 8. AP-003 验收口径

AP-003 分两个独立指标，不把“入站 API 纯净”和“core 内部零依赖”混为一谈。

### 8.1 入站 API 契约纯度（AP-003 退出条件）

- `application/core/service_api_faces.go` 的公开接口签名不得引用
  `backend/**`、`store/**`、`middle/switch/**`、`middle/view/**` 具体实现包；
- `assemble.ScheduledTaskSpec/Status`、`goaldomain.*`、`store.Teamwork*`、
  `seelexctxsearch.*` 必须迁到 `contract/dto` 或纯协议包，旧类型只可保留别名；
- `contract/protocol`、`contract/dto` 和现有 `middle/protocol/model` 作为协议
  形状可作为迁移期允许项；删除 `middle/protocol/model` 另列后续 batch；
- GUI/TUI wire 字段与 JSON 标签逐字不变；
- 新增 AST 门禁扫描公开接口声明与 import，禁止具体实现包回流入站面。

实施状态：**已实现**。`application/api_contract_test.go` 同时扫描根门面和能力
接口文件；Goal、Command、Search、WebSearch 配置和审批类型已改用契约层身份。

### 8.2 `application/core` 内部依赖预算（独立指标）

- 当前基线 39，AP-003 期间不得上升；
- 每次 DTO 迁移必须报告依赖数量前后值；
- M2 阶段目标下降，但不设“直接为零”口号；最终 10 以下属于 M9 目标；
- `contract` 聚合包不得成为新的上帝包：只有真实跨层共享 DTO/port 才能进入。

## 9. 第一个实施切片

切片名称：**Queue application ownership**

实施顺序：

1. 先加红测：`serviceComponents` 必须装配 `conversationQueue`；
   `service_queue.go` 两个公开方法必须只含一行组件转发。
2. 新增消费方私有 `conversationQueueUnit` 端口与
   `conversationQueueDeps/conversationQueue`，编译期钉住 `SessionUnit` 结构满足。
3. 将 `queueEditGuardLocked`、`queuedInputUnitLocked`、
   `applyQueueEditLocked` 的单一实现移入组件。
4. `Service` 只保留一行方法转发；装配处注入锁、解析、镜像和发布回调。
5. 跑现有队列行为/错误/-race 用例，新增组件结构门禁。
6. 更新 `application/core/README.md`、`README-service.md` 或
   `README-chat.md` 与 implementation。

本切片不修改 `chat.go` 主循环、不迁移 `startChat/runChat`、不改
`SessionUnit` 字段、不碰 GUI/TUI。

实施结果：`conversationQueue`、`conversationQueueUnit` 与一行转发已落地；
结构门禁与既有 Queue 行为回归通过。下一批仍须先经过设计门禁。

## 10. 未决语义与门禁结论

- 现有 revision 同时存在进程级与会话本地两套空间；本阶段保持现状，
  未来若要统一必须先单独确认前端协议。
- `SessionUnit.status/Submit/Finish` 与 `ChatState.Running/QueuedCount` 当前并存：
  可见目录状态由 `ChatState`/审批数推导，`SessionUnit.status` 更接近资源准入骨架。
  本阶段不合并两套判定；后续若让 `SessionUnit.status` 成为聚合状态，必须先列出
  所有读写点和迁移 parity。
- `HistoryWindow` 是可变视图投影而非纯查询；这是现有产品语义，
  “Query”名称只表示不写 durable record。
- 新 `application/conversation` 包与 facade closure allowlist 冲突；
  第一切片留在 core，后续新建包前必须先调整架构门禁。
- AP-003 验收已拆分为 API 具体类型零泄漏与 core 依赖预算两条独立指标。

以上门禁满足后，进入第一个切片；不满足的项不得用测试绿灯替代。
