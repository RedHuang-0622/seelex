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

`lite`、`medium`、`high`、`max` 映射到行为提示和 Engine MaxLoops。`Apply` 同时更新 PromptStack 与 Engine；`Cycle` 为前端提供顺序切换。

具体提示词不保存在 Go 常量中。Seelex-owned identity、通用系统规则、各 effort 规则和 PlanAct preflight/replan 模板位于 [`internal/promptassets`](../../internal/promptassets/README.md)，并在构建时嵌入二进制。这里仅保留层组合和 effort→运行时 policy 的映射。

## Review 指南

## Plan policy

`PlanningPolicy` maps effort to runtime-enforced WorkPlan constraints: Lite is optional; Medium permits at most four serial nodes with concurrency one; High permits DAG branches with concurrency three; Max runs every currently runnable node in the loaded plan concurrently. `core.Service` snapshots this policy when intercepting a normal user request: Medium, High, and Max make a mandatory `plan_load` preflight before the request enters the normal ReAct loop.

When a loaded Plan fails, an explicit replan interaction uses the same `plan_load` contract to replace only the remaining recovery workflow. It carries bounded node evidence and stops before `plan_run`, so a changed recovery path is reviewed before any new side effect.

- 不要让 map 迭代顺序影响 prompt；渲染顺序必须确定。
- Effort 更新应同时刷新 max loops 和最终 system prompt。
- Skill prompt 与 system layers 的边界不能混淆。
- 所有读写都经过 mutex，返回 slice 时需复制。

## 测试

```text
go test ./application/prompt -count=1
go test ./application/core -run 'Effort|Skill' -count=1
```
