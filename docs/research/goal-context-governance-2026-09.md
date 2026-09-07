# Goal 上下文治理：业界做法调研（2026-09）

> 调研日期：2026-09-07。
> 方法：公开资料 + 多轮 web_search（中英）+ 仓库既有调研交叉；外部产品/论文描述为公开信息快照，机制级细节以厂商文档/论文原文为准。
> 关联仓库既有资料：`docs/research/coding-agent-harness-comparison.md`（2026-08-02）、`docs/research/agent-market-research-2026-08.md`（2026-08-07）、`docs/arch/context-prefix-chain.md`、`seelexctx/README.md`。
> 提问语境：以 Seelex 的 goal 能力（`plugins/default/goal/SKILL.md` + seelexctx 上下文承袭）为对照基准，调研“围绕目标（goal）的上下文治理”业界怎么做。

---

## 0. 结论速览

市面上“goal 的上下文治理”没有单一产品命名，而是 **6 条已收敛的机制路径**，目标（goal）在其中扮演六种角色：

| 机制路径 | goal 的角色 | 代表做法 |
|---|---|---|
| A. 目标即边界 | 隔离单元 | per-goal 会话/worktree/并行 agent |
| B. 目标种子注入 | 静态上下文源 | CLAUDE.md / GEMINI.md / AGENTS.md / rules / ProjectKnowledge |
| C. 目标章程承袭 | 子代理上下文契约 | 父只下传目标+必要证据；子只回传结果 |
| D. 目标期窗口维持 | 压缩/外化的锚点 | auto-compact / Condenser / result 外化 / masking |
| E. 目标导向记忆选择 | 检索查询词 | CoALA 检索、LangMem、Cursor 索引、goal-conditioned top-K |
| F. 目标终态与账本 | 上下文归档/续跑边界 | progress ledger / task terminal / A2A Task |

**一句话**：业界把 goal 当作“**上下文的边界 + 传递的契约 + 取舍的锚点**”来治理——先按目标隔离与静态注入，跨代理只传目标与结论，运行期以目标为锚压缩与外化，结束后把目标级成果写回记忆供下一目标复用。

对 Seelex goal 的对照结论：Seelex 在 **C（结构化快照承袭）、D（可逆压缩+预算）、E（goal-conditioned top-K）、F（任务终态+逃逸）** 上已属业界第一梯队甚至差异化；主要短板在 **B（不消费 AGENTS.md/CLAUDE.md 惯例记忆文件、目标级成果未跨会话落库）** 与部分工程件（completion buffer、语义检索、hooks/plan-mode 降权）。详见第 5 节。

---

## 1. 问题域拆解：goal 在业界语境里是什么

治理的对象不是“goal 这一个词”，而是它承载的三层结构：

1. **目标树（plan / DAG / plan mode）**：Claude Code plan mode、Codex plan mode、Cline Plan/Act、Cursor plan-first、Seelex WorkPlan DAG；
2. **一次可执行的子目标（task card / charter / droid context）**：Claude Code subagent、Magentic-One 的子任务卡、Factory droid context；
3. **执行载体（session / run / worktree）**：一次目标 = 一个隔离上下文实例。

“上下文治理” = 决定这个单元**装什么（注入）、丢什么（压缩/外化）、传什么（承袭）、留什么（记忆/归档）、何时停（终态）**。

---

## 2. 六条主流治理路径

### A. 目标即边界：per-goal 上下文隔离与并行

- 做法：一次目标一个独立上下文，避免跨目标污染；并行即多个目标各持一份上下文。
- 业界事实：Cursor background agents + git worktree + 云端 Ubuntu VM；Codex 后台/云端 worktree agent；Claude Code 并行 subagents；Antigravity 隔离 agent 环境；Magentic-One 每个 worker 独立 session。
- 治理含义：**上下文的“单位”对齐目标而非对话**；“一个会话干一件事”（仓库 agent-market-research 调研原文）。隔离是治理的第一道闸——它让压缩/记忆/回滚都能以目标为粒度实施。

