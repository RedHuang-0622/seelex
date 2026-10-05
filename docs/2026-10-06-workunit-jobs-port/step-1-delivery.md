# 步骤① 交付记录：workunit 的作业端口扩成完整作业面（只定接口，不迁移）

一次性工作包（`docs/2026-10-06-workunit-jobs-port/`）。任务书 = `docs/arch/workunit-ports-and-assembly.md`
§5①（六项）；口径 = `workunit-single-lifecycle-one-implementation.md`（§3 接口先行 / §7 纪律）。
基线：main 头 = `3e5a167`（五个交接点 `3e5a167` / `82e4361` / `17f37c4` / `599aef4` / `c4492ab` 已核对，
开工时工作区干净）。

一句话结论：**作业面从"只有 `Reclaim`"扩成完整作业面（四格 + 合成），两个实现被编译器钉住；
生命周期实现退化成"只持端口"；读面定形；机械门禁落地（带阴性对照）。一张表都没搬。**

---

## 1. 改动文件清单

| 文件 | 改动 | 性质 |
|---|---|---|
| `seelebridge/workunit/contract.go` | `Jobs` 从 1 个方法扩成**四格 + 合成**（8 个方法）；`var _ Jobs = (jobs.Manager)(nil)` 保留原处；注释写清 `Peek`=增量读不推进游标 / `Snapshot`=全量 / `Reclaim`=按 scope | 接口 |
| `seelebridge/tools/async_exec.go` | **只加结构证据行**：`var _ workunit.JobSignals = (*asyncRegistry)(nil)` + 一行 import + 一段说明注释；**没有任何行为语句**（该表行为零变化） | 结构证据 |
| `seelebridge/workunit/progress.go` | **新增**：`Stage` / `EncodeStages`·`DecodeStages` / `Progress` / `ProgressOf` / `UnitReader`（读面定形，无生产调用者） | 新增 |
| `seelebridge/workunit/progress_test.go` | **新增**：读面形状证据（编解码唯一一份 + 有界、折算不编造、按 nodeID 读法 + 归属过滤、四格逐格由 `jobs.Manager` 满足） | 新增用例（既有用例一条未动） |
| `seelebridge/workunit_parent.go` | **重写**：`lifecycleHost` 只持三格端口（`scenes` / `units` / `records`），只剩判据；具体类型与按层分支全部移出（200 行） | 装配（行为不变） |
| `seelebridge/workunit_assembly.go` | **新增**：装配表——三格端口接口 + 唯一实现 `hostPorts` + 子代理层生产驱动点（`beginNodeUnit` / `finishNodeUnit` / `resolvedNodeScope` / `nodeWorktreeFor`，从 `workunit_parent.go` 移入） | 新增 |
| `e2e/workunit_ports_test.go` | **新增**：机械门禁（按层分支 / 具体类型 / 每个实现的编译期断言 / 契约包 worktree 依赖例外）+ 阴性对照 | 新增门禁 |
| `seelebridge/README.md` | 补 workunit 一节（一份实现 + 三格端口 + 装配处 + 五个文件职责） | 文档 |
| `seelebridge/workunit/README.md` | 文件表补 `progress.go`；新增「一之三、作业面」；验证命令补机械门禁 | 文档 |
| `docs/arch/workunit-ports-and-assembly.md` | §2 第 1 行改成落地形状；§3 补①落地版（三格宿主端口 + 记录形状例外）；§5 补① 落地记录 + 三处**文档口径更正** | 文档 |
| `docs/2026-10-06-workunit-jobs-port/step-1-delivery.md` | 本文件 | 文档 |

**未动**（非目标，逐条核查过）：两张作业表（没有迁移）、`seelebridge/worktree/**`、Seele 的
`jobs` 包（vendor）、前端读面渲染、调度闸门、`seelebridge` 的 `session/` 与
`workunit_team_records.go` 里的三份手写折算（归步骤②）。

---

## 2. 新旧接口签名对照

### 2.1 作业面（`seelebridge/workunit/contract.go`）

