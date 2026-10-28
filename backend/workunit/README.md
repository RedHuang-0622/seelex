# workunit：工作单元的生命周期契约与唯一实现

## 生态位

`backend/workunit` 是**异步作业的"工作单元"面**：现场、会话、作业，以及**生命周期的唯一一份
实现**。它不装执行面（`Run` / 回合外壳 / 装配 / 调度 / 统一事件流一律不进这里），只回答
一个问题——

> 一件异步的活，**怎么开始、怎么收尾、怎么回收、重启后怎么接着做**，三层是不是同一套说法。

主要调用方：装配器 [`backend/engine/assemble`](../engine/assemble/README.md)（两个注册点
`nodeWorkUnit` / `teamUnit` 在这里注册，`hostPorts` 在这里实现三格端口）、
[`backend/multiagent/subagent`](../multiagent/subagent/README.md)（记录状态词表与在跑判据）、
[`backend/tool/node`](../tool/node/README.md)（收尾分类）、
[`backend/tool/worktree`](../tool/worktree/README.md)（两个哨兵错误，反向依赖本包）、
[`backend/tool/registry`](../tool/registry/README.md)（作业面同形那一格）、
[`backend/multiagent/teamwork`](../multiagent/teamwork/README.md)（teammate 收尾分类与 `Ownership` 折算）、
[`internal/telemetry`](../../internal/telemetry/README.md)（阶段预览裁剪）。

物理位置：2026-10-16 按
[plan.md](../../docs/2026-10-15-architecture-reshape/plan.md) §2.7 落地——
**契约**从 `seelebridge/workunit/` 搬来（包名不变），**唯一实现**从装配包下沉进同一个包：
`backend/engine/assemble/workunit_parent.go` **已删除**（更早的原路径 `seelebridge/workunit_parent.go` **也已删除**），
`git mv` 之后的唯一位置就是本包的 `lifecycle.go`。
两半合成一个包是刻意的：生命周期实现不认识任何具体宿主，只持三格端口（`ports.go`），
所以它和契约同处零实现依赖的位置；而端口实现（`hostPorts`）留在装配处，那里才有
`worktree` / `teamwork` / `store` 的具体类型。
落点与 plan §2.7 写的 `backend/multiagent/workunit/` 不同：进口方横跨
tool / multiagent / internal 三块（`backend/tool/worktree` 依赖本包的哨兵），放 multiagent
会让 tool 板出现跨板回指——记录见
[m4-record.md](../../docs/2026-10-15-architecture-reshape/m4-record.md) 的 D-M4-W1。

## 一、层链：只增不减

| 层 | 相对下层**增**了什么 | 现场 | 会话 | 收尾策略 |
|---|---|---|---|---|
| `job` | ——（基线） | 无 | 无（后台运行 + 结果回传） | 不适用：**不实现 `Unit`** |
| `subagent` | worktree 现场 + 一件活自己的异步会话 + 存储与合并纪律 | **临时**（派出即建、收尾即清） | 一件活一条，落 `store/workunit.SessionStore` | `Immediate` |
| `teammate` | 听 leader 调度 + 装配（plugin / 系统提示词 / skill 前缀复用） | 归 team | 归 team | `AtTeamClose` |

两条关键口径：

- **`job` 层不实现 `Unit`**：它不是"少一份现场的工作单元"，它根本不是工作单元（没有现场也没有
  持久会话）。让 job 去实现 `Unit` 只会得到五个空读数——那正是「为接口而接口」。作业面只在
  teammate 的 `Reclaim` 里被需要，端口是 `Jobs`。
- **一个 `Unit` = 一件事 = 一份现场 + 一条会话**。粒度差异不靠分支表达：teammate 的角色级现场
  就是 teammate 单元自己那一份，Work Item 级的现场属于该 Work Item 的 subagent 单元。于是
  `Reclaim` 永远只拆"我自己这一份"。

## 一之二、两个接口，方向相反（接口先行）

| 接口 | 是谁的面 | 形状 |
|---|---|---|
| `Lifecycle` | **父实现**的契约面（**一份实现**） | `Begin` / `Finish` / `Reclaim` / `Recover` / `AlreadySettled` / `Notice`，入参是 `Unit` |
| `Unit` | 层的**读数**（两层各一份数据） | `Kind` / `ID` / `SessionPath` / `Policy` / `Owns` —— 只有身份与策略，没有逻辑 |

- **依赖倒置**：调用方只依赖 `Lifecycle`，不依赖具体类型。实现（`lifecycle.go` 的
  `Host`）端口字段**不导出**；两个注册点在 `new` 时经 `NewHost` 注入它，**只持有
  `Lifecycle` + 自己的 `Unit` 读数**，方法体一律转发。要两层不同实现 = 再写一个
  `Lifecycle` 实现 + 改装配处一行。
