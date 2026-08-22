# Seelex 全仓库代码审阅（2026-08-22）

> 性质：一次性工作包（front-review）。本文档是对当前工作树（HEAD
> `bf750b8`）的全仓库分层代码审阅，包含分层解读、字符画架构图、数据流图、
> 技术设计决策解读，以及以 Tech Leader 视角提出的设计质询与解答。
> 本文属于阶段性审阅记录，不作为长期架构事实来源；长期架构以
> [`docs/arch/`](../arch/README.md) 为准。

## 1. 范围与方法

- 审阅范围：根装配层（`main.go`）、`application/`、`seelebridge/`、
  `seelexctx/`、`sessionstore/`、`plugin/`、`skill/`、`workspace/`、
  `session/`、`mcpstack/`、`internal/`、`tui/`、`gui/`、`e2e/`，
  以及上游依赖 `github.com/RedHuang-0622/Seele v0.1.2` 在本仓库的消费面。
- 统计口径：仓库共 394 个 Go 文件（231 个源码文件、163 个测试文件），
  源码约 4 万行；GUI 前端为 `gui/frontend/dist/` 下约 2.6 万行 JS/CSS。
- 方法：先读根 README/DESIGN 与 `docs/arch/` 基线，再逐模块读
  README 与核心实现，最后用 `go build ./...` 验证当前工作树可编译。
- 审阅结论的事实边界：代码与测试是“已实现”能力的最终事实来源；
  本文引用的行数、函数名、数据流均以当前工作树为准。

## 2. 文档导航

| 文档 | 内容 |
|---|---|
| [`01-layered-architecture.md`](01-layered-architecture.md) | 分层解读 + 字符画架构图 + 依赖方向 |
| [`02-data-flows.md`](02-data-flows.md) | 九条关键数据流图（主对话、协议、Plan/子代理、上下文、存储、扩展、账号、GUI、定时任务） |
| [`03-design-decisions.md`](03-design-decisions.md) | 关键技术设计决策解读（动机、实现位置、权衡） |
| [`04-tech-leader-qa.md`](04-tech-leader-qa.md) | Tech Leader 质询与解答（含风险定级） |
| [`verification.md`](verification.md) | 构建/统计验证记录 |

## 3. 总体结论摘要

### 设计强项

1. **分层与依赖方向清晰**：Hexagonal 风格，前端只消费
   `application` 的 Snapshot/Event/Action；`seelebridge` 作为上游 Seele
   的防腐层，上游类型不外泄；域子包之间通过接口/闭包注入协作，
   依赖构成 DAG。
2. **快照 + 事件增量协议**：`Application Snapshot` 是权威状态，
   Event 带 `seq/revision`，慢消费者走 `resync.required` 兜底；
   TUI/GUI 不各自维护业务状态机。
3. **存储的 immutable generation 语义**：manifest 原子切换、
   失败 generation 不可见、四后端（JSON/SQLite/PostgreSQL/Redis）
   逻辑语义一致。
4. **安全边界分两层**：ProjectScope 物理 containment +
   PathGate/PermissionGate 策略层；Windows shell 显式 PowerShell
   `-NoProfile -NonInteractive`。
5. **并发以 actor/CSP 为主**：task/session/subagentContext/scheduler
   均收敛为单消费者 mailbox，避免多把大锁互相嵌套。
6. **测试纪律**：163 个测试文件，e2e 用 scripted engine 走公开
   contract，不依赖真实 LLM；GUI 协议有 node --test 覆盖。

### 主要风险（详见 [04-tech-leader-qa.md](04-tech-leader-qa.md)）

| 级别 | 问题 | 位置 |
|---|---|---|
| P1 | `sessionstore.go` 2447 行，违反仓库自身“上帝文件”判据 | `sessionstore/sessionstore.go` |
| P1 | `main.go` 1143 行，内联大量产品工具 schema/handler | `main.go` |
| P2 | PathGate 对所有平台无条件小写化路径，Linux 大小写敏感语义偏差 | `seelebridge/security/pathgate.go` |
| P2 | GUI 前端 `dist/` 即源码，无 `src/` 与构建链，56KB `app.js` 靠 `patch_app.py` 补丁 | `gui/frontend/dist/` |
| P2 | TUI 覆盖率约 26%，前端交互回归主要靠手工 | `tui/` |
| P3 | 双轨事件（EventHub 快照轨 + sessionstore 事件库事实轨）并存 | `seelebridge/events.go` / `application/event/hub.go` |

## 4. 验证记录

- `go build ./...`：通过（exit 0）。
- 关键文件规模、Go 文件统计见 [`verification.md`](verification.md)。
