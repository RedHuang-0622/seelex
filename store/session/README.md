# store/session

## 生态位

Session transcript 聚合的领域包。首个 M5 切片拥有 message/event 分片命名和
pending-tail 可恢复性判定；根 `store` 在迁移期保留 head 发布与读写编排适配。

## 职责与非职责

- 职责：稳定分片文件名解析、草稿尾连续性与重复 seq 判定。
- 非职责：文件 IO、head 发布、锁、LLM/tool 语义、UI 投影或 artifact 读写。

## 文件索引

- `session.go`：`PendingTailReport`、pending-tail 状态与分类、message/event 分片命名。
- `commit.go`：原子会话提交 payload 的唯一实际 struct；根 `store.Commit` 仅保留 alias。
- `event_identity.go`：从 Seele framework event 的 `agent.runtime` 定位读取会话归属；
  事件持久化与观测查询共用这一份判据。
- `session_not_found.go`：`IsSessionNotFound` 的唯一实现；根 `store` 只保留同身份变量 alias。
- `session_test.go`：分类和文件名兼容回归。

## 依赖方向

只依赖标准库、Seele event、`contract/protocol` 与 `store/internal/layout`；
禁止 import 根 `store`、上层或其它 store 领域包。

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

### event_identity.go

- `func SessionIDFromEvent(event frameworkevent.Event) string` — SessionIDFromEvent returns the session identity carried by the framework
- `func SessionIDFromLocations(locations []frameworkevent.Location) string` — SessionIDFromLocations is the location-slice form of SessionIDFromEvent.

### event_kind.go

- `func EventKindOf(event protocol.TranscriptEvent) string` — EventKindOf 返回事件的显式类别；旧数据 Kind 为空时按 Role/ToolCalls 回退。

### event_kind_test.go

- `func TestEventKindOfExplicitWins(t *testing.T)` — TestEventKindOfExplicitWins 钉住"显式类别优先"：Kind 非空时不得被 Role 回退判定覆盖。
- `func TestEventKindOfFallback(t *testing.T)` — TestEventKindOfFallback 钉住旧数据（Kind 为空）的回退判据：
- `func TestEventKindStringsAreStable(t *testing.T)` — TestEventKindStringsAreStable 钉住字面值：词表住在 contract/protocol，判据不得自己造串。

### session.go

- `func (report PendingTailReport) RowCount() int` — RowCount returns the number of unpublished rows.
- `func EmptyMessageHead(sessionID string) MessageHead` — EmptyMessageHead returns the canonical zero head for a session.
- `func AssignRowSeqs(rows []protocol.TranscriptEvent, base uint64, commitID string) ([]protocol.TranscriptEvent, error)` — AssignRowSeqs validates runtime-assigned event identities before persistence.
- `func StructuralEventsCommitID(events []StructuralEvent) string` — StructuralEventsCommitID derives the deterministic EVENT commit identity.
- `func MessageRowsCommitID(rows []protocol.TranscriptEvent) string` — MessageRowsCommitID derives the deterministic transcript commit identity.
- `func ApplyPendingTailHead( head MessageHead, rows []protocol.TranscriptEvent, shards []ShardInfo, total uint64, commitID string, now time.Time, ) MessageHead` — ApplyPendingTailHead builds the head that publishes a validated draft tail.
- `func ApplyCommittedMessageHead( head MessageHead, delta []protocol.TranscriptEvent, commitID string, now time.Time, ) MessageHead` — ApplyCommittedMessageHead updates the published head after message rows were
- `func QueueHeadState(items []QueueRow) QueueState` — QueueHeadState returns the current queue head state.
- `func ValidateCompactFrame(frame CompactFrameRecord, publishedSeq uint64) error` — ValidateCompactFrame rejects frames beyond the published message head.
- `func ValidateForkStart( parentProject, parentSession, childProject, childSession string, watermark, fromSeq uint64, childExists bool, ) error` — ValidateForkStart enforces distinct child, watermark and non-overwrite rules.
- `func ValidateRetentionPrefix(remaining []protocol.TranscriptEvent, upToSeq, lastSeq uint64) error` — ValidateRetentionPrefix rejects deleting a non-contiguous published prefix.
- `func NextQueueSeq(items []QueueRow) uint64` — NextQueueSeq returns the next stable queue sequence.
- `func NewQueueRow(state LifecycleState, requestID, content string, now time.Time) QueueRow` — NewQueueRow creates one queued row with deterministic identity.
- `func Enqueue(state LifecycleState, requestID, content string, now time.Time) (LifecycleState, bool)` — Enqueue appends one item unless the same requestID is already queued.
- `func SaveDraft(state LifecycleState, sessionID, content string, now time.Time) LifecycleState` — SaveDraft installs a normal unsent draft.
- `func MoveDraftToQueue(state LifecycleState, sessionID, content string, now time.Time) LifecycleState` — MoveDraftToQueue atomically clears draft and appends the queued copy.
- `func DirectSendState(state LifecycleState, content string, now time.Time) (LifecycleState, bool)` — DirectSendState clears draft and appends only when an older queue item exists.
- `func SendFront(state LifecycleState) (LifecycleState, QueueRow, error)` — SendFront transitions the FIFO head to sending.
- `func ConfirmFront(state LifecycleState, requestID string) (LifecycleState, error)` — ConfirmFront removes the head after a published send.
- `func FailFront(state LifecycleState, sessionID string, now time.Time) (LifecycleState, error)` — FailFront removes the head and returns its content to draft.
- `func FailDirect(state LifecycleState, sessionID, content string, now time.Time) LifecycleState` — FailDirect returns a directly-sent content back to draft.
- `func PublishMessageHead(layout *storelayout.Layout, scope storelayout.Scope, commitID string, head MessageHead, updatedAt time.Time) (string, error)` — PublishMessageHead publishes the typed message head through layout.
- `func PublishEventHead(layout *storelayout.Layout, scope storelayout.Scope, commitID string, head EventHead, updatedAt time.Time) (string, error)` — PublishEventHead publishes the typed event head through layout.
- `func ClassifyPendingTail(headSeq uint64, rows []protocol.TranscriptEvent, duplicate bool) (string, string)` — ClassifyPendingTail preserves the single draft-tail recoverability rule.
- `func MessageShardName(fromSeq, toSeq uint64) string` — MessageShardName returns the stable sorted-shard filename.
- `func ParseMessageShardName(name string) (uint64, uint64, bool)` — ParseMessageShardName validates and parses a message shard name.
- `func EventShardName(fromID, toID uint64) string` — EventShardName returns the stable event-shard filename.
- `func ParseEventShardName(name string) (uint64, uint64, bool)` — ParseEventShardName validates and parses an event shard name.

### session_not_found.go

- `func IsSessionNotFound(err error) bool` — IsSessionNotFound is the single missing-session criterion. The JSON v8

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
- `func TestLifecycleQueueTransitions(t *testing.T)`

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
