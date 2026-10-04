# 2026-10-05 teamwork 自体勘察：用 teamwork 读 teamwork

> 本文由 **leader 会话**在 teamwork 编排下产出：1 个 leader（编排 + 汇总）+ 6 个
> 只读 teammate（5 份并行勘察 + 1 份交叉验证），2 个里程碑。
> 每条机制结论都带 file:line 或测试名；无法落到落点的写 Hypothesis。
>
> goal：`g-1`；team_id：`teamwork-self-survey`；上限 `limits.team.max_teammates = 6`。

---

## 0. 这次是怎么跑出来的（teamwork 自己跑自己）

| 里程碑 | 屏障 | 工作项 | 角色 | 结果 |
|---|---|---|---|---|
| `m-map` 测绘 | `depends_on: []` | `wi-bridge` | bridge | done |
| | | `wi-store` | store | done（作业已 done、尾插未落地，leader 退役句柄后收尾） |
| | | `wi-runtime` | runtime | done |
| | | `wi-surface` | surface | done（同 wi-store） |
| | | `wi-spec` | spec | done |
| `m-reconcile` 对账 | `depends_on: [m-map]` | `wi-reconcile` | verifier | done |

全部 6 个 teammate 用 `tools_policy: "readonly"`——本次是**勘察**，不是改代码，只读权责
把裁决从"观点"变成"能跑能读的证据"。屏障确实生效：`m-map` 全 done 之前，
`team_work(m-reconcile)` / 派发会被拒收（实测 `team_work` 通过即屏障已开的证据）。

---

## 1. 机制描述

### 1.1 计划形状：两张看板

```
Milestone 1 ──屏障──▶ Milestone 2 ──▶ …
  ├── Work Item A ──┐        milestones[].depends_on 串行（milestoneOpen 闸门）
  └── Work Item B ──┘        work_items[].depends_on 里程碑内 DAG
```

- **两种顺序事实，仅此两种**：里程碑之间 `milestones[].depends_on`（屏障），里程碑内
  `work_items[].depends_on`（DAG）。跨里程碑的 item 依赖在校验层被拒
  （`sessionstore/teamwork_items.go`）。
- **里程碑状态是算出来的**，不是写进去的：`recomputeMilestones` → `resolveMilestoneStatus`
  （`seelebridge/teamwork/items.go`）。判据两条都成立才算 done：① 依赖里程碑全 done（屏障）；
  ② 自己的工作项全 done。**空工作项的里程碑屏障开了算 `active`、但永远不算 done**（"没排活 = 还没干"）。
- **阶段制已整条退场**：`TeamworkPlan` 里没有 `Stages`；旧计划里的 `stages` 被 JSON 忽略；
  工具 schema 里也没有 `stages` 属性。唯一残留是 `team_dispatch(role, goal)` 老入口仍在 live。

### 1.2 leader 工具面（13 个）

注册处 `seelebridge/runtime_teamwork.go` `registerTeamworkTools()`，schema 全在
`runtime_teamwork_schema.go`：`team_plan` / `team_work` / `team_item` / `team_dispatch` /
`team_join` / `team_milestone` / `team_retire` / `team_close` / `team_accept` / `team_fail` /
`team_recover` / `team_items` / `team_context`。

- `team_plan`：`team_id` / `version` / `members[]` / `milestones[]`；member 有
  `role` / `role_session_id` / `worktree` / `tools_policy(readonly|readwrite)` /
  `permission_groups`（路由组 → 位，逐格分配，优先于档位）。**无 stages**。
- `team_dispatch`：`{role?, item?, goal?}` 双入口——给了 `item` 就走 Work Item 口径
  （屏障 + 依赖 DAG + 一 item 一套 Session/worktree），否则走 teammate 级老口径。
- `team_context` 是**非消费读**（`Peek`，游标不推进）。注意：`team_join` 走的是
  `jobs.Observe`，**"Peek 语义"的真身在 `team_context` 而不是 `team_join`**——SKILL 里
  把两者写在一起容易误读。
