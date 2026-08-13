# seelebridge 薄桥化与 Runtime 装配件拆分方案

日期：2026-08-13
状态：方案（未实施）
目标：方案 B——`seelebridge` 根目录只保留 `runtime.go`（组合根）+ `ports.go`（RuntimePort 纯委托），其余全部下沉分包；Runtime 由"上帝模块"收敛为"装配件组合根"。

## 0. 前置

当前工作区处于"门面/别名已删除、全量测试通过、**尚未提交**"状态。实施前先提交该绿状态存档，避免两天的改动悬空。

## 1. 现状（根目录 24 个非测试文件）

| 文件 | 职责混叠 | 归属 |
|---|---|---|
| `runtime.go` | Runtime 结构 + 装配（保留） | 根（组合根） |
| `ports.go` | RuntimePort 实现（保留） | 根（纯委托） |
| `agent_node.go` | 节点执行/会话/树/task/worktree/plan 六域 | `node/`（Coordinator） |
| `context_components.go` | 装配器/窗口/记忆接线（17 方法） | 并入 `runtime.go` |
| `branch.go` | plan branch 路由 + 账号 | `plan/`（路由）或 `account/` |
| `registry.go` | 工具注册表状态 | `tools/` |
| `scoped_tools.go` | scoped 工具注册门面 | `tools/` |
| `todo_tool.go` | todo/task 工具门面 | `task/` |
| `worktree.go` | worktree 门面 | `worktree/` |
| `fork_tool.go` | fork 注册门面 | `fork/` |
| `mcp.go` | MCP Manager 实现（13 方法） | `mcp/` |
| `scheduler.go` | 定时任务 actor + 门面 | `scheduler/` |
| `docker.go` | docker 探针/恢复 | `mcp/`（或 `internal/docker/`） |
| `plugins.go` | plugin 定义/激活 | `plugin/`（顶层模块） |
| `accounts.go` | 账号选择/路由 | `account/` |
| `actor.go` | 可见性/父证据投影 | 并入 `ports.go` |
| `node_scope.go` | NodeScope 上下文助手 | `node/` |
| `node_tool_result.go` | 节点工具结果/工作区信息 | `node/` |
| `plan_factory.go` | buildNode/nodeFactory | `plan/` |
| `history_search.go` | 历史检索（1 方法） | `tools/` 或 `session/` |
| `telemetry_diagnostic.go` | 诊断遥测 | `internal/telemetry/` |
| `fork_deps.go` / `node_deps.go` / `task_tools_deps.go` / `tools_deps.go` | Deps 闭包接线 | 各归属域内 |

## 2. Runtime 装配件拆分设计

Runtime 保持"此处即上帝"——它是组合根，但**只组装、不实现业务**：

```text
type Runtime struct {
    accounts   *account.Manager      // 账号选择（新建 account/）
    tools      *tools.Registry       // 工具注册表（迁 registry.go）
    plan       *plan.Executor        // plan 域
    fork       *fork.Tool            // fork 编排（已拆）
    node       *node.Coordinator     // 节点编排（新建，接 agent_node.go）
    session    *session.SubagentSessions / SubagentTree / SubagentContextActor
    worktree   *worktree.WorktreeManager
    task       *task.TaskRegistry
    scheduler  *scheduler.State
    mcp        *mcp.Manager
    plugin     *plugin.Manager
    ...（filesystem/sandbox/projectScope 已是域组件）
}
```

约束：

- 每个域 Manager 拥有自己的状态与锁，Runtime 只持有引用；
- 跨域协作一律走 Deps 闭包（`node.Coordinator` 经 deps 拿 session/task/worktree/plan，`fork.Tool`/`tools.Router`/`task.Tools` 已是先例）；
- `ports.go` 的每个方法 ≤3 行，纯委托到对应 Manager；
- `NewRuntime` 是唯一的装配点（构造 Manager → 注入 deps → RegisterBuiltins 注册工具）。

## 3. agentnode 是否独立成模块（探索结论）

