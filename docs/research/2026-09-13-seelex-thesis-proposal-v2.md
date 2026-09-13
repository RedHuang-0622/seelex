# 开题报告（修订版 v2）

## 面向软件工程任务的开源 Coding Agent Harness 设计与实现
### ——以 Seelex 项目为例

| 项目 | 内容 |
|---|---|
| 课题名称 | 面向软件工程任务的开源 Coding Agent Harness 设计与实现——以 Seelex 项目为例 |
| 课题类型 | 应用研究与工程实现（软件工程 / 人工智能交叉） |
| 本版日期 | 2026-09-13 |
| 前版 | `docs/research/2026-09-11-seelex-thesis-proposal.md`（本版为其修订与扩展，结论以前版为基线，增量见第零章） |
| 研究基线 | Seelex `Developer Alpha`（Go **1.25.8**，依赖 Seele **v0.1.3**；**746 个 `.go` 文件 / 124,993 行**，其中测试 **380 文件 / 60,589 行**；**87 个 Go 包**；**1,559 个 Test/Benchmark/Fuzz 函数**；`go.mod:9`） |
| 预期周期 | 2026-09 — 2027-06（10 个月，见第七章） |

---

## 零、本版修订说明（相对 2026-09-11 版的增量）

### 0.1 为什么要修订

前版把研究内容收敛为八条，参数多取自早期设计文档。2026-09-13 对全部模块做了一次**代码级功能盘点与参数核对**（四个并行只读调研域：Agent 运行时/编排、上下文与持久化、工具生态与前端、产品与能力缺口），得到两类结论：

1. **发现了大量前版未覆盖的已实现能力**（合计 60+ 项，见附录 C），其中多项具备独立研究价值：压缩过程本身被表达为 DAG、父子 Agent 的**前缀重放摘要协议**、**目标治理与裁决闭环**（goal 栈 + ADVISOR 双会话 + 终态门禁）、**代理团队工厂与工作台投影**、**顺序日志作为单一事实源**、**桌面操控工具面**、**皮肤包与组件库分层**等。
2. **发现前版三处参数与代码不一致**（详见 0.3），必须更正后才能作为验收依据。

### 0.2 本版结构增量

| 章节 | 增量 |
|---|---|
| 第三章 研究内容 | 由 8 条扩展为 **12 条**（新增记忆体系、目标治理与裁决、代理团队与工作台、恢复与存活） |
| 第四章 创新点 | 由 5 个扩展为 **6 个**（新增"治理闭环"与"压缩即 DAG + 前缀可重放摘要"） |
| 第五章 技术方案 | 新增**参数实现核对表**（每个阈值均标注代码行号） |
| 附录 A | 新增：阈值参数 → 代码出处 → 实现状态 |
| 附录 B | 新增：可量化基线与可复现性分级（🟢本机可复现 / 🟡仓库文档口径 / 🔴需真实模型） |
| 附录 C | 新增：四域功能盘点（60+ 项，本次探索的直接产出） |
| 附录 D | 新增：工程成果与简历/成果映射 |
| 附录 E | 新增：与前版的差异与更正记录 |

### 0.3 参数更正记录（前版 → 本版）

| 前版表述 | 代码实况 | 证据 |
|---|---|---|
| 工具结果归档阈值 **20000 字符** | 实为 **60000 字符**（可配 `limits.max_tool_result_chars`）；20000 仅存在于过期注释 | `seelexctx/limits.go:134` |
| 并行分支"**默认最大并发 3**" | 并非硬编码默认：`PolicyConcurrency` 显式配置为空时**随节点数增长**；"3"来自 Effort `high` 档在提示层的声明 | `seelebridge/plan/policy.go:56-66`、`seelebridge/plan/tool_provider.go:297`、`docs/feature-instrumentation.md` |
| 12.5% 安全余量、75%/90%/60%、clamp(4,40)、ratio 0.7 | **全部属实，已定位到代码** | `seelexctx/controller.go:57,70,73,76`、`seelexctx/window.go:38` |
| 依赖 Seele v0.1.2 或 v0.1.1 | 实为 **v0.1.3**（README/CHANGELOG 未同步） | `go.mod:9` |
| 内置 Plugin 6 个 | 磁盘仅 **2 个**（`default`、`freecad`），Skill **17 个** | `plugins/default/plugin.md`、`plugins/freecad/plugin.md` |

---

## 一、选题背景与研究意义

### 1.1 研究背景

**（1）从"生成代码"到"完成工程任务"的范式迁移**

自 Chain-of-Thought Prompting [1] 证明大语言模型（LLM）具备可被显式引导的多步推理能力以来，LLM 在软件工程中的定位经历了两次跃迁：从"语法补全"到"意图级代码生成"，再从"生成一段代码"到"自主完成一项工程任务"。ReAct [2] 以"推理—行动—观察"交错闭环把 LLM 的输出从"答案"变为"动作"；Toolformer [4]、ToolLLM [5]、CodeAct [6] 与 SWE-agent [11] 进一步把工具调用、可执行代码与编译器/测试/终端纳入动作空间。SWE-bench [10] 则把评测对象换成真实仓库的 issue 修复——其成败不取决于单次生成质量，而取决于**模型之外的一整套执行基础设施**。

**（2）Harness 成为决定成败的关键变量**

工程实践逐渐形成共识：模型能力是上游变量，**包裹模型的执行框架（Agent Harness / Agentic Runtime）才是产品能力的实际边界**。一个可用的编码智能体必须回答七个问题（前版为六个，本版新增第七个）：

1. 模型在什么时机可以看到哪些工具？
2. 文件与 Shell 操作如何被限制在合法工作区内？
3. 长任务如何被拆分、并行执行并把结果合并回主会话？
4. 上下文超出预算时，哪些内容保留、压缩或按需读回？
5. 会话、计划与工具结果如何在进程重启后可靠恢复？
6. 多个模型、账号、插件与外部工具如何在运行时安全切换？
7. **（新增）"任务已经完成"这一判断由谁做出、如何被审计、如何避免模型自我宣告？**

**（3）当前主流方案的结构性矛盾**

- **上下文不可逆**：多数框架压缩后只留摘要，被压缩的原始证据不可读回，模型无法回溯"自己此前看到了什么"，压缩本身成为不可审计的黑箱 [13][15]。
- **压缩过程不可解释**：压缩被实现为一次函数调用而非可观察的生命周期，无法回答"这一段摘要由哪些输入、哪次模型调用、是否降级产出"。
- **多智能体编排缺乏证据契约**：多智能体研究 [7][8][9][16] 证明协作有增益，但父子智能体之间继承什么、回传什么、如何去重与溯源，多数实现停留在"把父对话复制一份"的工程直觉层面。
- **"完成"缺少外部裁决**：模型自述完成即结束，缺少独立角色的裁决与缺席（超时/限流）时的降级规则。
- **安全边界单薄**：或依赖提示词约定，或只给粗粒度沙箱，缺少"物理路径边界"与"策略审批边界"的分层。
- **评测与生产割裂**：以真实 LLM 与网络为中心的测试不可复现，CI 无法为 Tool Calling、审批与编排路径提供回归保护。
- **闭源与模型绑定**：主流商业方案难以复现内部机制，也难以满足数据主权与合规审计要求。

Seelex 是针对上述矛盾的工程回应：一个开源、本地优先、多模型可路由、上下文可逆、目标可裁决的 Coding Agent Harness。截至本开题，项目已完成从运行时原语到产品前端的贯通实现，但仍处于 Developer Alpha：跨进程 Agent 间协议尚未实现、缺少标准化编码基准结果、前端交互测试覆盖率偏低（TUI 覆盖率低于核心编排层）。把这些"已知缺口"转化为可验证的研究问题，正是本课题的起点。

### 1.2 研究意义

**理论意义**