- teammate 工具面**硬移除 `fork_subagents`**（`runtime_role_turn.go` 里同时过滤 `VisibleTools`
  与在 Dispatch 处直接拒绝）；理由：防绕过 `max_teammates` 与孙级 worktree。

### 1.3 持久面（三份文件，都在会话目录内）

| 面 | 文件 | 语义 |
|---|---|---|
| 计划头 | `<sessionRoot>/metadata/teamwork.json` | `moduleTeamwork` head，原子替换 + checksum 自愈读 |
| 审计 | `<sessionRoot>/teamwork/events.jsonl` | append-only，单次 write 落 `O_APPEND`，崩溃残尾跳过 |
| 绑定账本 | `<sessionRoot>/teamwork/worktrees.jsonl` | append-only **KV 语义**：按 `work_item` 取最后一行；`released=true` 即出账 |

- 审计 kinds：`plan / dispatch / join / milestone / retire / close`，外加
  `item / accept / fail / settle / recover / message`。
- **会话作用域是结构性的**：`sessionRoot = root / "project-" + hash(ProjectID) / "session-" + hash(SessionID)`，
  `hash = sha256[:8]` hex。所以路径是 **`session-<hash(sessionID)>`，不是 `session-<sessionID>`**；
  计划头只在其自身会话目录里可寻址，不存在一个全局计划文件可以被异地写坏。
  （"跨会话被拒"应表述为**结构性隔离 + 空作用域拒绝**，sessionstore 并没有一道显式的跨会话 ACL。）
- 写入前统一 `key.validate()`（SessionID 非空）。

### 1.4 执行体与装配：leader 不跑人

两级装配：组合根注入后端面 → `Runtime` 把自身各方法当端口填进 `teamwork.Options`。

- 作业类别 `Kind = "worker"`（`teamwork/teamwork.go` `KindWorker`）；
  作用域 `jobs.Scope{Session: 主会话, Subject: "emp_<role>"}`——`Subject` 同时是**权限主体**，
  权限面因此不需要第二套映射。
- 执行体 ctx 从 `jobs.Background` 派生（作业会活过派发它的那一轮），所以**会话归属与工作正文
  一律随载荷走**（`WorkerRequest.MainSessionID` / `Goal` / `OutputPath`），不能指望 ctx 里还有主会话。
- `Coordinator` **自己不跑任何 teammate**：它只掌控顺序、作用域与收口。这就是
  "leader 阻塞与否与 worker 是否推进正交"的落点。
- 端口清单（`teamwork.Options`）：`PlanStore` / `Jobs`（必填）+ `Workers` / `Worktrees` /
  `Spaces`(Workspaces) / `Teammates` / `Sessions` / `Boards` / `JobOutputs`（可选）。
  **`ItemSettler` 是唯一不在 Options 里的端口**——它由 `WorkerExecutor(runner, settler, …)`
  注入，而生产上 `runner` 与 `settler` 都是同一个 `Runtime`。缺端口的动作一律**显式报错**，
  `Boards` / `JobOutputs` 缺失按"没装配"降级。
- **尾插是自动的、有硬编码顺序**：`WorkerExecutor.Start` 在 `RunWorker` 返回后调
  `SettleWorkItem` → ① 先 `MergeWorkspace`（改动回到主干）→ ② 再 `EnqueueTeammateMessage`
  （有界一行，≤240 字，合并失败也插并写明"请 leader 亲自执行合并"+ bug 原文）→ ③ 状态落
  `review` / `failed`。顺序反了就会出现"leader 已看到结论、而主干上还没有这份改动"。
