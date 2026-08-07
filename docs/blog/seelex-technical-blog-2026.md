# 从零构建一个带"上下文治理"的 Coding Agent Harness：Seelex 的技术决策与产品思考

> 作者视角：Seelex（`github.com/RedHuang-0622/seelex`）开发者
> 写作日期：2026-08-07
> 配套文档：`docs/research/agent-market-research-2026-08.md`（市场调研）、`docs/product/roadmap-2026.md`（产品规划）

---

## TL;DR

AI 编程工具 2026 年的竞争已经从"谁的模型强"转向"谁的 Harness 好"——也就是模型之外的运行时骨架：上下文怎么治理、多 agent 怎么编排、权限怎么审批、数据怎么持久化。本文用 Seelex 的真实代码回答四个问题：

1. **上下文超了怎么办？**——我们做了可逆的预算驱动压缩（主流基本不可逆）。
2. **长任务怎么拆？**——WorkPlan DAG + 并行 Subagent，每个节点独立 Session。
3. **能力怎么扩展？**——Plugin/Skill/MCP 事务式切换，用 FreeCAD 当垂直样板。
4. **怎么保证安全？**——ProjectScope + PathGate + Human-in-the-loop 审批链。

最后讨论：在这个 Cursor 估值 $29B、Claude Code 满意度第一、开源 agent 月月洗牌的市场里，一个自托管 Harness 的差异化机会在哪里。

---

## 1. 为什么是 Harness

先看一组 2026 年的市场事实（详见市场调研）：

- Claude Code 把 SWE-bench Verified 推到 87.6%（Opus 4.7），但它的 auto-compact 曾出现过"压缩后永久卡 102%"的 bug——**上下文治理本身就是工程问题，不是模型问题**。
- Block 用 Goose agent 实现全公司 **98.7% 的 token 削减**——省 token 靠的是 harness 策略，不是模型。
- Cursor 3.0 的卖点是"Agents Window 并行多 agent"，Devin Local 的卖点是"比 Cascade 省 30% token"——**并行编排与 token 效率是显式竞争力**。
- MCP 已经 97M 月下载、5800+ 官方 server，成了 agent↔工具的事实标准——**可扩展性取决于接入标准的能力**。

结论：一个可用的 agent 不只有模型调用。它需要回答"模型什么时候能调哪些工具、上下文保留什么、长任务怎么拆、崩溃怎么恢复、谁批准危险操作"。

Seelex 把这些能力组织成可替换、可测试的模块，而不是写进某个前端或单一循环。下面是四个核心技术决策。

---

## 2. 上下文治理：预算驱动，而不是"满了就截断"

### 2.1 核心设计

上下文治理的第一原则是**窗口内不动、窗口外压缩**——只对滑出窗口的轮次做摘要，窗口内原样保留。Seelex 的滑动窗口轮数 N 不是魔法数字，而是由 provider 推导：

```go
// seelexctx/window.go
// N = clamp((ContextTokens × Ratio − Reserved) ÷ AvgRoundTokens, MinRounds, MaxRounds)
// 默认 ratio=0.7, min=4, max=40；决策顺序：显式配置 > provider 推导 > 保守回退
type DefaultWindowPolicy struct{ Config WindowConfig }
```

窗口之上是**软/硬双阈值**（`seelexctx/controller.go`）：

- **软阈值**：跨过即触发"升级压缩"，提前动手，避免撞墙（对齐 Claude Code 从 90% 提前到 64-75% 的经验）。
- **硬阈值**：走 `hardThresholdPath`——先把超大工具输出归档为 `result_ref`，模型只看到省略标记，按需 `read_tool_result` 读回。

```go
// application/core/context_controller.go
return "<seelex-content-reference>\nresult_ref=" + resultRef + "\n" + ...
```

### 2.2 我们做的最关键的决策：压缩必须可逆

主流方案（Claude Code auto-compact、OpenHands Condenser）压缩后是**不可逆**的——摘要丢细节，丢了就没了。我们做了一个少见的选择：**压缩帧持久化，模型随时可以读回原文**（`read_compressed_turn` + TurnArchiver）。

代价：存储与索引复杂度上升。收益：一个"读回原文"的路径解决了 agent 最常见的失败模式——"摘要里漏了一个关键数字，任务跑偏"。这在生产场景（审计、长任务续跑）是刚需。

> **踩坑故事**：早期压缩路径有一个"错误静默回退"——Compressor 出错时静默降级到 QuickChat，下游不感知，任务在错误的上下文上继续跑。这正是"上下文治理必须显式"的教训：**任何降级都要是显式事件，不能是静默行为**。（见 `docs/arch/architecture-and-flaws.md` 与 ARCHITECTURE_REVIEW.md）

