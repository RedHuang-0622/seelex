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

## 风暴、成本与幂等保护

- 单个 Plan 链最多允许 2 次成功载入的 recovery Plan；达到上限后保留失败交互，不再请求模型。
- Runtime 进程全局最多 2 个 replan 同时进行，每分钟最多 6 个 replan 操作。
- 同一窗口内最多 6 个真实 provider 请求；协议修复重试也必须先取得该预算，因此不会把操作额度放大为双倍 token 消耗。
- 同一 Interaction ID 和同一 Runtime recovery operation key 都会去重，阻止双击或并发事件重复提交。
- 只有 `plan_load` 的前置 schema/策略校验失败才允许一次修复重试；任何意外工具调用、执行错误或已成功载入的 Plan 都不会重试。
- `RuntimeState.replan` 暴露 in-flight、窗口使用量、累计成功/失败/拒绝、重复拒绝和 provider 请求数，且不包含请求内容或密钥。

## 验证

- `TestResolvePlanFailureReplansWithoutRunningReplacement`
- `TestResolvePlanFailureKeepsInteractionWhenReplanFails`
- `TestRuntimePrepareReplanForcesPlanLoadForExplicitLiteRecovery`
- 定量结果、真实 API 试跑记录和本机验证边界见 [test-report.md](test-report.md)。

## 真实 API A/B（2026-07-29）

同一真实账号、相同的“检查节点证据不完整”恢复意图下：

| 组别 | 路径 | 结果 | provider 请求 |
|---|---|---|---|
| A / control | Lite + Plan Skill，模型自发调用 `plan_load` | 成功 | 由正常 ReAct 调用 |
| B / treatment | `PrepareReplan`，隔离请求 + 强制 `plan_load` | 成功 | 2（首个数组格式被前置拒绝后纠正） |

这个单样本不能证明强制路径会提高模型本身的规划质量；它证明了两条路径都能完成该恢复意图。B 的两次 provider 请求也实际覆盖了“错误 JSON 在载入前被拒绝、单次纠错重试后成功”的幂等边界。保留 B 的依据是系统保证：schema、effort policy、幂等键、全局成本预算和可审计指标不依赖模型自觉遵守。
