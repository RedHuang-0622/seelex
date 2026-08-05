# Session 存储模块化重构方案

## 设计目标

将会话持久化从“技术对象分片 + opaque `State []byte`”改为“领域模块分片 + root manifest 原子提交”，同时保留旧布局的只读兼容能力。

DDD 约束：Session 是聚合根，ConversationRound 是有序子实体。每个会话使用单调递增的 `RoundNo`（从 1 开始），独立 EventStream 使用单调递增的 `EventSeq`。随机 generation/segment 字符串只能作为物理存储句柄，不能承担会话截断或恢复语义。

目标模块：

- `metadata`：session ID、标题、workspace、revision、时间。
- `conversation_message`：语义消息及 tool call/result 摘要，按完整 RoundNo 分片。
- `events`：独立 append-only 事件流，支持 EventSeq 范围读取和流式/框架诊断。
- `provider-cache`：可丢失的 provider working history。
- `execution`：task projection、checkpoint、continuation、read-file provenance。
- `context`：system prompt、Plan/Task/Skill/Compact stack。
- `tool-results`：不可变大结果和压缩原文。

## 方案对比

| 维度 | 方案 A：模块 manifest + immutable generation | 方案 B：保留 Commit.State，增加模块读取接口 |
|---|---|---|
| 领域边界 | 清晰，每个模块有 owner 和 revision | 仍然依赖大 State blob |
| 长会话读取 | 直接读取 conversation_message/events shard | 仍需解析完整 State |
| 原子性 | root manifest 一次切换所有模块版本 | 各模块难以表达一致 revision |
| 迁移成本 | 中等，需要 v1 reader + 首次写迁移 | 低，但只能缓解接口问题 |
| 跨 backend 一致性 | 可用统一 ModuleManifest contract tests | JSON/SQL/Redis 继续语义分叉 |
| 回滚 | 保留旧 generation，manifest 回退 | State blob 与其他模块难以回退 |

推荐方案 A。方案 B 只能作为过渡兼容层，不能作为最终布局。

## 核心接口

接口定义在调用方可见的 `sessionstore` contract 中，完整草案见
[interfaces.md](./interfaces.md)。具体 JSON、SQLite、Redis backend 通过
adapter 实现，不向 Application 泄漏 backend 字段。

第一阶段先落地 `ConversationReader` 和 `EventReader` 的兼容适配；
CompactFrame 的新索引字段与 context 独立存储在第二阶段完成。所有写入通过
`SessionAggregateRepository.Commit`，并携带 `ExpectedRevision` 与
`IdempotencyKey`，避免并发重试重复分配顺序号。

## 写入协议

1. 从 Service 锁内只复制各模块内存快照，不做外部 IO。
2. backend 为变化模块创建新的 immutable shard/generation。
3. 校验模块 manifest、范围索引、引用完整性。
4. 原子替换 root `manifest.json`，写入 layout version、commit ID、各模块 revision。
5. 旧 generation 延迟清理，不影响读者继续使用旧 manifest。

单模块直接更新（例如 CompactStack 或 event append）必须创建该模块的新 revision，并更新 root manifest；禁止原地修改已发布 generation 中的 `state.json`。

## 恢复协议

1. 先读取 root manifest。
2. 并行读取 metadata、conversation_message 尾窗、context、execution projection；
   只有需要流式重放或诊断时才读取有界 events 尾窗。
3. 校验所有模块 revision 属于同一 commit；失败时按模块降级，不把损坏模块静默成空历史。
4. UI 只读取 conversation range。
5. Provider 只使用 conversation_message 尾窗 + compact stack + 当前未完成 user unit。
6. provider-cache 丢失时从 conversation_message/context 重建，不阻塞会话恢复。

前端滑动窗口与上下文恢复分成两条查询路径：

- 前端窗口：按 `RoundNo` 使用 `BeforeRound/AfterRound` 游标分页，保证完整 round
  和 tool call/result 配对；淘汰页只释放内存，不删除持久化 shard。
- 上下文恢复：始终加载 `system.json`；Plan/Task/Skill 只加载各自栈顶且状态为
  `pending/running/blocked/waiting_input` 的 frame；CompactStack 只加载覆盖当前
  round 且 revision 仍有效的 frame；最近未完成 tool call 需要时才读取对应 artifact
  和有限 EventSeq 范围。
- 长远记忆兜底：若 Provider cache 缺失，先从 conversation_message/context 重建；
  若当前上下文确实没有与请求相关的历史，再调用内置
  `long_term_memory.query`，按 session → project → workspace 限定范围，并把命中
  的来源 ID、轮次范围和 revision 一并带入上下文。
- 长远记忆写入：round 提交后通过 outbox 异步提取候选 `MemoryEntry`，按
  `source_revision + fingerprint` 幂等写入并建立索引；会话/project/workspace
  删除或撤回时同步标记 stale/retired，索引可从 conversation_message 重建。

## 兼容迁移

- `layout_version=1`：保留现有 `manifest.json`、`generation-*`、`state.json` 读取器。
- 首次成功写入 v2：把旧 `SessionRecord` 拆到 metadata、conversation_message、execution、context 模块；旧 provider history、legacy transcript、tool results 和 framework events 建立引用。
- 新 manifest 提交成功前不删除旧数据。
- v2 读取失败时只回退同一会话的 v1 generation，不能跨 session 或跨 workspace 回退。

## 实施步骤

| 阶段 | 文件/模块 | 交付 |
|---|---|---|
| 1 | `sessionstore` JSON + `application_adapters.go` | conversation_message/events 独立读取接口；v1 fallback；修复 event generation 丢失路径 |
| 2 | `SessionRecord`/`EventRecord`/`CompactFrame` | 稳定 message/event range、session ID 和 event revision |
| 3 | `SessionContextStore` + Runtime | context 四 stack 独立持久化并接入 Resume |
| 4 | SQLite/Redis | 对齐 v2 module contract 和事务边界 |
| 5 | Application resume/UI | 直接 select 会话模块，加入前端滑动窗口、ContextResume 和长远记忆查询兜底，移除完整 State blob 的默认读取 |
| 6 | Memory projection/index | 实现 outbox、MemoryExtractor、MemoryWriter/Indexer、scope 权限、失效和重建 |

## 验收与回滚

- 每阶段先运行对应 package unit tests，再跑 integration smoke。
- JSON contract 必须覆盖两次 Commit 后 event 不丢、旧布局可读、范围读取不解析无关模块、断电时 manifest 二选一。
- Memory contract 必须覆盖“无相关上下文才查询”、结果有界截断、来源范围可追溯、
  查询工具不可递归和 memory backend 不可用时显式降级。
- Memory lifecycle contract 必须覆盖提交后最终一致、重复 outbox 不重复写入、
  删除/撤回后的不可见性、scope 隔离和索引重建。
- 回滚只需停止写 v2 并继续使用 v1 reader；不得删除旧 generation。
