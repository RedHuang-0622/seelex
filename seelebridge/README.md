# Seele Bridge

## 模块定位

`seelebridge` 是 Seelex 与 Seele v0.0.8 新装配模型的防腐层。它把 Seele 的
`accountpool`（P2C 账号租约）、`agent`（NewWithComponents 装配）、`session`
（主会话与节点子代理会话）、`tools`（Registry）、`workplan`（codec 导入 +
事件投影）、`event`/`telemetry` 能力包装成 Seelex 可装配、可限制和可测试的
Runtime，同时隔离上游 API 变化。

## 文件结构

| 文件 | 职责 |
|---|---|
| `runtime.go` | Runtime 创建（composition root）、账号/Provider、回调入口。 |
| `subagent_events.go` | 子代理工具 middleware 与稳定 started/completed 投影。 |
| `accounts.go` | 账号池注册（`accountpool.P2CPool[agent.Completer]`）、同步/节点账号选择器与 `ResolveAccountForBranch`。 |
| `registry.go` | `tools.Registry` 装配与 `bridge.NewRegistryRuntime` 适配。 |
| `stream_completer.go` | 流式账号 Completer（lease-until-EOF，覆盖整条流直到 EOF/错误/Close）。 |
| `scoped_tools.go` | 受项目根限制的 read/grep/glob/write/edit/bash 工具。 |
| `project_scope.go` | 根路径 canonicalization、read/write/workdir resolve。 |
| `pathgate.go` | `seele.yaml` path zone allow/ask/deny 规则。 |
| `plugins.go` | 插件 include/exclude 可见性快照（`WithVisibilityPolicy` 输入）。 |
| `mcp.go` | MCP attach/detach/refresh/status 和 breaker channel。 |
| `branch.go` | 节点会话组件、branch/account 路由与 branch binding。 |
| `plan.go` | adjacency/edge、cycle detection、topological order。 |
| `plan_factory.go` | 产品 DSL → `codec.NodeSpec` → `node.Node`（agent/approve/function 等 kind）。 |
| `plan_events.go` | `event.Sink` 实现：执行事实入库 + 投影为 `PlanNodeEvent`。 |
| `plan_tool_provider.go` | `plan_load` 严格 JSON schema/description 装饰器。 |
| `plan_input_adapter.go` | canonical object DAG 归一化，并兼容可唯一推断的旧式顺序边列表。 |
| `plan_preflight.go` | 隔离的规划/重规划回合（独立 Completer 实例、无工具、HTTP 层强制 `tool_choice=plan_load`）。 |
| `plan_authority.go` | 原子 PlanAct scope、preflight 内部调用能力和 authority 生命周期。 |
| `agent_node.go` | `kind:agent` 节点子代理包装（注入 NodeScope + 节点级 PromptBlocks）。 |
| `node_scope.go` | 节点作用域 ctx 注入与读取（可见性/账号/装配器共享）。 |
| `context_components.go` | 节点会话 `SessionComponents` 上下文组件（Assembler/Processor 等）。 |
| `task_terminal.go` | task_complete/task_failed/task_needs_user_decision 工具注册。 |
| `storage.go` | Seele session store 与旧 nested workspace store 兼容。 |
| `config.go` | 简化账号 YAML 与 role fallback。 |
| `trace.go` | `telemetry.NewMemoryTracer` / `NewLifecycleHook` 构造。 |

## Runtime 生命周期

1. `NewRuntime` 加载账号并注册进 `accountpool.P2CPool[agent.Completer]`，装配
   `bridge.NewAccountCompleter`（同步）与 `streamingAccountCompleter`（流式，
   租约覆盖整条流），初始化 scope 和 MCP 状态。
2. `RegisterBuiltins` 注册 scoped tools 与 plan/task 工具 provider；不能同时
   暴露绕过 scope 的同名原始工具。
3. Application 通过 `BindProjectRoot` 限定当前 session 的工具范围。
4. Plugin Manager 调用 Define/Activate 维护 include/exclude 可见性快照；
   每次请求经 `bridge.WithVisibilityPolicy` 过滤可见工具集。
5. Tool/Plan 执行事实经 `planEventSink`（`event.Sink`）投影为
   `PlanNodeEvent`，订阅者（Application）实时更新 Plan 状态与前端快照。
6. `Shutdown` 关闭 MCP、账号池和后台资源。

权限门始终保存一份 manual 基线配置。`Runtime.SetFullAccess(true)` 只把当前 checker 切到 `full_access`，`SetFullAccess(false)` 重新构造 manual checker；`Runtime.FullAccess()` 是 Application Snapshot 的权威状态来源。即使 CLI 以 `-permission full_access` 启动，composition root 也先装配 manual 规则和审批桥，再打开覆盖层，因此 GUI 可以安全切回 manual。

主 Session 可通过 `AttachHistoryRouter` 独立装配 `sessionstore.DurableHistory`；指定恢复 ID 时 `NewMainSessionWithID` 同时用它作为框架 Session identity 和 durable key。该路径不读取或覆盖 `SessionContextStore` 的 application state blob。

子代理节点通过 `NodeScope.Role == RoleSubAgent` 识别。工具 middleware 发布 `running/success/error`，worktree 编排发布 `worktree_creating/rebasing/merging`；阶段事实沿用 Plan binding，并在存在 session ID 时写入 `agent.runtime` Location。

## ProjectScope 与 PathGate

ProjectScope 先把用户路径解析为 canonical absolute target，再验证它位于绑定 root。read 需要目标存在；write 允许目标尚不存在但父级必须安全；workdir 必须是目录。PathGate 在 scope 内进一步给出 allow/ask/deny 意图。

