# Seelex 文档索引

> 文档按长期维护设计、研发过程和研究资料组织。GUI 的当前事实以 `gui/` 目录为准。

---

## 📐 arch/ — 架构与设计

| 文档 | 说明 |
|------|------|
| [`architecture-and-flaws.md`](arch/architecture-and-flaws.md) | 架构说明书与已知硬伤清单（初稿） |
| [`design-decisions-mcp-storage.md`](arch/design-decisions-mcp-storage.md) | MCP 中间件从 CAD 专属→通用→存储解耦的设计推演 |
| [`mcp-call-chain-flowchart.md`](arch/mcp-call-chain-flowchart.md) | Agent 调用 MCP 全链路函数流 + 熔断事件通道 |
| [`context-improvement-plan.md`](arch/context-improvement-plan.md) | Context 包拆分为 snapshot/provider/compactor/merger 方案 |
| [`skill-effort-architecture.md`](arch/skill-effort-architecture.md) | Effort system prompt 与 Skill 用户上下文的当前实现设计 |
| [`agent-workbench-architecture.md`](arch/agent-workbench-architecture.md) | DSL 对话卡片、Agent E2E 与 Workspace 沙盒未来总体架构 |

## 🧭 product/ — 产品规划

| 文档 | 说明 |
|------|------|
| [`agent-workbench/prd.json`](product/agent-workbench/prd.json) | Agent Workbench 机器可读 PRD、里程碑、验收标准与指标 |

## GUI — Wails 客户端设计与审查

| 文档 | 说明 |
|------|------|
| [`gui/README.md`](gui/README.md) | GUI 架构总览、模块边界和维护规则 |
| [`gui/decisions.md`](gui/decisions.md) | Wails、协议、reducer、keyed DOM、Markdown 和 CI 决策记录 |
| [`gui/ci-and-testing.md`](gui/ci-and-testing.md) | GUI 分支 CI、测试分层和本地等价命令 |
| [`gui/code-review.md`](gui/code-review.md) | 功能打点到详设、源码位置、测试证据的审查追溯矩阵 |
| [`gui/modules/dsl-card-runtime.md`](gui/modules/dsl-card-runtime.md) | JSON DSL 卡片在 Conversation 中的协议、渲染与安全设计 |
| [`gui/modules/agent-e2e-interaction.md`](gui/modules/agent-e2e-interaction.md) | 确定性 Core scenario、Playwright 与 Wails smoke 设计 |
| [`gui/modules/workspace-sandbox.md`](gui/modules/workspace-sandbox.md) | 右栏 Files/Changes/Artifacts 与后端路径沙盒设计 |

## 📓 devlog/ — 研发过程

| 文档 | 说明 |
|------|------|
| [`test-report.md`](devlog/test-report.md) | 测试报告（已更新至 7ed72fb） |
| [`finish-review.md`](devlog/finish-review.md) | 机械设计方向最终审查 + 后续重构更新 |
| [`code-changes.md`](devlog/code-changes.md) | 代码变更摘要（2026-07-17） |
| [`2026-07-17-seelex-runtime-plugin-refactor-front-review.md`](devlog/2026-07-17-seelex-runtime-plugin-refactor-front-review.md) | Plugin 重构前置审查 |
| [`2026-07-17-tui-application-core-separation-front-review.md`](devlog/2026-07-17-tui-application-core-separation-front-review.md) | TUI/Application 分离前置审查 |
| [`2026-07-17-tui-application-core-separation-plan.md`](devlog/2026-07-17-tui-application-core-separation-plan.md) | TUI 分离实施方案 |

## 🔬 research/ — 调研报告

| 文档 | 说明 |
|------|------|
| [`agent-frontend-design-research.md`](research/agent-frontend-design-research.md) | AI Agent 前端界面 + DSL 卡片渲染设计调研 |
| [`approve-research.md`](research/approve-research.md) | Approve 节点选型（OpenCode vs Claude Code vs Seele） |

## 📊 根目录

| 文档 | 说明 |
|------|------|
| [`feature-instrumentation.md`](feature-instrumentation.md) | 功能打点表与北极星指标 |
