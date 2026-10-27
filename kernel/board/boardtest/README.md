# middle/view/board/boardtest

## 生态位

`middle/view/board/boardtest` 是项目看板测试的**共享夹具包**：内存版存储面
（`Store` + 注入一次写失败的 `FlakyStore`）与行读取助手。两侧一起用它——
`middle/view/board` 的用例（映射 / 同步器语义）与 `application/assembly` 的用例
（Service 取数口 / 钩子接线），因此夹具只有一份。

## 职责与非职责

- 职责：模拟 `board.ProjectBoardStore` 的两个语义（追加即读回、水位 = 文件内最大 Seq）；
  提供等待落盘、休眠过静默窗、折叠读、载荷取字段等断言助手。
- 非职责：不复制任何生产判定；不 import `middle/view/board`（**刻意的**：本包若 import 它，
  该包自己的内部用例再 import 本包就形成测试环）。因此接口满足关系由使用侧断言。

## 依赖方向

只依赖 `store`、`middle/protocol/model` 与标准库 / `testing`。禁止依赖任何 Seelex
业务包——夹具一长业务依赖就会把"被侧的编译图"拖进另一侧。

## 使用方式

```go
st := boardtest.NewStore()
syncer := NewProjectBoardSyncer(ProjectBoardDeps{
    Store:          st,
    WorkTableRows:  rows,
    ProjectRootFor: func(string) string { return boardtest.ProjectRoot },
    Triggers:       TriggerFor(ProjectBoardHookModeTodoList),
    FlushIdle:      20 * time.Millisecond,
    Now:            func() time.Time { return time.Unix(boardtest.At, 0) },
})
defer syncer.Close()
syncer.OnToolComplete("todo_add", nil, "s-1")
boardtest.WaitForWrites(t, st, store.ProjectBoardWorkFile, 1)
```

夹具常量（`Project` / `ProjectRoot` / `At`）是两侧用例的共同事实源：装配入参在两处就地
拼装，但取值只此一份。

## Review 指南

- 夹具里出现业务判定（例如替被测代码决定"该不该投影"）即为越界：它是存储面替身，不是判据。
- 新增助手时先看是否已有同义函数（`Folded` 与 `Raw` 的区别是"折叠读 / 原始行"）。

## 测试与验证

```text
go test ./middle/view/board/... -count=1
```

本包自身没有用例（无 `_test.go`）：它的正确性由两侧用例的断言体现（若水位或追加语义被改坏，
两侧都会红）。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### rows.go

- `func WaitForWrites(t *testing.T, st *Store, file string, want int)`
- `func Settle(idle time.Duration)` — settleBoard 等过若干倍静默窗：用来证明"没有第 N 次落盘"（消极断言需要时间）。
- `func MarshalRows(t *testing.T, rows []store.ProjectBoardRow) string`
- `func RowByKey(rows []store.ProjectBoardRow, key string) (store.ProjectBoardRow, bool)`
- `func PayloadText(row store.ProjectBoardRow, key string) string`
- `func Keys(rows []store.ProjectBoardRow) []string` — boardKeys 取行集的 key 集（排序后），用于"投影/不投影"的集合断言。
- `func TodoItem(id, text, status string) model.WorkItem`
- `func TaskItem(id, text, status string) model.WorkItem`
- `func NewLiveRows(items ...model.WorkItem) *LiveRows`
- `func (live *LiveRows) Set(items []model.WorkItem)`
- `func (live *LiveRows) Add(item model.WorkItem)`
- `func (live *LiveRows) Drop(index int)`
- `func (live *LiveRows) Rows() []model.WorkItem`
- `func SessionRows(t *testing.T, st *Store, session, file string) int`

### store.go

- `func NewStore() *Store`
- `func boardStoreKey(projectRoot, sessionID, file string) string` — boardStoreKey 是内存替身的文件键：落点是**按会话分目录**的一份文件
- `func (st *Store) AppendProjectBoardRows(projectRoot, sessionID, file string, rows []store.ProjectBoardRow) (uint64, error)`
- `func (st *Store) ProjectBoardWatermark(projectRoot, sessionID, file string) (uint64, error)`
- `func (st *Store) Watermark(projectRoot, sessionID, file string) uint64`
- `func (st *Store) WriteCount(file string) int`
- `func (st *Store) LastWrite(file string) []store.ProjectBoardRow`
- `func (st *Store) Raw(projectRoot, sessionID, file string) []store.ProjectBoardRow` — raw 是**某会话某文件**的原始行（未折叠；I6 断言"空集合不建文件"用它）。
- `func (st *Store) Folded(projectRoot, sessionID, file string) []store.ProjectBoardRow` — folded 复用存储面**唯一读法**（折叠读），不自己实现第二份。
- `func (st *Store) DropFile(projectRoot, sessionID, file string)`
- `func (st *FlakyStore) AppendProjectBoardRows(projectRoot, sessionID, file string, rows []store.ProjectBoardRow) (uint64, error)`

