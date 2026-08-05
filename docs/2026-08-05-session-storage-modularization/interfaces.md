# Session Storage v2 接口契约

本文档定义调用方看到的模块接口。具体 JSON、SQLite、PostgreSQL、Redis
实现不得把 backend 特有字段泄漏到 Application。

## 基础类型

```go
import (
    "context"
    "errors"
    "time"
)

type SessionKey struct {
    WorkspaceID string
    SessionID   string
}

type Revision struct {
    CommitID string
    Number   uint64
}

type CommitOptions struct {
    // ExpectedRevision 用于乐观并发控制。零值表示仅允许创建新会话。
    ExpectedRevision *Revision
    // IdempotencyKey 由一次逻辑提交生成，重试同一 key 必须返回同一 revision。
    IdempotencyKey string
}

type PageRequest struct {
    Offset int
    Limit  int
}

type RoundRange struct {
    From uint64 // inclusive
    To   uint64 // inclusive
}

type EventRange struct {
    From uint64 // inclusive EventSeq
    To   uint64 // inclusive EventSeq
}

type MessageRange struct {
    FromID string
    ToID   string
}

type Page[T any] struct {
    Items    []T
    Offset   int
    Total    int
    Revision Revision
    HasMore  bool
}

type SessionMetadata struct {
    WorkspaceID string
    SessionID   string
    Title       string
    CreatedAt   time.Time
    UpdatedAt   time.Time
    Revision    Revision
}

type ConversationMessage struct {
    ID        string
    RoundNo   uint64
    Role      string
    Content   string
    CreatedAt time.Time
}

type ConversationRound struct {
    RoundNo  uint64
    Status   string // pending, completed, interrupted, failed
    Messages []ConversationMessage
}

type EventRecord struct {
    EventID   string
    EventSeq  uint64
    RoundNo   uint64
    MessageID string
    CallID    string
    Kind      string
    PayloadRef string
    CreatedAt time.Time
}

type EventWindow struct {
    Events       []EventRecord
    EventFrom    uint64
    EventTo      uint64
    Revision     Revision
    HasMore      bool
}

type SnapshotRevision struct {
    Metadata   Revision
    Conversation Revision
    Events      Revision
    Context     Revision
    Execution   Revision
}

// 以下 DTO 只表达跨模块契约；具体业务字段由对应 bounded context 扩展。
type ConversationSnapshot struct {
    Rounds   []ConversationRound
    Revision Revision
}

type EventSnapshot struct {
    Events   []EventRecord
    Revision Revision
}

type ExecutionSnapshot struct {
    Projection  []byte
    Checkpoints []byte
    Revision    Revision
}

type ContextSnapshot struct {
    SystemPrompt []byte
    PlanStack    []byte
    TaskStack    []byte
    SkillStack   []byte
    CompactStack []byte
    Revision     Revision
}

type ProviderCacheSnapshot struct {
    Units    []byte
    Revision Revision
}

type ToolArtifact struct {
    Ref       string
    Digest    string
    SizeBytes int64
}

type ToolArtifactPage struct {
    Ref      string
    Data     []byte
    Offset   int
    Total    int
    HasMore  bool
}

type EvidenceRef struct {
    Kind string
    Ref  string
}

type StackSnapshot struct {
    Plan, Task, Skill, Compact []byte
}

type LegacySessionRecord struct {
    State []byte
}
```

## 聚合写入接口