1. **把上下文从工程直觉提升为可计算的资源模型**。以"预算驱动的流水线"（组装 → 工具结果处理 → 压缩 → 控制器）建模上下文，给出可复算的预算公式、三阈值与窗口推导规则（附录 A），并进一步把**压缩过程本身**建模为一张可回放、可降级的 DAG，使"上下文够不够、压缩得对不对"从经验判断变为可测量、可回归的工程量。
2. **为父子智能体的证据传递建立结构化契约**。把父子 Agent 的上下文传递建模为"前缀重放摘要 + 有界记忆块 + 结构化合并"三件套，明确 finding / decision / constraint / progress 在合并时的差异语义，使多智能体协作从"复制对话"走向可校验的状态演化。
3. **为"任务完成"提供可审计的外部裁决模型**。把目标建模为会话级 LIFO 目标栈，引入独立上下文的裁决角色（ADVISOR/TechLeader）与缺席矩阵（超时/限流时的判负与转人工规则），使"完成"从模型自述变为带审计轨迹的三方裁决。
4. **为智能体执行提供事件投影式的可审计模型**。以"快照为权威状态、事件为增量同步"描述可观测性，并以 append-only 顺序日志作为单一事实源，使每次工具调用、审批与计划节点状态迁移都可被外部重放与审计。

**工程与实践意义**

5. **给出可运行、可替换、可测试的开源参考实现**。以端口—适配器架构隔离"模型如何执行"与"产品如何解释执行结果"，使上游运行时、应用语义层与前端可独立演进。
6. **回应数据主权与成本可控的真实需求**。本地优先部署、项目级路径约束、逐操作人工审批、多账号池路由与 token 成本可归集。
7. **沉淀可复用的验证方法论**。主张测试 Harness 与生产 Harness 共用同一套应用契约，用确定性脚本引擎替代真实 LLM 构造端到端旅程，使编排、审批、协议与压缩路径可在无网络、无密钥的 CI 中回归。

---

## 二、国内外研究现状

> 前版已给出 27 篇文献的完整综述（英文 18 篇 / 中文 9 篇，2021—2026 年），本版不再重复正文，保留第四章的引用编号与第九章的文献表。以下仅补充本版新增的对标结论。

**（1）推理与行动范式**。CoT [1] → ReAct [2] → Reflexion [3] 构成"推理—行动—反思"的演进主线，成为编码智能体的默认骨架。

**（2）工具使用与动作空间**。Toolformer [4]、ToolLLM [5]、CodeAct [6]；SWE-agent [11] 指出"智能体—计算机接口"本身是性能变量。

**（3）多智能体协作**。Generative Agents [7]、AutoGen [8]、MetaGPT [9]；综述 [16] 指出通信开销、错误传播与状态一致性仍是开放问题。

**（4）上下文工程与记忆**。`Lost in the Middle`[13] 的位置偏差为"滑动窗口 + 优先最近轮次"提供实证；MemGPT [14] 以虚拟内存隐喻做分层记忆；上下文工程综述 [15] 把上下文扩展为包含文本、环境状态、工具结果与记忆的统一对象。

**（5）软件工程智能体与评测**。SWE-bench [10] 确立真实仓库 issue 修复范式；综述 [12] 指出"缺乏过程可审计性与端到端可复现性"是共性短板。

**（6）协议与互操作生态**。MCP [17] 解决"智能体—工具"，A2A [18] 解决"智能体—智能体"。

**（7）本版新增：与外部 Harness 的机制对标结论**（来源：`docs/research/2026-09-12-external-harness-comparison.md`、`coding-agent-harness-comparison.md`）

- **方向已收敛**：append-only 日志作为单一事实源，Seelex 与 Codex / OpenHands 等实现已趋同（`:34`）。
- **压缩已从"一次调用"变为"生命周期"**：Seelex 的窗口外帧接近 Codex 的非摘要式处理；**工具结果外化 + 按需读回**在同类实现中少见（可比者仅 Gemini 的 50k 级外化）（`:38`）。
- **Seelex 的差异化强项是压缩可逆性**（主流基本不可逆）（`coding-agent-harness-comparison.md:78`）。
- **已确认的能力缺口**（作为本课题的研究边界声明）：无跨会话长期记忆、无"judged goal loop"、无 hooks 体系、无 completion buffer（`2026-09-12-external-harness-comparison.md:45,56,69,73`）。

**（8）研究述评与本课题切入点**

前版归纳的五个未解决问题仍然成立（压缩不可逆、编排缺证据契约、安全边界层次不足、测试与生产割裂、闭源与模型绑定）。本版据（7）追加两项：

6. **"完成判定"缺少外部裁决与缺席降级规则**——模型自述即完成，缺少独立角色的裁决、超时/限流下的判负规则与可审计轨迹。
7. **压缩过程本身不可观察**——无法回答某段摘要是由哪些输入、哪次调用、是否降级产出，导致压缩质量无法度量，也无法把"压缩正确性"变成回归测试。

**本课题定位**：不做模型训练、不做云端执行环境、不做 IDE 插件正面竞争，而聚焦"**Harness 层的治理能力**"——把可逆上下文、可观察压缩、结构化多智能体编排、目标裁决闭环、双层安全边界、事务式扩展系统与事件投影可观测性，做成一个可运行、可测试、可替换、可自托管的开源实现，并以 Seelex 为工程载体交付与验证。

---

## 三、研究目标与研究内容

### 3.1 研究目标

设计并实现一个面向软件工程任务的开源 Coding Agent Harness，使其在**单进程本地运行**的前提下同时具备八项能力：① 项目作用域的工具调用与逐操作审批；② 按需加载的多智能体工作流编排；③ 预算驱动且**可逆、可观察**的上下文治理；④ 分层记忆与按需检索；⑤ **可裁决、可审计的"完成"模型**；⑥ 事务式的插件/Skill/MCP 扩展切换；⑦ 可恢复、可审计的持久化与可观测协议；⑧ 可离线、确定性复现的验证体系。最终以可运行的 TUI 与桌面 GUI 双前端，以及三平台 CI 通过的工程基线作为交付证据。

### 3.2 研究内容（12 条）

**内容一：分层架构与端口化边界设计**
建立"运行时原语层（Seele）—防腐适配层（seelebridge）—应用语义层（application）—前端（TUI/GUI）"四层结构。应用层以消费方定义的端口声明依赖（`application/contract` 现有 **13 个接口 / 71 个契约类型**），上游类型变化被限制在防腐层内，前端只消费应用层 DTO、Snapshot 与 Event。

**内容二：双执行路径（ReAct 默认 + 可选 DAG 编排）**
不强制所有请求先生成计划：简单任务直接进入主 ReAct 闭环；复杂任务由模型按需加载 DAG（JSON Schema 校验 + 字段归一化 + 节点/边/拓扑校验 + 环检测 + 拓扑排序 + Effort 策略约束）。研究重点为"何时值得规划"的成本—收益边界，以及校验失败时的降级行为。

**内容三：预算驱动的可逆上下文流水线（本课题核心）**
`组装器 → 工具结果处理器 → 压缩器 → 上下文控制器` 四段组件链：

- 预算公式：`Budget = window − outputReserve − safetyReserve`，其中 `safetyReserve = window / 8`（12.5%）（`seelexctx/controller.go:57,62`）；
- 三阈值：软阈 75% / 硬阈 90% / 压缩目标 60%（`controller.go:70,73,76`）；超硬阈先归档超大工具结果再收缩窗口（`controller.go:349`）；
- 窗口推导：`N = clamp((ContextTokens × Ratio − Reserved) ÷ AvgRoundTokens, MinRounds, MaxRounds)`，默认 `Ratio 0.7 / Min 4 / Max 40`（`seelexctx/window.go:31-38`）；
- 工具结果外化：单条结果超 **60000 字符**（可配）时归档为不可变引用 `result_ref`，模型先见有界摘要，需要时按页/按过滤条件读回；错误结果原样透传不做截断（`limits.go:134`、`seelexctx/processor.go:100,133,144,160`）；
- **压缩即 DAG**：压缩过程以 Seele workplan 表达，5 节点 `select_range → {chapter1_anchor, chapter2_thick} → merge_frame → publish_stack`，两分支并行、`chapter2` 一次重试后回退**本地确定性折叠**（`summary_source=local`，不消耗模型 token）；装配失败时按依赖序串行兜底（`seelexctx/dag.go:33-58`）；
- **两章节压缩帧**：帧固定为 Chapter1 锚点句 + Chapter2 厚内容；模型只看到栈顶帧；`PushCompact` 维护"request 首尾同空/非空非倒置、`prev_segment_id` 必等于栈顶"的链不变量（`seelexctx/frame.go:8`、`sessionstore/session_context.go:743,763,784`）；
- **前缀重放摘要协议**：厚摘要复用**真实请求字节前缀** + 固定压缩指令生成，使摘要与 wire 前缀对齐；失败一次即本地折叠（`seelexctx/replay.go`、`docs/2026-09-06-compaction-dag/design.md`）。

