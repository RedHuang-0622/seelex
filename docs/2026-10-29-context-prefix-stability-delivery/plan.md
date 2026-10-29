# 上下文前缀稳定性 M2–M5 实现方案

## 设计目标

1. L1 快路径只复用已装配前缀，truth 增长只追加；压缩、冷加载、项目/工具纪元变化显式失效。
2. draft actor 用会话内单写者、FIFO、幂等键和轮尾 ack 保证字节不丢、不乱序。
3. memory 与 tools 的变化走 append-only 纪元表，冷恢复只 fold 已发字节。
4. 节点子代理不再把动态尾部帧排进 WorkingHistory 前面。

## 方案对比

| 维度 | 方案 A：owner 内快照 + 显式 actor | 方案 B：新装配层缓存服务 |
|---|---|---|
| 耦合度 | 低：Context 只消费 task owner 的窄端口 | 高：assembly 将同时理解 transcript、compress 和 cache |
| 内聚性 | 高：L1/draft 与 transcript owner 同生命周期 | 中：缓存生命周期与事实源分离 |
| 可测试性 | 高：纯键、纯 fold、actor 可独立测 | 中：需组装完整应用 |
| 实现成本 | 中 | 高 |
| 改动面 | kernel owner + 少量装配接线 | 跨 application/store/backend |
| 可回滚性 | 高：L1 可删，纪元表为追加字段 | 低：新服务成为路径依赖 |

## 推荐：方案 A

### M2

- 新键由装配出口计算，六格为 system digest、project version、memory epoch、compact seq、tools epoch/digest、retained_from。
- `ContextL1Snapshot` 保存消息、Seq 水位、token 基数和统计；键相同且水位连续时只转换新增事件，否则从 `truth[retained_from:]` 重建。
- 正常请求不再调用 `TranscriptTailWindowBy`；该函数只服务压缩边界选择。
- 删除回合收尾的 `ReleaseWorkingHistory` 热路径。
- 冷加载没有 L1，首轮重建；用同一序列化器比较冷/热字节。

### M3

- 在 `kernel/context/draft` 建立无外部依赖的 actor 队列：入队前分配 seq，`(session,seq)` 幂等，有界背压，FIFO 单写者。
- 每会话 draft 记录轮内尾巴与最后 ack 水位；ack 成功后清工作区并推进水位，失败保留 `uncommitted`。
- 水位随 `SessionRecord.Execution` 持久化；actor 重启由调用方按水位重放。
- 压测覆盖并发、重复投递、队列满、提交失败与崩溃重放。

### M4

- 在会话记录增加 append-only `MemoryUpdate` 行，字段按主设计 §5.2。
- memory 选择只在压缩滚动 epoch 时执行一次；渲染和冷读只按 Seq fold `Bytes`，并校验 Digest。
- 同一 epoch 内 query 变化不改前缀。

### M5

- 会话首次使用时冻结工具 catalog 并稳定排序，计算 `tools_digest`。
- 在会话记录增加 `ToolsUpdate` 行；可用性变化只追加 `⟦toolset⟧` 哨兵，catalog 变化才 bump epoch。
- system 注入固定“最后一块为准”规则；序列化转义定界符；工具门按最后声明拒绝已移除工具。

### 旁支

- 节点子代理的 `InheritedBlocks` 只含 project + skill/compact；plan/task 通过 `InheritedTailBlocks` 追加在 WorkingHistory 后。
- `sessionRecordLocked` 映射续跑 stopped/paused/attempts 字段，并验证重启幂等和 pause 绑定 goal。

## 核心接口

```go
type ContextL1Cache interface {
    Lookup(sessionID string, key prefixprobe.Key) (task_context.ContextL1Snapshot, bool)
    Record(sessionID string, snapshot task_context.ContextL1Snapshot) task_context.ContextL1Stats
}

type DraftActor interface {
    Submit(ctx context.Context, item DraftItem) (Ack, error)
}

type MemoryLedger interface {
    Append(MemoryUpdate) error
    Fold(epoch uint64) ([]byte, error)
}

type ToolsLedger interface {
    Snapshot(epoch uint64) (Catalog, error)
    Append(ToolsUpdate) error
}
```

## 实现顺序

| # | 步骤 | 文件 | 设计模式 |
|---|---|---|---|
| 1 | 先红：L1 增量、冷热一致、性能门禁 | `kernel/context` tests | 契约测试 |
| 2 | L1 + prefix key + truth 直切 | `kernel/context`、`kernel/task` | Snapshot/Cache |
| 3 | 删除每轮 release，修节点尾帧 | conversation/assembly/node | Strategy 接线 |
| 4 | 先红：draft 并发/重复/失败/重放 | `kernel/context/draft` tests | Actor |
| 5 | draft actor + 水位持久化 | draft/task/session record | Single Writer |
| 6 | 先红：memory epoch 字节一致/篡改检测 | seelectx/task/store tests | Event Sourcing |
| 7 | memory 纪元表与 fold | task/store/seelectx | Append-only Ledger |
| 8 | 先红：四类工具可用性事件零失效 | assemble/tool tests | Freeze + Sentinel |
| 9 | tools 纪元、排序、哨兵、门禁 | assemble/tool/task | Catalog Strategy |
| 10 | 文档与全量门禁 | README、arch、devlog | Docs-as-code |

## 测试策略

- M2：重复轮次 L1 hit、压缩显式 miss、冷加载热加载逐字节相等、历史增长不触发全量重切。
- M3：`-race` 并发提交、重复 seq 合并、队列满背压、提交失败不丢、按水位重放。
- M4：同 epoch 换 query 不改字节、跨压缩冷读一致、Digest 篡改报错。
- M5：插件/MCP/goal/permission 事件不改变 tools_digest，仅追加哨兵；catalog 变化 bump epoch；截断/乱序解析报错。
- 全量：`go build ./...`、`go vet ./...`、`go test ./... -count=1 -timeout=120s`、架构预算、race 目标包。

## 回滚

- L1 是派生数据，可删字段/停止复用而不改 truth。
- memory/tools 纪元字段是 JSON 追加字段，旧档读零值。
- draft actor 使用窄端口装配，关闭后回退当前 EnginePort 历史路径。
- 节点尾部块拆分为纯接线调整，回退只恢复块归属，不改 transcript。
