# Seele Bridge

## 模块定位

`seelebridge` 是 Seelex 与 Seele v0.0.8 新装配模型的防腐层。它把 Seele 的
`accountpool`（P2C 账号租约）、`agent`（NewWithComponents 装配）、`session`
（主会话与节点子代理会话）、`tools`（Registry）、`workplan`（codec 导入 +
事件投影）、`event`/`telemetry` 能力包装成 Seelex 可装配、可限制和可测试的
Runtime，同时隔离上游 API 变化。

## 文件结构

根目录只保留两个非测试文件（薄桥化终极形态）：

| 文件 | 职责 |
|---|---|
| `runtime.go` | Runtime 组合根：NewRuntime 装配全部 Manager、RegisterBuiltins、上下文接线（Assembler/Controller/Compressor）、账号路由、可见性策略、Deps 闭包注入 |
| `ports.go` | application/contract 端口实现：task/plan/子代理树/节点/MCP/plugin/调度器/历史检索/todolist/actor 消息边界的逐行委托与类型别名 |

## 子包结构

`seelebridge` 根包是组合根 + 端口文件；领域实现按模块下沉到子包：

| 子包 | 内容 |
|---|---|
| `security/` | `ProjectScope` 项目根 containment + `PathGate` allow/ask/deny + `CommandSandbox` shell 隔离（见 `security/README.md`） |
| `fs/` | `FileSystem` 文件系统 actor（写路径分片串行化，见 `fs/README.md`） |
| `plan/` | Plan 执行域：`Executor`/`ToolProvider`/`PlanPolicy`/`PlanPreflight`/`PlanNodeEvent`/`ReplanGuard`/`SeelexNodeInput`/`PlanBranchBinding`/`BuildNode` 等（见 `plan/README.md`） |
| `task/` | `TaskRegistry` actor、`TaskRecord`/`TodoItem` 共享 DTO、`TaskTerminalProvider`/`Tools`（见 `task/README.md`） |
| `fork/` | `fork_subagents` 纯类型、summary 节点与执行编排 `Tool`（见 `fork/README.md`） |
| `node/` | `kind:agent` 节点执行域：`AgentNode`/`Coordinator`/`ScopeAssembler`/预算/charter/skill 匹配/工具结果归档（见 `node/README.md`） |
| `session/` | 子代理会话注册表、fork 子代理树与父证据/merge-back actor（见 `session/README.md`） |
| `worktree/` | 子代理 worktree 生命周期管理器（见 `worktree/README.md`） |
| `account/` | 账号装配与选择：ClientFor/RegisterAccounts/ResolveForBranch/ForRole/ByName（见 `account/README.md`） |
| `mcp/` | MCP 服务器生命周期 Manager：provider 懒创建/breaker/lazy 登记/工具重挂载（见 `mcp/README.md`） |
| `plugin/` | 插件 include/exclude 可见性过滤 Manager（见 `plugin/README.md`） |
| `scheduler/` | 定时周期任务 actor：白名单命令/prompt 任务/状态快照（见 `scheduler/README.md`） |
| `tools/` | scoped 工具 Router、`RegistryState`（内联工具+权限门控）、websearch（见 `tools/README.md`） |
| `internal/model/` | 各域共享的纯类型层（`AccountSpec`/`AccountRole`/`NodeScope`，无运行时依赖） |
| `internal/config/` | 简化账号 YAML 加载（`Config`/`AccountLimits`/`Load`） |
| `internal/docker/` | Docker 守护进程自动恢复（探针/daemon-down 判定/恢复提示，见 `internal/docker/README.md`） |
| `internal/stream/` | 流式账号 Completer 适配（`NewStreamingCompleter`） |
| `internal/telemetry/` | 内存遥测追踪器/生命周期钩子/诊断钩子构造（见 `internal/telemetry/README.md`） |

