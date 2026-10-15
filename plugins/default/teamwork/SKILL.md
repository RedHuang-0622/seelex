---
description: Teamwork（leader + 异步 worker）规范：编制尽量少而关键的 teammate、支持一个 teammate 多任务并发、任务之间尽量解耦、完成一件交一件
---

# Teamwork：里程碑甘特 + Work Item 的 leader 工具面

你是**主代理（leader）**。团队**不是一排轮流发言的座位，没有席位轮转**；
团队 = 一张**里程碑甘特**：里程碑之间**串行**（屏障），里程碑内部按 **Work Item 依赖 DAG 并行**。
本文件是 leader 的工具面规范：用 `team_*` + `jobs_manage` 把一条 V 模型流水线跑完并留证据。

口径锚点：`docs/arch/teamwork-leader-worker-architecture.md`、`docs/devlog/2026-10-03-workitem-milestone-refactor.md`。

## 0. 两张看板（先记住形状，再记住工具）

```
Milestone 1  ← 阶段级屏障（milestones[].depends_on 串行）
  ├── Work Item A ──┐
  ├── Work Item B ──┼── 里程碑内可并行（work_items[].depends_on 的 DAG）
  └── Work Item C ──┘
        ↓ 全部 done
Milestone 2  ← 下一个阶段才开

Teammate（在编）
  ├── Work Item A   ← 每件事各自一套 Session + git worktree
  └── Work Item B
```

- **执行隔离规则**：**一个 Work Item 一个 Session + 一个 worktree**（专项专做；两件事的上下文与改动不互相污染）。
- **调度规则（支持多任务并发）**：**一个 Teammate 可以同时跑多个 Work Item** —— 按 `item` 派发，
  每件各自一套 Session + worktree，**互不折叠**（计划头的读-改-写已原子化，并发 settle/accept 不丢更新，
  见 `seelebridge/teamwork/items_concurrent_test.go`）。要并行广度**先靠"一个人并发多件"**，人还是不够再加
  teammate；**按 `role` 派发**（旧姿势）会折叠到在跑的作业，别用它来并发。不许让 teammate 自己挂子代理
  —— 它的工具面里**没有** `fork_subagents`。
- **甘特不表示时间**：只表示依赖（Work Item 之间、Milestone 之间）。

## 1. 生态位

- **你（leader）**：编排（排活 / 派发 / 观察 / 汇合 / 验收 / 收口）+ 关键路径工作；**不用座位表达自己**。
  TL 那一关（复核 / 验收 / 终止团队）也是**你**自己的活：不设 `tl` 角色、不占 teammate 席位。
- **teammate（员工 / 评审者）**：一角色一 teammate；干活方式 =「在角色会话里带工具跑有限回合」，
  由你按 **Work Item** 派发；**一个 teammate 可以并发持有多件**（每件一套 Session + worktree）。
  **编制原则 = 尽量少但关键**：按**职责边界**定人，不按任务条数定人。

## 2. 工具面

| 工具 | 关键参数 | 你用它做什么 |
|---|---|---|
| `team_plan` | `team_id, members[], milestones[]` | 定义/整份替换**在编成员与里程碑**。里程碑的 `depends_on` 是屏障。**同名里程碑下已排好的工作项与运行态会被保留**——改成员不会把已经干到一半的活抹掉。（`stages` 已整条退场：它不在 schema 里，也不被任何读侧认作顺序事实。） |
| `team_work` | `milestone, items[]` | 给**当前这一步**排活：`{id, role, name, description, goal, depends_on}`。依赖未完成的里程碑会被**拒收**——分里程碑排活，不是一次把全程铺好。 |
| `team_item` | `id, role?, name?, description?, goal?, depends_on?` | 调整**尚未开始**的工作项。已开始（running/review）与已结束（done/failed）的是**既定事实**，改不了。 |
| `team_dispatch` | `item`（或旧的 `role, goal`） | 派发**一个工作项**，立即拿 `handle`（**不等待**）。屏障没开、前置没 done、真超员，都会**显式拒绝**。 |
| `team_context` | `roles[], include_body, max_bytes` | **只读**看成员工作上下文。**非消费读法**：不派发、不回收、不销项，游标不推进。 |
| `team_items` | 无 | 读回全部工作项：状态 / 归属 / 依赖 / 这件事的 Session 与 worktree / 是否可重派，以及**卡点**两格——`gate`（这件事此刻卡在哪：`gate:依赖未 done` / `job:running` / `job:句柄已作废（可重派）` / `merge:改动未合并待处置` / `review:等你裁决` / `failed:待人工处置` / `ready:可派发` / `done:已验收`）与 `next`（清掉这个卡点该做的动作）。**先读这两格再决定做什么**，别靠轮询反推。 |
| `jobs_manage` | `op=observe\|fetch\|kill\|done, handle` | 观察 / **消费式取回** / 终止 / 销项一个**团队作业**（句柄形如 `bteam-7`；后台作业的 `bt-` / `bs-` 归另一个工具 `bg_manage`，拿错会被显式拒绝）。`fetch` 取尽即销项——**取回产出优先用 `team_context`**。验收通过会替你销掉那条作业行，不必自己 `op=done`。 |
| `team_join` | `handles[], budget_ms` | **有界**汇合等待（真实强依赖点才用）。只观察、**不取回输出**。 |
| `team_accept` | `id, note` | **验收通过**：工作项 → done，**并销掉这一件的作业行**（不必再自己 `jobs_manage(op=done)`），同时结束这件事的执行隔离（释放它的 worktree、清它的会话；下一件事重新开一套）。**这也是打开里程碑内下一步的时刻**（依赖闸门） 。 |
| `team_fail` | `id, note` | **判定不通过**：现场与记忆**都留着**，可以重派（重派复用同一个会话号 → 上下文记忆还在）。 |
| `team_recover` | 无 | 中断恢复（额度耗尽 / 重启）：读回计划与绑定账本，明确列出**可重派**的工作项。它**不动**任何会话与工作区。 |
| `team_milestone` | `id, content` | 声明里程碑 + **由你撰写内容**（旧口径：判据是 `after` 的阶段派发过）。Work Item 口径下里程碑状态是**算出来的**（依赖 done + 它下面全部工作项 done）。 |
| `team_close` | 无 | **整队收口**（唯一入口）：逐在编成员四步 → 所有活绑定（Work Item 的 Session + worktree）**一并结束** → 封板看板 → 计划标 closed。幂等。 |