- **归属用契约自己的小结构**：`Ownership`（teamID / itemID / role / milestone / roleSessionID /
  goal / worktree）。`teamwork.WorkerRequest` 不进契约包——依赖方向只能是
  `teamwork → workunit`，反过来就是把实现类型引进契约。


## 一之三、作业面（`Jobs`）：四格 + 一个合成，两张表只求同形

作业面回答的是"一件**在飞的活**"的形状：提交 / 读数 / 增量读 / 取消 / 销项 / 按作用域回收 /
变更信号。它按能力切成**四格**（`JobSubmitter` / `JobReader` / `JobController` / `JobSignals`），
`Jobs` 是四格的合成——"每个形态只装配它需要的那几格"（后台命令只提交 + 增量读；整队收口只要
按作用域回收；看板只要全量读 + 变更信号）。

| 事实 | 现状 |
|---|---|
| 词汇 | 用 Seele 的 `jobs`（`Spec`/`Handle`/`Record`/`Scope`/`FetchBudget`）；`github.com/RedHuang-0622/Seele/jobs` 是 vendored 的外部契约，**不改它**，只做窄投影 + 编译期断言（先例：`SessionLedger`） |
| 方法名 | 就是 `jobs.Manager` 的名字（`Dispatch` = 提交、`Observe` = 状态读数……）——换名字 `var _ Jobs = (jobs.Manager)(nil)` 这条就钉不住（接口是结构化的，名字也是结构的一部分） |
| 实现一（teammate） | `jobs.Manager`：**八格逐字对齐**，编译期断言在 `contract.go` |
| 实现二（subagent / `bash_bg`） | tools 自建 async 表：今天只在 **`JobSignals`** 那一格上与这里同形（`backend/tool/registry/async_exec.go` 的断言）；其余七格的名字与签名都不同 |
| 两张表 | **本轮只求同形，一张都不搬**——搬表是步骤②（`CHANGELOG.md:112-116` 已登记七点不一致，`docs/arch/workunit-ports-and-assembly.md` §5② 是路线） |

`docs/arch/workunit-ports-and-assembly.md` §2 把这一族端口与现场/会话/读面并列；生命周期实现
（`lifecycle.go` 的 `Host`）只持**三格宿主端口**（现场 / 编排账本 / 会话记录，见
`ports.go` 与 `backend/engine/assemble/README.md` 的 workunit 一节），作业面今天由编排那一格驱动（回收的唯一调用点在
`Coordinator.reclaimStepsLocked` 步 1），宿主直接驱动它是步骤② 的落点。

## 二、文件

| 文件 | 内容 |
|---|---|
| `contract.go` | `Kind` / `Scene` / `Result` / `Outcome(Kind)` / `Ownership`（归属读数）/ `Unit`（层读数：`Kind` `ID` `SessionPath` `Policy` `Owns`）/ `Lifecycle`（父实现契约面：`Begin` `Finish` `Reclaim` `Recover` `AlreadySettled` `Notice`）/ `FinishPolicy`（`Immediate`、`AtTeamClose`）/ **作业面四格 + 合成** `Jobs`（`JobSubmitter`：`Dispatch`；`JobReader`：`Observe` `Peek` `Snapshot`；`JobController`：`Kill` `Done` `Reclaim`；`JobSignals`：`Events`） |
| `classify.go` | `ClassifyFinish(result, mergeErr)`：三层**唯一一份**收尾分类（跑失败判死 → 未提交（不判死）→ 主工作区挡路（不判死）→ 其他合并错误判死 → 落定）+ `bounded` + 它认的两个哨兵 `ErrUncommittedChanges` / `ErrMergeBlockedByMain`（与 `Is*` 判据；定义在这里 = 依赖方向是 实现 → 契约） |
| `session.go` | `SessionLedger`（结构上由 `*store/workunit.SessionStore` 满足）/ `Resume` / `RecoveryNoteRole` / `RecoveryNotePrefix` / `RecoveryNote(kind, record)` |
| `progress.go` | 进度**读面**：`Stage` + `EncodeStages`/`DecodeStages`（打点载荷的唯一一份编解码 + `ClipPreview` 唯一一份预览裁剪）+ `Progress`/`ProgressOf`（记录 → 进度的唯一一份折算）+ `UnitReader`（按 nodeID 读一件事的进度：`Read`/`Record`/`List`/`Records`）。步骤②已把它接进生产读路（宿主记录读面 / 子代理恢复定位 / teammate 会话级读回） |
| `ports.go` | **宿主注入面**（三格窄端口）：`SceneFace`（建 / 合并 / 认领 / 拆 / 在册）、`UnitFace`（归属 / 重入 / 回执 / 回收 / 回灌）、`RecordFace`（状态读数 / 回灌读数 / 落盘 / 写终态 / 清记录）。实现留在装配处（`backend/engine/assemble` 的 `hostPorts`）：`worktree` / `teamwork` / `store` 的具体类型只允许出现在那里 |
| `lifecycle.go` | **唯一一份生命周期实现** `Host`：`Begin` / `AlreadySettled` / `Finish` / `Reclaim` / `Recover` / `Notice`，只持上面三格端口。判据全在这里（重入怎么回答、合并只做一次、恢复的粒度、要不要收作业），装配处只负责构造（`NewHost`）与挂在装配链的位置 |
| `recovery.go` | **恢复链的判据面**（P21-16 从 `backend/engine/assemble/{runtime_unit_recovery.go,resume_notes.go}` 归位）：`KindOf`（记录身份 → 哪一层：写明的身份优先 → 缺身份时按团队现场名单兜底 → 认不得的词交回调用方留痕）、`RecoveryStrategy`（一格策略：认哪些记录、恢复成什么）+ `RestoreDeps{SceneNodes, Strategies, Log}` + `RestoreUnits`（按身份切开、只喂非空批次、次序固定）、`ResumeNotes`（恢复说明容器：`Set` / `Take` 读完即消 / `Peek` 非消费读 / `Clear`；**键语义留在调用点**） |
| `recovery_test.go` | 身份判据的三段顺序（含未知身份词按既有链归类并交回留痕）、分派（批次 / 次序 / 空批幂等 / 无现场名单）、`ResumeNotes` 语义与并发安全（供 `-race`） |

