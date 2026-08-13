# Seelex PM Story：从"Seele 的薄壳"到"Coding Agent Harness"

> 一份基于历史文档的决策叙事（2025-07 → 2026-08）
> 视角：产品经理。目标：讲清楚"每个地方为什么这样做"——当时的选项、权衡、代价与后来的复盘。
> 素材：`docs/devlog/`、`docs/arch/`、`docs/YYYY-MM-DD-topic/` 工作包、`docs/research/`、`docs/product/`。
> 约定：每条决策末尾标注主要出处；与代码冲突时以代码为准（见 `docs/devlog/README.md`）。

---

## 序章：这个项目在讲一个什么故事

2026 年 AI 编程的竞争已经从"谁的模型强"转向"谁的 Harness 好"——模型之外的运行时骨架：上下文怎么治理、多 agent 怎么编排、权限怎么审批、数据怎么持久化（`docs/blog/seelex-technical-blog-2026.md`）。

Seelex 的故事，是一个开发者用 **201 个 commit** 从零回答这四个问题、并把"模型调用"做成"可信执行产品"的决策史。它不是一个"又一个 coding agent"，而是一个**可组合、可审计、可自托管的 Coding Agent Harness**：本地优先、多模型可路由、上下文可逆治理（`docs/product/roadmap-2026.md` §1）。

下面按时间线讲八个阶段的决策。每个阶段都有一个"当时的处境 → 我们选了什么 → 为什么 → 付出了什么"。

---

## 第一章 起点：先证明端到端能跑（2025-07 ~ 2026-07-17）

**处境**：Seelex 最初定位是 Seele Agent Runtime 的一个薄 TUI 客户端。任务是先打通端到端，而不是设计一个完美的框架。

**决策 1 · 用"8 层装配"快速穿线，不为抽象提前付费**
- 决策：main 以 8 层装配串联 配置→Agent→Engine→Session→Skill→TUI，Model 直接持有具体类型。
- 为什么：验证端到端可用优先；薄客户端阶段引入接口是过度设计。
- 代价：main 过长、Model 依赖具体类型（ARC-014）、全局状态蔓延——**直接催生了后续三次重构**。
- 出处：`docs/arch/architecture-and-flaws.md`

**决策 2 · 依赖倒置：Model 接口化**
- 决策：为 Engine/ChatClient/Agent/Session/Skill 引入接口，用安全断言替代硬类型断言。
- 为什么：具体类型导致无法单测、UI 与框架强耦合；接口化后 mock 注入可行。
- 出处：`docs/arch/architecture-and-flaws.md`

**决策 3 · Context Provider 化 + TraceProvider + 双向合并**
- 决策：单一 Export/Import 重构为 Provider 接口（参考 Claude Code Context Provider），保持旧签名向后兼容；新增从 `tracer.Tree` 自动提取 Decisions/Findings 的 TraceProvider；新增 Compactor（token 预算三级压缩）与 MergeBack（子代理结果回写父代理）。
- 为什么：A2A 场景需要"父→子承袭、子→父回写"闭环；多来源（Engine/Trace/Store）可插拔可测试。
- 代价：压缩不可逆，需要 Validate 与预算接口兜底；Provider 限定 ≤4 方法防过度设计。
- 出处：`docs/arch/context-improvement-plan.md`

**转折点 · 2026-07-17：TUI / Application Core 分离 + Runtime/Plugin 重构**
- 决策：把产品语义（Chat/Task/Approval/Session/Workspace）收进独立的 Application Core，前端（TUI）只消费 Snapshot/Event；运行时能力经 seelebridge 隔离。
- 为什么：**这是"产品"第一次从"脚本"里诞生**——核心可单测、前端只是投影、GUI 有了同一套内核。
- 出处：`docs/devlog/2026-07-17-tui-application-core-separation-plan.md`、`2026-07-17-seelex-runtime-plugin-refactor-front-review.md`

> **PM 视角**：第一章的主题是"先跑通，再拆对"。薄壳阶段积累的债（main 过长、全局状态）不是失败，而是**重构的理由与时机清单**。产品层从第一天就按"核心可测试、前端可替换"来长。

---

## 第二章 产品形态成形：扩展体系与 Agent Workbench（2026-07-19 ~ 07-24）

