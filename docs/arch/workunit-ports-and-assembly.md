# workunit 的父（端口）清单与装配方式

状态：**接口清单 + 两步计划（① 已落地，见 §5「① 落地记录」；② 未开始）**。配套口径见
`workunit-single-lifecycle-one-implementation.md`（§3 接口先行、
§7 纪律、§8 交集与析构、§9 读面、§10 红灯）；现状锚点见 `workunit-duplication-inventory.md`。
一句话：**没有"一个上帝父结构体"，而是一组按职责切开的父（端口），每个形态只装配它需要的那几个。**

## 1. 为什么要切开（而不是一个父结构体）

- 两层的差集（现场、会话、调度、装配、收口时机）不是同一件事，揉成一个父就会长出 `if 哪一层` 分支；
- 接入的父**不止一个**：作业面、现场、会话、读面各有各的实现候选与生命周期；
- 每个父能单独换、单独假（fake），生命周期实现体里因此可以不认识任何具体类型。

## 2. 父（端口）清单

| # | 父/端口 | 它回答什么 | 方法面 | 现有实现候选 | 谁注入 |
|---|---|---|---|---|---|
| 1 | **作业面** `JobFace` | 一件在飞的活：提交、句柄、状态、增量读、取消、销项、按作用域回收、变更信号 | 按能力切成的**四格 + 一个合成**：`JobSubmitter`（`Dispatch`=提交）/ `JobReader`（`Observe`=状态、`Peek`=增量读不推进游标、`Snapshot`=全量）/ `JobController`（`Kill` / `Done` / `Reclaim(ctx, Scope)` 按作用域）/ `JobSignals`（`Events()`）；`Jobs` = 四格的合成 | Seele `jobs.Manager`（teammate 现在）：**八格逐字对齐**，`contract.go` 的 `var _ Jobs = (jobs.Manager)(nil)`；`tools` 那张 async 表（subagent / `bash_bg` 现在）：今天只在 `JobSignals` 那一格同形（断言在 `seelebridge/tools/async_exec.go`） | 装配处（new） |
| 2 | **现场** `SceneFace` | 一件活的现场：建、收尾合并、回收、重启认领 | `Begin(ctx, identity)(Scene,err)` / `Finish(ctx, scene, result, mergeErr)(Outcome,err)` / `Reclaim(ctx, scene)` / `Adopt/Restore(ctx)` | `*worktree.WorktreeManager`（已是一份实现） | 装配处（new） |
| 3 | **会话/记录** `SessionFace` | 跑到哪、现场在哪、重启怎么回灌 | `Save/Load/List/Delete(record)` / `Recover(ctx, scope)(Resume,err)` / `InFlight(status)` | `*sessionstore.NodeSessionStore`（`SessionLedger` 已有编译期断言） | 装配处（new） |
| 4 | **读面** `ReadFace` | 进度 / 阶段 / 结论 / 在跑与否（只读，不新起事实源） | `Stage` / `EncodeStages·DecodeStages` / `Progress·ProgressOf` / `UnitReader`（`a3` 勘定：`docs/arch/workunit-progress-read-surface.md`，建议落 `seelebridge/workunit/progress.go`） | 投影自 ① + ③，**不新增一张表** | 装配处（new） |
| 5 | 编排闸门 | 屏障、依赖、在编校验 | —— | leader 侧（`teamwork.Coordinator`） | **不进 workunit**，以回调/端口接 |
| 6 | 装配 | plugin / 系统提示词 / skill 前缀 | —— | `runtime_role_turn` 一侧 | **不进 workunit**，以回调/端口接 |
| 7 | 账本/看板 | 谁的活、哪一件、终态归档 | —— | team plan + board | **不进 workunit**，以端口接 |

**切分判据**：第 5–7 条是"编排内容"，不是两层的交集（§8）——它们通过**端口/回调**接进来，
生命周期实现不许认识它们的类型。

**① 落地版（2026-10-06）**：生命周期实现（`seelebridge/workunit_parent.go` 的 `lifecycleHost`）
只持**三格宿主端口**——`sceneFace`（现场）/ `unitFace`（编排闸门 + 账本）/ `recordFace`
（会话记录）；这三格的唯一实现是 `seelebridge/workunit_assembly.go` 的 `hostPorts`，`teamwork` /
`worktree` / `sessionstore` 的具体类型只允许出现在那个文件里（机械门禁：
`e2e/workunit_ports_test.go`）。会话记录端口的**记录形状**仍是契约自己声明的
`sessionstore.NodeSessionRecord` / `sessionstore.Key`（`session.go` 的 `SessionLedger` 就是它）
——那是"复用同一份记录、不另立第二种真相"的落点，不是"持具体实现"。
作业面（第 1 行）今天由 `unitFace` 那一格驱动（`Coordinator.reclaimStepsLocked` 步 1 是全仓唯一
的作业回收调用点），宿主**先不**直接驱动它：多一个调用点就是第二条回收路径，那是 §5② 的落点。

## 3. 装配矩阵：不同父的不同用法 → 不同形态（全部非上帝）

