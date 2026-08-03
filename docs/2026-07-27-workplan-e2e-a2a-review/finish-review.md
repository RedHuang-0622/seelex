# Seele WorkPlan、Seelex Plan 与 E2E/A2A 详细审查

> 审查日期：2026-07-27
> 代码范围：Seelex 当前工作树、`github.com/RedHuang-0622/Seele v0.0.5`
> 审查目标：说明 Plan 的真实运行机制、当前 E2E 的实际覆盖边界，并澄清 E2E 与 A2A 的区别。

## 1. 先说结论

Seelex 当前的 Plan 不是由 TUI 自己执行，也不是 Application 自己调度。实际分工是：

1. 主 Agent 根据 Effort 提示决定是否调用 `plan_load`、`plan_run`。
2. Seele `WorkPlanTool` 把 `plan_load` JSON 转成 DAG。
3. Seele Scheduler 从 entry 开始执行节点；单出边顺序执行，多条无条件出边并发执行。
4. Seelex Application 监听工具开始、工具结束和节点完成回调，把执行结果映射成 `Runtime.Plan`。
5. TUI/GUI 只读取 Snapshot 展示，不参与调度。

当前能力应拆成两部分判断：

| 能力 | 当前判断 |
|------|----------|
| 线性 Plan | 基本可用；输出可通过 `{{.PrevResult}}` 传给下一节点 |
| manual Plan | 基本可用；真实 WorkPlan handler、ApprovalBroker 和 PlanState 已有 L1 E2E |
| DAG 数据保存 | 已保存节点和边，并有稳定拓扑排序 |
| TUI DAG 层级展示 | 未完整接通；`Edges` 已有，但 `Depth/Children` 未构造 |
| 并行 Plan | 不建议作为可靠能力发布；共享上下文存在并发写风险，分支失败可能被吞掉 |
| 实时运行状态 | 不完整；只有节点完成回调，没有节点开始回调，Plan 在执行中仍可能显示 pending |
| 完整 A2A | 尚未实现；当前更接近“主 Agent 编排多个模型节点”，不是独立 Agent 间协议通信 |
| 当前 E2E | L1 Core scenario；不是 Browser/Wails E2E，也不是真实账号池/真实模型 E2E |

最终审查结论：**线性/manual 主链路有条件通过；并行 WorkPlan 与完整 A2A 不通过生产可靠性审查。**

## 2. Plan 从哪里开始

### 2.1 Effort 决定主 Agent 是否倾向使用 Plan

位置：`application/effort.go:20-65`

| Effort | Plan 行为 | MaxLoops |
|--------|-----------|---------:|
| lite | 不注入 Plan 指令 | 20 |
| medium | 多步骤任务使用 `plan_load → plan_run`，提示最多并发 2 节点 | 64 |
| high | 多步骤任务使用 Plan，提示最多并发 4 节点并验证结果 | 512 |
| max | 总是优先规划，提示使用 Fork/子 Agent，并发不限 | 1024 |

默认 Effort 是 `high`，位置为 `application/effort.go:57-65`。

这里需要特别注意：medium 的“最多 2 个”和 high 的“最多 4 个”只是 system prompt 中的行为要求，不是 Scheduler 的硬限制。Seele Scheduler 会把同一节点的全部无条件出边一次性启动为 goroutine，位置为：

- `Seele/workplan/runtime/graph/graph.go:123-142`
- `Seele/workplan/runtime/scheduler/scheduler.go:88-103`
- `Seele/workplan/runtime/scheduler/scheduler.go:124-146`

因此模型如果生成 10 条同级出边，medium/high 目前都可能实际并发 10 个节点。

### 2.2 WorkPlan 工具在启动时接入

Seelex 启动链路：

```text
main
  → Runtime.RegisterBuiltins()
  → WorkPlanTool 注册到 Seele Agent
  → ApprovalBroker 注入 PlanApprovalGate
  → Application Service 创建
  → Plan NodeResult callback 绑定到 Application
  → 启动 TUI/GUI
```

关键位置：

- `main.go:72-96`：组装 Runtime、Application、ApprovalBroker 和回调。
- `seelebridge/runtime.go:118-125`：创建并注册 `WorkPlanTool`。
- `seelebridge/runtime.go:123-132`：绑定 `ProgressCallback` 和 `ApprovalGate`。