**处境**：核心能跑之后，产品要回答"能力怎么扩展、长任务怎么被看见"。

**决策 4 · Skill 按插件分区、自包含**
- 决策：Skill 从全局 `skills/` 迁入 `plugins/<name>/<skill>/`，SKILL.md 与脚本同目录；`switch_plugin` 同时切换系统提示词与可见 Skill 列表。
- 为什么：解决"提示词与脚本割裂、Skill 不归插件管"；归属与可见性收敛。
- 代价：破坏性变更——全局 skills/ 删除、脚本路径全迁移。
- 出处：`docs/arch/plugin-skill-restructure.md`

**决策 5 · Effort 贯穿全栈，驱动模型选择与规划深度**
- 决策：四级 Effort 从配置、Provider 传输层贯通到应用层，空值回退 Provider 默认。
- 为什么：统一思考深度控制、按难度自动降模型成本。
- 代价：历史设计稿中的示例数值未落地（最终 profile 为 lite/medium/high/max）。
- 出处：`docs/arch/effort-system-design.md`

**决策 6 · Skill 指令走 user message，不进 system prompt**
- 决策：Skill 以版本化可逆 envelope（`seelex:skill-context:v1`）随 user message 传输；UI/Snapshot 只展示用户原文。
- 为什么：Skill 属用户侧行为，避免污染全局 system prompt、随注册表增长。
- 代价：指令每轮重复发送增加 token；envelope 仅为展示还原，不构成安全边界。
- 出处：`docs/arch/skill-effort-architecture.md`

**决策 7 · Agent Workbench：DSL 卡片 = Conversation 一等 item**
- 决策：方案 B——Agent 经 `render_card` 提交 JSON，Core 校验后生成 `ConversationItem(kind=card)` 进对话区；**否决**前端解析 Markdown 与引入 React/A2UI。
- 为什么：显式结构化入口、Core+前端双校验、持久化清晰、可回滚 ErrorCard。
- 代价：协议需显式升级 v2（card/workspace/scope 事件）。
- 出处：`docs/arch/agent-workbench-architecture.md`

**决策 8 · Plan 可视化：按 Effort 复合展示 + 钩子捕获 Plan 事件**
- 决策：Plan 面板按 Effort 选型（lite 轮播 / medium 打点 / high 树 / max 表格）；事件经 ToolHookBridge 拦截 plan_run 结果解析（方案 A），不修改框架 PlanRunner（方案 B）。
- 为什么：粒度与 effort 匹配、窄终端友好、零框架侵入。
- 代价：拿不到 Fork 子 Agent 中间流式状态，需自定时刷新兜底。
- 出处：`docs/arch/plan-visualization-design.md`

**决策 9 · 子代理可见性：先做数据结构，不做 UI**
- 决策：复用 WorkPlan DAG + TraceTree + EventHub，新增 SubAgentTree 数据与事件进 Snapshot；TUI 树渲染留 P2/P3。
- 为什么：最小起步、不涉 UI。
- 代价：当前仅数据结构，无用户可见能力。
- 出处：`docs/arch/subagent-visibility-design.md`

> **PM 视角**：这一章产品在"能力"与"可解释性"之间反复找平衡——每一项扩展都要求**可回滚、可审计、不污染核心**。Skill 不进 system prompt、卡片进对话不进 Workspace、事件走钩子不动框架，都是同一个原则：**产品语义与运行时解耦，能力可以被看见**。

---

## 第三章 工程质量爬坡：GUI、CI、E2E、作用域（2026-07-23 ~ 07-28）

**处境**：功能多了，产品开始"能跑但不可信"。这一章几乎全是在补"可验证"。

**决策 10 · GUI 状态同步：事件 reducer + Snapshot 兜底**
- 决策：正常路径 delta 增量归并；异常（seq 缺口/未知事件）统一重同步。
- 为什么：不反复调 Bridge、长会话性能好、reducer 可纯函数测试。
- 出处：`docs/2026-07-23-gui-client-stability/plan.md`

**决策 11 · 会话渲染：keyed Reconciliation，不全量 innerHTML 重建**
- 决策：用后端实体 ID 作 DOM key 复用未变化节点。
- 为什么：保持滚动与思考块/工具 OUT 展开状态。
- 代价：前端复杂度上升。
- 出处：`docs/2026-07-23-gui-client-stability/plan.md`