```go
// 旧（只有一格）
type Jobs interface {
	Reclaim(ctx context.Context, scope jobs.Scope) error
}
var _ Jobs = (jobs.Manager)(nil)

// 新（四格 + 合成；方法名 = jobs.Manager 的名字，概念别名见各自注释）
type JobSubmitter interface {                       // 概念：提交
	Dispatch(ctx context.Context, spec jobs.Spec) (jobs.Handle, error)
}
type JobReader interface {                          // 概念：读数（状态 / 增量 / 全量）
	Observe(handle jobs.Handle) (jobs.Record, bool)                                  // = Status；不推进游标
	Peek(ctx context.Context, handle jobs.Handle, budget jobs.FetchBudget) (string, jobs.Record, error) // 增量、不推进游标、不销项
	Snapshot(scope jobs.Scope) []jobs.Record                                         // 全量
}
type JobController interface {                      // 概念：控制（取消 / 销项 / 按作用域回收）
	Kill(ctx context.Context, handle jobs.Handle) error
	Done(ctx context.Context, handle jobs.Handle) error
	Reclaim(ctx context.Context, scope jobs.Scope) error                            // 按 scope
}
type JobSignals interface {                         // 概念：变更信号
	Events() <-chan struct{}
}
type Jobs interface { JobSubmitter; JobReader; JobController; JobSignals }
var _ Jobs = (jobs.Manager)(nil)                    // 原处保留
```

**口径更正（已写回任务书 §5①）**：任务书原文把方法面写成 `Submit` / `Status`，那是**概念名**。
契约里的 Go 方法名必须是 `jobs.Manager` 的名字（`Dispatch` / `Observe`），否则
`var _ Jobs = (jobs.Manager)(nil)` 这条钉不住——接口是结构化的，名字也是结构的一部分。

### 2.2 第二个作业面实现（`seelebridge/tools/async_exec.go`）

```go
// 旧：没有针对作业面的编译期断言
// 新：只加结构证据（源码 diff = 1 行 import + 1 段说明注释 + 1 行断言；无任何行为语句）
var _ workunit.JobSignals = (*asyncRegistry)(nil)
```

它今天与作业面**同形的只有"变更信号"这一格**（同一形状 `<-chan struct{}`、同一语义：容量 1 +
latest-wins，派发 / 终态 / 新字节各发一次）。其余七格的名字与签名都不同（见 §6）。

### 2.3 生命周期实现（`seelebridge/workunit_parent.go`）

```go
// 旧：持宿主 + 两个端口，且直接承认 teamwork / worktree / sessionstore 的具体类型
type lifecycleHost struct {
	r      *Runtime                       // 宿主：BindWorkspace / beginNodeWorktree / coordinatorForSession …
	ledger workunit.SessionLedger         // 账本端口
	spaces *worktree.WorktreeManager      // ✗ 具体类型
}
func newLifecycleHost(r *Runtime) *lifecycleHost

// 新：只持三格端口（字段仍不导出）；具体类型与按层分支都不在本文件
type lifecycleHost struct {
	scenes  sceneFace       // 现场：建 / 合并 / 认领 / 拆单体 / 在册读数
	units   unitFace        // 编排闸门 + 账本：归属 / 重入 / 回执 / 单个回收 / 回灌
	records recordFace      // 会话记录：状态读数 / 回灌读数 / 落盘 / 写终态 / 清记录
}
func newLifecycleHost(r *Runtime) *lifecycleHost   // 装配处：缺宿主 = 三格都不装（显式失败）

// 装配处（新文件 seelebridge/workunit_assembly.go）：三格端口的唯一实现
type sceneFace interface {
	BeginScene(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string) (workunit.Scene, error)
	MergeScene(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string) error
	ReleaseScene(nodeID string)
	AdoptScenes(ctx context.Context, sessionPath string) error
	SceneRegistered(nodeID string) bool
}
type unitFace interface {
	Owned(ctx context.Context, sessionPath string, own workunit.Ownership) bool
	Settled(ctx context.Context, sessionPath, itemID string) (bool, error)
	SettleUnit(ctx context.Context, sessionPath string, own workunit.Ownership, resultErr, mergeErr error) (workunit.Outcome, error)
	ReclaimUnit(ctx context.Context, sessionPath, role string) error
	RecoverUnits(ctx context.Context, sessionPath string) (workunit.Resume, error)
}
type recordFace interface {
	Status(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string) (string, bool, error)
	Readout(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string) (workunit.Resume, error)
	SaveRecord(ctx context.Context, own workunit.Ownership, nodeID, sessionPath, status, summary, stage string)
	SettleRecord(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string, outcome workunit.Outcome)
	Clear(ctx context.Context, own workunit.Ownership, nodeID, sessionPath string) error
}
type hostPorts struct{ r *Runtime }   // 唯一实现（具体类型只允许出现在这个文件）
```