## 3. 节奏（默认「忙自己的事」，点状汇合）

1. `team_plan`：一次写全**成员 + 里程碑屏障**（`milestones[].depends_on`）。要加角色先算人数
   （`limits.team.max_teammates`，默认 6，一角色一 teammate，禁 `main`/`user`）。
2. `team_work(milestone, items)`：给**第一个里程碑**排活——一个里程碑一件事一条 item，
   写清「名称 + 描述 + 达成目标（验收判据）」。
   - V 模型的依赖边放**里程碑内**：`impl` 那条 item 是 `test_case` 那条的 `depends_on`
     ——exec 做完并验收，test 才被放行（这就是"milestone 内部支持依赖关系"的落点）。
3. `team_dispatch(item=...)` 派当前可派的工作项：依赖已 done 的可以并发多派，**同一个 teammate 也可以
   同时持有多件**（按 `item` 派发才并发；按 `role` 派发会折叠到在跑的那件）。拿 `handle` 继续做自己的
   关键路径活，**默认不干等**。
4. **尾插是自动的**：teammate 跑完 → 先合并这件事的 worktree → 成功就插一条有界回执到它的消息队列；
   合并失败也会插，正文写明「插入失败、请 leader 亲自执行」并把 bug 原文打印进去。你在回合边界读这条，
   不必轮询。
5. **验收（完成一件交一件）**：读证据（`team_context` / `team_items` / `git diff`）→ 通过就
   `team_accept(id, note)`（工作项销项、这件事的隔离结束、下游依赖闸门打开）；不通过就 `team_fail(id, note)`
   再重派。**teammate 不必等同一批全部做完才交**：做完一件即可 settle、你就逐件验收，**别攒批**
   （里程碑屏障只约束"里程碑何时开下一步"，不约束交付粒度）。
6. **本里程碑全部 `done` → 屏障打开** → 回到第 2 步给**下一个里程碑**排活。
7. 全部里程碑 done → `team_close` → goal 收口（`goal_done`）。
   **也可以中途提前 `team_close`**（例如范围被砍掉）；已完成的结论不会被收口改写。

## 4. 排活模板（照抄后按任务改名/裁剪）

```jsonc
// 1) 先写成员 + 里程碑屏障（工作项**不在这里**：分里程碑、用 team_work 排）
{
  "team_id": "v-model", "version": 1,
  "members": [
    { "role": "pm",              "tools_policy": "readwrite" },
    { "role": "arch",            "tools_policy": "readwrite" },
    { "role": "contract_review", "tools_policy": "readonly" },
    { "role": "exec",            "tools_policy": "readwrite" },
    { "role": "test_case",       "tools_policy": "readwrite" }
  ],
  "milestones": [
    { "id": "m-design",   "name": "设计",   "depends_on": [] },
    { "id": "m-contract", "name": "契约",   "depends_on": ["m-design"] },
    { "id": "m-impl",     "name": "实现",   "depends_on": ["m-contract"] },
    { "id": "m-verify",   "name": "验证",   "depends_on": ["m-impl"] }
  ]
}

// 2) 给当前里程碑排活（里程碑内 DAG：test 依赖 impl）
{
  "milestone": "m-impl",
  "items": [
    { "id": "wi-impl", "role": "exec", "name": "实现契约", "goal": "按 contract_review 的结论填实现，跑通编译" },
    { "id": "wi-unit", "role": "test_case", "name": "单元回归", "depends_on": ["wi-impl"], "goal": "覆盖新增分支，全绿" }
  ]
}
```

