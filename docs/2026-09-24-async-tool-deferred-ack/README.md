# 异步工具「受理回执 + 尾部补记」（A 链路）· 打点台账

> 状态：**A 链路（推送/补记）2026-09-24 已回滚，未实现**；**轮询型切片同日实现，默认关**
> （见 §8.6 台账；`limits.async_exec.enabled: false`）。§8.3 五指标 A/B 同日实测完成，
> 结论见 [`ab-2026-09-24.md`](ab-2026-09-24.md)：**两条硬判据（墙钟、进程表/目录）成立，
> 成本判据（总 prompt ≤ A×1.10）不成立（+158%）**，所以切片保持默认关。所有者裁定回到
> "工具串行阻塞"，本文 §0 的实测事实与 §1 不变量仍然有效（它们是证据，不是实现），
> 但 §4–§6 的推送补记路线不再推进。
> 落地形态改为**轮询型**：后台命令返回一个会话句柄，结果由模型自己的下一次工具调用取回
> （对齐 Codex `unified_exec`/`write_stdin`；避开 idle 唤醒链路与"隔着几万 token 认 id"两个问题）。
>
> 本文是一次性工作包的进度台账，不是长期事实来源；完成的判据是代码与测试。
> 长期口径一旦落地要回写 [`docs/arch/context-prefix-chain.md`](../arch/context-prefix-chain.md)
> 与 [`docs/2026-09-08-session-storage-architecture/my_design.md`](../2026-09-08-session-storage-architecture/my_design.md) 的不变量表。

## 0. 事实来源（2026-09-24 真实 API 冒烟）

探针：仓库根 `async_tool_wire_live_probe_test.go`（tag `asynclive`，opt-in，不进 CI）。
provider=openai 兼容端点（api.deepseek.com），model=deepseek-flash，thinking 模式默认开。

| 探针 | 结果 | 结论 |
|---|---|---|
| P1 冷/暖基线 | `http=200`，ratio `0.00 → 0.94` | 前缀可缓存；运行级 nonce 隔离生效 |
| P2 A 链路（ack 定稿 + 尾部补记） | `http=200`，ratio **0.95** | 尾部追加**不破**缓存 → A 成立 |
| P3 中段改写 4 个字 | `http=200`，ratio **0.25** | 命中停在改写点 → 「定稿不可改写」有实测依据 |
| P4 tool 结果回填到若干轮之后 | `http=400` `insufficient tool messages following tool_calls message` | **B（一行账本）在 wire 层非法** |
| P5 同 `call_id` 两条 tool 回执 | `http=400` `Messages with role 'tool' must be a response to a preceding message with 'tool_calls'` | 补记**不能**用 `role=tool`，只能是 user/internal |
| P6 Responses API 有状态链 | 跳过（该端点无 `/v1/responses`） | **未验证**，不得当作已排除 |
| P7 轮询形态（两次 `async_output` 取回 + 取回完追问） | 三次请求全 `http=200`；cached 随前缀单调增长 **3328 → 3456 → 3584 → 3584**，ratio 稳定 **0.94**，未命中停在 212 | **轮询不破前缀缓存**：每对取回都是相邻的 `tool_call`/`tool_result`，历史只在尾部生长；边际成本 ≈ 每轮 +130 prompt token |

方法学记录（重要）：provider 缓存 TTL 长于进程。不加运行级 nonce 时，第二次运行会命中
上一次自己写进去的缓存，P3 曾因此测出「0.95 不塌陷」的**反向结论**。凡测缓存命中的用例
必须自带一次性盐。

A/B 另有一条口径（§8.3 实测踩到）：开与关两臂的**工具 schema 不同**（关闭态不下发
`background`、不注册 `async_output`），因此两臂的缓存前缀天然不相交，每臂**首个请求**
必定付一次冷启动 miss。拿首个请求的比率判「轮询是否破前缀」会得出反向结论——必须看
轮内比率序列与未命中字节（`prompt_cache_miss_tokens`），而不是单点比率。