**依赖方向**（步骤③E 之后）：契约包**不 import 任何实现包**——`classify.go` 认的两个哨兵错误
（`ErrUncommittedChanges` / `ErrMergeBlockedByMain`）现在**定义在契约里**（判据在哪、哨兵就在哪），
现场实现（`worktree`）反过来 import 契约去构造它们。`teamwork` / `session` / `node` 同理只被**反向**
依赖。原先"契约包依赖 `worktree` 的唯一一处已记录例外"（`classify.go`）已撤掉，
`e2e/workunit_ports_test.go` 把这条钉成**零命中**的硬判据（带阴性对照）。

`worktree` 侧保留了同名入口（`worktree.ErrUncommittedChanges` 等）作为**引用**而非定义：现场
的调用点读起来还是本包的话，而判据只剩契约那一份（`workunit.Is*`，`errors.Is` 链上是同一个值），
既有调用点与既有用例因此零改动。

## 三、五条不变式

1. **现场是人的资产**：未提交产出的现场**不许**被框架悄悄丢；只有"回收"这一条路能拆现场，
   而回收只有一个入口（teammate 侧 = `team_close`）。
2. **认领先于 `Prune`**：重启回灌必须先认领现场（`Recover` 跑完再 `worktree.Prune`），否则
   "干净但还没合并"的现场会被当孤儿连分支一起删。
3. **收尾分类只有一份**：`ClassifyFinish`。同一件"跑完了但没合进去"，不许在一条路上是警告、
   在另一条路上是判死（旧实现就是这样漂移的）。
4. **恢复说明只有一族**：`RecoveryNote` 的稳定前缀 + `system` 注入 role；三层在审计里因此是
   同一种东西，而不是三份自造说明。
5. **策略不是实现，只是调用点**：`Immediate` 与 `AtTeamClose` 调的是**同一个** `Lifecycle.Reclaim`
   ——析构只有一份（拆现场 + 清会话 + 回收作业），差别只在什么时候调：subagent 收尾当场调，
   teammate 留到整队收口调。

## 四、会话恢复（重启回灌）

1. **形状一致**：一个工作单元 = 一条 `store/workunit.Record`（`NodeID` 定位：
   subagent 是节点 id，teammate 是 `<role>-<itemID>`）；记录里的 `History` / `StagesJSON` /
   `ContextJSON` / `Worktree` 就是"跑到哪、现场在哪"。
2. **运行期落盘**：跑着就写（进程中断可从最近一次记录恢复），落定再写终态。
3. **认领先于 `Prune`**（不变式 2）。
4. **回灌 + 恢复说明**：历史灌回执行面并注入 `RecoveryNote`；记录说 `running/queued` 而本进程
   没有它的执行面 ⇒ **中断**，记进 `Resume.Interrupted`，交上层重跑或人工处置。

## 五、测试与验证

