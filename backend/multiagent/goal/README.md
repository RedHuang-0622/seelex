# goal

## 生态位

`backend/multiagent/goal/` 是**目标栈与收口审计**的领域实现：一个会话可以压入多层
目标（嵌套目标压栈、下层转 paused），主代理用 `goal_begin/goal_update/goal_done`
推进与收口。主要调用方：`application/core` 的 goal 协调器（按会话持有一份
`Controller`）、`wiring/registry`（goal 工具注册）与 TUI/GUI 的目标看板。

**TL/ADVISOR 评审环已删除（2026-10-17 用户裁决）**：不再有 b 回合、终态 gate、
TL 指令队列或评审过程投影；团队编排由 `backend/multiagent/teamwork` 承担。goal
只负责"目标是什么、进展如何、何时收口"。

## 职责与非职责

- 做：LIFO 目标栈、状态迁移（active/paused/completed/failed/aborted）、进度打点、
  收口审计账本、看板存档（`board_archive.go`）。
- 不做：不驱动 LLM、不跑评审、不装配团队、不写会话正文。

## 目录与文件

| 文件 | 职责 |
|---|---|
| `controller.go` | `Controller`：栈操作与状态迁移的唯一实现 |
| `record.go` | `GoalRecord` / `Budget` / `Status` 等同身份 alias；数据形状事实源在 `contract/dto/goal.go`，行为保留为域自由函数 |
| `store.go` / `sessionstore_store.go` | 持久化端口与会话上下文存储适配 |
| `audit.go` | 收口审计端口与条目（append-only 账本） |
| `stack.go` | 栈投影 |
| `board_archive.go` | 目标看板存档（active/closed、恢复、节流） |

## 核心实现

- `Controller.Begin/Update/Finish/Abort/Reload/Status` 是状态机唯一入口；
  每次成功迁移追加一条审计（装配了 `AuditAccount` 时）。
- `Status` 是栈底→栈顶的全量只读视图；栈顶即 active，下层 paused。
- `StatusOfRecord` 把持久记录里的状态词读回枚举（未知词不猜）。
- `CloneGoalRecord` / `GoalUpdateChangesDefinition` 承接原方法行为，DTO 本身保持纯数据。

## 数据流

工具/门面 → `application/core` goal 协调器 → `Controller`（内存栈 + 审计）→
`ContextStateStore`（会话第五栈）→ 看板存档 → 运行时投影（`GoalGovernanceView`）。

## 依赖方向

`goal → contract/dto + store`（+ stdlib）。禁止 import `application/*`、
`backend/engine/*`。工具层只经 application 门面调用，不直接驱动 Controller。

## 并发与存储

`Controller` 内部一把互斥锁保护栈与审计；持久化经注入的 `Store`/`Audit` 端口，
未装配时退化为内存态（进程内）。审计文本字段有 rune 上限。

## 扩展方式

新增状态：先扩 `Status` 词表与 `StatusOfRecord`，再改迁移表与审计；
新增持久面：在 `Store` 端口加方法，由 `ContextStateStore` 适配。

## Review 指南

- 状态迁移是否只经 `Controller`（不许在调用方改 `record.Status`）；
- 终态是否同时弹栈并落审计；
- 是否又长回了"评审/团队装配"职责（那属于 teamwork，不属于 goal）。

## 测试与验证