- 输出正文归产品：派发时 `JobOutputPath(role)` 分配 `<sessionRoot>/teamwork/jobs/<role>-<n>.log`，
  同时写进 `WorkerRequest.OutputPath` 与 `jobs.Spec.OutputPath`；框架不建写句柄、只按偏移读，
  销项/驱逐都不删，**整队收口时 `ClearJobOutputs` 一并清掉并作废落点**。

### 1.5 Work Item 生命周期状态机

```
pending ──DispatchItem──▶ running ──SettleWorkItem──▶ review ──AcceptItem──▶ done
                            │                          │
                            │                          └──FailItem──▶ failed ──AcceptItem（销项）──▶ done
                            └── 派发失败：撤现场，状态不动
```

| 转换 | 闸门 / 副作用 |
|---|---|
| `DispatchItem` | 屏障开 + 依赖全 done + 角色在编 + 未超员；建 Session（`derive(main,team,role) + "-wi-" + itemID`）+ worktree（`seelex/<role>-<itemID>`）+ 绑定行 + 审计；`Dedup="teamwork:item:"+id`（同一工作项的重复派发折叠到在跑那一条） |
| `SettleWorkItem` | 只对 `running` 生效（幂等）；先合并、再尾插、后写态；合并失败或 run 失败 → `failed` 并留下现场 |
| `AcceptItem` | `review` 放行；`running` 且 **handle 仍在册** → 拒（等尾插走完）；`failed` 也放行（收口要求账先收干净）；`done` 幂等。副作用：释放 worktree + 清这一件事的会话 + 追加释放行 → `done` |
| `FailItem` | `done` 拒；只落 `failed`/`Note`，**不动现场**（重派复用同一会话号，记忆留着） |
| `Recover` | 只读＋审计，**不动会话与现场**：把"状态说在跑、而本进程作业表里查不到句柄"显式化为可重派 |
| `Close` | 唯一回收点：`unsettledItems` 闸门（真在跑的 / failed 未处置的拒收）→ 逐在编成员四步 → `releaseAllItems` → `ClearJobOutputs` → 封板看板 → 标 closed → 一条 close 审计。幂等（第二次 `already_closed`） |
| `Retire` | 只结束一轮、只释放工作区与清会话内容，**不回收作业**；名下还有在跑/待验收的工作项会被拒 |

### 1.6 约束与铁律（代码落点）

- 在编 ≤ `limits.team.max_teammates`（默认 6，`config/seelex.yaml`）；超限**显式拒绝、不静默排队**。
- 一角色一 teammate，禁 `main` / `user`，角色与 `role_session_id` 都唯一。
- 顺序只进计划，不进调用姿势：`milestones[].depends_on` + `work_items[].depends_on`。
- "开始与结束是既定事实"：`team_item` 只改得动 `pending` 的工作项。
- 只读权责给评审；赋权即免审批（位内直通）。
- teammate 不挂子代理（`fork_subagents` 硬移除）。

---

## 2. 本轮实测异常：**并发 settle 丢失更新**（Confirmed）

**现象**：5 个工作项并发派发，作业全部 `exit=0`，但 2 个（`wi-store` / `wi-surface`）在 jobs
报表已 `done` 的情况下，**计划状态仍停在 `running`**，且 handle 仍在册。

**为什么卡死**：`team_accept` 的放行判据是 `review` 或 `running 且 handle 不在册`；这里是
`running + handle 在册` → **拒**。而 `team_recover` 的 `Interrupted` 判据是
`running && !handleAlive`（`jobs.Observe`）——终态记录**仍留在内存作业表**（I-4），同进程
`Observe` 依旧为真，所以它**不报**。于是这一项既进不了验收、也不进可重派清单。

**根因（verifier 复核）**：`SettleWorkItem` 是
`ReadPlan →(:458 MergeWorkspace，慢 git) →(:544) WritePlan` 的**读-改-写**，
**全程没有跨步的锁**（Coordinator 无 mutex；store 的模块锁只覆盖单次 `commitModuleHead`）。
并发 settle 各持陈旧快照写回，**后写覆盖先写** → 已落 `review` 的项被覆盖回 `running`。
本次 5 个 settle 在 ~30 秒内交错完成，3 个落地、2 个被覆盖，与"丢失更新"的形态一致。