```powershell
go vet ./backend/workunit/
go test ./backend/workunit/ -count=1
go test ./e2e/ -run TestWorkunitPortGate -count=1   # 装配与作业面的机械门禁（含阴性对照）
```

用例守着的是**语义**而不是实现细节：分类表（含"两种收尾失败同时具备 → 未提交优先"）、
`AtTeamClose` 不得自己拆现场、`SessionLedger` 与既有存储的同名同签名（编译期断言）、
恢复说明的前缀族与有界性；`progress_test.go` 守读面（打点编解码只有一份、折算不编造事实、
按 nodeID 的读法与归属过滤）；`e2e/workunit_ports_test.go` 守装配（实现里没有按层分支与具体
类型、每个实现都带编译期断言、契约包 **import worktree 零命中**）。

生命周期实现自己的行为判据（重入交回零值结论、合并只做一次、落定才按策略回收、恢复的粒度）
由装配侧那几条跨层用例钉住（`backend/engine/assemble/workunit_single_path_test.go` 等）——
它们跑的是**真现场 + 真账本**，因此留在装配包的测试里，不在这里重写一遍假实现。

## 六、文件与函数索引

### contract.go

| 类型 / 函数 | 签名 | 摘要 |
|---|---|---|
| `Kind` / `KindJob` `KindSubagent` `KindTeammate` | `type Kind string` | 层链的标注（描述性，不是分支判据） |
| `Scene` / `Result` / `Outcome` / `Ownership` / `Progress`（见 `progress.go`） | `type ... struct` | 现场 / 一轮结果 / 收尾结论 / 归属读数 |
| `OutcomeKind` + `OutcomeSettled` `OutcomeUncommitted` `OutcomeMergeBlocked` `OutcomeFailed` | `type OutcomeKind string` | 收尾分类的词汇表（三层唯一一份） |
| `Unit` | `interface` | 一层的**读数**：`Kind` / `ID` / `SessionPath` / `Policy` / `Owns` |
| `Lifecycle` | `interface` | 父实现的契约面：`Begin` / `Finish` / `Reclaim` / `Recover` / `AlreadySettled` / `Notice` |
| `Jobs` / `JobSubmitter` / `JobReader` / `JobController` / `JobSignals` | `interface` | 作业面四格 + 合成（`jobs.Manager` 结构上满足，编译期断言钉住） |
| `FinishPolicy` / `Immediate` / `AtTeamClose` | `interface` | 层间唯一的策略差异：什么时候回收（调的是同一个 `Reclaim`） |
| `Immediate.AfterFinish` | `func (Immediate) AfterFinish(ctx context.Context, lifecycle Lifecycle, u Unit) error` | 落定即回收（subagent） |
| `AtTeamClose.AfterFinish` | `func (AtTeamClose) AfterFinish(context.Context, Lifecycle, Unit) error` | 不做事：回收留给 team_close（teammate） |

### classify.go

| 函数 | 签名 | 摘要 |
|---|---|---|
| `ClassifyFinish` | `func ClassifyFinish(result Result, mergeErr error) Outcome` | 三层唯一一份收尾分类（顺序即语义） |
| `IsUncommittedChanges` / `IsMergeBlockedByMain` | `func IsUncommittedChanges(err error) bool` | 两个哨兵的判据（唯一一份） |
| `remedyFor` | `func remedyFor(kind OutcomeKind) string` | 每一类结论的处置办法（非落定必带） |
| `boundedTo` / `bounded` | `func boundedTo(text string, limit int) string` | 说明按 rune 裁到有界 |
| `Outcome.Settled` | `func (o Outcome) Settled() bool` | 这次收尾算不算落定（可进验收） |
| `Outcome.String` | `func (o Outcome) String() string` | 日志 / 看板投影（`kind: notice`） |

### session.go

| 函数 | 签名 | 摘要 |
|---|---|---|
| `SessionLedger` | `interface` | 会话记录端口（`*store/workunit.SessionStore` 结构上满足） |
| `Resume.Empty` | `func (r Resume) Empty() bool` | 这次回灌什么都没发生（正常读数） |
| `InFlight` | `func InFlight(status string) bool` | "记录说自己在跑"的唯一判据（词表引 `dto.SubAgent*`） |
| `RecoveryNote` | `func RecoveryNote(kind Kind, record storeworkunit.Record) string` | 恢复说明的唯一构建器（事实在前、动作在后，有界） |
| `StatusQueued` / `StatusRunning` | `const` | "在跑"子集（直接引契约枚举） |

### progress.go

