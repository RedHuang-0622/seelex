# seelebridge/node — 节点子代理执行域

## 模块定位

承载 `plan kind:agent` 节点的子代理执行包装与节点级提示词装配。主要调用方：

- 根包 `plan_factory.go` 的 `buildNode`（`node.NewAgentNode`）——plan_load 构造节点；
- `fork/` 域 `buildForkPlan` 经 `Deps.NodeFactory` 复用同一构造，生成 fork DAG 的 agent 节点；
- 根包 `agent_node.go` 门面与 `node_deps.go`（Deps 注入）。

## 与其它域的关系

```text
plan ──►(agent 节点)──► node ──► session（子代理会话/上下文）
  │                        │
  │                        ├──► worktree（独立工作区生命周期）
  │                        └──► task（task 注册表终态打点）
  └──► fork（构造 DAG 复用 node；subagent = node 产生的子会话执行体）
```

subagent 是 node 域产生的子代理执行体；fork 编排其并行；plan 描述 DAG；
node 是执行内核（经 Coordinator.Deps 使用 session/worktree/task）。

## 职责与非职责

职责：

- `AgentNode.Run`：节点作用域注入 → worktree 开启 → 节点级 PromptBlocks 注入 → 节点 Session 执行 → 终态打点 → merge-back → worktree 收尾；
- 第一视角与语义结果（`Deps.RecordNodeStage`/`RecordNodeResult`）：spawn 阶段在
  `Run` 内记录（goal/会话 ID），turn/tool 阶段经 telemetry StageHook 记录，result
  阶段与预定义语义结果（`NodeSemanticResult`，对象结构由 seelex 制定、非 subagent
  自拟）在 `mergeBack` 内记录并投入语义结果队列；
- 节点级 PromptBlocks 的 ctx 注入/读取（`WithNodePromptBlocks` / `NodePromptBlocksFromContext`）；
- 执行预算（`NodeBudgetInfo`）、子代理章程渲染（`NodeSubagentCharter`）、skill 匹配（`MatchNodeSkills`）、分支角色判定（`RoleForPlanBranch`）。

非职责：

- 不持有会话/账号池/任务注册表/父证据（全部经 `Deps` 回调）；
- 不实现 worktree 生命周期（`worktree/` 域）；
- 不做 plan 调度与事件（`plan/` 域）。

## 目录或文件结构

| 文件 | 职责 |
|---|---|
| `agent_node.go` | `Deps`、`AgentNode`、`Run`/`mergeBack`、`withWorktreeUnmergedNotice`、`NodeScopeFor`、`RoleForPlanBranch`、`NodeSubagentCharter`、`MatchNodeSkills`、`WithNodePromptBlocks` |
| `coordinator.go` | `SessionPort`/`TreePort`/`TaskPort` 接口、`Coordinator`（含阶段日志与语义结果委托） |

## 核心实现

`Deps` 是全部运行时能力的函数字段集合（当前 15 项：工厂、binding、worktree 三件套、会话注册/终态、父证据、merge-back、预算、PromptBlocks、Tracer），由根包 `Runtime.nodeDeps()` 闭包注入。

`AgentNode.Run` 生命周期：`scope()`（惰性解析，plan_run 时 binding 已冻结）→ `BeginNodeWorktree`（RoleSubAgent）→ `WithNodeScope` → `AppendNodePhase(running)` → `WithNodePromptBlocks` → `factory.NewAgent` → `RegisterNodeSession` + `Chat` → `CompleteSubagentNode` → `mergeBack`（失败也执行，幂等）→ `FinishNodeWorktree`/`ReleaseNodeWorktree`。

收尾失败分两类处理：rebase/审批/merge 失败与 `Chat` 失败一样让节点失败（现场保留）；而 `worktree.IsUncommittedChanges` 判定的"子代理未提交改动"只降级为**产出末尾的显式警告**（`withWorktreeUnmergedNotice`）——节点按 Chat 结果判定成功、不 `Release`（现场保留供人工检查或补提交）、并补记 `worktree_unmerged` 阶段事件。这样单个子代理忘记执行收尾协议不会让 workplan fail-fast 取消同批兄弟节点、丢掉它们的产出。

## 数据流或生命周期

`plan_load`（buildNode）→ 构造 `AgentNode`（scope/blocks 闭包延迟解析）→ `plan_run` 触发 `Run` → 会话与工具活动经 deps 回根包 → merge-back 写 Runtime mailbox → 主会话下一次 ChatStream 前注入。

## 依赖方向

`node` → `internal/model`、`plan`、`worktree`、`skill`、`seelexctx`、`internal/promptassets`。**禁止反向依赖 seelebridge 根包**（这是拆包打破循环依赖的硬约束）。

## 并发、存储、安全或错误语义

- `Run` 同步执行，取消经 ctx（fork 超时/用户停止由根包级联）；
- 收尾失败分流：只有 rebase/审批/merge 这类"改动/工作区不可用"的失败才让节点失败；"未提交改动"（`worktree.IsUncommittedChanges`）降级为产出中的 `[收尾警告]` 前缀段（含现场路径），节点仍成功——警告只加在返回给调用方的产出文本上，`mergeBack`/`NodeSemanticResult` 仍用原始结论，语义结果不被污染；
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

单元测试应留在本包（当前由根包 `agent_node_test.go`/`node_scope_test.go` 经兼容别名覆盖运行时方法；收尾降级的红灯用例见根包 `agent_node_uncommitted_test.go`，用真实 worktree 复现"子代理留下未提交改动"）。验证：

```text
go test ./seelebridge/... -count=1
go build ./...
```