二者不能互相替代：scope 防越界，gate 表达策略。

scoped shell 在 POSIX 使用 bash/sh；Windows 使用系统 PowerShell 的绝对路径，并强制 `-NoProfile -NonInteractive`，避免 PATH 命中 WSL shim、用户 profile 或交互启动导致工具超时。所有平台都把 `cmd.Dir` 固定为 ProjectScope 解析后的目录。

Windows GUI 构建还会以 `SysProcAttr.HideWindow` 启动 scoped shell，避免每次 `bash` 工具调用闪现命令行窗口；stdout、stderr 与退出码仍通过工具结果返回。

## Plan branch

Plan 分支执行走 Seele v2 装配模型：`plan_run` 经 `codec.Import` 导入 DAG 后由
`workplan.NewFromPlan(plan, bridge.NewAgentFactory(...))` 执行，每个
`kind:agent` 节点获得**独立 Session**（`SeelexAgentNode` 包装：注入 NodeScope
与节点级 PromptBlocks——目标/父证据/预算），天然并行隔离。账号选择按
role + branchID 走确定性 hash（`ResolveAccountForBranch`），显式 binding
AccountID 直接 pin，不占用主链路租约。节点执行事实经 `event.Sink` 投影为
`PlanNodeEvent`（queued/running/终态），不再使用框架分支运行时回调。

## Effort PlanPolicy

`Runtime.RegisterBuiltins` makes `plan_*` available at startup; Plan is not a standalone Plugin. For Medium, High, and Max, `PreparePlan` performs an isolated preflight request that forces `tool_choice=plan_load` before Application forwards the original request to ReAct. Before delegating `plan_load` to Seele, the bridge normalizes either the canonical object-keyed DAG or an LLM-friendly `nodes[]` / `edges[]` form into Seele's canonical JSON, then validates the current effort policy: Medium is a maximum four-node serial chain, High is capped at three concurrent branches, and Max permits every currently runnable node in the loaded plan to run concurrently.

The optional `plan_load` tool schema exposes only the canonical object-keyed DAG: node specs belong under `nodes`, and edges are source-keyed target arrays. The adapter retains narrowly scoped recovery compatibility for legacy node entries with `id`/`key`, edge entries with `from`/`source` and `to`/`target`, explicit edge-chain strings, adjacency targets written as `{ "to": "id" }`, and referenced top-level node specs containing only `input` and optional `kind`. It never guesses a missing edge source or target, and rejects unrelated top-level fields such as `item`; invalid references return a pre-execution `plan_load` error and can use the bounded corrective retry path.

The preflight and recovery prompt templates live in [`internal/promptassets`](../internal/promptassets/README.md). Runtime supplies only the current `PlanPolicy` limits to those templates; prompt prose is not embedded in `seelebridge` Go code.

`PrepareReplan` uses the same isolated, forced `plan_load` path for an explicitly selected recovery. It receives only the objective, old Plan, failure and completed-node evidence; it atomically replaces the WorkPlan but never calls `plan_run` itself.

Recovery planning is protected by a process-wide guard: by default at most two concurrent operations, six replan operations per minute, and six actual provider requests per minute. Its metrics are safe to expose in Application snapshots; a corrective retry is permitted only after a pre-execution `plan_load` validation failure.

Medium, High, and Max no longer create a mandatory planning preflight. Their
`PlanPolicy` only constrains a voluntarily loaded DAG. Normal requests enter
the primary ReAct loop directly, so no Plan authority envelope is injected
into user input and no request-scoped PlanAct lease is acquired. The optional
Plan remains a visible checklist; normal project-scoped tools perform the
actual work, while prompt policy keeps `plan_run` out of the main workflow.

每条 branch 必须携带 `PlanBranchBinding`，包括 session/workspace/account/trace/plan/node IDs。节点账号选择按 role 与 seed 确定性解析（`resolvePlanBranchAccount`/`ResolveAccountForBranch`），显式 binding 优先 pin，避免并发分支共享不可控状态。

## 兼容性原则

- 上游 Seele 已有能力优先薄适配，不复制实现；新代码只依赖新根模块
  （agent/session/tools/workplan/seelectx/accountpool/event/telemetry/errors）。
- 上游类型只在 bridge 内集中出现；Application/frontend 使用自己的 DTO。
- 升级 Seele tag 时先运行本包测试，重点审查 Tool schema、Session 装配、
  WorkPlan 事件投影、MCP 和账号池 API。

## Review 指南

- 是否存在未经过 ProjectScope 的文件/shell 工具。
- Windows shell 是否继续使用显式系统路径和 non-interactive 参数，超时后是否能终止。
- bind/unbind 是否对并发 tool call 有确定快照语义。
- plan provider 是否只装饰 schema，不替换 framework handler。
- 流式租约是否覆盖整条流（EOF/错误/Close 均幂等释放），规划回合是否使用独立 Completer 实例。
- 节点账号/作用域是否按 binding 隔离，确定性 hash 是否稳定。
- MCP attach/detach 失败是否留下半连接状态或 goroutine。
- 配置 fallback 是否可能选择 disabled/错误 role 账号。
- Full Access 是否仍是 manual 基线上的可逆覆盖，且开启时 Application 会释放已经等待的审批请求。

## 测试

```text
go test ./seelebridge -count=1 -timeout=120s
go vet ./seelebridge
go test -race ./seelebridge -count=1
```

项目边界重点在 `project_scope_test.go`/`runtime_test.go`，Plan 内核在
`plan_kernel_test.go`，账号池/流式租约在 `runtime_test.go`/
`stream_completer_test.go`，存储兼容在 `storage_test.go`。
