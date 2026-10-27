# application/session

## 生态位

`application/session` 承载 Session 聚合、状态与生命周期应用用例。Chat 执行循环仍在
Conversation/Backend，但会话持有的 ChatState、Queue、Approval 引用、View 指针和
revision 由本包的 `state` / `viewstate` 子包唯一拥有。

主要调用方：`application/assembly`、`application/conversation` 与 Wiring 装配；
Middle 只提供纯切换、历史和驻留策略。

## 职责

- 提供 Session 聚合状态、Snapshot 投影以及 Meta、SSH、Lifecycle、Archive、
  Delete、Draft、Resume、Fork、Interaction 与 Release。
- 保持 Application 入站行为与 DTO 身份稳定。
- 隔离 Session 用例，避免继续扩大 `application/assembly`。

## 非职责

- 不执行 Agent loop、工具、引擎流或持久化事务。
- 不实现存储、权限、工作区或 Runtime 规则。
- 不反向导入 `application/assembly`。

## 文件结构

- `meta.go`：会话置顶、别名与排序位的读写应用组件。
- `state/`：SessionUnit、Domain actor、View、InputQueue、Runtime slot 与 chat 状态。
- `viewstate/`：权威 Snapshot、消息序号、revision 与事件发布顺序。
- `ssh.go`：SSH 允许环境、固定环境与项目环境表的校验编排。
- `release.go`：LRU/完整释放的二次守卫、flush 与有序回收。
- `residency.go`：`ResidencyPort`（预览、LRU 序、输入索引、驱逐、归档）与纯值 deps。
- `residency/`：Application-owned resident/content 两层 LRU、受保护集、正文卸载
  与自动回读运行时。
- `history/`：Session resume、冷恢复顶层流程、durable 分页与可见窗口安装。
- `fork/`：durable Session fork 准入、深拷贝、持久化、workspace 绑定与 child
  engine 收口。
- `lifecycle.go`：unload 的过渡锁、持久化、释放和视图续接。
- `archive.go`：archive 门控、记录/引擎卸载与后台释放。
- `delete.go`：删除记录、释放内存、解绑工作区与快照发布。
- `draft_submission.go`：未物化草稿的显式提交激活。
- `draft_entry.go`：`/new` 的旧会话保存、清理与 draft shell 安装。
- `draft_materialization.go`：首请求物化、标题、commit 与发布顺序。
- `resume.go`：hot/restoring/cold 会话选择分支。
- `restore_recovery.go`：冷恢复失败回退与 fresh draft reset。
- `fork_lineage.go`：只读取 `forked_from` 的窄 JSON 投影并安全解码，不依赖 Store 载荷类型。
- `fork.go`：父 ID 校验、per-session 过渡锁、latest cut 与 commit 后 resume。
- `submit.go`：显式 session 的草稿物化、加载/正文判定、恢复与提交顺序。
- `restoring_shell.go`：restoring 会话的 Snapshot 空壳投影。
- `interaction.go`：`InteractionApplication`，统一 approval/session/account
  决议与失败 notice/close 顺序，不拥有 broker、Session 或 Account 状态。
- `meta_test.go`：组件读取与 nil 语义测试。
- `ssh_test.go`：环境交集、固定环境与路径校验测试。
- `release_test.go`：守卫、flush 和回收顺序测试。
- `operations_test.go`：删除顺序、记录错误短路和工作区投影测试。
- `orchestration_test.go`：草稿提交锁、resume 分支与恢复回退测试。
- `draft_entry_test.go`：草稿入口保存/清理与物化顺序测试。
- `*_external_test.go`：草稿提交、物化与 fork-lineage 的 owner 外部测试。

## 核心实现

`MetaApplication` 通过 `MetaDeps` 接收 metadata port 与只读查询；写入继续调用
`kernel/sessionpolicy.SetMeta`，读取由 `SessionQuery` 注入。

`SSHApplication` 通过 `SSHDeps` 读取 Session SSH 状态和工作区环境表，只拥有允许
环境子集、固定环境不可变和未选择环境缺省值等应用判据。

`ReleaseApplication` 通过 `ReleaseDeps` 接收受保护集、持久化、引擎/正文/任务/
工作区释放动作；包内只固定二次守卫、flush 与回收顺序。

`LifecycleApplication`、`ArchiveApplication`、`DeleteApplication` 分别固定 unload、
archive、delete 的应用顺序；准入与归档判据仍在 middle 层。

`InteractionApplication` 只消费当前视图交互、后台 approval 反查以及
resume/account/notice/close 回调。审批 broker、Session 恢复和账号切换仍由原 owner
执行。

`ForkApplication` 只拥有 transition/resume 编排；fork snapshot 深拷贝、持久化与
workspace 绑定仍由注入的 `ForkLocked` 宿主执行。`SubmitApplication` 只拥有显式
session 的 admission 顺序，队列和 Chat 执行仍由 Conversation/Session state 拥有。

## 数据流

`application/assembly` / `Frontend` → `application/session` 用例 →
`LifecycleApplication` / `ArchiveApplication` / `DeleteApplication` /
`ReleaseApplication` → 注入的窄端口 → metadata、Session、Workspace、Engine、
Runtime 或存储的既有唯一实现。

