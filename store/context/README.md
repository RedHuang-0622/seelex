# store/context

## 生态位

会话 context 与 checkpoint 的持久化机制 owner。首个 M7 切片拥有
`context.json` 原子读写和 workplan checkpoint 键派生；prompt/栈解释仍留给
application context 域。

## 职责与非职责

- 职责：context blob 路径、原子发布、读取，以及 checkpoint ID 的安全键映射。
- 非职责：Plan/Task/Goal/Skill 栈语义、prompt 装配、provider wire 缓存。

## 文件索引

- `context.go`：`Store`、`StateFile`、`CheckpointStorageKey`。
- `context_test.go`：round-trip 与键稳定性。

## 依赖方向

只依赖标准库、`store/root`、`store/internal/layout`；禁止 import 根 `store` 或上层。

## 数据兼容

`<session>/context.json` 文件名与 opaque payload 字节保持原样；checkpoint 键仍为
`checkpoint-<hash>`。

## 测试

```text
go test ./store/context ./store -run 'Context|Checkpoint' -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

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

