# Node 第一视角工作台（一阶段产物设计）

日期：2026-08-14
状态：**产物方案待批准；已实施"第一视角阶段日志 + 预定义语义结果返回 + 即时实时流"
数据面并接入子代理详情视图（GUI「第一视角」tab），真实 API 冒烟通过
（同一 subagent 多阶段认证 + 即时推送）**

一句话产物定义：**打开任意 node = 进入它的第一视角剖面**（动态）；mainagent 介入
node 走**与主会话完全相同的输入队列路径**（打断/注入/恢复都是队列操作）；node 结果
按依赖流向 mainagent 或 plan 中下一个 node，并被动打点 worktable。
**血缘线可视化不在一阶段范围**（不追求；继承/回报数据仍在快照中可见）。

---

## 1. 核心主张

1. **node == mainagent**：同一实体、同一套 View / Action / Result 协议，Role 不同。
2. **视图 = 第一视角动态剖面，不是消息转发**：看到的是 node 正在看的东西与当前动作
   （状态/轮次/正在执行的工具/上下文窗口/会话流）。
3. **介入 = 复用现有输入队列路径**，不是新机制：mainagent 的一切介入（问询/超时叫停/
   注入/恢复/终止）都只是向 node 的输入队列入队一条消息，与主会话 `Submit` 排队、
   `OnIterationComplete` 边界消费是同一套路径。
4. **结果返回依赖驱动**：node 结果流向"依赖它的下一跳"（mainagent 或下一 node）；
   每次返回被动打点 worktable 条目。

## 2. 现有消息队列路径（复用基线，先讲清楚）

主会话现状（application/core）：

- `Submit(text)` → 构造 `chatRequest{displayInput, modelInput, skills, budget}`；
  会话运行中 → 追加进 `inputQueue`（快照展示排队数）；空闲 → 直接开一轮；
- 框架 ReAct 循环在 `OnIterationComplete` 边界回调（seelex 侧实现）：
  - 队列非空且 Session-backed → `return false` 终止本轮，`runChat` 结尾把
    `deferredInputQueue + inputQueue` 批量合并起下一轮（一轮一消费）；
  - 旧装配路径 → 队列 drain + `combineChatRequests` 合并 + `Engine.AppendHistory`
    （user 消息）→ `return true` 继续 loop，下一轮 LLM 调用即看到；
- **框架只提供两个原语**：`Session.AppendHistory(msg)`（chat.go:243）与
  `LoopHooks.OnIterationComplete`（loop.go 同步回调）。队列本身是 seelex 的。

结论：**node 会话复用同一路径 = 给每个 node 会话挂同构的输入队列 + 同一消费钩子**
（seelex 侧实现，框架原语已足够）。node 会话当前没有这个队列——`AgentNode.Run`
是一次性 `agent.Chat(ctx, input)`，没有任何排队/注入入口。

## 3. 一阶段产物组成

### 3.1 视图：第一视角剖面（动态）

- **状态与动作**：running / paused / interrupted / done、当前轮次、正在执行的工具
  （数据面来自现有 tree 投影 + tool events + conversation + context snapshot，按
  node 聚合，无需新框架能力）；
- **上下文窗口**：当前装配片段清单 + 内容预览 + 来源 + token（seelex 装配器可捕获）；
- **会话流**：现有 `SubagentSessionDetail` 承载；
- **血缘线**：一阶段不追求（继承/回报内容仍可在上下文快照中可见，不做故事线视图）。

### 3.2 交互：介入 = node 输入队列（同一路径）

- 每个 node 会话持有一个与主会话同构的输入队列；
- mainagent 工具 `node_send`（统一入口，消息类型细分）：`ask` / `interrupt(reason)` /
  `inject(content, clear)` / `resume` / `stop`；
- 打断 = 入队 interrupt/stop → `OnIterationComplete` 消费时 `return false`
  （优雅，当前工具已完成）或 ctx cancel（硬）；
- 注入 = 先清空待消费队列、再入队内容 → 边界消费时 `AppendHistory` 进下一轮装配；
- GUI 详情面板提供同一组按钮。

### 3.3 结果流（Result Routing）

- 链式 plan（node1 → node2）：node1 结果写入 `WorkflowContext`，node2 输入读取
  （框架原语已在，seelex 的 `AgentNode.Run` 目前用 `_` 丢弃 wc，需接线）；
- 独立 fork / 根节点：结果返回 mainagent（merge-back，现有能力）；
- 被动效果：每次结果返回 tick worktable 对应条目（状态/结果摘要/耗时/目的地）。

### 3.4 暴露：GUI + mainagent 同一协议

- 同一份 DTO：`NodeView`（第一视角剖面）、`NodeSendResult`（入队结果）、
  `ResultRoute`（结果去向 + worktable tick 事实）；
- 纯 DTO 驱动，前端重构只动视图。

## 4. 用户故事（一阶段）

**视图**

- 作为 mainagent，我可以打开任意 node 的第一视角剖面，实时看到它的状态、当前轮次、
  正在执行的工具、上下文窗口与会话流，以便判断是否介入。
- 作为 GUI 用户，我可以在详情面板看到与工具同一协议的节点视图，以便不依赖命令行
  也能掌握子代理状态。

**介入（复用消息队列）**

- 作为 mainagent，当 node 超时时，我可以向它的输入队列发送 `interrupt(timeout)`，
  node 在当前迭代边界优雅停止，以便及时止损。
