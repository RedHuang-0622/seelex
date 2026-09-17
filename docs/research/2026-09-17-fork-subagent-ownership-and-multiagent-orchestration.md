# fork_subagents 的管理归属 + 多代理编排行业口径（2026-09-17）

> 本文两部分：
> - **Part A**：回答"fork subagent 的管理者是 fork 出去的 node 还是 session / 底层框架融合了什么"——全部结论来自本仓库代码与本机实测，逐条带 `文件:行` 或工具输出。
> - **Part B**：工作打点表 todo:94–110 的检索结果（分主题清单 + 可引用结论 + 未核实清单）。
>
> 取证方式（本次实际可用通道）：`web_search` 工具返回 403（额度不足）→ 改用本机 Playwright MCP 直接抓官方页面原文（成功）；`fork_subagents` 工具返回 402（子代理账号余额不足）→ 检索改由主代理串行完成。
> 所有 Part B 结论均来自**官方一手页面**（发布方域名）或 Crossref 书目 API；未取得一手原文的条目一律进"未核实"，不做推断填充。

---

## Part A：fork subagent 的归属链路

### A1 结论速览

**都不是"其中一方"，而是三层分工；真正的"管理者"是 Runtime（会话域 actor），node 是执行体，session 是身份与额度归属。**

| 层 | 归属 | 证据 |
|---|---|---|
| 编排 / 调度 | **会话级**：`plan.Executor` 是 Runtime 单例，额度按会话建槽 `slots[sessionID]{policy,binding,runID}`；fork 的 `plan_run` 与该会话的 `plan_run` **同槽登记、事件归属同一会话** | `seelebridge/plan/executor.go`（`slotMu/slots`、`beginRunFor/endRunFor/AppendPhase`）；`seelebridge/fork/tool.go` 的 forkCtx 注释与代码 |
| 执行单元 | **节点级**：每个子代理 = `kind:agent` 节点 = `AgentNode.Run`（开 worktree → 建节点 Session → 执行 → 终态打点 → merge-back → worktree 收尾） | `seelebridge/node/agent_node.go`（`Run`）；`seelebridge/node/README.md` |
| 身份 / 归属键 | **节点 ID 为身份键 + 会话 ID 为归属字段**：会话注册表 `sessions map[nodeID]`、`mainSessionIDs map[nodeID]→主会话ID`、子代理树 `nodes map[nodeID]` / `children map[parentID][]nodeID` | `seelebridge/session/subagent_sessions.go:29-60`、`seelebridge/session/subagent_tree.go:83-101` |
| fork 独占语义 | **会话级**：fork 运行中同会话对话被拒（`ErrForkRunningChat`）；`forkActive map[sessionID]int` 按会话计数 | `application/core/service_input.go:83,134`；`seelebridge/runtime.go:718-752` |
| 父子归属 | 父 = 发起 fork 的**节点**（NodeScope 存在且 `Role==RoleSubAgent`）否则 = `MainAgentNodeID` | `seelebridge/fork/tool.go:120-124` |

一句话：**"谁在跑"是 node（每节点一个独立 Session + 一个 worktree），"归谁管"是 session（run 槽、事件归属、会话独占与计数），"谁持有账本"是 Runtime 的会话域 actor（注册表 / 子代理树 / task 注册表）。**

### A2 完整调用链（可复核）