## 1. 不变量（拟新增，编号接存储架构 §4）

- **I-16 定稿不可改写**：已进入 `context` 区（append-only 已定稿轮次）的字节，此后任何
  事件都不得回写。实测依据＝P3。
- **I-17 回执恰一次且与调用同单元**：每个 `tool_use` 在其所属回合内恰有一条 tool 回执；
  `deferred` 的回执内容是「已受理」，不是结果。实测依据＝P4/P5。
- **I-18 迟到者自成尾部单元**：完成补记以 `call_id` 为键落在**新单元**，不参与其原单元的
  定稿判定；补记在 wire 上的位置只取决于物理到达序，因果边（`call_id` / `origin_round`）
  只存在于载荷，**永不参与排序**。
- **I-19 未定稿单元不入压缩区**：含未决 `call_id` 的单元不算「完整协议单元」，既不得被折出
  保留窗口，也不得进压缩候选（否则 ack 被摘要掉，补记失去回指锚点，且 provider 会因未回答
  的 tool_use 拒请求）。落点＝`application/core/context_runtime/layout.go` 的 `RetainDecision`
  与四区判定。
- **I-20 思考正文属于定稿字节**：`reasoning_content` 必须逐字节持久化并原样回传
  （实测：不回传直接 `400`）。Seele 侧已具备（`agent/core/api/strategy_openai.go:57`），
  Seelex 侧要钉住「装配重放不得丢/不得改写」。

## 2. Seele 底层判定（带证据）

结论：**A 链路不需要修改 Seele**；三条理由与一条待观察项。

1. **wire 序列由 Seelex 决定的部分已经够用**：Seele 的 ReAct 循环
   （`session/loop.go:327` `for _, tc := range assistantMsg.ToolCalls`）是**串行**执行工具并把
   结果紧接 append 进 `history`，因此「tool 回执必须紧跟 tool_calls」这条 provider 契约
   **自动满足**；ack 只是一条普通 tool 结果字符串，Seele 不需要知道「异步」这个概念。
2. **补记注入有现成口**：Seele 提供 `Session.AppendHistory`（`session/chat.go:297`）；
   Seelex 侧已有按会话路由的封装 `Service.appendEngineMessage`
   （`application/core/service_input.go:36` → `AppendHistoryFor(sessionID, …)`），不做视图假设。
3. **`reasoning_content` 已在底层回传**（见 I-20），A 链路不新增要求。

待观察项（不改，但要盯）：`Session.AppendHistory` 取的是 `e.mu`，而这把锁被 `ChatStream`
全程持有（见 `application/core/README.md` 关于别名引擎的段落）。因此**运行中调用会阻塞到本次
LLM 往返返回**——补记注入只能走回合边界 drain，绝不能从工具回调里直接调
（同一形状的坑已在 `9643ecd` 修过一次：迭代边界不得重入会话锁）。

顺带结论：**同回合内工具并发**才需要改 Seele（要把 `loop.go:327` 的串行 for 改成并发执行＋
按调用序收结果）。A 链路本身不吃这条，故本工作包**不包含**它。

## 3. 归属决策（AGENTS.md「新功能归属」三问）

1. 崭新功能还是改进既有？→ 新协议（工具完成语义），但**不新开顶层包**：
   deferred 执行体归 `seelebridge`（它已持有工具执行与 worker 语义），
   补记注入与排队归 `application/core`（它已持有 per-session 队列与回合边界）。
2. 放进哪个已有模块？
   - 工具入参与执行：`seelebridge/tools/router.go`（bash 类 input 结构在 :416）；
   - 未决登记表：新文件 `seelebridge/asyncexec/`（若需要独立生命周期则配 README）；
   - 补记入队与 drain：`application/core/service_queue.go` + `chat.go` 回合边界；
   - 四区判定：`application/core/context_runtime/layout.go`；
   - 共享 DTO：`application/contract/dto`（补记信封，DTO 纯化、方法转自由函数）。