| 形态 | 作业面 | 现场 | 会话 | 策略 | 编排/装配 |
|---|---|---|---|---|---|
| 后台命令（`bash_bg`） | ✔ 提交 + 增量读 | — | — | 不实现 `Unit`（job 是基线） | — |
| subagent（一件活） | ✔（提交 + 句柄 + 回收） | ✔ worktree | ✔ 一件活自己的会话 | `Immediate`（收尾即拆） | 无（leader 派的） |
| teammate（角色会话里的活） | ✔ | ✔（角色级 + item 级） | ✔ | `AtTeamClose`（留给 `team_close`） | leader 闸门 + 装配 |
| （未来的）只读巡检员 | — | — | ✔ 短会话 | `Immediate` | — |
| （未来的）换一个作业面实现 | 换实现，其余不动 | | | | |

**方法的不同用法就是策略**（§8 的延续，不是第二份实现）：

| 方法 | 用法 | 调用点 |
|---|---|---|
| `Reclaim`（析构） | **只有一份实现** | `Immediate.AfterFinish` 收尾当场调；`AtTeamClose.AfterFinish` 不调，留 `team_close` 的收口步调 |
| `Peek` vs `Snapshot` | 增量读 / 全量读，两个用法一个父 | 正文给模型用 `Peek`；看板给界面用 `Snapshot` |
| `Submit` | 只出现一次（谁想跑谁提交） | 派活处 |
| `Kill` | 只在取消/收口出现 | 收口步 |

## 4. 判据（怎么算做对了）

1. 只装配一部分父也要能编译、行为正确（用例：只给作业面的形态跑通）。
2. **每个父都能在测试里换成假的**（不需要真 git、真进程 —— 现在的重集成测试就是缺这一层）。
3. 生命周期实现里搜不到 `if kind ==`（层差异只在装配表里）。
4. 换一个父实现 = 改装配处一行 + 编译期断言先红。

## 5. 两步计划

### ① 现在做：只定接口（不迁移）

1. `seelebridge/workunit/contract.go`：把 `Jobs` 从"只有 `Reclaim`"扩成**完整作业面**（§2 第 1 行的方法面，
   只收现在真正用到的几家）；**保留** `var _ Jobs = (jobs.Manager)(nil)` 这条编译期断言。
2. 给 `tools` 那张 async 作业表也补**结构上满足**的编译期断言，让"两个实现同形"从今天起就被
   编译器钉住；**不改**它的行为、**不动**迁移。
   （2026-10-06 口径更正：原文写的是 `var _ workunit.Jobs = ...`，但 tools 自建表今天**不满足**
   完整作业面——它的七格名字与签名都不同。落地的是它**已经同形**的那一格
   `var _ workunit.JobSignals = (*asyncRegistry)(nil)`；完整面的断言在契约那份实现上
   （`var _ Jobs = (jobs.Manager)(nil)`）。两表同形要等 §5② 先解决七点语义差异。）
3. 契约里写清：`Peek`（增量、不推进游标）与 `Snapshot`（全量）是两个用法；`Reclaim` 只按 scope 回收。
4. 装配表落地一版：生命周期实现只持有端口，不持有 `teamwork` / `worktree` / `session` 的具体类型。
5. 读面按 `a3` 的勘定收口一版：`workunit/progress.go`（`Stage` 编解码 + `ProgressOf` + `UnitReader`）；
   `a3` 已钉出真因不是形状不统一（两层本来就落同一张 `sessionstore.NodeSessionRecord`、走同一个
   `workunit.SessionLedger`），而是"记录→进度折算"**三份手写** + teammate 阶段打点恒为单元素。
   折算的合并归 ② 那一波（它跨 `session/` 与 `workunit_team_records.go`），① 只先把读法定形。
6. 验证：`gofmt` / `go build ./...` / `go vet` / 相关包 `-count=1` 真跑并记录；**不做**行为迁移，
   既有用例一条不动（动了就是迁移，越界）。

### ① 落地记录（2026-10-06，交付报告见 `docs/2026-10-06-workunit-jobs-port/step-1-delivery.md`）

| # | 落地物 | 位置 |
|---|---|---|
| 1 | 作业面四格 + 合成 `Jobs`；`var _ Jobs = (jobs.Manager)(nil)` 保留原处 | `seelebridge/workunit/contract.go` |
| 2 | tools 自建表同形那一格的断言（只加断言行，行为零变化） | `seelebridge/tools/async_exec.go` |
| 3 | 现场 / 编排账本 / 会话记录三格端口 + 装配处 | `seelebridge/workunit_assembly.go` |
| 4 | 生命周期实现只持端口（无按层分支、无具体类型） | `seelebridge/workunit_parent.go` |
| 5 | 读面定形（`Stage` / `EncodeStages`·`DecodeStages` / `ProgressOf` / `UnitReader`） | `seelebridge/workunit/progress.go` |
| 6 | 机械门禁（按层分支 + 具体类型 + 每个实现的编译期断言 + 契约包 worktree 依赖例外），带阴性对照 | `e2e/workunit_ports_test.go` |

