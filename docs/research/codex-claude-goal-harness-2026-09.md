# Codex / Claude 的 goal：harness 层如何承载（2026-09）

> 调研日期：2026-09-07。方法：web_search 多轮交叉（公开文章/官方更新/第三方源码拆解），辅以仓库既有研究
> （`docs/research/coding-agent-harness-comparison.md`、`docs/research/agent-market-research-2026-08.md`）。
> 提问语境：Seelex 的 goal 是"提示词技能 + plan/task 工具"，用户疑问——Codex 和 Claude 的 goal 机制
> 是否也只是提示词工程。结论：**不是**。两家的 goal 均已下沉为 harness 子系统（命令级入口 + 目标状态机 +
> 自评停机循环 + 预算门禁 + 上下文延续 + UI 投影）。提示词只承担"目标负载的写法"，机制在代码里。
>
> 证据分级说明：本文事实按可靠度分三级——**【一手/多源】**（版本号、命令名、产品行为，多个独立来源一致）；
> **【源码拆解-二手】**（第三方对 Claude Code 反编译/社区对开源 Codex 的分析，未逐一核对源码行）；**【推断】**
> （按机制必要性推断，标"推断"字样）。无法直接抓取官方原文全文，厂商文档细节以搜索摘要转述为准。

---

## 0. 一句话结论

| | Codex（OpenAI，Rust 开源 CLI） | Claude Code（Anthropic，闭源 Node/TS） |
|---|---|---|
| goal 入口 | `/goal` 命令（CLI v0.128.0，2026-04-30）+ 桌面版 Goal Mode「追求目标」 | `/goal` 命令（v2.1.139，2026-05-11/12，正式版）+ 同期上线的 **Agent View** |
| 核心语义 | 持久目标循环：设目标（+预算），自动判定完成，未完成继续下一轮直到达成 | 设定完成条件后自动循环执行、自行判定"做完了"再停，不再干一轮就停等用户催 |
| 载体 | "一整套目标生命周期"（第三方称非提示词模板） | 目标作为一等任务状态对象，Agent View 可视化并行目标/代理 |
| 背后 harness | goal+plan 双模式、多 Agent 并行/后台、长任务正式化；2026-08 另测试"持久模式" | compaction 服务、CLAUDE.md 分层加载、subagent 独立上下文窗口、plan mode 权限态 |

---

## 1. 先对齐：两家到底何时、以什么形态"上了 goal"

- **OpenAI Codex CLI v0.128.0（2026-04-30）** 新增 `/goal`：【一手/多源】"用户输入 `/goal "目标描述"` 后，
  Codex 会自动判断目标是否完成，未完成则继续下一轮，直到目标达成"（"持久目标循环"，设目标和预算）。
  第三方评测明确写：**"它不是又一个普通的提示词模板，而是 Codex 内部新增了一整套目标生命周期"**（掘金
  7636334176748814377，二手但指向性强）。
- **Claude Code v2.1.139（2026-05-11/12）** 跟进的 `/goal` 是正式版（非实验态）【多源】，
  与 **Agent View** 同版本发布【多源】。行为：给一个目标 + 完成条件，Claude 自己循环干，干完自己停
  （回应"大任务干完一轮就停、用户反复手动说继续"的痛点）。
- **时间线巧合**：Codex 2026-04-30 上 /goal，Claude 2026-05-11/12 上 /goal——相隔仅 11 天级别，
  社区有文章直接以"同一个 /goal 功能，两种 Agent 性格"做对比（掘金 7641004611159785499）。
- 后续演化：Codex Goal Mode 转正并纳入"软件工程平台"能力集（Goal Mode、多 Agent 并行执行、长任务调度、
  Auto Review、浏览器验证等，php.cn 2796387 / 腾讯新闻 20260522）；2026-08-27《连线》报道 OpenAI 测试
  Codex"持久模式"——不被休眠、长时间持续主动执行（凤凰/IT之家 20260828，属延伸家族，非 /goal 本身）。

---

## 2. 这些"目标机制"落在 harness 的哪几层

把两家 /goal 拆开，能看到它们共享同一张 **harness 子系统清单**。goal 文本只是负载，每个子系统都是代码：

### 2.1 命令入口与目标对象（state machine / 生命周期）
- **Codex**：【一手/多源】`/goal` 是 CLI 命令（slash command），命令被解析后进入"目标生命周期"
  （创建/运行/完成/预算耗尽等状态的受管对象）；"设目标和预算"说明 goal 对象携带**预算字段**；
  桌面版以「追求目标 / 目标」菜单进入【一手/多源】。
- **Claude Code**：【一手/多源】`/goal` 与 **Agent View** 捆绑上线——Agent View 被解读为"任务管理器 +
  批量执行器"视图（博客园 20322381 二手），即**每个 goal/agent 是可枚举、可观察的一等状态**，而非一段对话尾巴。
- **推断**：一个"干几天不停"的目标必然要有**持久化状态 + 恢复语义**（崩溃/关应用/换会话后能续跑）；
  社区"长期任务五层工作系统"把续跑机制列为独立一层（CSDN 162941467，社区总结，指向一致）。