**规避 / 处置**（本次实际做法）：用 `jobs_manage(op=done, handle)` 退役那个已终态的作业句柄，
使 `handleAlive` 转假，`team_accept` 随即放行——这正是文档里"重启后收尾"那条路。

**未闭的证**：仓库内没有一条并发 settle 的 `-race` / 交错 repro 把丢失更新钉死。
建议补：两个 work item 并发 settle，断言两条都落到 `review`。

---

## 3. 口径更正（文档 / README ≠ 代码）

| # | 处 | 写的是 | 实际是 |
|---|---|---|---|
| 1 | `seelebridge/teamwork/README.md` | worktree 指派名 `seelebridge/<role>-<itemID>` | 代码 `WorkItemWorktreeName` 产出 `seelex/<role>-<itemID>`（实测 `seelex/store-wi-store`） |
| 2 | `runtime_teamwork_schema.go` `teamworkMilestoneDescription()` | "every stage in `after` must have dispatched…" | 该 schema 参数只有 `id` / `content`，`after` 早已不存在——**唯一漏网的 stage 残句** |
| 3 | README / arch 的"两道闸" | 装配期 `SetPlan` + 持久化点各一道 | 对**计划**只有持久化一道（`ValidateTeamworkPlan` 只在 `WriteTeamworkPlan` 内被调）；`SetPlan` 只做归一化。所谓"装配闸"是 `agentteam.Normalize` 对 `TeamSpec`，不是对 plan |
| 4 | 勘察命题里的路径 | `session-<sessionID>` | `session-<hash(sessionID)>`，`hash = sha256[:8]` hex |
| 5 | 工作桌"四源" | plan / tasklist / subagent / todo | 第四源是 **asyncRuns**（后台登记表投影）；todo 只是注册表里 `kind=todo` 的 task |
| 6 | `application/core/agentteam/README.md`、`docs/arch/a2a-agent-team-factory.md` | "顺序的唯一事实是 `stages[].depends_on`" | 应为 `milestones[].depends_on` |

另有两条属**已知边界**而非缺陷，留档：

- `Spec.Dedup` 的**判据实现在外部 Seele 模块**（`vendor` 目录不在本仓库），仓库内只设键；
  "同载荷才折叠"的说法无法在仓库内核到 → 记为 Hypothesis。
- `team_dispatch(role, goal)` 老入口仍 live，属 M4 清场范围。

---

## 4. 证据索引

| 结论段 | 来源工作项 | 关键落点 |
|---|---|---|
| 1.1 计划形状 | `wi-bridge` / `wi-spec` | `items.go` `recomputeMilestones`、`sessionstore/teamwork_items.go` 跨里程碑依赖校验 |
| 1.2 工具面 | `wi-surface` | `runtime_teamwork.go` `registerTeamworkTools()`、`runtime_teamwork_schema.go`、`runtime_role_turn.go` |
| 1.3 持久面 | `wi-store` | `sessionstore/teamwork.go`、`module_heads.go`、`teamwork_items.go` |
| 1.4 执行体 | `wi-runtime` | `runtime_teamwork.go`、`teamwork/executor.go`、`runtime_teamwork_items.go`、`runtime_teamwork_output.go` |
| 1.5/1.6 生命周期与约束 | `wi-bridge` | `teamwork/items.go`（DispatchItem/SettleWorkItem/AcceptItem/FailItem/Recover）、`coordinator.go`（Close/Retire/retireSteps）、`sessionstore/teamwork.go` `ValidateTeamworkPlan` |
| 2. 丢失更新 | verifier | `items.go` SettleWorkItem 的 RMW、`module_heads.go` 锁粒度、`items.go` `handleAlive` |