| 函数 | 签名 | 摘要 |
|---|---|---|
| `EncodeStages` / `DecodeStages` | `func EncodeStages(stages []Stage) []byte` | 打点载荷的唯一一份编解码（空表 = 零值） |
| `ClipPreview` | `func ClipPreview(text string) string` | 预览裁剪的唯一一份实现（按 rune，三层转调） |
| `ProgressOf` | `func ProgressOf(kind Kind, record storeworkunit.Record) Progress` | 记录 → 进度读数的唯一一份折算 |
| `NewUnitReader` | `func NewUnitReader(ledger SessionLedger, projectID, mainSessionID string, kind Kind, owned func(storeworkunit.Record) bool) *UnitReader` | 建一层面向 nodeID 的读面（`owned` = 归属过滤） |
| `UnitReader.Read` | `func (r *UnitReader) Read(nodeID string) (Progress, bool, error)` | 读一件事的进度读数（无记录 = found=false） |
| `UnitReader.Records` / `Record` | `func (r *UnitReader) Records() ([]storeworkunit.Record, error)` | 原始记录清单 / 单条（层专有格只有这里读得到） |
| `UnitReader.List` | `func (r *UnitReader) List() ([]Progress, error)` | 本读面全部读数（按 NodeID 稳定排序） |

### ports.go

| 类型 | 签名 | 摘要 |
|---|---|---|
| `SceneFace` | `interface` | 现场端口：`BeginScene` / `MergeScene` / `ReleaseScene` / `AdoptScenes` / `SceneRegistered` |
| `UnitFace` | `interface` | 编排闸门 + 账本端口：`Owned` / `Settled` / `SettleUnit` / `ReclaimUnit` / `RecoverUnits` |
| `RecordFace` | `interface` | 会话记录端口：`Status` / `Readout` / `SaveRecord` / `SettleRecord` / `Clear` |

### lifecycle.go

| 函数 | 签名 | 摘要 |
|---|---|---|
| `NewHost` | `func NewHost(scenes SceneFace, units UnitFace, records RecordFace) *Host` | 构造（三格允许缺 = 宿主未装配） |
| `Host.Begin` | `func (h *Host) Begin(ctx context.Context, u Unit) (Scene, error)` | 建（或复用）现场；幂等；team 托管时顺手落"这一轮开始了" |
| `Host.AlreadySettled` | `func (h *Host) AlreadySettled(ctx context.Context, u Unit) (bool, error)` | **重入**的唯一判据（两层共用） |
| `Host.Finish` | `func (h *Host) Finish(ctx context.Context, u Unit, result Result, mergeErr error) (Outcome, error)` | 这一轮怎么结束：重入 → 合并一次 → 分类 → 按策略回收 |
| `Host.Reclaim` | `func (h *Host) Reclaim(ctx context.Context, u Unit) error` | 拆现场 + 清会话（+ team 侧回收作业）；唯一入口，幂等 |
| `Host.Recover` | `func (h *Host) Recover(ctx context.Context, u Unit) (Resume, error)` | 重启回灌：认领先于 Prune，按会话级粒度读回 |
| `Host.Notice` | `func (h *Host) Notice(outcome Outcome) string` | 把结论折成给人看的一行（非落定必带处置办法） |
| `Host.teamOwned` | `func (h *Host) teamOwned(ctx context.Context, u Unit) bool` | 唯一允许的"按层"判断：依据是归属读数，不是 `Kind` |

### 用例

| 文件 | 钉住什么 |
|---|---|
| `contract_test.go` | 收尾分类唯一一份 / 说明有界 / **层间差异只剩策略**（`TestFinishPolicyIsTheOnlyDifference`） |
| `session_test.go` | 会话账本的编译期断言 / 恢复说明陈述事实与下一步 / 空读数不算错误 |
| `progress_test.go` | 打点编解码往返与有界 / 折算只读不编造 / 按 nodeID 的读法与归属过滤 / 读面不许按层分支 |
| `progress_bench_test.go` | 读面五个基准（预览裁剪 / 编解码 / 折算 / 列表），供"读面变慢"时对照 |

## 文件与函数索引

> 由源码 doc 注释自动提取（首行摘要）；描述源码行为，与实现保持同步。
> 刷新方式：`python scripts/gen_core_readme_index.py`。

### classify.go

