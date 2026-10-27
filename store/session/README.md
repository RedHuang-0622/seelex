# store/session

## 生态位

Session transcript 聚合的领域包。首个 M5 切片拥有 message/event 分片命名和
pending-tail 可恢复性判定；根 `store` 在迁移期保留 head 发布与读写编排适配。

## 职责与非职责

- 职责：稳定分片文件名解析、草稿尾连续性与重复 seq 判定。
- 非职责：文件 IO、head 发布、锁、LLM/tool 语义、UI 投影或 artifact 读写。

## 文件索引

- `session.go`：`PendingTailReport`、pending-tail 状态与分类、message/event 分片命名。
- `session_test.go`：分类和文件名兼容回归。

## 依赖方向

只依赖标准库与 `contract/protocol`；禁止 import 根 `store`、上层或其它 store 领域包。

## 并发与兼容

本包无状态。分片名 `message_<from>_<to>.jsonl` 与
`event_<from>_<to>.jsonl` 是旧数据冷读契约；草稿尾部必须从 `head+1` 连续递增，
重复 seq 或断裂只能报告，不能隐式发布。

## Review 指南

检查状态词表是否只有一份、分片起止是否含端点、零起点/反向区间是否拒绝，以及根包是否
仍保留第二份 `Sscanf`/连续性判据。

## 测试与验证

```text
go test ./store/session ./store -run 'Pending|Draft|Event|Message' -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### session.go

- `func (report PendingTailReport) RowCount() int` — RowCount returns the number of unpublished rows.
- `func ClassifyPendingTail(headSeq uint64, rows []protocol.TranscriptEvent, duplicate bool) (string, string)` — ClassifyPendingTail preserves the single draft-tail recoverability rule.
- `func MessageShardName(fromSeq, toSeq uint64) string` — MessageShardName returns the stable sorted-shard filename.
- `func ParseMessageShardName(name string) (uint64, uint64, bool)` — ParseMessageShardName validates and parses a message shard name.
- `func EventShardName(fromID, toID uint64) string` — EventShardName returns the stable event-shard filename.
- `func ParseEventShardName(name string) (uint64, uint64, bool)` — ParseEventShardName validates and parses an event shard name.

### session_test.go

- `func TestPendingTailClassification(t *testing.T)`
- `func TestShardNames(t *testing.T)`