**决策 12 · 文档 CI 与 schema-first 黄金旅程 E2E**
- 决策：独立 `gui-tests` job（不并入三平台 matrix）；E2E 用严格 scenario v1 loader + scripted Engine 驱动真实 Core，fixture 纳入文档契约校验。
- 为什么：门禁独立可定位、fail-fast 拒绝未知字段/未实现路径。
- 出处：`docs/2026-07-23-gui-docs-ci/plan.md`、`docs/2026-07-24-golden-journey-minimal/code-changes.md`

**决策 13 · 会话窗口虚拟滚动：DOM 从 ~5000 降到 <800**
- 决策：固定窗口虚拟滚动（windowSize 50 + overhang 10，<15 条全渲染）+ HTML 永久缓存。
- 为什么：500 条消息的 DOM 开销是真实卡顿源；Markdown 只解析一次、流式追加 O(1)。
- 出处：`docs/2026-07-26-gui-conversation-windowing/plan.md`

**决策 14 · Workplan 并行：深拷贝隔离 + 条件性通过**
- 决策：fork 并行统一为 ParentSnapshot 深拷贝 + BranchContext 分支隔离 + WaitGroup 收敛 + semaphore 限流 + Join 唯一写回；默认 fail-fast。
- 为什么：当时两条并行路径共享同一上下文，互踩是真实风险。
- **转折**：审查结论——线性/manual Plan 有条件通过，**并行 WorkPlan 与完整 A2A 不过生产可靠性审查**（缺上下文隔离、fork 错误不传播、只支持单层汇合）。这个"不通过"直接催生了第八章的 fork 重做。
- 出处：`docs/2026-07-27-seele-workplan-parallel/plan.md`、`docs/2026-07-27-workplan-e2e-a2a-review/finish-review.md`

**决策 15 · Plan JSON DSL：单一状态源，组件化不引框架**
- 决策：Plan JSON 作单一状态源，plan-dsl.js 每次从 JSON 派生 DSL；前端组件化用原生 ES module，不引 React/Vue。
- 为什么：点/边/进度同源；无依赖即可达目标结构。
- 代价：app.js 已成 ~600 行 God Module（当时已承认）。
- 出处：`docs/2026-07-28-plan-json-dsl/finish-review.md`

**决策 16 · 项目/会话作用域：fail-closed ProjectScope**
- 决策：文件工具与 shell 采用 fail-closed——未绑定项目拒绝、路径解析到项目根并拒 `..`/绝对越界/符号链接逃逸、bash 强制 workdir 在项目内；创建/恢复/解绑统一事务化。
- 为什么：防越界与隐式授权；"项目=授权边界"是产品安全的第一条线。
- 代价：shell 工具权限被收紧，部分合法用法需显式授权。
- 出处：`docs/2026-07-28-project-session-scope/code-changes.md`

**决策 17 · CommandSandbox 端口：先定义边界，再谈实现**
- 决策：引入可替换 `CommandSandbox` 端口，优先 isobox 原型（macOS Seatbelt / Linux gVisor / Windows AppContainer）；能力不可实施时 fail-fast、不回退普通 exec.Command。
- 为什么：把"沙箱"从口号变成可替换的端口，不把安全寄托在单一实现。
- 代价：**后续接入疑似导致工具挂起，先回滚恢复可用性**（见第六章决策 24）——端口保留，实现延后。
- 出处：`docs/2026-07-28-project-session-scope/sandbox-research.md`

**决策 18 · 存储可插拔原子 Repository**
- 决策：会话持久化改为可插拔原子 Repository——JSON 用 generation+manifest 原子替换，SQLite/PostgreSQL 单事务 upsert，GUI 可配置切换。
- 为什么：旧 JSON 覆写无事务易混读。
- 出处：`docs/2026-07-28-session-storage-settings/code-changes.md`

> **PM 视角**：这一章是"工程信任"的爬坡——**每一次"能跑"之后立刻补"可验证"**：CI 独立门禁、E2E schema 化、DOM 性能有数字、作用域 fail-closed、存储原子化。特别值得注意的是决策 14 的"不通过"审查：产品允许自己说"这里还不够好"，这是后续质量的制度基础。

---