1. 工具注册：`Runtime.registerForkTool()` 注册 `fork_subagents`（schema：`subagents[].id/goal`、`max_concurrency`、`timeout_sec`）→ handler = `Runtime.forkSubagentsHandler`（`seelebridge/runtime_plan.go`）。
2. handler：读 ctx 会话 ID → `ForkBegin(sessionID)` → `fork.Tool.Handle` → `ForkEnd`（`runtime_plan.go:236-241`）。
3. `fork.Tool.Handle`（`seelebridge/fork/tool.go:43-146`）：
   - 参数校验 + `PlanPolicy.MaxNodes` 护栏；
   - B6：按归一化 goal 查 task 注册表 → 命中绑既有 `task_id`（终态→置 retry），未命中新建 `subagent:<id>`；
   - 结果复用短路：全部子代理都命中"已完成 task + 子代理树仍保留完整输出"→ 直接返回已存输出（`reused:true`），不重跑；
   - `buildForkPlan`：程序化 DAG `start(auto) → s1..sN(agent) → summary(summary)`；
   - `SubagentTreeRegisterFork(parentID, specs)` 登记子代理树；
   - 超时：剥离外层 deadline、从 `Background` 派生 `forkCtx` 并**把执行会话 ID 重新注入**，`RunPlan(forkCtx, loaded, withNodeOutputs=false)`。
4. `RunPlan`（`seelebridge/plan/tool_provider.go`）：`beginRunFor(sessionID)` → `newPlanRunner`（Seele `workplanrunner.New` + `SetMaxForkConcurrency`）→ `Run` → `planRunResultJSON`（fork 路径 `withNodeOutputs=false`：节点输出不内嵌，只留 `final_output`）。
5. 每个 agent 节点：`seenode.NewAgentNode`（`runtime_plan.go` 的 `nodeFactoryDeps`）→ `AgentNode.Run` → `factory.NewAgent(input)` 建**每节点独立 Session** → `RegisterNodeSession(sessionIDFromCtx, nodeID, sess, goal)` → `ChatStream` → `CompleteSubagentNode` → `mergeBack` → worktree 收尾。
6. 收尾：`FinishNodeWorktree`/`ReleaseNodeWorktree`；"子代理留下未提交改动"只降级为产出末尾 `[收尾警告]`，节点仍算成功（`agent_node.go` 注释 + `worktree.IsUncommittedChanges` 分支）。

### A3 底层框架融合了什么（"融合"这个词是准确的）

```text
Seele（框架）           workplan(codec/runner/checkpoint/approve/event) + session(Session) + tools注册表
        ▲ 提供 DAG 内核、并发槽、事件与快照、会话对象
seelex（装配层）        plan.Executor（per-session 槽）/ node.AgentNode / fork DAG 构造 /
                        session 域 actor（subagentSessions、subagentTree）/ task 注册表 / worktree / telemetry
```

- **fork 不是独立执行器**：设计文档明确 "fork 是 plan 的无依赖特例，**不需要独立执行器**"（`docs/2026-08-03-subagent-fork-architecture/plan.md` §4.2），执行路径与 `plan_load/plan_run` 共用同一 workplan runner。
- **框架侧只管 DAG**：节点 ID、父子归属、任务幂等、结果复用、worktree 生命周期、子代理树全部是 seelex 侧装配（`seelebridge/fork`、`node`、`session`、`task`、`worktree`）。
- 并发上限走框架 runner（`SetMaxForkConcurrency`，值来自 `plan.PolicyConcurrency`：显式配置优先，否则 = 节点数）。

### A4 三个由代码路径确认的边界问题（建议核验/后续处理）

1. **run_id 槽竞争（Confirmed by code path）**：fork 的 `RunPlan` 用 `beginRunFor(sessionID)` 覆盖**同一会话槽**的 `runID`，结束时 `endRunFor` 又把槽清空（仅当仍等于自己的 runID）。若同会话内主 plan 的兄弟节点与 fork 并发执行，兄弟节点后续 `AppendNodePhase` 读到的是 **fork 的 runID 或空串** → 事件归属漂移（`plan/executor.go` 的 `AppendPhase` 在调用时读槽；`node/coordinator.go` 的 `AppendPhase` 委托给它）。这是"同槽登记"设计的副作用，不是状态损坏。
2. **nodeID 是进程级全局键（Confirmed by code）**：`subagentSessions.sessions[nodeID]`、`subagentTree.nodes[nodeID]` 均以模型自选的 `spec.id`（常见 `s1/s2/lead`）为键，`RegisterFork` 注释自承"同 id 重复 fork 覆盖旧记录"。多会话并行 fork 相同 id 会互相覆盖记录（归属字段 `mainSessionIDs` 无法区分同名节点）。
3. **提示词与工具面不一致（Confirmed）**：子代理章程文本提到 `fork_subagents`（`agent_node_test.go:148` 断言 `charter.md` 含该词），但可见性策略把 `fork_subagents` 对子代理屏蔽（`seelebridge/tools/policy.go:133` `nodeScopeExcludedTool`，理由"fork 会递归派生子代理（无深度控制）"）→ **当前实际不存在嵌套 fork**，`fork/tool.go` 的嵌套父归属分支与章程文案属死路径/陈旧文案。

