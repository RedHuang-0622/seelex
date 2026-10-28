# store/context

## 生态位

会话 context 读侧与 checkpoint 持久化机制 owner。M7 拥有 `context.json`
原子读写、workplan checkpoint 键派生、四类栈的 wire/状态判据，以及
“compact frame + 持久行 + retry attempt”到 provider wire 的纯装配状态机。

## 职责与非职责

- 职责：context blob 路径与原子发布、checkpoint 键、Context/Stack/checkpoint
  wire 身份、四类栈状态判据，以及无 I/O 的 provider wire 装配。
- 非职责：provider 调用；Event/attempt cache 的具体读取与字段映射；上层
  Application 对 prompt、plan/task/goal 的解释与用例编排。

## 文件索引

- `context.go`：opaque context blob 的唯一路径与读写实现。
- `wire.go`、`wire_engine.go`：provider wire 类型、预算与纯装配状态机。
- `fork.go`：父 context payload 到独立子会话 payload 的纯重写。
- `stacks.go`、`compact.go`、`operations.go`：栈词表、边界的唯一判据。
- `context_test.go`、`wire_engine_test.go`：wire 身份、装配、修复与稳定性。

## 依赖方向

只依赖标准库、`contract/protocol`、`store/root`、`store/internal/layout`；
禁止 import 根 `store` 或上层。根包只把 `Event` 与 attempt cache 映射成这里的
输入，不保留第二份装配状态机。

## 数据兼容

`<session>/context.json` 文件名与 opaque payload 字节保持原样；checkpoint 键仍为
`checkpoint-<hash>`。

## 读侧投影

`SessionContextStore.CompactStackSnapshot` 是压缩检索、重复压缩基准与相关记忆
选取的唯一窄读口；它只返回 `CompactFrame` 深拷贝。这些消费者不得为了读压缩栈
再依赖 `SessionContextRecord`。Plan/Task 的 prompt 投影与上下文 blob 投影仍待
M7 后续切片继续收缩。

`SessionContextStore.PromptStackSnapshot` 是 prompt 装配与本地压缩的下一层目标
投影：只含 Plan/Task/Skill/Compact 四栈，显式排除 schema、fork origin、
SystemPrompt 与 GoalAudit。

## 测试

```text
go test ./store/context -count=1
go test ./store -run 'Context|Checkpoint|Wire' -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### compact.go

- `func AppendCompact(stack []CompactFrame, frame CompactFrame) ([]CompactFrame, error)` — AppendCompact validates and appends one compact frame without performing IO.

### context.go

- `func New(layout *storelayout.Layout) *Store` — New constructs a context state store.
- `func (s *Store) StatePath(scope storelayout.Scope) string` — StatePath returns <session>/context.json.
- `func (s *Store) WriteState(scope storelayout.Scope, payload []byte) error` — WriteState atomically publishes an opaque context blob.
- `func (s *Store) ReadState(scope storelayout.Scope) ([]byte, error)` — ReadState reads the opaque context blob.
- `func CheckpointStorageKey(id string) string` — CheckpointStorageKey maps a workplan checkpoint ID to its safe storage key.

### context_test.go

- `func TestContextStateRoundTrip(t *testing.T)`
- `func TestCheckpointStorageKey(t *testing.T)`
- `func TestSessionContextRecordWireIdentity(t *testing.T)`
- `func TestContextPayloadWireIdentity(t *testing.T)`
- `func TestStackWireAndVocabulary(t *testing.T)`
- `func TestAppendCompactChain(t *testing.T)`
- `func TestWireParamsAndResultWire(t *testing.T)`
- `func TestContextOperationValidation(t *testing.T)`

### fork.go

- `func ForkState(payload []byte, opts ForkOptions) ([]byte, []CompactFrame, error)` — ForkState 把父 context payload 重写为独立子会话 payload，并返回重写后的
- `func forkSkillFrames(frames []SkillFrame, cutTime time.Time) []SkillFrame`
- `func RewriteForkCompactStack(frames []CompactFrame, cut uint64, cutMessageID string) []CompactFrame` — RewriteForkCompactStack 在分叉点裁剪继承的 compact frames：无事件坐标的旧帧

### fork_test.go

- `func TestForkStateCreatesEmptyChild(t *testing.T)`
- `func TestForkStateFiltersSkillsAndRewritesCompactFrames(t *testing.T)`
- `func TestForkStateRejectsCorruptOrUnsupportedParent(t *testing.T)`

### operations.go

- `func newStackInput( itemID string, kind StackKind, status string, payload any, enteredAt time.Time, now time.Time, roleName string, roleSessionID string, ) (StackItemInput, error)`
- `func NewPlanStackInput(frame PlanFrame, now time.Time) (StackItemInput, error)`
- `func NewTaskStackInput(frame TaskFrame, now time.Time) (StackItemInput, error)`
- `func NewGoalStackInput(frame GoalFrame, now time.Time) (StackItemInput, error)`
- `func RequireTopGoal(frames []GoalFrame, goalID string) error`
- `func PushSkillFrame(frames []SkillFrame, frame SkillFrame, now time.Time) ([]SkillFrame, error)`
- `func PopSkillFrame(frames []SkillFrame, skillID string) ([]SkillFrame, error)`

### stacks.go

- `func ValidStackKind(kind StackKind) bool`
- `func StackKindIndex(kind StackKind) int`
- `func IsTerminalStackStatus(status string) bool`

### wire.go

- `func NormalizeWireParams(params WireParams) WireParams`
- `func ApplyWireBudget(settings storeroot.Settings, base WireParams) WireParams`

### wire_engine.go

- `func providerContent(row WireRow) string`
- `func AssembleWireRows(frame *WireFrame, rows []WireRow, attempts WireAttemptProvider, params WireParams) WireResult` — AssembleWireRows builds a deterministic provider wire without performing I/O.
- `func (state *wireState) emit(message WireMessage, cost int)`
- `func unitCost(row WireRow) int`
- `func (state *wireState) consumeRow(row WireRow)`
- `func (state *wireState) budgetWouldExceed(cost int, boundaryComplete bool) bool`
- `func (state *wireState) closePendingBeforeUser()`
- `func (state *wireState) closeTail()`
- `func (state *wireState) attachAttempts(provider WireAttemptProvider)`
- `func roleForAttempt(role string) string`
- `func WireDigest(messages []WireMessage) string` — WireDigest returns the stable digest of one assembled wire.
- `func WirePrefixEqual(left, right []WireMessage) bool` — WirePrefixEqual compares persistent wire messages while ignoring retry attempts.

### wire_engine_test.go

- `func (attempts staticWireAttempts) RecentForAnchor(anchorSeq uint64, k int) []WireAttempt`
- `func TestAssembleWireRowsKeepsFrameTailAndAnchoredAttempts(t *testing.T)`
- `func TestAssembleWireRowsRepairsOpenTailAndStopsAtUnitBoundary(t *testing.T)`
- `func TestAssembleWireRowsMarksOpenTail(t *testing.T)`
- `func TestWireDigestAndPrefixIgnoreAttempts(t *testing.T)`