## 第四章 长任务的"宪法"：终态、checkpoint、replan（2026-07-29 ~ 07-30）

**处境**：长任务反复读测无收敛、工具轮数熔断保不住压缩状态。产品需要给"任务"下定义。

**决策 19 · 强制终态协议：不再信任模型"自然停止"**
- 决策：引入 `task_complete` / `task_failed` 内置终态工具，确定性 Judge 主判。
- 为什么：模型自然停止不可靠；长任务必须有可验收的结束。
- 代价：LLM Judge 首版不引入（防第二个无界 ReAct），留单次无工具解释性判定。
- 出处：`docs/2026-07-29-planact-context-control/plan.md`

**决策 20 · Checkpoint 只存结构化证据，绝不用 wall-clock timeout**
- 决策：不保存思维链与原始大日志；ContextController 按 token/输出阈值压缩；大结果经 `task_context_get` 按需取。
- 为什么：保证长编码任务不被时间截断、压缩后仍留目标/权威 Plan/未完成依赖。
- 出处：`docs/2026-07-29-planact-context-control/plan.md`

**决策 21 · 语义进展取代结果 hash 进展**
- 决策：只有状态转换/新 evidence/已验证测试/文件变更/交付/用户决策推进 epoch；重复只读调用累计无进展，达上限仅允许一次无工具终态回合。
- 为什么：相同工具输出排序变化不算新进展，防"刷进展"假活。
- 出处：`docs/2026-07-29-planact-context-control/plan.md`

**决策 22 · Replan：只由用户显式触发，且全程有界**
- 决策：replan 不调 `plan_clear`（plan_load 原子替换避免无计划空窗）；请求仅带目标/最近 plan JSON/失败描述/节点证据（≤12KiB），不发送完整对话；单链 2 次 recovery、全局 2 并发/分钟 6、幂等键去重。
- 为什么：防止模型"还想再检查一次"自动扩大副作用。
- 出处：`docs/2026-07-29-replan/plan.md`

**决策 23 · 压缩是产品事实：元数据上屏、失败即停**
- 决策：`Snapshot.Task` 增 `context_compactions`（版本/原因/消息数/估算 token），用户可见压缩事实；压缩失败经 context_hook 传回 runChat 停止下一轮并报告。
- 为什么：不能以丢失证据的 checkpoint 继续执行；**任何降级都要是显式事件，不能是静默行为**。
- 出处：`docs/2026-07-30-context-summary/code-changes.md`、`docs/blog/seelex-technical-blog-2026.md` §2.2

> **PM 视角**：这一章定义了产品的核心名词——**"任务"从聊天变成有验收的工作单元**（`task_complete` 闭环）。这也是后来北极星指标"每周完成的有效任务数"的种子。长任务的宪法是：有终态、有证据、有边界、失败要可见。

---

## 第五章 大转向：Seele v2 与"权威 preflight"的失败（2026-08-01）

**处境**：上游 Seele 重构到新装配模型，Seelex 必须整体迁移；同时上一章的"强制 preflight 权威 Plan"开始暴露问题。

**决策 24 · 整体迁移到新装配模型**
- 决策：`agent.NewWithComponents` / `session.Session` / `event.Sink` 契约接管，`engine.ReplaceHistory` 退役；TaskService 只消费事件投影，不再直连 workplan 内部状态。
- 为什么：让 Plan→Task 的状态流统一走事件投影，为后续并发改造铺路。
- 出处：`docs/2026-08-01-seele-v2-underlying-refactor/plan.md`

**决策 25 · 存储双轨：ProjectKnowledge + SessionContextStore**
- 决策：项目级 `ProjectKnowledge`（跨会话共享，hash 判定增量重建）+ 会话级 5 栈 `SessionContextStore` + ChatQueue 滑动窗口；窗口 N 由 `WindowPolicy` 构造注入（ratio=0.7、Min=4、Max=40）。
- 为什么：项目知识与会话上下文生命周期不同，分开治理。
- 出处：`docs/2026-08-01-seele-v2-underlying-refactor/plan.md`