### A5 本机实测（本次会话事实）

- 调用 `fork_subagents`（7 个子代理、timeout 2400s）：**失败**——`stream with account "subagent-1": HTTP 402 Insufficient Balance`。
  → 说明 fork 的 agent 节点按 `RoleSubAgent` 经账号池解析到子代理账号（`account.ResolveForBranch(pool, role, planID+":"+branchID)`，`runtime_plan.go` 的 `resolvePlanBranchAccount`/`roleForPlanBranch`），与主会话账号隔离；子代理账号余额为 0 时 fork 直接不可用（不是"降级用主账号"）。
- fork 汇总窗口上限：每子代理 **30 行 × 160 rune**（`seelebridge/fork/summary.go` 的 `SummaryMaxLines/SummaryLineLimit`），超出部分需 `read_tool_result` 读回——设计如此，但决定了"fork 派活"不适合回传长篇交付物。

---

## Part B：工作打点表 todo:94–110 检索结果

状态图例：✅ = 已取官方一手原文；⚠️ = 仅书目/二手可核验；❌ = 本次未取得。

### todo:100 MetaGPT SOP ✅

- 来源：arXiv abs 页 https://arxiv.org/abs/2308.00352（ICLR 2024）
- 原文（摘要）："MetaGPT encodes **Standardized Operating Procedures (SOPs) into prompt sequences** for more streamlined workflows, thus allowing agents with human-like domain expertise to **verify intermediate results** and reduce errors. MetaGPT utilizes an **assembly line paradigm** to assign diverse roles to various agents, efficiently breaking down complex tasks into subtasks…On collaborative software engineering benchmarks, MetaGPT **generates more coherent solutions than previous chat-based multi-agent systems**."
- 机理解读（可引用）：SOP = 角色分工 + 提示词序列 + **可验证的中间产物**（文档交接），用"先出中间件再校验"压制 naive chaining 的级联幻觉（摘要原文提到 "logic inconsistencies due to cascading hallucinations caused by naively chaining LLMs"）。
- 局限：本次未抓论文 Limitations 段 → 见未核实 #1。

### todo:102 Anthropic 多代理 research 系统 ✅（关键数字全部取自官方博客）

- 来源：https://www.anthropic.com/engineering/multi-agent-research-system（Published Jun 13, 2025）
- 架构："uses a **multi-agent architecture with an orchestrator-worker pattern**, where a lead agent coordinates the process while delegating to specialized subagents that operate **in parallel**"；LeadResearcher 先把计划写入 Memory（"if the context window exceeds **200,000 tokens** it will be truncated and it is important to retain the plan"），再派生 Subagents，最后交 CitationAgent 定位引用。
- 性能与成本（原文）："a multi-agent system with **Claude Opus 4 as the lead agent and Claude Sonnet 4 subagents outperformed single-agent Claude Opus 4 by 90.2%** on our internal research eval"；"**token usage by itself explains 80% of the variance**"（BrowseComp；三因素合计 95%）；"agents typically use about **4× more tokens** than chat interactions, and multi-agent systems use about **15× more tokens** than chats"。
- effort 分配（原文数值，可直接作为 fork/子代理预算参考）："Simple fact-finding requires just **1 agent with 3-10 tool calls**, direct comparisons might need **2-4 subagents with 10-15 calls each**, and complex research might use **more than 10 subagents** with clearly divided responsibilities."；早期失败模式 "**spawning 50 subagents for simple queries**"。
- 委派契约（原文）："Each subagent needs an **objective, an output format, guidance on the tools and sources to use, and clear task boundaries**. Without detailed task descriptions, agents **duplicate work, leave gaps**…"（举例：一个子代理查 2021 芯片危机、另两个重复查 2025 供应链）。
- 何时不该用（原文）："some domains that **require all agents to share the same context or involve many dependencies between agents are not a good fit**"；"**most coding tasks involve fewer truly parallelizable tasks than research**, and LLM agents are **not yet great at coordinating and delegating to other agents in real time**"；适用面 = "heavy parallelization, information that exceeds single context windows, and interfacing with numerous complex tools"。