根包对子包保持单向依赖；子包禁止 import seelebridge 根包，跨域协作一律走
Deps 闭包或端口接口注入（`node.Coordinator`、`fork.Tool`、`tools.Router`、
`mcp.Manager` 均为先例）。公开 DTO 统一走 `application/contract/dto`。

## 子包结构

`seelebridge` 根包是组合根 + 公共 facade；领域实现按模块下沉到子包：

| 子包 | 内容 |
|---|---|
| `security/` | `ProjectScope` 项目根 containment + `PathGate` allow/ask/deny + `CommandSandbox` shell 隔离（见 `security/README.md`） |
| `fs/` | `FileSystem` 文件系统 actor（写路径分片串行化，见 `fs/README.md`） |
| `plan/` | Plan 执行域：`Executor`/`ToolProvider`/`PlanPolicy`/`PlanPreflight`/`PlanNodeEvent`/`ReplanGuard`/`SeelexNodeInput`/`PlanBranchBinding` 等（见 `plan/README.md`） |
| `task/` | `TaskRegistry` actor、`TaskRecord`/`TodoItem` 共享 DTO、`TaskTerminalProvider`（见 `task/README.md`） |
| `fork/` | `fork_subagents` 纯类型、summary 节点与执行编排 `Tool`（见 `fork/README.md`） |
| `node/` | `kind:agent` 节点子代理执行域：`AgentNode`/`Deps`/预算/charter/skill 匹配（见 `node/README.md`） |
| `session/` | 子代理会话注册表与父证据/merge-back 两个 actor（见 `session/README.md`） |
| `worktree/` | 子代理 worktree 生命周期管理器（见 `worktree/README.md`） |
| `tools/websearch/` | `web_search` 工具注册与账号池配置加载（见 `tools/websearch/README.md`） |
| `internal/model/` | 账号等各域共享的纯类型层（`AccountSpec`/`AccountRole`，无运行时依赖） |
| `internal/config/` | 简化账号 YAML 加载（`Config`/`AccountLimits`/`Load`；根 facade 装配细节） |
| `internal/storage/` | legacy shard 会话存储（`SessionStore`/`NestedSessionStore`） |
| `internal/stream/` | 流式账号 Completer 适配（`NewStreamingCompleter`） |
| `internal/telemetry/` | 内存遥测追踪器/生命周期钩子构造（`NewTracer`/`NewLifecycleHook`） |

根包经 `plan_aliases.go`/`task_aliases.go`/`fork_aliases.go`/`node_aliases.go`/`security_aliases.go`/
`model_aliases.go`/`config_aliases.go`/`storage_aliases.go`/`telemetry_aliases.go`
重导出子包符号（`seelebridge.PlanEdge`、`seelebridge.TaskRecord`、
`seelebridge.CommandSandbox`、`seelebridge.Message` 等）保持公共 API 兼容。
子包遵循"域组件禁止 import seelebridge 根包"的依赖规则，根包→子包单向依赖。

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

Windows GUI 构建还会以 `SysProcAttr.HideWindow` 启动 scoped shell，避免每次 `bash` 工具调用闪现命令行窗口；若已安装 Git for Windows，优先使用其 `bash.exe`，使 `pwd && ls -la` 等模型常用 Bash 语义可直接执行；否则才回退到系统 PowerShell/cmd。stdout、stderr 与退出码仍通过工具结果返回。

## Plan branch

Plan 分支执行走 Seele v2 装配模型：`plan_run` 经 `codec.Import` 导入 DAG 后由
`workplan.NewFromPlan(plan, bridge.NewAgentFactory(...))` 执行，每个
`kind:agent` 节点获得**独立 Session**（`SeelexAgentNode` 包装：注入 NodeScope
与节点级 PromptBlocks——目标/父证据/预算），天然并行隔离。账号选择按
role + branchID 走确定性 hash（`ResolveAccountForBranch`），显式 binding
AccountID 直接 pin，不占用主链路租约。节点执行事实经 `event.Sink` 投影为
`PlanNodeEvent`（queued/running/终态），不再使用框架分支运行时回调。

