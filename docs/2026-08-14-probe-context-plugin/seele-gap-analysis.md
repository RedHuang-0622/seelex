# Seele 缺口重评（无血缘前提，复用现有消息队列）

日期：2026-08-14
性质：调研结论（只读，未改任何代码）
范围：`github.com/RedHuang-0622/Seele@v0.1.2` + seelex 现有消息队列实现

## 0. 结论先行

**在不追求血缘线可视化的前提下，Seele 框架没有必须补的硬缺口。**

原因有两点：

1. **"运行中介入"可以完全复用 seelex 现有消息队列路径**——主会话的输入队列
   （`inputQueue`/`deferredInputQueue` + `OnIterationComplete` 边界消费 +
   `AppendHistory`）是 **seelex 侧实现**，框架只提供了两个原语
   （`Session.AppendHistory` 与 `LoopHooks.OnIterationComplete`）。给 node 会话挂
   同构队列 + 同一消费钩子即可，框架无需新增机制。
2. **结果流原语已在框架内**——workplan `WorkflowContext.PrevText/PrevResults` +
   `SetNodeHook`；seelex 的 `AgentNode.Run` 目前用 `_` 丢弃 wc，接线即可。

仅保留两个**可选增强**（都不阻塞一阶段）；血缘相关缺口**明确移出范围**。

## 1. 现有消息队列路径（复用基线）

主会话（application/core）：

- `Submit(text)` → `chatRequest{displayInput, modelInput, skills, budget}`；
  运行中 → 追加 `inputQueue`；空闲 → 直接开一轮；
- 框架 `OnIterationComplete` 边界回调（seelex 实现，chat.go:1325-1348）：
  - Session-backed：队列非空 → `return false` 终止本轮 → `runChat` 结尾批量提升
    （`deferredInputQueue + inputQueue`）起下一轮，一轮一消费；
  - 旧装配路径：drain + `combineChatRequests` + `Engine.AppendHistory` →
    `return true` 继续 loop；
- 框架原语：`Session.AppendHistory(msg)`（chat.go:243，带锁）、
  `LoopHooks.OnIterationComplete`（loop.go 同步回调，注释已预留"输入队列等场景"）。

node 会话现状：`AgentNode.Run` 一次性 `agent.Chat(ctx, input)`，**无输入队列、无
OnIterationComplete 队列消费、无 AppendHistory 注入入口**——这是 seelex 侧缺口，
不是框架缺口。

## 2. 按"调用方能否自补"重评

### 2.1 硬缺口：调用方补不了，必须 Seele 补

**无。**

打断（优雅 = `OnIterationComplete return false` / ctx cancel）、注入
（队列 drain + `AppendHistory`）、恢复（终止本轮 → 起新一轮，主会话已模拟）、
结果流（`WorkflowContext` + `SetNodeHook`）全部可 seelex 侧实现。

### 2.2 可选增强（不阻塞一阶段，Seele 值得考虑）

**E1 Session 状态机 + 视图查询**

- 现状：`Session`/`ReActLoop` 无 running/paused/interrupted 状态、无 `View()` 快照；
  seelex 靠 hooks/tree 镜像状态（容易与真实状态漂移）。
- 价值：`State()` / `View()`（轮次/当前工具/装配片段/token/状态）让第一视角剖面
  有单一事实源；一阶段可先用 seelex 镜像。

**E2 `OnIterationComplete` 返回语义扩展**

- 现状：只能 `return false`（终止）或 `true`（继续），无"挂起等待"。
- 价值：若要求"注入后挂起、等人工确认再恢复"的精细语义，可扩展为
  `false`（终止）/ `paused`（挂起等待 resume）/ `true`（继续）；
  主会话现用"终止本轮 → 起新一轮"已模拟该行为，非必须。

**E3 结果路由事件（可选）**

- 现状：workplan `SetNodeHook`（NodeResult）+ `WorkflowContext` 已有，但无
  `result.returned` 事件携带"目的地 = mainagent / 下一 node ID"。
- 价值：worktable 被动打点、统一审计有单一事实源；seelex 也可自行在 `AgentNode`
  完成处触发，框架事件只是更干净。

### 2.3 明确移出一阶段范围（不追求）

- 血缘线可视化（继承片段故事线、装配片段清单事件、handoff 事件、PromptBlock
  来源字段）——不排期；继承/回报内容仍可在上下文快照中可见。

## 3. 对照一阶段产物的缺口映射

| 产物需求 | 现有支撑 | 缺口 | 谁补 |
|---|---|---|---|
| 第一视角剖面（状态/轮次/当前工具/会话流） | tree 投影 + tool events + `NodeSessionConversation` + `NodeContextSnapshot` | 无硬缺口（按 node 聚合展示即可）；E1 可选 | seelex |
| 上下文窗口（装配片段清单） | seelex 装配器（ScopeAssembler）可捕获 | 无硬缺口 | seelex |
| 介入 = 复用消息队列（ask/interrupt/inject/resume/stop） | 主会话队列模式 + 框架 `AppendHistory`/`OnIterationComplete` | node 会话缺队列与消费钩子 | seelex |
| 打断/恢复 | `return false` / ctx cancel / 新一轮 | 无硬缺口；E2 可选 | seelex（+可选 Seele） |
| 结果返回（mainagent / 下一 node）+ worktable tick | workplan `WorkflowContext` + `SetNodeHook` + merge-back | `AgentNode.Run` 丢弃 wc；结果事件 | seelex（+可选 E3） |
| node == main 统一协议 | 同一 Session/Assembler/hooks | 无硬缺口 | seelex |

## 4. 建议（对 Seele 版本规划）

- **一阶段不需要 Seele 发版**：全部 seelex 侧落地；
- 若想顺手给框架补"第一视角单一事实源"，做 E1（状态机 + `View()`）；
- E2（挂起语义）与 E3（结果路由事件）按产品节奏可选，均向后兼容增量；
- 血缘相关能力（handoff/装配片段事件/PromptBlock 来源字段）等产品决定要故事线时
  再排，不在此列。

## 5. 验证方式

- 介入复用队列：node 会话 `OnIterationComplete` 消费队列与主会话同一实现路径
  （同一 helper），注入消息进入下一轮装配；打断在当前工具完成后停止；
- 恢复：终止本轮 → node 执行器起新一轮，已有进度（history）不丢；
- 结果流：链式 plan 中 node1 完成后 node2 输入包含 node1 结果；
- worktable tick：结果返回后条目自动更新，无需人工登记。