3. 是否新开包？→ 只有当「未决登记 + worker 监督 + 终态合成」需要独立生命周期时新开
   `seelebridge/asyncexec/`；否则并入 `seelebridge/tools`。判据：单文件 >500 行或职责混合
   即拆。

## 4. 阶段台账

### P0 · 走通最小垂直切片（bash 一条工具，端到端）

- [ ] P0.1 bash 入参加 `background bool`（`seelebridge/tools/router.go`），默认 false 不改行为。
- [ ] P0.2 `background=true` 时：起执行体、立即返回确定性 ack（含 `call_id` 与输出落点），
      原命令的 tool_result 通道改为「完成时写补记信封」。
- [ ] P0.3 ack 文本模板进常量表，禁止含时间戳/随机量/绝对路径中的用户目录。
- [ ] P0.4 完成补记以 **user/internal** 信封入该会话队列（复用 per-session inputQueue，
      打 internal 标记，前端按内部来源渲染，不当作用户输入）。
- [ ] P0.5 回合边界 drain：`chat.go` 收尾段把补记经 `appendEngineMessage` 注入，
      **不得**从工具 goroutine 直接调 engine。
- [ ] P0.6 钉测试：ack 与 tool_use 同单元；补记落新单元；`call_id` 幂等（重投不双写）。
- [ ] P0.7 真链路冒烟：同会话连续 ≥10 轮、含 ≥3 个 deferred，**缓存命中不低于基线**
      （探针只证明了手搓 wire，未证明 Seelex 装配 + R2 读取器 + 选窗后仍成立）。

### P1 · 未决状态入持久化与恢复

- [ ] P1.1 未决 `call_id` 登记表随 record 落盘（进程重启后能认出「还在等谁」）。
- [ ] P1.2 装配期：任何未回执的 tool_use 合成一条 `interrupted` 终态回执（provider 会拒带
      未回答 tool_use 的历史）——落点在既有「冷加载装配草稿尾恢复」路径（`ff2f540`）。
- [ ] P1.3 执行体丢失（worker panic/被 kill/超时）→ 监督者合成终态回执，仍走 I-18 尾部单元。
- [ ] P1.4 钉测试：崩溃注入后会话不得永久停在未定稿态。

### P2 · 压缩与门禁对齐（I-19）

- [ ] P2.1 `ContextLayout` 四区判定加「含未决 `call_id` ⇒ 非完整单元」谓词。
- [ ] P2.2 折叠/压缩选窗、`RetainDecision` floor 兜底同用该谓词（判据与报表同一份口径）。
- [ ] P2.3 真空区/取尾窗选窗回报同步，避免报表与判据分叉。
- [ ] P2.4 钉测试：未决单元既不被折出、也不进压缩候选；补记与 ack 在压缩报表里同区间可见。

### P3 · 可见面

- [ ] P3.1 轨迹视图：ack 行与补记行可区分（pending / completed / interrupted 三态）。
- [ ] P3.2 `tool_wait(call_id, budget)` 工具：模型明确需要结果时同步等待（没有它异步纯亏）。
- [ ] P3.3 工作台投影补记状态（**只投影、不做事实源**，防口径分叉）。

### P4 · 扩面（本包外，需另立）

- [ ] P4.1 computer-use / MCP 慢工具接入 deferred（与 tool-execution worker 进程化同批评估）。
- [ ] P4.2 同回合内工具并发（需改 Seele `loop.go:327`）。

## 5. 决策（2026-09-24 所有者裁定）

- **D1 空闲时的行为 = 按 deferred 类型分流** —— **同日作废**：改走轮询型（§8）后既不需要自动起一轮、
  也不需要"结果重要性"标签；推送/注入路线整条不再实现。
- **D2 P0 白名单 = 只 bash**：P0 不引入 worker 监督者，执行体是进程内 goroutine + 输出文件；
  终态合成（超时/丢失）留到 P1。