### B. 目标种子注入：记忆文件与静态上下文分层

- 做法：把与目标强相关、长期稳定的知识**蒸馏成低 token 高密度文件**，随目标进入上下文，不占检索成本。
- 分层：全局（`~/.claude/CLAUDE.md`、`~/.gemini/GEMINI.md`）→ 项目（`CLAUDE.md`/`CLAUDE.local.md`、`GEMINI.md`、`AGENTS.md`、`.cursor/rules`、`ClineRules`）→ 子代理作用域（subagent 自带 CLAUDE.md）。
- 产品：Claude Code / Gemini CLI / Cursor / Cline / Windsurf memories（project/personal）。
- 工程共识：写 CLAUDE.md 的本质是“给每次都失忆但会完整读上下文的 Agent 一份可复用的目标上下文”（掘金/ETH SRI 转述，二手）。
- Seelex 现状：`ProjectKnowledge`（`seelex.project.md` + 模块扫描，hash 版本化，会话前预读）自动构建且比静态文件更自动，但**不消费 AGENTS.md/CLAUDE.md 惯例文件**（harness 文档 3.3 已指出）。

### C. 目标章程驱动的子代理上下文承袭（“give child only the goal”）

主流模式高度收敛：**父只下传目标 + 必要证据，子只回传结果/发现，绝不回传全文**。

- **Claude Code subagents**：子代理拥有“独立、聚焦的上下文”（系统 + 子代理指令 + CLAUDE.md 作用域）；父代理只拿到子代理的输出文本，证据取舍由父决定。
- **Gemini CLI subagents**：子代理 + 工具隔离（独立工具集/MCP/上下文），`@agent` 显式委派。
- **OpenHands**：microagents（注册表式“按场景注入的指令上下文”）+ agent delegation，结果经事件回传。
- **Magentic-One**：Orchestrator 是唯一握任务全貌者，维护 **progress ledger（进度账本）** 并向 worker 派发**子任务卡**；worker 只看到自己任务所需上下文，结果写回 ledger。
- **Factory（droid）**：droids = 技能/上下文包，装配后获得与目标匹配的最小上下文。
- 学术侧：**CoALA**（Cognitive Architectures for Language Agents）把这种“决策循环读外部记忆/上下文、只取相关片段”形式化为 retrieval 过程。

**Seelex 对照**：`ContextSnapshot`（Goal/Decision/Finding/Constraint/PendingWork 分字段）→ Compactor（预算）→ 子 Session 注入 → `merger.MergeBack` 回投主会话 mailbox —— 机制上同类、结构上比“纯文本摘要回传”更严谨（harness 文档 3.7）。业界大多数子代理回传是“自由文本/事件”，Seelex 是“字段化 DTO + 预算 + 去重合并”，是差异化点。

### D. 目标期内的窗口维持：压缩、摘要与结果外化

目标进行中窗口会触顶，业界按“目标还能不能续跑”决定何时压、压什么：

- **Claude Code auto-compact**：提前到 **~64-75% 占用 + completion buffer（为当前任务收尾预留）** 触发；把窗口外历史**不可逆摘要**，保证“当前任务能继续”。
- **OpenHands**：`LLMSummarizingCondenser`——超 `max_context_length` 时**保留 `keep_first`（任务级初始事件）**，中间滚动摘要（OpenHands 论文架构 EventStream）。
- **Gemini CLI**：自动压缩摘要。
- **ACON（arXiv 2510.00615）**：面向 long-horizon agent 的上下文压缩优化——按目标相关性取舍，而非机械截断。
- **The Complexity Trap（arXiv 2508.21433）**：观察 masking（不摘要、直接省去）与 LLM 摘要效果相当 —— 反摘要派证据，支持**结果外化/截断**优先于花式摘要。
- 结果外化普遍化：超大工具输出截断 + 提示；Seelex 用 `result_ref`（>20000 字符归档、模型只见省略标记 + 按需 `read_tool_result`）。

