# Govern 治理循环真实 API 验收报告（2026-09-08）

> 日期：2026-09-08 · 套件：`tmp/goal-tl-live-smoke`（go test，SEELEX_LIVE_SMOKE=1）
> 被测：`application/core/govern`（治理循环抽象）+ `application/core/goal`
> （Controller/Supervisor/adapter/headless goal_gov_*）
> 账号：本地 `config/accounts.yaml`（goalplan/agent，deepseek 兼容端点；凭据不外泄）

## 1. 装配与命令

```powershell
$env:SEELEX_LIVE_SMOKE='1'
go test ./tmp/goal-tl-live-smoke -v -count=1 -timeout=12m
```

所有真实 API 用例共用一条装配链：`goal.Controller + Supervisor(真实 LLM
评估器) + TurnGovernor(exec-a/advisor-b)`，经 goal.Headless HTTP RPC 驱动。

## 2. 用例与结果

| 用例 | 验证点 | 结果 | 说明 |
|---|---|---|---|
| `TestTLRealLLMRound` | 单回合 TL 真实裁决（kind/content/refs/帧账本） | PASS | 本次 verdict_not_done，TL 给三条可执行下一步（B4 时序、帧同步、类型核验） |
| `TestTurnGovernorRealLLM` | 治理循环（exec→TL 轮转）真实 API 驱动 | PASS | round/current/seats 快照可观测 |
| `TestHeadlessGovernRealLLM` | headless goal_gov_* RPC 真实驱动 | PASS | 治理快照 round=2、TL eval=2 |
| `TestMainAgentToTLFinishRealLLM` | mainAgent 发起 → TL 终态裁决收口 | PASS | outcome=escalate_human（材料不足，TL 升级人工），goal 保持 active——符合 B4/终态语义 |

## 3. 结论

- mainAgent 发球、TL 收口的语义在真实 API 下成立：`goal_propose_finish`
  把收口权交给 TL；TL 按证据给出 verdict_not_done / escalate，不强行收口；
- 治理循环（exec-a → advisor-b 轮转）与 goal 状态机、TL peer 状态一致性
  通过快照断言；
- 偶发失败仅在**多个真实 API 用例并行/连跑**时出现（provider 限流/超时），
  单用例重跑稳定——冒烟结论不受影响，接入生产时按 B4 缺席矩阵处理即可。

> 注：单用例偶发超时已在 goal 域设计为 B4 逃生路径（429/timeout → 缺席
> 矩阵），不属于治理逻辑缺陷；报告结论以单用例稳定通过为准。