```go
type SessionAggregateWriter interface {
    Commit(ctx context.Context, key SessionKey, snapshot AggregateSnapshot, opts CommitOptions) (Revision, error)
    Delete(ctx context.Context, key SessionKey) error
}

type AggregateSnapshot struct {
    Metadata        SessionMetadata
    Conversation    ConversationSnapshot
    Events          EventSnapshot
    Execution       ExecutionSnapshot
    Context         ContextSnapshot
    ProviderCache   ProviderCacheSnapshot // optional/rebuildable
    Events          EventSnapshot
    ToolArtifacts   []ToolArtifact
    // NextRoundNo/NextEventSeq 是聚合提交后的游标，不是随机 ID。
    // Writer 必须校验它们不小于当前 manifest 游标。
    NextRoundNo      uint64
    NextEventSeq     uint64
    ModuleRevisions  SnapshotRevision
}

// RoundAllocator 只属于 Session 聚合，不应由 JSON/SQL/Redis backend 自行生成。
// 生产实现可以把它作为聚合内部方法，而不是暴露给 UI 或 Provider。
type RoundAllocator interface {
    BeginRound() (roundNo uint64, err error)
    AppendEvent(roundNo uint64, event EventRecord) (eventSeq uint64, err error)
    CloseRound(roundNo uint64, status string) error
}

// SessionAggregate 是领域层的最小写入边界，不暴露 JSON、SQL 或 Redis 细节。
type SessionAggregate interface {
    Key() SessionKey
    Revision() Revision
    BeginRound() (roundNo uint64, err error)
    AppendMessage(roundNo uint64, message ConversationMessage) error
    AppendEvent(roundNo uint64, event EventRecord) (eventSeq uint64, err error)
    CloseRound(roundNo uint64, status string) error
    Snapshot() AggregateSnapshot // 纯内存副本，不执行 IO
}

// SessionAggregateRepository 负责加载恢复所需的有限窗口并发布新 revision。
// UI 历史分页不应调用该接口，而应直接使用模块 reader。
type SessionAggregateRepository interface {
    LoadForResume(ctx context.Context, key SessionKey, opts ResumeOptions) (SessionAggregate, error)
    Commit(ctx context.Context, aggregate SessionAggregate, opts CommitOptions) (Revision, error)
}

type ResumeOptions struct {
    ConversationTokenBudget int
    EventTailMax             int
    ConversationTailRounds  int
}

// SessionModuleReader 是查询侧组合接口；各模块仍可独立实现和测试。
type SessionModuleReader interface {
    SessionCatalogReader
    ConversationReader
    ConversationWindowReader
    EventReader
    ContextReader
    ContextResumeReader
    ArtifactReader
}
```

`Commit` 是唯一允许同时发布多个模块 revision 的入口。应用层不得继续
通过 `State []byte` 拼接一个新的领域大 blob 作为 v2 写入。

## 会话列表与 metadata

```go
type SessionCatalogReader interface {
    ListSessions(ctx context.Context, workspaceID string, page PageRequest) (Page[SessionMetadata], error)
    ReadMetadata(ctx context.Context, key SessionKey) (SessionMetadata, error)
}
```

列表查询只读取 manifest/metadata，不读取 conversation_message、events 或
ToolResult。

## Conversation 模块

```go
type ConversationReader interface {
    ReadConversationTail(ctx context.Context, key SessionKey, limitRounds int) (Page[ConversationRound], error)
    ReadConversationRounds(ctx context.Context, key SessionKey, span RoundRange) (Page[ConversationRound], error)
    ReadConversationAround(ctx context.Context, key SessionKey, messageID string, before, after int) (Page[ConversationRound], error)
}
```

实现必须按 RoundNo 索引读取，不能先读完整 SessionRecord 再切片。分页
返回的是完整 `ConversationRound`，不能截断一个 round 的 tool call/result
配对。

前端滑动窗口使用稳定游标，不使用会话当前长度推导 offset：

```go
type ConversationWindowRequest struct {
    BeforeRound *uint64
    AfterRound  *uint64
    MaxRounds   int
    MaxBytes    int
    Revision    *Revision
}

type ConversationWindow struct {
    Rounds       []ConversationRound
    BeforeRound  *uint64
    AfterRound   *uint64
    Revision     Revision
    HasBefore    bool
    HasAfter     bool
}

type ConversationWindowReader interface {
    ReadConversationWindow(ctx context.Context, key SessionKey, req ConversationWindowRequest) (ConversationWindow, error)
}
```

窗口实现必须保证一个 round 内的 user、assistant、tool call/result 成对返回；
被淘汰的前端页面可以用 `BeforeRound` 再次冷读取。

