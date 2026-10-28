# store/workunit

## 生态位

node/subagent/teammate 会话记录与主会话索引的持久化 owner。主要调用方是根
`store.NodeSessionStore` 的迁移期单层转发、`backend/multiagent/subagent`、
`backend/workunit` 与装配层恢复链。

## 职责与非职责

- 职责：`NodeSessionRecord` 稳定 wire、`subagents/` 文件布局、原子写、枚举、
  删除、缺失即 `ErrNotFound`，以及记录写侧责任链。
- 非职责：子代理执行、作业调度、主会话消息提交、worktree merge 或跨域收口。

## 依赖方向

依赖标准库、`store/root` 与 `store/internal/layout`；禁止 import 根 `store`、
Application、Middle、Backend 或其它 store 领域实现。

## 数据兼容

目录保持 `<sessionRoot>/subagents/`，文件名保持
`<mainSessionID>-<subSessionID>.json`；JSON tag、schema version 与排序保持不变。
本包不迁移或删除既有记录。

## 文件与函数

- `record.go`：`Record`、`UnitRecord`、`WorktreeRecord`、`SchemaVersion`、`ErrNotFound`。
- `store.go`：`Store.Write/Read/List/Delete/MarkTerminal` 与写侧 `Writer` 责任链。
- `store_test.go`：路径/默认值、缺失/list/delete/terminal identity、writer chain。

## 测试

```text
go test ./store/workunit -count=1
go test ./store -run 'NodeSession|Subagent|UnitRecord' -count=1
```

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### store.go

- `func New(sessionRoot string) *Store` — New 构造记录存储。sessionRoot 是**主会话**的存储目录（记录按主会话索引）。
- `func (s *Store) Write(record Record) error` — Write 写入（覆盖）一条记录：补默认值 → 建目录 → 编码 → 原子替换。
- `func (s *Store) Read(mainSessionID, nodeSessionID string) (Record, error)` — Read 读取一条记录。文件不存在返回 `ErrNotFound` 包装（带点名的那种子会话 ID）；
- `func (s *Store) List() ([]Record, error)` — List 从主会话索引列举全部记录（按 NodeID 排序）。
- `func (s *Store) Delete(mainSessionID, nodeSessionID string) error` — Delete 删除一条记录。文件或目录不存在 = 已经离场（幂等，不是错误）。
- `func (s *Store) MarkTerminal(mainSessionID string, record Record) error` — MarkTerminal 就地记录一条 node 记录的终端状态。
- `func FileName(mainSessionID, nodeSessionID string) string` — FileName 生成 `<mainSessionID>-<nodeSessionID>.json` 文件名
- `func NewWriter(recorder Recorder) *Writer` — NewWriter 以既有存储为末端起一条链。
- `func (w *Writer) With(links ...Link) *Writer` — With 派生一条**在外面再包一环**的链（本链不动）：新的一环先跑，之后才走本链原有的环。
- `func (w *Writer) Save(projectID, mainSessionID string, record Record) error` — Save 走完整条链再落盘（链上每一环只看得到自己这一层的事实）。
- `func (w *Writer) Store() Recorder` — Store 返回链的末端存储：**读面**（列举/读取/删除/项目作用域）仍走它，本类型只管

### store_test.go

- `func TestStoreRoundTripAndPhysicalPath(t *testing.T)`
- `func TestStoreMissingListDeleteAndTerminal(t *testing.T)`
- `func (r *recordingRecorder) Save(_, _ string, record Record) error`
- `func (*recordingRecorder) ProjectID() string`
- `func (*recordingRecorder) Load(string, string, string) (Record, error)`
- `func (*recordingRecorder) List(string, string) ([]Record, error)`
- `func (*recordingRecorder) Delete(string, string, string) error`
- `func TestWriterChainOnlyFillsOwnFacts(t *testing.T)`
