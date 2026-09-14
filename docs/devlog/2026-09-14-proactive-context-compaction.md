# 2026-09-14 上下文探测即主动压缩 + /compact 工具化（compact_context）

> 日期: 2026-09-14 | 范围: `application/core/context_runtime/coordinator.go`、
> `application/core/context_compact.go`（新）、`application/core/command.go`、
> `main.go`、`seelebridge/tools/policy.go`、`config/seele.yaml`、
> `computer_use_live_smoke_test.go`（新）、README 与测试

## 一、现场：超限错误被甩给了用户

真实会话里出现过：

```text
provider context exceeds the safe token budget: estimated=422265 budget=166808
```

预算 = `context_window − max_tokens − context_window/8` = 200000 − 8192 − 25000 =
166808（软阈值 75% = 125106、硬阈值 90% = 150127、压缩目标 60% = 100084）。根因不是
provider 报错，而是 Seelex 装配层的发送前门禁：那一轮的工具调用/输出把**最近一个
不可拆的协议单元**顶到全量预算之上，正常有界窗口压不动，于是直接拒绝发送。

## 二、改法一：探测即主动压缩（不是超限后再兜底）

装配结果一旦**逼近硬阈值**（预算 90%），立刻把可变 transcript 折叠为有界
checkpoint 帧（稳定 system 前缀 + 任务证据摘要 + plan + 当前输入）：

- 触发点是"探测到接近上限"，不是"已经超限"——给下一轮留出确定余量，也避免在
  窗口边缘反复抖动；
- 折叠形态仍装不下（如 system 指令自身顶满窗口）才返回
  `ErrProviderContextBudgetExceeded`；
- 压缩帧带协议前缀 `<!-- seelex:context-compact:v1 -->`，属动态尾部（不进可复用
  前缀），回合结束由 `history_safety.go` 清理；原始轮次仍完整留在会话存储，
  细节按 `read_tool_result` / `read_compressed_turn` / `search_history` 回读；
- 每次压缩留一条 `context_budget_autonomous` 记录（GUI 轨迹页可见）。

红→绿证据：`TestContextBudgetProactivelyCompactsAtHardThreshold`（单轮 ≈152.5k
tokens，夹在硬阈值与全量预算之间）在旧的"仅超限才折叠"实现下失败（贴着上限的
历史会被照发），改到硬阈值触发后通过。边界另一侧由
`TestContextBudgetOvershootKeepsNewestSettledRound`（≈130k，低于硬阈值 → 保留最新
轮细节）把住。

## 三、改法二：/compact 工具化（compact_context）

- `application/core/context_compact.go`：`CompactContextNow`（命令与工具共用落点）
  + `CompactContextHandler`（工具）。
- `/compact` 内置命令：同一入口，输出压缩版本/原因/压缩前消息数/估算 token。
- `compact_context` 工具（`main.go` 注册）：模型可在逼近上限或开始长任务前主动
  收拢上下文，参数 `reason` 写入压缩记录供审计。
- 结果分三类，不把"没做事"混成"出错了"：已压缩 / 没有任务执行纪元 / 未达软阈值
  （未达阈值不产生压缩记录、不改写状态）。
- 手动入口绕过"每个 progress epoch 只压一次"的自动节流（`prepareOptions.forceCompact`），
  但仍以"确实超过软阈值"为前提。
- 可见性：`compact_context` 对子代理不可见（它折叠的是共享会话的 provider 历史，
  并行子代理执行时属会话级单例状态）；权限默认 `allow`（非破坏性，原始轮次仍在
  会话存储）。

## 四、验证

```text
go test ./application/core -run "Compact|ContextBudget" -count=1 -v
go test ./application/... ./gui/... ./e2e/... -count=1
```

新增用例：`context_compact_test.go`（工具折叠并留记录、未达阈值如实 no-op、无任务
纪元提示、/compact 注册与同一落点）、`context_budget_last_resort_test.go`（硬阈值
主动压缩）、`policy_test.go`（子代理不可见 `compact_context`）。

**真实 API + computer use 端到端冒烟**（新增 `computer_use_live_smoke_test.go`，
`-tags computerlive`，走完整应用链路：`app.Submit` → 真实 provider → 模型自己调用
`computer_screenshot` → 截图落会话媒体分区 → 随下一次请求送入 → 回答必须命中真实
前台窗口标题）：

```text
$env:SEELEX_SMOKE_ACCOUNTS='config/accounts.yaml'
go test -tags computerlive . -run TestComputerUseLiveSmoke -count=1 -v -timeout=15m

真实 API + computer use 冒烟通过：
  session=sess_1789401264516404900
  截图引用=[media:ce842be8c006587a575a8f81146e3235e945cf1b2c5e2aa0d0e0be34d09ed204]
  命中标题片段="runtime_computer.go"
  前台窗口="runtime_computer.go - seelex - Visual Studio Code"
```

两条证据链各自独立：媒体引用出现在工具结果里且该 hash 在磁盘上真实存在（工具确实
被调用）；回答命中由 `computer.ForegroundWindow` 独立读出的地面真值（画面确实到了
模型眼前）。

## 五、未做 / 下一步

- 媒体 GC 前置条件仍未满足：截屏目前只被工具结果文本里的 ref 提到，
  `ToolResult.Multimodal` 尚未透出——接 GC 之前必须先补这条引用集。
- 压缩帧的摘要质量依赖 `TaskExecutionState.ContextSummary`（objective/plan/evidence）；
  若某类任务证据为空，折叠后模型只能靠回读，属可接受但不理想。
- `compact_context` 在子代理侧被禁用；若将来需要子代理自压，需要按节点作用域隔离
  会话历史（当前共享同一 provider 历史）。
