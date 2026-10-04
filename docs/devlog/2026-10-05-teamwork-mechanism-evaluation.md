# 2026-10-05 teamwork 机制评价（修完并发丢失更新之后）

> 本文由 **leader 会话**在 teamwork 编排下产出：4 个 teammate（2 实现 / 1 复核 / 1 评价）、
> 3 个里程碑。同日上一轮 `2026-10-05-teamwork-self-survey.md` 负责"看清机制"，
> 这一轮负责"修掉它的一个真缺陷 + 评价它"。goal：`g-2`；team_id：`teamwork-lockfix`。

---

## 0. 这一轮跑了什么

| 里程碑 | 屏障 | 工作项 | 角色 | 权责 | 结果 |
|---|---|---|---|---|---|
| `m-fix` 修复 | `depends_on: []` | `wi-impl` 计划头读-改-写原子化 | exec | readwrite | done |
| | | `wi-race` 并发回归（`depends_on: [wi-impl]`） | test_case | readwrite | done |
| `m-verify` 复核 | `depends_on: [m-fix]` | `wi-review` 独立复核补丁 | reviewer | **readonly** | done |
| `m-evaluate` 评价 | `depends_on: [m-verify]` | `wi-evaluate` 机制评价 | assessor | **readonly** | done |

三处真实发生的闸门（不是"理论上会拦"）：

- **里程碑内 DAG**：`wi-race` 排在 `wi-impl` 之后，必须等它验收通过才被放行；
- **里程碑屏障**：`m-verify` / `m-evaluate` 的排活都只在上一里程碑全 done 之后被受理；
- **只读权责**：reviewer / assessor 只有读权，结论是"能跑能读"得来的——reviewer 自己跑了
  `-run Concurrent -count=30 -race`，并**找出了本轮修复没覆盖的一条缺口**。

---

## 1. 锁的修复（本轮唯一代码改动）

### 1.1 症状与根因

见 `docs/devlog/2026-10-05-teamwork-self-survey.md` §2：5 个工作项并发 settle，作业全部
`exit=0`，2 个的计划状态却被覆盖回 `running`，`team_accept` 拒收、`team_recover` 的
`Interrupted=[]` 又不报——**不是死锁，是丢失更新**。

根因：计划头的每个变更方法都是 `ReadPlan → 改 → WritePlan`，而 store 的模块锁只覆盖
**单次** `commitModuleHead`；跨这三步没有锁。并发 settle 各持陈旧快照写回，后写覆盖先写。

### 1.2 修法

- 新增 `seelebridge/teamwork/planlock.go`：**包级**、按 `sessionstore.Key` 分片的
  `*sync.Mutex` 表（`planLockFor` / `lockPlan` / `unlockPlan`）。不放在 Coordinator 字段上，
  是因为同进程同 Key 可能并存多个 Coordinator（装配层的 `coordinatorForKey` 只是**单个
  Runtime** 的承诺；测试与"重启"复现里同 Key 会并存多实例）——包级表把"同 Key = 同锁"
  变成结构事实。
- 全覆盖：11 个计划写点全部进锁——`SetPlan` / `Dispatch` / `Milestone` / `Retire` / `Close` /
  `PlanMilestone` / `AdjustItem` / `DispatchItem` / `SettleWorkItem` / `AcceptItem` / `FailItem`。
  需嵌套的路径（`Retire`/`Close` → `retireSteps`）走 `retireStepsLocked`，外层持锁、内层不取。
- `SettleWorkItem` 重构为**两段式**：`阶段A（锁内：只读 + 幂等判定 + 取现场） → 步1 合并
  （**锁外**，慢 git） → 阶段B（锁内：重读 + 尾插 + 写态）`。可观察顺序仍是文档口径的
  **合并 → 尾插 → 落态**，但几十秒级的 `MergeWorkspace` 不再冻结编排面。
- **锁序铁律**：计划锁恒为最外层，永远先于 sessionstore 的模块锁取得 ⇒ 全序
  `planLock → moduleLock`，无反向获取，故无环。

### 1.3 判别力（这条修复的证，不是"看起来对"）

| 证据 | 结果 |
|---|---|
| `items_concurrent_test.go` 会合点用例，修复前副本 `-count=3` | **3/3 失败**：`wi-0 状态 = running，want review`；`已 done 被并发 settle 的陈旧快照覆盖` |
| 同一用例，修复后 `-count=1` / `-count=20` | 全绿 |
| `go test ./seelebridge/teamwork/ -count=1`（leader 收口前复跑） | `ok 1.029s` |
| reviewer 独立复跑 `-run Concurrent -count=30 -race` | PASS |

**一条值得写进口径的事**：`-race` 在**修复前的副本上也不报 DATA RACE**——丢失的原子性跨
`ReadPlan / MergeWorkspace / WritePlan` 三次调用，静态探针索引不到。这类缺陷**不能拿 `-race`
当判据**，必须靠会合点排定的交错用例（本轮用例不靠 sleep、不靠调度骰子）。

### 1.4 残留（本轮未做）

| 项 | 严重度 | 说明 |
|---|---|---|
| `DispatchItem` 锁内跑 `git worktree add` | 中 | 慢合并已移出临界区，同类慢操作却留在锁内——不对称 |
| `Close` 锁内做 `Reclaim` / 释放 / 清会话 / 封板 | 中 | `Reclaim` 有一只 5s 级（`jobs.Limits.DefaultWait`）的**有界**停顿，非死锁 |
| 跨进程 / 多 Runtime 同 Key 写计划头 | — | 无保护，明确 out of scope（需 store 层 CAS 或跨进程锁） |
| `planLocks` 包级表不回收 | 低 | Key 数量有界；不设 GC 是为了让并发安全不依赖后台清理 |

