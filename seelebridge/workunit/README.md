# workunit：异步作业的「工作单元」契约

一个**只装生命周期**的契约包：现场、会话、作业。它不装执行面（`Run` / 回合外壳 /
装配 / 调度 / 统一事件流一律不进这里），只回答一个问题——

> 一件异步的活，**怎么开始、怎么收尾、怎么回收、重启后怎么接着做**，三层是不是同一套说法。

## 一、层链：只增不减

| 层 | 相对下层**增**了什么 | 现场 | 会话 | 收尾策略 |
|---|---|---|---|---|
| `job` | ——（基线） | 无 | 无（后台运行 + 结果回传） | 不适用：**不实现 `Unit`** |
| `subagent` | worktree 现场 + 一件活自己的异步会话 + 存储与合并纪律 | **临时**（派出即建、收尾即清） | 一件活一条，落 `sessionstore.NodeSessionStore` | `Immediate` |
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

- **依赖倒置**：调用方只依赖 `Lifecycle`，不依赖具体类型。实现（`seelebridge` 的
  `lifecycleHost`）端口字段**不导出**；两个注册点在 `new` 时注入它，**只持有
  `workunit.Lifecycle` + 自己的 `Unit` 读数**，方法体一律转发。要两层不同实现 = 再写一个
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
| 实现二（subagent / `bash_bg`） | tools 自建 async 表：今天只在 **`JobSignals`** 那一格上与这里同形（`seelebridge/tools/async_exec.go` 的断言）；其余七格的名字与签名都不同 |
| 两张表 | **本轮只求同形，一张都不搬**——搬表是步骤②（`CHANGELOG.md:112-116` 已登记七点不一致，`docs/arch/workunit-ports-and-assembly.md` §5② 是路线） |

`docs/arch/workunit-ports-and-assembly.md` §2 把这一族端口与现场/会话/读面并列；生命周期实现
（`seelebridge` 的 `lifecycleHost`）只持**三格宿主端口**（现场 / 编排账本 / 会话记录，见
`seelebridge/README.md` 的 workunit 一节），作业面今天由编排那一格驱动（回收的唯一调用点在
`Coordinator.reclaimStepsLocked` 步 1），宿主直接驱动它是步骤② 的落点。

## 二、文件

| 文件 | 内容 |
|---|---|
| `contract.go` | `Kind` / `Scene` / `Result` / `Outcome(Kind)` / `Ownership`（归属读数）/ `Unit`（层读数：`Kind` `ID` `SessionPath` `Policy` `Owns`）/ `Lifecycle`（父实现契约面：`Begin` `Finish` `Reclaim` `Recover` `AlreadySettled` `Notice`）/ `FinishPolicy`（`Immediate`、`AtTeamClose`）/ **作业面四格 + 合成** `Jobs`（`JobSubmitter`：`Dispatch`；`JobReader`：`Observe` `Peek` `Snapshot`；`JobController`：`Kill` `Done` `Reclaim`；`JobSignals`：`Events`） |
| `classify.go` | `ClassifyFinish(result, mergeErr)`：三层**唯一一份**收尾分类（跑失败判死 → 未提交（不判死）→ 主工作区挡路（不判死）→ 其他合并错误判死 → 落定）+ `bounded` + 它认的两个哨兵 `ErrUncommittedChanges` / `ErrMergeBlockedByMain`（与 `Is*` 判据；定义在这里 = 依赖方向是 实现 → 契约） |
| `session.go` | `SessionLedger`（结构上就是 `*sessionstore.NodeSessionStore`）/ `Resume` / `RecoveryNoteRole` / `RecoveryNotePrefix` / `RecoveryNote(kind, record)` |
| `progress.go` | 进度**读面**：`Stage` + `EncodeStages`/`DecodeStages`（打点载荷的唯一一份编解码 + `ClipPreview` 唯一一份预览裁剪）+ `Progress`/`ProgressOf`（记录 → 进度的唯一一份折算）+ `UnitReader`（按 nodeID 读一件事的进度：`Read`/`Record`/`List`/`Records`）。步骤②已把它接进生产读路（宿主记录读面 / 子代理恢复定位 / teammate 会话级读回） |

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

1. **形状一致**：一个工作单元 = 一条 `sessionstore.NodeSessionRecord`（`NodeID` 定位：
   subagent 是节点 id，teammate 是 `<role>-<itemID>`）；记录里的 `History` / `StagesJSON` /
   `ContextJSON` / `Worktree` 就是"跑到哪、现场在哪"。
2. **运行期落盘**：跑着就写（进程中断可从最近一次记录恢复），落定再写终态。
3. **认领先于 `Prune`**（不变式 2）。
4. **回灌 + 恢复说明**：历史灌回执行面并注入 `RecoveryNote`；记录说 `running/queued` 而本进程
   没有它的执行面 ⇒ **中断**，记进 `Resume.Interrupted`，交上层重跑或人工处置。

## 五、测试与验证

```powershell
go vet ./seelebridge/workunit/
go test ./seelebridge/workunit/ -count=1
go test ./e2e/ -run TestWorkunitPortGate -count=1   # 装配与作业面的机械门禁（含阴性对照）
```

用例守着的是**语义**而不是实现细节：分类表（含"两种收尾失败同时具备 → 未提交优先"）、
`AtTeamClose` 不得自己拆现场、`SessionLedger` 与既有存储的同名同签名（编译期断言）、
恢复说明的前缀族与有界性；`progress_test.go` 守读面（打点编解码只有一份、折算不编造事实、
按 nodeID 的读法与归属过滤）；`e2e/workunit_ports_test.go` 守装配（实现里没有按层分支与具体
类型、每个实现都带编译期断言、契约包 **import 实现包零命中**）。