**内容四：分层记忆与按需检索**
- 相关记忆块：按当前查询从压缩帧做词法 top-K 渲染有界记忆块（`limit=3`、recency 权重 `0.15`、上限 1024 token）（`seelexctx/memory/memory.go:47`、`block.go:16`）；
- 历史检索读回：压缩栈作为索引，命中帧范围后从事件流读回真实记录（`seelexctx/search/search.go:35-47`）；
- 跨会话记忆：memory 块按项目粒度跨会话装配，fork 复用同 workspace 的稳定前缀（`docs/arch/context-prefix-chain.md`）；
- CLI/项目级记忆：用户级与项目级 `MEMORY.md` 索引常驻、详情按需工具读写（索引 ≤20 条）（`docs/plan/cli-memory.md`、`docs/plan/memory-architecture.md`）。

**内容五：多智能体编排与父子证据承袭/合并**
每个 DAG 节点获得独立 Session、NodeScope、PromptBlocks、账号绑定与 token 预算；父证据在执行前注入（前缀重放摘要 + 有界记忆块），子节点产出经 `merger.MergeBack` 形成继承上下文块并经有界邮箱在安全边界注入主会话（`seelebridge/node/agent_node.go:44,228`）。研究重点为并行分支的状态隔离与合并结果的来源可追溯性。

**内容六：目标治理与"完成"裁决闭环（本版新增）**
- 会话级 **LIFO 目标栈**与状态机，栈深上限 16（`application/core/goal/record.go:21`、`controller.go:143`）；活栈投影落 sessionstore，形成**第五栈持久化**，重启后继续治理，终态弹栈即删除（`goal/sessionstore_store.go:53`）；
- **双会话裁决**：执行侧（EXEC）事件驱动裁决侧（ADVISOR / TechLeader）在**独立上下文**中回合制评审，裁决结果以 corr 指令回投执行侧（`goal/techleader.go:228`、`advisor.go:143`）；
- **抽帧节流**：非关键信号在评估窗口内抑制，关键信号立即评估（`DefaultEvalWindow = 3`）（`goal/a2a.go:33`）；
- **指令邮箱**：容量 32，满则丢最旧并计数，指令正文 ≤1200 rune（`goal/a2a.go:25`、`techleader.go:43`、`record.go:33`）；
- **缺席矩阵与终态门禁**：完成声明必须经裁决侧裁决，执行侧永不等待裁决侧；超时/限流按"判负或转人工"处理（`goal/gate.go:41`）；
- **审批预筛**：审批请求先经裁决侧预筛——低风险可代答，高风险转人工（`goal/gate.go:138`、`a2a.go:29`）；
- **生命周期审计**：状态机每次变更 append-only 记账，按会话隔离、可带跨会话出处（`goal/audit.go:62`）。

**内容七：代理团队工厂与工作台投影（本版新增）**
- **团队工厂**：`TeamSpec →` 角色会话 + 注册表 + 工作顺序，幂等键 `(team_id, role_name)`；定时角色不入顺序表（`application/core/agentteam/factory.go:43`、`spec.go:25`）；
- **preset 与隐式拉起**：创建目标时按 preset 自动装配 TechLeader 团队（`JoinPolicy=on_goal_create`），同工厂可承载多团队（`agentteam/presets.go:13,81`）；
- **子代理与团队边界**：子代理等同一次工具调用，不进成员表、不占顺序、不占发言权，仅经 fork 工具派发（`docs/2026-09-10-a2a-agentteam-recovery/README.md`）；
- **工作台行投影**：plan / tasklist / subagent / todo 四源统一投影为表格（行上限 200、证据截断 800 字符）（`application/core/work_table.go:29`、`seelexctx/limits.go:116,130`）；
- **traceboard**：节点打卡与阶段合成追踪点并分组截断（`plan_node_events=30`，已完成节点有界保留 50）（`work_table.go:179,122`、`limits.go:114`）；
- **增量汇聚**：`worktable.changed` 突发更新 latest-wins，关闭时排空尾态（`application/core/worktable/worktable_publisher.go:37`）；
- **Todo 三态 + Assignee**：仅 `pending/doing/done`；系统按 `role:sessionID` 被动把角色列入名单，上限 20 条（`work_table.go:651`、`limits.go:129`）。

**内容八：双层安全边界与人机协同审批**
第一层**物理边界**：文件与 Shell 工具先把目标解析为规范化绝对路径，验证仍在绑定 workspace root 内；第二层**策略边界**：合法范围内进一步计算 allow / ask / deny，默认 `manual`（白名单外审批），声明式 LMRW 规则支持模式匹配（如 `rm -rf /*`、fork bomb 直接 deny）（`config/seele.yaml`、`README.md` 关键技术决策 §5）。隐藏工具即使被模型构造出调用也必须在执行时被拒绝。

**内容九：事务式扩展系统与账号池路由**
插件定义为同时影响工具可见性、系统提示、Skill 与 MCP 的能力集合；切换按"准备 → 切换 → 清理"执行，任一步失败按逆序回滚（`plugin/apply.go:21`、`manager.go:94`）；热更新以 `DiffState` 计算差异并事务式应用（`plugin/apply.go:56`）；工具可见性以**请求级快照**注入运行时（`seelebridge/plugin/plugin.go:92`）。账号按角色（agent / subagent / goalplan / websearch）分组入池，分支用 `role + branchID` 确定性哈希选路，流式请求租约保持至 EOF。

**内容十：持久化、Snapshot/Event 与顺序日志**
- **顺序日志**：每会话一条物理 append-only JSONL 作为唯一事实源，`Ordinal` 单调，满足 I-LOG-1..5 不变量（`docs/2026-09-07-session-order-log/README.md:62,93`）；
- **分区与 head 发布**：按 `project_id → session_id` 分区，消息行按固定规模（100 行）分片追加，模块 head 仅在新数据完整写入后原子发布（`sessionstore/README.md:27,62,262`）；
- **三栈通道**：plan / task / goal 各自独立 JSONL + head（head 只装水位），批次弹栈为 LIFO，fork 时按 message 锚重建（`sessionstore/README.md:182,185`）；
- **结构性 EVENT 通道**：append-only 摘要、幂等只看 head 水位，正确的实现要求 `shardReads == 0`（`sessionstore/README.md:228,245`）；
- **快照权威 + 事件增量**：客户端先读完整 Snapshot，再消费带 sequence/revision 的有序 Event，缺口或不兼容即重载。

**内容十一：恢复、存活与中断一致性（本版新增）**
- **通用恢复七步模板**：定位 → 判定 → 补历史 → 重建 → 注入 → 同键重跑 → 收敛；按 Key 去重、在途跳过、已终态只补历史（`application/core/resume/resume.go:53,228,360`）；
- **子代理冷恢复续跑**：running/queued 残留会重建现场并同键续跑，恢复说明以 `role=system` 只进该子代理上下文（`seelebridge/runtime_subagent_resume.go:35`）；
- **中断轮截断**：残缺工具链保留为开放单元，装配时补齐合成占位，三处同构切分器、幂等（`docs/2026-09-08-interrupted-round-truncation/README.md:18,20`）；
- **恢复前缀一致性**：冷恢复尾窗乱序时以前缀校验重建，超预算显式拒绝（`ErrProviderContextBudgetExceeded`）（`docs/2026-09-08-context-restore-review/README.md:35,240`）；
- **存活**：Snapshot 为纯内存复制；子代理回流经有界邮箱在锁外 drain，满则丢弃并计诊断（`docs/arch/session-snapshot-liveness.md`）；
- **并发模型**：同会话串行、跨会话并行（per-session 过渡锁 + FIFO actor，`application/core/session_runtime/transition_manager.go:24,29`）；驻留引擎超限驱逐（`ResidentSessionLimit=6`）、replan 并发 2 / 每窗口 6（`seelexctx/limits.go:108,109,112`、`application/core/resident_lru.go:62`）。

**内容十二：确定性验证体系与性能/成本基线**
以脚本化引擎、固定装置、事件记录器与同一套应用端口构造不依赖真实 LLM、网络与密钥的端到端旅程；覆盖提交、工具生命周期、审批、快照/事件、压缩与最终可观察结果。同时建立基于真实 TUI 全链路的性能与 token 成本基线（附录 B）。

---

## 四、拟解决的关键问题与创新点

### 4.1 拟解决的关键问题