### 2.2 循环控制器（完成谓词 + 自动续轮 + 停机）
- 【一手/多源】两家一致：**不再以"轮次用尽"停机，而以"目标完成自评"停机**——Codex"自动判断目标是否完成，
  未完成则继续下一轮"；Claude"干完了自己知道停"。
- 这是 Seelex 现有 `maxLoops=9999`（无限循环）+ 提示词"直到完成"没有覆盖的语义：Seelex 有**无上限的循环**，
  但没有**goal 级的完成判定与停机协议**（Claude/Codex 的完成判定是一个"每轮收敛检查 + 汇报"的受管行为，
  而非让模型自己记得停）。【推断：完成判定本身依赖模型语义，但"何时检查、检查结果如何进入状态机、如何停止并
  汇报"是 harness 逻辑。】

### 2.3 预算与门禁（guardrail）
- Codex：【多源】goal 可"设目标和预算"（qklw 标题）；社区场景："Token/额度预算即将耗尽"时用 Goal 模式续
  （掘金 7645830893321797659）→ goal 与**额度/预算护栏**绑定，预算耗尽有受管行为。
- Claude Code：既有 Task Budgets / effort 档位体系（本仓库早期调研 harness 3.x 已列），/goal 长循环沿用
  同样的消耗控制面。【二手/推断】
- 说明：预算不是"提示词让模型省着点用"，而是**执行层计数 + 中断/暂停策略**。

### 2.4 上下文延续与压缩底座（goal 长循环不炸窗口的关键）
- Claude Code 的上下文管理是**明确的 harness 代码**（第三方源码拆解，二手但多源一致）：
  - `src/services/compact/autoCompact.ts`（约 352 行，导出 `autoCompactIfNeeded`）、`src/services/compact/compact.ts`
    —— 自动压缩：接近上下文限制时按阈值提前触发、做窗口外摘要、保留收尾缓冲（掘金 7648915999019466761 等）；
    另有"四层压缩流水线 + hooks + 电路断路器 + GrowthBook 配置"拆解（掘金 7655279236191698987）。
  - 记忆文件分层加载：**6 级优先级指令加载策略（从全局策略到本地规则）**（devpress m0_63466673 二手）；
    CLAUDE.md 由 harness 在会话启动时自动读取并**常驻上下文**（博客园 tsingroo 19660112 二手）。
  - subagent：每个 subagent **独立上下文窗口 + 独立工具访问权限**（简书 6bd7a5fcd897 等二手多源）。
  → `/goal` 的长循环能不断片，靠的是这套 compaction/memory/session 服务；goal 机制只是它们的**上层调度**。
- Codex：开源 Rust CLI 本身有会话/上下文构建层（本仓库早期对比 3.x 已列 session resume、worktree 等）；
  Goal 的技术原理分析以 OpenAI Cookbook + 官方文档 + **开源仓库**为据（掘金 7659613324195643442，二手），
  说明目标机制可在其开源代码中定位（本文未能抓取全文，未给具体文件路径，标【二手-未核】）。

### 2.5 可见性/投影（goal 视图）
- Claude Code：Agent View = 目标/代理的可视化、可审计面（【多源】，经管之家/博客园解读）。
- Codex：桌面版 goal 菜单、官方"Goals 激活时会发生哪些变化"指南（腾讯新闻 20260519 转述 OpenAI 官方文）
  → 激活前后有**系统行为差异**（状态、后台、预算等），说明 goal 是运行时可查询对象。
- Seelex 对照：目前只有 `GoalSkillActive` 布尔投影到前端（`dto.RuntimeVisibilityProjection`），
  没有"这个 goal 现在到哪一步、剩多少预算"的目标级投影。

### 2.6 编写指南 = 提示词层（唯一留在 prompt 的部分）
- OpenAI 官方专门发文"如何在 Codex 中使用 Goals"（何时用、激活后变化、**如何编写**，腾讯新闻 20260519 转述）；
  社区产出"可执行 Codex Goal 的六要素模板"（目标/成功标准/禁区/失败停机条件等，掘金 7646007198240440335 二手）。
- 结论：**提示词工程只在"goal 负载怎么写"这一层存在**；目标能不能建、能不能续、花多少、何时停、在哪个
  窗口里跑、做完怎么汇报——全是 harness。

---

## 3. 两家 goal 的差异（社区对比口径）

- 掘金 7641004611159785499（"同一个 /goal，两种 Agent 性格"，二手）：
  - **Codex /goal**：更像"目标即预算化持续任务"——给目标（和预算）后**自主推进、多轮/多会话续跑**，
    官方定位是 Goal Mode + 多 Agent/长任务平台能力；社区反馈 token 消耗显著上升（因为真的一直跑）。
  - **Claude Code /goal**：默认**同步、交互受控**的本地执行风格，/goal 补的是"不用反复按继续"的自主循环；
    配 Agent View 让多个目标/代理并行且可视。
  - 一句话：Codex 把它做成**平台级长任务后台对象**，Claude 把它做成**会话内自主循环 + 可视化任务面板**。