### todo:104 Claude Code subagent/delegation ✅

- 来源：https://code.claude.com/docs/en/sub-agents（即 docs.claude.com/en/docs/claude-code/sub-agents）
- 定义与动机（原文）："Use one when a side task would **flood your main conversation** with search results, logs, or file contents you won't reference again: the subagent does that work **in its own context and returns only the summary**."；"Each subagent runs in its **own context window** with a **custom system prompt, specific tool access, and independent permissions**."
- 委派机制（原文）："Claude uses each **subagent's description** to decide when to delegate tasks"；"**Subagents work within a single session**"（要并行多会话 → background agents；互相传消息 → cross-session messaging；Claude 自己监督的会话团队 → agent teams）。
- 配置面（原文）：文件 = Markdown + YAML frontmatter，位置 `.claude/agents/`（项目）/ `~/.claude/agents/`（用户，优先级更高）；字段含 `name`(必填)、`description`、`tools`、`disallowedTools`、`model`、`permissionMode`、`mcpServers`、`hooks`、`maxTurns`、`skills`、`initialPrompt`、`memory`、`effort`、`background`、`omitClaudeMd`、`isolation`；`--agents` flag 接受同名字段的 JSON；**插件来源的子代理不支持 `hooks`/`mcpServers`/`permissionMode`**（安全原因，字段被忽略）。
- 成本护栏（原文）："When the combined descriptions of your subagents … exceed **15,000 tokens**, Claude Code shows a warning at startup with the total token count"；`maxTurns` 到顶返回标记为 **partial** 的输出且可 resume。
- 未确认：官方是否明确写"子代理不能再派生子代理"——本次抓取片段未出现该句 → 未核实 #2（不凭记忆断言）。

### todo:94 CrewAI sequential / hierarchical ✅

- 来源：https://docs.crewai.com/en/concepts/processes ；https://docs.crewai.com/en/concepts/agents
- 原文：Sequential = "Executes tasks **sequentially**, ensuring tasks are completed in an orderly progression."；Hierarchical = "Organizes tasks in a managerial hierarchy, where tasks are delegated and executed based on a structured chain of command. A manager language model (**manager_llm**) or a custom manager agent (**manager_agent**) must be specified"。
- hierarchical 细节（原文）："Emulates a corporate hierarchy… This agent oversees task execution, including **planning, delegation, and validation**. **Tasks are not pre-assigned**; the manager allocates tasks to agents based on their capabilities, reviews outputs, and assesses task completion."
- 委派开关（原文）：Agent 字段 `allow_delegation`（bool，**默认 False**，"Allow the agent to delegate tasks to other agents"）；另有 `max_iter`（默认 20）、`max_rpm`、`verbose` 等资源护栏。

### todo:96 AutoGen GroupChat + speaker selection ⚠️部分