证据：`node/` 当前依赖 `seelex/internal/promptassets`（internal，禁止跨模块）与 `seelex/seelebridge/internal/model`（internal），以及公开的 `seelex/skill`、`seelex/seelexctx`、同模块的 `plan`/`worktree`。

结论：**现阶段保留为 `seelebridge/node` 子包（方案 B），不独立成 Go module**。

- internal 约束：独立 module 无法 import `internal/promptassets`、`internal/model`，必须先提升为公共包或迁入 node 模块——属前置重构，收益与成本不匹配；
- 收益再评估：seelebridge 本身就是"省上下文的桥层"，node 独立成 module 并不能减少主二进制加载的上下文（最终仍 link 进同一产物），真正收益只是依赖治理与独立版本，对内部桥层不划算；
- 未来路径（若 seelebridge 超 3 万行再触发）：① `internal/promptassets` → 公共 `seelex/promptassets`；② 共享类型（NodeScope/SeelexNodeInput/NodeWorktree）下沉 node 模块或 dto；③ node module 依赖 `seelexctx`/`skill` 的发布版本；④ 用 replace 做本地开发，对齐 Seele 的 v0.1.x 发布节奏。

## 4. 各模块 README 关系说明（统一模板）

每个域 README 增加固定小节「与其它域的关系」，关系图如下，各模块引用对应边：

```text
plan ──(agent 节点)──▶ node ──▶ session（子代理会话/上下文）
  ▲                      │
  │ fork 构造 DAG         ├─▶ worktree（独立工作区）
  └──── fork ◀───────────┤
                         └─▶ task（task 注册表/终态）
subagent = node 域产生的子代理执行体；fork 编排其并行；plan 描述 DAG；node 是执行内核。
```

每模块 README 应写清：

- `node/README.md`：node 是 plan DAG 中 agent 节点的执行内核；fork 构造的 DAG 复用 node；node 经 deps 使用 session/worktree/task；subagent 即 node 产生的子会话执行体。
- `fork/README.md`：fork 把"并发子代理"编排成 plan DAG；节点类型是 node；结果经 session 合并回主会话。
- `plan/README.md`：plan 是 DAG 描述与调度；agent 节点委托 node 执行；fork 是 plan 的编程式特例。
- `session/README.md`：session 承载子代理会话/树/merge-back；node 是 producer，fork/plan 是消费者。
- `worktree/README.md`：worktree 为 node 提供隔离工作区；生命周期由 node 编排、根包接线。
- `task/README.md`：task 是 worktable 状态面；node 完成/失败打点、fork 幂等登记均写 task。

## 5. 执行阶段

| 阶段 | 内容 | 验证 |
|---|---|---|
| P0 | 提交当前绿状态存档 | `go build ./...` + 全量测试 |
| P1 | `node.Coordinator`：agent_node.go 六域方法迁入 node/，deps 注入 | `go list -deps` 无环 + node 测试 |
| P2 | `mcp/` + `scheduler/` + `internal/docker/` + `plugin/` 四域拆分 | 每域 `go build` + 相关测试 |
| P3 | 小文件归位：registry→tools、todo_tool→task、worktree→worktree、fork_tool→fork、accounts→account、node_scope/node_tool_result→node、plan_factory→plan、history_search、telemetry_diagnostic | 每批全绿 |
| P4 | ports.go 收敛 + actor/context_components 并入；Runtime 字段全部 Manager 化 | contract 接口实现逐方法核对 |
| P5 | 各模块 README 补「与其它域的关系」；测试归位（白盒随包、联调进 `seelebridge_test`/`e2e`） | e2e readme/link 检查 + 全量 + live smoke |

## 6. 风险

- `node.Coordinator` 迁移量最大（六域 14+ 方法），deps 接口要先定稿再动；
- 独立模块探索的结论依赖 internal 约束，未来提升 promptassets 会影响 `internal/` 目录审计；
- ports.go 收敛时 contract.RuntimePort 方法签名不得变化（adapters 已按 dto 对齐）；
- 测试归位与 readme 关系段同步做，避免再次出现"实现迁走、文档/测试滞留根目录"。
