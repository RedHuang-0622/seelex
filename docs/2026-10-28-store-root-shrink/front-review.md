# Store 根包继续收缩 · 前置审查

日期：2026-10-28
分支：`codex/store-root-shrink`
基线：`66f20702`
状态：**领域 owner 与 God 类型收缩完成，根 Session engine 保留真实持久化机制**。
Session 状态/投影/seq、结构身份、Project、Artifact/Board/Teamwork/WorkUnit、
Role draft/permission/registry/投影，以及 Role/Stack/Wire 显式适配器均已完成收口。

## 需求摘要

删除 Project、Board、Teamwork、Role、WorkUnit、Artifact 在 `store` 根包中的兼容层；
检查 **`store` 之外**的包是否重复实现 Session 核心/状态机；若没有完整重复实现，则划清
`store` 与这些外部包各自负责的 Session 业务；确认 Context/Stack/Wire 的真实归属。

## 关键事实

### 文档位置

本方案只维护一个文件：

```text
docs/2026-10-28-store-root-shrink/front-review.md
```

它位于专用 worktree：

```text
G:\seelex-worktrees\store-ddd-M8-to-M11
```

不是主工作区 `G:\Program\go\seelex` 下的文件；主工作区存在用户未提交改动，本方案不改那里。

### Session 查重范围

`store/session` 已拥有：

- transcript/EVENT 行 JSONL 编解码、残尾处理与 append；
- message/event 分片命名、seq 分配、head 类型与确定性 commit id；
- pending-tail 分类、queue/lifecycle 纯状态迁移、retention/fork/compact 校验；
- draft 物理尾 seq 与 shard 枚举。

根包仍有 19 个 `return storesession.*` 单层包装。根包真正独有的部分是：

- `storeEngine`/`jsonRepository` 的锁内 I/O 编排与 head 发布；
- lifecycle/queue/retention/compact/fork 跨数据文件和 head 的写序；
- `Router` project scope、repository lifecycle 和现有 workspace API；
- DurableHistory/EventStore/SessionGranularStore 对根会话通道的适配。

但“Session 查重”不是比较根 `store` 与 `store/session`。需要检查的是 `store` 之外的
Application、Assembly、Conversation、Backend、Boot、Internal/Host、Kernel、Middle、Wiring
是否另有 Session 核心、状态迁移或同一判据。

### store 外部 Session 实现查重