## 3. `plan_load` 实际做了什么

### 3.1 输入契约

Seele handler 接受：

```json
{
  "entry": "analyze",
  "nodes": {
    "analyze": {"input": "分析需求"},
    "implement": {"input": "根据分析实现：{{.PrevResult}}"},
    "review": {"input": "复核实现：{{.PrevResult}}", "kind": "manual"}
  },
  "edges": {
    "analyze": ["implement"],
    "implement": ["review"]
  }
}
```

源码位置：`Seele/agent/core/tool/builtin/workplan_handler.go:34-43`。

当前公开的 `plan_load` 节点类型实际只有两种：

- `kind` 为空或其它值：创建 auto 节点。
- `kind: "manual"`：有 Gate 时创建 approve 节点；没有 Gate 时降级成 auto 节点。

位置：`Seele/agent/core/tool/builtin/workplan_handler.go:81-95`。

所以 `kind: "fork"`、`kind: "subagent"` 并不是当前 `plan_load` 的有效显式类型。Plan 的并发来自“一个节点有多条出边”，不是来自一个 `fork` kind。

### 3.2 Seele 建图

`plan_load` 会：

1. 解析 JSON。
2. 校验 entry、nodes 和 edge 引用。
3. 创建新的 `WorkPlan` 和 Graph。
4. 为每个节点创建 auto/manual Node。
5. 设置 entry。
6. 添加边。
7. 原子替换 `WorkPlanTool.wp`。

位置：`Seele/agent/core/tool/builtin/workplan_handler.go:45-110`。

真正运行前，Runner 还会验证 entry、边引用、环和孤儿节点：`Seele/workplan/runtime/runner/runner.go:53-58`。

### 3.3 Seelex 同步创建展示状态

工具刚开始调用时，Seelex 会先解析同一份 JSON：

- `application/chat.go:121-140`：收到 `plan_load` start。
- `application/chat.go:197-243`：创建 `PlanState`。
- `seelebridge/plan.go:15-30`：邻接表转 `Edges`。
- `seelebridge/plan.go:72-139`：稳定拓扑排序。

`PlanState` 的数据结构在 `application/state.go:71-109`：

```text
PlanState
  ├─ EntryNodeID
  ├─ Status
  ├─ Nodes[]
  ├─ Edges[]
  ├─ Progress
  └─ Elapsed
```

此时状态是 `pending`，每个节点也是 `pending`。

## 4. `plan_run` 实际怎么执行

### 4.1 工具层

位置：`Seele/agent/core/tool/builtin/workplan_handler.go:121-161`。

执行顺序：

1. 锁住 `WorkPlanTool.mu`。
2. 检查是否已经加载 Plan。
3. 把 `ProgressCallback` 写到 `WorkPlan.NodeHook`。
4. 调用 `WorkPlan.Run(ctx)`。
5. 把结果编码为 `status/node_count/final_output/nodes` JSON。

如果没有 Plan，返回：

```json
{"status":"failed","error":"no plan loaded - call plan_load first"}
```

但 Go error 是 `nil`。如果 `WorkPlan.Run` 返回 error，handler 同样把 error 编码进 failed JSON，再返回 `nil` error。

Seelex 已在 `application/chat.go:161-173` 对这种“JSON failed + nil error”做了额外识别。

### 4.2 Scheduler 顺序执行

位置：`Seele/workplan/runtime/scheduler/scheduler.go:34-110`。

顺序节点的循环是：

```text
读取 current node
  → RunNode
  → 生成 NodeResult
  → 保存到 WorkPlanResult
  → 保存 PrevResults[nodeID]
  → 调用 OnNodeDone
  → 出错则返回
  → 更新 PrevOutput
  → 选择下一节点
```

节点输出保存在 `WorkflowContext`：

- `PrevOutput`：紧邻上一节点输出。
- `PrevResults`：已执行节点的 `nodeID → output`。
- `Vars`：命名变量。
- `Result`：累计 WorkPlanResult。
- `Metadata`：扩展信息。

位置：`Seele/workplan/core/types/context.go:10-27`。

模板可使用：

```text
{{.PrevResult}}
{{.PrevResults.node_id}}
{{.Vars.key}}
```

### 4.3 auto 节点并不是完整带工具的子 Agent

`plan_load` 的 auto 节点会创建 `AgentStrategy`，位置为 `Seele/workplan/sugar/auto/auto.go:84-127`。