**问题一：上下文预算与信息保真的平衡。** 在"必须压缩"与"不能丢失可回溯证据"之间建立确定边界：压缩只发生在窗口之外、目标为预算的 60%、原始内容保留可逆寻址。需给出**压缩 DAG 的验收判据**（前缀重放是否与 wire 字节对齐、`prev_segment_id` 链是否成立、降级比例是否可观测）与压缩失败时的显式降级与上报路径。

**问题二：并行多智能体的状态隔离与合并一致性。** 多分支并行时对会话上下文、项目知识与账号租约的读写必须互不串台；合并需对四类语义字段差异化处理并保留来源标识，避免"最后写入者获胜"。以并发评审与竞态测试作为验证手段。

**问题三：安全边界的可证明性。** 论证"路径规范化 + 根目录包含性检查"能抵抗符号链接、相对路径与工作目录切换造成的逃逸；论证策略层与物理层职责不可互相替代；保证工具可见性过滤在请求级生效。

**问题四：可审计性与过程可解释性。** 把"用户看到的结果"与"运行时发生的事实"分离保存：可见对话、provider 历史、追加式执行事件、计划状态与不可变工具结果各自独立；给出计划节点级与压缩帧级的事件投影方案。

**问题五：完成判定的可裁决性（本版新增）。** 需要定义"完成"的三方裁决语义（执行侧声明、裁决侧裁决、缺席时的降级规则），并证明该闭环不会把执行侧拖死（执行侧永不等待裁决侧）、不会因裁决侧限流而无限挂起（429/超时 → 判负或转人工），且每次状态变更都有 append-only 审计。

**问题六（诚实标注的开放问题）：跨进程互操作与语义检索。** 当前编排为单进程内多节点会话，尚未实现跨进程/跨组织协议化互操作；压缩历史的检索为"已知引用/词法命中才能读回"的拉取式，缺少语义检索入口（`docs/feature-instrumentation.md:53`）。两项作为后续演进方向与边界声明，不在本期交付承诺内。

### 4.2 创新点（6 个）

**创新点一：可逆的上下文压缩与引用式工具结果外化。** 与主流"摘要替换原文"的不可逆压缩不同，压缩帧原始消息保留为可寻址对象，模型与审计者可事后按范围读回；超大工具结果以"不可变引用 + 有界摘要 + 按页/过滤读回"三段式取代简单截断。

**创新点二：压缩即 DAG（本版新增）。** 把压缩本身表达为 5 节点工作流 DAG（两分支并行 + 重试 + 本地确定性折叠兜底），并以"前缀重放摘要协议"让摘要与真实请求字节前缀对齐。由此压缩过程可观察（节点状态、`summary_source=replay|local`）、可降级、可回归，压缩正确性首次具备不变量级验收基线。

**创新点三：以结构化快照为契约的父子智能体证据承袭与合并。** 父子 Agent 的传递从"复制对话"改为"前缀重放摘要 + 有界记忆块 + 语义合并"，并明确四类字段的差异化合并语义。

**创新点四：物理边界与策略边界分离的双层安全模型。** 将"能否逃出项目目录"与"项目内哪些操作仍需人工确认"拆分为两个不可互相替代的边界，配合请求级工具可见性快照。

**创新点五：可裁决、可审计的"完成"模型（本版新增）。** 会话级 LIFO 目标栈 + 独立上下文的裁决角色 + 抽帧节流 + 有界指令邮箱 + 缺席矩阵 + append-only 审计，把"完成"从模型自述变为带可追溯轨迹的三方裁决，并显式定义限流/超时下的降级规则。

**创新点六：测试 Harness 与生产 Harness 共用契约的确定性验证方法，以及事务式能力切换运行时模型。**

---

## 五、研究方案与技术路线

### 5.1 总体架构

```text
┌────────────────────── 客户端层 ──────────────────────┐
│  TUI (Bubble Tea)              GUI (Wails / WebView) │
│  Plan 分级视图 · 时间轴 minimap · 组件库 + 皮肤包     │
└──────────────────────────┬───────────────────────────┘
                           │ Snapshot / Event / Action
┌──────────────────────────▼───────────────────────────┐
│ application/ 应用语义层                              │
│ Chat · Task · Plan · Goal/Govern · AgentTeam         │
│ Worktable · Approval · Session · Workspace · Resume  │
└───────┬──────────────────┬───────────────────┬───────┘
        │                  │                   │
┌───────▼────────┐ ┌───────▼─────────┐ ┌───────▼───────┐
│ seelebridge/   │ │ seelexctx/      │ │ sessionstore/ │
│ Runtime·Tools  │ │ Assemble·Compact│ │ 顺序日志      │
│ Plan·Account   │ │ DAG·Frame·Merge │ │ 分区 + head   │
│ MCP·PathGate   │ │ Memory·Search   │ │ 三栈通道      │
└───────┬────────┘ └───────┬─────────┘ └───────┬───────┘
        │                  │                   │
┌───────▼──────────────────▼───────────────────▼───────┐
│ Seele 运行时：agent · session · tools · workplan ·   │
│               accountpool · mcp                      │
└──────────────────────────────────────────────────────┘
支撑模块：plugin/ · skill/ · workspace/ · mcpstack/ · e2e/
```

依赖方向严格单向：前端只依赖应用层 DTO/Event；应用层通过消费方定义的端口依赖下层；上游运行时类型只允许出现在防腐层内。

### 5.2 关键技术方案：参数实现核对表

> 本表每个阈值均已在本机代码中定位（2026-09-13 核对）。**这是本版相对前版最重要的可验证性提升**：研究报告中的参数不再来自设计文档，而来自可执行代码。

| 域 | 参数 | 取值 | 代码出处 |
|---|---|---|---|
| 上下文预算 | 安全余量 | `window / 8`（=12.5%） | `seelexctx/controller.go:57` |
| 上下文预算 | 预算公式 | `window − outputReserve − safetyReserve` | `seelexctx/controller.go:62` |
| 压缩阈值 | 软阈 / 硬阈 / 压缩目标 | 75% / 90% / 60% × Budget | `seelexctx/controller.go:70,73,76` |
| 窗口推导 | Ratio / MinRounds / MaxRounds | 0.7 / 4 / 40 | `seelexctx/window.go:38` |
| 工具结果 | 归档阈值 | 60000 字符（可配） | `seelexctx/limits.go:134` |
| 证据与摘要 | EvidenceChars / SummaryChars | 800 / 800 | `seelexctx/limits.go:116,128` |
| 历史与工作台 | HistoryWindow / WorkTableRows | 200 / 200 | `seelexctx/limits.go:113,130` |
| 计划投影 | PlanNodeEvents | 30 | `seelexctx/limits.go:114` |
| Todo | TodoMaxItems | 20 | `seelexctx/limits.go:129` |
| 驻留与重规划 | ResidentSessionLimit / MaxConcurrentReplans / MaxReplansPerWindow | 6 / 2 / 6 | `seelexctx/limits.go:108,109,112` |
| 压缩 DAG | 节点数 / 分支 / 重试 / 兜底 | 5 节点 / 2 分支 / 1 次 / 本地折叠 | `seelexctx/dag.go:33-58` |
| Effort lite | MaxLoops / MaxToolCalls / 无进展轮次 | 15 / 30 / 6 | `application/prompt/effort.go:48,50` |
| Effort medium | 同上 + 计划约束 | 48 / 96 / 10 + MaxNodes 4 且强制串行 | `effort.go:54,58,59` |
| Effort high | 同上 + 节点循环 | 384 / 768 / 24，MaxNodeLoops 48 | `effort.go:63,66,67` |
| Effort max | 同上 + 节点循环 | 768 / 1536 / 48，MaxNodeLoops 96 | `effort.go:71,72,73` |
| 计划并发 | MaxForkConcurrency | 显式配置优先；否则随节点数增长 | `seelebridge/plan/policy.go:56-66` |
| 目标治理 | MaxStackDepth / DefaultEvalWindow / MaxDirectiveQueue | 16 / 3 / 32 | `goal/record.go:21`、`goal/a2a.go:33,25` |
| 子代理 | fork 超时默认 | 2 小时（7200s） | `seelebridge/fork/tool.go:107` |
| 工具（桌面） | 截图默认宽度 / 点击间隔 | 1600 / 60ms | `seelebridge/tools/computer/mcp/main.go:30`、`computer/click.go` |
| 搜索 | 内置 provider | tavily / bochaai / searxng | `seelebridge/search/builtin*.go` |
| MCP | breaker 事件通道 | cap 64 | `seelebridge/mcp/mcp.go:51` |
| 前端 | 皮肤 / 协议 | 3 套（graphite/verdigris/paper）/ protocol v1 | `gui/frontend/dist/themes/manifest.json`、`docs/gui/modules/application-protocol.md:27` |

