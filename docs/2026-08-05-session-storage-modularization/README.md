# Session Storage v2 设计文档

本目录定义 Session 持久化重构的领域模型、接口边界、物理布局和迁移策略。
当前阶段只冻结设计契约，不修改生产实现。

## 阅读顺序

1. [front-review.md](./front-review.md)：现状、风险和改造范围。
2. [plan.md](./plan.md)：方案比较、实施阶段和验收标准。
3. [architecture.md](./architecture.md)：DDD 聚合、有界上下文、编号模型、写入与恢复协议。
4. [interfaces.md](./interfaces.md)：Application 可依赖的 Go 接口与数据契约。

## 已冻结的决策

- Session 以 `(workspace_id, session_id)` 作为聚合键。
- `RoundNo` 是会话轮次的唯一截断单位，从 1 开始单调递增，取消的轮次也不复用。
- `EventSeq` 是独立 EventStream 的单调递增顺序号；`MessageID` 是 UI 定位键。
- `generation-*`、随机 `segment_id` 只能作为物理句柄，不能承担领域顺序语义。
- `conversation_message`、`events`、Context、Execution 和 Artifact 按领域模块独立读取。
- root manifest 是唯一发布点；已发布 revision 不原地修改。
- Provider history 只是可重建缓存，不是恢复真相源。
- `conversation_message` 保存语义消息与 tool call/result 摘要；`events` 独立保存流式与框架事件。
- 前端历史按 `RoundNo` 滑动窗口读取；模型恢复只注入 system prompt、未完成 stack 顶部和有限消息尾窗。
- 当前上下文没有相关历史时，Agent 才调用内置 `long_term_memory.query`，结果带来源范围并受预算限制。
- 长远记忆是可重建投影：由 round 提交后的 outbox 异步提取、索引，并支持 scope 隔离、撤回和重建。
- v1 只读兼容；首次成功 v2 提交后再发布模块化数据，不删除旧数据。

## 实现前必须通过的评审点

- `RoundNo`/`EventSeq` 的分配是否只发生在 Session 聚合边界。
- Commit 是否具备 expected-revision 和 idempotency 语义。
- 冷读取是否只触碰请求模块，而不是反序列化整个 `state.json`。
- 断电或并发写入时 root manifest 是否只会指向旧 revision 或新 revision。
- v1 无法建立稳定范围时是否显式返回 `ErrLegacyUnindexed`。
