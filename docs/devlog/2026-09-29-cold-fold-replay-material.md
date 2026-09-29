# 冷加载/刚清空的折叠为什么从不叫模型：重放素材取错了源

日期：2026-09-29（承接 `2026-09-29-replay-material-not-wire-legal.md`）
范围：`application/core/context_runtime/coordinator.go`

## 0. 一句话

前缀重放素材此前**只**取 `existing`——装配开始时读到的引擎历史。可这条链上最常见的折叠恰恰发生
在没有在飞请求的时候（冷加载后的 `/compact`、会话级维护身份、冷加载后第一条消息触发的自动折叠），
那时引擎历史**还没有物化**，`existing` 必然为空，于是每一帧都落在 `no-replay-material` 上：帧里
只有一句"本次不调用模型"，模型从未被问到——而"模型为什么没被叫到"正是这条链上唯一无法从帧形状
反推的事实（读帧的人只能猜配置、猜账号）。

## 1. 现场

用户贴回的一帧（`origin=auto`、`reason=context_budget_autonomous`、`version=3`）：

```json
"summary_source": "local",
"summary_note": "无重放素材（上一次真实请求的引擎历史为空），本次不调用模型",
...
"layout": { "retain": { "all_context_tokens": 236830, ... }, "compared_tokens": 248783 }
```

判据量 24 万 token、被折区间 `message-1..message-1222`——内容是有的；缺的是**素材**。
`summary_note` 由 `chapter2Node` 在 `len(state.input.History) == 0` 时写下，而 `History` 就是
`CompactionFrameRequest.ReplayHistory`，装配层在 B 段传的是 `existing`：

```
application/core/context_runtime/coordinator.go
    existing := c.foldHistory(sessionID)          // 装配开头：读引擎历史
    ...
    push = c.pushCompactionFrame(..., replayMaterial, compactedRange)
```

冷加载/刚清空时 `foldHistory` 读到的就是空——引擎绑定要等这次装配把它物化出来。也就是说：
**素材源与"这次折叠的触发时机"是互斥的**，越常见的触发时机越拿不到素材。

## 2. 改法

素材首选 `existing`（前缀重放靠它命中前缀缓存，语义不变）；为空时改取 `fullContext`——本次装配
从 transcript 投影出的那份历史，也就是"如果这次不折叠、就会发出去的那份对话内容"。两者的角色
一致（都是"请求时点上模型会看到的对话"），而 `fullContext` 在任何折叠场景下都存在：装配正在用它
算判据量。规整与协议校验照旧由 `PrepareReplayMaterial` / `ValidateReplayProtocol` 把住
（前一轮那条 400 的修复对这条新素材源同样生效）。

## 3. 有牙证明

`application/core/context_compact_index_test.go` 的夹具（`fakeEngine{}`，引擎历史为空——正是冷加载
形态）里补一条判据：

```go
if len(request.ReplayHistory) == 0 {
    t.Fatal("引擎历史为空时重放素材不能为空：折叠会静默退化成没有模型摘要的本地折叠")
}
```

修复前该用例红在这一行（`TestFoldPushesCompactionFrameIntoIndex`），修复后绿；同一用例原有的
区间/回执/门禁判据全部照旧通过。

## 4. 验收

`go build ./...`、`go vet`、`gofmt -l`（改动文件为空）、
`go test ./application/core/... ./seelebridge/ ./seelexctx/... -count=1` 全 ok；随后全仓
`go test ./... -count=1` 63 包全 ok。

## 5. 边界

- 素材这条链上还剩两种"没有模型摘要"的来路：开关关闭（`no-summarizer`）与调用失败
  （`chunk-replay-failed`/`replay-failed`，带真实报错）。三者各自的 `summary_note` 都已写进帧正文，
  本轮不动它们的口径。
- `fullContext` 是**有界尾窗**：素材因此可能与"被折区间的最旧几段"不完全重合（模型看到的是"当时
  的对话状态"，与 `existing` 的角色一致）。要不要为冷加载场景改用"被折区间原文"当素材（更便宜、
  与帧语义更贴），属于另一个判断，本轮不猜。