但是 Seelex 注册 WorkPlan 时使用的具体工厂是 `NewChatAgentFactory(r.agent.LLM())`。这个工厂创建的 `chatAgent` 只是：

```text
system message + user message
  → 同一个 ChatCompleter.Complete(..., tools=nil)
```

位置：`Seele/agent/core/tool/builtin/workplan.go:18-50`。

因此当前 WorkPlan 节点：

- 是独立的一次模型调用；
- 可以有独立 system prompt；
- 但没有独立工具集；
- 不运行完整 ReAct loop；
- 不会直接向另一个 Agent 发消息；
- 节点之间通过 `WorkflowContext` 传结果。

称为“子任务模型节点”或“A2A-like 编排”比较准确，称为完整 A2A 子 Agent 还不准确。

## 5. 一个线性 + manual 的实际案例

### 5.1 Plan

```json
{
  "entry": "analyze",
  "nodes": {
    "analyze": {
      "input": "审查登录模块，列出风险"
    },
    "implement": {
      "input": "根据这份审查结果给出修改方案：{{.PrevResult}}"
    },
    "review": {
      "input": "人工确认后，复核方案是否覆盖全部风险：{{.PrevResult}}",
      "kind": "manual"
    }
  },
  "edges": {
    "analyze": ["implement"],
    "implement": ["review"]
  }
}
```

### 5.2 运行过程

```text
主 Agent 调用 plan_load
  → Seelex 创建 pending PlanState
  → Seele 创建 analyze/implement/review 三个节点

主 Agent 调用 plan_run
  → analyze 调用模型
  → 输出写入 PrevOutput 和 PrevResults.analyze
  → implement 的 {{.PrevResult}} 被替换为 analyze 输出
  → implement 调用模型
  → 输出写入 PrevOutput 和 PrevResults.implement
  → review 是 manual 节点，ApprovalGate 阻塞
  → Seelex ApprovalBroker 打开 interaction
  → 用户选择 execute
  → review 才真正调用模型
  → Scheduler 返回 WorkPlanResult
  → plan_run 返回 completed JSON
  → Seelex 将 PlanState 收敛为 completed/progress=1
```

manual 节点有一个容易混淆的细节：审批框的问题内容来自当前 `PrevOutput`，而节点的 `input` 是审批通过后才渲染并交给模型的执行输入。位置为 `Seele/workplan/sugar/approve/approve.go:87-118`。

用户选择：

- `execute`：执行 manual 节点的模型调用。
- `skip`：直接返回上一节点输出。
- `abort`：返回 error。

## 6. 并行 Plan 的意图与当前限制

下面的 Plan 会被识别为 fan-out/fan-in：

```json
{
  "entry": "analyze",
  "nodes": {
    "analyze": {"input": "分析需求"},
    "backend": {"input": "设计后端：{{.PrevResult}}"},
    "tests": {"input": "设计测试：{{.PrevResult}}"},
    "review": {
      "input": "汇总后端 {{.PrevResults.backend}} 和测试 {{.PrevResults.tests}}"
    }
  },
  "edges": {
    "analyze": ["backend", "tests"],
    "backend": ["review"],
    "tests": ["review"]
  }
}
```

设计意图是：

```text
analyze
  ├─ backend ─┐
  └─ tests ───┴─ review
```

Scheduler 的确会为 `backend` 和 `tests` 各启动一个 goroutine，等两者完成后，把输出合并到 `PrevResults`，再执行共同的 `review`。

但当前实现有两个严重问题：

### 6.1 并行节点共享同一个可变 WorkflowContext

`scheduler.fork` 把同一个 `wc` 指针传给所有 goroutine：`Seele/workplan/runtime/scheduler/scheduler.go:124-145`。

auto 节点执行时会写 `wc.PrevOutput`：`Seele/workplan/sugar/auto/auto.go:27-35`。

因此两个并行 auto 节点会并发写同一个 `PrevOutput`。这既是 Go data race，也可能导致 backend 节点读到 tests 的输入，或反过来。当前 L1 manual-plan 只有单节点，无法发现该问题。

### 6.2 分支错误被记录，但没有向 Run 返回

`scheduler.fork` 会把第一个错误保存到 `firstErr`，但函数只返回 `commonNext string`，没有返回 error：`Seele/workplan/runtime/scheduler/scheduler.go:148-209`。

