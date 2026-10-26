# workspace

## 生态位

`middle/switch/workspace/` 是 Seelex **工作区用例**的唯一编排点：创建工作区、
把工作区绑定/解绑到当前视图会话或草稿、解析当前工作区根，以及为应用视图收集
工作区投影。主要调用方是 `application/core` 的门面（公开方法只做一行转发）；
工作区存储与 Git/文件能力仍由 [`store/workspace`](../../../store/workspace/README.md)
提供，经 `contract.WorkspacePort` 及其 optional 端口注入。

本包不 import `application/*`、`store` 具体类型、`wiring` 或 `backend/*`。
视图快照（`Snapshot.Workspaces` / `CurrentWorkspace` / 绑定表）的 owner 仍是
应用视图：本包只产出 `Projection`，由宿主在 `ViewMu` 内写入。

## 职责与非职责

**做**：

1. `CreateWorkspace`：校验名称/根路径，空 `gitRemote` 时探测 origin，创建后立即绑定；
2. `CreateWorkspaces`：一次校验多个项目草稿（先拒绝空名称、空根路径和重复根路径），再批量登记；**不绑定当前视图**，供 GUI temp list 创建后再由用户选择项目；
3. `BindWorkspace`：按当前视图事实走两条真值表——
   - **草稿**：只绑视图/草稿槽位（不提前写 `BindSession`，物化首次提交才落绑定）；
   - **普通会话**：换项目且已有历史时先保存旧会话，再另起独立会话（显式 ID 或
     引擎自分配 ID），挂接 context store、绑存储与运行时、重投影工作表格；
   同一项目或空历史保持当前会话；
4. `UnbindWorkspace`：解绑当前会话/草稿、清全局项目根与写作用域、清待审阅请求；
5. 当前工作区读面：树、文件数、Git 提交/改动/提交详情、文件内容、提交内文件内容、
   面板保存；root 只来自宿主视图，客户端只能传相对路径；
6. `Projection`：在视图锁外收集工作区清单与会话绑定，供应用视图与其它用例复用。

**不做**：

- 不做持久化、路径 containment、敏感文件过滤、Git argv 与超时（在 store 层）；
- 不持视图锁、不直接改 Snapshot、不发事件（经 `ViewPort` 交给应用视图）；
- 不承担会话删除/释放（`DeleteSession` / `releaseSessionAsync` 留在
  `application/core` 的会话域）；
- 不改工作区绑定键、持久化格式或用户可见文案。

## 目录与文件结构

| 文件 | 职责 |
|---|---|
| `workspace.go` | `Projection` / `ViewState` / `ViewPort` / `Sessions` / `Runtime` / `Engine` / `Deps` / `UseCase`；创建、绑定、解绑、读面与投影收集 |
| `workspace_test.go` | 用例真值表（草稿/普通、同项目/换项目/空历史/运行中）、解绑、root 转发、缺能力文案、投影拷贝 |

## 核心实现

- `UseCase` 无状态：所有跨层能力经 `Deps` 注入，四组窄面分别是 `Store`
  （工作区存储/绑定）、`Sessions`（会话存储写作用域）、`Runtime`（项目根与任务账）、
  `View`（视图过渡锁 + 投影写面 + 发布）；其余单点能力用函数值注入。
- `bindInfo` 是绑定的唯一编排：运行门 → 草稿分支或普通分支 → （换项目时）
  `SaveCurrent` → `BindProjectRoot` → 新会话/context 挂接 → `BindSession` →
  `SetSessionWorkspace` → 投影 → 工作表格刷新 → 发布。
- `ViewPort` 的 `Apply*` 方法在宿主视图锁内完成写入并返回 revision；
  `PublishSnapshotChanged` / `RequestCatalogRefresh` / `ClearReviewRequest`
  由宿主在锁外执行，次序与迁移前一致。
- 读面共用一条 root 解析：`workspaceRootPort` 只在锁内拷贝 root，文件 I/O 在锁外；
  `FilePort` 同时服务文件预览、提交内文件内容与待审阅请求。
