# application/session/query

## 生态位

`application/session/query` 承载 Session 的只读目录、元数据、快照、transcript 与
输入索引查询。当前真实调用方是 `application/assembly.Service` 的兼容查询入口与
项目看板装配。

## 职责与非职责

职责：

- 在共享读锁内组合会话目录的目录行、活体行和状态富化；
- 从会话绑定事实读取项目根；
- 委托会话元数据、快照、transcript 与全量输入索引读取；
- 拥有全量用户输入索引的组合：durable range → record 降级、已加载窗口标注、
  摘要构建与空会话降级；慢读在共享写锁之外，窗口读取只取 `ViewMu.RLock`；
- 等待目录 worker 刷新，不进入共享写锁。
- 从全量 RuntimeState 提取会话专属运行投影并深拷贝可变字段。
- 只读选择 Snapshot 的冷读、驻留和内容 LRU 回退路径。
- 组装驻留 Snapshot 的字段、Task/Plan 克隆与 WorkTable enrichment。

非职责：

- 不拥有 Session、Catalog、Snapshot 或持久化存储；
- 不启动、恢复、切换或释放会话；
- 不导入 `application/assembly`，不持有完整 `Service`。

## 文件结构

- `application.go`：`Deps`、`Application`、构造器与只读用例。
- `application_test.go`：读锁、目录组合、绑定根与 durable 读取委托测试。
- `input_index.go`：`InputIndexApplication`、durable 全量读取、record 降级与
  已加载窗口组合。
- `input_index_test.go`：全量索引、record fallback 与空 session 门禁。
- `runtime.go`：SessionRuntime 字段归属、Plan/Goal/WorkTable 深拷贝。
- `snapshot_read.go`：Snapshot 冷/热路径选择。
- `resident_snapshot.go`：驻留 Snapshot 字段组装与 enrichment。

## 核心实现

`Deps` 只接收实际需要的读能力。`Application` 不缓存任何查询结果；每次调用都
读取宿主当前权威事实。目录组合的 `ViewMu` 只覆盖内存读取和投影拼接。

## 数据流

`Service` 兼容入口 → `query.Application` / `InputIndexApplication` → 注入的
目录/元数据/Workspace/存储读与锁内窗口端口。快照、transcript 与 input index
的慢读发生在共享写锁之外。

## 依赖方向

允许依赖 `contract`、`contract/protocol` 与 `middle/switch` 的只读元数据
契约。禁止依赖 `application/assembly`、backend 具体实现、GUI/TUI 或写入用例。

## 并发与错误

- `ListSessions` 只取 `ViewMu.RLock`，不调用慢 I/O；
- `WaitCatalogRefresh`、`SnapshotOf`、transcript 与 input index 不获取共享写锁；
- input index 只围绕已加载窗口取 `ViewMu.RLock`；durable range 与 record
  fallback 在锁外；
- 未装配目录锁时返回空目录，未绑定 workspace 时返回空根；
- 底层读取错误原样返回。
- Runtime projection 不保留进程级 model/provider/plugin/account 字段，Goal 历史与
  WorkTable 切片必须断开共享。

## 扩展方式

新增只读 Session 投影时先确认有真实调用方，再扩展最小 `Deps`；不得把会话生命周期
或命令执行塞入本包。

## Review 指南

- 目录组合是否仍在读锁内且不触发慢 I/O；
- 项目根是否只来自 session binding，不回退到当前视图；
- 是否出现第二份 transcript/input-index 判据；
- input index 是否仍由本包唯一拥有，core 是否退化为仅装配回调；
- 是否 import `application/assembly` 或持有完整 `Service`。

## 测试与验证

```text
go test ./application/session/query -count=1
go test ./application/assembly -run 'SessionQuery|ListSessions|SnapshotOf|Transcript|InputIndex' -count=1
go test ./application/... -count=1
```

## 文件与函数索引

### `application.go`

- `func New(deps Deps) *Application` — 构造只读 Session 查询应用。
- `func (application *Application) ListSessions() []model.SessionInfo` — 组合权威会话目录。
- `func (application *Application) ProjectRootFor(sessionID string) string` — 从会话绑定读取项目根。
- `func (application *Application) SessionMeta(sessionID string) (model.SessionMeta, error)` — 读取会话展示元数据。
- `func (application *Application) WaitCatalogRefresh(ctx context.Context) error` — 等待目录刷新。
- `func (application *Application) SnapshotOf(sessionID string) (model.SessionSnapshot, error)` — 读取会话快照。
- `func (application *Application) GetSessionTranscript(sessionID string, fromSeq, toSeq uint64) ([]model.TranscriptEvent, error)` — 读取 transcript 区间。
- `func (application *Application) SessionInputIndex(sessionID string) (model.SessionInputIndex, error)` — 读取用户输入索引。

### `application_test.go`

测试文件按仓库 README 规范不展开函数索引。

### `input_index.go`

- `func NewInputIndex(deps InputIndexDeps) *InputIndexApplication` — 构造只读全量输入索引应用。
- `func (application *InputIndexApplication) Read(sessionID string) (model.SessionInputIndex, error)` — 组合 durable 全量输入、record 降级和已加载窗口。

### `input_index_test.go`

测试文件按仓库 README 规范不展开函数索引。

### `runtime.go`

- `func SessionRuntimeOf(runtime model.RuntimeState) model.SessionRuntime` — 提取会话专属运行字段并深拷贝。
- `func CloneGoalGovernanceView(view *dto.GoalGovernanceView) *dto.GoalGovernanceView` — 克隆 Goal 收口账本。

### `snapshot_read.go`

- `func ReadSnapshot(sessionID string, deps SnapshotReadDeps) (model.SessionSnapshot, error)` — 按驻留、内容 LRU 和冷读条件选择 Snapshot 来源。

### `resident_snapshot.go`

- `func BuildResidentSnapshot(input ResidentSnapshotInput) model.SessionSnapshot` — 从不可变 View/Task/WorkTable 输入组装驻留快照。