### 5.3 实验与验证方案

| 验证维度 | 验证方法 | 判定依据（可复现命令/口径） |
|---|---|---|
| 功能正确性 | `gofmt -l .`、`go build ./...`、`go vet ./...`、`go test ./... -count=1` | 全绿；87 包、1,559 个测试函数 |
| 编排正确性 | 确定性 E2E 场景（脚本化引擎，无真实 LLM） | 提交、工具生命周期、审批、快照/事件、终态结果符合预期 |
| 压缩正确性 | 压缩 DAG 单测 + `PushCompact` 链不变量 + `ContextCacheDivergenceProbe` | `prev_segment_id` 恒等于栈顶；前缀命中率不低于控制组 |
| 上下文治理 | 预算/窗口/阈值单测 + 压缩前后估算记录 + 尾窗读放大测量 | 窗口符合公式；尾窗读覆盖全量读语义 |
| 并发与隔离 | 竞态测试（`-race`）+ 并发会话隔离用例 + 并行计划评审 | 无数据竞争；分支上下文与账号租约互不串台 |
| 目标裁决 | goal 栈 LIFO、抽帧窗口、指令邮箱满丢最旧、缺席矩阵用例 | 终态门禁不可绕过；执行侧不等待裁决侧；审计可回放 |
| 安全边界 | 路径逃逸用例集（相对路径/符号链接/切换工作目录）+ 隐藏工具构造调用 | 全部被拒绝；审批链可解释 |
| 持久化与恢复 | 顺序日志不变量、head 原子发布、冷恢复、中断轮截断用例 | 读者只见完整水位；恢复后前缀一致 |
| 性能与成本 | 真实 TUI 全链路基准（同沙箱/同模型/同口径）+ 会话存储 pprof 对照 | 延迟分位、吞吐、成功率、token 成本可复现（附录 B） |
| 工程基线 | Windows / Linux / macOS 三平台 CI（含 race、覆盖率产物、漏洞扫描） | 三平台构建与测试全绿 |

---

## 六、可行性分析

**（1）技术可行性。** 项目已具备可运行基线：四层架构、上下文域、持久化、目标治理、代理团队、工作台、双层安全边界与三平台 CI 均已落地。本课题的增量工作是在既有基线上"补齐缺口 + 强化验证"，而非从零构建。

**（2）工程可行性。** 技术栈（Go 1.25.8、Bubble Tea、Wails、JSON 持久化）成熟稳定；构建与测试命令短小可复现；仓库无 vendor 第三方代码。

**（3）条件可行性。** 开发仅需普通开发机与一个 OpenAI 兼容端点；离线验证路径不依赖真实模型与网络；真实模型冒烟测试显式启用，成本可控。

**（4）风险与对策。**

| 风险 | 表现 | 对策 |
|---|---|---|
| 上下文跨模块一致性漂移 | 新旧两套窗口/阈值策略并存 | 收敛为单一上下文实现；过渡期加一致性测试护栏 |
| 参数口径漂移（本版已发现 2 处） | 设计文档参数与代码不符（20000 vs 60000、并发默认值） | 建立"参数实现核对表"（§5.2）并纳入文档更新规则：参数变更须同步测试与文档 |
| token 估算误差 | 保守估算在中文与长工具参数场景偏差大 | 引入 provider 用量反馈校准（EMA）；评估分词器接入 |
| 并行分支共享状态竞争 | 分支对同一状态读写互相干扰 | 父快照克隆 + 节点作用域隔离 + 竞态回归测试 |
| 裁决侧（ADVISOR）依赖网络 | 裁决请求被限流/超时会导致任务悬置 | 缺席矩阵：判负或转人工；执行侧永不等待裁决侧 |
| 上游运行时依赖变动 | 依赖升级可能阻断构建 | 防腐层集中适配；升级纳入 CI 回归 |
| 前端交互测试覆盖率偏低 | 前端回归保护弱于核心编排层 | 补齐前端协议测试，不以全仓平均值掩盖前端缺口 |
| 标准基准缺失 | 尚无官方口径 SWE-bench/Terminal-Bench 结果 | 本期建立基准接入计划，明确标注"未完成"，不推算结论 |
| 文档与代码漂移 | 依赖版本号、Plugin 清单、报告路径均出现过不一致 | 把"文档事实核对"列为阶段交付项（S0、S6） |
| 范围蔓延 | 试图在期内同时完成跨进程协议与语义检索 | 明确列为后续演进方向，不在本期承诺内 |

---

## 七、进度安排

| 阶段 | 时间 | 主要任务 | 阶段产出 |
|---|---|---|---|
| S0 开题与基线固化 | 2026.09 | 文献综述、边界声明、**功能与参数全量盘点**（本版附录 A/C）、性能与成本基线冻结 | 开题报告 v2、基线报告、参数核对表 |
| S1 架构收口与模块边界 | 2026.10 | 明确各层职责与非职责，消除职责重叠与双轨实现；文档事实与代码对齐 | 架构说明、边界评审记录、漂移清单清零 |
| S2 上下文与压缩可观察化 | 2026.10—2026.11 | 压缩 DAG 验收判据、前缀重放一致性不变量、读回链路与校准机制 | 上下文治理实现、压缩回归测试、前缀命中报告 |
| S3 多智能体编排与可见性 | 2026.11—2026.12 | 并行隔离竞态评审、合并语义与来源标识、节点级可见性与 SubAgentTree | 编排实现、竞态测试报告、可见性设计 |
| S4 目标治理与裁决闭环 | 2026.12—2027.01 | 裁决语义形式化、缺席矩阵用例、审批预筛边界、审计可回放 | 治理实现、裁决用例集、审计报告 |
| S5 安全边界、持久化与可观测收敛 | 2027.01—2027.02 | 逃逸用例集、隐藏工具拒绝、顺序日志不变量、恢复路径与事件投影完整性 | 安全用例集、协议规范、恢复测试报告 |
| S6 验证体系与基准接入 | 2027.02—2027.04 | 确定性 E2E 扩充、覆盖率补齐（含前端）、性能与成本基准复跑、标准化基准接入 | 测试报告、性能报表、基准结果（含局限性说明） |
| S7 论文撰写与交付 | 2027.04—2027.06 | 论文撰写、外部可用性验证、成果整理 | 学位论文/技术报告、可分发版本 |

---

## 八、预期成果与应用价值

**预期成果**

1. 一个可运行的开源 Coding Agent Harness：项目作用域工具调用、按需多智能体编排、**可逆且可观察**的上下文治理、分层记忆、**可裁决的完成模型**、双层安全边界、事务式扩展系统与快照/事件可观测协议。
2. 一套确定性端到端验证体系与若干组可复现基准数据（功能、并发、性能、token 成本），含**参数实现核对表**与**可复现性分级**，可作为同类系统的对照基线。
3. 一份系统性的设计与实现技术报告（学位论文/技术报告），覆盖架构决策、关键算法（压缩 DAG、前缀重放、预算推导、裁决门禁）与验证证据。
4. 可复用的垂直领域能力样板（以声明式插件组织工具、技能与外部服务），示范 Harness 的领域扩展路径。

**应用价值**

- 对个人开发者：在可控成本下获得可自托管的编码智能体，支持多模型路由与会话恢复。
- 对数据敏感型团队：本地部署 + 路径约束 + 逐操作审批 + 事件审计满足合规与审计要求。
- 对研究与教学：提供可复现、可替换、可测试的智能体运行时样本，降低多智能体与上下文治理论文的工程复现门槛。

---

## 九、参考文献

> 与前版一致的 27 篇（英文 18 / 中文 9，2021—2026 年）。文献分级与复核说明见前版附录 A。

