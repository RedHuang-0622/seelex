# 2026-09-20 前端：提交按会话显式路由 + 细节动效（数字 / 增量 / 滚轮拖动）

范围：`gui/frontend/dist`（前端壳层与视图）、`docs/gui/CHANGELOG.md`。本文只记前端部分；
压缩窗口那一批（`application/core`、`seelexctx`、`seelebridge`）的改动留在同一工作树里未提交。

## 1. 用户报告

> 一个运行中的会话依然会污染到输入框内容的发送，场景是一个会话在运行中然后我想发送消息到
> 一个没有运行中的会话。前端更改完之后做出更新。

> 包括数字变更的细节和工具过程中操作类型增加的细节动效，多用滚动滑动滚轮等拟物操作。

## 2. 根因（前端一路）

composer 的提交此前是 `invoke("Submit", text)`（ambient）。后端 `Service.Submit` →
`components.input.Dispatch` → `submitConversation`，**路由只读 `Core.Snapshot.Session.ID`**
（后端"当前视图会话"）。前端另有一条并发路径：`resumeSessionFromList` 发起 `ResumeSession`
后，后端可能先动视图指针（`beginAsyncRestore`：`Session = {目标, restoring}`）再异步装载，
渲染层要等权威快照才切过去——这就是切换在途的 TOCTOU 窗口。

后端早已有会话级 API 且注释写明这类事故：

```
application/core/session_scope.go（SubmitToSession）
  // 路由只认显式 sessionID，绝不重读「当前会话」：SubmitToSession 与
  // 切换（ResumeSession/hot_attach）之间存在 TOCTOU 窗口，若先读 current
  // 再委托 Submit（内部再读一次 current），切换落在两次读之间会把 A 的
  // 输入路由进 B 的队列（压力测试 TestStressConcurrentSessionsDoNotPollute
  // 抓到：queued-2 进入 sess-4 视图）。
```

设计口径也早已定下（`docs/gui/modules/multi-session-pages.md` §6）：

> 旧 `Submit/Cancel/Resolve/Snapshot` 在迁移期委托 active session；**新前端一律使用显式
> session ID**。

缺口就在前端：composer 仍走 ambient `Submit`，没有把"我此刻在看哪个会话"钉死。

## 3. 修法

`composer-input.js` 新增两个纯函数（无 import，`node --test` 直接覆盖）：

- `isSigilInput(text)`：`/` `#` `$` `@` 开头（与后端 router 的 `HasPrefix` 同一判据——
  只看首字符，不管后面有没有内容；`#` 空名在 router 里是 no-op、`@` 空名有语义，都不该被
  当普通对话发出去）。
- `composerSubmitPlan({ text, viewedSessionID })` → `{ rpc, args }`：
  - 普通对话 → `SubmitToSession(<视图会话 ID>, text)`：**绝不重读后端 current**，运行中会话
    不会再"吸"走发给空闲会话的输入；
  - sigil → `Submit`：前缀→用例的映射是后端路由器的职责，切换在途的窗口由输入区锁覆盖
    （`chat-view.js` `renderControls(switching)`）；
  - 拿不到视图会话 ID（快照未就绪）→ 退回 `Submit`（宁可走老路，也不往空 ID 上投）。

`app.js` 的 composer `submit` 监听改为：

```js
const plan = composerSubmitPlan({ text, viewedSessionID: client.current()?.session?.id });
await invoke(plan.rpc, ...plan.args);
```

视图会话 ID 在 `await` 之前取——这是本次提交要发往的会话，而不是 RPC 回来时的会话。

## 4. 细节动效（拟物）

新增 `gui/frontend/dist/motion.js`：纯规则 + 只读 DOM 应用器（模块被导入时不碰 `document`，
因此 `node --test` 可以直接 import，不需要 app.js 那样的源码级替换）。三条：

