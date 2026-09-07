# Session Store

## Application state sidecar

Alongside framework message history, every backend can persist an opaque application-owned state blob keyed by `(project_id, session_id)`. `Router.SaveState` and `LoadState` use the same JSON, SQLite, PostgreSQL, or Redis selection as history. The store does not inspect the blob; application code uses it for the visible transcript, Plan projection, and provenance caches without putting those records into provider history.

## Unified partition and shard contract

Every backend partitions first by `project_id`, then isolates `session_id`, then stores immutable history generations in fixed-size message shards. A manifest atomically switches the active generation, so readers see either the old complete generation or the next one. JSON uses generation directories; SQLite and PostgreSQL use `seelex_session_manifest` plus `seelex_session_shard`; Redis uses a project hash-tagged manifest and shard keys in one cluster slot. This keeps range/recovery semantics independent of the chosen storage strategy.

## 模块定位

`sessionstore` 提供统一、原子、项目作用域的会话持久化。当前 backend 为 JSON shards、SQLite、PostgreSQL 和 Redis；调用方只依赖 `Repository`/`Router`。

## 数据模型

`Key{ProjectID, SessionID}` 是唯一存储键。名称不是索引。空 ProjectID 表示未绑定项目的默认 scope。

`Repository` 契约：

- `WriteAtomic`：完整替换一个逻辑 history，读者只能看到旧版本或新版本。
- `Read`/`ReadRange`/`List`/`Delete`：都显式接收 project scope。
- `Ping`/`Close`：生命周期管理。

## Backends

### JSON

每次写创建新的 `generation-*` 目录，将 history 按 100 条分 shard，最后原子替换 `manifest.json` 指向新 generation。旧 generation 不会在 manifest 提交前暴露。

**transcript 事件按增量追加到会话目录 `transcript.log`**（append-only JSONL，一行一个
`Event`，含显式 `kind`：user_input/llm/tool_call/tool_output/error/…），不再随
generation rollover 整代重写 `events.NNN.json`。追加按 `Seq > 已落盘 head` 求增量，
重复提交幂等；崩溃残尾（未换行收尾的半行）按恢复语义跳过。事件读取
（`ReadEventTail`/`ReadEventRange`）以日志为物理事实源；history/state 仍走
generation 快照，属于派生投影。

状态：**JSON 后端已落地**；SQLite/PostgreSQL/Redis 的 transcript 仍为 generation
snapshot 布局（有序日志后端子设计见
[`docs/2026-09-07-session-order-log/README.md`](../docs/2026-09-07-session-order-log/README.md)）。

### SQLite/PostgreSQL

统一使用 `seelex_session_manifest` 与 `seelex_session_shard`：manifest 以 `(project_id, session_id)` 定位当前 immutable generation，shard 以 `(project_id, session_id, generation, shard_index)` 保存固定大小的消息片。事务先写新 generation，再原子切换 manifest。旧版单行 `seelex_sessions.messages_json` 仍可读取，下一次写入自动迁移到分片表。SQLite 使用 modernc，无 CGO；PostgreSQL 使用 pgx stdlib。

SQLite 本地库打开时固定附加 `_pragma=busy_timeout(5000)` 并把连接池收敛为单连接：SQLITE_BUSY 只发生在多连接之间，单连接后由 `database/sql` 排队，多会话并行落盘不再随机失败（只设 busy_timeout 不够 —— 它覆盖不了同进程内读事务与写事务的升级冲突）。

### Redis

Redis 使用 `redis://` 或 `rediss://` DSN。每个项目拥有一个 hash-tagged keyspace；同项目的 manifest、history shards、state 和 session index 位于同一 Cluster slot，因此一次 `MULTI/EXEC` 可以原子切换该 session 的 generation。DSN 仅写入本地配置，GUI 只显示 `configured`。

## Router

Router 用 RWMutex 把 active repository、config 和 project ID 绑定为原子视图。`Configure` 先 normalize/open/ping/save config，再在锁内 swap，最后关闭旧 backend。进行中的旧操作完成后，新操作才看到 replacement。

显式 `LoadWorkspace` 等方法不修改 active write scope，避免恢复其他项目会话时读错 shard。

## 会话展示元数据（`SessionMetaStore`）

`SessionMeta`（置顶、别名、手动排序位）描述"用户怎么看这个会话"，不参与执行与存储
归属。它以**项目级 blob** 落在 state 通道的伪会话键 `seelex:project:session-meta`
上：

- 为什么不并进 `SessionRecord`：record 每次回合结束都由存活状态整体重建，夹在其中的
  展示字段会被覆盖；独立 blob 与记录写路径完全隔离。
- 为什么不造新通道：JSON/SQLite 的目录枚举只认有 manifest 的会话目录，只写 state 的
  键不会出现在 `SessionsOf`/`List` 里（回归用例 `TestSessionMetaStoreRoundTrip` 钉住
  这一点，避免造出幽灵会话）；`Delete(session)` 也删不到它。
- 写是读-改-写，进程内由 `SessionMetaStore.mu` 串行化（桌面单进程形态；实例由
  `main.go` 构造一次并以指针注入 `SessionPort`，值拷贝后仍共用同一把锁）。
  `TestSessionMetaStoreConcurrentSets` 在 `-race` 下断言无丢失更新。
- 读失败或缺键一律按"无元数据"处理：展示元数据不得让会话目录整体失败。

## Seele v2 会话适配

两个适配器把 Router 接到 Seele v0.0.8 的会话契约：