**Seelex 对照**：滑动窗口（软 75% / 硬 90% / 目标 60%）+ 三级压缩 + **压缩帧可逆**（`read_compressed_turn` + TurnArchiver；主流几乎都不可逆）+ 压缩 DAG（两章节 Summary + 前缀重放）——阈值与“提前触发”对齐 Claude Code，可逆性为业界差异强项；**缺 completion buffer**（压缩时无收尾预留，harness 文档 3.2 已列）。

### E. 目标导向的记忆选择与检索（goal as query）

目标之外、跨会话的知识靠“**以当前目标为查询，从外部记忆中取回相关片段注入窗口**”：

- **CoALA**：working（=上下文窗口）/ episodic（经验）/ semantic（事实）三分 + retrieval 算法按当前目标/状态从 episodic+semantic 取回、注入 working。
- **LangGraph + LangMem**：semantic / episodic / procedural 三类长期记忆 + 反思（reflection）/ 巩固（consolidation）自动写入；任何存储后端可用。
- **记忆服务**：Mem0 / Zep / Letta（MemGPT，记忆分层 + 自我编辑）等把“哪些历史该进上下文”做成服务。
- **代码侧检索注入**：Cursor 代码库 embedding 索引、Augment Context Engine、Aider repo map（token 预算固定注入、tree-sitter 层级符号）。
- **Episodic Memory in Agentic Frameworks（arXiv 2511.17775）**：直接用“下一步建议”形式消费情景记忆。

**Seelex 对照**：`seelexctx/memory` 的 `Select` = 以**当前查询（目标文本）**从压缩帧取 top-K、渲染有界“相关记忆”块 —— 已是 goal-conditioned retrieval 雏形，但为**词法匹配 top-K**，无 embedding/语义检索，也**未跨会话落库**（仅帧内）。

### F. 目标终态与账本：上下文如何收口与续用

目标完成/失败时，上下文要可审计地收口，成果可被下一目标复用：

- **Magentic-One**：Orchestrator 依据 ledger 判定 **done / terminate**，决定是否换策略或宣告目标终结。
- **A2A（Google → Linux Foundation）**：Task 状态机 + Message/Artifact 交换——**agent 之间只交换任务与产物，不交换私有上下文**（协议层把“上下文治理”留在各 agent 内部，杜绝上下文泄漏）。
- **Claude Code**：Task Budgets / effort 档位约束单目标消耗；checkpoint//rewind 提供目标级回滚。
- **Seelex**：goal skill 明确定义三种逃逸（achieved / degraded / timeout）+ `task_complete`/`task_failed`/`task_needs_user_decision` 终态工具 + ContextSnapshot.PendingWork 支持目标间续跑 —— 与 Magentic-One ledger 的“目标达成判定”同构，且更产品化。

---

## 3. 机制 × 产品/框架对照

| | A 隔离 | B 静态注入 | C 章程承袭 | D 压缩/外化 | E 检索/记忆 | F 终态 |
|---|---|---|---|---|---|---|
| Claude Code | plan mode/subagents | CLAUDE.md 三级 | subagent 独立上下文+回传文本 | auto-compact 64-75%+buffer | memory tool（可选） | Task Budgets/checkpoint |
| Gemini CLI | subagents | GEMINI.md | subagent+工具隔离 | 自动压缩 | — | — |
| Codex | worktree/云端 agent | custom instructions | 后台 agent | — | — | session resume |
| Cursor | worktree+VM | rules/memories | background agents | — | **embedding 索引** | PR 合并 |
| OpenHands | Docker runtime | microagents | delegation+事件回传 | **LLMSummarizingCondenser**（keep_first） | — | EventStream 回放 |
| Magentic-One | 独立 worker session | — | **子任务卡 + progress ledger** | 上下文截断 | — | Orchestrator done/terminate |
| LangGraph/LangMem | per-thread state | — | handoff/state | checkpoint/state | **semantic/episodic/procedural** | graph 终态 |
| A2A 协议 | — | — | Task/Message/Artifact（不泄上下文） | — | — | **Task 状态机** |
| **Seelex** | WorkPlan DAG/子会话隔离 | ProjectKnowledge（自动） | **ContextSnapshot 字段化承袭+MergeBack** | **可逆压缩 DAG+result_ref** | **goal-conditioned top-K（词法）** | **终态工具+逃逸语义** |