## 依赖方向

允许依赖 `contract/dto`、`contract/protocol`、本包的 `state` / `viewstate` 与
注入的窄端口。
residency 判据经 `ResidencyPort` 注入，由 `wiring/adapters.NewResidencyPort`
提供 Middle 实现；本包不 import `middle/residency`。
Fork lineage 只消费会话上下文 JSON 中的 `forked_from` 字段，不 import `store`。
禁止依赖 `application/assembly`、backend 具体实现或 Middle 可变会话状态。

## 并发与错误

- 读写函数由宿主注入；组件不缓存 metadata、SSH 状态或会话实体。
- nil 应用读取保持 `switch.ErrMetaUnsupported` 语义。
- 写入后的目录刷新由注入的 `switch.MetaDeps.Wake` 负责。
- SSH 写入失败不得留下部分环境集合；固定环境不得被后续调用改写。
- Release 在任何具体回收动作前重新检查受保护集，确认不过时原样返回 `nil`。
- Release 的慢 flush 与引擎卸载不持有 `application/assembly` 的共享锁；锁只由注入
  回调在各自的短临界区内获取。

## 扩展方式

新增 Session 能力时使用独立 deps 与组件；不得把完整 `*Service` 注入本包，
也不得把 Agent、Chat 或 Session 主状态机搬入本包。

## Review 指南

- 检查写入是否仍由 `kernel/sessionpolicy.SetMeta` 唯一校验。
- 检查读取是否仍来自绑定事实或 SessionQuery，而不是视图猜测。
- 检查 SSH 环境子集与固定环境判据是否只保留一份。
- 检查 Release 的受保护集、flush 顺序与具体释放实现是否仍委托宿主。
- 检查组件是否依赖 core。

## 测试与验证

```text
go test ./application/session/... -count=1
go test ./application/assembly -run 'SessionMeta|SessionSSH|Release' -count=1
go test ./application/... -count=1
go test -race ./application/... -count=1
```

## 文件与函数索引

### `lifecycle.go`

- `func NewLifecycle(deps LifecycleDeps) *LifecycleApplication` — 构造 Session unload 应用。
- `func (application *LifecycleApplication) Unload(sessionID string) error` — 持久化并释放一个会话。

### `residency.go`

- `type ResidencyPort interface` — preview、LRU 序、输入索引、驱逐与归档的消费方端口。
- `type EvictionSnapshot struct` / `EvictionDeps` — 两层 LRU 的应用读数与动作。
- `type ArchiveGate` / `ArchiveStore` / `ArchiveEngine` / `ArchiveUseCaseDeps` — 归档用例的纯值 deps。

### `archive.go`

- `func NewArchive(deps ArchiveDeps) *ArchiveApplication` — 构造 Session archive 应用。
- `func (application *ArchiveApplication) Archive(sessionID string) error` — 归档会话并卸载驻留资源。

### `delete.go`

- `func NewDelete(deps DeleteDeps) *DeleteApplication` — 构造 Session delete 应用。
- `func (application *DeleteApplication) Delete(sessionID string) error` — 删除记录并释放全部进程内资源。

### `draft_submission.go`

- `func NewDraftSubmission(deps DraftSubmissionDeps) *DraftSubmissionApplication` — 构造草稿提交应用。
- `func (application *DraftSubmissionApplication) Materialize(sessionID, firstInput string) error` — 在视图归属复判后物化草稿。

### `draft_entry.go`

- `func NewDraftEntry(deps DraftEntryDeps) *DraftEntryApplication` — 构造草稿入口应用。
- `func (application *DraftEntryApplication) Begin() error` — 保存旧会话并进入新草稿壳。

### `draft_materialization.go`

- `func NewDraftMaterialization(deps DraftMaterializationDeps) *DraftMaterializationApplication` — 构造首请求物化应用。
- `func (application *DraftMaterializationApplication) Materialize(firstQuestion string) error` — 绑定 workspace、打开引擎并提交第一个真实会话。

### `resume.go`

- `func NewResume(deps ResumeDeps) *ResumeApplication` — 构造会话恢复应用。
- `func (application *ResumeApplication) Resume(sessionID string) error` — 选择 hot、restoring 或 cold 分支。

### `restore_recovery.go`

- `func NewRestoreRecovery(deps RestoreRecoveryDeps) *RestoreRecoveryApplication` — 构造冷恢复失败回退应用。
- `func (application *RestoreRecoveryApplication) Handle(...)` — 回退前一会话或重置草稿。
- `func NewRestoreDraftReset(deps RestoreDraftResetDeps) *RestoreDraftResetApplication` — 构造 fresh draft reset。
- `func (application *RestoreDraftResetApplication) Reset()` — 安装 fresh draft shell 并发布。

### `fork_lineage.go`

- `func NewForkLineage(deps ForkLineageDeps) *ForkLineageApplication` — 构造 fork lineage 读取应用。
- `func (application *ForkLineageApplication) Load(...)` — 解码并校验 fork 引用。

### `restoring_shell.go`

- `func NewRestoringShell(deps RestoringShellDeps) *RestoringShellApplication` — 构造 restoring shell 应用。
- `func (application *RestoringShellApplication) PointLocked(...)` — 投影 restoring 空壳并 bump revision。
