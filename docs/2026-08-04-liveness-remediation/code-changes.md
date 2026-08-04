# 代码变更摘要

## 新增/修改文件

| 文件 | 类型 | 说明 | 设计方式 |
|---|---|---|---|
| `application/contract/ports.go` | 修改 | 扩展 Runtime 投影与 mailbox 接口 | 应用层拥有的 Port 接口 |
| `application/core/runtime_projection.go` | 新增 | 复制状态后单向发布 Runtime 投影 | 不可变值对象 |
| `application/core/service_snapshot.go` | 修改 | Runtime 锁外采集、Snapshot 纯内存副本 | collect/apply 两阶段 |
| `application/core/session_scope.go` | 修改 | 异步 session catalog 缓存与非阻塞关闭 | 后台缓存 worker |
| `application/core/chat.go` | 修改 | 子代理 merge-back 在主会话锁外 drain | mailbox 消费者 |
| `seelebridge/actor.go` | 修改 | Runtime 本地投影和有界 mailbox | 单向消息边界 |
| `seelebridge/agent_node.go` | 修改 | 子代理读取本地父证据、非阻塞 merge-back | actor 隔离 |
| `seelexctx/lifecycle/actor.go` | 修改 | OperationTimeout、CloseContext、取消后的 drain | 生命周期状态机 |
| `seelexctx/lifecycle/pipeline.go` | 修改 | 有时限 flush 与 context-aware 关闭 | 单一 flush 所有者 |

## API 变更

| API | 变更 | 兼容性 |
|---|---|---|
| `RuntimePort` | 新增 Runtime 投影与 `DrainSubagentContexts` | 所有适配器/测试桩已同步 |
| `ContextActor` | 新增 `SnapshotContext`、`CloseContext` | 保留 `Snapshot`、`Close` |
| `BatchPipeline` | 新增 `FlushContext`、`CloseContext`、`OperationTimeout` | 保留 `Flush`、`Close` |

## 依赖方向

`Application -> RuntimeProjection -> Runtime mailbox -> Application drain`

不存在 Runtime 到 Application 的 callback，也不存在子代理到主会话的直接 `History`/`AppendHistory` 调用。
