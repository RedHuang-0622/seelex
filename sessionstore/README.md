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

### SQLite/PostgreSQL

统一使用 `seelex_session_manifest` 与 `seelex_session_shard`：manifest 以 `(project_id, session_id)` 定位当前 immutable generation，shard 以 `(project_id, session_id, generation, shard_index)` 保存固定大小的消息片。事务先写新 generation，再原子切换 manifest。旧版单行 `seelex_sessions.messages_json` 仍可读取，下一次写入自动迁移到分片表。SQLite 使用 modernc，无 CGO；PostgreSQL 使用 pgx stdlib。

### Redis

Redis 使用 `redis://` 或 `rediss://` DSN。每个项目拥有一个 hash-tagged keyspace；同项目的 manifest、history shards、state 和 session index 位于同一 Cluster slot，因此一次 `MULTI/EXEC` 可以原子切换该 session 的 generation。DSN 仅写入本地配置，GUI 只显示 `configured`。

## Router

Router 用 RWMutex 把 active repository、config 和 project ID 绑定为原子视图。`Configure` 先 normalize/open/ping/save config，再在锁内 swap，最后关闭旧 backend。进行中的旧操作完成后，新操作才看到 replacement。

显式 `LoadWorkspace` 等方法不修改 active write scope，避免恢复其他项目会话时读错 shard。

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