- 作为 mainagent，打断后我可以发送 `inject(content, clear)`（先清空待消费队列再
  注入），node 下一轮装配即看到修正上下文，以便纠正方向。
- 作为 mainagent，我可以发送 `resume` 让 node 从已有进度继续，或 `stop` 彻底终止，
  以便按实际情况决定去留。
- 作为 mainagent，我可以发送 `ask` 让 node 在边界回答当前工作状态（只读问询，
  不打断执行），以便在叫停前先了解情况。

**结果流**

- 作为 mainagent，node 完成时结果按依赖自动流向下一个 node（链式 plan）或返回
  给我（独立 fork），以便无需手工搬运中间产物。
- 作为 GUI 用户，每次结果返回 worktable 对应条目自动打点（状态/摘要/耗时/目的地），
  以便追踪交付物而无需手动登记。

**工具化**

- 作为 mainagent，我可以在不打开 GUI 的情况下用 `node_inspect` / `node_send`
  完成以上全部操作，以便自动化与脚本化编排。

## 5. 现状底座盘点

已有：

- 主会话输入队列模式成熟（`inputQueue`/`deferredInputQueue` + `OnIterationComplete`
  消费 + `combineChatRequests` 合并 + `AppendHistory`）；
- 框架原语：`Session.AppendHistory`、`LoopHooks.OnIterationComplete`、ctx cancel、
  workplan `WorkflowContext` + `SetNodeHook`、checkpoint/Resume；
- seelex 数据面：tree 投影、tool events、`NodeSessionConversation`、
  `NodeContextSnapshot`、`NodeWorktreeInfoFor`、scheduler 周期任务、worktable。

缺失（本产物要补，全部 seelex 侧）：

1. node 会话的输入队列 + 边界消费钩子（复用主会话模式）；
2. `AgentNode.Run` 结果写入/读取 `WorkflowContext`（node→node 接线）+ 返回 mainagent；
3. 结果返回的 worktable 被动打点；
4. `node_inspect` / `node_send` 工具与统一 DTO；
5. GUI 第一视角剖面面板（状态/动作/上下文窗口/会话流 + 介入按钮）。

## 6. Seele 还需要补什么（无血缘前提）

**结论：框架硬缺口清零。** 注入/打断/恢复可完全用现有原语在 seelex 侧实现
（复用主会话队列模式到 node 会话）；结果流用 workplan 原语接线即可。

仅保留两个可选增强（都不阻塞一阶段）：

- **可选 1：Session 状态机 + 视图查询**（`State()` / `View()`）——避免 seelex 在
  应用层维护双份运行状态镜像；一阶段可先靠现有 hooks/tree 镜像。
- **可选 2：`OnIterationComplete` 返回语义扩展**（终止 / 挂起等待 resume / 继续）——
  若要求"注入后挂起等人工确认再继续"的精细语义；主会话现用"终止本轮→起新一轮"
  已模拟该行为，非必须。

明确移出一阶段范围：血缘线可视化相关（handoff 事件、装配片段清单事件、
PromptBlock 来源字段）——不追求，不排期。

详见 `seele-gap-analysis.md`（无血缘前提重评）。

## 7. 一阶段验收标准

- 打开任意运行中 node = 第一视角剖面动态刷新（状态/轮次/当前工具/上下文窗口/会话流）；
- mainagent 用 `node_send` 完成闭环：问询 → 打断（超时/叫停）→ 注入（清空+注入）→
  恢复/重跑，且全部走 node 输入队列（与主会话同一路径）；
- 链式 plan 中 node1 结果到达 node2 输入；独立 node 结果返回 mainagent；
- 每次结果返回 worktable 对应条目被动更新；
- GUI 与工具行为一致；前端重构后契约不变；
- 验证门：`gofmt -l .`、`go build ./...`、`go vet ./...`、
  `go test ./... -count=1 -timeout=120s`、`go test ./e2e/... -run 'Docs|ModuleReadme'`，
  涉及真实 API 时跑 manual smoke。

## 8. 落地机制速查（支撑附录）

- **node 输入队列**：`seelebridge/session` 新增 per-node 队列 actor（或复用现有
  `SubagentSessions` actor 加 enqueue 命令），`AgentNode.Run` 把 node 会话的
  `OnIterationComplete` 接到队列消费（与主会话同构：非空 → return false 终止本轮，
  由 node 执行器起下一轮 / 或 drain + AppendHistory 继续）；
- **结果流接线**：`AgentNode.Run` 完成后写 `wc.PrevResults/PrevText`（沿用 workplan
  语义）+ 触发结果事件 → worktable tick；
- **探活/巡检**：`probe_subagents` / `probe_schedule`（`ask` 的支撑，一阶段可选）；
- **worktable tick**：挂在结果事件上的被动订阅。

## 9. 决策点（待用户拍板）

1. 打断语义：**优雅**（当前工具完成、迭代边界停止，推荐）还是硬打断（立即 cancel）？
2. 注入后：自动起下一轮（推荐，与主会话一致）还是"挂起等人工确认再恢复"？
3. node→node 结果流：复用 workplan `WorkflowContext`（推荐）还是 seelex 自建结果总线？
4. worktable tick 粒度：每个 node 一条结果条目 vs 按结果事件细分？
5. 探活/周期巡检是否并入一阶段（支撑 `ask`），还是后续单独排期？