- `func IsUncommittedChanges(err error) bool` — IsUncommittedChanges 报告 err 是不是"现场收尾协议未执行"那一族（判据**唯一一份**）。
- `func IsMergeBlockedByMain(err error) bool` — IsMergeBlockedByMain 报告 err 是不是"合并被主工作区挡路"那一族（判据**唯一一份**）。
- `func ClassifyFinish(result Result, mergeErr error) Outcome` — ClassifyFinish 是三层**唯一一份**收尾分类：把"这一轮怎么结束的"归到四类之一。
- `func remedyFor(kind OutcomeKind) string` — remedyFor 返回该结论的处置办法（落定为空 —— 它走验收，不走处置）。
- `func boundedTo(text string, limit int) string` — boundedTo 把说明裁到有界长度（按 rune 裁，避免切断多字节字符）。
- `func bounded(text string) string` — bounded 把收尾说明裁到 Outcome 的长度上限。
- `func (o Outcome) Settled() bool` — Settled 报告一次收尾是否算"落定"（可以进验收）。
- `func (o Outcome) String() string` — String 便于日志与看板投影（`kind: notice`）。

### contract.go

- `func (Immediate) AfterFinish(ctx context.Context, lifecycle Lifecycle, u Unit) error` — AfterFinish 立刻回收：调的就是**同一个**析构函数（`Lifecycle.Reclaim`）。
- `func (AtTeamClose) AfterFinish(context.Context, Lifecycle, Unit) error` — AfterFinish 不做任何事：回收由 team_close 统一触发（调用点不同，析构函数是同一个）。

### contract_test.go

- `func TestClassifyFinishIsTheOneClassification(t *testing.T)` — TestClassifyFinishIsTheOneClassification：三层唯一一份收尾分类的判据表。
- `func TestClassifyFinishNoticeIsBounded(t *testing.T)` — TestClassifyFinishNoticeIsBounded：说明要进回执与看板行，必须有界。
- `func TestFinishPolicyIsTheOnlyDifference(t *testing.T)` — TestFinishPolicyIsTheOnlyDifference：三层之间唯一的差异点是"什么时候回收"——
- `func (f fakeUnit) Kind() Kind`
- `func (f fakeUnit) ID() string`
- `func (f fakeUnit) SessionPath() string`
- `func (f fakeUnit) Policy() FinishPolicy`
- `func (f fakeUnit) Owns() Ownership`
- `func (f *fakeLifecycle) Begin(context.Context, Unit) (Scene, error)`
- `func (f *fakeLifecycle) Finish(context.Context, Unit, Result, error) (Outcome, error)`
- `func (f *fakeLifecycle) Reclaim(context.Context, Unit) error`
- `func (f *fakeLifecycle) Recover(context.Context, Unit) (Resume, error)`
- `func (f *fakeLifecycle) AlreadySettled(context.Context, Unit) (bool, error)`
- `func (f *fakeLifecycle) Notice(Outcome) string`

### lifecycle.go

- `func NewHost(scenes SceneFace, units UnitFace, records RecordFace, opts ...HostOption) *Host` — NewHost 收下三格端口（构造式注入，不读全局）。三格都允许传 nil = 宿主未装配，
- `func (h *Host) teamOwned(ctx context.Context, u Unit) bool` — teamOwned 报告这一份是不是**归团队托管**（生命周期里唯一允许出现的"按层"判断，依据是
- `func (h *Host) Begin(ctx context.Context, u Unit) (Scene, error)` — Begin 建（或复用）这一份现场：唯一实现。
- `func (h *Host) AlreadySettled(ctx context.Context, u Unit) (bool, error)` — AlreadySettled 回答"这件事已经收过尾了吗"——**重入**的唯一判据，两层共用。
- `func (h *Host) Finish(ctx context.Context, u Unit, result Result, mergeErr error) (Outcome, error)` — Finish 只回答"这一轮怎么结束的"：分类 + 合并 + 回执；不拆现场。唯一实现。
- `func (h *Host) Reclaim(ctx context.Context, u Unit) error` — Reclaim 拆这一份：唯一入口，幂等。唯一实现。
- `func (h *Host) Recover(ctx context.Context, u Unit) (Resume, error)` — Recover 重启回灌：先认领现场（**必须在 Prune 之前**），再按**会话级**粒度读回这一层的
- `func (h *Host) Notice(outcome Outcome) string` — Notice 生成给人看的说明：**非落定必带处置办法**（合同见 Outcome）。

### merge_retry.go

- `func DefaultMergeRetry() MergeRetry` — DefaultMergeRetry 返回既有的默认等待预算。
- `func WithMergeRetry(retry MergeRetry) HostOption` — WithMergeRetry 覆盖默认的挡路等待策略（测试与需要不同预算的装配点使用）。
- `func (h *Host) mergeScene(ctx context.Context, own Ownership, u Unit) error` — mergeScene 调用现场端口一次，并在**主工作区挡路**时按预算等待重试。

### merge_retry_test.go