- 双方都在往"功能越来越像"收敛（Elie Bakouch 时间线对比被多篇转载，如头条 7650049575851393587）【多源二手】。

---

## 4. 回答"总不可能是提示词工程吧"——分层实证表

| harness 层 | Codex 证据 | Claude Code 证据 | 是提示词吗 |
|---|---|---|---|
| 命令/入口 | `/goal` CLI v0.128.0；桌面 Goal Mode | `/goal` v2.1.139 + Agent View | 否（代码命令解析→对象创建） |
| 状态机/生命周期 | "一整套目标生命周期"；目标+预算 | 目标是一等状态，Agent View 枚举 | 否 |
| 循环/停机 | 自动判完成、未完成续下一轮 | 自己循环、做完自停 | 完成判定是语义，循环与停机协议否 |
| 预算门禁 | 目标携带预算；额度护栏场景 | 沿用 Task Budgets/effort | 否（执行层计数/中断） |
| 上下文延续 | 会话/后台多 Agent；开源可定位 | compact.ts/autoCompact.ts；CLAUDE.md 6 级加载；subagent 独立窗口 | 否（TS/Rust harness 服务） |
| 可见性 | Goal Mode 正式能力、goals 指南 | Agent View 面板 | 否（UI/投影） |
| 目标写法 | 官方"如何编写 Goals"；六要素模板 | /goal 完成条件写法指南 | **是（仅此层）** |

---

## 5. 对 Seelex 的意义（gap 与最小落地路径）

Seelex 当前：goal = 提示词技能（GOAL 方法论 + 逃逸描述）+ `plan_load/plan_run`（DAG 工具面）+ `task_*` 终态。
对比上表，缺的是**把 goal 建成一等 harness 对象**：

1. **goal 注册与状态机**：新增 goal 级状态（active/paused/completed/预算耗尽）并持久化——可复用
   `application/core/task_context` 的 task 生命周期与 `sessionstore` 原子持久化，把 skill 激活升级为
   "注册 goal 对象 + 投影到 Runtime/前端"（现有 `GoalSkillActive` 布尔投影扩展为目标级 DTO）。
2. **完成谓词 + 停机协议**：goal 循环不应只靠 `SetMaxLoops(9999)`（input.go 现做法）+ 提示词自停；
   应加 goal 级收敛检查与停机上报（对照两家自评停机；可挂到 `task_complete`/`task_needs_user_decision` 语义上）。
3. **goal 预算护栏**：复用 `Limits()`（seele.yaml limits），给 goal 配 token/轮次护栏 + 耗尽时受管暂停/汇报。
4. **目标级续跑**：goal 中断后可续——`ContextSnapshot.PendingWork`（已存在）+ plan `ResumePlan/checkpoint`
   （已存在）串成"goal 恢复路径"。
5. **提示词层收敛**：goal SKILL.md 保留方法论，但把"完成条件/成功标准/停机条件/预算"结构化字段化，作 goal
   对象输入 schema，而非散在正文里靠模型自觉遵守。

一句话：两家证明"goal"的护城河在 harness——状态、循环、预算、上下文、视图；
Seelex 要追上，缺的不是更多提示词，而是把 goal 从"技能正文"变成"任务对象 + 状态机 + 停机协议 + 预算 + 投影"。

---

## 6. 来源清单

- 一手/多源事实：Codex v0.128.0 /goal（区块链网 qklw 20260503 转译更新说明；掘金 7636334176748814377）；
  Claude v2.1.139 /goal + Agent View（经管之家 16617438、博客园 20322381、网易 m.163.com KT1B4Q5T05561FZY、
  掘金 7638983028574437419）；Codex Goal Mode 正式化与平台化（php.cn 2796387、腾讯新闻 20260522A0B2Z500、
  腾讯新闻 20260417A02OKI00）；"在 Codex 中使用 Goals"官方文转述（腾讯新闻 20260519A03L2900）；
  持久模式（凤凰网 8vy9NQyuP1c / IT之家 995818，2026-08-28）。
- 二手源码拆解：Claude Code compaction（掘金 7648915999019466761：compact.ts/autoCompact.ts；
  掘金 7655279236191698987：四层压缩流水线；CSDN fzuim 159711970：三层自愈记忆）；6 级指令加载
  （devpress m0_63466673）；CLAUDE.md 常驻（博客园 tsingroo 19660112）；subagent 独立窗口
  （简书 6bd7a5fcd897）；Claude 整体 deny-first + 五层压缩 + 子代理（CSDN universsky 162838463）。
- 二手对比：掘金 7641004611159785499（同一 /goal 两种性格）；头条 7650049575851393587（功能时间线趋同）；
  社区方法论 CSDN 162941467（长期任务五层工作系统）。
- Codex 内部技术原理：掘金 7659613324195643442（基于 Cookbook+官方文档+开源仓库，本文未核全文，标未核）；
  掘金 7646007198240440335（Goals 定位）；CSDN Enya61 161384293（Plan Mode 与 Goal Mode）。
- 仓库既有：`docs/research/coding-agent-harness-comparison.md`；`docs/research/agent-market-research-2026-08.md`。
