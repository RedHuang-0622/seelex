# 2026-10-05 teamwork 整体评价与 LoopX 对照

> goal：`g-3`｜team_id：`teamwork-vs-loopx`｜里程碑：`m-profile`（两画像并行）→ `m-contrast`（对照）→ `m-audit`（对抗复核）
> 4 个 teammate 全部 `readonly`（评审类角色给只读权责，裁决从「观点」变「证据」）。
>
> **证据边界（先说清，否则后面全是空话）**
> - 本仓**没有** LoopX 源码：`**/*loopx*` 只匹配到那份调研文档本身。LoopX 侧的每一条都是**二手引用**，
>   `loopx/*.py:行` **不可在本仓核对**；「200+ 小时轨迹」等为公开报道，**未独立验证**。
> - LoopX 侧唯一来源：`docs/2026-08-07-loopx-context-research/research-loopx-context-management.md`（下称 **LX**）、
>   `docs/product/pmstory.md` 决策 35。
> - **附带发现**：那份调研的「三、Seelex 现状」一节**已过期**——它引用的 `application/core/context_controller.go`
>   已被 `bf750b8` 容器化重构删除，「无 headless/-p 模式」也不再有事实依据。所以本文的 Seelex 侧
>   **一律以当前 HEAD 代码为准**，不复用那份文档的「现状」。

---

## 1. teamwork 整体评价

### 1.1 它是什么

一句话：**把「顺序事实」写进计划头、把「工人」跑在自己的会话与 worktree 里的后台作业编排面**。
leader 只做三件事——写契约、派活、验收；顺序不靠「记得先叫谁」，靠 `milestones[].depends_on`
（屏障）与 `work_items[].depends_on`（里程碑内 DAG）。它不是一个 agent 的「思考增强」，而是
**把一份工作拆成互不污染的执行单元、并让 leader 保持在线**的机制。

### 1.2 它相对「一个 agent 自己干」多出了什么真东西

| 真东西 | 证据 |
|---|---|
| **并行是真的** | 本会话第 1 轮 5 项勘察：墙钟 89s vs 逐项耗时和 368s = **4.1×** |
| **但也真的有不划算的时候** | 第 2 轮 4 项是链式依赖：953s 墙钟 / 919s 串行和 = **0.96×**（几乎白付编排开销） |
| **独立上下文才有的反证** | verifier 抓出 1 条 Contradiction 纠 leader 题面；reviewer 找出实现者漏掉的收口缺口；本轮又抓出「隔离是有条件的」「README 漂移应升档」——**同一上下文里的自我复核抓不到这些** |
| **权责可授权** | `tools_policy` 是一等字段（`sessionstore/teamwork.go:111`）：`readonly` 能跑测试不能改码，赋权即免审批 |
| **事实外置** | append-only 审计（`sessionstore/teamwork.go:316-340`）：会话被压缩后仍能逐行读回「谁何时拒收」 |
| **失败带现场** | failed 留 worktree + 记忆，重派复用同一会话号（`seelebridge/teamwork/items.go:57`） |

### 1.3 取舍（每条都标代价）

| 来源 | 代价 |
|---|---|
| 顺序只从计划结构来（`sessionstore/teamwork.go:135-145`） | 计划必须先写全；重规划 = 重写计划 |
| 隔离 = 一事一会话 + 一事一 worktree（`items.go:57,62`） | worktree 数被人数封顶（`max_teammates` 默认 6，`config/seelex.yaml:167`），teammate 面**硬移除** `fork_subagents` |
| 隔离边界是**工作区**不是文件 | 两项同改一批文件只在合并时冲突 |
| 权责档取代审批流 | readonly 评审无法自建「修复前副本」做阴性对照 |
| 尾插自动、leader 不轮询（`items.go:438`） | leader 只拿到有界回执（≈200 B/项），正文要另花一次 `team_context` |
| 全程显式拒绝（`coordinator.go:507`、`items.go:318`、`:547`） | 每次拒绝都是 leader 同步返工 |

### 1.4 最脆的地方（按严重度；标明实测 / 读码）

1. **「在不在跑」是内存投影**（`handleAlive` → `jobs.Observe`）——`accept` / `recover` / `close` 三个闸门共用它。
   **实测**：上一轮 a2/a4 已 done，`recover` 仍报 `running=2 / 可重派=0`，`accept` 被拒到必须退役句柄。
