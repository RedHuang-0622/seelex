# 过程中的 LLM 痕迹：热挂载丢「思考过程」、冷加载丢「工具轮正文」

日期：2026-09-29
范围：`application/core/chat.go`、`application/core/chat/visible_output.go`（+ 两个新回归用例）

## 0. 一句话

一条回合的「过程」在可见会话里有两个来源，而这两个来源各有一次读错：
**思考过程（reasoning_content）** 只由回合收尾的 `attachLatestReasoning` 挂一次、且只挂最后一条 assistant
消息（工具轮的每一步永远拿不到自己的推理）——热挂载不重建正文，于是**热挂载后工具轮的思考过程为空**；
**工具轮的说明正文（narration）** 在框架 wire 上被丢弃（只有 `onChunk` 有），应用侧唯一的持久化归宿是
工具钩子边界的按迭代归位，而归位的输入读**可见窗口**——窗口不在尾部（回看态）时读到的是**别的轮次**的正文
（或空），落盘行因此没有本轮正文，**冷加载自然看不到「过程中 LLM 说了什么」**。

## 1. 热挂载丢思考过程

**红灯**（`application/core/hot_attach_reasoning_repro_test.go`，`TestHotMountKeepsStepReasoningInVisibleWindow`）：

```
--- FAIL: TestHotMountKeepsStepReasoningInVisibleWindow (0.00s)
    hot_attach_reasoning_repro_test.go:54: visible assistant reasoning = ["" "" "综合结果"], want ["先列目录" "再看文件" "综合结果"]
    hot_attach_reasoning_repro_test.go:60: assistant 步骤[0] reasoning = "", want "先列目录"（工具轮的思考过程在热挂载窗口里丢了）
```

**根因链**：

1. 可见窗口的推理只有一个来源：`chat.go:194`（回合收尾）`service.attachLatestReasoning(...)`；
2. 旧实现只取**一条**（`chat.go:807-812`：从历史尾部往前找第一条带推理的 assistant 就 break）；
3. 且只挂到窗口**最后一条** assistant 消息（`chat.go:824-840`）；
4. 热挂载不重建正文（`session_lifecycle.go:18-24`：`hotAttachSession` 只 `ensureSessionContent` +
   换视图指针）⇒ 内存窗口是唯一答案 ⇒ 工具轮的思考过程在热挂载窗口里为空；
5. 对照冷加载为什么正常：落盘走**按步骤**回填（`task_context/task_context_state.go:359`
   `BackfillAssistantReasoning`，配对判据见 `:409 findReasoningCandidate` = 工具调用 ID 集合 +
   归一化正文），rows 里每个步骤都带 `ReasoningContent`，冷读/分页回读因此看得到。

**改法**：`attachLatestReasoning` 改成**按步骤**挂——遍历引擎历史里带推理的 assistant 步骤（调用 ID 集合 +
归一化正文做配对身份），逐条与可见窗口里对应的 assistant 步骤配对、顺序消费，每个步骤挂**它自己**的推理，
并为每条真正写入的消息发一条 `message.delta`（`reasoning_content`）。配对口径与落盘回填同一把尺子；
一条都配不上时退回老口径（最后一条推理挂窗口最后一条 assistant 消息），既有语义与「已有推理不覆盖」的
保守策略都保留。

## 2. 冷加载丢工具轮正文

**红灯**（`application/core/cold_load_narration_repro_test.go`，`TestColdLoadKeepsToolWheelNarration`；
长会话冷加载尾窗 → 用户行开启本轮 → **前端自动翻更早一页**（`LoadMoreHistory(0)`，回看态）→ 流式说明正文
走 `appendDelta` → 工具轮 `OnLLMComplete`（`Content=nil`，只有 tool_calls）→ 生产钩子桥
`ToolHooks().OnToolStart`）：

```
--- FAIL: TestColdLoadKeepsToolWheelNarration
    cold_load_narration_repro_test.go:119: 写盘侧：工具轮事件正文 = "durable-15"，期望 "先看一眼入口，再决定改哪里。"（本轮流式正文）
```

**根因链**：

1. 工具轮正文在 provider wire 上不存在（框架 `session/loop.go` 的 `callLLM` 在带 tool_calls 时
   `Content: nil`），应用侧只能从 `onChunk` 拿；
2. 它的落盘归宿是工具钩子边界的按迭代归位（`handleToolStart` →
   `task_context.AttributeToolNarrationLocked`），输入是 `streamedAssistantTextLocked`；
3. 旧实现读**可见窗口里最后一条非工具 assistant 消息**的 `Content`；
4. 会话处于回看态（可见窗口未贴尾）时，`appendVisibleDelta` 按设计早退（增量不进窗口，避免挂到旧消息上）
   ⇒ 读到的是**别的轮次**的正文（上面红灯读到的 `durable-15` 就是上一轮）或空 ⇒ 工具轮 transcript 事件
   没有本轮正文 ⇒ 冷加载（durable rows）看不到；
5. 「宁可缺正文、不写错轮次」的代价是同一条链上**每一条**回看态回合都会丢正文。

**改法**：回看态下改读**请求作用域的流缓冲**（`chat/visible_output.go` 的 `VisibleOutputStream` 新增
`text` 累积与 `Text()`：`Consume` 里把 think 块剥离后的可见正文累积下来），`streamedAssistantTextLocked`
先取它、取不到再回退可见窗口（贴尾态与旧语义一致，非流式提供方也走回退）。

## 3. 有牙证明

- `TestHotMountKeepsStepReasoningInVisibleWindow`：修前 `["" "" "综合结果"]`，修后
  `["先列目录" "再看文件" "综合结果"]`；
- `TestColdLoadKeepsToolWheelNarration`：修前写盘侧拿到 `durable-15`，修后拿到本轮流式正文，且冷加载
  （新 Service `ResumeSession`）的可见会话里能找到该正文；
- 两个用例均保留为回归，随修复提交。

## 4. 验收

`go build ./...`、`go vet ./application/core/...` ok；
`go test ./application/core/... ./seelebridge/ ./gui/ -count=1`、`node --test gui/frontend/dist/*.test.mjs` 全绿。

## 5. 边界

- 两条链都改成「按步骤/按请求取数」，但**展示口径不变**：思考仍在 assistant 消息的 `reasoning_content`
  上（聊天区一行带过、轨迹区完整查看），正文仍走 assistant 消息的 `content`。
- 回看态**不写错轮次**这条原则保留：拿不到本轮正文时宁可空，也不把别的轮次正文挂到本工具轮上。
- 前端未改（`components.js` / `markdown.js` 的渲染路径本来就能显示这两种痕迹，缺的是后端没给）。