- 来源：https://microsoft.github.io/autogen/stable/user-guide/agentchat-user-guide/selector-group-chat.html ；`.../swarm.html` ；https://microsoft.github.io/autogen/0.2/docs/reference/agentchat/groupchat/
- 现行（AgentChat，0.4+）官方口径：`SelectorGroupChat` = "participants take turns **broadcasting messages to all other members**. A generative model (e.g., an LLM) **selects the next speaker based on the shared context**"；默认"**will not select the same speaker consecutively** unless it is the only agent available"（可由 `allow_repeated_speaker=True` 改）；可自定义 selection prompt / selection function / candidate function；**agent 的 `name` 与 `description` 就是选择依据**。
- `Swarm` = "agents can **hand off task to other agents**… The key idea is to let agent delegate tasks to other agents using a **special tool call**, while **all agents share the same message context**… This enables **local decisions** rather than a central orchestrator"；handoff 由 `HandoffMessage` 驱动，用模型的 tool-calling 生成 → 若模型并行调用工具可能**一次生成多个 handoff**（官方建议关掉 `parallel_tool_calls`）。
- 0.2 时代的 `GroupChat`：有 `speaker_selection_method` 与 `auto` 选择法（含 select_speaker_* 模板、多名字/零名字重试模板）——但**"auto/round_robin/random/manual" 四项枚举本次未逐字取到** → 未核实 #3。
- 可引用要点：AutoGen 两条路线是"**中央 LLM 选发言者**（SelectorGroupChat）"vs"**平权 handoff + 共享上下文**（Swarm）"，且都强调**共享消息上下文**（与 Anthropic/Cognition 的"上下文不可共享是硬约束"观点正好相反）。

### todo:98 LangGraph supervisor / swarm / handoff ✅

- 来源：https://docs.langchain.com/oss/python/langchain/multi-agent ；`.../supervisor` ；https://raw.githubusercontent.com/langchain-ai/langgraph-supervisor-py/main/README.md ；https://raw.githubusercontent.com/langchain-ai/langgraph-swarm-py/main/README.md
- **官方当前推荐（重要）**：langgraph-supervisor README 首段——"**We now recommend using the supervisor pattern directly via tools rather than this library** for most use cases. The tool-calling approach gives you more control over **context engineering**… "。
- 模式表（原文，multi-agent 页）：`Subagents` = "A main agent coordinates subagents **as tools**. All routing passes through the main agent"；`Handoffs` = "Behavior changes dynamically based on state. **Tool calls update a state variable that triggers routing or configuration changes**"；`Skills` = "specialized prompts/knowledge loaded on-demand"；`Custom workflow` = 用 LangGraph 自建。
- 选择矩阵（原文）：Subagents 在"Distributed development / Parallelization / Multi-hop"都满分、**Direct user interaction 仅 1 星**；Handoffs 反过来（direct interaction 4★，并行与分布式为 "-"）。成本对比（原文数值）：单任务 **Subagents 4 次模型调用 vs Handoffs 3 次**（"Subagents adds one extra call because **results flow back through the main agent**—this overhead provides centralized control"）；两轮会话 8 vs 5；**"Stateful patterns (Handoffs, Skills) save 40-50% of calls on repeat requests"**；多领域任务 Subagents ~9K tokens vs Handoffs ~14K+。
- 关键性质（原文）："**Subagents are stateless by design**—each invocation follows the same flow… The main agent maintains conversation context, but **subagents start fresh each time**. This provides strong context isolation but repeats the full flow."
- supervisor 机制（原文 + README）：supervisor "controls all communication flow and task delegation"；库特性 = "**Tool-based agent handoff mechanism**"；swarm = "agents dynamically hand off control to one another… remembers which agent was **last active**"（`create_swarm` + `create_handoff_tool` + checkpointer）。
- 何时用（原文，multi-agent 页）："not every complex task requires this approach—a single agent with the right tools and prompt can often achieve similar results"；多代理的价值点是 ①context management ②parallelization；触发条件 = "a single agent **has too many tools and makes poor decisions** about which to use"、需要长 prompt/领域工具、需要顺序约束解锁能力。

### todo:106 学术：Contract Net / blackboard / market-based ⚠️仅书目核验