---

## 2. 机制评价

### 2.1 顺序与隔离从哪里来，各自代价

- **里程碑屏障**（`milestones[].depends_on`）解"阶段串行"。代价：**空工作项的里程碑永不算
  done**（"没排活 = 还没干"），屏障只能靠"item 全 done"打开。
- **里程碑内 DAG**（`work_items[].depends_on`）解 V 模型先后。代价：**一个 running 卡住整条下游**。
- **一 Work Item 一 Session + 一 worktree** 是隔离的全部来源。代价：每件事一套 worktree，而
  worktree 数量被 teammate 人数封顶——这正是 `max_teammates` 是"产品级约束"而非"偏好"的原因
  （teammate 被硬移除 `fork_subagents`，没有孙级 worktree）。
- **显式拒绝、不静默排队**。代价：leader 得自己收窄范围或改计划重试；换来的是"队伍满了"
  不会伪装成"它在跑"。

### 2.2 本轮真正被实证拦住的，只有一条

必须把"理论上会拦"和"真的拦过"分开：

- **有实证**：`AcceptItem` 的"running 且 handle 在册 → 拒"——事故当时 `team_accept` 两次拒收，
  逼出 leader 退役句柄才收尾。闸门有效，但它拦的是**丢失更新的连带后果**，不是真在跑。
- **未触发即未实证**：屏障闸、里程碑内依赖闸、超员闸（在跑峰值 5 < 6）、`Retire` 的
  `busyItems` 闸、`Close` 的 `unsettledItems` 闸——本轮全部一次通过，**没有留下"被它拒过"的证据**。
- 一条口径缺陷：**拒收不落审计**，所以"闸门有没有生效"只能由"没出现绕行"反推。

### 2.3 脆弱点清单

| # | 项 | 严重度 | 来源 |
|---|---|---|---|
| ① | 计划头读-改-写非原子（丢失更新） | 高 → **已闭** | 本轮实测（修复前 3/3 失败，修复后全绿） |
| ② | `planLocks` 包级表不回收 | 低 | 读码 |
| ③ | `DispatchItem` 锁内跑 `git worktree add` | 中 | 读码（reviewer） |
| ④ | **收口后无 `closed` 闸**：`TeamworkStateClosed` 只在 `Close` 里出现，`DispatchItem` 不读 `State` ⇒ `team_close` 之后仍能派发待派项，把 `running` 写回一份 `State=closed` 的计划 | **高** | 读码（reviewer 本轮发现） |
| ⑤ | **`team_recover` 对"终态作业仍在册"不报**：`Interrupted = running && !handleAlive`，而 jobs I-4 下终态记录仍留内存表 ⇒ `Observe` 恒真。实测 a2/a4 已 done 却被报 `可重派=0` | **高** | 本轮实测 |
| ⑥ | `-race` 不覆盖跨调用原子性 | 中 | 实测 |
| ⑦ | 文档漂移：`README.md` 的 worktree 前缀、`runtime_teamwork_schema.go` 的 "every stage in `after`" | 低 | 可核对 |

### 2.4 反方观点：换成"单代理串行 + 无隔离"会丢什么

- **并行**：本轮 5 路勘察并发约 90 秒；串行大约 5 倍。
- **独立证据**：verifier / reviewer 在**自己的会话**里才抓出与 leader 题面相反的结论
  （Contradiction、schema 残句、④ 这条缺口）——同一上下文里的自我复核抓不到这些。
- **权责**：只读权责让"评审"这件事本身可授权、可免审批。

**生产上最缺的那一条**：**工作项的持久化终态判据**。现在"在不在跑"要问内存作业表
（`handleAlive` → `jobs.Observe`），于是 `accept` / `recover` / `close` 三个闸门都在"猜" handle
的含义。④⑤ 与本轮那次卡死绕行都从这一处生出来。把"在跑"从内存投影改成落盘事实，是这套
机制从"能演示"走向"生产可用"的那一步。

---

## 3. 后续项（本轮未做，建议按序）

1. **收口后 `closed` 闸**（高）：`DispatchItem` / `PlanMilestone` / `AdjustItem` 读
   `plan.State.State`，closed 即显式拒绝；顺带把"拒收不落审计"补上（拒收也留一行）。
2. **`recover` 的终态判据**（高）：别只看 `handleAlive`；作业终态要能从持久面读到。
3. `DispatchItem` 的慢操作（`BindWorkspace`）移出临界区（中），与 `SettleWorkItem` 对称。
4. 文档漂移两处（低）：`README.md` 前缀、schema 里的 stage 残句。

---

## 4. 证据索引

| 段 | 来源 | 落点 |
|---|---|---|
| 修复本身 | `wi-impl`（exec） | `seelebridge/teamwork/planlock.go`、`coordinator.go`、`items.go` 两段式 |
| 判别力 | `wi-race`（test_case） | `seelebridge/teamwork/items_concurrent_test.go`、`docs/devlog/2026-10-05-concurrent-settle-lost-update-regression.md` |
| 完整性 / 死锁 / 缺口 | `wi-review`（reviewer） | 11 个写点逐条核对；④ 为本轮新发现 |
| 评价 | `wi-evaluate`（assessor） | 本轮审计流 + 上述两份 devlog |
| leader 复跑 | — | `go test ./seelebridge/teamwork/ -count=1` → `ok 1.029s` |