> **依赖边只在"真实强依赖"处画**：上面 `wi-unit depends_on wi-impl` 是 V 模型的真实依赖（实现没完成就测不了）；
> **同一步里彼此独立的两条**（例如两个互不相干的文件改造）应当交给**同一个 teammate 并发**派发（各给一条 `item`），
> 而不是串成链。判定口诀：**"B 能不能在 A 之前开始？"能 → 不要加边。**

写计划的硬约束（写错会被**显式拒绝**，按提示改而不是重试）：
1. **在编成员 ≤ `limits.team.max_teammates`（默认 6）**，一角色一 teammate，角色不重复，禁 `main`/`user`。
2. **工作项 id 全计划唯一**；`role` 必须在编；`name` 必填（工作内容必须有名字）。
3. **依赖只在里程碑内**：`work_items[].depends_on` 只能指向**同一个里程碑**里的工作项
   （跨里程碑的顺序用 `milestones[].depends_on` 表达）。两者都不许成环。
4. **分里程碑排活**：只能给「依赖里程碑都已 done」的里程碑 `team_work`；派发同样受这条闸门约束。
5. **编制少而关键 + 任务解耦**：teammate **按职责边界定人**（一角色覆盖一片活，能一个人并发吃下的
   一片活就不要拆成多个 teammate；上限 `limits.team.max_teammates`，默认 6）；Work Item 之间
   **优先彼此无依赖**（无依赖才可并发），只有**真实强依赖**才写 `depends_on`。不要为了"看起来有流程"
   把一批活串成长链——里程碑内长链越多，并发度越低，一个人卡住就全线卡住。

## 5. 铁律

- **绝不唤醒忙会话**：框架不把结果 push 进忙会话。尾插落在 teammate 自己的**消息队列**上，
  作业完成经**变更信号口**（UI/投影）与**回合边界有界摘要**浮现。
- **不静默排队**：人数满 / 角色不在编 / 依赖没 done / 屏障没开，一律**显式拒绝**。被拒就改计划或收窄范围，
  不要重试等位（排队会把「人满了」伪装成「在跑」）。
- **开始与结束是既定事实**：`team_item` 只改得了**未开始**的工作项；改了历史 = 看板与审计对不上。
- **版本事实归 git**：worktree / 分支 / 提交 / 合并一律走 git；Seelex 不内置版本系统。
  工作区脏而没有任何提交时，合并会**显式报错**并把现场留给你亲自处理（不静默丢产出）。
- **只读权责给评审**：评审类角色用 `tools_policy: "readonly"`（能跑 test/lint/编译，不能改代码），
  把裁决从「观点」变「证据」。**赋权即免审批**：位内直通，位外才弹提权页。
- **人少而关键**：编制按**职责边界**，不按任务条数；能一个人并发吃下的一片活，就不要拆成多个 teammate。
- **解耦优先**：Work Item 之间**优先无依赖**（无依赖才可并发）；只有真实强依赖才加边，长链要能说出理由。
- **一个 teammate 可并发多件**：按 `item` 派发即并发（各自 Session + worktree）；要并行广度
  **先加并发件数，再加人**（按 `role` 派发会折叠，不是并发手段）。
- **完成一件交一件**：settle → 回执 → 你验收，**逐件发生**；不要求同一批同时交，更不要攒批验收。
- **teammate 不挂子代理**：它的工具面里没有 `fork_subagents`；要并行广度就按上面两条加并发件数、再加人。
- **顺序进计划，不进调用姿势**：顺序写进 `milestones[].depends_on` 与 `work_items[].depends_on`，
  不靠你「记得先叫谁」。
- **证据不代写**：teammate 没交证据的结论不进验收、不进看板。

## 6. 失败 / 中断 / 收口

- 作业终态（done/failed/killed）如实呈现（含 `exit=124` 硬超时 / `exit=137` 被杀）；不要当成「突然结束」。
- **中断恢复**（额度耗尽 / 进程重启）：先 `team_recover` 看**可重派**清单，然后 `team_dispatch(item=...)`
  重派——它会**复用同一个会话号**，上下文记忆靠这个建回来；现场（worktree）也一直留着。
- 工作项推不动：先看证据（`team_context` / `team_items`），再决定**重派**（改 `goal` 收窄范围）/
  **调整未开始项**（`team_item`）/ **收窄计划**（`team_plan`）/ `task_needs_user_decision`。
- 同一件事最多重派 3 次；到顶就上报。
- **收口三步**：① `team_close`（整队收口：回收 + 所有活绑定一并结束 + 封板 + 标 closed）；
  ② goal 收口（`goal_done`，见 `$goal`）；③ 把结论与证据写进看板与最终回复。