- **D3 重复派发 = 拦**：同会话同命令在途期间不重跑，回执 `repeated=true`。跑完之后允许重派
  （一次失败不得永久封死该命令），故去重键只在 `state=running` 时生效。

## 6. 已落地（第一提交，**已于同日回滚**；下列勾选只作历史，不是当前状态）

- [x] P0.1 `seelebridge/tools/router.go`：`scopedBashInput` 新增 `background`，
      `bashSchema()` 同步下发，bash 工具描述写清「结果不在本次回执里」。
- [x] P0.2/P0.3 新文件 `seelebridge/tools/deferred.go`：`deferredRegistry`（在途登记、
      去重、`deferredMaxRuns=32` 只数在途、`deferredMaxRecords=256` 封顶已完成记录）、
      确定性受理回执 `deferredAck`（无时间戳/耗时字段，因为它永久占前缀字节）。
- [x] P0.2 `Router.scopedBashDeferred`：`context.WithoutCancel(ctx)` 摘掉取消信号但保留
      NodeScope/会话键（否则回执一返回、命令就被连带杀掉），独立上限 `deferredHardCap`，
      stdout+stderr 交错写同一文件（顺序本身是信息），收尾追加
      `[seelex:async-exit] id=… exit=…` 机器可读行。
- [x] P0.6 测试 `seelebridge/tools/deferred_test.go`：去重只在在途生效、跨会话隔离、
      在途上限不数已完成、回执两次渲染字节相同且不含结果正文、真实 echo 端到端。
- [x] 探针更名 `async_tool_wire_live_probe_test.go`（tag `asynclive`）。

实现中的一处口径简化（值得记住，别回退）：**补记不需要 provider 的 `tool_call_id`**。
配对由受理回执本身满足，补记只是普通消息，因此 Seelex 自己的 `async_id` 就够了，
工具层完全不必穿透框架的 call id——省掉一条跨层依赖。

尚未落地（P0 的另一半，下一步）：

- [ ] P0.4 完成补记入队：以 internal 信封进该会话 inputQueue（前端不当作用户输入）。
- [ ] P0.5 回合边界 drain：`chat.go` 收尾段经 `appendEngineMessage` 注入；
      **禁止**从工具 goroutine 直接调 engine（`Session.AppendHistory` 持的锁被 `ChatStream`
      全程持有，见 §2）。
- [ ] P0.4b D1 分流标签：工具侧 `wake=on_complete|merge_next`，取代单一 bool。
- [ ] P0.7 真链路冒烟：≥10 轮含 ≥3 个 deferred，缓存命中不低于基线
      （探针只证明手搓 wire，未证明 Seelex 装配 + R2 读取器 + 选窗后仍成立）。

当前可观测行为（提交后即可用，非半成品）：模型可发 `background=true`，立即拿到
`async_id` 与 `log_path`，之后用 `read_file` 自行取回结果；**不会**自动补记，
也不会自动起一轮——那是 P0.4/P0.5 的内容。

## 8. 轮询型试探切片（默认关 · A/B 判据先写死 · 一键回滚）

### 8.1 开关（kill-switch）

`seele.yaml` → `limits.async_exec.enabled`，**默认 false**。关闭时：`bash` schema 不出现
`background`、handler 收到 `background=true` 直接**报错**（不得静默当成同步执行——与
`sandbox.go` 头注释"能力不可实施时必须拒绝，不得悄悄降级"同源）。

### 8.2 切片范围（就这些，多了不算本切片）

- `seelebridge/tools`：句柄表（内存态、在途上限、独立硬超时）+ 输出文件（字节上限）；
- `bash` 加 `background`；新增 `async_output(handle, wait_ms)`（默认 5s、上限 60s，
  到点未结束返回 `running` + 增量尾部）；
- 同一 handle 的取回继承首次派发授权，不重复弹审批；
- **不动** `application/core`、四区判定、压缩、事件协议、快照镜像。