结果是：

1. 失败分支的 `NodeResult.Status` 会是 failed。
2. Scheduler 仍可能继续执行汇合节点。
3. `WorkPlan.Run` 最终可能返回 nil error。
4. `plan_run` handler 会输出整体 `status: completed`。
5. Seelex 会把整个 Plan 标成 completed。

这会造成“节点失败，但整个 Plan 显示成功”的错误结果，是当前并行 Plan 的阻断问题。

### 6.3 只支持一层立即汇合

当前 fork 只执行 fan-out 后的第一层节点，然后检查这些节点是否指向同一个直接后继。

可继续：

```text
start → {A, B} → join
```

会提前停止：

```text
start → {A → A2, B → B2} → join
```

因为 A 的直接后继是 A2，B 的直接后继是 B2，Scheduler 会把它判断为 divergent 并返回空 next。该行为在 Seele 自身测试 `workplan/runtime/scheduler/scheduler_test.go:161-187` 中被明确当作当前语义。

### 6.4 分支完成回调并不实时

fork 会先 `wg.Wait()` 等全部分支结束，再逐个触发 `OnNodeDone`：`Seele/workplan/runtime/scheduler/scheduler.go:124-170`。

如果 backend 1 秒完成、tests 60 秒完成，backend 的完成状态也要等到 60 秒后才推送给 Seelex。

## 7. Seelex 如何把 Plan 映射到 TUI/GUI

### 7.1 状态更新

- `plan_load` start：创建 pending Plan，`application/chat.go:121-140,197-243`。
- 节点完成：`HandlePlanNodeComplete` 更新节点、输出、耗时和进度，`application/chat.go:378-399`。
- `plan_run` complete：解析最终 JSON 并收敛总体状态，`application/chat.go:245-357`。
- `plan_run` failed JSON：创建 retry/skip/abort interaction，`application/chat.go:428-539`。
- 用户处理失败交互：`application/app.go:179-211`。

### 7.2 TUI 四种显示模式

位置：`tui/plan.go:76-118`。

| Effort | 展示 |
|--------|------|
| lite | 单行进度 |
| medium | 节点列表 |
| high | 带缩进和 Children 的树 |
| max | 表格和 Children |

High/Max 使用 `Depth` 和 `Children` 的位置：

- `tui/plan.go:183-243`
- `tui/plan.go:246-308`

但 `updatePlanFromLoad` 只按拓扑序创建平铺 Nodes，并没有根据 `Edges` 计算 `Depth` 或 `Children`：`application/chat.go:214-242`。

所以当前 TUI 能稳定显示节点顺序，但 High/Max 的“树状 DAG”数据没有真正接通；多数情况下仍是平铺列表。

### 7.3 运行态不会正确显示为 running

`handleToolStart` 对 `plan_run` 没有设置 `PlanRunning`：`application/chat.go:121-140`。

节点回调只在节点完成后触发，而且 `HandlePlanNodeComplete` 没有设置 `plan.Status = PlanRunning`：`application/chat.go:378-399`。

因此执行期间：

- Plan 总体可能一直显示 `pending`，直到最终结果到达。
- 节点没有 start callback，通常不会出现 `NodeRunning`。
- 单个长节点运行时，界面可能长时间看起来像“还没开始”。

## 8. 失败恢复目前怎么工作

`plan_run` 失败后，Seelex 会弹出：retry、skip、abort。

但三者的真实语义是：

| 选项 | 当前实现 |
|------|----------|
| retry | 给主 Agent 追加 system message，请模型再次调用 `plan_run` |
| skip | 给主 Agent 追加 system message，请模型执行 `plan_clear + plan_load` 重建 Plan |
| abort | Application 直接把 Plan 和剩余节点标成 aborted |

位置：`application/app.go:179-211`。

所以 retry/skip 是 **LLM 驱动恢复**，不是 Application 或 Scheduler 的确定性重试/跳过。模型不继续调用工具时，恢复不会发生。

## 9. 当前 E2E 到底是什么

### 9.1 分层定义

仓库文档把 Agent E2E 分为五层，位置：`docs/gui/modules/agent-e2e-interaction.md:32-44`。

