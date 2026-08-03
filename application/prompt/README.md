# Application Prompt

## 定位

本包管理 system prompt 的分层组合和 Effort 行为策略，为 `core.Service` 提供线程安全、可解释的 prompt 状态。

## PromptStack

`PromptLayer` 以 `kind + name + text` 标识一层。固定 system 顺序为 identity、base/plugin、effort、instructions；Skill 请求内容不永久写入 system prompt，而随用户 turn 进入结构化上下文。

主要操作：

- `Push`/`Pop`/`PopKind`/`ClearKind`：维护层。
- `Reset`：重置基础 prompt。
- `Render`：按固定顺序生成最终 system prompt。
- `Layers`：供服务端输入冻结和 Skill 上下文构建。装配后的 system prompt 与层摘要不得进入 Snapshot、Event 或前端诊断。

## EffortManager

`lite`、`medium`、`high`、`max` 映射到行为提示、Engine MaxLoops、PlanPolicy 和请求级 ReAct budget。`Apply` 同时更新 PromptStack 与 Engine；`Cycle` 为前端提供顺序切换。

每个 budget 都限制工具轮数和工具调用数，并在 `core.Service` 接管底层 Engine 未主动执行 `MaxLoops` 时作为最终的收束边界。预算按提交时的 effort 快照保存；它不会禁止 Markdown 导出等交付工具，只会阻止预算耗尽后的下一次 ReAct 迭代。长编码任务不设 wall-clock 超时，仍可由用户显式取消。

具体提示词不保存在 Go 常量中。Seelex-owned identity、通用系统规则、各 effort 规则和 PlanAct preflight/replan 模板位于 [`internal/promptassets`](../../internal/promptassets/README.md)，并在构建时嵌入二进制。这里仅保留层组合和 effort→运行时 policy 的映射。

每个 budget 同时限制工具轮数、工具调用数和连续无进展轮数；无进展只在重复工具工作未产生新事实、变更、产物或 Plan 节点状态时计数。它是最后熔断器，主路径仍是 Application 的 checkpoint 与 token 驱动上下文裁剪；长编码任务没有 wall-clock timeout。预算不会禁止 Markdown 导出等交付工具。

## Review 指南

## Plan policy

`PlanningPolicy` maps effort to runtime-enforced constraints for an optional
`plan_load`: Medium permits at most four serial nodes with concurrency one;
High permits DAG branches with concurrency three; Max runs every currently
runnable node in a voluntarily loaded plan concurrently. `core.Service`
snapshots this policy with the request budget, but every normal user request
enters the ReAct loop directly; effort does not create a mandatory preflight.

When a loaded Plan fails, an explicit replan interaction uses the same `plan_load` contract to replace only the remaining recovery workflow. It carries bounded node evidence and stops before `plan_run`, so a changed recovery path is reviewed before any new side effect.

- 不要让 map 迭代顺序影响 prompt；渲染顺序必须确定。
- Effort 更新应同时刷新 max loops 和最终 system prompt；新增等级必须通过单一 profile 同时定义 prompt、PlanPolicy 和 budget。
- Skill prompt 与 system layers 的边界不能混淆。
- 所有读写都经过 mutex，返回 slice 时需复制。

## 测试

```text
go test ./application/prompt -count=1
go test ./application/core -run 'Effort|Skill' -count=1
```