2. **计划头读-改-写跨三步无锁** → 并发 settle 丢失更新。**实测修复前 `-count=3` → 3/3 失败**；
   已修（`planlock.go` + `SettleWorkItem` 两段式，提交 `9ef07f1`），但**只护进程内**。
3. **收口后没有 `closed` 闸**：`plan.State.State` 只在读路径与 `Close` 内被读，`DispatchItem` 不读
   （`items.go:297` 只过 `milestoneOpen`）→ `team_close` 之后仍能把 `running` 写回一份 closed 计划。**复核复现（读码）**。
4. **「一工作项一 worktree」是设计口径，不是运行保证**（本轮新发现）：`BindWorkspace` 在无 git 面/建失败时
   **显式降级**为该轮落在主工作区（`seelebridge/runtime_teamwork_items.go:45-63`），而载荷与账本照旧写
   `seelex/<role>-<item>`（`items.go:62,347-360`）→ **看板与 JSONL 看不出隔离是否存在**。
5. **锁内仍有慢操作**：`BindWorkspace` 跑 `git worktree add`（`items.go:356`）、`Close` 锁内做 `Reclaim`（5s 级有界停顿）。
6. **`-race` 对跨调用原子性不敏感**（实测：修复前的副本 `-race` 也不报 DATA RACE）。
7. **拒收不落审计** → 「闸门生效过没有」只能反推。
8. **文档漂移**：`README.md:60` 的 worktree 前缀与代码不符——照 README 实现会**现场分裂**
   （`workItemNodeID` 靠 `TrimPrefix("seelex/")` 折回），**严重度应由「低」升到「中」**；
   `docs/arch/team-board-gui-tui-contract.md:100` 仍以**已整条退场**的 `Stages[]` 为顺序事实。
9. **空里程碑屏障两处口径不一致**：`items.go:812-825`（`milestoneOpen` 对空依赖直接跳过判定）vs
   `coordinator.go:229-232`（`Milestone` 要求依赖全 done）——**读码推定**。

### 1.5 成本（为拿这些好处要多付什么）

- **编排动作**：审计流 67 行/3 轮，第 1 轮 23 次落审计调用 / 6 项（3.8/项），第 2 轮 19 / 4（4.75/项）——
  这只是**下界**，不含 `team_context` / `jobs_manage` / 自己读码。
- **契约撰写**：leader 手写每项 goal（本轮 ≈1.6 KB + 1.4 KB，落在 `metadata/teamwork.json`）。
- **墙钟**：见 1.2——4.1× 与 0.96× 都是真实的。
- **token**：teammate 输出字节**读不到**（收口会清作业输出）；leader 侧落盘 593 KB message + 6.7 MB tool result
  （含单条 5.9 MB grep）——**贵在工具结果面，不在 teamwork 本身**。
- **认知负担**：计划一致性 + 失败的人工处置（解冲突 / 变基 / 合并）。

### 1.6 一句话：什么时候不该用它

**子任务沿链互相依赖、或要改同一批文件、或没有独立可复核的产出**——那时只多出编排开销（实测 0.96× 收益、
每项 4~5 次编排调用），单代理 + 一个 Plan 更划算。

---

## 2. LoopX 是什么（二手画像，逐条带出处）

一句话：字节跳动工程师黄瑞腾开源的 **"Loop Engineering" 本地控制面**（MIT、Python 3.11+、零运行时依赖），
面向**数天到数百小时的长任务**：把全部控制状态（目标 / gate / todo / claim / 证据 / quota / handoff）外置成
「模型上下文之外、事件溯源、只读投影」的状态内核，让每轮 agent 退化成可替换的**有界执行 worker**（LX §一）。