**决策 26 · 同日推翻"强制规划"：权威 preflight 是失败设计**
- 决策：删除 `PlanPolicy.RequirePlan`、`PlanActScope`、preflight 注入与 authority envelope——规划改为**模型自愿**；仅显式 `PrepareReplan` 保留隔离规划会话。
- 为什么：强制流程伤害长尾简单任务，模型应能按需加载 DAG。
- 代价：这是**上线后最短命的设计**之一；但产品收获了一条纪律——流程是杠杆不是关卡。
- 出处：`docs/2026-08-01-seele-v2-underlying-refactor/plan.md`、`docs/2026-07-29-replan/finish-review.md`

> **PM 视角**：最快的决策是"撤回昨天的决策"。这一章产品学会：**Plan 应该是可选的杠杆**——简单任务不承担规划延迟与 token 成本（这也写进了 README 的产品叙事）。

---

## 第六章 并行执行落地：fork 架构与死锁战役（2026-08-02 ~ 08-05）

**处境**：第三章审查说"并行 WorkPlan 不过生产可靠性审查"。现在是兑现的时候，而并发的真实代价——死锁——也在这一章集中爆发。

**决策 27 · fork 入口分层：模型自由层 + 固化层**
- 决策：模型自由层（todolist + `fork_subagents` 程序化全并行 DAG，参数仅 id+goal）+ 固化层（goal skill 激活才注入 plan 工具族）。
- 为什么：minimax 等弱模型不会构造 plan JSON 且不知收尾；自然语言规划强、结构化 JSON 弱。
- 出处：`docs/2026-08-03-subagent-fork-architecture/plan.md`

**决策 28 · worktree 生命周期由框架强制编排**
- 决策：开 worktree→干活→子代理自 rebase（框架兜底检测）→合并前用户审批（diff 摘要）→merge 回主工作区；非 git/只读/创建失败降级共享工作区。
- 为什么：物理隔离并行 + 不依赖模型自觉，防弱模型乱写文件进主工作区。
- 代价：merge 冲突处理与 CRLF 幻影脏（Windows 下干净 worktree 被误判为脏）成为新问题。
- 出处：`docs/2026-08-03-subagent-fork-architecture/plan.md`、`docs/devlog/2026-08-11-subagent-interview-answers.md` Q11

**决策 29 · 子代理能力对齐主代理（删 6 工具白名单）**
- 决策：只排除 plan 工具族与 task 终态工具；skill 目录/完整指令按节点注入。
- 为什么：状态所有权差异而非能力差异；防递归 DAG 与错误终结主任务。
- 出处：`docs/2026-08-03-subagent-fork-architecture/plan.md`

**决策 30 · 死锁战役（三次教训）**
- 教训 1（2026-08-02 冒烟实测 **19 分钟死锁**）：子代理 merge-back 不能直接 AppendHistory 写回主会话——主会话 ChatStream 期间持锁，直接写会形成循环等待。改为：merge-back 只写 Runtime 自有 `subagentContextActor` mailbox，application 在锁外时刻 Drain 注入。
- 教训 2（工具完成投影自锁）：`handleToolCompleteObserved` 持写锁后再经 `goalSkillActiveFn` 取读锁 → RWMutex 自锁。改为原子布尔快照。
- 教训 3（Worktable 环路死锁）：service.mu → 会话锁 → actor → channel 环路，4 子代理全卡 queued。改为 CSP：回调 observer 改 channel、emitChange 非阻塞（满则丢+计数）、锁外取子代理树。
- 出处：`docs/devlog/2026-08-11-subagent-interview-answers.md` Q4/Q5/Q8、`docs/2026-08-04-context-memory-lifecycle/runtime-tool-completion-deadlock.md`、`docs/2026-08-09-worktable/retrospective.md` R4

**决策 31 · 上下文"三时刻存在"原则 + B3 沙箱回滚**
- 决策：上下文只在①前端 select ②后端写 ③select 递 LLM 时存在，其余时间不驻留内存（长会话 3 份无界副本是卡顿根因）；scopedBash 恢复 v1 直连 exec，CommandSandbox 接口保留 fail-fast 待根因定位后再接入。
- 为什么：可用性优先——沙箱接入疑似导致工具挂起，先恢复，再回来修。
- 出处：`docs/2026-08-04-context-memory-lifecycle/plan.md`、`docs/2026-08-04-liveness-remediation/finish-review.md`