| 层 | 对象 | 当前状态 |
|----|------|----------|
| L0 | Schema/Unit | 已有一部分 |
| L1 | 真实 Application + scripted ports | 当前已实现最小版本 |
| L2 | Browser + fake Wails Bridge | 未实现 |
| L3 | production Wails + 系统 WebView | 未实现 |
| L4 | 真实 provider/live Agent | 未实现 |

### 9.2 当前 L1 链路

```text
JSON fixture
  → Scenario loader/validator
  → ScriptedEngine
  → 真实 application.Service
  → EventHub + ToolHookBridge + ApprovalBroker
  → Runner 等待 Event/Snapshot
  → Result
```

关键位置：

- `e2e/scenario/harness.go:12-49`：创建真实 Application harness。
- `e2e/scenario/scripted_engine.go:46-109`：按 fixture 发 delta/tool/approval/error。
- `e2e/scenario/ports.go:40-47`：允许注入真实工具 handler。
- `e2e/scenario/runner.go:31-62`：逐步执行并等待 Chat 收敛。
- `e2e/scenario/runner.go:65-135`：当前支持的 action/expect。

### 9.3 manual Plan 黄金旅程

fixture：`e2e/fixtures/manual-plan.json:1-55`。

它执行：

```text
submit
  → plan_load（真实 WorkPlan handler）
  → plan_run（真实 WorkPlan handler）
  → manual ApprovalGate
  → ApprovalBroker interaction
  → resolve execute
  → deterministic test Agent
  → completed PlanState
```

真实工具注入和断言位置：`e2e/scenario/runner_test.go:48-128`。

其中节点 Agent 是测试用确定性实现：

```go
return "completed: " + input, nil
```

位置：`e2e/scenario/runner_test.go:117-128`。

因此这条 E2E 能证明：

- Scenario → Application → ToolHook → WorkPlan handler 链路能通。
- manual Gate 能阻塞并由 ApprovalBroker 唤醒。
- `plan_load → plan_run → PlanState completed` 能收敛。

它不能证明：

- 真实 LLM 调用质量。
- 真实账号池和 provider 切换。
- 多节点 DAG 和模板传值。
- fork 并发正确性。
- 分支失败传播。
- retry/skip/abort 完整恢复。
- TUI/GUI 视觉和交互。
- Browser/Wails 生命周期。
- 多 Session/Workspace 隔离。

### 9.4 当前测试证据

本轮重新执行：

| 命令 | 结果 |
|------|------|
| `go test ./application ./seelebridge ./tui ./e2e/scenario -count=1 -timeout=180s` | 通过 |
| 两条 Golden Journey 各 `-count=100` | 通过 |
| `go vet ./...` | 通过 |
| `go build ./...` | 通过 |

这些结果说明当前已有测试是稳定的，但不改变并行 DAG 未被测试的事实。

## 10. E2E 与 A2A，用大白话解释

### 10.1 E2E 是什么

E2E = End-to-End，端到端。

大白话：**别只测一个零件，要从用户动作开始，一直走到用户看到最终结果。**

类比网购：

```text
用户下单 → 支付 → 仓库出库 → 物流 → 用户收货
```

只测试支付接口不是 E2E；整条链真的走完才是 E2E。

在 Seelex 中，一条完整 Plan E2E 可以是：

```text
用户发送任务
  → 主 Agent 选择 Plan
  → plan_load
  → plan_run
  → manual 审批
  → 用户点击 execute
  → 节点执行完成
  → PlanState 更新
  → TUI/GUI 显示完成
```

E2E 关注的是：**一条用户业务旅程能不能从头走到尾。**

### 10.2 A2A 是什么

A2A = Agent-to-Agent，Agent 到 Agent。

大白话：**一个 Agent 不把所有事都自己做，而是把不同任务交给别的 Agent，再把结果收回来。**

类比软件团队：

```text
主 Agent：拆任务和汇总
  ├─ 后端 Agent：审查 Go 服务
  ├─ 测试 Agent：设计 E2E
  └─ 文档 Agent：整理报告
```

A2A 关注的是：

- 怎么找到另一个 Agent。
- 怎么把任务和上下文交过去。
- Agent 身份、角色和权限怎么区分。
- 结果怎么回来。
- 多 Agent 怎么并发。
- 失败、超时、取消、重试怎么传播。
- 一个 Agent 是否能给另一个 Agent 发后续消息。

### 10.3 两者不是一回事