- Crossref 书目核验（api.crossref.org，本次抓取成功）：
  - **Smith, R. G., "The Contract Net Protocol: High-Level Communication and Control in a Distributed Problem Solver", IEEE Transactions on Computers, 1980-12**（另有 1988 选集重印条目 `Readings in Distributed Artificial Intelligence`，DOI `10.1016/b978-0-934613-63-7.50039-5`）。IEEE 原文 DOI 本次输出被截断 → 见未核实 #4。
  - **Erman, Hayes-Roth, Lesser, Reddy, "The Hearsay-II Speech-Understanding System: Integrating Knowledge to Resolve Uncertainty"**：Crossref 本次返回的是 1981 选集条目（DOI `10.1016/b978-0-934613-03-3.50029-5`）；常被引用的 CACM 23(11):644-655 (1980) 未在本次抓取中确认 → 未核实 #5。
  - **Gerkey, B. P. & Matarić, M. J., "A Formal Analysis and Taxonomy of Task Allocation in Multi-Robot Systems", The International Journal of Robotics Research 23(9):939-954, 2004-09, DOI `10.1177/0278364904045564`**（Crossref 全文命中，含作者单位）。
- 机制口径本身（CNP 的 announcement/bidding/awarding 三阶段、blackboard 的知识源+控制单元、市场/拍卖式分配的激励兼容性）本次**未取到一手正文**，故不作断言 → 未核实 #4/#6。
- 与 Part A 的关联（本仓库既有判断，可复用）：`docs/arch/agent-team-work-vs-market.md` 已把"框架类方案（AutoGen/CrewAI/LangGraph/MetaGPT 系）"归为消息传递、把本仓库路线归为"唯一帧账本 + 裁决入账"；本次 Part B 的框架口径与该判断一致（AutoGen/Swarm **共享消息上下文**、CrewAI hierarchical **manager 评审**、LangGraph **往返主代理**）。

### todo:108 已知优劣 / 失败案例 / 何时不用 ✅

- **成本**（Anthropic 原文，见 todo:102）：多代理 ≈ **15× chat tokens**（代理本身 ≈ 4×）；"**For economic viability, multi-agent systems require tasks where the value of the task is high enough to pay for the increased performance**"。
- **并行有效判据**（Anthropic 原文 + LangGraph 原文）：适合"**breadth-first / 多条独立方向**"、信息超出单窗口、工具繁多；不适合"要共享同一上下文 / 代理间强依赖"以及"**大多数编码任务**"（真正可并行部分少 + 模型实时协调委派能力不足）。
- **上下文碎片化（对立观点，一手）**：Cognition《Don't Build Multi-Agents》(https://cognition.ai/blog/dont-build-multi-agents) 两原则原文——"**Share context, and share full agent traces, not just individual messages**"、"**Actions carry implicit decisions, and conflicting decisions carry bad results**"；并称这类（把子任务分给并行子代理再汇总）架构 "**very fragile**"，建议默认排除不满足上述原则的架构，最简可行解是 "**a single-threaded linear agent**"（上下文连续），超大任务再引入专门的上下文压缩模型。
- **失败模式有量化研究（一手）**：arXiv 2503.13657 "Why Do Multi-Agent LLM Systems Fail?"（https://arxiv.org/abs/2503.13657）——"**MAST-Data**, a comprehensive dataset of **1600+ annotated traces collected across 7 popular MAS frameworks**"；"first Multi-Agent System Failure Taxonomy (**MAST**)… through rigorous analysis of **150 traces**, guided by expert human annotators, validated by **inter-annotator agreement (kappa = 0.88)**… identifies **14 unique modes, clustered into 3 categories**: (i) **system design issues**, (ii) **inter-agent misalignment**, (iii) **task verification**"；并给出 LLM-as-a-Judge 标注流水线与跨模型（GPT4/Claude 3/Qwen2.5/CodeLlama）、跨任务（coding/math/general agent）的失败分布分析，结论含 "their performance gains on popular benchmarks are often minimal"。
- **归纳的"何时不用"清单（可引用，均出自上述一手来源）**：
  1. 任务需要所有代理共享同一上下文，或代理间依赖很密；
  2. 任务本身可并行部分很少（典型：多数编码任务）；
  3. 单代理加对工具/提示词即可解决（LangGraph："single agent with the right tools and prompt can often achieve similar results"）；
  4. 任务价值不足以覆盖 ~15× token 成本；
  5. 需要低延迟/低成本的单轮问答（并行会放大模型调用数，LangGraph 给出 4 vs 3 / 8 vs 5 的调用数差）；
  6. 需要强一致状态：并行分支各自做隐式决策 → 冲突（Cognition 原则 2）。