> **PM 视角**：并发不是技术债而是产品特性——"并行子代理不互相踩坏文件"（FileSystemActor 路径分片）是卖点；死锁是它的反面教材。三次死锁教训沉淀成一条纪律：**跨边界只传消息与不可变快照，不共享可变状态**（actor / atomic / 短锁三分法，见访谈 Q5）。

---

## 第七章 从工程到产品：调研、试点、路线图（2026-08-06 ~ 08-09）

**处境**：工程能力齐了，但产品没有真实用户、没有市场验证。这一章从"工程直觉"走向"市场证据"。

**决策 32 · Agent 竞品调研：竞争焦点转向"持久化 Agent 工作台"**
- 决策/结论：Claude/Cursor/Devin/OpenHands 等证据显示，竞争从单会话转向记忆/定时/自迭代闭环；**Seelex 最大共性差距 = 跨会话记忆缺席**；闭环骨架（verify/approve/deliver 节点）最接近共识。
- 落地优先 Top3：D1 自迭代试点、D2 verify 验证门+有界回环、B1 SubAgentTree+B3 checkpoint。
- 出处：`docs/2026-08-06-agent-landscape-research/research-agent-landscape-2026-08.md`

**决策 33 · 试点两阶段：先用 tasklist 验证语义，再固化为 DAG**
- 决策：M1 用 tasklist 串行手写驱动验证闭环价值（零产品代码）；M2 再固化为 Plan DAG 子代理；试点载体先用独立示例仓库，后期才对 seelex 自身 dogfooding。
- 为什么：先验证语义，避免"改自己=破坏性风险叠加"；隔离风险、结果可独立评判。
- 出处：`docs/2026-08-06-agent-landscape-research/plan-iterative-product-pilot.md`

**决策 34 · 敏捷 A2A：角色即隔离**
- 决策：开发固化为 TL×Programmer A2A 协议——TL 只决策（拆需求/任务卡/门禁/重规划），Programmer 只实现（编码/测试/自检/本地 commit），评审独立第三账号；四道门禁（需求锁→验证门→评审门→生产门）；任务切分三优先（验收标准边界 > 文件所有权 > 工作量）。
- 为什么："写者不给自己打分"是结构性的质量保证；文件级所有权是 worktree 并行合并安全的前提。
- 代价：需补 10 个缺口（4 个 S 级）；验收标准写不出命令的 REQ 退回。
- 出处：`docs/2026-08-07-agile-a2a/architecture.md`、`design.md`

**决策 35 · LoopX 调研：借鉴"类型化的 gate"，不借鉴 JSONL/quota**
- 决策/结论：gate 升为一等对象（具体问题+阻塞路线+safe default+旁路）、verify 失败类型化 recovery_kind、incident 文档文化；明确不借鉴完整事件溯源 JSONL、quota 分钟槽账本、heartbeat 19 步（单进程本地 harness 成本高收益有限）。
- 为什么：LoopX 事故教训=固化机器可读 typed contract 而非加提示语。
- 出处：`docs/2026-08-07-loopx-context-research/research-loopx-context-management.md`

**决策 36 · Worktable：统一工作台 + CSP 并发改造**
- 决策：右侧工作台统一为工作表格（Plan 树/待办/子代理三区移除，详情弹窗保留）；todolist 内化为 kind=todo 的 task（单一注册表 actor + 责任链事件）；缓存优化——system prompt 只留 plan 级稳定信息，动态任务状态改请求尾部 worktable 标记块。
- 为什么：fork 期间 Plan=nil 且子代理跑完即清走、无证据（R1）；4 子代理全卡 queued 事故暴露环路死锁（R4）；节点推进即整段前缀缓存失效，命中率 66% vs codex-cli 99.8%（R3）。
- 出处：`docs/2026-08-09-worktable/plan.md`、`retrospective.md`

**决策 37 · 产品路线图：三支柱差异化 + 明确"不做"清单**
- 决策：差异化三支柱 = 可逆上下文治理 + 多模型账号池路由 + 本地优先可审计；三个 Persona（成本敏感个人/数据主权团队/研究型用户）；四阶段路线（2026 Q4 产品化收口 → 2027 Q1 Beta → 2027 H1 v1.0 → 2027 H2+ 生态）；北极星指标 = **每周完成的有效任务数**。
- 明确不做：IDE 插件、云托管、自研模型、代码库语义索引（v1.x 再评估）。
- 为什么：避开巨头正面战场；治理/数据主权是自托管 Harness 的生态位。
- 出处：`docs/product/roadmap-2026.md`