| 动效 | 规则（motion.js） | 落点 |
|---|---|---|
| 数字变更 | `countParts`（恰好一个整数段才滚）+ `tweenValue`（easeOutCubic 取整）+ `rollNumber`（后一次滚动取消前一次，不来回跳） | 工作表格 `N 项` / `N 打点` / 类型·会话·实发计数；右栏未读角标 `N 未读` |
| 增量入场 | `markEntering`（只在**首次插入**挂一次性类） | 工作表格新建主行 / 新建 trace 展开行（td 上闪黄铜底）；会话与轨迹条目（自下抽出） |
| 滚轮 / 拖动 | `wheelScrollDelta`（到边/不足一屏放行）+ `bindHorizontalWheelDelegate` + `bindHorizontalDragDelegate`（过阈值才算拖动，吞掉随之而来的 click） | sheet 栏、`#conversation-tabs`、`#right-tabs`（`.scroll-edges-x`） |
| 滚动边缘 | `scrollEdges` / `syncScrollEdges` / `bindScrollShadows` / `syncAllScrollShadows` | 限高滚动块（工作表格行区、项目抽屉、右栏各列表、git log） |

样式在 `styles.css` 第 26 节：`.is-rolling`（提色温 + tabular-nums）、`cell-enter` /
`row-draw-in`、`.scroll-shadows.is-scroll-up/down`、`.scroll-edges-x.is-scroll-start/end`。

减少动效偏好下：入场动画关闭（`animation: none`），app.js 的 `setupScrollAffordances()`
直接不接管滚轮/拖动，数字直接落终值。`prefersReducedMotion` 收敛为 motion.js 一处实现，
app.js 不再自带一份。

## 5. 验证（红 → 绿 / 全绿）

新增与更新的前端用例：

```text
$ node --test gui/frontend/dist/motion.test.mjs
    tests 15 / pass 15 / fail 0
    （countParts 单/多整数段；tweenValue 端点与单调；rollNumber 无 rAF 落终值 / 逐帧滚 /
      后一次取消前一次 + is-rolling 生命周期；scrollEdges 到边；wheelScrollDelta 到边放行；
      滚轮与拖动委托只接管 .scroll-edges-x；draggable 页签上的拖动不被接管；markEntering）

$ node --test gui/frontend/dist/composer-input.test.mjs
    tests 12 / pass 12 / fail 0
    （原有 8 例 + 路由 4 例：普通对话→SubmitToSession；sigil→Submit；空 ID→Submit；
      app.js 源码级断言：必须经 composerSubmitPlan、不得残留 invoke("Submit"）

$ node --test gui/frontend/dist/work-table.test.mjs
    tests 27 / pass 27 / fail 0
    （原有 26 例 + 新建行/trace 行挂入场类、计数走 rollNumber、sheet 栏带 scroll-edges-x）
```

全量：

```text
$ node --test gui/frontend/dist/*.test.mjs
    tests 386 / pass 386 / fail 0

$ node --check gui/frontend/dist/*.js *.mjs
    ok
```

**未取到的证据（如实记录）**：本机没有 WebView，动效的像素观感（滚动边缘阴影的明暗、
sheet 栏拖动的手感、里程表在进位时的读数）只能靠真机走查；仓库既有口径同样是"算法已测、
像素待 E2E"（见 `docs/gui/code-review.md` §4 警告 1）。切换期 composer 处于「正在切换会话…」
的那一帧也仍未截到——锁窗口比观测通道短，判据落在
`chat-view-input-lock.test.mjs` 的 7 例上。

## 6. 顺带回答两个 review 问题（压缩窗口那一批，未改代码）

**Q1：`TokenAudit.TargetAfterCompaction` 仍报静态 60%（"全仓无读方"）——有改动的必要吗？**

结论：**"全仓无读方"不成立，但对新的保留窗口策略确实没有影响；改口径属独立（契约面）变更，
建议做，但不该并进压缩那一批。** 证据：

- 定义：`application/core/task_context/token_counter.go:171`
  `TargetAfterCompaction: budget * 60 / 100`（`newContextBudget`）。
