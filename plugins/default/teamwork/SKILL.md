---
description: Teamwork（leader + 异步 worker）：V 模型阶段派活、契约先行、证据收尾的 leader 工具面规范
---

# Teamwork：V 模型 leader 工具面

你是**主代理（leader）**。团队**不是一排轮流发言的座位，没有席位轮转**；
团队 = 一组可被**派发 / 观察 / 汇合 / 终止 / 收口**的 worker 作业，顺序由**你**写进团队计划。
本文件是 leader 的工具面规范：用 `team_*` + `jobs_manage` 把一条 V 模型流水线跑完并留证据。

口径锚点：`docs/arch/teamwork-leader-worker-architecture.md`（§0 决策 / §4.5 工具面 / §4.6 team plan / §6 信号）。

## 1. 生态位

- **你（leader）**：编排（派发 / 观察 / 汇合 / 收口）+ 关键路径工作；**不用座位表达自己**。
  TL 那一关（复核 / 收口 / 终止团队）也是**你**自己的活：**不设 `tl` 角色**、不占 teammate 席位。
- **teammate（员工 / 评审者）**：一人一会话一 worktree，**一角色一 teammate**；
  干活方式 = 「在角色会话里带工具跑有限回合」，由你派发；**teammate 不挂子代理**。
- **顺序的唯一事实**：`stages[].depends_on`——不是「上一轮是谁」的隐式状态，也不是你的调用姿势。
- **收口的唯一入口**：`team_close`——它是全队**唯一**回收作业的地方；逐人一轮结束是 `team_retire`
  （只放工作区、清上下文，**不动作业**）。

## 2. 工具面（leader 编排面 + 作业管理）

| 工具 | 关键参数 | 你用它做什么 |
|---|---|---|
| `team_plan` | `team_id, stages[], members[], milestones[]` | 定义/整份替换硬编排计划；顺序只在 `stages[].depends_on` 里。同一 `team_id` 的计划**整份替换**，不是打补丁。 |
| `team_dispatch` | `role, goal` | 派发一个 teammate 的作业，立即拿 `handle`（**不等待**）。`goal` 正文里写清「阶段 id + 交付物 + 证据要求 + 边界」。 |
| `team_context` | `roles[], include_body, max_bytes` | **只读**看成员工作上下文（在编条目 + 它名下最新作业行 + 可选正文）。**非消费读法**：不派发、不回收、不销项，游标不推进。 |
| `jobs_manage` | `op=observe\|fetch\|kill\|done, handle` | 观察 / **消费式取回** / 终止 / 销项一个作业。`fetch` 取尽即销项——**取回产出优先用 `team_context`**。 |
| `team_join` | `handles[], budget_ms` | **有界**汇合等待（真实强依赖点才用）。只观察、**不取回输出**。 |
| `team_milestone` | `id, content` | 声明里程碑 + **由你撰写内容**。判据是依赖边（`after` 里每个阶段至少派发过一次），不是墙钟。 |
| `team_retire` | `role` | 结束某 teammate **一轮**任务：释放 worktree → 清会话内容 → **保留在编**。**不回收作业**——作业正文活到 `team_close`。工作区脏会显式报错。 |
| `team_close` | 无 | **整队收口**（唯一入口）：逐在编成员走同一套四步（**回收作业在这里**）→ 封板看板（`closed` / `team.close`）→ 计划标 closed → 落 `close` 审计。幂等；第二次返回 `already_closed`。 |

## 3. V 模型阶段模板（照抄后按任务改名/裁剪）

左腿（分解）→ 右腿（验证）**一左一右配对**，配对的锚点是完成条件。
**末尾不设 `review` 阶段**：TL 的复核与收口由**你本人**做——leader 就是 TL，不占席位、不配 `tl` 角色，
它的裁决以「里程碑 content + 看板打点 + 收口动作」的形式落在你自己的回合里。

```jsonc
{
  "team_id": "v-model",
  "version": 1,
  "stages": [
    // 左腿：需求 → 设计（含契约）→ 契约评审 → 实现
    { "id": "req",             "roles": ["pm"],              "depends_on": [] },
    { "id": "design",          "roles": ["arch"],            "depends_on": ["req"] },
    { "id": "contract_review", "roles": ["contract_review"], "depends_on": ["design"] }, // 只读：接口/签名/DTO/错误语义/不变式
    { "id": "impl",            "roles": ["exec"],            "depends_on": ["contract_review"] },
    // 右腿：验收 → 集成 → 单元（各自依赖它配对的左腿阶段 + 实现）
    { "id": "accept",          "roles": ["test_case"],       "depends_on": ["req", "impl"] },    // ↔ 需求
    { "id": "integration",     "roles": ["test_case"],       "depends_on": ["design", "impl"] }, // ↔ 设计
    { "id": "unit",            "roles": ["test_case"],       "depends_on": ["impl"] }             // ↔ 契约/模块
  ],
  "members": [
    { "role": "pm",              "worktree": "seelex/pm" },
    { "role": "arch",            "worktree": "seelex/arch" },
    { "role": "contract_review", "tools_policy": "readonly" },
    { "role": "exec",            "worktree": "seelex/exec" },
    { "role": "test_case",       "worktree": "seelex/test-case" }
  ],
  "milestones": [
    { "id": "m-design",   "after": ["design"] },
    { "id": "m-contract", "after": ["contract_review"] },
    { "id": "m-impl",     "after": ["impl"] },
    { "id": "m-verify",   "after": ["accept", "integration", "unit"] }
  ]
}
```

写计划的四条硬约束（写错会被 `team_plan` 显式拒绝，按提示改而不是重试）：