- `func (s *retryScene) BeginScene(context.Context, Ownership, string, string) (Scene, error)`
- `func (s *retryScene) MergeScene(context.Context, Ownership, string, string) error`
- `func (s *retryScene) ReleaseScene(nodeID string)`
- `func (s *retryScene) AdoptScenes(context.Context, string) error`
- `func (s *retryScene) SceneRegistered(string) bool`
- `func (s *retryScene) calls() int`
- `func (retryUnits) Owned(context.Context, string, Ownership) bool`
- `func (retryUnits) Settled(context.Context, string, string) (bool, error)`
- `func (retryUnits) SettleUnit(context.Context, string, Ownership, error, error) (Outcome, error)`
- `func (retryUnits) ReclaimUnit(context.Context, string, string) error`
- `func (retryUnits) RecoverUnits(context.Context, string) (Resume, error)`
- `func (*retryRecords) Status(context.Context, Ownership, string, string) (string, bool, error)`
- `func (*retryRecords) Readout(context.Context, Ownership, string, string) (Resume, error)`
- `func (*retryRecords) SaveRecord(context.Context, Ownership, string, string, string, string, string)`
- `func (*retryRecords) SettleRecord(context.Context, Ownership, string, string, Outcome)`
- `func (r *retryRecords) Clear(context.Context, Ownership, string, string) error`
- `func (retryUnit) Kind() Kind`
- `func (u retryUnit) ID() string`
- `func (retryUnit) SessionPath() string`
- `func (retryUnit) Policy() FinishPolicy`
- `func (retryUnit) Owns() Ownership`
- `func blockedErr(attempt int) error`
- `func TestHostFinishRetriesMergeBlockedByMainWithinBudget(t *testing.T)` — TestHostFinishRetriesMergeBlockedByMainWithinBudget 钉住新边界：挡路的等待与重试
- `func TestHostFinishDoesNotRetryDefinitiveMergeFailure(t *testing.T)` — TestHostFinishDoesNotRetryDefinitiveMergeFailure 钉住"只有挡路才重试"：
- `func TestHostFinishStopsRetryWhenContextCancelled(t *testing.T)` — TestHostFinishStopsRetryWhenContextCancelled：等待期间取消必须立刻收口为
- `func TestHostFinishReportsBlockedBudgetWithRemedy(t *testing.T)` — TestHostFinishReportsBlockedBudgetWithRemedy：预算耗尽仍被挡住 → 交回带
- `func TestHostFinishClassificationStaysTheOneClassifier(t *testing.T)` — TestHostFinishClassificationStaysTheOneClassifier：同一份合并事实交给 Host 与

### progress.go

- `func EncodeStages(stages []Stage) []byte` — EncodeStages 把打点表编成记录里的那串载荷：**唯一一份**（含预览的 rune 上限与空阶段丢弃）。
- `func DecodeStages(payload []byte) []Stage` — DecodeStages 解出记录里的打点表：**唯一一份**。
- `func ClipPreview(text string) string` — ClipPreview 把一段正文压成有界的一行（按 rune 裁，避免切断多字节字符）——**唯一一份**
- `func ProgressOf(kind Kind, record storageworkunit.Record) Progress` — ProgressOf 把一条会话记录折成进度读数——**唯一一份折算**。
- `func NewUnitReader(ledger SessionLedger, projectID, mainSessionID string, kind Kind, owned func(storageworkunit.Record) bool) *UnitReader` — NewUnitReader 建一个**面向一层**的读面。
- `func (r *UnitReader) Read(nodeID string) (Progress, bool, error)` — Read 返回一件事的进度读数。
- `func (r *UnitReader) Records() ([]storageworkunit.Record, error)` — Records 返回本读面的**原始记录清单**（归属过滤已生效）：层专有格（`History` /
- `func (r *UnitReader) Record(nodeID string) (storageworkunit.Record, bool, error)` — Record 返回一件事**原始**的会话记录：层专有格（`History` / `ContextJSON` / `ResultJSON`，
- `func (r *UnitReader) List() ([]Progress, error)` — List 返回本账本里**归属于本读面**的全部单元进度读数（按 NodeID 稳定排序）。
- `func (r *UnitReader) records() ([]storageworkunit.Record, error)` — records 读回本会话的单元记录（未装配账本 = 空读数；归属过滤在这里统一落地）。

### progress_bench_test.go

- `func benchRecords(n int) []storeworkunit.Record`
- `func BenchmarkClipPreview(b *testing.B)`
- `func BenchmarkEncodeStages(b *testing.B)`
- `func BenchmarkDecodeStages(b *testing.B)`
- `func BenchmarkProgressOf(b *testing.B)`
- `func BenchmarkUnitReaderList100(b *testing.B)`

### progress_test.go