| 外部位置 | 当前职责 | 与 Session 存储的重叠 | 结论与迁移方向 |
|---|---|---|---|
| `application/session/state/ports.go`、`domain.go` | `SessionUnit` 持有会话身份、View、ChatState、审批、运行时 InputQueue、Composer、驻留标记；`Domain` 是注册表/active actor | `Unit` 内另有一套 `status/loaded` 字段及 `Submit/Finish/ColdLoad/Unload/Status/HasSession` 迁移方法 | **真实重复**。生产代码没有调用这套迁移方法；实际可见状态由 `viewstate.SessionStatusLocked` 推导。状态词表和可见状态判据收口到 `sessionstate`，删除未使用的命令式状态机，`viewstate` 只做 DTO 映射 |
| `application/session/viewstate/session_runtime.go` | 生产唯一可见 Session 状态推导：`restoring → awaiting_approval → running → queued → idle`，并生成目录活体行 | 与 `SessionUnit.status` 重复；queued 同时检查 `PendingRequests()` 和 `ChatState.QueuedCount` | 保留状态优先级语义，但把它收口为 `sessionstate` 的纯状态投影函数。队列权威只认 `PendingRequests`；`QueuedCount/InputQueue` 只是 ChatState 展示投影 |
| `application/session/state/ports.go` 的 `InputQueue` | 当前进程内、可编辑、可撤回/重排的运行期输入队列 | 与 `store/session.LifecycleState.Queue` 名字相同、都由 FIFO 和 seq 组成 | **不是同一聚合**。运行时队列是当前进程权威；durable queue 只做崩溃恢复镜像。不可合并；文档和测试必须持续区分 |
| `application/conversation/queue.go`、`turn_state.go` | 运行期队列编辑、ChatState `running/queued/error` 更新 | 不实现 durable queue 或 transcript state | 合法应用用例。保留；只允许通过 `SessionUnit` 修改运行态 |
| `application/session/cold/*` | 冷读、record/transcript 装配、Engine/queue 恢复、任务恢复、锁内提交和锁外发布 | 会读取 store 快照，但不判 pending-tail 连续性、不写 head、不分片 | 冷加载应用编排，不是第二份存储状态机。pending-tail 分类仍归 `store/session`，恢复决策归 `kernel/sessionpolicy` |
| `application/session/history/*` | hot/restoring/cold 分支、恢复锁、可见窗口安装和页面接管 | 读取持久快照，不实现 transcript/head 状态机 | 合法 Session 应用用例。保留；继续依赖消费方窄端口 |
| `application/session/fork*`、`application/session/fork/*` | fork 准入、过渡锁、子会话 ID、写序、workspace 绑定和 resume | 调用 Kernel fork 语义与 store 快照端口，不自行计算 fork 段落可达集合 | Application 编排层，不是第二份 fork 核心算法。保留 |
| `application/assembly/session_draft.go`、`session_draft_snapshot.go`、`session_pending_tail.go` | 草稿壳切换、运行期 transcript 后缀定时写盘、冷加载时调用 pending-tail 决策 | 会持有 `(left,right]` 双指针，但不分类残尾、不发布 head | 宿主适配/长任务 I/O 编排。草稿后缀写盘可继续使用 runtime seq，但最终提交与 head 发布仍归 Session 存储 |
| `application/assembly/session_cold_read.go`、`snapshotOfCold` | 用 record 事实拼只读 Snapshot | 再次把事件/record 投影成 UI 会话 | 投影职责与 Kernel/Store 有漂移风险，但不拥有持久状态。应消费唯一的 conversation projection，不自行重建规则 |
| `kernel/session/archive.go` | 构建 `SessionRecord`、合并磁盘 transcript 与内存 transcript、从事件重建可见对话、记录/v3 兼容、archive 状态写面 | 与 `store/json_layout.go`、`internal/host` 存在 event→conversation/SessionRecord 的多份投影；其中 `mergeTranscriptEventsBySeq` 是 Kernel 的持久化恢复协调语义 | **真实部分重复**。Kernel 应成为 `TranscriptEvent -> SessionRecord conversation` 的唯一应用投影 owner；store 只返回原始 rows/head/meta，不再派生同义 conversation |
| `kernel/session/fork.go` | fork 切断点、段落边界、事件截断、record/context/血缘裁剪和可达 tool-result 集合 | 使用根 `store.Event`，并另有 `conversationMessagesFromEvents` 简化投影 | fork 语义本身无第二份；简化 conversation 投影必须删除并改用唯一投影。根类型改为消费方窄端口，Host 做边界适配 |
| `kernel/session/queue_wire.go`、`pending_tail_wire.go`、`wire_history.go` | durable queue、pending-tail、wire、lifecycle 的消费端口及转发 | 没有队列状态机、残尾分类或 wire 算法 | 合法端口层；保留。实现仍在 store/Application |
| `kernel/session/transition_manager.go`、`transition_actor.go` | per-session/view 的过渡锁和 FIFO actor | 只串行化命令，不维护 Session 生命周期状态 | 并发原语，不是第二份状态机 |
| `kernel/sessionpolicy/{unload,draft,queue,pendingtail}.go` | 卸载、草稿身份、队列编辑、pending-tail 恢复的准入与顺序判据 | 决定何时调用 store 能力，但不持久化 head/queue | 唯一应用策略层。保留；防止宿主重新复制判据 |
| `middle/residency/archive.go` | archive 门控 → flush → unload → mark archived → 释放异步任务 → 刷新目录 | 只把 archived 作为 record 可见状态写入；不实现存储事务 | archive 应用顺序唯一 owner。保留；store 仅原子写状态 |
| `internal/host/session_workspace_ports.go` | `model/store/kernel` 类型适配、项目定位、Root Router 能力接线 | `deriveSessionRecord` 又从事件派生了简化 Conversation；多个 Root 转发可能形成新 facade | Host 只能做无业务判据的转换。删除 `deriveSessionRecord` 等第二份投影；所有具体 store 能力由 composition root 注入窄端口，Host 不构造万能 Store facade |
| `backend/engine/assemble/runtime_session*.go` | Engine bundle 注册、会话上下文绑定、内存释放 | 不实现 Session 状态迁移；但直接持有 `store.SessionContextStore`、`DurableHistory` 等根类型 | 业务上不是重复状态机；结构上是错误依赖。改为消费方端口 + 外部适配器 |
| `boot/`、`wiring/` | 对象构造与依赖注入 | 当前注入大量根 Store/Router 能力 | 只允许 composition root 选择实现并注入窄端口；不得承载 Session 状态判断或写序 |

### Session 职责唯一化结论

当前**没有**第二份完整的 durable Session 核心：根 `store`/`store/session` 仍是 transcript、
head、seq、分片、pending-tail 和 durable queue 的唯一持久化实现。

但当前确实存在三处必须消除的重复：

1. **可见状态机重复**：`SessionUnit.status/loaded` 的迁移命令与
   `viewstate.SessionStatusLocked` 的实时投影并存。生产只用后者。目标 owner 是
   `application/session/state`；`viewstate` 只把 `dto.SessionStatus` 放进 Snapshot。
2. **event→conversation 投影重复**：至少存在
   `store/json_layout.go:derivedConversationMessages`、
   `kernel/session:conversationFromTranscriptLocked`、
   `kernel/session:conversationMessagesFromEvents`、
   `internal/host:deriveSessionRecord` 等实现，字段保真、tool call 展开和内部行过滤
   并不一致。目标 owner 是 `kernel/session` 的唯一投影；store 提供原始持久事实。
3. **transcript seq 分配重复**：`kernel/task` 在 append 时分配运行时 seq，
   `store/session.AssignRowSeqs` 又在落盘时分配/校验 seq。运行时在落盘前就需要 seq
   生成消息身份和 CAS，因此目标 owner 应是 Kernel Session transcript；store 改为
   只校验显式 seq 严格递增、与 head 连续，不再静默补号。

### 修正后的 Store 边界

判断标准不是“代码是否读起来像状态机”，而是这项判据回答什么问题：

