# 代码变更摘要

## 新增/修改/删除文件

| 文件 | 类型 | 说明 | 设计模式 |
|---|---|---|---|
| `docs/gui/architecture.md` | 新增 | 权威总体架构、数据流、并发与 generation 发布模型 | Ports and Adapters、Actor、Repository |
| `docs/gui/module_dotting.json` | 新增 | 机器可读模块职责、状态、接口、输入输出、路径和依赖 DAG | Registry、Dependency DAG |
| `docs/gui/schemas/*.schema.json` | 新增 | Snapshot/Event/Page/Error/Card/Generation/HTTP 跨模块契约 | Schema-first、Versioned DTO |
| `docs/gui/examples/*.json` | 新增 | 与 Schema 一一对应的合法 payload | Contract fixture |
| `docs/gui/api/*.md` | 新增 | HTTP、安全、分页、错误与快照语义 | Adapter、Idempotency、Optimistic concurrency |
| `docs/gui/recipes/*.md` | 新增 | generation 提交、回滚、重建和故障恢复 | Immutable generation、CAS |
| `docs/gui/modules/generation-store.md` | 新增 | 不可变 checkpoint repository 详设 | Repository、State machine |
| `docs/gui/modules/http-api-adapter.md` | 新增 | 规划 HTTP adapter 详设 | Ports and Adapters、DI |
| `docs/gui/modules/evidence-gated-dev-loop.md` | 新增 | RAG 证据门禁、人工评审、分层 baseline 与 Dev/E2E 反馈闭环 | Pipeline、Policy、Human-in-the-loop、Actor |
| `docs/gui/schemas/evidence-assessment.schema.json` | 新增 | 原子主张、证据绑定、readiness、gate 和人工处置契约 | Evidence graph、Versioned policy |
| `docs/gui/schemas/dev-iteration.schema.json` | 新增 | 需求→架构→详设→Dev→E2E run、work item 和反馈路由 | State machine、Traceability |
| `docs/gui/recipes/iterate-requirement-to-dev.md` | 新增 | 可恢复、有界的自迭代操作流程 | Generation baseline、Idempotency |
| `docs/gui/architecture-review.md` | 新增 | 并发、循环依赖、模块边界和解耦审查 | Static dependency review |
| `docs/gui/README.md`、`CHANGELOG.md`、`decisions.md` | 新增/修改 | 权威索引、设计变更与 ADR | Documentation governance |
| `docs/gui/modules/*.md` | 修改 | 统一实现/规划状态和权威架构链接 | Single source of truth |
| `docs_contract_test.go` | 新增 | Schema/示例/链接/模块 DAG 可执行验证 | Contract test、Graph validation |
| `docs/README.md`、旧架构/审查文档 | 修改 | 指向新的权威设计包并保留历史推演属性 | Compatibility documentation |

没有删除运行时代码，没有修改当前 Wails/Application 行为，也没有修改 `go.mod`/`go.sum`。

## API 变更

| API | 变更 | 兼容性 |
|---|---|---|
| 当前 Go/Wails API | 无运行时变更 | 完全兼容 |
| HTTP `/api/v2` | 新增规划契约，尚未实现 | 不构成当前公开运行时能力 |
| Protocol v2 Schema | 新增目标契约 | 与当前 v1 显式分离，不静默兼容 |
| Generation repository ports | 新增设计契约，尚未实现 | 不影响当前 Session store |

## 设计模式使用

| 模式 | 文件 | 效果 |
|---|---|---|
| Schema-first | `schemas/`、`examples/`、`docs_contract_test.go` | JSON 字段只有一个事实源 |
| Ports and Adapters | `architecture.md`、`modules/http-api-adapter.md` | Wails/HTTP 并列，领域层不依赖 transport |
| Actor | `architecture.md`、`modules/multi-session-pages.md` | session 内单写，跨 session 有界并行 |
| Repository + immutable generation | `modules/generation-store.md`、`recipes/` | 原子发布、可验证恢复和回滚 |
| Optimistic concurrency | `api/`、Generation 设计 | revision/generation CAS 防止覆盖 |
| Registry/DAG | `module_dotting.json` | 模块状态与依赖可自动审计 |
| Evidence gate | `modules/evidence-gated-dev-loop.md`、相关 Schema | 分离检索相关性、支持关系、充分度和人工状态 |
| Feedback routing | `dev-iteration.schema.json` | E2E 失败精确重开需求/架构/详设/Dev/Test 层 |

## 接口抽象

| 接口 | 计划实现方 | 使用方 |
|---|---|---|
| `GenerationRepository` / `GenerationWriter` | `session/generation` | Application SessionActor |
| `SessionRuntimeFactory` / `SessionScheduler` | composition root / application runtime | WorkbenchCoordinator |
| `WorkspacePort` | `workspace` | Application/Card action resolver |
| `HTTPHandler` dependencies | `transport/httpapi` adapters | HTTP routes/middleware |
| `gui.Application` | 当前 `application.Service` | Wails Bridge（现有接口保持） |
| `EvidenceRetriever` / `EvidenceAssessor` / `GatePolicy` | `evidence/*` adapters | DevIterationOrchestrator |
| `HumanReviewQueue` / `BaselinePublisher` | review/generation adapters | DevIterationOrchestrator |

## 循环依赖检查

- [x] 当前 Go import 图无直接环。
- [x] `module_dotting.json` 依赖图无未知节点和有向环。
- [x] 规划模块保持 Application-owned ports 与 composition-root 注入。
- [x] 审查已记录 Engine Hook、Context/MCP、Session callback 和 Approval observer 的概念回边及解耦方案。

## 增量验证

- `go test . -run '^TestGUI' -count=1 -v`：通过。
- Schema 编译、全部 JSON 示例、模块 DAG、模块文档和 GUI Markdown 本地链接：通过。
- `git diff --check`：通过。
- 定向 `-race`：未执行成功；Windows 环境缺少 `gcc`，必须由具备 CGO/C 编译器的 CI runner 完成。

## Commit 建议

```text
docs(gui): formalize workbench contracts

- define authoritative architecture and module dependency registry
- add versioned JSON schemas, validated examples and HTTP semantics
- document immutable generation operations and recovery recipes
- record concurrency, dependency and module-boundary review findings
- add executable documentation contract tests

Refs: GUI documentation governance, architecture review
```

用户已确认提交；本文件随上述 commit message 一并纳入提交。