**英文文献**：[1] Wei J, et al. Chain-of-Thought Prompting Elicits Reasoning in Large Language Models. NeurIPS 2022. arXiv:2201.11903. [2] Yao S, et al. ReAct: Synergizing Reasoning and Acting in Language Models. ICLR 2023. arXiv:2210.03629. [3] Shinn N, et al. Reflexion: Language Agents with Verbal Reinforcement Learning. NeurIPS 2023. arXiv:2303.11366. [4] Schick T, et al. Toolformer: Language Models Can Teach Themselves to Use Tools. NeurIPS 2023. arXiv:2302.04761. [5] Qin Y, et al. ToolLLM: Facilitating LLMs to Master 16000+ Real-World APIs. ICLR 2024. arXiv:2307.16789. [6] Wang X, et al. Executable Code Actions Elicit Better LLM Agents. ICML 2024. arXiv:2402.01030. [7] Park J S, et al. Generative Agents: Interactive Simulacra of Human Behavior. UIST 2023. arXiv:2304.03442. [8] Wu Q, et al. AutoGen: Enabling Next-Gen LLM Applications via Multi-Agent Conversation. arXiv:2308.08155. [9] Hong S, et al. MetaGPT: Meta Programming for a Multi-Agent Collaborative Framework. ICLR 2024. arXiv:2308.00352. [10] Jimenez C E, et al. SWE-bench: Can Language Models Resolve Real-World GitHub Issues? ICLR 2024. arXiv:2310.06770. [11] Yang J, et al. SWE-agent: Agent-Computer Interfaces Enable Automated Software Engineering. NeurIPS 2024. arXiv:2405.15793. [12] Liu J, et al. Large Language Model-Based Agents for Software Engineering: A Survey. arXiv:2409.02977. [13] Liu N F, et al. Lost in the Middle: How Language Models Use Long Contexts. TACL 2024, 12:157-173. arXiv:2307.03172. [14] Packer C, et al. MemGPT: Towards LLMs as Operating Systems. arXiv:2310.08560. [15] Mei L, et al. A Survey of Context Engineering for Large Language Models. arXiv:2507.13334. [16] Guo T, et al. Large Language Model Based Multi-Agents: A Survey of Progress and Challenges. IJCAI 2024. arXiv:2402.01680. [17] Anthropic. Model Context Protocol Specification (Revision 2024-11-05). 2024. [18] Linux Foundation. Agent2Agent (A2A) Protocol Specification. 2025.

**中文文献**：[19] 郭先会, 张梦姣, 马军. 基于大语言模型的智能体构建综述[J]. 通信技术, 2024, 57(9). [20] 赵鑫, 李军毅, 周昆, 等. 大语言模型[M]. 北京: 高等教育出版社, 2024. [21] 张犬俊, 房春荣, 谢杨, 等. 大模型时代的软件工程: 现状和展望[J/OL]. 中国科学: 信息科学, 2026(网络首发). [22] 彭鑫. 大模型时代的软件智能化开发: 分析、思考与展望[EB/OL]. 2023. [23] 中国信息通信研究院. 人工智能发展报告(2024年)[R]. 2024. [24] 中国软件评测中心. 人工智能大语言模型技术发展研究报告(2024年)[R]. 2024. [25] 中国人工智能学会. 中国人工智能系列白皮书——大模型技术(2023版)[R]. 2023. [26] 深圳市人工智能行业协会. 2024人工智能发展白皮书: 人工智能大模型[R]. 2024. [27] 中移智库. 2024年提示工程——大模型中的提示词设计研究报告[R]. 2024.

---

## 附录 A：参数实现核对表（阈值 → 代码 → 状态）

| 前版声称 | 代码实况 | 状态 |
|---|---|---|
| 12.5% 安全余量 | `safetyReserve := window / 8` | ✅ 一致（`seelexctx/controller.go:57`） |
| 软阈 75% / 硬阈 90% / 压缩目标 60% | `Budget()*75/100`、`*90/100`、`*60/100` | ✅ 一致（`controller.go:70,73,76`） |
| `clamp(...,4,40)`、比例 0.7 | `WindowConfig{Ratio:0.7, MinRounds:4, MaxRounds:40}` | ✅ 一致（`window.go:38`） |
| 工具结果 20000 字符归档 | 实际默认 **60000** 字符；20000 仅见过期注释 | ❌ **已更正**（`limits.go:134`） |
| 并行"默认最大并发 3" | 无硬编码默认；`PolicyConcurrency` 未配置时随节点数增长，"3"来自 Effort 提示层声明 | ⚠️ **已澄清**（`policy.go:56-66`、`tool_provider.go:297`） |
| Effort 四档 loop/工具预算 | 15/30、48/96、384/768、768/1536；medium 强制串行且 ≤4 节点 | ✅ 一致（`effort.go:48-73`） |
| 可逆压缩、merge-back、双层边界、插件事务 | 均有生产实现（见 §3.2 内容三/五/八/九） | ✅ 已实现 |
| 跨进程 A2A、标准化编码基准、语义检索 | 无实现 | ⛔ 明确未实现（`README.md` 边界、`feature-instrumentation.md:53`） |

## 附录 B：可量化基线与可复现性分级

**B1 工程规模（🟢 本机 2026-09-13 清点）**

| 指标 | 数值 |
|---|---|
| Go 文件 / 行数 | 746 / 124,993 |
| 测试文件 / 行数（占比） | 380 / 60,589（48.5%） |
| 测试/基准/fuzz 函数 | 1,559 |
| Go 包 | 87 |
| `application/contract` 契约 | 13 接口 / 71 类型 |
| 文档 | 455 Markdown / 40,619 行；67 个按日期迭代目录 |
| 前端 | 71 JS/MJS 文件 / 14,944 行；32 个前端测试文件 |
| 迭代 | 556 commits / 62 天（2026-07-13 → 2026-09-13）；4 个 release tag |
| Plugin / Skill | 2 / 17 |

**B2 上下文与缓存（🟡 仓库文档 + 🟢 探针可复现）**

| 指标 | 数值 | 口径 |
|---|---|---|
| 跨轮首请求前缀命中率 | **46.3% → 80.2%** | 三臂对照探针（C 组为验收基线）`docs/research/2026-09-12-cache-hit-vs-codex-root-cause.md:104-115` |
| 失效 token | 4,537（53.7%）→ 1,668（19.8%） | 同上 |
| CompactStack 场景失效 | 79,651 / 83,554 token（95.3%） | 同上 `:115` |
| 回合内 append-only 命中 | 58.9% / 81.5% | 同上 `:104,105` |

**B3 会话存储（🟡 文档口径 + 🟢 本机复现）**

| 指标 | 前 → 后 | 备注 |
|---|---|---|
| mutex delay | 53.1 s → 9.55 s（≈5.6×） | mock provider，同机同场景，2026-09-09 |
| 探查墙钟 | 12.7 s → 4.64 s | 同上 |
| 尾窗读（5000 行 / 50 分片） | 56–184 ms → 3.8–10.5 ms（8–20×） | **本机复现：94.0 ms → 10.6 ms（8.9×）** |
| store 足迹 | 32 文件 / 47.6 KB → 8 文件 / 8.7 KB | ≈5.5× 体积缩减 |
| 冷恢复 | 32–100 ms；进程重启后 28–81 ms | — |
| 驻留内存 | `inuse_space` ≈3.1 MB，无泄漏 | — |

**B4 端到端性能与成本（🔴 需真实模型 API，2026-08-05）**

| 指标 | 旧 → 新 | 口径 |
|---|---|---|
| 串行 warm 吞吐 | 0.288 → **0.401 req/s（+39%）** | tmux 驱动真实 TUI → DeepSeek `deepseek-v4-flash` |
| 串行单轮均值 / p95 | 3.48 s → 2.49 s；6.88 s → 3.10 s | 同上 |
| 对话 P50 / P95 | 3.71 → 3.18 s / 13.07 → 13.67 s | 25 样本/组 |
| 连续工具调用（自锁场景） | 7/8（含 1 次流式中断）→ **8/8** | 8 样本/组 |
| 多轮上下文召回 | 8/8 → 8/8 | — |
| token 账目 | 总 37,689,781；缓存命中输入 97.6%；输出 0.6% | 完整计费数据 |
| **同批次已知回归** | TUI/backend 首次提交失败（懒创建会话 `Draft` 缺省） | 报告 §4 已定位根因 |

**B5 基准与质量门禁**