- **非 Store 状态**回答“用户当前看到会话是什么状态”“这一轮是否正在运行 / 排队 /
  等待审批”“该不该恢复、归档、卸载、fork”。这些不是磁盘事实，移出 Store。
- **Store 状态**回答“哪些字节、哪些 head、哪些分片在什么顺序下构成一次可恢复提交”。
  这属于写粒度和磁盘读取契约，必须留在 Store。

#### 必须移出 Store

| 当前内容 | 现位置 | 新 owner | 说明 |
|---|---|---|---|
| `idle/running/queued/awaiting_approval/restoring` 的可见状态判定 | `store` 目录状态派生 + `viewstate` | `application/session/state` | Store 最多返回 draft/archived 和运行期输入事实，不能默认生成 `idle` 或决定运行态 |
| `SessionUnit.status/loaded` 的命令式迁移 | `application/session/state`（当前未用于生产） | `application/session/state` 的纯状态投影 | 删除与 `viewstate.SessionStatusLocked` 并存的第二套迁移 API |
| 运行时 InputQueue 的编辑、召回、重排、draft 壳 | `application/session/state`、`application/conversation` | `application/session/state` | 这是进程内业务状态 |
| 是否恢复 pending tail、是否归档/卸载/fork | `kernel/sessionpolicy`、`middle/residency` | 保持现有 Application/Kernel policy | Store 只返回连续性报告或执行显式动作 |
| `TranscriptEvent -> conversation/SessionRecord` 的过滤、tool call 展开、内部行剔除、message ID 生成 | Store/Kernel/Host 多处 | `kernel/session` 唯一投影 | Store 不定义用户可见对话 |
| Store 返回目录行时硬编码 `StatusIdle` | `store/session_granular.go`、`store/json_layout.go` | Application state 投影 | Store 返回 `draft/archived/active` 等持久 lifecycle 事实；运行态由 Application 叠加 |

#### 必须保留在 Store

| Store 能力 | 保留原因 |
|---|---|
| 项目 scope、路径/布局、分片命名、head 文件格式 | 磁盘格式与旧数据兼容契约 |
| message/EVENT 行的读取：range、tail、完整 rows、head、meta | 这是实际磁盘读方法；上层只消费其结果 |
| `message rows + message head` 的原子提交 | 一次发布粒度，不能拆给 Application |
| draft tail 的 append、recover、discard 与 `head+1` 连续性分类 | 发布点、物理尾和恢复安全性均在磁盘层 |
| `EVENT rows + event head` 的原子提交 | 结构事件发布粒度 |
| lifecycle draft/queue 文件的原子迁移与 queue head/state | durable queue 的物理状态机，不是 UI 运行态 |
| context blob、stack active/history/head 与 EVENT 写序 | 原子发布顺序和锁边界 |
| compact/retention/fork 的物理前置校验与写入 | 涉及 head 水位、文件前缀、工具结果复制和不可覆盖约束 |
| per-session/module 锁、关闭、写入单写者 | 并发与持久化安全 |
| 写入时 seq 必须与持久 head 连续、提交后 head 必须单调推进 | 这是 durable write invariant；运行态由上层决定，但落盘事实必须由 Store 裁定 |

pending-tail 的 `clean/recoverable/gap` 分类继续留在 `store/session`，因为它直接依赖
`head.LastSeq`、物理分片和连续行。上层保留的是“运行中是否探测、是否主动 recover、
给用户什么 notice”的应用决策。

### Event 投影统一

事实源固定为 **message shard 中已提交的 `protocol.TranscriptEvent` 行**，不是内存
conversation、不是 provider history，也不是 root `store.Event` 的另一份结构。

唯一职责链：

```text
message shard rows
  -> store: 原始读取与 head 边界
  -> kernel/session: ProjectConversation
  -> protocol.SessionRecord.Conversation
  -> application/frontend
```

唯一投影接收实际事件 struct `[]protocol.TranscriptEvent`，输出实际的
`protocol.ConversationRecord` / `[]protocol.Message`，并显式接收策略：

- system/internal 行是否过滤；
- TL 行是否进入普通对话；
- 一个 assistant event 中的正文、reasoning 和多个 tool call 如何展开；
- `tool` event 如何生成 `tool_result`；
- message ID / EventSeq / 群聊角色字段如何映射。

Store 不能被要求 import `kernel/session`。因此 Store 侧只保留
`ReadEventRange`、`ReadEventTail`、`ReadMessageHead`、`ReadLifecycleFacts` 等原始读面；
原先会派生 conversation 的 `derivedConversationMessages`、`derivedRecordPayload`、
`derivedResumeRecordPayload` 不在 Store 内继续生成领域对话。

投影去重清单：

| 重复实现 | 目标处理 |
|---|---|
| `store/json_layout.go:derivedConversationMessages` | 语义迁入唯一 `kernel/session` 投影；Store 只返回 rows/head |
| `kernel/session:conversationFromTranscriptLocked` | 保留为唯一实现基础，拆出纯投影函数并加 policy 参数 |
| `kernel/session:conversationMessagesFromEvents` | 删除；先把 `[]store.Event` 收敛为 `[]protocol.TranscriptEvent`，再调用同一投影 |
| `internal/host:deriveSessionRecord` | 删除；Host 不生成领域 record |
| `store/conversation.go:decodeConversationRange` 的 conversation 语义 | 改为原始事件窗口读取；过滤、分页前的逻辑投影统一到 Kernel Session |
| `application/conversation/unified.go:MainRecords` | 保留为协议 Message 到 conversation-record DTO 的视图映射，不得反向生成 SessionRecord |