```text
go test ./backend/multiagent/goal/ -count=1
go test ./application/core/ -run Goal -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### audit.go

- `func (e AuditEntry) normalized() AuditEntry` — normalized 返回文本字段有界的副本。
- `func truncateAuditText(value string) string`
- `func (s *ContextStateStore) AppendGoalAudit(ctx context.Context, entry AuditEntry) error` — AppendGoalAudit 实现 AuditAccount：映射为 store GoalAuditEntry 后

### audit_test.go

- `func TestControllerAuditAppendOnlyLifecycle(t *testing.T)` — TestControllerAuditAppendOnlyLifecycle 验证 Controller 自动审计：
- `func TestControllerAuditNestedRestoreAndTerminal(t *testing.T)` — TestControllerAuditNestedRestoreAndTerminal 验证嵌套治理审计：子 finish
- `func TestAuditSourceSessionProvenanceRoundTrip(t *testing.T)` — TestAuditSourceSessionProvenanceRoundTrip 验证"用户在其它会话完成 goal"
- `func TestAuditPerSessionIsolation(t *testing.T)` — TestAuditPerSessionIsolation 验证审计按会话隔离：两会话各自审计独立

### board_archive.go

- `func GoalGovernanceViewFromBoardArchive(archive store.GoalBoardMeta) (dto.GoalGovernanceView, bool)` — GoalGovernanceViewFromBoardArchive converts a persisted goal board into the
- `func goalBoardHistoryViews(entries []store.GoalBoardHistory) []dto.GoalHistoryView`
- `func goalBoardProgressViews(items []store.GoalProgress) []dto.GoalProgressView`
- `func isTerminalAuditKind(kind string) bool` — isTerminalAuditKind 报告审计条目是否 goal 终态（收口账本的派生来源）。
- `func (s *ContextStateStore) auditLedger() []store.GoalAuditEntry` — auditLedger 返回本会话的 goal 审计账本（未装配会话存储 → nil）。
- `func (s *ContextStateStore) refreshGoalBoard(ctx context.Context, records []*GoalRecord) error` — refreshGoalBoard 组装并（按指纹节流后）写入 goal 看板存档。
- `func (s *ContextStateStore) refreshBoardAfterAudit(ctx context.Context, entry AuditEntry) error` — refreshBoardAfterAudit 在审计追加成功后刷新看板：只有终态条目
- `func reportBoardWriteFailure(state string, err error) error` — reportBoardWriteFailure 决定一次看板存档失败是否上报（§7 的显式口径）。
- `func planGoalBoard( prev store.GoalBoardMeta, active *store.GoalBoardActive, audit []store.GoalAuditEntry, now time.Time, ) (store.GoalBoardMeta, bool)` — planGoalBoard 组装看板载荷并判断是否需要写盘（指纹节流）。
- `func mergeBoardHistory( prev store.GoalBoardMeta, active *store.GoalBoardActive, audit []store.GoalAuditEntry, ) []store.GoalBoardHistory` — mergeBoardHistory 计算收口账本的**并集**：存档已有条目 ∪ 由审计终态条目
- `func closedInfo(prev store.GoalBoardMeta, audit []store.GoalAuditEntry, now time.Time) (int64, string)` — closedInfo 返回关闭时间（unix 秒）与原因：权威来源是**刚离开活动栈那个 goal**
- `func terminalAuditFor(audit []store.GoalAuditEntry, goalID string) (store.GoalAuditEntry, bool)` — terminalAuditFor 在审计账本里找终态条目：优先 goalID 匹配的那条，否则退回
- `func activeFrame(records []*GoalRecord) *store.GoalBoardActive` — activeFrame 把栈顶（records 末元素）投影成看板当前帧快照；空栈 → nil。
- `func boardFingerprint(meta store.GoalBoardMeta) string` — boardFingerprint 计算看板载荷的内容指纹：对"去掉 Seq/UpdatedAt/Fingerprint"

### board_archive_boundary_test.go

- `func TestGoalBoardArchiveStaysOutOfDecisionFiles(t *testing.T)`

### board_archive_test.go

- `func newBoardArchiveRouter(t *testing.T) *store.Router` — newBoardArchiveRouter 建一个临时会话存储（看板存档落在会话目录的 metadata 下）。
- `func newBoardArchiveStore(t *testing.T, router *store.Router, sessionID string) ( *ContextStateStore, store.SessionBoards, *store.SessionContextStore, )` — newBoardArchiveStore 装配"会话存储 + 看板存档面"的 goal 域适配器。
- `func readBoardArchive(t *testing.T, boards store.SessionBoards) store.GoalBoardMeta` — readBoardArchive 读回看板存档（读取失败直接失败：这些用例都先写过）。
- `func historyFor(meta store.GoalBoardMeta, goalID string) int` — historyFor 返回账本里某个 goal 的条目条数（"不会重复进 history"的判据）。
- `func TestGoalBoardArchiveSeparatesActiveFrameFromHistory(t *testing.T)` — TestGoalBoardArchiveSeparatesActiveFrameFromHistory 验证 active/history 分离：
- `func TestGoalBoardArchiveHistoryIsAppendOnlyPerGoal(t *testing.T)` — TestGoalBoardArchiveHistoryIsAppendOnlyPerGoal 验证账本**只追加不重写**：收口后
- `func TestGoalBoardArchiveSurvivesColdRead(t *testing.T)` — TestGoalBoardArchiveSurvivesColdRead 验证**重启快照**语义：换一个会话存储/
- `func (r *failingBoardRepository) WriteGoalBoard(context.Context, store.GoalBoardMeta) error`
- `func (r *failingBoardRepository) ReadGoalBoard(context.Context) (store.GoalBoardMeta, error)`
- `func (r *failingBoardRepository) WriteTeamBoard(context.Context, store.TeamBoardMeta) error`
- `func (r *failingBoardRepository) ReadTeamBoard(context.Context) (store.TeamBoardMeta, error)`
- `func TestGoalBoardArchiveWriteFailurePolicy(t *testing.T)` — TestGoalBoardArchiveWriteFailurePolicy 钉住写失败的**显式决定**（§7）：
- `func (r *stubBoardRepository) WriteGoalBoard(context.Context, store.GoalBoardMeta) error`
- `func (r *stubBoardRepository) ReadGoalBoard(context.Context) (store.GoalBoardMeta, error)`
- `func (r *stubBoardRepository) WriteTeamBoard(context.Context, store.TeamBoardMeta) error`
- `func (r *stubBoardRepository) ReadTeamBoard(context.Context) (store.TeamBoardMeta, error)`
- `func TestGoalDomainDecisionsIgnoreBoardArchive(t *testing.T)` — TestGoalDomainDecisionsIgnoreBoardArchive 钉住 I1：**领域判定不吃存档**。
- `func TestGoalBoardArchiveThrottlesUnchangedPayload(t *testing.T)` — TestGoalBoardArchiveThrottlesUnchangedPayload 验证**指纹节流**：载荷内容
- `func TestGoalGovernanceViewFromBoardArchiveSeparatesRecoveredAndClosed(t *testing.T)`

### controller.go

- `func viewOf(record *GoalRecord) *View`
- `func projectionOf(records []*GoalRecord) Projection`
- `func NewController(options Options) *Controller` — NewController 构造控制器（Depth 默认 1；Now 默认 time.Now().Unix）。
- `func (c *Controller) Begin(ctx context.Context, request BeginRequest) (*GoalRecord, error)` — Begin 注册并压栈一个 goal（design §3.4 goal_begin）。
- `func (c *Controller) Update(ctx context.Context, request UpdateRequest) (*GoalRecord, error)` — Update 更新栈顶 active goal（design §3.4 goal_update）。
- `func (c *Controller) Finish(ctx context.Context, request FinishRequest) (*GoalRecord, error)` — Finish 把栈顶 goal 标记 completed 并弹栈（design §3.4 goal_finish；
- `func (c *Controller) Abort(ctx context.Context, request FinishRequest) (*GoalRecord, error)` — Abort 把栈顶 goal 标记 aborted 并弹栈。
- `func (c *Controller) finishOrAbort(ctx context.Context, request FinishRequest, terminal Status, kind EventKind) (*GoalRecord, error)`
- `func (c *Controller) Status() StatusView` — Status 返回全量视图（深拷贝，锁外安全）。
- `func (c *Controller) Projection() Projection` — Projection 返回前端投影（深拷贝视图）。
- `func (c *Controller) projectionLocked() Projection`
- `func (c *Controller) History() []*GoalRecord` — History 返回已收口（finish/abort）goal 的审计副本（栈底→收口序）。
- `func (c *Controller) Frame() string` — Frame 渲染栈顶 goal 的 Goal 帧文本（design §3.5；Part II TechLeader 每轮
- `func (c *Controller) Subscribe(buffer int) *Subscription` — Subscribe 订阅事件流（buffer ≤ 0 时用 64）。Close 幂等。
- `func (s *Subscription) EventsChannel() <-chan Event` — Events 是订阅通道（等价 Subscription.Events，便利字段）。
- `func (s *Subscription) Close()` — Close 移除订阅并关闭通道（幂等）。
- `func (s *Subscription) Dropped() int64` — Dropped 返回因缓冲满而被丢弃的事件数（事件自带全量投影，可覆盖追平）。
- `func (c *Controller) emitLocked(event Event)` — emitLocked 在持锁下向全部订阅者投递；不阻塞（满则 dropped++）。
- `func (c *Controller) persistLocked(ctx context.Context) error` — persistLocked 在持锁下持久化当前栈（无 store 时为空操作）。
- `func (c *Controller) appendAuditLocked(ctx context.Context, entry AuditEntry) error` — appendAuditLocked 在持锁下向审计账本追加一条有界审计（无 Audit 时为空
- `func (c *Controller) Reload(ctx context.Context) error` — Reload 从 Store 装载 goal 栈（崩溃/重启恢复；design §3.7）。装载后修正：

### controller_test.go

- `func newTestController(t *testing.T, depth int) *Controller`
- `func requireActive(t *testing.T, controller *Controller, title string) *GoalRecord`
- `func TestBeginFinishSingle(t *testing.T)` — TestBeginFinishSingle 验证会话单例主路径：begin → update → finish 弹栈删除、
- `func TestStackDepthRejectsNested(t *testing.T)` — TestStackDepthRejectsNested 验证 D2 会话单例：depth=1 时嵌套 begin 被拒。
- `func TestNestedDepthRestores(t *testing.T)` — TestNestedDepthRestores 验证 D2 放开嵌套（depth>1）：压栈下层 paused、
- `func TestNestedGoalsPopLIFOUntilEmpty(t *testing.T)` — TestNestedGoalsPopLIFOUntilEmpty 验证嵌套逐层弹栈直至栈空（治理收口
- `func TestGoalStackDepthBound(t *testing.T)` — TestGoalStackDepthBound 验证 Depth 放开后仍受 MaxStackDepth 上限约束：
- `func TestUpdateOnlyActive(t *testing.T)` — TestUpdateOnlyActive 验证更新边界：空栈/非 active 不可更新。
- `func TestAbortPopsAndHistory(t *testing.T)` — TestAbortPopsAndHistory 验证 abort 路径。
- `func TestIdempotentBegin(t *testing.T)` — TestIdempotentBegin 验证同标题 active 幂等返回现有 goal。
- `func TestJSONStoreRoundtrip(t *testing.T)` — TestJSONStoreRoundtrip 验证存储 roundtrip：变更落盘、新控制器 Reload 恢复
- `func TestFrameRendersGoal(t *testing.T)` — TestFrameRendersGoal 验证 Goal 帧渲染（TechLeader/装配嵌入素材）。
- `func TestSubscribeDropAndClose(t *testing.T)` — TestSubscribeDropAndClose 验证有界订阅：溢出仅计数（不丢语义由全量投影兜底）；
- `func TestConcurrentBeginFinishRace(t *testing.T)` — TestConcurrentBeginFinishRace 在 -race 下验证控制器并发安全与不变量。

### record.go

- `func StatusOfRecord(wire string) dto.GoalStatus` — StatusOfRecord 把 goal 存档里的状态词读回枚举（落盘格的唯一转换点；导出给跨包投影读方）。
- `func IsTerminal(status Status) bool` — IsTerminal 报告状态是否终态（不再停留在 goal 栈上）。
- `func CloneGoalRecord(r *GoalRecord) *GoalRecord` — CloneGoalRecord 深拷贝记录（返回副本，避免锁外读到栈内可变引用）。
- `func validateGoalRecord(r *GoalRecord) error`
- `func newGoalRecord(id string, request BeginRequest, now int64) *GoalRecord` — newGoalRecord 由 BeginRequest 构造记录并应用默认值。
- `func GoalUpdateChangesDefinition(r UpdateRequest) bool` — ChangesDefinition 报告这次更新是否动到了 goal 的**定义**：标题 / 正文 / 完成条件 /

### record_status_test.go

- `func TestGoalStatusOfRecordReadsTheWireAndNeverFoldsToAKnownState(t *testing.T)` — record_status_test.go — goal 状态格的判据：**存档面读回 + 终态判定**。

### sessionstore_store.go

- `func NewContextStateStore(session *store.SessionContextStore, boards ...store.SessionBoards) *ContextStateStore` — NewContextStateStore 构造适配器。session 为 nil 时 Load/Save 返回
- `func (s *ContextStateStore) BindBoards(boards store.SessionBoards)` — BindBoards 注入会话看板存档取用面（store.Router.BoardsForSession 的产物）。
- `func (s *ContextStateStore) boardRepository() (store.SessionBoards, bool)` — boardRepository 返回看板存档取用面（**绑定会话**的面）；未装配返回 false。
- `func (s *ContextStateStore) ReadGoalBoard(ctx context.Context) (store.GoalBoardMeta, error)` — ReadGoalBoard reads the bound session's persisted goal-board projection.
- `func (s *ContextStateStore) Load(ctx context.Context) ([]*GoalRecord, error)` — Load 实现 Store：从会话 GoalStack 读取当前栈（空栈返回空切片）。
- `func (s *ContextStateStore) Save(ctx context.Context, records []*GoalRecord) error` — Save 实现 Store：全量替换会话 GoalStack 并持久化。写入前先确保会话
- `func goalFramesFromRecords(records []*GoalRecord) []store.GoalFrame` — goalFramesFromRecords 把 goal 域记录投影为 store 第五栈帧
- `func recordsFromGoalFrames(frames []store.GoalFrame) []*GoalRecord` — recordsFromGoalFrames 把 store 第五栈帧还原为 goal 域记录

### sessionstore_store_test.go

- `func newSessionRouterForGoalTest(t *testing.T) *store.Router`
- `func newGoalSessionStore(t *testing.T, router *store.Router, sessionID string) *store.SessionContextStore`
- `func TestContextStateStoreReloadRestoresNestedStack(t *testing.T)` — TestContextStateStoreReloadRestoresNestedStack 验证会话恢复：Controller
- `func TestContextStateStoreSessionIsolation(t *testing.T)` — TestContextStateStoreSessionIsolation 验证 goal 第五栈按会话隔离：
- `func TestContextStateStoreFinishPopsToEmpty(t *testing.T)` — TestContextStateStoreFinishPopsToEmpty 验证栈空语义落盘：子 goal 完成 →
- `func TestContextStateStoreNilRejects(t *testing.T)` — TestContextStateStoreNilRejects 验证未装配会话上下文存储时 Store 显式失败。

### stack.go

- `func (s *Stack) Len() int` — Len 返回栈中 goal 数。
- `func (s *Stack) Push(record *GoalRecord)` — Push 压栈。
- `func (s *Stack) Pop() *GoalRecord` — Pop 弹栈顶；空栈返回 nil。
- `func (s *Stack) Top() *GoalRecord` — Top 返回栈顶（不弹）。
- `func (s *Stack) All() []*GoalRecord` — All 返回栈底→栈顶的切片（引用；调用方需持锁或仅读）。

### stack_test.go

- `func TestStackLIFO(t *testing.T)`

### store.go

- `func NewMemoryStore() *MemoryStore` — NewMemoryStore 构造空内存存储。
- `func (m *MemoryStore) Load(_ context.Context) ([]*GoalRecord, error)` — Load 实现 Store。
- `func (m *MemoryStore) Save(_ context.Context, records []*GoalRecord) error` — Save 实现 Store（原子替换快照）。
- `func NewJSONFileStore(path string) *JSONFileStore` — NewJSONFileStore 构造文件存储（父目录需存在；测试用 t.TempDir()）。
- `func (s *JSONFileStore) Load(_ context.Context) ([]*GoalRecord, error)` — Load 实现 Store；文件不存在视为空栈。
- `func (s *JSONFileStore) Save(_ context.Context, records []*GoalRecord) error` — Save 实现 Store（原子写：同目录 temp + rename）。