三处**文档口径更正**（仓库现状优先，按"先改文档并说明理由"处理）：

- **方法名**：§2 第 1 行原写的 `Submit` / `Status` 是**概念名**，契约里的 Go 方法名必须是
  `jobs.Manager` 的名字（`Dispatch` / `Observe`），否则 `var _ Jobs = (jobs.Manager)(nil)` 钉不住。
  概念别名写在各方法注释里。
- **tools 那张表的断言**：见上文第 2 条的更正。`JobSignals` 是它今天唯一同形的一格（同一形状、
  同一语义：容量 1 + latest-wins 的变更信号）；其余七格**既不在 ① 也不在 ②**——② 已按 §12.4 收窄为
  "并判据/读法/容器"，不含合表（见下文 ②）。
- **`UnitReader` 的入参**：读面勘定原文给的是三参构造，但记录形状里**没有 `Kind` 这一格**
  （两层共用同一张记录表），所以落地版多两个入参：`kind`（这一层的读数标注）与 `owned`
  （归属过滤，可为 nil）——否则 `Read`/`List` 只能替调用方猜归属，那正是勘定 §U5 要避免的。

**前置**：`seelebridge/workunit/contract.go` 当前由在做父实现重构的 `a1` 占用（分支 `seelex/lifecycle-parent`）。
先确认那条分支已落地且工作区干净，再动这个文件——两个改动同时落在同一文件必冲突。

### ② 之后做：并掉剩余的本质重复（**不是**合表）

**先纠正本文初版的口径**：本文 2026-10-06 初版把 ② 写成"两张作业表合一（迁到 Seele `jobs.Manager`）"，
这与 `docs/arch/teamwork-leader-worker-architecture.md` **§12.4（2026-10-01 的用户裁决）** 直接冲突。
那条路**已走过并认定失败**：`event.Sink`（`WithEventSink`）必须在 `jobs.New` **构造期**定死，而那一刻
还不知道"这条作业属于哪个会话的哪条事件流"；框架的 `event.Recorder` 是**单例 + 全局序号**，而 Seelex 的
会话事件库是**按会话追加/排序**——发出的作业事件 append 不到会话事件流尾部。裁决原文：**"本步不再开工，
也不必等 §12.3 的契约增补；§12.3 降级为留档，不构成待办。"**（七点不一致与六个契约增补，都只作为
"当年为什么绕不过去"的留档。）

**所以 ② 收窄成"并判据 / 读法 / 容器"，不并表**：

| # | 重复 | 今天几份 | 并到哪 | 锚点 | 代价 |
|---|---|---|---|---|---|
| 1 | 「记录 → 进度」折算 | 3 份手写 | `workunit.ProgressOf`（① 已定形） | `session/subagent_sessions.go` 写侧/读侧、`workunit_team_records.go` | 改两条链的落盘/回读路径 = 行为迁移 |
| 2 | 阶段 JSON 编解码 | 2 份 | `workunit.EncodeStages`·`DecodeStages`（① 已定形） | 同上 | 同上 |
| 3 | teammate 阶段打点恒单元素 | 1 处语义缺口 | 走同一套 `Stage` | `workunit_team_records.go` 的 `teamUnitStages` | 会改回灌文案（勘定 U3/U4 要复验） |
| 4 | 恢复说明容器 | 2 份（已收一半） | `workunit.RecoveryNote`（已有） | `runtime_subagent_resume.go:386`、`workunit_team.go:383` | 小 |

**登记为分层事实、不再当缺陷的两张表**（写进 `workunit/README.md` 的依赖方向段）：

- 表① 框架 `jobs.Manager` = **团队作业面**：`jobs_manage` 是框架 builtin
  （`runtime_teamwork.go:248`）、`Scope{Session,Subject}`、`Events()` 信号扇出；
- 表② Seelex tools 作业表 = **工具后台作业面**：`job_manage`、进程树、输出文件语义、Windows `RemoveAll` 约束。

两者只在 `workunit` 的作业端口后面以同一形状被调用（① 的成果），**各自的服务对象不同——长得像，但不是同一件事**。

**完成判据**：① 折算只剩一份（唯一实现 + 两处调用点转调）；② 抽一条**跨层一致性用例**——同一份收尾脚本
在 subagent 与 teammate 上跑出同一份 `Stage` 序列；③ 既有用例一条不改；④ ① 的两条编译期断言仍在原处。

**要重启"迁到 Seele"怎么办**：那是**重新裁决**，不是继续旧计划——先撤 §12.4 的结论、先解决会话事件流归属
（失败根因），再谈 §12.3 的六个增补。**反向合一**（teammate 也搬离 `jobs.Manager`）同理要重新裁决：
代价是框架的 `jobs_manage` 变成空壳、`Events()` 投影与信号扇出重做。

**不做**（非目标）：这一波不碰前端读面渲染、不碰调度闸门、不改作业的输出文件语义、不动 `seelebridge/worktree/**`。