- `durable_history.go` — `DurableHistory` 实现 `seelectx.DurableHistory`
  （Load/Save/Clear）：Session 每次 Chat 前 Load、结束后 Save；`Reset`
  显式清空。Save = ProviderHistory 原子写 + 会话状态 blob（SaveState
  编排）；SessionRecord/TranscriptEvent/ToolResults 的持久化继续由
  `SaveCommit` 负责。
- `session_context.go` — `SessionContextStore` 读写会话级上下文记录
  （state blob）：SystemPrompt + Plan/Task/Skill/Compact 四栈 + 聊天队列
  （"now using X" = 栈顶）。Schema 版本校验失败显式拒绝加载（不静默重建），
  走会话恢复错误路径。

主 Runtime 通过 `seelebridge.Runtime.AttachHistoryRouter` 独立装配 `DurableHistory`，不复用 `SessionContextStore` 的 application-owned state blob。恢复会话时 DurableHistory 与框架 Session 使用同一个 session ID；Application 成功提交完整 `SessionRecord` 后才释放 provider working history，下一轮再从 durable tail 冷加载。

## 子代理会话记录（NodeSessionRecord）

子代理（fork/plan 的 `kind:agent` 节点）会话记录按主会话索引落盘：

```text
sessions-json/<project>/session-<mainID>/subagents/<mainID>-<subID>.json
```

文件名为 `<mainSessionID>-<subSessionID>.json`（用户约定：从主会话索引可直接
列举全部子会话，无需全局扫描）。内容为 opaque JSON 记录
（`NodeSessionRecord`：NodeID/SessionID/Goal/Status/History/ContextJSON/
StagesJSON/ResultJSON/Worktree 现场 + schema 版本）。

生命周期（2026-08-24 策略）：运行期由 `seelebridge/session` 的
`SubagentSessions` actor 在注册/阶段/结果/终态时写入（进程中断可恢复）；
节点结束（done/failed）时最终结论经 `seelex.subagent.result` 事件写入主会话
事件库（"结论跟随 mainagent"），随后删除节点记录文件。删除后详情数据面保留
在进程内存快照；重启后恢复锚点从主会话事件库重建。

`NodeSessionStore`（JSON backend 已实现）提供 `Save/Load/List/Delete`，显式
项目作用域，不改变 Router 的 active write scope；SQLite/PostgreSQL/Redis
接入为后续项（当前返回明确错误）。

## 配置与安全

- JSON/SQLite 使用本地 path；PostgreSQL/Redis 使用 DSN。
- `Config.Safe` 不把 DSN 返回 GUI，只报告 configured。
- 配置文件写入采用原子替换和私有权限。
- project/session ID 进入路径前经过 hash/安全编码，不能直接形成逃逸路径。

## Review 指南

- 所有 backend 是否保持相同逻辑 snapshot 语义。
- JSON manifest 是否最后提交；失败 generation 是否不会被读取。
- SQL migration/upsert 是否兼容 SQLite 与 PostgreSQL placeholder，Redis key 是否保留同一 project hash tag。
- Router 是否在任何错误路径关闭 replacement、保留 old repository。
- range offset/limit 和 empty history 的语义是否一致。

## 测试

```text
go test ./sessionstore -count=1
```

## Atomic transcript and result contract

`WriteCommit` publishes bounded provider history, append-only transcript events, opaque application state, and immutable tool-result objects under one `(project_id, session_id)` scope. `WriteAtomic` remains a compatibility wrapper for history-only callers.

`ReadEventTail` returns newest complete protocol units within token and unit limits. A user turn may include sequential or parallel tool rounds, but it is omitted if any tool call lacks a matching result; orphan tool events are never returned alone. `ReadToolResult` is read-only. JSON manifests publish the committed result-reference set, SQL stores all parts in one transaction, and Redis uses one `MULTI/EXEC` in the project hash slot.

测试覆盖 JSON/SQLite 的 generation 原子性与状态 sidecar、SQLite 分表分片、Redis 的配置和 key 分片策略、backend 切换和显式 workspace read 不污染 active scope。

## 会话 fork 存储契约（一期，已实现）

对话 fork（新开会话的分支，**非 subagent fork**）在存储层的契约：

- **段落边界**：`EventParagraphs` 由完整协议单元（`CompleteEventUnits`）推导
  段落表（EventFrom/EventTo/RequestID/MessageFrom/MessageTo）；fork 切断点
  必须落在段落边界（`IsParagraphBoundary`），EventSeq 含端点，避免截出半截
  轮次。
- **通道枚举**：`ListToolResults` 返回 tool-results 通道全部不可变结果（含
  `compressed:<segment_id>` 压缩原文归档），供 fork 深拷贝物理复制。
- **快照版本**：`CurrentGeneration` 返回会话当前已发布 generation，是血缘
  `forked_from_generation` 的事实来源（不可变快照绑定，禁止引用父的最新
  内存状态或后续提交）。
- **落盘路径**：fork 由 application 层构建截断后的 `Commit`（ProviderHistory
  重建缓存 + 继承事件流 + 截断 SessionRecord + 父通道全量 tool-results），
  经 `WriteCommit` 原子写入子会话 key；两端此后完全独立。删父安全的前提是
  tool-results 物理复制——`read_tool_result`/`read_compressed_turn` 在删父后
  仍从子会话自己的通道读回。
- **不随 fork 传播**：子代理节点记录（`subagents/`）与执行事实事件库
  （`framework-events.json`）归属父会话执行，不复制到子会话。

血缘 meta（`forked_from`/ForkPoint）与截断重写属于 application 层
（`application/model` + `application/core/session_runtime`），存储层只负责
通道与原子提交，不解释内容。
