# 前缀重放素材不是 wire 合法字节序：那次 400 的真因、复现与修复

日期：2026-09-29（承接 `2026-09-29-replay-fast-fail-local-fold.md`）
范围：`seelexctx/replay_material.go`（新）、`seelexctx/replay.go`、`seelexctx/dag.go`

## 0. 一句话

上一轮把"降级原因"写进帧证据的改动**当场兑现**：19:18:52 那帧的 `summary_note` 直接带回了
provider 的原始报文。报文把范围从"猜是账号限额还是配置漂移"缩到了一条：**重放素材里有一条
assistant 宣告的工具调用没有回执**，provider 按配对规则整条拒收。素材本身忠实复刻了历史，
错在它取自**请求出口协议修复之前**的引擎历史快照——这条链上的每一环此前都没有做 wire 规整。

## 1. 现场与硬证据

帧正文（`session-76e94811332dd7a6`，19:18:52，`origin=explicit_after_turn`，
`reason=context_budget_autonomous`）：

```
"summary_note": "前缀重放两次调用均失败，已回退本地折叠：seelexctx: prefix replay
completion: agent.bridge: complete with account \"agent-1\": HTTP 400:
{\"error\":{\"message\":\"An assistant message with 'tool_calls' must be followed
by tool messages responding to each 'tool_call_id'. (insufficient tool messages
following tool_calls message) (request_id: 1c584b19-fb66-429b-8384-689f23d31bde)\",
\"type\":\"invalid_request_error\",\"param\":null,\"code\":\"invalid_request_error\"}}"
```

同一形状在 18:06:19 那次折叠（`session-7834e40e49e594bc`）也出现过一次，报文逐字相同。

代码顺序（`application/core/context_runtime/coordinator.go`）——素材是**修复之前**的快照：

| 位置 | 事实 |
| --- | --- |
| `prepareExecutionContextFor` 开头 | `existing := c.foldHistory(sessionID)`：装配一开始就把引擎历史读进内存 |
| 装配中段 | `replacement := c.withInFlightTail(existing, assembled)` → `replaceFoldHistory` |
| 紧随其后 | `c.history.PrepareProviderHistoryFor(sessionID)`：**这一步**才补残缺工具链（`RepairInterruptedToolChains`，合成占位） |
| B 段（锁外） | `pushCompactionFrame(..., existing, ...)`：把**开头那份 `existing`** 当 `ReplayHistory` 送给压缩 DAG |

也就是说：真实请求走的是"修复后"的字节序，重放请求走的是"修复前"的字节序。两者只差一条
未回执的声明，而这一条足以让整条请求 400。

这条未回执的声明从哪来：工具轮被中断（用户停/进程断/请求失败）时，assistant 那条宣告已经
落进引擎历史，结果没有落上。装配层对残缺链的补齐是**故意**推迟到请求 seam 的
（`PrepareNewHistoryContentFor` 的注释："避免把'即将执行'的工具调用误判为中断丢失而注入占位"），
所以"中断发生 → 请求 seam 修复"之间那段窗口里，引擎历史里就是一条裸的未回执声明——而折叠
恰好落在这个窗口里（回合结束后/冷加载后的自主压缩）。

旁证：本会话耐久转写（`message/message_*.jsonl`，2138 行）里 **没有任何**配对违规，也
**没有任何** `Seelex recovery note: interrupted tool call` 占位——占位只活在引擎历史里
（provider-only，不渲染、不入转写），符合设计；而模型侧能看到这些占位，说明真实请求确实
拿到了修复后的字节序。

## 2. 复现：同一条 provider，同一个报文

`seelebridge/replay_live_broken_chain_probe_test.go`（`-tags replayprobe` +
`SEELEX_REPLAY_PROBE=1`，真账号）直接用生产的 QuickChat 通道发三种字节序：

```
A1-未规整-尾单元未落定      失败（77ms）err=... HTTP 400: ... (insufficient tool messages following tool_calls message)
A2-未规整-中段死链          失败（52ms）err=... HTTP 400: ... (insufficient tool messages following tool_calls message)
B1-规整后-尾单元丢掉        成功（1.404s）
B2-规整后-中段补回执占位    成功（1.821s）
C1-生产入口-尾单元未落定    成功（1.748s）chapter2="### 目标 (Goal)\n核对装配路径，并给出证据。..."
C2-生产入口-中段死链        成功（1.861s）chapter2="### 目标 (Goal)\n- 任务1：核对装配路径，并给出证据。..."
```