## Event 模块

```go
type EventReader interface {
    ReadEventTail(ctx context.Context, key SessionKey, maxEvents int) (EventWindow, error)
    ReadEventRange(ctx context.Context, key SessionKey, span EventRange) ([]EventRecord, error)
    ReadEventRoundRange(ctx context.Context, key SessionKey, span RoundRange) ([]EventRecord, error)
}
```

Event tail 只返回有界事件窗口；范围读取必须保持 EventSeq 连续性。工具调用和
结果的语义配对由 conversation_message 的 `CallID` 完成，Event 不承担恢复真相源。

## Context 模块

```go
type ContextReader interface {
    ReadContext(ctx context.Context, key SessionKey) (ContextSnapshot, error)
    ReadCompactFrame(ctx context.Context, key SessionKey, segmentID string) (CompactFrame, error)
    ReadCompactFrames(ctx context.Context, key SessionKey, span RoundRange) ([]CompactFrame, error)
}

type ContextWriter interface {
    AppendCompactFrame(ctx context.Context, key SessionKey, frame CompactFrame) (Revision, error)
    ReplaceStacks(ctx context.Context, key SessionKey, stacks StackSnapshot) (Revision, error)
}
```

上下文恢复只返回 system prompt 和各 stack 的有效栈顶：

```go
type StackFrame struct {
    ID            string
    Kind          string
    Status        string // pending/running/blocked/waiting_input/completed/...
    CreatedRound  uint64
    UpdatedRound  uint64
    Payload       []byte
    Revision      Revision
}

type ContextResume struct {
    SystemPrompt []byte
    PlanTop      *StackFrame
    TaskTop      *StackFrame
    SkillTop     *StackFrame
    CompactTop   *CompactFrame
    Snapshot     ContextSnapshot
}

type ContextResumeReader interface {
    ReadContextForResume(ctx context.Context, key SessionKey, currentRound uint64) (ContextResume, error)
}
```

`ReadContextForResume` 默认过滤已完成、已取消和已失败的 stack frame；
`systemPrompt` 始终返回。Compact frame 还必须通过 conversation/event revision
和覆盖范围校验后才能返回。

## 长远记忆查询工具

长远记忆查询是内置工具，不依赖外部 MCP 发现；它只在当前上下文缺少相关历史
且请求确实需要历史证据时触发。

```go
type MemoryScope string

const (
    MemoryScopeSession  MemoryScope = "session"
    MemoryScopeProject  MemoryScope = "project"
    MemoryScopeWorkspace MemoryScope = "workspace"
)

type MemoryQueryRequest struct {
    Key         SessionKey
    Query       string
    Scope       MemoryScope
    CurrentRound uint64
    MaxResults  int
    TokenBudget int
    Cursor      string
}

type MemoryHit struct {
    MemoryID       string
    Snippet        string
    Score          float64
    SourceModule   string
    MessageFrom    string
    MessageTo      string
    RoundFrom      uint64
    RoundTo        uint64
    EventFrom      uint64
    EventTo        uint64
    SourceRevision Revision
}

type MemoryQueryResult struct {
    Hits       []MemoryHit
    Cursor     string
    Revision   Revision
    HasMore    bool
}

type LongTermMemoryQueryTool interface {
    Query(ctx context.Context, req MemoryQueryRequest) (MemoryQueryResult, error)
}

type MemoryNeedDetector interface {
    NeedsQuery(currentInput string, context ContextResume) bool
}

type MemoryStatus string

const (
    MemoryActive  MemoryStatus = "active"
    MemoryStale   MemoryStatus = "stale"
    MemoryRetired MemoryStatus = "retired"
)

type MemoryEntry struct {
    MemoryID        string
    Scope           MemoryScope
    Summary         string
    SourceSessionID string
    SourceRoundFrom uint64
    SourceRoundTo   uint64
    SourceEventFrom uint64
    SourceEventTo   uint64
    SourceRevision  Revision
    Fingerprint     string
    Status          MemoryStatus
    ExpiresAt       *time.Time
    UpdatedAt       time.Time
}

type MemoryWriter interface {
    Upsert(ctx context.Context, entry MemoryEntry) error
    RetireBySource(ctx context.Context, sourceSessionID string, sourceRevision Revision) error
    RetireScope(ctx context.Context, scope MemoryScope, scopeID string) error
}

type MemoryIndexer interface {
    Index(ctx context.Context, entry MemoryEntry) error
    Remove(ctx context.Context, memoryID string) error
    Rebuild(ctx context.Context, key SessionKey) error
}

type MemoryOutboxPublisher interface {
    PublishMemoryCandidate(ctx context.Context, key SessionKey, sourceRevision Revision, round RoundRange) error
}
```

