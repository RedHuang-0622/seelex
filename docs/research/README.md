# Research Documents

本目录存放外部项目、开源库、协议和技术方案的调研与选型比较。

调研应记录日期、问题、候选方案、平台支持、许可证、安全边界、维护活跃度、集成成本、来源链接和结论。调研结论不是已实现能力；采用后还需在架构文档、模块 README 和代码中落地。

## 文档索引

| 文档 | 说明 |
|---|---|
| [`2026-08-24-conversation-fork-research.md`](2026-08-24-conversation-fork-research.md) | 对话 fork（会话级）功能可行性调研：Codex fork 机制、session store 链路、requestID 前提核实、上下文六层模型与风险、事件流三轨盘点、边界场景 user story。**一期决策已定稿**（深拷贝 + 血缘 meta + 整帧继承，否决方向已标注；见「十、一期决策契约」）（2026-08-24） |
| [`2026-08-24-session-resource-granularity.md`](2026-08-24-session-resource-granularity.md) | 会话资源与锁粒度盘点：单例现状、M1 会话级已落地项、多会话解除单例的路径（2026-08-24） |
| [`2026-08-24-fork-subagent-recovery.md`](2026-08-24-fork-subagent-recovery.md) | fork 子代理异常中断状态恢复可行性调研：串行/执行者归因、checkpoint 原语、前置准备与现状对照（2026-08-24） |
| [`context-management-review.md`](context-management-review.md) | 上下文管理（继承/合并/压缩）实现审查 + 论文/博客理论依据调研（2026-08-20） |
| [`agent-frontend-design-research.md`](agent-frontend-design-research.md) | AI Agent 前端界面与 DSL 卡片渲染设计调研 |
| [`agent-harness-research-report.md`](agent-harness-research-report.md) | Agent harness 能力缺口与建议 |
| [`agent-market-research-2026-08.md`](agent-market-research-2026-08.md) | 2026-08 Agent 产品市场调研 |
| [`approve-research.md`](approve-research.md) | Approve 节点选型（OpenCode vs Claude Code vs Seele） |
| [`coding-agent-harness-comparison.md`](coding-agent-harness-comparison.md) | 编码 Agent harness 对比 |