### 实际 Struct 统一

同一业务概念只能有一个非别名 struct；允许跨层保留 alias 和编译期断言，不允许以
“字段差不多”为由再定义一份。

| 概念 | 唯一实际 struct | 允许的兼容形式 | 必须删除/改名 |
|---|---|---|---|
| message event | `contract/protocol.TranscriptEvent` | `store.Event`、`middle/protocol/model.TranscriptEvent` 只能是 alias | `store/session.TranscriptEvent` 这类只有 Seq/Type 的丢字段投影；若仍需结构事件，改名 `StructuralEventRef` |
| 可见消息 | `contract/protocol.Message` | `middle/protocol/model.Message` 只能是 alias | `store.ConversationMessage` 独立 struct 先改为同身份 alias，随后删除 |
| 工具调用 | `contract/protocol.ToolCall` | model alias | `store.ConversationToolCall` 独立 struct |
| 完整会话聚合 | `contract/protocol.SessionRecord` | model/service DTO 以 alias 暴露 | 其他同名完整 record struct |
| Store 会话索引/摘要 | 当前 `contract/dto.SessionRecord` | alias 只可短期保留 | 改名为 `SessionIndexRecord` 或 `SessionCatalogRecord`，避免与完整 `SessionRecord` 身份混淆 |
| durable queue 对外项 | `contract/dto.QueueItem` | root `store.QueueItem` alias | `store/session.QueueItem` 不再作为公开同义结构；内部持久行可保留私有结构但只通过明确函数转换 |

落地时增加双向编译断言和 JSON round-trip 用例，证明：

- `store.Event` 与 `protocol.TranscriptEvent` 是同一类型；
- `model.Message` 与 `protocol.Message` 是同一类型；
- Store 读出的 `protocol.Message` 可直接放入 `protocol.SessionRecord.Conversation`；
- 删除独立 Store conversation struct 后，字段、JSON tag、`omitempty` 与旧数据解码不变。

以下看起来像重复，但语义不同，不能合并：

- UI Composer 草稿；
- durable lifecycle `DraftItem`；
- message channel 的 pending tail；
- 运行期 InputQueue 与 durable queue mirror；
- 持久 `archived` 状态与运行期 `restoring` 投影；
- Task/Goal 领域自己的状态机与 Store 对它们的 append-only 投影。

### 目标 Session 职责矩阵

| 业务 | 唯一 owner | 其他层只允许 |
|---|---|---|
| 当前进程的 SessionUnit、ChatState、审批、运行期 Queue、View、Composer | `application/session/state` | Application 用例调用公开命令；View 读 DTO 投影 |
| 可见状态优先级与目录活体行 | `application/session/state` 的纯投影；`viewstate` 映射 Snapshot | 不按字段另行重算第二份状态 |
| transcript 运行时顺序、事件身份、消息编辑 CAS 基线 | `kernel/session` 的 Session transcript API | `kernel/task` 只追加事件；store 只校验和持久化 |
| TranscriptEvent → conversation/SessionRecord 投影 | `kernel/session` 的唯一函数，规则可由端口注入 | store 不 import Kernel；Host 只转换端口值 |
| durable message/EVENT JSONL、head、seq 校验、分片、pending-tail 分类 | `store/session` | Kernel/Application 通过窄端口调用，不重算物理连续性 |
| durable queue/lifecycle/retention/compact/fork 的物理原子写序 | 独立 Session/Context store adapter（不再挂在根 God receiver） | Application UoW 编排跨域顺序 |
| cold/restore/resume、draft 物化、archive/unload/fork 应用用例 | `application/session/*` + `kernel/sessionpolicy` | Host/Backend 只注入能力与执行副作用 |
| 跨 transcript/artifact/compaction 的提交顺序 | Application Unit-of-Work | Store 只提交单一聚合 |

### Context / Stack / Wire

- 纯 Wire 装配已经唯一实现在 `store/context/wire*.go`
  （入口 `AssembleWireRows`）；外部没有第二份 wire 算法。
- 根 `wire_assembler.go` 不是第二份 wire 算法，而是：
  - 从 compact/message head 读取 frame 和尾行；
  - 把根 `Event` 适配成 `context.WireRow`；
  - 把 attempt cache 适配成 `WireAttemptProvider`；
  - 调用 `context.AssembleWireRows`。
- `kernel/session/wire_history.go` 只是消费端口，`internal/host` 只是装配转发；
  两者都不重新装配 wire。
- Stack 的条目类型和基础词表已进入 `store/context`，但批次弹栈、锚、水位、head
  和 EVENT 写序仍在根 `stack_channel.go`/`stack_journal*.go`。
- Task/Goal/Subagent 领域自己的状态机不是 Stack 存储算法的复制：它们拥有业务状态，
  Store 只持久化 append-only 投影。唯一需要迁移的是根包的 I/O orchestration。
