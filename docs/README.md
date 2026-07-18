# Seelex 文档索引

> 文档按类型分为 5 类。设计/架构文档保留原始内容，其余文档已更新至 v0.0.2。

---

## 📐 arch/ — 架构与设计

| 文档 | 说明 |
|------|------|
| [`architecture-and-flaws.md`](arch/architecture-and-flaws.md) | 架构说明书与已知硬伤清单（初稿） |
| [`design-decisions-mcp-storage.md`](arch/design-decisions-mcp-storage.md) | MCP 中间件从 CAD 专属→通用→存储解耦的设计推演 |
| [`mcp-call-chain-flowchart.md`](arch/mcp-call-chain-flowchart.md) | Agent 调用 MCP 全链路函数流 + 熔断事件通道 |
| [`context-improvement-plan.md`](arch/context-improvement-plan.md) | Context 包拆分为 snapshot/provider/compactor/merger 方案 |

## 🛠 cad/ — CAD 方案（⚠️ 大部分已过时）

| 文档 | 状态 | 说明 |
|------|:----:|------|
| [`cad-architecture-overview.md`](cad/cad-architecture-overview.md) | ❌ 废弃 | 旧三支柱 CAD 架构，被通用 mcpstack 取代 |
| [`cad-command-stack.md`](cad/cad-command-stack.md) | ❌ 废弃 | 旧 commandstack 包设计，已删除→mcpstack |
| [`cad-freecad-executor.md`](cad/cad-freecad-executor.md) | ❌ 废弃 | 旧自研 Python MCP Server，已删除→用现成 |
| [`cad-mcp-bridge.md`](cad/cad-mcp-bridge.md) | ❌ 废弃 | 旧自研 MCP 客户端方案，已改用框架 Provider |
| [`cad-infrastructure-complete.md`](cad/cad-infrastructure-complete.md) | ❌ 废弃 | 旧双栈集成指南，保留作历史对照 |

当前 CAD 定位：FreeCAD 是 Plugin（与 WebSearch 同级），`freecad/` 包仅做参数验证。

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