### 2.3 证据

- 窗口推导：`seelexctx/window.go`（WindowPolicy / DefaultWindowPolicy）
- 软/硬阈值：`seelexctx/controller.go`（softThresholdHit / hardThresholdPath）
- 归档读回：`application/core/reference_tools.go`（read_tool_result + result_ref + digest）
- 与主流对比：`docs/research/coding-agent-harness-comparison.md` §3.2（"压缩可逆性是 Seelex 的差异化强项"）

---

## 3. 多 Agent 编排：WorkPlan DAG，而不是"把所有事塞进一个提示词"

### 3.1 核心设计

面对长任务，Seelex 可以按需加载 WorkPlan DAG（`plan_load` 载入 JSON DSL → 拓扑校验 → `plan_run` 执行）。每个节点拥有**独立的 Session、NodeScope、PromptBlocks、账号 binding 和 token budget**，并行执行后再把 findings/decisions/progress **merge 回父会话**。

关键点：**Plan 不是所有请求的强制前置步骤**——简单任务直接进主 ReAct 循环，不承担规划延迟和 token 成本。这避免了很多 agent 框架"必须走完整规划流水线"的开销。

`fork_subagents` 是同一个思想的工具化：派发 N 个隔离 subagent（worktree 隔离）并行调研/实现，返回结构化 findings。

### 3.2 我们解决的两个坑

**坑 1：子代理无作用域。** 早期 plan 执行整体委托给上游的 WorkPlanTool，框架创建的 branch 子聊天**不继承项目作用域工具集**（ProjectScope 过滤、证据 envelope），导致并行能力被框架持有而 Seelex 不可控。后来通过 `seelebridge` 让主代理自己持有 fork 语义，节点继承项目 scope。（`docs/arch/seele-v2-runtime-architecture.md` §2.1）

**坑 2：工具完成死锁。** 上下文生命周期曾出现过"tool completion 与 context 生命周期互相等待"的死锁，专门有 `docs/2026-08-04-context-memory-lifecycle/runtime-tool-completion-deadlock.md` 记录。解决方式是给父↔子、应用↔运行时两条边界都做成"消息进出 + 不可变快照"，避免共享可变状态。

### 3.3 证据

- DSL 与执行：`seelebridge/plan.go`、`seelebridge/branch.go`
- fork 工具：`seelebridge/fork_tool.go`（docs/2026-08-03-subagent-fork-architecture/plan.md §4）
- 节点可见性：`seelebridge/subagent_tree.go`（subagent tree 渲染）
- 边界设计：`docs/arch/seele-v2-runtime-architecture.md` §3.2 装配拓扑

---

## 4. 可扩展性：Plugin/Skill/MCP 事务式切换，FreeCAD 当样板

### 4.1 核心设计

Seelex 的扩展系统是三件套：

- **Plugin**：声明式（`plugin.md`），可以**事务式**切换工具、Agent Skills 和 MCP Server——激活失败自动回滚到上一个可用状态（`plugin/manager.go`，有回滚能力）。
- **Skill**：目录化（`SKILL.md`），随插件注入。
- **MCP**：动态挂载 + 工具可见性过滤 + 调用轨迹（`mcpstack/`）。

### 4.2 为什么用 FreeCAD 当垂直样板

`plugins/freecad/` 不是为了"支持 CAD"，而是验证**平台抽象能否承载一个完整垂直域**：MCP 交互探索 → JSON 驱动批量建模兜底 → headless FreeCADCmd 脚本，三级降级链。如果插件系统能优雅承载 CAD 这种完全不同的领域（几何对象、批量 recompute、STEP/STL 导出），那它就能承载任何垂直域。

> **经验**：垂直样板的价值不在于功能本身，而在于暴露平台缺口。FreeCAD 插件逼出了"MCP timeout ≠ 建模失败，fallback 不能产生冲突对象"这类平台级约束——这些约束在纯 coding 场景里永远不会浮现。

### 4.3 证据

- 插件回滚：`plugin/manager.go`（Activate 失败 restoreToolPluginLocked）
- 垂直样板：`plugins/freecad/README.md`（降级链与 Review 约束）
- MCP 栈：`mcpstack/`（snapshot/interceptor/breaker/persist 双栈）

---

## 5. 安全边界：ProjectScope + PathGate + Human-in-the-loop

### 5.1 核心设计

文件与 Shell 工具同时受两层约束：