| 机制 | 要点 | 出处 |
|---|---|---|
| 状态内核 | append-only JSONL `events.jsonl` 是唯一真相；md 是投影；dashboard 只读 + capability 握手 | LX §2.1 |
| 注入 | 分级投影**决策载荷**，模型不读事件流本体；每层限长 | LX §2.1 |
| mutation authority | agent **不能写控制自身的状态**；claims 需显式授权 | LX §2.1 |
| 有界 turn 事务 | 相位固定，commit 策略硬编码；**副作用由控制面独占**，host 只产 typed result | LX §2.2 |
| 独立验证 | trusted **argv-only** validator，**不用 LLM 当裁判**，verdict 四值 + `recovery_kind` | LX §2.2 |
| 幂等 | `turn_key`（lineage + 执行上下文 hash），resume journal 按相位续跑 | LX §2.2 |
| quota | 只对**已验证的状态转移**记账；monitor/a​ck/refresh 一律 no-spend | LX §2.3 |
| 安静 | monitor-only 未到期 → `monitor_quiet_skip` / `DONT_NOTIFY` | LX §2.3/§2.8 |
| gate | 一等对象：具体问题 + 阻塞路线 + safe default + 可推进的旁路；「不能把旁路有进展写成 gate 已解决」 | LX §2.4 |
| typed todo | 40+ 字段；**完成必须有 successor 或显式 no_followup**，否则 `TODO_SUCCESSION_GAP` | LX §2.5 |
| goal frontier | 9 条规则的**机器归约**决定「该做什么」；目标完成是机器判定 | LX §2.5/§2.6 |
| heartbeat | 19 步无人值守循环 + 退避 + `reset_token` + 3 次 unchanged 才停 | LX §2.8 |

**它自己暴露的边界**：3 起事故的共同模式是「**下一个迁移归谁拥有**边界模糊 + 机器投影与可执行真相脱节」，
修复一律固化成机器可读的 typed contract（最有名的一条：`repair/replan` 只有**当下一个 agent 能看到不同的、
可执行的 control-plane 状态**才算完成，`delta_present=true` 才 ACK）；作者自述「**不是生产自动化控制器**」，
危险权限 / 生产写入 / 最终 ownership 仍归人类（LX §2.7/§七）。

---

## 3. 对照

### 3.1 先划边界：哪里可比

- **不可比（"谁调用谁"层）**：工具/API 形状；部署形态（teamwork 是同进程端口装配，LoopX 是 CLI 驱动外部 host）；
  **隔离单元 / 并行上限 / 独立评审角色**——LX 文档**零覆盖**（其 prompt 反而明写 effects 归 adapter）。
  这三条的正确口径是「**不可比 / 无法判断**」，**不是「LoopX 弱」**。
  （reviewer 另点出：LX 的 `workspace_guard` / `required_write_scopes` 是**声明式写入范围**，
  与 teamwork 的文件系统隔离不是一回事，硬比就是偷换概念。）

### 3.2 可比维度上的优劣

| 维度 | teamwork | LoopX | 谁占优 / 代价 |
|---|---|---|---|
| 真相源 | 可变计划头 + 事件/绑定做审计 | 事件流唯一真相 + 投影 | **LX**；代价＝投影链 + backfill |
| 顺序表达 | 声明式屏障 + item DAG，校验无环，**派发时拒绝** | typed todo + 前沿归约 9 规则，是给 host 的**建议** | **teamwork 在"拦"上占优**（结构性可拒）；**LX 在"算"上占优**（机器可判定下一步）——两者互补而非同一维度 |
| 隔离 | 一事一会话 + 一事一 worktree（**有条件的**，见 §1.4-4） | 文档未覆盖 | **不可比** |
| 验证 | `AcceptItem` 闸 + 尾插 review/failed；**无机器 validator** | argv-only validator + verdict/recovery_kind | **LX**；代价＝host 契约 |
| 人机接口 | 审批 + 显式拒绝 | gate 一等对象 + quiet/`DONT_NOTIFY` | **LX** |
| 无人值守 | 无：leader 不叫就没有下一步 | heartbeat + 退避 + reset_token | **LX，结构性** |
| 失败恢复 | 类型化 item 态 + `FailItem` 留现场；`Recover` 靠**内存** `handleAlive` | 相位绑失败 + `turn_key` 幂等续跑 | **LX**；且 teamwork 该项已**实测高危** |
| 成本 | 换并行墙钟 + 每项一 worktree；保护的是 **leader 的上下文** | 只对已验证交付计费；保护的是 **用户的注意力** | **各有所护**，不是同一个"省" |
| 依赖与可移植 | 绑 seelex 会话/jobs/权限装配 | 零依赖、host 可换 | **LX**；代价＝要有 wire 契约（TurnEnvelope 签名） |
| 审计 | 事件流水，但**拒收不落审计** | 不可篡改 + 指纹冲突检测 | **LX** |

### 3.3 三条结论