- `WorkspaceRootPort()` 把「当前绑定工作区根 + 后端本身」交给应用层，供**按仓库根
  查询**的编排用（多仓库 git）：根只有这一处可取，可选端口（如
  `contract.WorkspaceGitReposPort`）的断言与仓库相对路径的 containment 判据留在
  `application/workspace`（`application/workspace/application.go`）。本包同名的
  `GitLog/Changes/GitCommitDetail/GitCommitFileContent`（无 `repo` 参数）仍是
  「工作区根视角」的同一条读面，桌面端已改由应用层按仓库根直接调端口，两处语义
  一致（仓库根 = 工作区根时完全等价）。
- 可选能力用类型断言发现（树 / 文件 / 文件写入），未装配时返回固定文案；不会让
  只读后端被动升级成可写。

## 数据流 / 生命周期

```text
CreateWorkspace(name, root, remote)
  → 校验 → (remote 为空) Store.DetectGitRemote → Store.Create
  → bindInfo

CreateWorkspaces([{name, root, remote}, ...])
  → 整体校验（空值 / 重复根路径）
  → 逐项 Store.Create
  → 返回创建结果（不绑定、不切会话、不发布 snapshot）

BindWorkspace(id)
  → Store.Get → bindInfo

bindInfo
  → View.LockTransition
  → View.State（会话 ID / 草稿 / running / 当前工作区）
  → running ? 宿主 ErrChatRunning
  → 草稿：BindProjectRoot → Sessions.SetWorkspace → Projection → View.ApplyDraftBinding
  → 普通：History → 换项目且非空历史：
           Sessions.SetWorkspace(旧项目) → SaveCurrent
           BindProjectRoot → 新会话 → SetSystemPrompt → context 挂接
         Store.BindSession → Runtime.SetSessionWorkspace
         Projection → View.ApplyBinding
         （新会话）SwitchSessionTasks → ClearSubagentTree → RefreshWorkTable
  → PublishSnapshotChanged → RequestCatalogRefresh → ClearReviewRequest
```

## 依赖方向

允许：标准库、`contract`（`WorkspacePort` / optional 端口）、`contract/dto`、
`middle/protocol/model`。

禁止：`application/*`、`store`（具体类型）、`wiring`、`backend/*`。

检查方式：

```text
go list -deps ./middle/switch/workspace/ | select-string 'seelex/(application|store|wiring|backend)'
```

输出应为空（`contract`/`middle` 之外的 Seelex 包都不应出现）。

## 并发、存储与错误语义

- 本包**无状态、无锁**：视图过渡锁、`ViewMu` 与组件锁次序都由宿主决定；
  本包只在锁内读 `View.State`，不在持锁期间做存储/Git/文件 I/O。
- `Projection()` 在视图锁外调用 `Store.List` / `AllBindings`，并复制一份；
  视图只拿到副本，不会与存储共享可变切片/映射。
- 错误文案保持迁移前逐字一致：缺绑定 `worktree: no workspace bound to current session`、
  缺树端口 `worktree: workspace backend does not support tree listing`、
  缺文件端口 `worktree: workspace backend does not support file preview`、
  缺写端口 `worktree: workspace backend does not support file editing`、
  换项目保存失败 `save current session before switching project: %w`。
- 运行中拒绝返回宿主注入的 `ChatRunningError`（生产为 `application/core.ErrChatRunning`），
  保证 `errors.Is` 身份不变。

## 扩展方式

- 新增工作区读面：先在 `contract` 增加 optional 端口，再在本包加方法复用
  `workspaceRootPort`，最后在 `application/core` 加一行转发；不要另写 root 解析。
- 新增绑定分支：改 `bindInfo` 的真值表并在 `ViewPort` 增加窄方法；不要在应用层
  再写一份绑定编排。
- 新增投影字段：在 `Projection` 加字段，由 `applyWorkspaceProjectionLocked`
  写入 Snapshot；middle 不直接碰 Snapshot。

## Review 指南