| 概念 | 主要问题 | 类比 |
|------|----------|------|
| E2E | 整条业务链是否走通 | 从下单到收货 |
| A2A | 多个 Agent 如何协作 | 主厨把切菜交给帮厨 |
| A2A E2E | 包含真实多 Agent 协作的完整业务链是否走通 | 下单、仓储、物流各部门真的协作到收货 |

一个系统可以：

- 有 E2E，但没有 A2A：一个 Agent 从头做到尾，整链测试通过。
- 有 A2A，但没有 E2E：几个 Agent 能互相调用，但没有测试完整用户旅程。
- 同时有 A2A E2E：真实多 Agent 协作也被完整旅程测试覆盖。

### 10.4 Seelex 当前属于哪一种

当前 Seelex 有最小 L1 E2E；WorkPlan 也能把工作拆成多个模型节点。

但它还不是完整 A2A，原因是：

1. WorkPlan 节点通过同一个 ChatCompleter 调用模型。
2. 节点没有独立工具集和完整 ReAct loop。
3. 节点之间不直接发消息，而是通过共享 WorkflowContext 交接输出。
4. 没有独立的 Agent discovery、地址、会话或 A2A transport/protocol。
5. 当前 E2E 只覆盖单个 manual 节点，没有真实多 Agent fork。

所以最准确的说法是：**当前具备 WorkPlan 编排和 A2A-like 子任务拆分，但尚未完成、也尚未 E2E 验证完整 A2A。**

## 11. 代码审查发现

### 🚫 严重问题

#### 11.1 并行 auto 节点共享可变上下文

- 位置：`Seele/workplan/runtime/scheduler/scheduler.go:124-145`
- 位置：`Seele/workplan/sugar/auto/auto.go:27-35`
- 影响：data race、并行节点输入串扰、结果不确定。
- 建议：每个分支使用隔离的 WorkflowContext 副本；汇合时只合并显式输出、Vars 和 Result。

#### 11.2 fork 分支错误没有传播到整体结果

- 位置：`Seele/workplan/runtime/scheduler/scheduler.go:148-209`
- 位置：`Seele/agent/core/tool/builtin/workplan_handler.go:134-160`
- 影响：失败节点可能存在，但 `plan_run` 整体返回 completed。
- 建议：`fork` 返回 `(commonNext string, err error)`；任一分支失败时停止或按显式策略处理，handler 必须从 NodeResults 再校验失败状态。

### ⚠️ 警告

#### 11.3 Plan/节点 running 状态不可见

- 位置：`application/chat.go:121-140`
- 位置：`application/chat.go:378-399`
- 位置：`Seele/workplan/runtime/scheduler/scheduler.go:56-77`
- 影响：运行中仍显示 pending，长任务看起来像没有开始。
- 建议：增加 NodeStarted callback；`plan_run` start 时设置 `PlanRunning`；节点开始/结束各发事件。

#### 11.4 TUI 保存了 Edges，但没有生成 Depth/Children

- 位置：`application/chat.go:214-242`
- 位置：`application/state.go:71-99`
- 位置：`tui/plan.go:183-243,246-308`
- 影响：High/Max 的树形视图多数情况下仍是平铺列表。
- 建议：明确选择一种模型：要么 TUI 直接按 `Edges` 渲染 DAG，要么 Application 构造规范化层级；不要同时维护 Edges 和 Children 两份可能漂移的数据。

#### 11.5 Effort 并发上限没有硬约束

- 位置：`application/effort.go:23-47`
- 位置：`Seele/workplan/runtime/scheduler/scheduler.go:124-146`
- 影响：账号池、provider rate limit 和成本可能被瞬时 fan-out 打满。
- 建议：把 concurrency limit 作为 WorkPlan/Scheduler 配置，而不是只写在 prompt。

#### 11.6 `plan_run` 长时间持有工具锁

- 位置：`Seele/agent/core/tool/builtin/workplan_handler.go:125-135`
- 位置：`Seele/agent/core/tool/builtin/workplan_handler.go:168-175`
- 影响：`plan_status`、`plan_clear` 等需要同一把锁的操作会等待整个 Plan 结束；不能靠 `plan_status` 实时轮询。
- 建议：锁内只读取/替换 WorkPlan 指针；实际 Run 在锁外执行，并用独立执行状态保护重复运行。

#### 11.7 Seelex 与 Seele 的 `plan_load` 校验时机不一致

