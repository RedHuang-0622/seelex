# Runtime 拆分审查报告（2026-08-10）

> 状态：审查完成，拆分方案待实施（实施另开分支，不污染 main）。
> 关联模块：`seelebridge`（Runtime 组合根）、`application/core`（Service）。

## 1. 背景与触发

merge-back 并发审查（A：mailbox 不丢 / B：父证据累积写回 / C：超时仍回传）
已完成并提交（`subagentContext` actor 为拆分第一步）。审查过程中确认
`seelebridge.Runtime` 与 `application/core.Service` 均为「上帝对象」：

- `Runtime`：46 个方法、约 50 个字段、横跨 40+ 文件、8 个职责域；
- `Service`：130 个方法，`serviceState` 虽按域分组嵌入，但共享一把 `mu`，
  生命周期/会话/plan/task/prompt 耦合在同一把锁上。

## 2. Runtime 职责域盘点

| 职责域 | 代表字段 | 现状并发模型 | 主要文件 |
|---|---|---|---|
| 账号/模型/工具注册 | `pool`/`completer`/`registry`/`accountSpecs` | pool 自锁 + branchMu | accounts.go / registry.go |
| 主会话 | `session`/`sessionHooks`/`mainHistory` | sessionMu / mainSessionMu / mainHistoryMu | runtime.go / storage.go |
| Plan 执行 | `planPolicy`/`planEvents`/`planProvider`/`replanGuard`/`agentFactory` | planPolicyMu / planRunMu + 事件 actor | plan*.go / replan_guard.go |
| 子代理执行（含 fork） | `subagentContext`（已拆）/`subagentTree`/`nodeSessions*`/`wt`/`nodeStarted` | actor + nodeSessionsMu + worktreeState.mu | subagent_context.go / subagent_tree.go / agent_node.go / worktree.go / fork_tool.go |
| task 注册表 | `tasks` | 已是 actor | task_registry.go |
| 周期任务 | `scheduler` | 已是 actor | scheduler.go |
| 项目作用域/文件/沙箱 | `projectScope`/`filesystem`/`sandbox`/`dockerProbe` | filesystem actor + scope 自锁 | project_scope.go / filesystem_actor.go / sandbox.go |
| 上下文控制 | `window`/`ctxStore`/`historyRouter`/`turnArchiver`/`projectKnowledge` | 5 组 RW 锁 | context_components.go |
| 插件/权限/MCP/遥测 | `pluginDefs`/`permission`/`MCPStack`/`tracer` | pluginMu + actor | plugins.go / mcp.go / trace.go |

## 3. 拆分方案（按依赖强度分 4 步）

### Step 1：子代理执行域收敛（部分完成）

`subagentContext` actor 已拆出。剩余候选：

- `subagentSessions`：`nodeSessions`/`nodeSnapshots`/`nodeGoals`/
  `nodeContextSnapshots`/`nodeToolArchivers` + `nodeSessionsMu` → 独立 actor
  （读多写少：actor + atomic 读取面，与 `subagentContext` 同模式）；
- `worktreeManager`：`wt` + worktreeState + begin/finish/approve/release →
  独立组件（git 子进程天然串行，actor 化成本低）；
- `subagentTree` 已是自锁 state，保持现状。

### Step 2：Plan 执行域

`planExecutor`：policy / binding / events / runID / provider / preflight /
replan + `agentFactory` 收敛。事件已是 sink actor，主要把
`planRunMu`/`planPolicyMu`/`branchMu` 挪进组件。

### Step 3：上下文控制域

`contextAssembler`：window / ctxStore / historyRouter / turnArchiver /
projectKnowledge，5 组锁 → 1 个组件。

### Step 4：application 侧 Service 拆锁

把 chat / session / task / plan 四组生命周期从共享 `mu` 拆成各自状态锁 +
CSP 通道（`task.changed`/`worktable.changed`/`subagent.changed` 已是通道，
天然支持）。风险最高，单独排期并配快照一致性测试。

## 4. 波及文件范围

| 层 | 文件 | 改动类型 |
|---|---|---|
| seelebridge 组件 | `subagent_sessions.go`（新）、`worktree_manager.go`（新）、`plan_executor.go`（新）、`context_assembler.go`（新） | 新增；从 agent_node.go / worktree.go / plan*.go / context_components.go 迁出字段与方法 |
| seelebridge 门面 | `runtime.go`（收窄为组合根+门面）、`actor.go`、`agent_node.go`、`fork_tool.go` | Runtime 方法改为委托组件；字段删除 |
| seelebridge 事件面 | `plan_events.go`/`subagent_events.go`/`task_registry.go`（已是 actor，保持） | 少量接口调整 |
| application 边界 | `application_adapters.go`、`application/contract/ports.go` | 适配器转发到组件；端口接口尽量不变（兼容性关键） |
| application core | `chat.go`/`session_history.go`/`service_interaction.go`/`service_snapshot.go`/`task_service.go`/`work_table.go` | Step 4 拆锁；CSP 消费者已存在 |
| 测试 | `seelebridge/*_test.go`、`application/core/*_test.go` | 构造方式从 `Runtime{...}` 改为组件级；fake 端口同步 |
| 文档 | `seelebridge/README.md`、`docs/gui/modules/*.md`、`docs/arch/*` | 模块边界与数据流更新 |

## 5. 兼容性约束

`application/contract/ports.go` 的 `RuntimePort`（约 40 个方法）是 application
唯一消费面。拆分时保持 Runtime 实现该接口（内部委托），application 层在
Step 1–3 零改动；Step 4 才需要动 application。

## 6. 风险与建议

- 风险：Step 4 拆 `Service.mu` 风险最高（130 方法、快照一致性依赖单锁）；
  Step 1–3 是纯 seelebridge 内部重构，风险低。
- 顺序：Step 1（subagentSessions + worktreeManager，与 merge-back 域同族，
  收益直接）→ Step 2 → Step 3 → Step 4。
- 新组件模式：与 `subagentContext` actor 保持一致——channel 命令 + 单
  goroutine 串行处理可变状态，atomic 快照做无锁读取面，Close 幂等。

## 7. 验收

- `go build ./seelebridge/... ./application/...` 通过；
- `go test -race ./seelebridge/ -skip '^Test(Worktree|ResolveNodePath|GlobSkipsHeavyDirs|ProjectScopeResolvesOnlyInsideBoundRoot|RuntimeProjectScopedToolsUseBoundProject|ScopedBashPublishesDiagnosticStages|BashDiagnosticObserverPanicDoesNotBreakTool)'` 通过；
- `go test ./application/... -count=1` 通过；
- `application/contract/ports.go` 无破坏性改动（Step 1–3）。