### Plan 执行域组件（`planExecutor`）

Plan 策略、分支绑定、run ID、事件通道、重规划护栏、审批门与子代理工厂的
状态收进 `planExecutor`（`plan_executor.go`），`Runtime` 保留公开方法委托
（`SetPlanPolicy`/`SetPlanBranchBinding`/`SetPlanApprovalGate`/
`SetPlanNodeCallback`/`PlanNodeEventChannel`/`SetEventPersister`/
`SetEventErrorHandler`/`ReplanMetrics`/`PrepareReplan`），
`application/contract/ports.go` 不变。组件不持有 `*Runtime`：
`accounts`/`loadPlanDefinition`/`dispatch`/`nodeFactory` 以 deps 闭包注入，
`planToolProvider` 持有 `*planExecutor` 引用；节点工厂（`buildNode`/
`nodeFactory`）仍留在 Runtime，因为 `SeelexAgentNode` 依赖 Runtime 的
节点作用域与子代理上下文服务。

### `fork_subagents` 的结果边界

`fork_subagents` 是上述 Plan 的轻量入口：它程序化构造 `start → N 个 agent → summary`，随后在工具调用内同步执行 `runPlan`。因此外层工具在整个子代理 DAG 和 summary 节点结束前不会返回；诊断运行状态时应读取 Plan 的节点事件、工具活动和子会话快照，而不是只看外层工具卡的等待文案。

summary 当前按节点 ID 拼接前驱输出并写入 `final_output`。它适合小型、结构化交付，不是无界 transcript 传输通道：大结果可能超过 provider 的单条上下文预算。调用方必须将“结果被省略/过大”视为未读取的证据，并通过可分页的结果引用或节点详情读取原文；在可靠的有界摘要与引用映射完成前，不能凭外层结果声称已审查完整子代理产出。

### 子代理上下文继承与重试复用

**上下文继承（缓存友好）**：节点子代理会话经 `nodeScopeAssembler` 合并节点
块（charter/skills）外，还继承主代理的稳定上下文块——project（项目语义）、
stack（now using 栈顶）与按当前查询召回的 memory——插在节点块之前。这些
块在同一会话内内容稳定，模型请求前缀可命中 DeepSeek 硬盘缓存；同时子代理
能读到主代理的项目知识/任务栈/相关记忆，不再只有 goal + 父证据。

**重试状态（B3 生产者）**：`bindSubagentTask` 重新命中既有 task 时，终态
（completed/failed）重开为 `retry`（`RetryCount` 自增，worktable 显示
`RETRY n`），节点真正启动时再转 `running`（计数保留）。`validateTaskTransition`
允许终态 → retry，禁止 retry → queued/pending（避免 re-fork 注册树节点时
覆盖重试计数）。

**结果复用（省 token）**：若结果返回失败需要重试（`final_output` 被截断或
`read_tool_result` 失败），且全部子代理都命中“既有已完成 task + 子代理树
保留完整输出”（`subagentTree.summaryFor`），`fork_subagents` 直接读回已保存
输出返回（`reused:true`），不再重新执行；task 经 retry 计数后回到 completed。
只有全部命中才短路；部分命中仍整体重跑（保守策略，避免 DAG 混合状态）。

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

## Runtime-owned projections

Runtime accepts value copies of Application visibility and parent-evidence
projections. Tool visibility and subagent prompt assembly read those local
copies only; neither path calls Application or the main session. Merge-back
uses a fixed-capacity Runtime mailbox. A full mailbox increments a diagnostic
drop count and never blocks a child agent.

项目边界重点在 `project_scope_test.go`/`runtime_test.go`，Plan 内核在
`plan_kernel_test.go`，账号池/流式租约在 `runtime_test.go`/
`stream_completer_test.go`，存储兼容在 `storage_test.go`。