- `func (f *fakeLedger) Save(string, string, storeworkunit.Record) error`
- `func (f *fakeLedger) Delete(string, string, string) error`
- `func (f *fakeLedger) List(string, string) ([]storeworkunit.Record, error)`
- `func TestStageCodecRoundTripsAndBounds(t *testing.T)` — TestStageCodecRoundTripsAndBounds：打点载荷只有一份编解码，且预览有界。
- `func TestProgressOfIsAReadOnlyProjection(t *testing.T)` — TestProgressOfIsAReadOnlyProjection：折算逐一对应记录里已有的格子，不加事实。
- `func TestUnitReaderReadsByNodeIDAndFiltersOwnership(t *testing.T)` — TestUnitReaderReadsByNodeIDAndFiltersOwnership：读法收成一份，归属过滤由调用方给出
- `func TestUnitReaderRecordSharesTheSameLookup(t *testing.T)` — TestUnitReaderRecordSharesTheSameLookup：层专有格（History/ContextJSON/ResultJSON、
- `func TestUnitReaderCannotBranchOnKind(t *testing.T)` — TestUnitReaderCannotBranchOnKind：Kind 是描述性标注，读面不据它分支——同一份记录、同一份

### recovery.go

- `func KindOf(record storageworkunit.Record, teamScenes map[string]bool) (Kind, string)` — KindOf 读出这条记录属于哪一层（**唯一转换点**：快照里的 wire 串 → 契约枚举）。
- `func RestoreUnits(sessionID string, records []storageworkunit.Record, deps RestoreDeps)` — RestoreUnits 是恢复链的**分派点**：先按身份把记录切开，再交给各自的策略。
- `func (n *ResumeNotes) Set(key, note string)` — Set 记下某个键下一次装配要带的恢复说明（空键 / 空说明 = 不记，幂等）。
- `func (n *ResumeNotes) Take(key string) string` — Take 取走某个键的恢复说明并**清掉**（一次装配读完即消）。
- `func (n *ResumeNotes) Peek(key string) string` — Peek 读回某个键的恢复说明但**不清掉**（非消费读：装配期间同一次提示可能被读多次）。
- `func (n *ResumeNotes) Clear(key string)` — Clear 丢掉某个键还没被读走的恢复说明（收口清会话内容时用：会话内容都不在了，

### recovery_test.go

- `func unitRecord(nodeID, kind string) storeworkunit.Record`
- `func TestKindOfReadsTheRecordsOwnIdentity(t *testing.T)` — TestKindOfReadsTheRecordsOwnIdentity：身份判据的顺序是判据的一部分——
- `func (s *stubStrategy) Kind() Kind`
- `func (s *stubStrategy) Restore(records []storeworkunit.Record)`
- `func TestRestoreUnitsDispatchesByIdentityAndKeepsOrder(t *testing.T)` — TestRestoreUnitsDispatchesByIdentityAndKeepsOrder：分派按身份切开、只把**非空批次**交给
- `func TestRestoreUnitsWithoutSceneIndex(t *testing.T)` — TestRestoreUnitsWithoutSceneIndex：没有现场名单（未装配计划/账本）时，缺身份的老记录一律
- `func TestResumeNotesIsReadOnceAndKeyScoped(t *testing.T)` — TestResumeNotesIsReadOnceAndKeyScoped：恢复说明的容器语义——空键/空说明不记、
- `func TestResumeNotesIsConcurrencySafe(t *testing.T)` — TestResumeNotesIsConcurrencySafe：容器自持锁，并发读写不产生数据竞争（本用例供 `-race` 使用）。

### session.go

- `func (r Resume) Empty() bool` — Empty 报告这次回灌什么都没发生（无存储/空会话时的正常读数，不是错误）。
- `func InFlight(status string) bool` — InFlight 报告一条会话记录的 status 是不是"说自己在跑"：记录说在跑、而本进程已无它的
- `func RecoveryNote(kind Kind, record storageworkunit.Record) string` — RecoveryNote 组装"这件事重启前跑到哪、下一步做什么"的恢复说明。三层**共用这一份构建器**

### session_test.go

- `func TestSessionLedgerIsSatisfiedByExistingStore(t *testing.T)` — TestSessionLedgerIsSatisfiedByExistingStore：契约要复用**既有**存储，不是再写一份。
- `func TestResumeNoticeStatesFactsAndNextStep(t *testing.T)` — TestResumeNoticeStatesFactsAndNextStep：恢复说明三层一份：事实在前、动作在后、有界、不编造。
- `func TestResumeEmptyReadsAsNoop(t *testing.T)` — TestResumeEmptyReadsAsNoop：没有存储/没有残留时的读数必须干净（不是错误、也不是"有事"）。
