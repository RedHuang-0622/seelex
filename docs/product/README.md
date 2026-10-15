# Product Documents

本目录存放 PRD、用户旅程、里程碑、验收标准和产品指标。产品文档描述“为什么做、交付什么、如何验收”，不替代代码模块 README 或架构设计。

每份规格应包含版本/状态、目标用户、范围与非范围、验收标准、依赖和已知风险。规划能力必须标识为未实现，并链接实际代码或实施工作包。

## 文档索引

| 文档 | 状态 | 说明 |
|---|---|---|
| [`roadmap-2026.md`](roadmap-2026.md) | 规划草案（非现状） | 产品规划（2026-08 → v1.x）：一句话定位、形态三段（头/身/手）、3 个 Persona、MVP 定义、四阶段路线图与商业化策略。**阶段 2/3 内容为规划，未实现**；阶段 3 的团队协作/企业版/云服务已加"须按个人 agent 口径重新评估"注记 |
| [`pmstory.md`](pmstory.md) | 决策叙事 | 基于历史文档的产品/工程决策复盘：各阶段的权衡、代价与出处 |
| [`agent-workbench/prd.json`](agent-workbench/prd.json) | 机器可读 PRD | Agent Workbench 的里程碑、验收标准与指标；`out_of_scope` 明确排除远程 SSH Workspace 与云端多人协同 |

## 定位口径（单一事实来源）

产品定位以根 [`README.md`](../../README.md) 与 [`roadmap-2026.md`](roadmap-2026.md) §1 为准，二者一致：

> **Seelex 是一个个人 agent（personal agent）**——本地优先、多模型可路由、上下文可逆治理的开源 agent harness。
> 它不是团队协作的工作台：teamwork 是"一个人把一件事拆开并行做"的手段，不是产品形态。

`roadmap-2026.md` 中与上述口径冲突或尚待裁决的条目（阶段 3 的团队协作、企业版、云端服务），
裁决状态见 [`docs/2026-10-15-product-architecture-pre-design/global-review.md`](../2026-10-15-product-architecture-pre-design/global-review.md) 的 D01/D06，
文档侧梳理见 [`docs/2026-10-15-product-direction-doc-review/README.md`](../2026-10-15-product-direction-doc-review/README.md)。
