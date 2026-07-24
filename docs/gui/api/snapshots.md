# Snapshot、Event 与 Generation 语义

## 三类版本

| 字段 | 变化原因 | 用途 |
|---|---|---|
| `protocol_version` | 不兼容契约升级 | 能否解析 |
| `revision` | scope 内权威内存状态提交 | 条件 mutation、reducer 一致性 |
| `generation_id` | 成功发布持久化 checkpoint | 崩溃恢复、回滚、分页基线 |

三者不能互换。多个 revision 可以尚未 checkpoint；一次 checkpoint 对应某个已提交 revision。generation rollback 会恢复出新的内存 revision，而不是把 revision 数字倒退。

## Snapshot

Snapshot 是某 scope 的权威、深拷贝、可序列化状态。Workbench Snapshot 只含页摘要和 limits；Session Snapshot 包含有界 transcript window；Workspace 大数据使用 Query Page。Snapshot 不包含 token、绝对路径、完整 Workspace 树或无界历史。

客户端接受 Snapshot 时记录 revision floor 和起始 seq。网络 adapter 可用 ETag 缓存；`304` 只在协议版本、scope 和授权上下文相同的情况下返回。

## Event

Event 的 seq 在 scope 内严格单调。客户端处理规则：

1. 无基线、seq gap、未知 kind 或 payload 无效 → 拉同 scope Snapshot。
2. Event revision 低于 Snapshot floor → 只推进 seq，不重复归并。
3. 同 revision 的兄弟事件按 seq 分别处理。
4. session A gap 不刷新 session B 或 Workbench。
5. `resync.required` 不携带补丁，客户端必须拉 Snapshot。

## Generation

generation 只有在 manifest 和资源完成校验、最终目录原子发布后才存在。`current` 更新是单独 CAS 步骤。API 只列出 committed generation，不暴露 staging。

Rollback 操作：校验目标完整兼容 → CAS current → 从目标恢复领域状态 → 分配新的内存 revision → 发布 snapshot changed。目标 generation 本身不修改，也不创建伪造的父链。

## 一致性级别

- 单 scope action 后读：使用 response revision 或 `If-None-Match` 轮询，可获得 read-your-writes。
- 跨 session/workspace：不提供全局串行化；通过各 scope revision、precondition 和事件关联实现明确一致性。
- generation：提供单 session 原子发布；不同 session generation 无全局事务。
