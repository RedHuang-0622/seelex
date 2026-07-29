# PlanAct 最终审查

审查日期：2026-07-29。范围为本工作树中 Plan 输入适配、effort 前置规划、权威预规划上下文和受限 replan 的未提交变更。

## 结论

**有条件通过，可继续作为 Developer Alpha 的默认规划链路；不应把当前真实 API 小样本表述为“已证明生成质量提升”。**

这次改动把“模型被提示先做计划”升级成了可执行的 PlanAct 状态机：Medium/High/Max 先由独立调用强制 `plan_load`，再把已加载的 canonical DAG 以 authority marker 交给普通 ReAct；当前回合既从可见工具集中移除 `plan_load`/`plan_clear`，又在 handler 层保留拒绝逻辑。`plan_run` 失败后的 replan 只能由用户显式触发、仅替换计划、不自动执行，并受到幂等、并发、窗口和 provider 请求预算约束。

## 五轴评价

| 维度 | 状态 | 评分 | 依据 |
|---|:---:|:---:|---|
| 正确性 | ⚠️ | B+ | effort 约束已进入 Runtime；DAG 适配在 policy/委托前规范化；预规划和 replan 均有明确边界。真实 API 仍出现 provider 不返回工具调用、entry 不一致和超时。 |
| 可读性 | ✅ | B+ | `PlanPreflight`、`ReplanRequest`、authority marker 的职责清晰；但 preflight prompt 与 adapter 契约存在表述冲突。 |
| 架构 | ✅ | A- | Application 编排、Runtime 执行、Tool provider 校验的分层合理；authority 目前仍是 Runtime 全局布尔状态。 |
| 安全与成本 | ✅ | A- | 预规划不自动执行；replan 有单链、全局并发、时间窗、provider 预算、操作键去重和有限纠错。 |
| 性能 | ✅ | B | `plan_load` 基准为 45,036 ns/op、8,897 B/op、142 allocs/op；前置调用增加一次真实 provider 往返，是可见且预期的延迟/费用。 |
| Go 专项 | ✅ | A- | `go test`、`go vet`、普通及 production GUI build 已通过；Windows 本地没有执行 `-race`。 |

## 已验证的亮点

- `nodes[]`/`edges[]`、`id`/`key`、`from`/`source`、`to`/`target` 和邻接表 target 对象都会规约到 Seele 可执行的 canonical object DAG；缺失边源或引用不存在节点会在执行前拒绝。
- Medium 强制不超过 4 节点的串行链；High 并发上限为 3；Max 以已加载计划节点数允许所有当前可运行节点并发。
- Authority context 使用专属 marker，并在会话保存前恢复为原始用户请求，避免把内部 WorkPlan 长期带入后续对话。
- 实际工具调用统计已修正为只计 `role=tool`；`role=tool_result` 只是同次调用的完成事件。

## 警告与下一步

### 1. Preflight prompt 与适配层的叙述冲突

`planPreflightPrompt` 仍要求 `nodes`/`edges` “never arrays”，而 `plan_load` schema、Skill 和 adapter 已声明数组形式可接受。这并不破坏 canonical 预规划，但会让模型面对两套规则，并使 preflight 路径无法主动验证适配形式。

建议统一成：**canonical object 是首选输出；数组是兼容输入，运行时会规范化；两者都必须满足完整引用校验。**

### 2. 真实 API 的结论应保持克制

已有结果证明了强制规划、输入适配和安全拒绝可在真实账户上工作：Medium 两次均生成 2 节点串行 DAG；High 两次成功加载；此前 High 的质量试验为 2/3 成功，另一个样本在 50 秒限制内超时；两次显式 replan 则分别遇到 entry 不一致和未返回工具调用。

因此目前能主张的是“**系统约束和失败边界有效**”，不能主张“**High 的生成质量已被统计显著提升**”。下次应以固定任务集、固定 timeout、独立会话并记录成功率、结构分、首 token/总时延、provider 请求数与 token 成本。

### 3. Authority 应演进为请求作用域

当前 `planAuthoritative` 是 Runtime 级布尔值。现有单 Chat 生命周期下足够，但并行会话、多窗口或未来多 Agent 同享 Runtime 时会互相影响工具可见性。建议把它收敛到 request/session-scoped 的 capability 或在 Runtime 入口处建立独占的 PlanAct operation scope。

### 4. 补一条防御层直测

现有测试已证明 authority 时 `plan_load`/`plan_clear` 不可见，并在 Chat 层验证了 `[true,false]` 生命周期。还应直接构造保留的旧 `planLoadPolicyHandler`，断言 authority 为真时 `Execute` 拒绝；这能覆盖“模型持有旧工具快照”的防御层，而不仅是注册表可见性。

## 本次复核命令

```text
git diff --check
CGO_ENABLED=0 go test ./seelebridge ./application/core -count=1 -timeout=120s
```

结果：通过；`seelebridge` 100.579 s，`application/core` 0.670 s。

