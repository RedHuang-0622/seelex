# M8 前置审查报告

## 需求摘要

接入 Seele 已完成的 ForkCoordinator 能力：Seelex 为每个计划分支绑定独立的
role/account/session/workspace runtime，接收分支生命周期事件，并在 application、TUI、GUI
与端到端场景中正确展示和验证。

## 前置结论

Seele 的 M1--M7 接口满足 M8 接入前提：`WorkPlanTool` 已可配置 branch event hook 和
runtime resolver；自动 DAG fork 与显式 fork 都经 `ForkCoordinator`；默认策略为 fail-fast。

但 Seelex 当前实现尚不能直接进入“完成”状态，M8 必须先处理下列两个设计点：

1. `ResolveAccount(pool, role)` 每次只返回该角色的第一个账号。它不能为同角色的并行
   分支提供稳定、可复现的账号租约；更不能验证双分支确实使用了不同的 factory/account。
2. Seele resolver 的签名只有 `func(branchID string)`；Seelex 当前也没有在 `plan_run` 前把
   当前 session、workspace、入口节点和用户显式账号选择冻结为一个 plan-scoped binding。
   因而不能正确区分入口/汇合节点与 fork 子节点，且无法满足 runtime metadata 的
   session/workspace 要求。

## 影响文件清单

| 文件 | 修改类型 | 位置 | 原因 |
| --- | --- | --- | --- |
| `seelebridge/config.go` | 修改 | account role 解析、账号选择 | 增加按 branch ID 的稳定账号选择和显式账号覆盖；保留 role fallback。 |
| `seelebridge/runtime.go` | 修改 | `Runtime`、plan setter | 定义 Seelex DTO；冻结 plan binding；为每个 branch 构造私有 `ChatClient`、单账号 pool 和 `AgentFactory`。 |
| `seelebridge/runtime_test.go` | 修改 | runtime/config 测试 | 证明分支 factory/account 隔离，不修改共享 `r.client`。 |
| `application/ports.go` | 修改 | `RuntimePort` | 暴露“开始一次 plan run 前冻结 binding”的窄接口。 |
| `application/chat.go` | 修改 | `plan_run` 生命周期、branch event handler | 运行前传入 session/workspace；映射 `queued/started/completed/failed/canceled/panicked` 并重算进度。 |
| `application/state.go` | 修改 | `NodeStatus` | 增加 `queued`、`canceled`、`panicked` 并保持 snapshot 深拷贝。 |
| `application/*_test.go` | 修改 | PlanState 测试 | 验证生命周期顺序、失败收敛和 snapshot/event 发布。 |
| `main.go` | 修改 | runtime 与 application 装配 | 绑定 branch event callback。 |
| `tui/plan.go`、测试 | 修改 | 图标/颜色/完成计数 | 展示 queued、canceled、panicked；只有 completed/skipped 计入成功进度。 |
| `gui/frontend/dist/app.js` | 修改 | `renderIncremental` | 对 plan snapshot 更新即时重绘。 |
| `gui/frontend/dist/styles.css` | 修改 | plan node 样式 | 为 queued/canceled/panicked 定义可辨识颜色。 |
| `e2e/scenario/runner_test.go` | 修改 | parallel-plan 场景 | 以真实 WorkPlanTool 验证两个分支使用各自 factory，事件有序且最终收敛。 |

## 依赖分析

- 上游：Seele 的 `builtin.WorkPlanTool`、`forkexec.BranchRuntime/Event`、
  `builtin.NewChatAgentFactory` 与 `api.ChatClient`。
- 边界：`seelebridge` 将 Seele `forkexec.Event` 转为 Seelex DTO；application 不直接导入
  Seele 的 `forkexec` 包。
- 下游：`application.Snapshot.Runtime.Plan` 是 TUI、GUI 和 E2E 读取的唯一状态来源。

## 推荐方案

### 1. Plan-scoped binding

在 `plan_run` 工具开始前，由 application 将当前 `Session.ID`、已绑定 workspace、入口节点、
用户显式选择账号（若有）冻结为 `seelebridge.PlanBranchBinding`。Runtime resolver 只读取这份
不可变 binding；它不从全局可变 client/pool 读取当前值。普通 fork 子节点默认使用
`RoleSubAgent`；入口和以下划线开头的显式汇合/高上下文节点使用 binding 指定的主角色。

### 2. 独立账号与 client（重点）

每个 branch runtime 按以下顺序取得账号：

1. 若用户显式选择账号，所有该计划分支固定使用该账号；
2. 否则按节点角色筛选账号；
3. 同角色多个账号以稳定 hash(`plan ID`, `branch ID`) 选择，不使用共享 pool 的 round-robin
   指针；
4. 角色缺失时沿用既有 `subagent/goalplan -> agent -> first available` 回退。

随后以该账号的 `BaseURL/APIKey/Model/Provider` 创建一个新的 `api.ChatClient`，并只给它
配置包含该账号的私有 `api.AccountPool`，再构造 `builtin.NewChatAgentFactory(client)`。
绝不调用共享 `r.client.SetProviderFilter`，也不把 factory 写回 graph node 或 workflow metadata。
这样两个同时运行的 branch 不会争用 provider filter、round-robin 索引或 HTTP client 配置。

账号不可用时应返回一个会在 `Chat` 时产生清晰错误的 factory；错误交给 Seele 的默认
fail-fast 策略处理，而不是回退到共享 client。

### 3. 生命周期与界面

事件映射为：`queued -> NodeQueued`、`started -> NodeRunning`、`completed -> NodeCompleted`、
`failed -> NodeFailed`、`canceled -> NodeCanceled`、`panicked -> NodePanicked`。每次映射都更新
snapshot revision 并发布 `EventRuntimeChanged`，使 GUI/TUI 立即重绘；最终 `plan_run` JSON 仍是
兜底收敛来源。

## 循环依赖检查

- [x] application 只引用 `seelebridge` 的 DTO，不引用 Seele fork runtime。
- [x] seelebridge 不引用 application；binding 由 application 接口传入值对象。
- [x] TUI/GUI 仍只依赖 application snapshot。

## 风险

- 多分支对应同一账号在配置只有一个 SubAgent 时是合法降级；E2E 必须使用两个测试账号来证明
  多账号选择，而不能把“账号不同”误当成生产配置硬约束。
- 当前 `plan_load` 在 tool start 时提前创建 PlanState；若 framework load 随后失败，旧问题仍会
  留下草稿状态。本 M8 不扩大到该独立一致性问题，测试应使用成功 load。
- `go test -race` 依赖本机 CGO；本机环境禁用 CGO，应在 CI 覆盖。