| 指标 | 数值 | 口径 |
|---|---|---|
| SWE-bench Pass@1 | **90/90**（batch10: pytest×8+xarray+flask；batch30: sympy×26+pytest×3+xarray；batch50: sympy×50 全版本区间） | **有偏样本**（仅取不依赖外部 HTTP 实例），git worktree 精确 base_commit + 容器 + 官方 eval 测试 |
| 覆盖率 | 全仓 62.8%（2026-08-03）；core 75.6% / seelebridge 66.7% / workspace 68.4% / tui 26.1% | `go test ./... -covermode=count -coverprofile`；**2026-09-13 重测全仓 57.2%**（口径漂移） |
| CI 门禁 | 3 OS × Go 1.25 矩阵；race + 覆盖率产物；GUI Node 测试 + App↔GUI 契约测试；govulncheck；密钥扫描；发布包白名单审计 | `.github/workflows/ci.yml` |

## 附录 C：四域功能盘点（本次探索产出，60+ 项）

> 说明：本附录是"寻找前版未覆盖能力"的直接结果。★ 表示前版完全未提及。

**C1 Agent 运行时与多代理编排域**

| 功能 | 作用 | 关键数字 | 证据 |
|---|---|---|---|
| ★Goal 栈状态机 | 会话级 LIFO 目标栈与状态机 | 栈深上限 16；订阅 buffer 64 | `goal/controller.go:143`、`record.go:21` |
| ★goal 第五栈持久化 | 活栈投影落库，重启续治理 | 终态弹栈即删；栈空即收口 | `goal/sessionstore_store.go:53` |
| ★DS-A2A 双会话裁决 | EXEC 事件→裁决侧独立上下文回合 | 上下文只尾部追加 | `goal/techleader.go:228`、`advisor.go:143` |
| ★抽帧节流 | 非关键信号窗口内抑制 | `DefaultEvalWindow=3` | `goal/a2a.go:33` |
| ★裁决指令邮箱 | 满丢最旧并计数 | cap 32；正文 ≤1200 rune | `goal/a2a.go:25`、`techleader.go:43` |
| ★缺席矩阵与终态门禁 | 完成需裁决；执行侧不等裁决侧 | done/not_done/escalate | `goal/gate.go:41` |
| ★审批预筛 | 低风险代答、高风险转人工 | 摘要 ≤400 rune | `goal/gate.go:138` |
| ★治理审计 | 每次状态变更 append-only | 按会话隔离、可跨会话溯源 | `goal/audit.go:62` |
| ★通用治理循环 govern | 座位顺序推进 + 断环 + 轮次护栏 | `NewTurnGovernor(seats,maxRounds)`；Break 幂等 | `govern/governance.go:92,131,168` |
| ★AgentTeam 工厂 | TeamSpec→角色会话 + 注册表 + 顺序 | 幂等键 `(team_id, role_name)` | `agentteam/factory.go:43`、`spec.go:25` |
| ★团队 preset | 目标创建时隐式拉起团队 | `JoinPolicy=on_goal_create` | `agentteam/presets.go:13,81` |
| ★子代理/团队边界 | 子代理等同工具调用，不进成员表 | 仅经 fork 派发 | `docs/2026-09-10-a2a-agentteam-recovery/README.md` |
| ★Worktable 行投影 | plan/tasklist/subagent/todo 四源合一 | 行 200；证据 800 | `work_table.go:29`、`limits.go:116,130` |
| ★traceboard | 节点打卡 + 阶段合成追踪点 | 事件 30；done 保留 50 | `work_table.go:179,122` |
| ★增量汇聚 | 突发更新 latest-wins | Revision/RequestID | `worktable/worktable_publisher.go:37` |
| ★Todo 三态 + Assignee | 系统按 role:sessionID 被动上名单 | 上限 20 | `work_table.go:651`、`limits.go:129` |
| Task 终态工具 | 终态工具校验后落 Plan 投影 | 未覆盖节点拒收 | `task_context/task_service.go:286,331` |
| Task transcript/checkpoint | seq 单调、checkpoint 有界 | token EMA 校准 | `task_context/task_context_state.go:107` |
| ★fork 切断点 | 按 EventSeq 段落边界深拷贝子会话 | 帧整帧继承 + 范围重写 | `session_runtime/fork.go:33,115` |
| ★fork 独立预算 | 同步编排不被通用超时掐死 | 默认 7200s | `seelebridge/fork/tool.go:28,107` |
| ★通用恢复七步模板 | 定位→判定→补历史→重建→注入→同键重跑→收敛 | 按 Key 去重、在途跳过 | `resume/resume.go:53,228,360` |
| ★子代理冷恢复续跑 | running/queued 残留重建并同键续跑 | 恢复说明 `role=system` | `seelebridge/runtime_subagent_resume.go:35` |
| 并发：同会话串行 | per-session 过渡锁（FIFO actor） | `TransitionLock(key)` | `session_runtime/transition_manager.go:24,29` |
| 并发：驻留与重规划限流 | 超限驱逐 + 并发/窗口限流 | 6 / 2 / 6-per-60s | `limits.go:108,109,112` |

**C2 上下文、记忆与持久化域**

| 功能 | 作用 | 关键数字 | 证据 |
|---|---|---|---|
| ★压缩 DAG 执行器 | 5 节点图选段→双章节→合并→发布 | 2 分支 / 重试 1 / 兜底串行 | `seelexctx/dag.go:46,111,230` |
| ★CompactFrame 两章节 | Chapter1 锚点 + Chapter2 厚内容 | 模型仅见栈顶帧 | `seelexctx/frame.go:8`、`sessionstore/session_context.go:148,171` |
| ★PushCompact 链不变量 | request 首尾一致 + prev 必须等于栈顶 | `anchor_source=ok/degraded` | `session_context.go:743,763,784` |
| ★前缀重放摘要协议 | 摘要复用真实请求字节前缀 | 失败一次 → 本地折叠 | `seelexctx/replay.go`、`docs/2026-09-06-compaction-dag/design.md` |
| 预算与安全余量 | `window − output − window/8` | 75%/90%/60% | `controller.go:57,70,73,76` |
| 硬阈值归档 | 先归档超大工具结果再收缩窗口 | — | `controller.go:349,352` |
| 结果引用与分页读回 | 省略警告 + `result_ref`，可页/过滤读回 | 归档阈值 60000 | `processor.go:100,133,144,160`、`limits.go:134` |
| ★相关记忆块 | 按查询从压缩帧取 top-K 渲染记忆块 | limit 3 / recency 0.15 / 1024 token | `memory/memory.go:47`、`block.go:16` |
| ★历史检索读回 | 压缩栈作索引，命中帧范围读回真实记录 | 3/20；4000/12000；回退 300 | `search/search.go:35-47` |
| ★CLI/项目级记忆 | MEMORY.md 索引常驻、详情按需 | 索引 ≤20；向量 limit 5/20 | `docs/plan/cli-memory.md`、`memory-architecture.md` |
| ★记忆生命周期铁律 | 危险操作先预警+备份；bugfix 先复现再修 | — | `MEMORY.md:6,36` |
| ★跨会话记忆装配 | memory 块按项目粒度跨会话 | fork 复用稳定前缀 | `docs/arch/context-prefix-chain.md` |
| ★会话顺序日志 | 每会话单条物理 append-only JSONL 为唯一事实源 | Ordinal 单调；I-LOG-1..5 | `docs/2026-09-07-session-order-log/README.md:62,93` |
| ★分区与 head 发布 | project→session 分区，head 为提交发布点 | 分片 100 行；退避累计 77ms | `sessionstore/README.md:27,62,262` |
| ★结构性 EVENT 通道 | append-only 摘要，幂等只看 head 水位 | 正确实现要求 shardReads=0 | `sessionstore/README.md:228,245` |
| ★三栈通道 plan/task/goal | 各自 JSONL + head（只装水位） | 批次弹栈 LIFO；fork 按锚重建 | `sessionstore/README.md:182,185` |
| ★中断轮截断 | 残缺工具链保留为开放单元 + 合成占位 | 3 处同构切分器、幂等 | `docs/2026-09-08-interrupted-round-truncation/README.md:18,20` |
| ★恢复前缀一致性 | 尾窗乱序→前缀校验重建 | 超预算显式拒绝 | `docs/2026-09-08-context-restore-review/README.md:35,240` |
| ★快照与回流存活 | Snapshot 纯内存复制；回流水位锁外 drain | 满则丢弃 + 诊断计数 | `docs/arch/session-snapshot-liveness.md` |
| ★脚本感知 token 估算 | CJK 1≈1、ASCII 4≈1、符号 2≈1 | 确定性偏保守 | `tokens/tokens.go:7,41` |
| ★lifecycle actor/pipeline | 泛型 actor + 有界批处理，背压显式错误 | mailbox 默认 256 | `lifecycle/actor.go:114,370` |
| ★会话 fork 存储契约 | 段落边界切断，tool-results 物理深拷贝 | generation 血缘；删父仍可读 | `sessionstore/README.md:374` |
| ★子代理会话记录 | NodeSessionRecord 落盘，终态写回后删文件 | schema 版本化 | `sessionstore/README.md:273` |
| ★工作树只读查询 | ListTree/CountFiles 仅元数据，逃逸拒绝 | 单目录 ≤500；预算 200k | `workspace/README.md:28`、`tree.go:24,27` |