- 下一步应把 wire/stack 的 I/O orchestration 移到 Session/Context adapter；纯算法
  继续只留一份，根 Router 不再暴露领域编排。

### 兼容层现状

| 域 | 根包兼容文件 | 唯一 owner |
|---|---|---|
| Artifact | `media.go`、`big_tool_result.go`、`search_index.go`、`tool_record.go` | `store/artifact`、`store/record` |
| Board | `project_board_compat.go`、`board.go` | `store/board`、`contract/dto` |
| Project | `project_record.go`、`project_known.go` | 尚无独立 `store/project` |
| Teamwork | `teamwork.go` | `store/teamwork` |
| WorkUnit | `node_session_store.go`、`unit_record_writer.go` | `store/workunit` |
| Role | `role_session.go`、`role_session_adapter.go`、`team_registry.go`、`role_permission.go` | `store/role` + 显式 Session I/O adapter |

## 影响文件清单

| 范围 | 修改类型 | 重点文件 | 原因 |
|---|---|---|---|
| Artifact 消费方 | 修改 | `backend/tool/io/media/media.go`、`internal/host/session_workspace_ports.go`、`boot/*` | 直接持有 `artifact.Store`，删除根 media alias |
| Board 消费方 | 修改 | `kernel/goal/*`、`kernel/board/*`、`boot/tools.go`、`internal/host/session_workspace_ports.go` | 使用 `store/board` + contract DTO |
| Project | 新增/迁移 | 新 `store/project`；`boot/tools.go`、`backend/engine/assemble/*`、`backend/engine/context/seelectx/*` | ProjectRecord/Knowledge 尚无领域包 |
| Teamwork | 批量修改 | `backend/multiagent/teamwork/*`、`backend/engine/assemble/*`、`kernel/board/*` | 直接使用 `store/teamwork` 类型/常量 |
| WorkUnit | 新增/迁移 | 新 `workunit.SessionStore`；`backend/{subagent,workunit,worktree}`、`internal/host` | 删除根 `NodeSessionStore`/writer 包装 |
| Role | 迁移 | 新 Role session service/port；`internal/host`、`backend/engine/assemble` | 根兼容面含真实 orchestration，需按端口拆分 |
| Session | 删除包装 | `message_rows.go`、`structural_events.go`、`pending_tail.go`、`queue_consume.go` | 直接调用 `store/session`，无第二份判据 |
| Session Runtime | 收口状态 | `application/session/state/*`、`application/session/viewstate/session_runtime.go` | 删除未使用 `SessionUnit` 状态迁移；可见状态与 queue 计数各留一份判据 |
| Session Projection | 合并实现 | `kernel/session/archive.go`、`kernel/session/fork.go`、`store/json_layout.go`、`store/conversation.go`、`internal/host/session_workspace_ports.go` | event→conversation/SessionRecord 只能一份，输入输出使用唯一实际 struct |
| Transcript Seq | 收口 owner | `kernel/task/task_context_state.go`、`store/session/session.go`、`store/message_rows.go` | 运行时分配一份，Store 只校验 |
| Store Lifecycle | 剥离运行态 | `store/json_layout.go`、`store/session_granular.go` | 删除 `StatusIdle` 默认与运行态推导；只暴露 draft/archived/active 持久事实 |
| Struct Identity | 去重 | `store/conversation.go`、`store/session_granular.go`、`contract/dto/session_record.go` | 保留唯一实际 struct；旧名只允许同身份 alias |
| Wire/Stack | 收缩 | `wire_assembler.go`、`stack_channel.go`、`stack_journal*.go` | 去掉类型/纯函数根重导出，I/O 留在明确适配器 |

## 依赖分析

- 这些域均被 `backend/engine/assemble`、`backend/multiagent/*`、`internal/host`、
  `kernel/*` 和 `boot` 消费。
- Project/Board/Teamwork/WorkUnit 的导出面变化会触发外部依赖批次协议。
- Session/Context 的类型与 Router 方法变化会影响 `internal/host` 和 persistence ports。
- 当前没有发现需要新增领域包间直接 import 的合法用例。
- Session 状态收口会触及 Application、Kernel、Host 三处，但不得改变 wire 状态词、
  状态优先级、readiness、persistability 或现有 JSON。
- event→conversation 合并必须先用同一份输入做 parity：正文、reasoning、tool calls、
  tool results、role attribution、内部行过滤和 message ID 全部逐字段一致。
- seq 收口必须覆盖 fork 继承 seq、draft append、resume 尾窗、幂等重放和并发单写者。

## 循环依赖检查

- [x] 新领域包只允许依赖 contract、store/root、store/internal/layout 和标准库。
- [x] 领域包禁止 import 根 `store`、Application、Middle、Backend、boot、host。
- [x] Project 的派生看板继续依赖 Board 的只读端口，不反向 import Project。
- [x] Wire/Stack 的 I/O adapter 放消费侧或 Session，不把上层 Router 类型带入 Context。

## 风险预估

