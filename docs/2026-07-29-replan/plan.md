# Replan 恢复路径

## 目标

把 `plan_run` 的失败从“提示模型自行补救”变成一条可审计、可限制且不自动扩大副作用的恢复路径。

## 已实现路径

```text
plan_run 失败
  -> Application 标记失败节点并保留已完成节点证据
  -> 打开 retry / replan / skip / abort 交互
  -> 用户选择 replan
  -> Runtime.PrepareReplan（隔离 LLM 请求，强制 tool_choice=plan_load）
  -> 既有 plan_load schema + effort policy 校验
  -> 原子替换旧 WorkPlan 和 UI PlanState
  -> 停在用户复核点；仅在后续显式 plan_run 时执行
```

`replan` 不调用 `plan_clear`：Seele 的 `plan_load` 已经原子替换当前 WorkPlan，避免清空旧计划与载入新计划之间出现无计划状态。

## 输入边界

Replan 请求只携带以下有界信息：

- 原始用户目标；
- 最近一次 `plan_load` 的 JSON；
- 失败描述；
- 已完成、跳过或失败节点的状态和输出证据（最多 12 KiB）。

它不发送不受限的完整对话，也不持久化模型原始思维链。

## 安全与 effort 规则

- 仍由现有 `plan_load` policy 校验 Medium 的四步串行、High 的并发 3、Max 的节点数并发。
- Lite 不强制首轮规划，但用户显式选择 replan 时可以创建恢复计划。
- 新 Plan 的提示词要求保留完成工作、优先诊断或安全替代，不自动重试失败副作用。
- Plan 载入成功后不自动 `plan_run`；用户先复核替代 DAG，再显式执行。

## 验证

- `TestResolvePlanFailureReplansWithoutRunningReplacement`
- `TestResolvePlanFailureKeepsInteractionWhenReplanFails`
- `TestRuntimePrepareReplanForcesPlanLoadForExplicitLiteRecovery`