**① teamwork 的独有优势：闸门在调用边界、隔离在工作区——「拒绝」是结构事实，不是事后校验。**
顺序与超员在**派发那一刻**被同步拒绝（`items.go:318`、`coordinator.go:507`），一个 work item 一套
Session + worktree，名额硬绑 `max_teammates`，teammate 面硬移除 `fork_subagents`。LoopX 把副作用交给外部
host，**只能在事后校验**（LX:38），做不到「这块盘现在能不能并发写」这种拒绝式裁决。
（诚实标注：worktree / 并行上限 / 独立评审三条在 LX 文档里**没有对应物**，「它结构上不可能有」是**结构论证 = Hypothesis**，
不是已证事实。）

**② LoopX 的独有优势：时间维度的自主性 + 注意力记账。**
它自己决定「下一步该不该跑」（quota 状态机 + 只对已验证的转移记账 + 安静是一等公民 + 3 次 unchanged 才停）；
teamwork 没人问就没有下一步——这不是缺一个参数，而是**要新增常驻循环与静默语义**。
以及一条更锋利的：LoopX 把「该做什么」**降维成机器可计算的归约**（frontier 9 规则 + successor 缺口检测 +
完成机器判定），teamwork 侧对应能力搜索（`Frontier|Eligible|ReadyItems`）**为空**。

**③ teamwork 最该从 LoopX 拿走的一条——两票不同，leader 裁决：先做「计划前沿的单次只读归约」。**

| 提案 | 内容 | 我的裁决 |
|---|---|---|
| comparator | 借 **delta 契约**：`TeamworkEvent` 加 `delta/outcome`，六处 `c.audit(...)` 记录每次裁决（**含拒绝**）的 delta | **采纳，作为配套**：它正好补上「拒收不落审计」这条口径缺口 |
| reviewer | 把「能不能派 / 该派谁 / 还差什么」收敛成**一次只读的落盘归约（plan frontier）**，让派发闸门只走它 | **第一优先**：它一次吃掉 closed 闸缺口、`ItemView.Interrupted`（`items.go:33-38` 已算却无出口的信号）、依赖/屏障两处判定漂移面，且**纯读、不动存储写接口** |

两条其实能合成一条实施路径：**先有 frontier 归约（算），再让每次裁决把「这次推进改变了什么」记成 delta（证）**。
这恰好也是 LoopX 三起事故教出来的同一个教训：**「重规划/推进只有当下一个 agent 能看到不同的、可执行的
状态才算完成」**——而 teamwork 本轮实测的形态与 `monitor-only-replan-stall` 同源（宣称推进、前沿未变）。

---

## 4. 复核的同意 / 不同意 / 无法判断

**同意**：LoopX「机器可判定的下一步」确是 teamwork 的真实缺口（搜索为空可复核）。
**不同意**：把 `README` 里的 worktree 名当事实（应升档为中，见 §1.4-8）；把「文档说它有」当运行事实。
**无法判断**（如实留白，不凑）：LoopX 任何机制的真实现状（无源码）、`jobs.Reclaim` 的 5s 停顿细节（外部模块）、
跨进程同 Key 的行为。

**文档内部摩擦 3 处（读 LX 时发现，供后续修订）**：§2.1「11 种事件类型」后接省略号；
§2.7 的「6 次无变化 → dead_monitor_repeat」与 §2.8 的「3 次 unchanged 才允许停」两个数未对齐；
「TurnEnvelope ≤8KB」未说清是强制上限还是设计预算。

---

## 5. 证据索引

| 段 | 来源 | 落点 |
|---|---|---|
| teamwork 整体评价（含 4.1×/0.96× 与成本量化） | assessor · `wi-teamwork-profile` | 本会话审计流 `…/session-f7dbdf5ba1a0295f/teamwork/events.jsonl` |
| LoopX 画像（含「文档未覆盖」清单） | loopx_modeler · `wi-loopx-profile` | LX §一~§七 |
| 对照表与三条结论 | comparator · `wi-contrast` | 本仓当前 HEAD 代码 + LX |
| 对抗复核（隔离是有条件的 / README 升档 / LX 调研的 Seelex 侧过期） | reviewer · `wi-audit` | `runtime_teamwork_items.go:45-63`、`items.go:62,347-360`、`README.md:60` |
| 本轮代码修复（已提交） | leader + exec/reviewer(test_case) | commit `9ef07f1`、`3677d15` |