查询结果必须经过 token/byte budget 截断；完整命中列表通过 `ToolArtifactRef`
保存，注入模型的只允许是带来源范围的 bounded `Snippet`。查询工具不得递归调用
自身；`MemoryNeedDetector` 应保持纯内存判断，不得在 `service.mu` 临界区内执行
查询或 backend IO。

`CompactFrame` 至少包含：

```go
type CompactFrame struct {
    SegmentID             string
    RoundFrom             uint64
    RoundTo               uint64
    EventFrom             uint64
    EventTo               uint64
    MessageFrom           string
    MessageTo             string
    EventRevision         Revision
    ConversationRevision  Revision
    Summary               string
    Evidence              []EvidenceRef
}
```

`RoundFrom/RoundTo` 和 `EventFrom/EventTo` 是恢复与审计的正式范围；旧的
Provider unit index 只能保留为兼容字段。读取方必须校验 `From <= To`、范围
属于同一 session，且引用的 revision 仍被 root manifest 发布。

## Artifact 与事件引用

```go
type ArtifactReader interface {
    ReadToolResult(ctx context.Context, key SessionKey, ref string, page PageRequest) (ToolArtifactPage, error)
}

```

Tool artifact 通过 immutable ref 读取，conversation_message 和 events 只保存
ref、digest、size 和范围，不复制大正文。events 使用独立的 EventSeq，不得再
写入 conversation_message 的 shard。

## v1 兼容适配

旧的 `LoadSessionRecord`、`LoadHistory` 和 `SaveCommit` 接口继续保留在
compat adapter 中：

```go
type LegacySessionReader interface {
    LoadSessionRecord(ctx context.Context, key SessionKey) (LegacySessionRecord, error)
}
```

compat adapter 只负责：v1 解码、RoundNo 推导、模块化 DTO 转换。新业务
代码不得直接依赖它。无法推导稳定范围时返回
`ErrLegacyUnindexed`，而不是按随机字符或当前时间拼接编号。

## 错误与超时契约

```go
var (
    ErrSessionNotFound  = errors.New("session not found")
    ErrLegacyUnindexed  = errors.New("legacy session has no stable range index")
    ErrModuleCorrupt    = errors.New("session module is corrupt")
    ErrRevisionMismatch = errors.New("module revision mismatch")
    ErrCommitConflict   = errors.New("session commit conflict")
    ErrInvalidRange     = errors.New("invalid session range")
    ErrArtifactNotFound = errors.New("tool artifact not found")
    ErrMemoryUnavailable = errors.New("long-term memory unavailable")
)
```

调用方使用 `errors.Is` 判断这些错误，不依赖 backend 的字符串内容。

- 所有接口接收 `context.Context`；backend 不得在内部替换为
  `context.Background()`。
- `ErrSessionNotFound`、`ErrLegacyUnindexed`、`ErrModuleCorrupt`、
  `ErrRevisionMismatch`、`ErrCommitConflict`、`ErrInvalidRange`、
  `ErrArtifactNotFound`、`ErrMemoryUnavailable` 必须保持可识别。
- 读接口返回模块缺失错误时，调用方按降级矩阵处理；不能把损坏的
  conversation_message/events 当作空切片。
- 所有 `Read*`/`Commit` 调用都必须服从上层传入的 context deadline；backend
  不得无限等待文件锁、网络连接或 mailbox。