`Lifecycle` 面**一个字节都没改**（`Begin`/`Finish`/`Reclaim`/`Recover`/`AlreadySettled`/`Notice`），
两个注册点（`workunit_node.go` / `workunit_team.go`）与生产链路（`runtime_teamwork_items.go:42`、
`workunit_parent.go` 的驱动点）签名不变——因此既有用例一条都不用动。

### 2.4 读面（`seelebridge/workunit/progress.go`，新增）

```go
type Stage struct{ Stage, Preview string }
func EncodeStages(stages []Stage) []byte      // 唯一一份编码（含 rune 上限、空阶段丢弃、空表 → nil）
func DecodeStages(payload []byte) []Stage     // 唯一一份解码（空 / null / 解不动 → nil，不编造）
type Progress struct{ Kind; NodeID; SessionID; Goal; Status; InFlight; Summary; Error; Stages; Worktree; UpdatedAt }
func ProgressOf(kind Kind, record sessionstore.NodeSessionRecord) Progress

type UnitReader struct{ ... }
func NewUnitReader(ledger SessionLedger, projectID, mainSessionID string, kind Kind,
	owned func(sessionstore.NodeSessionRecord) bool) *UnitReader
func (r *UnitReader) Read(nodeID string) (Progress, bool, error)
func (r *UnitReader) List() ([]Progress, error)
```

**口径更正**：读面勘定（`workunit-progress-read-surface.md` §2.1）给的是三参构造，但记录形状里
**没有 `Kind` 这一格**，而两层**共用同一张记录表**——三参版只能替调用方猜归属，正是该文档 §U5
要避免的。落地版多两个入参：`kind`（这一层的读数标注）与 `owned`（归属过滤，nil = 这张账本只装
这一层）。已写回 `workunit-ports-and-assembly.md` §5。

---

## 3. 编译期断言与机械门禁位置（文件:行）

### 3.1 编译期断言

| 断住什么 | 位置 |
|---|---|
| 完整作业面的实现 = Seele 的 `jobs.Manager`（原处保留） | `seelebridge/workunit/contract.go:277` — `var _ Jobs = (jobs.Manager)(nil)` |
| tools 自建表与作业面同形的那一格（变更信号） | `seelebridge/tools/async_exec.go:197` — `var _ workunit.JobSignals = (*asyncRegistry)(nil)` |
| 生命周期实现就是契约那份实现 | `seelebridge/workunit_parent.go:51` — `var _ workunit.Lifecycle = (*lifecycleHost)(nil)` |
| 装配处三格端口各有实现 | `seelebridge/workunit_assembly.go:106-108` — `_ sceneFace = (*hostPorts)(nil)` / `_ unitFace` / `_ recordFace` |
| 会话账本端口 = 既有存储（先例，未动） | `seelebridge/workunit/session.go` — `var _ SessionLedger = (*sessionstore.NodeSessionStore)(nil)` |
| 作业面四格逐格由 `jobs.Manager` 满足（用例侧） | `seelebridge/workunit/progress_test.go:24-29` |

### 3.2 机械门禁（`e2e/workunit_ports_test.go`）

| 判据 | 位置 |
|---|---|
| 门禁本体（扫真源码） | `TestWorkunitPortGate`（`:170`） |
| 扫描函数：生命周期实现里**不许按层分支 / 不许出现团队·现场·会话的具体类型**（AST 走 `go/parser`，注释与字符串不误报） | `scanLifecycleImplementation`（`:76`） |
| 存在性判据：每个实现都要有编译期断言 | `requireSnippet`（`:121`）/ `requireSnippetCount`（`:129`） |
| 已登记例外：契约包里 import `worktree` 的只允许 `classify.go` | `worktreeDependentsInWorkunit`（`:137`） |
| **阴性对照**（故意违规样例必须让它变红 + 干净样例必须过） | `TestWorkunitPortGateCatchesViolations`（`:224`） |

阴性对照里的三条样例分别对应三条判据：① 按层分支（`if u.Kind() == workunit.KindTeammate`）+
② 具体类型（`import …/teamwork` 与 `teamwork.WorkerRequest{}` 两种形态）+ ③ 断言缺席（拿掉
`var _ Jobs = (jobs.Manager)(nil)` / 装配处三格断言）。样例是**在用例内喂给同一套扫描函数**的，
不靠"我肉手改一次源码试过"。