### 8.3 A/B 判据（跑之前定死，跑之后不得改阈值）

A = 基线（同步阻塞）；B = 同一基线 + 本切片 + 开关打开。

| # | 指标 | 门槛（B 相对 A） | 抓的是什么 |
|---|---|---|---|
| 1 | 任务总墙钟 | ≤ A | 异步的全部价值；不降就说明白做 |
| 2 | 逐轮 `cached/prompt` 比率 | ≥ A − 2pp | 轮询是否破前缀（预期不破：全是正常 tool 对） |
| 3 | 任务总 prompt tokens | ≤ A × 1.10 | 信封重复的容忍度 |
| 4 | 结果真进上下文（后续回合引用了 `async_output` 正文） | 每任务 ≥ 1 次 | 轮询型最坏失效＝模型根本不取 |
| 5 | 进程表长度归零、输出目录有界 | 硬 | 孤儿进程与泄漏 |

样本任务（固定、可复现，各跑 3 次取中位）：① 构建 + 测试并行，中途继续读代码；
② 跑一次真实冒烟的同时改文件；③ 长命令跑到一半主动终止。

### 8.4 基线与回滚

基线用 `git worktree` 从干净提交点拉副本——**不能**用当前工作树，因为另一会话有在飞的
前端/皮肤改动未提交，混进 A/B 会污染对比。本切片要求做成**一个 commit**：回滚 = 单条
`git revert`，或直接把开关置 false（schema 与 handler 同时回到原行为）。

### 8.5 报告

A/B 原始数据与逐轮并排表落盘 `docs/2026-09-24-async-tool-deferred-ack/ab-<date>.md`
（差异行标注 + 量化并排）；聊天里只回三行：结论行（成立/不成立）、关键数字行、下一步行。
指标 1 或 5 不达标即判"不成立"，直接回滚，不再调阈值。

## 8.6 已落地（实现台账，2026-09-24 第二提交）

一个 commit 的形态（回滚 = `git revert`，或把 §8.1 的开关置 false）。

- [x] §8.1 开关：`seelexctx/limits.go` 新增 `AsyncExecLimits{Enabled}`（零值=关，不进
      `DefaultLimits`/`WithDefaults`）；`config/seelex.yaml` 落 `limits.async_exec.enabled: false`
      并写清"关就是关"；`seelebridge/runtime_tools.go` 注入 `Deps.AsyncExecEnabled`。
- [x] §8.2 `seelebridge/tools/async_exec.go`：`asyncRegistry`（句柄 `a<seq>`、去重键
      = 会话+命令 sha 前 8 字节、`asyncMaxRunning=32` 只数在途、`asyncMaxRecords=256`
      封顶已完成且**连带删输出文件**）、`startAsync`/`awaitAsync`（`context.WithoutCancel` +
      独立 `asyncHardCap=30m`，超时合成 `exit=124` 并把注记写进输出文件）、
      `cappedLogWriter`（`asyncLogCap=1MiB` 处截断，超限**仍报已消费**——报短写会让命令
      自己异常退出，那是把基础设施问题伪装成命令问题）、终态恰好合成一次（含执行体 panic）。
- [x] `bash` 加 `background`：关闭时 schema 不下发、handler 收到 `background=true` 直接报错。
- [x] 新增 `async_output(handle, wait_ms)`：默认 5s / 上限 60s / 负数立刻返回；每次只交付
      **新增**字节（`cursor` 是已交付偏移，`asyncPollTailBudget=4000` 字符/次）；未知句柄报错
      （不谎报空成功——那会让模型以为命令没输出）；句柄只在本会话有效。
- [x] 授权：`async_output` 归 `GroupRO`（`seelebridge/tools/permission_policy.go`），
      取本会话已获批那次派发的输出不再二次弹审批（§8.2 第四条）。