- Project/Board 迁移同时影响 goal 看板与 project_refresh：高概率、高严重度。
- WorkUnit 的 session root 解析若仍靠根 `Router` 私有路径，可能形成新 facade：中概率、高严重度。
- Role snapshot 同时读 message/event/compact，需避免形成新万能接口：高概率、中严重度。
- Session wrapper 删除主要是机械项，但不能改成第二套 seq/head 判据：中概率、高严重度。
- 全仓测试耗时较高，必须按域分批并保留 `-race`。
- `SessionUnit` 旧状态方法没有生产调用方；直接删除时容易误删仍有 UI 价值的
  Resident/Chat/Queue 访问器：中概率、中严重度。
- event→conversation 的几份实现字段保真度不同；直接选“最简单的一份”会丢工具调用、
  reasoning 或群聊归属：高概率、高严重度。
- Store 继续派生 SessionRecord 会迫使 Kernel/Host 复制同一投影；直接把上层语义塞进
  Store 又会违反存储边界：高概率、高严重度。实现前必须先固定唯一投影的输入/输出契约。
- 统一实际 struct 时若直接复用 `dto.SessionRecord` 与 `protocol.SessionRecord`，会把
  “存储索引摘要”和“完整会话聚合”再次混成同名概念：高概率、高严重度。必须先改名，
  再做接线。

## 建议方案

先做横切结构门禁，再做域迁移。每片先红后绿并单独验收：

1. **基线与结构红灯**：冻结 Router/jsonRepository/storeEngine/SessionGranularStore
   方法集合；记录根 Store import、`*For` 出口、Store 外 Session 状态/投影/seq 实现；
   新增“根 God 方法只减不增”“领域反向依赖”“Host 构造万能 Store”“第二份状态判据”
   失败门禁。
2. **Session Runtime 状态收口**：把可见状态优先级收进
   `application/session/state`；`PendingRequests` 为 queue 权威，ChatState 只存投影；
   删除 `SessionUnit` 未使用的命令式迁移方法和重复 `loaded/status` 字段；Store 目录/
   record 派生不再默认 `idle`，只返回持久 lifecycle 事实。
3. **Store 写粒度冻结**：逐项登记并保留 message/EVENT、lifecycle、context、stack、
   compact/retention/fork 的原子写方法和物理读方法；补写序、崩溃恢复、head 发布、
   锁与旧 JSON 兼容测试。运行态迁移不得顺手把这些能力搬出 Store。
4. **Transcript Seq 收口**：Kernel Session 唯一决定运行时事件身份；Store 保留
   durable seq/head 连续性校验与落盘提交。补 fork、draft、resume、幂等重放和并发测试。
5. **实际 Struct 去重**：先把 `TranscriptEvent`、`Message`、`ToolCall`、完整
   `SessionRecord` 的 alias/实际类型钉死；`dto.SessionRecord` 改成明确的索引摘要名；
   加编译期同类型与 JSON parity 测试。
6. **Session Projection 收口**：定义唯一的 `TranscriptEvent -> SessionRecord` 契约及
   policy 输入；Kernel/Host/Store 的多份 event→conversation 实现迁移到它并删除。
   Store 保留原始 rows/head/meta 读口，不再生产领域 conversation。
7. **Session 窄端口**：Kernel/Application 只依赖消费方定义的 raw transcript、tool-result、
   record-meta、context snapshot、queue、pending-tail 端口；`internal/host` 只做 DTO
   转换；composition root 注入具体实现，不新增 facade/getter。
8. **Wire/Stack 适配器**：保留 `store/context.AssembleWireRows` 唯一算法；把 frame/rows/
   cache 读取和 Stack 数据→head→EVENT 写序移出根 God receiver 到明确 Context adapter。
9. **Artifact**：runtime/host 直接持有 `artifact.Store`；删除根 media/blob/search/toolrecord 兼容。
10. **Board**：消费方直接调用 `store/board` 与 contract DTO；删除根 Board 转发。
11. **Teamwork + WorkUnit**：消费方改直接领域类型；WorkUnit 改为领域 SessionStore +
   path resolver 端口。
12. **Project**：新建 `store/project`，迁移 ProjectRecord/Knowledge；Board/goal 只消费
    Project DTO 或窄读口。
13. **Role**：把根真实 orchestration 拆成 role session service + Session/Event/Compact
    窄端口。
14. **全仓验收**：根兼容面归零、Session 外无第二份状态/投影/seq 判据、无直接 import
    根 Store 的生产调用方（除已登记的 composition/test 面），并保存方法数、文件数和
    import ratchet 证据。

任何修改领域目录外调用点的批次都必须按 `external-change-protocol.md` 保存 manifest、
checkpoint、dry-run、apply、rollback 与可重放 patch/脚本。无法用窄端口表达的合法用例
先报告冲突，不以 adapter/facade 绕过。

## 2026-10-28 实施记录

### 已完成

- **Session Runtime 状态收口**：删除 `SessionUnit.status/loaded` 与
  `Submit/Finish/ColdLoad/Unload/HasSession/Status` 第二套命令式状态机；
  新增 `application/session/state.ProjectVisibleStatus`，优先级固定为
  `restoring > awaiting_approval > running > queued > idle`，队列只认
  `PendingRequests`。`viewstate` 只组装事实并调用该纯投影。
- **唯一 conversation 投影**：新增
  `kernel/session.ProjectConversationEvents`，统一正文、reasoning、多 tool call、
  tool result、TL/internal/system 过滤、群聊归属、路由字段和消息 ID。Kernel、
  Host、Fork 全部调用这一实现。
