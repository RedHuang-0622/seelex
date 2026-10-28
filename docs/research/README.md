# Research Documents

本目录存放外部项目、开源库、协议和技术方案的调研与选型比较。

调研应记录日期、问题、候选方案、平台支持、许可证、安全边界、维护活跃度、集成成本、来源链接和结论。调研结论不是已实现能力；采用后还需在架构文档、模块 README 和代码中落地。

## 文档索引

| 文档 | 说明 |
|---|---|
| [`2026-09-07-a2a-techleader-startup-research.md`](2026-09-07-a2a-techleader-startup-research.md) | 市面 A2A/Agent 产品如何"启动"评审/技术负责人角色：Codex/Claude Code 显式点名与 description 匹配、A2A Agent Card+Task、MetaGPT watch 订阅、Magentic-One Orchestrator 分派；结论：goal 激活不等于角色启动，评审由事件/分派触发（2026-09-07） |
| [`2026-09-11-seelex-harness-source-review-and-comparison.md`](2026-09-11-seelex-harness-source-review-and-comparison.md) | Seelex harness 源码解剖（主循环与预算、工具/权限/沙箱、上下文工程、Plan/Subagent、Task/Goal 治理、Snapshot/Event、扩展、持久化、测试基座）与 Claude Code / Codex CLI / Gemini CLI / OpenHands / Aider / SWE-agent 机制对比；含强项、分级风险与建议（2026-09-11，静态源码 + 一手文档取证，未跑构建/测试） |
| [`2026-09-11-seelex-vs-codex-context-strategy-control-group.md`](2026-09-11-seelex-vs-codex-context-strategy-control-group.md) | Seelex vs Codex 上下文策略**对照组实验**（同脚本/同串行化器/同估算器，唯一变量=下一轮消息列表怎么来）：跨轮首请求命中 65.9%（生产）vs 98.6%（仅修字节一致）vs 98.3%（Codex append-only 策略模型），重读 8837/371/435 tok；含逐字节示例、动态事实安置头部 vs 尾部（79651 vs 96 tok，×830）与合并漂移缺陷（2026-09-11） |
| [`codex-session-resume.md`](codex-session-resume.md) | Codex 会话恢复与 rollout 有序存储调研：单会话 append-only JSONL、单 writer 保序、压缩检查点后的前缀重放恢复，以及 Seelex 借鉴点（2026-09-07；官方文档 403，依据开源 main 分支与本地只读实证） |
| [`2026-08-24-conversation-fork-research.md`](2026-08-24-conversation-fork-research.md) | 对话 fork（会话级）功能可行性调研：Codex fork 机制、session store 链路、requestID 前提核实、上下文六层模型与风险、事件流三轨盘点、边界场景 user story。**一期决策已定稿**（深拷贝 + 血缘 meta + 整帧继承，否决方向已标注；见「十、一期决策契约」）（2026-08-24） |
| [`2026-08-24-session-resource-granularity.md`](2026-08-24-session-resource-granularity.md) | 会话资源与锁粒度盘点：单例现状、M1 会话级已落地项、多会话解除单例的路径（2026-08-24） |
| [`2026-08-24-fork-subagent-recovery.md`](2026-08-24-fork-subagent-recovery.md) | fork 子代理异常中断状态恢复可行性调研：串行/执行者归因、checkpoint 原语、前置准备与现状对照（2026-08-24） |
| [`context-management-review.md`](context-management-review.md) | 上下文管理（继承/合并/压缩）实现审查 + 论文/博客理论依据调研（2026-08-20） |
| [`agent-frontend-design-research.md`](agent-frontend-design-research.md) | AI Agent 前端界面与 DSL 卡片渲染设计调研 |
| [`agent-harness-research-report.md`](agent-harness-research-report.md) | Agent harness 能力缺口与建议 |
| [`agent-market-research-2026-08.md`](agent-market-research-2026-08.md) | 2026-08 Agent 产品市场调研 |
| [`approve-research.md`](approve-research.md) | Approve 节点选型（OpenCode vs Claude Code vs Seele） |
| [`coding-agent-harness-comparison.md`](coding-agent-harness-comparison.md) | 编码 Agent harness 对比（产品侧；外部信息冻结于 2026-08，最新外部机制矩阵见 `2026-09-12-external-harness-comparison.md`） |
| [`2026-09-06-dash-context-cache-hit-comparison.md`](2026-09-06-dash-context-cache-hit-comparison.md) | 缓存命中率为何停在 97% 而非 99%：与 DeepSeek Harness / DASH 的上下文策略对比（提出三个结构性漏点，2026-09-06） |
| [`2026-09-10-collaborative-doc-session-model.md`](2026-09-10-collaborative-doc-session-model.md) | 在线文档协作维护模型 → Seelex 群聊会话（2026-09-10） |
| [`2026-09-11-deepseek-v4.1-flash-naming-and-deployment.md`](2026-09-11-deepseek-v4.1-flash-naming-and-deployment.md) | DeepSeek-V4.1-Flash 名称与接入方式调研（2026-09-11） |
| [`2026-09-12-cache-hit-vs-codex-root-cause.md`](2026-09-12-cache-hit-vs-codex-root-cause.md) | 缓存命中率不及 Codex 的根因探究（7 条根因按期望影响排序，2026-09-12）。2026-10-21 已由 model-output cache unit 修复取代：工具轮正文与 reasoning/tool_calls 一起原样回放，旧归零基线仅作历史记录 |
| [`2026-09-12-external-harness-comparison.md`](2026-09-12-external-harness-comparison.md) | External comparison: coding-agent harness mechanisms (late 2026) vs Seelex（外部机制矩阵，逐条附官方文档/repo 链接，2026-09-12） |
| [`2026-09-14-seelex-thesis-proposal-v3.md`](2026-09-14-seelex-thesis-proposal-v3.md) | 开题报告 v3：面向软件工程任务的 Coding Agent Harness 设计与实现——以 Seelex 项目为例（**现行版**；v1/v2 已于 2026-10-15 文档治理中退役） |
| [`2026-09-17-fork-subagent-ownership-and-multiagent-orchestration.md`](2026-09-17-fork-subagent-ownership-and-multiagent-orchestration.md) | fork_subagents 的管理归属 + 多代理编排行业口径（2026-09-17） |
| [`2026-09-30-goal-a2a-flow-and-techleader.md`](2026-09-30-goal-a2a-flow-and-techleader.md) | goal-a2a 现状流程 × 市面对照 × 更好的结合方式 × 更优 TechLeader 流程（2026-09-30） |
| [`2026-10-02-compaction-record-and-failure-trace-survey.md`](2026-10-02-compaction-record-and-failure-trace-survey.md) | 上下文压缩 / 压缩失败的「记录与留痕」现状调研（2026-10-02） |
| [`2026-10-05-teammate-plugin-assembly-external-research.md`](2026-10-05-teammate-plugin-assembly-external-research.md) | teammate 的「插件装配」——外部一手调研（2026-10-05） |
| [`2026-10-15-agent-remote-collaboration-protocols.md`](2026-10-15-agent-remote-collaboration-protocols.md) | Agent 远程协作的通信协议与传输绑定调研：SeelexDot 的唯一定义与同名物辨析、DS-A2A v0.1「transport 无关」的实际含义、MCP/A2A/ACP/ANP/SLIM 一手对照，以及维持契约下的 L0/L1/L2 替换点与 7 项待补缺口（2026-10-15） |
| [`codex-claude-goal-harness-2026-09.md`](codex-claude-goal-harness-2026-09.md) | Codex / Claude 的 goal：harness 层如何承载（2026-09） |
| [`goal-context-governance-2026-09.md`](goal-context-governance-2026-09.md) | Goal 上下文治理：业界做法调研（2026-09） |
| [`2026-10-19-deepseek-search-fetch-api.md`](2026-10-19-deepseek-search-fetch-api.md) | DeepSeek 官方 Search & Fetch API（2026-10-17 上线）：端点/字段/算子/计费/QPS/错误逐条核对，与博查现状对照，以及 web_search 内置适配器 `type: deepseek` 的落点与刻意未做的部分 |
| [`2026-10-28-coding-agent-peer-evaluation.md`](2026-10-28-coding-agent-peer-evaluation.md) | 2026-10 Seelex、Codex、Claude Code、Cursor、Copilot、Devin、Qoder、OpenHands、Aider、Antigravity CLI 的同级路线、强弱、适用场景与当前 Codex agent 评价 |
| [`websearch-provider-design-research-2026-08.md`](websearch-provider-design-research-2026-08.md) | websearch 通用接入设计模式调研（2026-08） |