**另做了一次真源码对照**（做完即还原，工作区最终干净）：临时在 `seelebridge/workunit_parent.go`
里加一行 `var _ = workunit.KindTeammate`，门禁立刻红：

```text
--- FAIL: TestWorkunitPortGate (0.01s)
    workunit_ports_test.go:213: seelebridge/workunit_parent.go: 生命周期实现里出现按层分支证据 KindTeammate
FAIL github.com/RedHuang-0622/seelex/e2e  0.559s
```

还原后同一条命令恢复 `ok`（`e2e 0.591s`），`gofmt -l seelebridge/workunit_parent.go` 无输出。

---

## 4. 六条命令原始读数

环境：Windows + PowerShell，`go 1.25.8`，仓库 `G:/Program/go/seelex`，工作区基线 `3e5a167`。

```text
=== ① gofmt -l（改动文件）===
（无输出）
exit=0

=== 备注：把范围放大到 gofmt -l seelebridge e2e 会列出 8 个**非本轮**文件 ===
seelebridge/runtime_role_turn.go
seelebridge/runtime_role_turn_test.go
seelebridge/runtime_teammate_session_live_test.go
seelebridge/runtime_teamwork_board_archive_test.go
seelebridge/runtime_teamwork_board_test.go
seelebridge/runtime_teamwork_close_test.go
seelebridge/runtime_teamwork_schema_test.go
seelebridge/teamwork/teamwork.go
（`git status` 显示这 8 个文件**本轮未改动** ⇒ 它们是基线就有的形态差异，不是本轮引入；
本轮改动文件一个都不在列表里。按"不混入无关改动"的提交约束，不顺手格式化它们。）
```

=== ② go build ./... ===
（无输出）
exit=0

=== ③ go vet ./seelebridge/... ===
（无输出）
exit=0

=== ④ go test ./seelebridge/... -count=1 ===
ok  github.com/RedHuang-0622/seelex/seelebridge              113.767s
ok  github.com/RedHuang-0622/seelex/seelebridge/account        3.741s
ok  github.com/RedHuang-0622/seelex/seelebridge/attachment     3.305s
ok  github.com/RedHuang-0622/seelex/seelebridge/fork           2.882s
ok  github.com/RedHuang-0622/seelex/seelebridge/fs             2.342s
ok  github.com/RedHuang-0622/seelex/seelebridge/imageattach    3.074s
ok  github.com/RedHuang-0622/seelex/seelebridge/internal/actor 1.693s
ok  github.com/RedHuang-0622/seelex/seelebridge/internal/config 1.781s
ok  github.com/RedHuang-0622/seelex/seelebridge/internal/docker 6.973s
?   github.com/RedHuang-0622/seelex/seelebridge/internal/mapper [no test files]
?   github.com/RedHuang-0622/seelex/seelebridge/internal/model  [no test files]
ok  github.com/RedHuang-0622/seelex/seelebridge/internal/stream 1.754s
ok  github.com/RedHuang-0622/seelex/seelebridge/internal/telemetry 5.119s
ok  github.com/RedHuang-0622/seelex/seelebridge/mcp            6.637s
ok  github.com/RedHuang-0622/seelex/seelebridge/multimodal     9.081s
ok  github.com/RedHuang-0622/seelex/seelebridge/node           3.868s
ok  github.com/RedHuang-0622/seelex/seelebridge/plan           3.799s
ok  github.com/RedHuang-0622/seelex/seelebridge/plugin         2.624s
ok  github.com/RedHuang-0622/seelex/seelebridge/scheduler      9.807s
ok  github.com/RedHuang-0622/seelex/seelebridge/search         8.454s
ok  github.com/RedHuang-0622/seelex/seelebridge/security       4.313s
ok  github.com/RedHuang-0622/seelex/seelebridge/session        1.724s
ok  github.com/RedHuang-0622/seelex/seelebridge/task           2.525s
ok  github.com/RedHuang-0622/seelex/seelebridge/teamwork       6.971s
ok  github.com/RedHuang-0622/seelex/seelebridge/tools          29.355s
ok  github.com/RedHuang-0622/seelex/seelebridge/tools/computer 2.541s
ok  github.com/RedHuang-0622/seelex/seelebridge/tools/computer/mcp 5.220s
ok  github.com/RedHuang-0622/seelex/seelebridge/tools/websearch 6.878s
ok  github.com/RedHuang-0622/seelex/seelebridge/worktree       35.879s
ok  github.com/RedHuang-0622/seelex/seelebridge/workunit       4.030s
exit=0