1. **在编成员 ≤ `limits.team.max_teammates`（默认 6）**，且一角色一 teammate、角色不重复、
   `role_session_id` 不重复。模板正好 5 人（**复核那条不占席位**）：**要加角色就先裁阶段，先算人再写 `stages`**。
2. **一个角色只在一个阶段里当主语**：该角色的每次派发都归到它**首次出现**的那个阶段
   （作业行归属、审计 Stage、里程碑判据都按这个归属）。三层验证共用一个 `test_case`（同一支常驻 teammate
   分三轮干活）时，里程碑 `after` 只挂它首次出现的阶段（`accept`），另两层的证据写进里程碑
   `content` 与 goal 看板；上限放宽后把三层拆成三个角色，各自挂里程碑。
3. **`depends_on` 只能指向本计划里已有的阶段，且不许成环**；右腿阶段要同时依赖它配对的左腿阶段与 `impl`。
4. **里程碑的 `after` 必须指向已有阶段**（`team_milestone` 的判据是「`after` 里每个阶段至少派发过一次」），
   `required` 只能列在编角色。

要点：

- **契约先行**：`contract_review` 出结论前不派 `impl`；`impl` 只填契约，不改契约。
- **契约变更 = 显式 replan**：改 `team_plan`（重建依赖）+ 看板记一条 `decision` 打点。
- **轻任务不开 V 模型**：单点速改直接做（必要时只派 1 个 teammate），别为一张小改动铺整条 V 模型。

## 4. 派活时序（默认「忙自己的事」，点状汇合）

1. **`team_plan`** 一次写全阶段/成员/里程碑；计划可改写，但改写 = 显式 replan，并同步看板 decision 打点。
2. **`team_dispatch(role, goal)`** 派当前依赖已满足的阶段（可并发多派）；拿到 `handle` 继续做自己的关键路径活，
   **默认不干等**。
3. **只在真实强依赖点 `team_join`**（有界）；用户输入与审批会排队，别把它当轮询用。
4. **阶段收尾**（固定动作，见 §5）→ 依赖边满足才派下一阶段。
5. **全部右腿阶段证据齐 → 你亲自复核（TL 那一关）** → 里程碑收口 → 团队收口
   （`team_close`：回收 + 封板）→ goal 收口。

## 5. 每阶段收尾（leader 的固定动作）

1. **取回产出**：`team_context(roles=["<阶段角色>"], include_body=true)` —— 这是**非消费**读法
   （不推进游标、不销项），正文因此活到收口。`jobs_manage(op=fetch)` 是**消费式**（取尽即销项），
   只在"我确实要把这条作业从工作表格上拿走"时用；销项统一在 `team_close`。
2. `git diff` / 该阶段的提交与文件核对改动事实——**版本事实归 git**，不凭描述。
3. 看板打点：`阶段 id + 结论 + 证据锚点 + 未决项`（必要时改写目标正文 / 完成条件）。
4. `team_milestone(id, content)`：content **你写**（交付了什么 + 证据 + 下一步）。
5. `team_retire(role)`：释放 worktree、清上下文，teammate 保持在线待下一轮——
   **它不回收作业**（作业与正文一直留到 `team_close`）。

## 6. 铁律

- **绝不唤醒忙会话**：框架不把结果 push 进忙会话。作业完成经**变更信号口**（UI/投影）与
  **回合边界有界摘要**（完成行 ≤512B）浮现——你在自己的回合边界读摘要收敛，不期待别人打断你。
- **不静默排队**：人数满 / 角色不在编 / 重复角色，`team_dispatch` **显式拒绝**。
  被拒就改计划或收窄范围，不要重试等位（排队会把「人满了」伪装成「在跑」）。
- **一角色一 teammate**：重复角色、内置角色（`main`/`user`）都被拒。
- **teammate 不挂子代理**：teammate 工具面里没有 `fork_subagents`（硬移除）。要并行广度就**你多派几个 teammate**。
- **版本事实归 git**：worktree / 分支 / 提交 / 合并一律走 git；Seelex 不内置版本系统。
- **顺序进计划，不进调用姿势**：顺序与阻塞写进 `stages[].depends_on` 与显式 `team_join`，
  不靠你「记得先叫谁」。
- **只读权责给评审**：`contract_review` 以及任何评审阶段用只读成员（能跑 test/lint/编译，不能改代码），
  把裁决从「观点」变「证据」。
- **作业正文活到收口**：退了场也不回收作业（`team_retire` 只放工作区、清上下文），正文文件归产品
  （`jobs.Spec.OutputPath`），销项 / 驱逐 / Close 都不由框架删——所以"阶段收尾读一次产出"不会把
  证据带走；`team_close` 才是回收与清理的那一刻。
- **证据不代写**：teammate 没交证据的结论不进里程碑、不进看板。

## 7. 失败 / 收口

- 作业终态（done/failed/killed）在 `jobs_manage`/`team_context` 的读数里如实呈现（含 `exit=124`
  硬超时 / `exit=137` 被杀）；不要当成「突然结束」。
- 阶段推不动：**先看证据**（`team_context` / `jobs_manage(op=observe)`），再决定
  **重派**（`team_dispatch`，改 `goal` 正文收窄范围）/ **收窄计划**（`team_plan`）/ **`task_needs_user_decision`**。
- 同一件事最多重派 3 次；到顶就上报。
- 收口前核对：右腿三阶段证据齐、**你自己复核过**、里程碑齐、看板完成条件逐条对得上。
- **收口三步**：① `team_close`（整队收口：回收作业 + 封板看板 + 计划标 closed）；② goal 收口
  （`goal_done`，见 `$goal`）；③ 把结论与证据写进看板与最终回复。团队收口**不需要**先把每个人
  `team_retire` 一遍——`team_close` 逐在编成员走的就是同一套四步。
