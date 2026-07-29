# PlanAct 上下文控制与完成协议

## 状态

实施中。本文记录本次工作包的目标、边界和验收条件；模块 README 只在实现完成后描述已落地的行为。

## 问题

当前 PlanAct 能强制预规划、加载权威 DAG 并限制 replan，但正常 ReAct 仍主要依赖模型自行决定何时停止。长任务会反复读取、测试或审查：即使已有足够证据，也未形成明确的用户交付终态。工具轮数上限只能阻断循环；它不能保留压缩后的任务状态，也不能保证产生结果。

## 决策

第一阶段不改变 `plan_load` 的外部 DAG 格式。Application 在单个 chat 请求内维护一个私有执行状态：

```text
preflight plan -> authoritative ReAct
  -> tool/node progress -> NodeCheckpoint
  -> ContextController compacts history when needed
  -> deterministic completion check
  -> task_complete | task_failed
  -> user-approved, guarded replan only after failure
```

`task_complete` 与 `task_failed` 是模型可见的内置终态工具；它们不执行外部副作用。终态数据用来形成用户答复、保存会话可恢复的事实摘要，并为已有的 replan 路径提供有界证据。

## 请求作用域

`TaskExecutionState` 归属 Application 的单次 `chatRequest`，不得存入 Runtime 全局状态。它持有：

- 原始 objective、effort、已加载 Plan 的 canonical JSON；
- 当前节点和全部 `NodeCheckpoint`；
- 已完成/失败节点、验证证据、待办和产物；
- 连续无进展次数与请求级预算状态；
- `running`、`completed`、`failed`、`replanning` 终态。

这样 Plan authority、checkpoint、上下文裁剪和 replan evidence 都不会在并行会话之间串台。

## NodeCheckpoint 与上下文控制

每个 `plan_run` 节点结束、Plan 失败、或工具输出达到阈值时，Application 写入结构化 checkpoint。checkpoint 只保存目标、状态、文件/产物、命令 exit code、关键输出摘要、事实、待办和失败证据；不保存模型思维链或原始大日志。

ContextController 的输入是 Engine history 与请求状态。它始终保留系统提示词（由 Engine 管理）、当前原始用户请求、authority plan、当前节点、未完成依赖和必要证据；被 checkpoint 覆盖的旧工具结果、重复读取结果和冗长成功日志会被替换为一个机器标记的摘要消息。触发依据为 token/输出大小/状态变化，绝不使用 wall-clock timeout。

第一版使用现有 `ChatEngine.History` 和 `ReplaceHistory` 完成压缩，避免改变 Seele Engine 协议。`seelexctx.EstimateTokens` 用于稳定估算，不把 provider token 统计当作唯一依据。

## 终态与验收

`task_complete` 需要提交用户摘要、完成节点、产物、验证证据和剩余风险；`task_failed` 需要提交失败类别、失败节点、可复现证据、部分进展和是否建议 replan。

确定性 Judge 是主判定：检查 Plan 必需节点、目标工具/测试结果、变更/产物存在性和终态 payload 的完整性。若模型在自然回复中没有调用终态工具，Application 仍可在 ChatStream 返回后以可验证的状态生成 checkpoint；但对于要求 Plan 的任务，缺少完成或失败终态会作为协议错误显示，不能静默当作成功。

LLM Judge 不在第一阶段引入；它会增加第二个无界 ReAct 风险。后续若需要，只允许单次、无工具、固定小上下文的解释性判定。

## 预算和无进展

节点语义分为 `read_only`、`change`、`verify`、`deliver`。第一阶段由运行期观察推断，第二阶段再把它变成兼容的 DAG 节点字段。

- `change` 需要可观察变更或明确失败；不对只读、验证、交付节点强制写入。
- 连续重复读取/测试且没有新事实、文件变更、节点状态或验证结果时，增加无进展计数并要求模型结束或失败。
- effort 的工具轮数/调用数仍是最后熔断器。熔断后允许一次只读终态交付回合；不允许继续调查或测试。
- 报告导出、写入交付物等必要工具属于 `deliver`，在正常完成协议中始终可用。

## Replan 边界

只有 `task_failed`、确定性验收失败或用户明确变更目标才可进入 replan。`ReplanRequest` 从 checkpoint 提取 objective、旧 Plan、失败和证据，继续使用现有 idempotency key、并发、窗口和 provider 请求预算。模型“还想再检查一次”不是 replan 理由。

## 实现切片

1. 在 `application/core` 增加请求私有 `TaskExecutionState`、checkpoint DTO、终态 payload 校验与内置工具注册。
2. 在 tool hook 和 Plan node callback 中记录进展、归纳工具结果并写 checkpoint。
3. 增加 ContextController：按 token/工具输出阈值将历史替换为 checkpoint context，且不保存内部 authority/终态标记到用户会话。
4. 将 ReAct budget 从直接报错调整为“最终终态决策”回合；未能终态时返回可解释的失败。
5. 将计划和系统提示词更新为“有界验证、终态必选、失败证据”的 Claude 风格规则，并用确定性 harness 回归。
6. 补充 Application 单元测试、PlanAct prompt harness、全仓测试和 Windows Dev GUI 构建。

## 验收

- 长审查任务在证据充分后可收敛为 `task_complete`，不依赖用户打断。
- 连续无进展工具循环会要求完成/失败而非无限继续。
- 大工具输出压缩后仍保留当前目标、authority plan、未完成工作、关键失败及验证结果。
- `task_failed` 能生成有界、可审计的 replan evidence；不自动执行替代 Plan。
- 系统提示词、authority marker 与内部压缩消息均不会暴露至 frontend snapshot 或持久化的普通用户输入。
- 无 wall-clock timeout；高 effort 仍允许长编码任务。