### todo:110 汇总输出 ✅

即本 Part B（分主题清单 + 未核实清单 + 可引用结论）。

### 未核实清单（本次无法确认，禁止据此推断）

1. MetaGPT 论文 Limitations 段的具体表述与实验数字（只取到摘要；README 未抓）。
2. Claude Code 官方是否有"subagent 不能再派生 subagent"的明文（抓取片段未见）。
3. AutoGen 0.2 `GroupChat.speaker_selection_method` 的完整枚举字面值（`auto/round_robin/random/manual`）——本次仅取到 `auto` 相关模板说明。
4. Smith 1980 IEEE 原文 DOI（Crossref 输出被截断）与 CNP 三阶段的原文表述。
5. Hearsay-II 原始出处（CACM 23(11):644-655, 1980）的书目确认（Crossref 只返回 1981 选集条目）。
6. market-based / auction 分配（含 Vickrey 次价拍卖）的一手文献与"激励兼容"原文表述。
7. Cognition 博文中对 Anthropic 多代理路线的直接回应段（本次抓取片段为原则主体）。

### 本次不可用通道（外部依赖，需用户决策）

| 通道 | 现象 | 影响 |
|---|---|---|
| `web_search` 工具 | `API error 403: You do not have enough money or package quota` | 检索改用 Playwright MCP（本机浏览器）完成，本次未受阻 |
| `fork_subagents` 工具 | `stream with account "subagent-1": HTTP 402 Insufficient Balance` | 检索无法并行派发子代理；只能主代理串行抓取 |
| Wikipedia / DBLP | `ERR_CONNECTION_CLOSED` / Anubis 反机器人校验 | 学术条目改用 Crossref API 核验书目 |
| Semantic Scholar API | `429 Too Many Requests` | 同上，改用 Crossref |

---

## 附：可复用结论（给 seelex 自身设计的 5 条）

1. **归属模型**：并行派活必须区分"执行体身份（node）"与"归属（session）"；本仓库已按此分层，但 nodeID 取自模型自选字符串，跨会话并行时是全局键冲突面（A4.2）。
2. **run 归属**：同一会话槽内嵌套/并发启动第二个 run（fork）会覆盖 `runID`，事件归属需要用"per-run 显式传参"而非"槽内隐式读取"（A4.1）。
3. **委派契约**：Anthropic 的四要素（objective / output format / tools & sources / task boundaries）是子代理 prompt 的最小集，本仓库 charter 已覆盖 Role/Context/Task/Investigation/Constraints/Verification，但**缺"输出格式 + 任务边界（别做什么、别重复什么）"的显式段**（可对照 `internal/promptassets/assets/subagent/charter.md`）。
4. **effort 分级**：可直接复用 Anthropic 的经验规则（1 agent/3-10 calls → 2-4 subagents/10-15 calls → >10 subagents）；本仓库 fork 只有 `MaxNodes` 与节点循环预算，缺"按查询复杂度给子代理数上限"的软规则。
5. **不要用多代理的判据**：需共享上下文、强依赖、可并行度低（多数编码任务）、价值覆盖不了 ~15× token 时，应退回单代理 + 工具/提示词（Cognition 原则 1/2 是硬约束而非偏好）。
