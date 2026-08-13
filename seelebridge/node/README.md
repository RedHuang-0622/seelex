# seelebridge/node — 节点子代理执行域

## 模块定位

承载 `plan kind:agent` 节点的子代理执行包装与节点级提示词装配。主要调用方：

- 根包 `plan_factory.go` 的 `buildNode`（`node.NewAgentNode`）——plan_load 构造节点；
- `fork/` 域 `buildForkPlan` 经 `Deps.NodeFactory` 复用同一构造，生成 fork DAG 的 agent 节点；
- 根包 `agent_node.go` 门面与 `node_deps.go`（Deps 注入）。

## 职责与非职责

职责：

- `AgentNode.Run`：节点作用域注入 → worktree 开启 → 节点级 PromptBlocks 注入 → 节点 Session 执行 → 终态打点 → merge-back → worktree 收尾；
- 节点级 PromptBlocks 的 ctx 注入/读取（`WithNodePromptBlocks` / `NodePromptBlocksFromContext`）；
- 执行预算（`NodeBudgetInfo`）、子代理章程渲染（`NodeSubagentCharter`）、skill 匹配（`MatchNodeSkills`）、分支角色判定（`RoleForPlanBranch`）。

非职责：

- 不持有会话/账号池/任务注册表/父证据（全部经 `Deps` 回调）；
- 不实现 worktree 生命周期（`worktree/` 域）；
- 不做 plan 调度与事件（`plan/` 域）。

## 目录或文件结构

| 文件 | 职责 |
|---|---|
| `agent_node.go` | `Deps`、`AgentNode`、`Run`/`mergeBack`、`NodeScopeFor`、`RoleForPlanBranch`、`NodeSubagentCharter`、`MatchNodeSkills`、`WithNodePromptBlocks` |

## 核心实现

`Deps` 是全部运行时能力的函数字段集合（当前 15 项：工厂、binding、worktree 三件套、会话注册/终态、父证据、merge-back、预算、PromptBlocks、Tracer），由根包 `Runtime.nodeDeps()` 闭包注入。

`AgentNode.Run` 生命周期：`scope()`（惰性解析，plan_run 时 binding 已冻结）→ `BeginNodeWorktree`（RoleSubAgent）→ `WithNodeScope` → `AppendNodePhase(running)` → `WithNodePromptBlocks` → `factory.NewAgent` → `RegisterNodeSession` + `Chat` → `CompleteSubagentNode` → `mergeBack`（失败也执行，幂等）→ `FinishNodeWorktree`/`ReleaseNodeWorktree`。

## 数据流或生命周期

`plan_load`（buildNode）→ 构造 `AgentNode`（scope/blocks 闭包延迟解析）→ `plan_run` 触发 `Run` → 会话与工具活动经 deps 回根包 → merge-back 写 Runtime mailbox → 主会话下一次 ChatStream 前注入。

## 依赖方向

`node` → `internal/model`、`plan`、`worktree`、`skill`、`seelexctx`、`internal/promptassets`。**禁止反向依赖 seelebridge 根包**（这是拆包打破循环依赖的硬约束）。

## 并发、存储、安全或错误语义

- `Run` 同步执行，取消经 ctx（fork 超时/用户停止由根包级联）；
- merge-back 不因 `Chat` 失败而跳过（长时间静置场景已积累的 Findings/Decisions 不丢）；
- 不做持久化；子代理树/快照由根包 `session/` 域持有；
- 节点不触碰主会话锁——回传只经 `Deps.EnqueueSubagentContext`。

## 扩展方式

- 新增节点行为：改 `Run` 或新增 `Deps` 回调；
- 新增预算维度：扩展 `NodeBudgetInfo` 与根包 `Runtime.nodeBudget`；
- 新增提示词块：在 `NodeSubagentCharter`/`MatchNodeSkills` 侧扩展，保持"单一权威契约"。

## Review 指南

- `Deps` 是否保持"函数字段"，没有偷偷引回根包类型；
- scope/blocks 惰性解析是否保留（plan_load→plan_run 冻结语义）；
- charter 是否仍为单一块（不再拆碎）；
- `TaskID` 是否只作绑定元数据、不进 prompt。

## 测试与验证

单元测试应留在本包（当前由根包 `agent_node_test.go`/`node_scope_test.go` 经兼容别名覆盖运行时方法）。验证：

```text
go test ./seelebridge/... -count=1
go build ./...
```
