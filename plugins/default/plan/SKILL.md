---
description: WorkPlan 入口；按当前 effort 创建受运行时约束的 plan_load DAG
---

# WorkPlan 规划

For every non-trivial task, the first action MUST be the `plan_load` tool call, unless the runtime supplies an authoritative preflight WorkPlan for this request. Do not substitute a prose outline or a final answer for loading the plan. Load before using any execution tool; invoke `plan_run` only when the task needs to execute plan nodes.

When an authoritative preflight WorkPlan is present, it is already validated and loaded: do not call `plan_load` again, do not replace it, and do not call `plan_clear`. Use `plan_run` only when execution is needed. If execution fails, the explicit user-selected replan path may load a reviewed recovery DAG; it remains available after the current turn.

The dedicated marker `<!-- seelex:plan-context:v1 authority=preflight-loaded -->` means that runtime planning is complete for the current request. It is not a request to plan again: never call `plan_load` or `plan_clear` while this marker is present, including when the ordinary user wording asks for a plan. Follow the supplied Loaded WorkPlan, call `plan_run` only when execution is needed, and use the explicit replan flow only after a `plan_run` failure and user review.

使用 `plan_load` 定义 DAG，再调用 `plan_run`。Plan 工具是启动即注册的基础工具，不需要切换到独立 Plugin。

When `plan_run` fails, inspect the failed node and completed-node evidence. A user-selected replan loads a recovery DAG for the remaining work only; it never executes automatically, so review the replacement before calling `plan_run`.

首选 canonical JSON：`nodes` 是按节点 ID 键控的对象，`edges` 是 `source: [targetID]` 邻接表；不要使用 `item`。为提高模型兼容性，`plan_load` 也接受适配形式：`nodes` 可写成带 `id` 或 `key` 的数组，`edges` 可写成带 `from`/`source` 和 `to`/`target` 的边数组。数组边必须同时给出来源和目标；不能只写 `{ "to": "target" }`。

适配形式示例：

```json
{
  "entry": "inspect",
  "nodes": [
    {"id": "inspect", "input": "inspect the repository"},
    {"key": "report", "input": "write the report"}
  ],
  "edges": [{"from": "inspect", "to": "report"}]
}
```

当前 Effort 的运行时规则：

- `lite`：Plan 可选。
- `medium`：Plan 最多 4 个节点，且必须是串行链；并发固定为 1。
- `high`：允许 DAG 并行节点；并发最多 3。
- `max`：允许所有当前可运行节点并行。

运行时会拒绝不符合这些规则的 `plan_load` 请求。

对方案设计任务，根据任务性质选择合适的模式：

- **/plan-design** — 启发式设计：搜索行业标杆、竞品分析、技术选型
- **/plan-efficiency** — 规划式效率：打点表、活动图、SubAgent 调度
- **/plan-norm** — 约束式规范：ASPICE 追溯、变更影响分析、审查检查单

## 通用设计流程

1. **需求分析**：理解目标、识别约束、明确成功标准
2. **方案设计**：模块划分、接口定义、数据流设计
3. **风险评估**：识别技术风险、提出缓解措施
4. **实施计划**：分阶段步骤、里程碑定义

## 原则
- 高内聚低耦合
- 接口先行
- 单一职责
- 优先组合而非继承
