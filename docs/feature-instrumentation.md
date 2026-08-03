# Seelex 当前能力与质量基线

> 核对日期：2026-08-03<br>
> 版本语义：源码构建显示 `dev`；正式版本只由 Git tag 通过 ldflags 注入。<br>
> 文档边界：这里只记录当前主链路和已知缺口，路线设计另见 `docs/product/` 与一次性工作包。

## 当前主链路

Seelex 当前已经形成一条可离线验证的 coding-agent harness 主链路：用户输入由 TUI 或 Wails GUI 进入共享的 `application.Service`，Application 负责聊天、任务、计划、审批和会话用例，`seelebridge` 负责把这些产品语义适配到 Seele runtime，最终通过 Snapshot 与有序 Event 投影回前端。

核心能力包括：

- ReAct 执行循环与 `lite`、`medium`、`high`、`max` 四级 Effort；
- WorkPlan DAG 校验、拓扑执行、审批节点、子代理节点与任务终态协议；
- 父证据注入、独立子会话、并发 fork、结构化 merge-back 与主会话 mailbox；
- Context Window Policy、可逆压缩、工具结果外化和 ProjectKnowledge；
- OpenAI-compatible / Anthropic provider、P2C account pool、角色路由和 lease-until-EOF；
- Plugin、Agent Skills、MCP attach/detach、事务式切换与回滚；
- Human-in-the-loop permission / approval；
- JSON、SQLite、PostgreSQL、Redis 会话存储适配；
- Bubble Tea TUI 与 Wails/WebView GUI 共享 Application contract。

## Effort 的实际执行预算

`application/prompt/effort.go` 是当前事实来源。四档不仅改变 prompt，还同时约束 ReAct 循环、工具调用预算和 Plan 并发策略。

| Effort | MaxLoops | MaxToolCalls | Plan 约束摘要 |
|---|---:|---:|---|
| `lite` | 15 | 30 | 不主动扩大计划规模 |
| `medium` | 48 | 96 | 最多 4 个节点，强制串行 |
| `high` | 384 | 768 | 子代理最大并发 3 |
| `max` | 768 | 1536 | 由已加载 Plan 的可运行节点决定并发上限 |

这些数字属于执行安全预算，不代表模型能力评级。新增或调整 Effort 时，必须在同一 profile 中同时修改 prompt、loop、tool budget 与 plan policy，并更新契约测试。

## 可验证能力边界

### 已接入生产路径

- Application Core 不依赖具体前端，TUI/GUI 只消费 DTO、Snapshot 与 Event。
- Event 序号缺口或订阅背压会触发 resync，前端可重新拉取 Snapshot。
- `plan_load` 对节点、边和拓扑进行规范化与环检测；`plan_run` 投影节点生命周期。
- 子代理拥有独立 Session、NodeScope、预算和可见工具集；完成后经 `merger.MergeBack` 形成继承上下文块，通过 mailbox 在安全边界注入主会话。
- Provider 账号按角色进入池化调度，流式请求在 EOF 前持续占有 lease，避免并发超售。
- Plugin 切换失败会回滚 Tool、Skill 与 MCP 状态。
- 会话与项目 binding 使用 ID，而不是展示名称，存储适配保持项目作用域。
- 发布工作流构建 Windows/Linux/macOS CLI 与 Windows GUI，并审计私密配置和运行时缓存。

### 尚未达到的能力

- 没有 OS 级 sandbox、容器或 VM 隔离；当前是项目路径门禁，不能等同于安全沙箱。
- 没有 git checkpoint、`rewind` 或统一 undo 语义。
- 没有 repo map、embedding index 或语义代码检索。
- 没有 IDE 扩展与代码 diff 审阅界面。
- 子代理生命周期事件已有底层事实，但 TUI/GUI 的完整 SubAgentTree 仍未上线。
- GUI 仍是 Alpha，真实 WebView E2E 不是当前自动化发布门槛。

## 测试基线

2026-08-03 当前工作树的本地结果为：全仓 62.8%，`application/core` 75.6%，`seelebridge` 66.7%，`tui` 26.1%，`workspace` 68.4%。这组数字用于暴露测试分布，不作为稳定承诺；可通过 `go test ./... -covermode=count -coverprofile=coverage.out` 与 `go tool cover -func=coverage.out` 复核，CI 也会上传覆盖率明细。

仓库的质量门禁包括 Go build/test/vet、Linux race/coverage、GUI contract 与 Node tests、跨平台构建和 release archive safety。真实 provider smoke test 必须显式启用，普通 CI 不依赖网络或秘密配置。

当前最需要补强的测试面是：

- TUI 输入、粘贴、补全与事件交互；
- workspace 更深的故障注入与异常恢复；
- composition root 的无网络启动链路；
- frontmatter、Plan 输入的 fuzz target；
- Windows WebView 的真实 E2E。

## 更新规则

- “已支持”必须能定位到生产调用链和自动化测试；只有设计文档或 manifest 不算完成。
- 当前实现、历史记录和规划必须分开描述。
- 覆盖率必须给出命令、日期与关键包分布，不只公布全仓平均值。
- 版本、Effort、依赖和发布内容改变时，应同步更新 README、CHANGELOG、模块文档和契约测试。
- 不把 Wails 的 `production` build tag 解释为项目 production-ready。