A 臂的报文与现场逐字一致（含同一账号 `agent-1`），**同一条素材**规整后 B/C 臂全部成功——
因果链闭合。同时这也解释了 18:06 那次"458ms 做完两次重放"的疑点：一次注定被拒的调用
50~80ms 就返回了，两次加起来正好落在那个量级，不需要再假设 ctx 超时或账号租约。

## 3. 修复：素材进 wire 之前按协议规整

新增 `seelexctx/replay_material.go`：`PrepareReplayMaterial`（规整）+
`ValidateReplayProtocol`（校验，可自答的判据）+ `ReplayMaterialReport`/`ReplayMaterialEvidence`
（事实进帧证据）。两条规则，都不发明事实：

1. **未落定的尾单元整段丢掉**（尾巴上的声明仍有未回执调用，且其后只有它自己的结果行）。
   这是"工具正在跑"或"上一轮被中断且没有后续"的形态。它不属于任何**已发出**的请求字节
   （上一个请求发出时它还没产生），补占位等于对一个可能正在执行的调用宣布"结果丢了"；
   丢掉既合法，又比补占位更贴近 provider 已经缓存的前缀。
2. **中段死链补回执占位**（声明之后还有别的消息 → 不可能是"正在跑"）。占位正文与装配层
   对真实请求补的那条**逐字相同**（`[Seelex recovery note: interrupted tool call "..." may
   not have executed; ...]`），因此素材与已发出字节仍对齐；空 ID/行内重复 ID 声明、孤儿结果、
   乱序结果沿用同一套既有口径（`repairToolPairing`）。

接两处：

- `chapter2Node`：**在分片之前**规整一次（分片切分与片 token 计数因此都基于合法素材），
  规整事实进 `state.replayMaterial` → merge 时写进帧证据 `replay-material:normalized`；
  规整后仍不合法 → 不发这次注定被拒的请求，记 `replay-material-invalid`（带违规位置）；
  素材被规整到空 → 记 `no-replay-material`（指名那条未回执的调用）。
- `quickChatPrefixReplaySummarizer.Summarize`：wire 出口再兜一次（幂等），分片链的每一片与
  任何直接调用摘要器的调用方都被覆盖；兜底后仍不合法则显式失败并指名位置。

顺手把降级原因写得更具体：`no-replay-material` 的 note 现在会说清是"引擎历史为空"还是
"规整后为空（宣告的调用 X 尚无回执）"。

## 4. 验证

- `seelexctx/replay_material_test.go`：尾单元丢弃（含幂等）、中段死链补占位（位置在声明结果块
  内、后缀文本在其后、幂等）、重复宣告逐次配对（合法时逐字不变；第二次声明缺回执时补占位且
  不抢第一条结果）、孤儿剔除、乱序搬回、合法素材逐字不变且**不留证据**、证据正文指名未回执
  调用、校验器五种违规各自指名位置、摘要器出口规整与"规整到空就显式失败"。
- `seelexctx/replay_material_dag_test.go`：DAG 集成四例（尾单元丢 + 实发素材合法 + 帧带证据、
  中段补占位、合法素材逐字且无证据、规整到空落 `no-replay-material` 且不调用模型）。
- 真账号探针如上（A/B/C 六臂）。
- `go build ./...`、`go vet -tags replayprobe ./seelebridge/`、全量 `go test`（见提交说明）。

## 5. 遗留与边界

- **素材快照仍然早于请求 seam 修复**（`existing` 在读入时定稿）。wire 出口规整已经让这件事
  无害，但"素材 == 上一次真实请求的字节"这条口径仍不是严格成立（差一条占位/一条被丢的尾单元）。
  真要抠前缀缓存命中率，可以让装配层把 `PrepareProviderHistoryFor` 的产物也喂给重放素材——
  那是独立的一步，不在本次范围内。
- **in-loop 控制器路径目前不注入摘要器**（`MainCompactionDAG` 与 `seelexController` 的差别），
  所以"工具正在跑时折叠"的活体形态暂时打不到重放请求上；本次的尾单元规则是为它预备的
  （等那条路径接通时，素材尾巴正好就是活的未落定单元）。
- 本机 GUI 仍是旧 exe：重启后新代码才会进运行进程（本次不需要改配置）。