- [x] 钉测试：`seelebridge/tools/async_exec_test.go`（12 条：回执不含命令输出、增量拼接
      恰好一次不重播、跨会话取回被拒、未知句柄报错、去重只在在途、在途上限、驱逐连带删
      日志、增量游标、截断不报短写、wait_ms 归一、开关三处同步、权限组）、
      `seelexctx/limits_test.go:TestLimitsAsyncExecDefaultsOff`。
- [x] 验证：`go build ./...`、`go vet ./...`、
      `go test ./application/core ./seelebridge/... ./seelexctx/... -count=1` 全绿。
- [x] §8.3 A/B 实测：`async_exec_ab_live_test.go`（tag `manualsmoke`；两臂只差
      `limits.async_exec.enabled`，3 任务 × 每臂 3 次 × A/B 交替，真 API + 录制代理）。
      结果见 [`ab-2026-09-24.md`](ab-2026-09-24.md)：硬判据 1（墙钟合计 68.43s→49.53s）与
      5（进程表 0 孤儿、目录收尾回收）**成立**，软判据 2（0.948→0.956）与 4（结果确实
      进入后续请求）**成立**，软判据 3（总 prompt）**不成立，+158%**。
- [x] 竞态闭合：`begin` 与 `snapshot` 同口径**按值返回副本**，派发侧不再锁外读表内可变
      字段。旧形状的反证留在 `async_exec_raceproof_test.go`（tag `raceproof`，opt-in 报红），
      回归钉在 `TestAsyncRegistryBeginHandsOutACopy`。
- [x] 目录泄漏闭合：`Router.CloseAsync` + `asyncRegistry.close` 登记进 `Runtime.Shutdown`
      逆序链（`seelebridge/runtime_tools.go`）；测试脚手架（`asyncTestRouter` /
      `newAsyncRegistryForTest`）同口径自收。修复前本机残留 44 个目录，隔离复测一次 B 臂
      回合前后计数不变。
- [x] 文档同步：`seelebridge/tools/README.md` 补「后台命令（轮询型）」小节与四条 Review
      边界；本文件 §0 补 P7 行与一条 A/B 冷启动口径。
- [x] 时间线记录：[`docs/devlog/2026-09-24-async-polling-ab-closeout.md`](../devlog/2026-09-24-async-polling-ab-closeout.md)
      ——竞态怎么从"潜在"升级成"实测存在"、目录泄漏的 44→49→回收过程、判据 3 为什么不放宽。

轮询型没有碰 I-17/I-18/I-19 的原因（值得先看再动 layout）：每个 `tool_call` 的
`tool_result` 就是受理回执，**配对在同单元内完成**，历史上不存在"未回答的 tool_use"，
因此四区判定与压缩选窗不需要加谓词。

未落地（本切片之外）：`tool_wait` 形态的同步等待（§P3.2）；同回合内工具并发；
判据 3 的成本压缩（取回等到终态 / `wait_ms` 贴近命令时长，可少 2 次信封重发）；
**硬超时只杀 bash、不杀其子进程**这一条未被样本覆盖（`ab-2026-09-24.md` §6.2 末），
要主张"硬超时也不留孤儿"需另设样本。

## 9. 验证命令

```text
go build ./...
go vet ./...
go test ./application/core ./seelebridge/... ./seelexctx/... -count=1 -timeout=300s
go test -tags asynclive . -run TestAsyncWire -count=1 -v   # 需 SEELEX_SMOKE_ACCOUNTS
go test -tags manualsmoke . -run TestManualSmokeAsyncAB -count=1 -v -timeout=60m
                                                          # §8.3 五指标 A/B（真 API，付费）
go test -tags raceproof ./seelebridge/tools/ -run TestRaceProof -count=20 -race
                                                          # 旧形状反证：**预期报 DATA RACE 并非零退出**，
                                                          # 它红了才说明"修之前确有竞态"这件事仍成立；不要当坏测试修掉
node --test gui/frontend/dist/*.test.mjs                   # 若动了可见面
```