- **Store 停止派生 conversation**：删除 `DerivedRecordWorkspace`、
  `DerivedResumeRecordWorkspace`、`LoadConversationRangeWorkspace`、
  `derivedConversationMessages` 与独立 conversation struct；Store 只返回原始
  message rows、head、compact 边界和 lifecycle 事实。
- **结构身份**：`store.Event = protocol.TranscriptEvent`、
  `store.ConversationMessage = protocol.Message`、
  `store.ConversationToolCall = protocol.ToolCall`；`dto.SessionRecord` 改为
  `SessionIndexRecord`（旧名暂留 alias）；durable queue 的持久行改名为
  `session.QueueRow`，公开投影仍为 `dto.QueueItem`。
- **Transcript seq 单一 owner**：Kernel/Role sequencer 显式分配 seq；Store 拒绝
  零 seq 与跳号，只保留 `<= head` 幂等过滤和 `head+1` 连续性校验。
- **持久 lifecycle 状态**：Store 目录/index 的普通会话返回持久事实 `active`；
  Host 在可见边界把 `active` 映射为 `idle`。旧 `idle` 仍可解码。
- **Project owner**：ProjectRecord、ProjectKnowledgeBuilder、
  RefreshProjectKnowledge 与 project-known 渲染迁入 `store/project`；根包只留
  alias 与 Router 单层转发。Boot 与 Engine Context 已直接消费新 owner。
- **Teamwork/WorkUnit 类型收缩**：`backend/multiagent/teamwork` 的生产文件已全部
  改吃 `store/teamwork` + `dto.SessionKey`，不再 import 根 Store；Engine、
  Subagent、Worktree 与 WorkUnit 的记录形状改吃 `store/workunit.Record`。
- **Teamwork 根 alias 已物理删除**：根包测试与 DTO 一致性测试已改用
  `store/teamwork`；`store/teamwork.go` 不再存在。
- **Board session-alias 已物理删除**：Goal/Team board 的类型、常量和校验调用
  已改用 `contract/dto` + `store/board`；`store/board.go` 不再存在。
- **Board Router 转发已物理删除**：新增 `internal/host/boardrouter` 无状态 adapter；
  `store/project_board_compat.go` 不再存在，Router method surface 从 121
  收紧到 113。会话删除只保留组合根注入的 reclaim callback。
- **Project 根兼容层已物理删除**：Project 测试与 Boot 直接调用
  `store/project`；`store/project_compat.go` 与两个 Router project-known
  便捷方法不再存在。
- **Artifact 根纯转发已删除**：`big_tool_result.go`、`search_index.go`、
  `tool_record.go` 不再存在；根测试直接经 `store/artifact` 与
  `store/record` owner 验证。`storeEngine` 再减 10 个方法。
- **Conversation alias 文件删除**：`store/conversation.go` 已删除；
  conversation 测试直接使用 `contract/protocol`，不存在独立 Store 消息类型。
- **WorkUnit 写链根 alias 删除**：`store/unit_record_writer.go` 已删除；
  Engine/Subagent 直接构造 `store/workunit.Writer`。
- **Context 类型收缩**：`backend/engine/context/seelectx` 的 CompactFrame /
  EvidenceRef / TranscriptEvent / CompleteEventUnits 改用 `store/context` 与
  `contract/protocol`。
- **外部测试同步迁移**：Engine、Teamwork、Subagent、WorkUnit、Worktree、
  Kernel Board、test/ 与 Assembly Integration 的领域类型引用已切到 owner；
  根包内直接验证旧 `jsonRepository` 写面的测试仍保留，不能靠测试专用 alias
  掩盖生产兼容层。
- **根 Store 生产 importer ratchet**：79 → 21；根生产 `.go` 文件 49 → 37。
  剩余文件主要是 Router、`SessionGranularStore`、message/EVENT/Stack/Wire
  的真实 I/O 编排，以及 Role session 与组合根作用域适配。

### 第二批 owner 收口（2026-10-28 继续）

- **WorkUnit**：删除根 `NodeSessionStore` 实体与根类型 aliases；新增
  `store/workunit.SessionStore` + `SessionRootResolver`，`Stores.WorkUnits`
  成为生产入口。Assembly、Subagent、WorkUnit 改吃 `workunit.Recorder/Record`；
  根包只留 `workunit_roots.go` 的路径解析器。
- **Role permission**：删除根 `role_permission.go` 与 Host 侧
  Router→PermissionStore 的全局 `sync.Map`；`Stores.RolePermissions` 在组合根
  构造一次并显式注入，权限缓存实例身份不再靠运行期全局表维持。
- **Role registry**：删除根 `team_registry.go` 及全部 registry alias /
  `Read/Write/RemoveTeamRegistryWorkspace`；旧数据诊断直接走
  `store/role.Store`。角色会话/floor/lifecycle 兼容读原语暂时留在
  `role_session_router.go`，等待 Session engine 下沉。
- **Artifact media**：删除根 `media.go`、Router media 方法、`MediaStoreOf`
  测试能力发现和根 media aliases；新增 `artifact.SessionStore`，Runtime
  显式注入 `Stores.Media`。`ToolResult.Multimodal` 改用
  `artifact.MediaRef`。