**C3 工具生态域**

| 功能 | 作用 | 关键数字 | 证据 |
|---|---|---|---|
| ★点击配对注入 | 按下/释放成对，按下失败也补释放 | 默认间隔 60ms | `tools/computer/click.go:16`、`computer.go:89` |
| ★SendInput 鼠标注入 | 以真实入队事件数判成败 | 替换 void 的 `mouse_event` | `tools/computer/input_windows.go:147` |
| ★DPI 感知（Per-Monitor V2） | 避免缩放后注入坐标偏移 | 125% 缩放曾偏约 20% | `tools/computer/screen_windows.go:66` |
| 截图 + 最近邻缩放 | 截虚拟桌面/区域，缩放不改坐标系 | 默认 max_width 1600 | `tools/computer/mcp/main.go:30`、`image.go:7` |
| ★键盘输入原语 | UTF-16 注入中文/emoji；修饰键逆序释放 | VK 常量集 | `input_windows.go:250`、`keys.go:58` |
| ★窗口枚举与聚焦 | 枚举顶层窗口并按标题置前 | — | `window_windows.go:82,110` |
| ★computer-use MCP 工具面 | MCP stdio 暴露截屏/鼠标/键盘/窗口/睡眠 | 工具清单在 toolDefinitions | `tools/computer/mcp/main.go:504` |
| Web 搜索装配点 | 账号池 websearch 段装配并注册 `web_search` | 失败仍注册占位工具，不 panic | `tools/websearch/websearch.go:25` |
| ★搜索厂商适配 | tavily / bochaai / searxng 三家 | 默认 max_results 10 | `search/builtin.go:21`、`builtin_bocha.go:23`、`builtin_searxng.go:13` |
| ★MCP 冷启动登记 | RegisterLazy 只登记不连接 | 启动零 MCP 进程 | `mcp/mcp.go:155` |
| ★MCP 按需加载 | Load 首次连接并注册工具，返回工具数 | 幂等；未知 server 显式报错 | `mcp/mcp.go:188` |
| ★MCP 工具重挂载 | 注销并重注册 provider | breaker 通道 cap 64 | `mcp/mcp.go:247,70` |
| Plugin 工具可见性过滤 | 按插件 include/exclude 通配过滤 | 单选激活；`*` 通配 | `seelebridge/plugin/plugin.go:92` |
| Plugin 事务式切换 | 顺序执行 + 失败逆序回滚 | 先备新状态再拆旧 | `plugin/apply.go:21`、`manager.go:94` |
| ★Plugin 热更新 diff | 计算差异并事务式应用 | 失败回滚到上一可用态 | `plugin/apply.go:56`、`manager.go:198` |
| ★Manifest 安全校验 | schema_version、重复 MCP 名、路径逃逸拒绝 | — | `plugin/loader.go:107,145,182` |
| ★Skill 资源安全 | 拒绝绝对路径与 `..` 逃逸 | plugin scope 不污染 global | `skill/skill.go:23,172` |
| ★Skill 加载优先级 | 多 root 按配置顺序，目录格式优先 | — | `skill/loader.go:145,202` |
| ★MCP 调用栈可观测 | history + cursor，Undo/Redo + 预算摘要回灌 | ForPrompt 按 budget 折算 | `mcpstack/prompt.go:13`、`mcpstack/README.md` |

**C4 交互前端域**

| 功能 | 作用 | 关键数字 | 证据 |
|---|---|---|---|
| ★GUI 协议 v1 | Snapshot/Event 契约 + delivery_seq 重放 | Replay/Ack/resync | `docs/gui/modules/application-protocol.md:27,47` |
| ★组件库三层样式 | pico 基线 → token 桥接 → 皮肤包 | @picocss/pico 2.1.1 | `docs/devlog/2026-09-11-component-library-and-skins.md:9` |
| ★皮肤包机制 | manifest 换肤，只覆盖语义 token | 3 套内置 | `gui/frontend/dist/themes/manifest.json` |
| ★皮肤安全边界 | id 白名单 + 路径限定 | id 正则 `[a-z0-9-]{0,31}` | `gui/frontend/dist/theme.js:16,31` |
| ★HTML 内嵌渲染 | 沙箱帧渲染 `seelex-html` 代码块 | 无网络、无应用访问 | `docs/devlog/2026-09-11-computer-use-input-and-html-embed.md` |
| ★会话时间轴 wheel | 迷你地图：按测量几何定位 + 拖拽视口 | 每渲染项一行 | `CHANGELOG.md`（Unreleased）、`docs/devlog/2026-09-11-conversation-wheel-and-restore-order.md` |
| TUI 建议弹层 | `/`、`#`、`@` 触发候选 | 建议窗口 8 行 | `tui/suggest_view.go:10` |
| TUI Plan 分级视图 | 按 Effort 与终端宽度渲染进度 | 进度条宽度上限 40–55 | `tui/plan.go:93` |
| TUI 跨会话待批计数 | 状态行「待批:N」+ 短 ID 提示 | — | `tui/README.md:27` |

## 附录 D：工程成果映射（论文内容 ↔ 可验证数字）

| 研究内容 | 可验证数字 | 可复现性 |
|---|---|---|
| 内容三（上下文流水线） | 跨轮前缀命中 46.3% → 80.2%；失效 token 53.7% → 19.8% | 🟢 探针可复现 |
| 内容三（压缩 DAG） | 5 节点 / 2 分支 / 1 次重试 / 本地折叠兜底 | 🟢 单测 |
| 内容十/十一（持久化与恢复） | mutex 53.1 s → 9.55 s（5.6×）；尾窗读 8–20×；store 5.5× 缩减；冷恢复 32–100 ms | 🟢 本机复现（8.9×） |
| 内容十（可观测协议） | 增量负载 4,411 B vs 197,077 B（2%） | 🟢 本机复现 |
| 内容六（目标治理） | 栈深 16 / 评估窗口 3 / 指令队列 32 | 🟢 常量核对 |
| 内容二与全局 | Effort 四档预算 15/30 → 768/1536 | 🟢 常量核对 |
| 内容十二（端到端性能） | 吞吐 +39%；连续工具调用 7/8 → 8/8；token 账目 | 🔴 需真实 API |
| 内容十二（基准） | SWE-bench 90/90（有偏样本，需标注） | 🔴 需容器与真实 API |
| 验证体系 | 1,559 测试函数；三平台 CI + race + 覆盖率 | 🟢 CI |

## 附录 E：与前版（2026-09-11）的差异与更正记录

1. **研究内容 8 → 12 条**：新增"分层记忆与按需检索""目标治理与完成裁决闭环""代理团队工厂与工作台投影""恢复、存活与中断一致性"；原"多智能体编排"拆分为编排与治理两支。
2. **创新点 5 → 6 个**：新增"压缩即 DAG + 前缀重放摘要协议""可裁决可审计的完成模型"。
3. **关键问题 5 → 6 个**：新增"完成判定的可裁决性"。
4. **参数更正 2 处**（附录 A）：工具结果归档阈值 20000 → **60000**；并行"默认 3" → **由 PlanPolicy/Effort 决定，未配置时随节点数增长**。
5. **新增可验证性材料**：附录 A（参数→代码行）、附录 B（可复现性分级 🟢/🟡/🔴）、附录 C（60+ 功能盘点）、附录 D（成果→数字映射）。
6. **风险表新增 3 项**：参数口径漂移、裁决侧网络依赖导致任务悬置、文档与代码漂移。
7. **进度表 S0/S6 新增"文档事实核对"交付项**；S4 由"安全边界与审批链"调整为"目标治理与裁决闭环"，安全边界并入 S5。
8. **诚实边界继承不变**：跨进程 A2A、官方口径编码基准、语义检索仍为未实现，不作为本期交付承诺。