> **PM 视角**：这一章产品第一次用**市场证据**而不是工程直觉做决策。调研报告成为依据（决策 32/35），试点用"零产品代码"验证语义（决策 33），路线图敢于写"不做"（决策 37）。差异化是选择出来的，不是堆出来的。

---

## 第八章 验证与反思：SWE-bench 与子代理访谈（2026-08-11）

**处境**：需要回答"这个 harness 真的能干活吗"，并把手里的隐性知识变成团队资产。

**决策 38 · SWE-bench 验证：诚实的有偏，而不是虚假的全量**
- 决策/结论：三轮评测累计 **90/90 Pass@1**（batch10：pytest×8+xarray+flask；batch30：sympy×26+pytest×3+xarray；batch50：sympy×50 全版本区间）；git worktree 精确检出官方 base_commit + 轻量容器验证，官方 eval 测试判定。
- 诚实标注：**有偏样本**（SWE-bench Lite 中挑选不依赖外部 HTTP 的实例），不代表整体基准分数；出官方口径分数需换环境跑全量。
- 为什么：验证要"可复现 + 如实标注局限"，为发布留出诚实的证据基线。
- 出处：`docs/swebench/batch10-report.md`、`batch30-report.md`、`batch50-report.md`

**决策 39 · 子代理访谈：把架构知识变成可传承的文档**
- 决策/结论：18 问覆盖架构边界（每节点独立 Session、fork 先造 DAG 再走 workplan 引擎、账号池 P2C）、并发模型（actor/atomic/锁三分法、mailbox 溢出只计数不丢内容、merge-back 防覆盖）、失败一致性（任务已停止 vs 工具执行失败、retry 触发条件、worktree 保留 vs 清理、取消不丢产出）、上下文缓存（前缀命中前提、稳定块前置动态块置后）、可观测性与测试（如何复现"任务已停止"、mock panic 教训、Windows race 限制）。
- 为什么：这些是 6 周死锁/并发战役换来的知识，**文档是单作者团队唯一的可扩展资产**。
- 出处：`docs/devlog/2026-08-11-subagent-interview-answers.md`

**决策 40 · 技术博客：把四个问题讲给市场听**
- 决策/结论：发布 `docs/blog/seelex-technical-blog-2026.md`，用真实代码回答上下文/编排/扩展/安全四问；明确生态位——Claude Code 吃终端重度用户、Cursor 吃 IDE 体验、Devin 赌自主执行、**Seelex 走"可组合、可审计、可自托管"的中间路**。
- 出处：`docs/blog/seelex-technical-blog-2026.md`

> **PM 视角**：验证的纪律是"诚实的有偏"——90/90 值得骄傲，但报告第一行就写明是有偏样本。访谈与博客是把个人经验产品化的两种形式：一个对内传承，一个对外叙事。

---

## 终章：现在的坐标与下一章

**现在的坐标**：
- **v0.0.1**（2026-08-03）已发布：Developer Alpha，TUI 主入口 + GUI Alpha，全功能清单见 `CHANGELOG.md`。
- **v0.0.2**（进行中）：工程信任修复——release 一致性、错误边界、并发、测试、开源治理（README 重写、移除陈旧 go.work/replace 声明）。
- **v0.1.0**：保留给破坏性架构重写（`CHANGELOG.md`）。

**已承认的缺陷**（产品下一章必须面对的债）：
- Full Access 曾不可关闭（安全状态欺骗）——已修复，见 `docs/devlog/2026-07-28-finish-review.md` §1 与 `seelebridge/registry.go`。
- 无账号配置时伪装成 OpenAI 假账号（`seelebridge/config.go` fallbackAccountConfig）——仍是风险。
- 项目范围不是沙箱、MCP 无 PathGate 门禁、grep symlink 越界——`docs/devlog/2026-07-28-finish-review.md` §3、`docs/arch/permission-path-gating.md`。
- Application Core 隐式 God Service、main.go 承载业务逻辑、契约层依赖实现层——`docs/arch/ARCHITECTURE_REVIEW.md` D1/D3。
- TUI 覆盖率 6.2%、benchmark 几乎为零、多项性能假设未验证——`docs/test/2026-07-27-test-report.md`、`docs/arch/ARCHITECTURE_REVIEW.md` P2。