---

## 4. 值得追踪的文献线索

- **CoALA**：Sumers et al., *Cognitive Architectures for Language Agents*（2023）——记忆三分 + retrieval 形式化框架（现状基准）。
- **Magentic-One**：arXiv 2411.04468——多代理系统，Orchestrator + ledger + 子任务卡。
- **OpenHands**：All Hands AI 论文与文档——EventStream + LLMSummarizingCondenser（condensation 保留任务级初始事件）。
- **ACON**：arXiv 2510.00615——long-horizon agent 上下文压缩优化。
- **The Complexity Trap**：arXiv 2508.21433——观察 masking ≈ LLM 摘要（对“重摘要”路线的重要反证）。
- **Episodic Memory in Agentic Frameworks**：arXiv 2511.17775——情景记忆 → 下一步任务建议。
- 中文工程文：掘金《给 AI Agent 管上下文：压缩、即时检索和任务隔离》（2025-09 前后）——社区口径与本文三条机制（C/D/E）基本一致。

---

## 5. 对 Seelex goal 的对照与差距清单

**已对齐/领先（仓库证据）**：
1. 父证据预算化字段化注入 + MergeBack（`seelexctx/snapshot`+`merger`、`seelebridge/plan` 3.7）——业界多数为自由文本回传；
2. 可逆压缩（`seelexctx/compactor`+`controller`+`read_compressed_turn`、`docs/arch/context-prefix-chain.md`）——主流不可逆；
3. goal-conditioned 记忆 top-K（`seelexctx/memory` `Select`）；
4. 任务终态协议 + 三种逃逸（`seelebridge/task_terminal.go`、`plugins/default/goal/SKILL.md`）；
5. plan 工具族随 goal skill 激活门控、goal 不受 MaxLoops 限制（`seelebridge/tools/policy.go`、`application/core/input.go`）。

**差距（建议路线，均已有内部文档指认）**：
1. **B 类记忆文件惯例**：不消费 AGENTS.md / CLAUDE.md / .clinerules（harness 3.3“不消费惯例文件”）；`ProjectKnowledge` 是自动构建，但无“人工惯例文件优先”融合层。
2. **goal 级成果未跨会话落库**：ContextSnapshot 的 Decision/Finding/Lesson 字段可复用但未持久化（harness 3.3“无跨会话事实/决策/教训持久化”）→ 目标结束后无法被下一目标按语义取回。
3. **completion buffer**：压缩触发无“当前目标收尾”预留（harness 3.2）。
4. **语义检索**：memory.Select 词法 top-K，无 embedding/代码索引（harness 3.2“检索式上下文是主要短板”）。
5. **hooks / plan-mode 全局降权**：无 Pre/PostToolUse 钩子与“plan 期间整体降权”语义（harness 3.4）。
6. **子代理 merge-back 可见性**：主会话同轮语义与用户可见性仍需验证（harness 3.7）。

---

## 6. 来源

- 仓库既有调研：`docs/research/coding-agent-harness-comparison.md`；`docs/research/agent-market-research-2026-08.md`；`docs/arch/context-prefix-chain.md`；`seelexctx/README.md`。
- arXiv：2411.04468（Magentic-One）；2510.00615（ACON）；2508.21433（Complexity Trap）；2511.17775（Episodic Memory in Agentic Frameworks）。
- CoALA / awesome-language-agents（Sumers et al. 认知架构综述聚合）。
- 厂商公开资料：Claude Code 官方文档（code.claude.com，经二手转述交叉）、OpenHands 文档与仓库、LangGraph/LangMem 文档、Cursor/Codex/Gemini CLI 公开资料。
- 中文工程资料：掘金《给 AI Agent 管上下文：压缩、即时检索和任务隔离》；掘金《CLAUDE.md、Hooks、Skills、Subagents 该用哪个》。
