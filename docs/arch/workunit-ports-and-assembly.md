# workunit 的父（端口）清单与装配方式

状态：**接口清单 + 两步计划**。配套口径见 `workunit-single-lifecycle-one-implementation.md`（§3 接口先行、
§7 纪律、§8 交集与析构、§9 读面、§10 红灯）；现状锚点见 `workunit-duplication-inventory.md`。
一句话：**没有"一个上帝父结构体"，而是一组按职责切开的父（端口），每个形态只装配它需要的那几个。**

## 1. 为什么要切开（而不是一个父结构体）

- 两层的差集（现场、会话、调度、装配、收口时机）不是同一件事，揉成一个父就会长出 `if 哪一层` 分支；
- 接入的父**不止一个**：作业面、现场、会话、读面各有各的实现候选与生命周期；
- 每个父能单独换、单独假（fake），生命周期实现体里因此可以不认识任何具体类型。

## 2. 父（端口）清单

| # | 父/端口 | 它回答什么 | 方法面 | 现有实现候选 | 谁注入 |
|---|---|---|---|---|---|
| 1 | **作业面** `JobFace` | 一件在飞的活：提交、句柄、状态、增量读、取消、销项、按作用域回收、事件 | `Submit(ctx, Spec)(Handle,err)` / `Status` / `Peek`（增量读、不推进游标）/ `Kill` / `Done` / `Reclaim(ctx, Scope)` / `Events()` / `Snapshot(Scope)` | Seele `jobs.Manager`（teammate 现在）、`tools` 那张 async 表（subagent / bash_bg 现在） | 装配处（new） |
| 2 | **现场** `SceneFace` | 一件活的现场：建、收尾合并、回收、重启认领 | `Begin(ctx, identity)(Scene,err)` / `Finish(ctx, scene, result, mergeErr)(Outcome,err)` / `Reclaim(ctx, scene)` / `Adopt/Restore(ctx)` | `*worktree.WorktreeManager`（已是一份实现） | 装配处（new） |
| 3 | **会话/记录** `SessionFace` | 跑到哪、现场在哪、重启怎么回灌 | `Save/Load/List/Delete(record)` / `Recover(ctx, scope)(Resume,err)` / `InFlight(status)` | `*sessionstore.NodeSessionStore`（`SessionLedger` 已有编译期断言） | 装配处（new） |
| 4 | **读面** `ReadFace` | 进度 / 阶段 / 结论 / 在跑与否（只读，不新起事实源） | `Stage` / `EncodeStages·DecodeStages` / `Progress·ProgressOf` / `UnitReader`（`a3` 勘定：`docs/arch/workunit-progress-read-surface.md`，建议落 `seelebridge/workunit/progress.go`） | 投影自 ① + ③，**不新增一张表** | 装配处（new） |
| 5 | 编排闸门 | 屏障、依赖、在编校验 | —— | leader 侧（`teamwork.Coordinator`） | **不进 workunit**，以回调/端口接 |
| 6 | 装配 | plugin / 系统提示词 / skill 前缀 | —— | `runtime_role_turn` 一侧 | **不进 workunit**，以回调/端口接 |
| 7 | 账本/看板 | 谁的活、哪一件、终态归档 | —— | team plan + board | **不进 workunit**，以端口接 |

**切分判据**：第 5–7 条是"编排内容"，不是两层的交集（§8）——它们通过**端口/回调**接进来，
生命周期实现不许认识它们的类型。

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
2. 给 `tools` 那张 async 作业表也补**结构上满足**的编译期断言（`var _ workunit.Jobs = ...`），
   让"两个父实现同形"从今天起就被编译器钉住；**不改**它的行为、**不动**迁移。
3. 契约里写清：`Peek`（增量、不推进游标）与 `Snapshot`（全量）是两个用法；`Reclaim` 只按 scope 回收。
4. 装配表落地一版：生命周期实现只持有端口，不持有 `teamwork` / `worktree` / `session` 的具体类型。
5. 读面按 `a3` 的勘定收口一版：`workunit/progress.go`（`Stage` 编解码 + `ProgressOf` + `UnitReader`）；
   `a3` 已钉出真因不是形状不统一（两层本来就落同一张 `sessionstore.NodeSessionRecord`、走同一个
   `workunit.SessionLedger`），而是"记录→进度折算"**三份手写** + teammate 阶段打点恒为单元素。
   折算的合并归 ② 那一波（它跨 `session/` 与 `workunit_team_records.go`），① 只先把读法定形。
6. 验证：`gofmt` / `go build ./...` / `go vet` / 相关包 `-count=1` 真跑并记录；**不做**行为迁移，
   既有用例一条不动（动了就是迁移，越界）。

**前置**：`seelebridge/workunit/contract.go` 当前由在做父实现重构的 `a1` 占用（分支 `seelex/lifecycle-parent`）。
先确认那条分支已落地且工作区干净，再动这个文件——两个改动同时落在同一文件必冲突。

### ② 之后做：两张作业表合一（高风险跨层，独立一波）

**七点不一致（原文并列为"七点"，`docs/arch/teamwork-leader-worker-architecture.md` §12 有探针证据）**：

1. `Dispatch` 要求非空 `Description`；钉住的用例用**空描述**调 `begin`。
2. `Dispatch` 立刻启动执行体并**打开/持有**每作业输出文件；钉住的用例把 `begin` 当"只登记"，
   并要求作业登记期间输出目录可删（Windows `RemoveAll` 因句柄被持有而失败——Go 打开时没带 `FILE_SHARE_DELETE`）。
3. `Fetch` 一步完成"推进游标 + 自动销项"；工具面是**两阶段**（`advanceTail` 读，再 `markCursor`）。
4. 管理器除执行体的 `Sink` 外**没有**外部"合成终态"入口；而 `registry.finish(handle, exit)` 是用例直接调的公开方法。
5. 管理器不保留可读的"已销项"状态字面量（只有 `ErrRetired`）。
6. 管理器**从不删除**每作业的输出文件。
7. 执行体**无法从 `Spec` 里知道自己的句柄**。

**做法**：先照着 §12 列的"六个最小 `jobs` 增补"，把上面几点逐条解决（多数是**语义差异**，不是纯搬家），
再让迁移以"行为逐字不变的门面"落地；期间既有 `async_*_test.go` 一条不改。

**不做**（非目标）：这一波不碰前端读面、不碰调度闸门、不改作业的输出文件语义。