1. **ProjectScope**：workspace root 的路径 containment——先保证工具"出不了项目"。
2. **PathGate**：在合法范围内继续执行 **allow / ask / deny** 策略，高危操作通过 human-in-the-loop 审批交互完成。

配套的还有工程级安全实践：CI 里扫描硬编码密钥（`sk-`/`password`/`token`）、检查 `return nil, nil` 反模式、发布配置白名单（只允许 README 与 example 进 config）。

### 5.2 为什么这值得写进博客

市场调研显示：2026 年企业 AI 团队最常引用的顾虑是 **visibility gap**——"看不到 agent 在干什么"。Cline 靠 step-by-step approval 拿到 4M+ 开发者，Cursor Enterprise 把 SSO/RBAC/审计日志当卖点。**"可信执行"正在替代"生成能力"成为企业采购理由**。Seelex 的 Snapshot/Event 协议 + Plan 节点事件 + MCP 调用轨迹，本质上就是"可审计执行"的工程实现。

### 5.3 证据

- 路径门禁：`docs/arch/permission-path-gating.md`
- 审批：`application/approval/`、`application/event/`（EventHub / ApprovalBroker）
- CI 安全：`.github/workflows/ci.yml`（密钥扫描 / 白名单检查）

---

## 6. 未来产品规划：从"工程证明"到"可用产品"

市场调研（`docs/research/agent-market-research-2026-08.md`）给出的判断：

- **差异化不在"又一个 coding agent"**，而在三个少见组合：**可逆上下文治理 + 多模型账号池路由 + 本地优先可审计**。
- 这三个点分别命中市场的**成本、信任、数据主权**痛点；闭源巨头（Cursor/Claude Code/Devin）不会提供"多模型自由路由 + 完全本地"，开源竞品（OpenCode/OpenHands/Cline）很少做"可逆压缩 + 账号池路由"。

对应产品规划（`docs/product/roadmap-2026.md`）的核心路线：

| 阶段 | 时间 | 目标 |
|---|---|---|
| 0 产品化收口 | 2026 Q4 | 一键安装、GUI Beta、费用可视化；30 分钟跑通真实任务 |
| 1 Beta 与社区 | 2027 Q1 | Plugin 生态目录、审计报告导出、AGENTS.md 支持；外部贡献 ≥ 10 PR |
| 2 v1.0 | 2027 H1 | MVP 全量、Plugin SDK 稳定、A2A 实验、三平台 release |
| 3 v1.x 商业化 | 2027 H2+ | 生态市场、团队协作、企业包（SSO/审计/私有化） |

**北极星指标**：每周完成的有效任务数（任务从开始到 `task_complete` 的闭环数）——比 MAU 更能反映"agent 真的在干活"。

**风险坦白**：
- 当前是 Developer Alpha、无真实用户——**工程强、产品未证**是最大风险。
- 单作者维护 201 commits，开源可持续性需要社区化（好第一 issue、维护者制度、赞助）。
- 上游 Seele 依赖与模型能力是变量——靠 `seelebridge` Anti-Corruption Layer 和"治理/路由"定位对冲。
- 最大的技术短板是没有代码库语义索引（Cursor 的 embedding 检索、Augment 的 Context Engine 是它们的护城河）——明确不进 v1.0，v1.x 用 ProjectKnowledge 演进。

---

## 7. 结论

2026 年的 coding agent 市场不是 winner-take-all：Claude Code 吃终端重度用户，Cursor 吃 IDE 体验，Devin 赌自主执行，开源靠 BYOK 与治理。Seelex 的生态位是**中间那条少有人走的路**——一个可组合、可审计、可自托管的 Harness，把上下文治理做到可逆、把模型选择权还给用户、把每次工具调用都变成可追溯的事件。

它现在还是 Developer Alpha，但它回答的四个问题（上下文、编排、扩展、安全）是任何严肃 agent 系统都绕不开的。这也是这个项目存在的意义：**不是又造一个 agent，而是把 agent 的"地基"做扎实。**

---

## 附：证据与延伸阅读

- 仓库：`github.com/RedHuang-0622/seelex`
- 市场调研：`docs/research/agent-market-research-2026-08.md`
- 产品规划：`docs/product/roadmap-2026.md`
- Harness 对比：`docs/research/coding-agent-harness-comparison.md`、`docs/research/agent-harness-research-report.md`
- 架构：`docs/arch/seele-v2-runtime-architecture.md`
- 代码：`seelexctx/window.go`、`seelexctx/controller.go`、`application/core/context_controller.go`、`application/core/reference_tools.go`、`seelebridge/fork_tool.go`、`plugin/manager.go`、`mcpstack/`