**给下一章的问题**（roadmap-2026 的风险清单）：
1. 无真实用户——30 分钟内"安装→配置→跑通真实任务"是阶段 0 的验收线。
2. 跨会话记忆缺席——最大共性差距，D1 试点是能力牵引器。
3. 代码库语义索引缺席——明确不进 v1.0，v1.x 用 ProjectKnowledge 演进评估。
4. 单作者 201 commits——社区化（好第一 issue、维护者制度、赞助）是可持续性的前提。

---

## 附录 A：决策时间线总表

| 时间 | 主线 | 关键决策 |
|---|---|---|
| 2025-07 ~ 2026-07-16 | 起点 | 8 层装配 → Model 接口化 → Context Provider/双向合并 |
| 2026-07-17 | 分层 | TUI/Application Core 分离 + Runtime/Plugin 重构 |
| 2026-07-19~24 | 形态 | Skill 分区、Effort 全栈、Skill envelope、DSL 卡片、Plan 可视化、子代理可见性 |
| 2026-07-23~28 | 质量 | GUI reducer、文档 CI、黄金旅程 E2E、会话窗口、Workplan 并行（不通过审查）、Plan JSON DSL、ProjectScope fail-closed、存储原子化 |
| 2026-07-29~30 | 宪法 | 强制终态协议、证据 checkpoint、语义进展、replan 有界、压缩显式化 |
| 2026-08-01 | 转向 | Seele v2 迁移、存储双轨、**推翻强制 preflight** |
| 2026-08-02~05 | 并发 | fork 分层、worktree 编排、三次死锁教训、三时刻存在、B3 沙箱回滚 |
| 2026-08-06~09 | 产品 | 竞品调研、试点两阶段、敏捷 A2A、LoopX 借鉴、Worktable、roadmap 三支柱 |
| 2026-08-10~11 | 收口 | Runtime 拆分（planExecutor）、SWE-bench 90/90、子代理访谈、技术博客 |
| 2026-08-12+ | 信任 | v0.0.2 工程信任修复；v0.1.0 留给破坏性重写 |

## 附录 B：主题决策速查

- **定位**：薄 TUI 客户端 → Coding Agent Harness（可组合/可审计/可自托管）
- **架构**：分层 + seelebridge ACL + 契约端口 + 事件投影 + actor 并发
- **上下文**：预算驱动滑动窗口 + 可逆压缩（差异化）+ 三时刻存在 + 语义进展 + 失败即停
- **执行**：Plan 可选（非强制关卡）+ 终态协议 + 证据 checkpoint + 有界 replan
- **并行**：fork_subagents 造 DAG 走 workplan 引擎 + worktree 框架编排 + 跨边界只传消息
- **安全**：ProjectScope fail-closed + PathGate allow/ask/deny + CommandSandbox 端口（回滚待续）
- **市场**：三支柱差异化 + 明确不做清单 + 北极星"每周完成的有效任务数"
- **验证**：SWE-bench 诚实有偏 90/90 + 文档契约测试 + 三平台 CI + 覆盖率门禁

## 附录 C：主要出处索引

- 研发日志：`docs/devlog/`（2026-07-17 分层计划与 review、2026-08-11 子代理访谈）
- 架构决策：`docs/arch/`（architecture-and-flaws、context-improvement-plan、plugin-skill-restructure、effort-system-design、skill-effort-architecture、agent-workbench-architecture、plan-visualization-design、subagent-visibility-design、session-snapshot-liveness、permission-path-gating、seele-v2-runtime-architecture、plan-executor）
- 工作包：`docs/2026-07-*`、`docs/2026-08-*`（plan / finish-review / retrospective / code-changes）
- 调研与规划：`docs/research/`、`docs/product/roadmap-2026.md`
- 叙事与验证：`docs/blog/seelex-technical-blog-2026.md`、`docs/swebench/*-report.md`
- 变更与状态：`CHANGELOG.md`、`README.md`、`docs/devlog/2026-07-28-finish-review.md`、`docs/arch/ARCHITECTURE_REVIEW.md`
