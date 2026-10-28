# `backend/engine/assemble` 迁移清单

日期：2026-10-26

状态：**BM-005 文件已分类；首个切片 `review_files` 注册归属已完成，其余文件尚未搬迁。**

机器事实源是
[`contract/runtime_migration_test.go`](../../contract/runtime_migration_test.go)
的 `runtimeAssemblyInventory`。新增生产文件若未登记目标归属，
`TestBM005RuntimeAssemblyProductionFilesAreClassified` 会失败。

## 迁移原则

1. `wiring/runtime` 只做构造、依赖注入、启动与关闭顺序，不成为新的领域包。
2. backend 领域包不得 import `backend/engine/assemble` 或未来的
   `wiring/runtime`；该边界由 `TestBM005DomainPackagesDoNotImportRuntimeAssembly`
   钉住。
3. Teamwork、Subagent、Conversation、Session 的跨域流程先抽稳定端口与用例，
   再迁入 Application；不允许整份 `runtime_*.go` 原样搬进新的上帝包。
4. 迁移期旧 `assemble` 路径保留同身份 alias / 一行转发，调用方按能力逐组切换。

## 文件分组

| 目标 | 当前文件 | 迁移边界 |
|---|---|---|
| `wiring/runtime` | `runtime.go`、`runtime_deps.go`、`runtime_bundle.go`、`runtime_transport.go`、`runtime_context.go`、`runtime_history.go`、`runtime_live.go`、`runtime_visibility.go`、`events_unified.go`、`strings.go`，以及 account / async / computer / MCP / node / plugin / project-board / schedule / task / tools 的 runtime 适配文件 | 只保留组合根、能力适配与生命周期顺序；不承载 WorkItem / Teamwork 业务判定。 |
| `wiring/registry` | `review_files.go` | **首个切片已完成**：工具名、schema、handler 与唯一 `review.RegisterShowFilesTool` 在 `backend/tool/review`；生产注册由 `wiring/registry` 直接调用域 helper，旧 assemble 入口为 D1 一行转发。 |
| `application/conversation` | `role_host_session.go`、`runtime_role_turn.go`、`runtime_role_prompt.go`、`runtime_role_plugins.go`、`runtime_role_user_spaces.go`、`runtime_agent_conversation.go` | 角色回合、上下文 scope、插件提示与请求租约归 Conversation 用例；Execution 只经端口。 |
| `application/session` | `runtime_session.go`、`runtime_session_release.go` | 会话激活、释放与 resident 生命周期归 Session 用例。 |
| `application/subagent` | `runtime_subagent*.go`、`runtime_unit_recovery.go` | fork 树、恢复、merge-back 与执行记录归 Subagent 用例。 |
| `application/teamwork` | `runtime_teamwork*.go`、`runtime_teammate*.go`、`runtime_workitem_execution.go` | Team plan、WorkItem、作业句柄、验收与会话桥接归 Teamwork 用例。 |
| `application/teamwork/workunit` | `workunit_*.go` | 执行单元组装、节点与团队记录归 WorkUnit 聚合。 |
| `backend/multiagent/teamwork` | `teamwork_job_namer.go` | 产品作业句柄命名策略属于 Teamwork 词汇。 |
| `wiring/runtime` | `teamwork_job_signals.go` | 上游单读者通道的扇出、订阅生命周期与停机顺序属于 runtime 基础设施，不维护 Teamwork 聚合不变量。 |

## 搬迁顺序

1. 先抽 WorkUnit / Teamwork 稳定端口，删除 Application 对 `*assemble.Runtime`
   具体类型的认识。
2. 再拆 Conversation、Session、Subagent 的跨域流程；每一步保持旧 `Service`
   方法转发与行为 parity。
3. 建立 `wiring/runtime`，迁入纯组合与适配文件；`backend/engine/assemble`
   降为 alias / 一行转发 shim。
4. 最后删除旧路径，并把 backend 内部依赖预算收紧到新图。

## 当前不做

- 不一次性移动全部 `runtime_*.go`；
- 不把 `runtime_teamwork.go` 整份搬进 Application；
- 不改变 WorkItem、worktree、Session、验收和恢复语义；
- 不在本轮创建空 `wiring/runtime` 占位包。

## 分类修正记录

| 日期 | 文件 | 原分类 | 新分类 | 理由 |
|---|---|---|---|---|
| 2026-10-26 | `teamwork_job_signals.go` | `backend/multiagent/teamwork` | `wiring/runtime` | 它管理 goroutine、订阅通道快照和 close 顺序，不拥有 WorkItem / TeamPlan 状态或判据。 |
| 2026-10-26 | `review_files.go` | `wiring/registry`（确认） | `wiring/registry` | 仅剩注册动作，已由 domain helper + wiring 调用完成。 |

## 已完成切片：review_files

- 新面：`backend/tool/review.RegisterShowFilesTool(ToolRegistrar, ShowFilesPort)`；
- 生产调用：`wiring/registry.RegisterReviewFilesTool`；
- 旧面：`backend/engine/assemble.RegisterReviewFilesTool`，D1 一行转发；
- 删除里程碑：BM-007；
- 行为保持：工具名、schema、描述、10 份上限、回执与落点端口未变。
