# Seele Bridge

## 模块定位

`seelebridge` 是 Seelex 与 Seele v0.0.8 的防腐层。它把 Seele Agent/Engine/Tool/MCP/WorkPlan/Storage/Permission 能力包装成 Seelex 可装配、可限制和可测试的 Runtime，同时隔离上游 API 变化。

## 文件结构

| 文件 | 职责 |
|---|---|
| `runtime.go` | Runtime 创建、工具注册、账号/Provider、权限和回调入口。 |
| `scoped_tools.go` | 受项目根限制的 read/grep/glob/write/edit/bash 工具。 |
| `project_scope.go` | 根路径 canonicalization、read/write/workdir resolve。 |
| `pathgate.go` | `seele.yaml` path zone allow/ask/deny 规则。 |
| `plugins.go` | Seele Tool holder 的 Plugin 定义与激活适配。 |
| `mcp.go` | MCP attach/detach/refresh/status 和 breaker channel。 |
| `branch.go` | WorkPlan branch runtime、account 路由和 branch event。 |
| `plan.go` | adjacency/edge、cycle detection、topological order。 |
| `plan_tool_provider.go` | `plan_load` 严格 JSON schema/description 装饰器。 |
| `plan_preflight.go` | 隔离的强制 `plan_load` 请求和 provider `tool_choice` 注入。 |
| `plan_authority.go` | 原子 PlanAct scope、preflight 内部调用能力和 authority 生命周期。 |
| `storage.go` | Seele session store 与旧 nested workspace store 兼容。 |
| `config.go` | 简化账号 YAML 与 role fallback。 |
| `trace.go` | Seele tracer 类型别名和构造。 |

## Runtime 生命周期

1. `NewRuntime` 加载账号、创建 Agent/ChatClient、初始化 scope 和 MCP 状态。
2. `RegisterBuiltins` 注册 scoped tools 与 WorkPlan provider；不能同时暴露绕过 scope 的同名原始工具。
3. Application 通过 `BindProjectRoot` 限定当前 session 的工具范围。
4. Plugin Manager 调用 Define/Activate、AttachMCP 等接口改变可见能力。
5. Tool/Plan callbacks 投影到 Application Event/Snapshot。
6. `Shutdown` 关闭 MCP、Agent 和后台资源。

## ProjectScope 与 PathGate

ProjectScope 先把用户路径解析为 canonical absolute target，再验证它位于绑定 root。read 需要目标存在；write 允许目标尚不存在但父级必须安全；workdir 必须是目录。PathGate 在 scope 内进一步给出 allow/ask/deny 意图。

二者不能互相替代：scope 防越界，gate 表达策略。

scoped shell 在 POSIX 使用 bash/sh；Windows 使用系统 PowerShell 的绝对路径，并强制 `-NoProfile -NonInteractive`，避免 PATH 命中 WSL shim、用户 profile 或交互启动导致工具超时。所有平台都把 `cmd.Dir` 固定为 ProjectScope 解析后的目录。

Windows GUI 构建还会以 `SysProcAttr.HideWindow` 启动 scoped shell，避免每次 `bash` 工具调用闪现命令行窗口；stdout、stderr 与退出码仍通过工具结果返回。

## Plan branch

## Effort PlanPolicy

`Runtime.RegisterBuiltins` makes `plan_*` available at startup; Plan is not a standalone Plugin. For Medium, High, and Max, `PreparePlan` performs an isolated preflight request that forces `tool_choice=plan_load` before Application forwards the original request to ReAct. Before delegating `plan_load` to Seele, the bridge normalizes either the canonical object-keyed DAG or an LLM-friendly `nodes[]` / `edges[]` form into Seele's canonical JSON, then validates the current effort policy: Medium is a maximum four-node serial chain, High is capped at three concurrent branches, and Max permits every currently runnable node in the loaded plan to run concurrently.

The preflight tool schema exposes only the canonical object-keyed DAG: node specs belong under `nodes`, and edges are source-keyed target arrays. The adapter retains narrowly scoped recovery compatibility for legacy node entries with `id`/`key`, edge entries with `from`/`source` and `to`/`target`, explicit edge-chain strings, adjacency targets written as `{ "to": "id" }`, and referenced top-level node specs containing only `input` and optional `kind`. It never guesses a missing edge source or target, and rejects unrelated top-level fields such as `item`; invalid references return a pre-execution `plan_load` error and can use the bounded corrective retry path.

The preflight and recovery prompt templates live in [`internal/promptassets`](../internal/promptassets/README.md). Runtime supplies only the current `PlanPolicy` limits to those templates; prompt prose is not embedded in `seelebridge` Go code.

`PrepareReplan` uses the same isolated, forced `plan_load` path for an explicitly selected recovery. It receives only the objective, old Plan, failure and completed-node evidence; it atomically replaces the WorkPlan but never calls `plan_run` itself.

Recovery planning is protected by a process-wide guard: by default at most two concurrent operations, six replan operations per minute, and six actual provider requests per minute. Its metrics are safe to expose in Application snapshots; a corrective retry is permitted only after a pre-execution `plan_load` validation failure.

When a Medium, High, or Max preflight succeeds, Application wraps the
canonical WorkPlan and original request in the current-turn
`<!-- seelex:plan-context:v1 authority=preflight-loaded -->` envelope,
parallel to Skill context. It is rewritten to the original input before
session persistence. Application acquires a request-ID-bound `PlanActScope`
before preflight. Its private context is the only caller allowed to load the
Plan; after a successful load it promotes to authority. Runtime then removes
`plan_load` and `plan_clear` from model-visible tools and retains guards for
both stale handlers. A second request cannot enter the scope until its owner
releases it when ChatStream returns; the explicit, guarded `PrepareReplan`
recovery path therefore has `plan_load` available after a `plan_run` failure.

每条 branch 必须携带 `PlanBranchBinding`，包括 session/workspace/account/trace/plan/node IDs。两条 fork 路径统一走 Seele ForkCoordinator，默认 fail-fast；best-effort 只有显式配置才可启用。账号选择按 role 与 seed 确定，避免并发分支共享不可控状态。

## 兼容性原则

- 上游 Seele 已有能力优先薄适配，不复制实现。
- 上游类型只在 bridge 内集中出现；Application/frontend 使用自己的 DTO。
- 升级 Seele tag 时先运行本包测试，重点审查 Tool schema、History、WorkPlan callback、MCP 和 permission API。

## Review 指南

- 是否存在未经过 ProjectScope 的文件/shell 工具。
- Windows shell 是否继续使用显式系统路径和 non-interactive 参数，超时后是否能终止。
- bind/unbind 是否对并发 tool call 有确定快照语义。
- plan provider 是否只装饰 schema，不替换 framework handler。
- branch runtime/账号是否按 binding 隔离，fail-fast 是否仍为默认。
- MCP attach/detach 失败是否留下半连接状态或 goroutine。
- 配置 fallback 是否可能选择 disabled/错误 role 账号。

## 测试

```text
go test ./seelebridge -count=1 -timeout=120s
go vet ./seelebridge
```

项目边界重点在 `project_scope_test.go`/`runtime_test.go`，Plan 在 `plan_test.go`，存储兼容在 `storage_test.go`。