=== ⑤ 机械门禁（e2e/workunit_ports_test.go）===
=== RUN   TestWorkunitPortGate
--- PASS: TestWorkunitPortGate (0.01s)
=== RUN   TestWorkunitPortGateCatchesViolations
--- PASS: TestWorkunitPortGateCatchesViolations (0.00s)
PASS
ok  github.com/RedHuang-0622/seelex/e2e  0.601s

=== ⑥ 布局门禁（e2e 全包：模块 README / 链接 / mermaid / 插件布局 / 门禁）===
ok  github.com/RedHuang-0622/seelex/e2e  6.737s
exit=0
```

（`go vet ./...` 与根包 `go test ./ -count=1` 的读数见 §4.1，作为改动面之外的旁证。）

### 4.1 改动面之外的旁证

```text
=== go vet ./... ===
VET_EXIT=0

=== go test ./ -count=1（根包）===
ok  github.com/RedHuang-0622/seelex  64.746s
ROOT_TEST_EXIT=0
```

另有一条**手工核对**（判据 C 的机器化形态已进 ⑤ 的门禁，这里给出等价的人工读数）：

```text
> Select-String -Path seelebridge\workunit_parent.go -Pattern "teamwork\.|worktree\.|sessionstore\.|seelebridge/session|KindTeammate|KindSubagent"
（无命中）
```

---

## 5. 删除清单

**无。** 本轮**没有删掉任何重复实现**，原因不是"没找到"，而是 ① 的**分界本身就是"只定接口、
不迁移"**：本轮已确认的重复实现，删除动作全都落在下一步：

| 已确认的重复（现状锚点） | 为什么这轮不动 | 唯一位置什么时候定 |
|---|---|---|
| 「记录 → 进度折算」三份手写：`seelebridge/session/subagent_sessions.go`（写侧 `buildRecordLocked` / 读侧 `restoreLocked`）、`seelebridge/workunit_team_records.go`（`saveTeamUnitRecord` / `teamUnitRecords`） | 折的合并**跨 `session/` 与根包**，会改两条链的落盘/回读路径 = 行为迁移 | 唯一位置已定形：`workunit.ProgressOf`（本轮） → 步骤② 转调 |
| `StagesJSON` 的编解码两份：`subagent_sessions.go` 的 `[]model.NodeStageLog` 与 `workunit_team_records.go` 的 `teamUnitStages` | 同上 | 唯一位置已定形：`workunit.EncodeStages`/`DecodeStages`（本轮） → 步骤② 转调 |
| 两张作业表：`jobs.Manager`（teammate）与 tools 自建表（subagent / `bash_bg`） | 七点语义差异未解决（`CHANGELOG.md:112-116`），搬表是步骤② | 唯一位置已定形：`jobs.Manager`（`var _ Jobs = (jobs.Manager)(nil)` 钉着） |

本轮的结构收益不是"删掉一份"，而是：**具体类型与按层分支从生命周期实现里消失（可机器验证）、
三个作业面/生命周期实现各带编译期断言、读面与两个实现候选的唯一位置先立住**——步骤② 的删除动作
因此只剩"转调 + 删旧体"，不需要再设计形状（这正是"先定接口"要买的东西）。

---

## 6. 未决项

### 6.1 判据 D 的明说：`workunit/progress.go` **没有生产调用者**

只有形状，没有接线。原因与锚点：

- 三份手写折算的**合并**归步骤②（任务书 §5①.4），本轮只把读法定形：`ProgressOf` 收编
  `subagent_sessions.go` 的写侧兜底/读侧还原与 `workunit_team_records.go` 的折算；
  `EncodeStages`/`DecodeStages` 收编两个载体的编解码。
- 因此 `Progress` / `UnitReader` 的消费方（前端"这件事跑到哪"、teammate 详情）在步骤② 才有；
  现在**没有任何生产调用点**（`grep -n "NewUnitReader\|ProgressOf(" seelebridge application gui tui`
  只命中 `workunit/progress.go` 与 `workunit/progress_test.go`）。
- 不把它挂到 `Unit` 方法面上（读面勘定 §5.3 的结论）：`Unit` 适配器今天同样没有生产调用者，
  给"没人调的接口"再加四个方法只会得到四个空实现。

### 6.2 tools 自建表为承载完整作业面还缺哪几个方法（步骤② 的先行项）

缺 **7 个**（8 格只同形 1 格）。逐格对照（表② = `seelebridge/tools`，表① = Seele `jobs.Manager`）：

| 作业面格 | 表①（`jobs.Manager`） | 表②（tools 自建表）今天 | 缺什么 |
|---|---|---|---|
| 变更信号 `Events` | `Events() <-chan struct{}` | `asyncRegistry.Events()`（`async_exec.go:182`） | **同形，无缺口**（本轮已钉住） |
| 提交 `Dispatch` | `Dispatch(ctx, Spec) (Handle, error)` | `beginJob(spec JobSpec) (asyncRun, bool, error)`（未导出）；对外是 `JobTool.Add` → `[]byte` | 名字、入出参类型、**scope 模型**（`JobSpec.SessionID` vs `jobs.Scope{Session,Subject}`） |
| 状态 `Observe` | `Observe(Handle) (Record, bool)` | `jobManager.Status(ctx, JobHandle) ([]byte, error)`；`asyncRegistry.snapshot`（未导出） | 无 `Record` 形状（载荷是 JSON 字节）；无按 scope 的授权判定 |
| 增量读 `Peek` | `Peek(ctx, Handle, FetchBudget) (string, Record, error)` | **没有**（只有两阶段的 `advanceTail` 取增量 + `markCursor` 推游标） | 缺 `Peek` 这个"不推进游标"的读法本身（七点不一致第 3 点） |
| 全量读 `Snapshot` | `Snapshot(Scope) []Record` | `Router.AsyncRuns() []AsyncRunInfo`；`asyncRegistry.infos()`（未导出） | 名字、形状；scope 通配语义（只有会话维度） |
| 取消 `Kill` | `Kill(ctx, Handle) error` | `jobManager.Kill(ctx, JobHandle) ([]byte, error)`；`killHandle`（未导出） | 签名（载荷 vs error）、scope 授权 |
| 销项 `Done` | `Done(ctx, Handle) error` | `jobManager.Done(ctx, JobHandle) ([]byte, error)`；`retire`（未导出） | 同上；另：管理器不保留可读的销项状态字面量（七点不一致第 5 点） |
| 按作用域回收 `Reclaim` | `Reclaim(ctx, Scope) error` | `killSession(sessionID) int`（未导出）；对外 `Router.CloseSessionAsync` | 无 `Subject` 维度、返回计数而非 error、名字不同；回收时机也不同（会话销毁 vs 团队收口） |

**结论（步骤② 的先行项）**：(a) 先按 `teamwork-leader-worker-architecture.md` §12 的"六个最小 `jobs`
增补"解决七点语义差异（多数是语义而非搬家）；(b) 再让 tools 表在**行为逐字不变**的前提下补出上面
7 格（新名字/新形状的面），(c) 期间既有 `async_*_test.go` 一条不改。

### 6.3 生命周期实现**不持**作业面端口（有意的空缺）

`lifecycleHost` 的三格里没有作业面。原因：作业面的回收今天落在编排那一格内部
（`Coordinator.reclaimStepsLocked` 步 1 是全仓**唯一**的作业回收调用点），宿主再挂一个作业面字段
并调用它就是**第二条回收调用点**（同一件事两份实现 + 与编排那份抢时机）。把作业面直接接到宿主上
属于步骤②（两张表合一）落地后的装配变更：那时 `ReclaimUnit` 那一格只清账本，作业回收由宿主按
scope 直接驱动。已写进任务书 §3 的「① 落地版」，并进 `seelebridge/workunit_assembly.go` 文件末尾的说明。

### 6.4 已登记例外与未动的既有重复（按要求只记录、不顺手改）

1. **`seelebridge/workunit/classify.go` 依赖 `worktree` 的两个哨兵错误**
   （`ErrUncommittedChanges` / `ErrMergeBlockedByMain`）。搬它们等于改 `worktree` 的公共 API，
   本轮明确不动；门禁把"契约包里 import worktree 的只允许 classify.go"钉住（`e2e/workunit_ports_test.go:137`）。
2. **记录形状**：`workunit_progress` / 端口签名里的 `sessionstore.NodeSessionRecord` / `Key` **不是**
   "持具体类型"——契约自己声明的记录形状就是它（`SessionLedger` 的编译期断言钉着同一份），
   另立第二种记录才是"第二份真相"。判据 C 的机器化口径因此是"**实现文件里不出现 teamwork /
   worktree / sessionstore 的选择器与 import**"，已进 ⑤。
3. **`workunit_team.go`（注册点）仍持 `teamwork.WorkerRequest`**：它是"派发时交进来的载荷"这一读数，
   与子层"不碰写端口"的纪律不冲突；判据 C 盯的是**生命周期实现**（`workunit_parent.go`），
   注册点的收口不在本轮（要动它就会动 `runtime_teamwork_items.go` 的派发链路）。
4. **`workunit_team_records.go` 里的三份折算之一**（`saveTeamUnitRecord` / `teamUnitRecords`）与
   子代理侧的两处字面量（`runtime_subagent_resume.go` 的 `"queued"|"running"`、
   `subagent_sessions.go:515`）都**未收**——按勘定 R1/R3，归步骤②。

### 6.5 读面勘定的其余不确定项（本轮回答一个，其余仍未验证）

- **U5（已由本轮回答）**：`UnitReader` 的归属问题——记录形状里没有 `Kind`、两层共用一张表，
  因此归属过滤必须由调用方给出（`NewUnitReader` 的 `owned` 参数），读面不替调用方猜。
- **U2（仍开放）**：`Progress` 的 `StartedAt/EndedAt` 对 teammate 恒零值（teammate 刻意不写记录
  时间戳，事实归计划的 `item.StartedAt/FinishedAt`）——读面的消费方要从计划取时间，尚未接线。
- **U3 / U4（仍开放）**：写侧兜底转调 `ProgressOf` 是否会改变落盘语义（需 `TestSubagentPersist` 与
  `workunit_team_test.go` 的"单元素打点"断言一起回归）；`teamUnitStages` 单元素 → 时间线会改回灌文案。
  两条都在步骤② 的收编里验。
- **U6（仍开放）**：统一实时事件（第二批）的粒度上限需先写"事件载荷字段表"（`assistant` 正文增量
  只有子代理侧有）。

---

## leader 复核（2026-10-06，verdict：**PASS**）

按判据 A–H 对**源码**逐条复核（不引用本文件自述），逐条给锚点：

| 判据 | 结论 | 复核锚点 |
|---|---|---|
| A 完整作业面 + 断言在原处 | ✔ | `workunit/contract.go:219/228/241/253/263`（四格 + 合成）＋ `:277 var _ Jobs = (jobs.Manager)(nil)` |
| B tools 表断言（行为零变化） | ✔ | `tools/async_exec.go:197 var _ workunit.JobSignals = (*asyncRegistry)(nil)`；diff = 1 行 import + 注释 + 断言行 |
| C 实现里无具体类型 | ✔ | 机械门禁 `e2e/workunit_ports_test.go:170` + 阴性对照 `:224`；装配处断言 `workunit_assembly.go:106-108` |
| D 读面定形 + 是否有生产调用者明说 | ✔ | `workunit/progress.go:49/58/81/115/141/165`；§6.1 明说"无生产调用者"（并给 grep 依据） |
| E 六条原始读数 | ✔ | §4（另附 §4.1 旁证：`go vet ./...`、根包测试）|
| F async 用例一条未改 | ✔ | 触碰的文件只有 `tools/async_exec.go`（断言行），无 `async_*_test.go` |
| G 删除清单 | ✔（"无"+ 逐条说明为什么 ① 注定没有）| §5 |
| H 未决项（tools 表缺几格） | ✔ | §6.2 缺 **7 格**，逐格对照 |

独立复核门禁由 leader 自己在合并后重跑（`build` / `vet` / e2e 全包含新机械门禁 / workunit·tools·teamwork·worktree·session 定向），读数记在
`step-2-handoff-prompt.md` 头部——交付自述与独立读数分开列，互不背书。

**一处口径纠正（已改回任务书）**：本文件 §6.2 把"两张作业表合一"列为步骤② 的先行项。`docs/arch/teamwork-leader-worker-architecture.md`
**§12.4（2026-10-01 裁决）** 已否决该路径（`event.Sink` 必须构造期定死 + 框架全局序号 vs Seelex 按会话追加），
并明写"**本步不再开工、§12.3 降级为留档、不构成待办**"。因此 ② 收窄为"并判据/读法/容器"，**不含合表**；
`docs/arch/workunit-ports-and-assembly.md` §5② 与本文 §6.2 结论的重叠部分以任务书为准。