- 读方有两处（都不是"无读方"）：
  1. `application/core/context_runtime/coordinator.go:358` —— 写进
     `model.TokenAudit.TargetAfterCompaction`（`application/model/context.go:58`，JSON
     `target_after_compaction`），是**对外播报**；
  2. `application/core/context_runtime/coordinator.go:612`
     `protectOversizedCurrentInputLocked`：`CountTextTokens(currentInput) <=
     budget.TargetAfterCompaction/2` 就原样放行，否则把这条超长用户输入归档成
     `result_ref` 并替换成引用警告——**这是行为读**（单条输入上限 = budget×30%）。
- 但它已不是"压缩后目标"：保留前缀改由 `WindowConfig.RetainedContextTokens`
  = min(`window.retain_tokens`, `window.ratio × all_context`) 决定
  （`seelexctx/window.go`），`application/core/session_history.go:293` 明确记着"旧口径 60%
  的 TargetAfterCompaction 已不再是读尾来源"，`context_window_rule_test.go:207` 也把
  "新读尾预算 ≠ 旧 60% 数字"钉住了。

所以：新策略不依赖它 ⇒ **不动压缩行为是对的**；但字段名与播报口径已经在说谎 ⇒ 该改，且因为
`model.TokenAudit` 是 DTO/JSON（前端/schema 会读），属**独立变更**。最小的两步是
(a) 审计改报实际保留窗口（同一份 `RetainedContextTokens`），(b) 把 `:612` 的阈值改名成它
真正表示的东西（单条输入上限），并把 `/2` 从调用点收进预算构造——不要留着
`TargetAfterCompaction` 这个名字同时表示两件不同的事。

**Q2：`seelebridge.windowTailBudget()` 是 D1 窗口策略（轮数维度），对新策略够用吗？**

结论：**作为"单位上限"够用且应当保留；作为"token 宽度"不够用。** 证据（
`seelebridge/runtime_context.go:286`，注释本身就写明了两条边界）：

- `tokenBudget = r.ContextWindow()`。保留前缀被 `RetainedContextTokens` 夹在
  `[1, all_context]`，而读尾的 `all_context` 就是账号上下文窗口 ⇒ 这一维**实际不约束**，
  对新策略形同 no-op（既不会收窄也不会放大）。
- `maxUnits` 走 `WindowPolicy.WindowRounds(ctx, ProviderContextInfo{ContextTokens})`：
  `AvgRoundTokens`/`ReservedTokens` 缺省 ⇒ `seelexctx/window.go` 的 `WindowRounds` 在
  `info.AvgRoundTokens <= 0` 时**直接回退** `min_rounds`（返回
  `errWindowPolicyUnavailable`）；只有显式 `window.rounds > 0` 时才提前返回 `rounds`。
  也就是说 clamp 公式与 `max_rounds`/`ratio` 在这条路径上是**死接线**，实际值 =
  `window.rounds` 或 `window.min_rounds`（=4）。
- 轮数是 D1 自己的轴（"从磁盘读哪些分片"的**单位上限**），换成 token 规则是范畴错误，而且
  你担心的"与 rounds/min_rounds/max_rounds 互踩"是真的。

所以够用/不够用的分界是：**轮数轴保留**（它表达"读几轮"，与压缩保留前缀无冲突）；**token 轴
应当改从同一份 `window.retain_tokens` / `window.ratio` 推导**，否则同一棵树里会有两个都自称
"窗口"的宽度——`application/core/session_history.go:295` 的 `RetainedReadTailBudget`（新规则）
与 `seelebridge/runtime.go:600` 的 `SetTailBudget(r.windowTailBudget())`（D1）可以不一样大。
要"自适应轮数"则需给 `ProviderContextInfo` 补 `AvgRoundTokens`/`ReservedTokens`（token_counter
已有估算），那确实是另一条决策，与本次口径收敛分开走。

## 7. 提交范围

本批只提交前端部分：`gui/frontend/dist/**`（含新增 `motion.js` / `motion.test.mjs`、更新的
`app.js` / `work-table.js` / `conversation-view.js` / `composer-input.js` / `styles.css` /
`index.html` 与各 `*.test.mjs`）以及前端的这两份文档。压缩窗口那一批
（`application/core`、`seelexctx`、`seelebridge`、`main.go`、`config/seelex.yaml` 等）保持未提交。
