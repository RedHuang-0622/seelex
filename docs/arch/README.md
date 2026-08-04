# Architecture Documents

本目录存放跨模块、长期有效的架构事实与设计原则，例如依赖方向、协议语义、并发模型、存储模型和已知结构性缺陷。

适合放置：

- 多个代码模块共同遵循的边界。
- 已接受且仍有效的架构决策和演进路线。
- 需要长期维护的调用链、状态机和安全模型。

不适合放置单一模块的当前实现细节（写入模块 README）、一次性实施计划（写入日期工作包）或外部方案调研（写入 `docs/research/`）。文档必须标明哪些是当前实现、哪些是目标设计。

## 文档索引

| 文档 | 说明 |
|------|------|
| [`seele-v2-runtime-architecture.md`](seele-v2-runtime-architecture.md) | Seelex 使用 Seele v0.1.1 远程模块边界的稳定架构（迁移完成） |
| [`architecture-and-flaws.md`](architecture-and-flaws.md) | 架构说明书与已知硬伤清单 |
| [`design-decisions-mcp-storage.md`](design-decisions-mcp-storage.md) | MCP 中间件从 CAD 专属→通用→存储解耦的设计推演 |
| [`mcp-call-chain-flowchart.md`](mcp-call-chain-flowchart.md) | Agent 调用 MCP 全链路函数流 + 熔断事件通道 |
| [`context-improvement-plan.md`](context-improvement-plan.md) | Context 包拆分为 snapshot/provider/compactor/merger 方案 |
| [`skill-effort-architecture.md`](skill-effort-architecture.md) | Effort system prompt 与 Skill 用户上下文的当前实现设计 |
| [`agent-workbench-architecture.md`](agent-workbench-architecture.md) | DSL 对话卡片、Agent E2E、Workspace 沙盒与多会话并行总体架构 |
| [`subagent-visibility-design.md`](subagent-visibility-design.md) | 子代理详情查看系统设计方案 |
| [`session-snapshot-liveness.md`](session-snapshot-liveness.md) | Session、Snapshot、Runtime 投影与子代理回流的数据流及无死锁边界 |