- 本包是否重新 import 了 `application/*`、`store`、`wiring` 或 `backend/*`？
- root 解析、投影收集、绑定编排是否仍只有这一份（`application/core` 内不得有第二份）？
- 草稿分支是否仍未提前写 `BindSession`？普通会话换项目是否仍完整走
  `SaveCurrent → 新会话 → context 挂接 → task 账 → 工作表格`？
- 运行中/保存失败/缺端口三条路径是否仍在改视图之前返回？
- 用户可见错误文案是否与迁移前逐字一致？

## 测试与验证

```text
go test ./middle/switch/workspace/ -count=1
go test ./application/core/ -run 'Workspace|BindWorkspace|Review' -count=1
go test ./middle/switch/... ./store/workspace/ -count=1
```

`workspace_test.go` 覆盖：创建校验与 remote 探测、草稿绑定不落 session binding、
换项目另起会话（含旧项目保存、显式 ID、context 挂接、task 账与工作表格）、
同项目/空历史保持会话、运行中拒绝（宿主错误身份）、解绑、读面 root 转发、
可选端口缺失文案、投影深拷贝。

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`（本包不在已定案树自动发现内，
> 新增/改名函数时按同款格式手工同步）。

### workspace.go

- `type Projection struct` — 工作区清单与会话 → 工作区绑定的窄投影面。
- `type ViewState struct` — 工作区用例在视图过渡锁内读取的视图事实快照。
- `type ViewPort interface` — 应用视图的窄投影写面（锁、Apply*、发布与目录刷新）。
- `type Sessions interface` — 会话目录/绑定的窄写面（写作用域 + 保存当前会话）。
- `type Runtime interface` — 工作区绑定涉及的运行时窄端口（项目根、会话工作区、任务账、子代理树）。
- `type Engine interface` — 工作区切换涉及的引擎窄端口（历史、新建会话、system prompt）。
- `type Deps struct` — 工作区用例的装配入参（四组窄面 + 单点函数值）。
- `type UseCase struct` — 工作区用例的唯一编排点（无状态）。
- `func New(deps Deps) *UseCase` — 装配工作区用例。
- `func (usecase *UseCase) CreateWorkspace(name, rootPath, gitRemote string) error` — 建工作区（空 remote 探测 origin）并立即绑定。
- `func (usecase *UseCase) CreateWorkspaces(requests []dto.WorkspaceCreateRequest) (dto.WorkspaceCreateBatchResult, error)` — 批量登记项目且不绑定当前视图。
- `func (usecase *UseCase) BindWorkspace(workspaceID string) error` — 把已存在的工作区绑定到当前视图会话/草稿。
- `func (usecase *UseCase) UnbindWorkspace()` — 解绑当前视图会话/草稿的工作区。
- `func (usecase *UseCase) Projection() Projection` — 在视图锁外收集工作区清单与会话绑定。
- `func (usecase *UseCase) Tree(relPath string, depth int) (dto.TreeListing, error)` — 列出当前工作区某目录的子条目。
- `func (usecase *UseCase) FileCount() (dto.TreeCount, error)` — 统计当前工作区文件/目录数。
- `func (usecase *UseCase) GitLog(limit int) (dto.GitLogResult, error)` — 返回当前工作区最近 limit 条提交。
- `func (usecase *UseCase) Changes(limit int) (dto.WorkspaceChangesResult, error)` — 返回当前工作区未提交改动。
- `func (usecase *UseCase) GitCommitDetail(hash string, limit int) (dto.GitCommitDetail, error)` — 返回某个提交改了哪些文件。
- `func (usecase *UseCase) FileContent(relPath string, limit int64) (dto.FileContent, error)` — 读取当前工作区某文件的前 limit 字节。
- `func (usecase *UseCase) GitCommitFileContent(hash, relPath string, limit int64) (dto.FileContent, error)` — 读取某文件在某个提交时的内容。
- `func (usecase *UseCase) WriteFile(relPath, content string) (dto.FileWriteResult, error)` — 覆盖当前工作区某文件的全部内容。
- `func (usecase *UseCase) FilePort() (contract.WorkspaceFilePort, string, error)` — 解析当前工作区 root 与可选文件端口。