- **Board adapter 方向**：`boardrouter` 从 `store/` 移到
  `internal/host/`；它是无状态端口转发，不再让底座依赖根 Store/Kernel。
- **wire 去重**：`ScheduleEventPayload` 与 `RoleDraftSyncResult` 的唯一实际
  struct 收口到 `contract/dto`。
- **Role session 类型/投影继续归位**：`RoleSnapshot`、`RoleWireMessage`、
  `RoleWireSnapshot`、Snapshot warning 与 wire message 投影迁入
  `store/role`；根包只剩同身份 alias。Role 常量与 Draft alias 只由
  `_test.go` 为旧 fixture 保留，生产 Host 直接消费 `store/role`。
- **Role session 判据归位**：Snapshot 前缀切分/告警、compact_ref 校验、
  pending 合并与 wire 投影只在 `store/role`；Root 只负责读 head/rows 和
  调用共享 wire 算法。
- **Role/Stack/Wire 离开 God receiver**：16 个 Role、10 个 Stack、1 个 Wire
  方法从 `Router` 移入 `RoleSessionAdapter` / `StackStore` / `WireStore`；
  Host 与 SessionContext 显式消费这些适配器。
- **跨层叶子依赖倒置**：`EventLogEntry` 收口到 `contract/dto`；
  Goal/Context 改用消费方端口；AgentSession/Subagent 使用
  `PreparedHistory` 工厂；HistorySearch 使用项目事件源端口。对应生产域不再
  import 根 Store。

本轮结构指标：

```text
根生产 Go 文件        40 → 37
根 Store 生产 importer 33 → 19
Router               113 → 81
jsonRepository        66 → 62
storeEngine          151 → 142
SessionGranularStore   44 → 44
```

### 验证证据

```text
go build ./...
go build -tags "gui,desktop,production" ./...
go vet ./...
go test ./... -count=1 -timeout=1200s
go test -p 1 ./... -count=1 -timeout=1800s   # 2026-10-28 最终串行全量：PASS
go test -race ./store/... ./backend/multiagent/subagent ./backend/tool/worktree -count=1
node --test gui/frontend/dist/*.test.mjs   # 874 passed
gofmt -l .                                  # empty
git diff --check                            # clean
```

`-race` 全量并行时 `TestCommitNotBlockedByFullHistoryRead` 的绝对耗时探针曾因
并行磁盘压力超过 30ms 预算；同一用例单独复跑通过，未放宽阈值。其余 race 包全部通过。

2026-10-28 最终串行全量还覆盖了两个 Windows 并行压力失败的单跑复核：
`TestRouterConcurrentProjectIsolation`（rename access denied）与
`TestManagerRealShellRoundTrip`（真实 shell 回显超时）均在独立进程中通过；
串行全仓随后整体通过。没有放宽测试阈值。

新增红绿用例：

- `application/session/state/visible_status_test.go`
- `kernel/session/projection_parity_test.go`
- `store/conversation_identity_test.go`
- `store/session/session_test.go::TestAssignRowSeqs`
- `store/media_session_test.go`
- `store/role/session_types_test.go`（Snapshot 告警与 Wire 字段保真）

### 尚未完成

- Role session 仍需要一个 `RoleSessionAdapter` 把显式项目 scope 接到
  `storeEngine` 的 message/event/compact/wire 机制；领域判据已不在该适配器。
- 根 `workunit_roots.go` 仍是 Session layout 到 WorkUnit owner 的路径适配器；
  等 Session engine 收口后可删除。
- Wire/Stack 已通过 `WireStore` / `StackStore` 显式适配器离开 `Router`；
  底层 JSONL/head 写序仍属于 Store 持久化机制。
- 最终 M11 要求“生产调用方不再 import 根 Store”尚未达成；allowlist 已从
  79 收缩到 19，剩余兼容调用继续只许减少。

### 预置验收断言

- 同一 `(events, record policy)` 经唯一投影得到逐字段相同的 conversation 和
  `SessionRecord`。
- `store.Event`、Kernel/Application 的 `TranscriptEvent` 与
  `contract/protocol.TranscriptEvent` 是同一实际 struct；Store 读出的
  `protocol.Message` 可直接写入 `protocol.SessionRecord.Conversation`。
- 删除 `store.ConversationMessage` 后，旧 state blob 和新事件行读取的字段/JSON
  round-trip 全部一致。
- 所有持久化事件都有显式 seq；Store 不把零 seq 静默补成新号。
- 运行中 + 有审批时，所有入口都投影为 `awaiting_approval`；恢复中优先返回
  `restoring`；队列状态只由 `PendingRequests` 决定。
- Store 不返回 `running/queued/awaiting_approval/restoring`；目录与 Snapshot 的这些
  值只能由 Application 层叠加。
- 运行态收口前后，message/EVENT、lifecycle、stack、compact、retention、fork 的
  原子写文件集合、写序、head 发布点与磁盘读接口不减少、不改变语义。
- 运行期 InputQueue、durable queue 和 pending tail 各自的恢复/失败用例互不替代。
- fork 继承区间、子会话新事件、draft append 与最终提交不存在 seq 重叠。
- Wire 的 compact frame、尾窗、attempt K、预算和 prefix digest 不因 adapter 迁移变化。
