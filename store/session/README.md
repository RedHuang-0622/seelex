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
- `func EmptyMessageHead(sessionID string) MessageHead` — EmptyMessageHead returns the canonical zero head for a session.
- `func AssignRowSeqs(rows []protocol.TranscriptEvent, base uint64, commitID string) ([]protocol.TranscriptEvent, error)` — AssignRowSeqs is the single event seq allocator. Explicit seq <= base is
- `func StructuralEventsCommitID(events []StructuralEvent) string` — StructuralEventsCommitID derives the deterministic EVENT commit identity.
- `func MessageRowsCommitID(rows []protocol.TranscriptEvent) string` — MessageRowsCommitID derives the deterministic transcript commit identity.
- `func ApplyPendingTailHead( head MessageHead, rows []protocol.TranscriptEvent, shards []ShardInfo, total uint64, commitID string, now time.Time, ) MessageHead` — ApplyPendingTailHead builds the head that publishes a validated draft tail.
- `func ApplyCommittedMessageHead( head MessageHead, delta []protocol.TranscriptEvent, commitID string, now time.Time, ) MessageHead` — ApplyCommittedMessageHead updates the published head after message rows were
- `func QueueHeadState(items []QueueItem) QueueState` — QueueHeadState returns the current queue head state.
- `func ValidateCompactFrame(frame CompactFrameRecord, publishedSeq uint64) error` — ValidateCompactFrame rejects frames beyond the published message head.
- `func ValidateForkStart( parentProject, parentSession, childProject, childSession string, watermark, fromSeq uint64, childExists bool, ) error` — ValidateForkStart enforces distinct child, watermark and non-overwrite rules.
- `func PublishMessageHead(layout *storelayout.Layout, scope storelayout.Scope, commitID string, head MessageHead, updatedAt time.Time) (string, error)` — PublishMessageHead publishes the typed message head through layout.
- `func PublishEventHead(layout *storelayout.Layout, scope storelayout.Scope, commitID string, head EventHead, updatedAt time.Time) (string, error)` — PublishEventHead publishes the typed event head through layout.
- `func ClassifyPendingTail(headSeq uint64, rows []protocol.TranscriptEvent, duplicate bool) (string, string)` — ClassifyPendingTail preserves the single draft-tail recoverability rule.
- `func MessageShardName(fromSeq, toSeq uint64) string` — MessageShardName returns the stable sorted-shard filename.
- `func ParseMessageShardName(name string) (uint64, uint64, bool)` — ParseMessageShardName validates and parses a message shard name.
- `func EventShardName(fromID, toID uint64) string` — EventShardName returns the stable event-shard filename.
- `func ParseEventShardName(name string) (uint64, uint64, bool)` — ParseEventShardName validates and parses an event shard name.

### session_test.go

- `func TestPendingTailClassification(t *testing.T)`
- `func TestShardNames(t *testing.T)`
- `func TestAssignRowSeqs(t *testing.T)`
- `func TestMessageShardRouteOnHead(t *testing.T)`
- `func TestStructuralEventIdentity(t *testing.T)`
- `func TestMessageShardReadAppendRepairsTail(t *testing.T)`
- `func TestTypedHeadPublish(t *testing.T)`
- `func TestApplyPendingTailHead(t *testing.T)`
- `func TestListAndDraftBaseSeq(t *testing.T)`
- `func TestSessionStateValidators(t *testing.T)`

### storage.go

- `func DecodeMessageRows(data []byte, onDecode func()) []protocol.TranscriptEvent` — DecodeMessageRows parses JSONL transcript rows, skipping empty rows and an
- `func ReadMessageRowsFile(file *os.File, onDecode func()) ([]protocol.TranscriptEvent, error)` — ReadMessageRowsFile reads all rows from an already-open shard.
- `func ReadMessageRowsAt(path string, offset int64, onDecode func()) ([]protocol.TranscriptEvent, error)` — ReadMessageRowsAt reads a shard from an optional committed-byte boundary.
- `func AppendMessageRows(path string, rows []protocol.TranscriptEvent) error` — AppendMessageRows writes complete JSONL rows after repairing a crash tail.
- `func AppendMessageRowsEx(path string, rows []protocol.TranscriptEvent) (appended []byte, preSize int64, truncated bool, err error)` — AppendMessageRowsEx returns appended bytes, post-truncate pre-size and whether
- `func DecodeStructuralEvents(data []byte) []StructuralEvent` — DecodeStructuralEvents parses EVENT JSONL rows.
- `func ReadStructuralEventsFile(file *os.File) ([]StructuralEvent, error)` — ReadStructuralEventsFile reads all EVENT rows from an open shard.
- `func ReadStructuralEventsAt(path string) ([]StructuralEvent, error)` — ReadStructuralEventsAt reads an EVENT shard, treating absence as empty.
- `func AppendStructuralEvents(path string, rows []StructuralEvent) (int64, error)` — AppendStructuralEvents writes EVENT rows and returns the resulting byte size.
- `func ListMessageShardFiles(dir string) ([]string, error)` — ListMessageShardFiles returns valid message shards ordered by FromSeq.
- `func DraftBaseSeq(messageDir string, headSeq uint64) (uint64, error)` — DraftBaseSeq returns the physical last row seq under a message directory.