- 位置：`application/chat.go:197-243`
- 位置：`Seele/agent/core/tool/builtin/workplan_handler.go:45-110`
- 影响：Seelex 在工具实际成功前先创建 PlanState；如果 Seele 拒绝 edge 引用，UI 可能留下从未真正加载的 pending Plan。
- 建议：工具 start 只显示 loading 草稿；以 `plan_load` 成功结果提交正式 PlanState，失败时回滚或清空草稿。

#### 11.8 retry/skip 依赖主 Agent 再次行动

- 位置：`application/app.go:179-211`
- 影响：恢复不是确定性的；模型没有继续调用工具时，用户点击后不会真正重试或跳过。
- 建议：把 retry 变成 Application 直接调用 Plan executor，或引入显式的恢复状态机；skip 应成为 Scheduler 策略而不是重建整个图。

### 💡 测试缺口

当前应优先增加：

1. 两节点线性 Plan，验证 `{{.PrevResult}}`。
2. fan-out/fan-in Plan，验证并发输入隔离和 `PrevResults` 汇合。
3. 任一 fork 分支失败，断言整体 failed 且汇合节点不执行。
4. manual 节点 execute/skip/abort 三条路径。
5. `plan_run` start 后立即断言 `PlanRunning/NodeRunning`。
6. 非法 edge、环、孤儿节点导致 UI 状态回滚。
7. TUI High/Max 对 Edges/Depth/Children 的快照测试。
8. `plan_retry` 的 retry/skip/abort 完整 scenario fixture。
9. L2 Browser 测试 Plan panel、审批 modal、状态变化。
10. L3 Wails smoke 和 opt-in L4 live provider。

## 12. 五轴审查结论

| 维度 | 状态 | 评分 | 说明 |
|------|:----:|:----:|------|
| 正确性 | 🚫 | C | 线性/manual 可跑；并行上下文隔离和错误传播存在阻断问题 |
| 可读性 | ✅ | A- | 工具、Scheduler、Application、TUI 分层清楚；注释和数据类型较明确 |
| 架构 | ⚠️ | B- | 主链路边界合理，但并发限制、恢复和 A2A 语义仍停留在 prompt/共享上下文层 |
| 安全性 | ✅ | A- | 本次范围未发现密钥、注入或越权问题；manual Gate 已接入 ApprovalBroker |
| 性能 | ⚠️ | B- | fan-out 无硬限制，长持锁阻塞 status/clear；并发回调需等待最慢分支 |
| Go 专项 | 🚫 | C | 并行 goroutine 共享可变 WorkflowContext；当前稳定性测试未覆盖该路径 |
| 测试/E2E | ⚠️ | B- | L1 单节点 manual 主链稳定，但多节点、fork、失败恢复、Browser/Wails/live 均缺失 |

## 13. 建议的修改顺序

### P0：先保证并行执行不说假成功

1. 分支使用隔离 WorkflowContext。
2. fork 返回并传播 error。
3. `plan_run` 根据所有 NodeResult 决定整体 status。
4. 增加 fork 失败和 race 测试。

### P1：让状态展示与真实执行一致

1. 增加 PlanRunning 和 NodeStarted。
2. fork 节点完成后立即回调，不等待最慢分支。
3. TUI 直接基于 Edges 渲染，或统一构造层级模型。

### P2：把策略从 prompt 下沉为执行约束

1. Scheduler 增加 concurrency limit。
2. retry/skip/abort 变成确定性状态机。
3. `plan_run` 不再全程持有 WorkPlanTool.mu。

### P3：再声明完整 A2A

至少需要补齐：

1. 独立 Agent 身份和 role/account routing。
2. 每个 Agent 的独立上下文、工具与权限。
3. Agent 间任务和结果协议。
4. 超时、取消、重试和错误传播。
5. 真正的多 Agent E2E fixture，以及 Browser/Wails 可见性验证。

## 14. 最终判断

- [ ] 通过，可直接把 Plan/并行 A2A 作为完整生产能力发布
- [x] 有条件通过：线性 Plan、manual approval 和 L1 Core scenario 可继续使用
- [x] 不通过：并行 WorkPlan 和“完整 A2A 已实现”的能力声明

阻断条件是：并行 WorkflowContext 隔离、fork 错误传播、整体状态判定和对应 E2E 尚未修复。
