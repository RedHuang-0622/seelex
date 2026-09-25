# 异步工具「受理回执 + 尾部补记」（A 链路）· 打点台账

> 状态：**A 链路（推送/补记）2026-09-24 已回滚，未实现**；**轮询型切片 2026-09-24 落地时默认关，
> 2026-09-25 起出厂常驻开**
> （台账见 §8.6 与 §10；开关 = `limits.async_exec.enabled: true`）。§8.3 五指标 A/B 同日实测完成，
> 结论见 [`ab-2026-09-24.md`](ab-2026-09-24.md)：**两条硬判据（墙钟、进程表/目录）成立，
> 成本判据（总 prompt ≤ A×1.10）不成立（T1 +127% / T2 +133%，不必等的 T3 只 +6.7%）**，
> 当时的结论是"它不是通用开关、别当默认打开"。本轮（§10）换了一种承担方式：**能力常驻工具面，
> 要不要后台由模型逐次决定**，同时修掉会把轮询掐死的"无进展预算"打架并补上终止口。所有者裁定回到
> "工具串行阻塞"这条只对 §8 落地时的默认态成立，本文 §0 的实测事实与 §1 不变量仍然有效（它们是证据，不是实现），
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
- **I-21 后台执行不进持久化台账**：一次后台命令的事实源只有 `seelebridge/tools` 的执行登记表
  （内存）；core 侧的工作表格行与请求尾部打点行都是**只读投影**，随派发出现、随终态被驱逐
  消失，**永不写进 task 注册表**（注册表 `record.Tasks` 会随会话落盘并在恢复时回灌，那样重启
  后会留下一条永远 running 的假行）。钉住用例：`TestAsyncRowDisappearsWhenRegistryDropsIt`。
- **I-22 单次工具调用钉住会话不得超过 `asyncMaxWaitMS=60s`**：`ChatStream` 在工具执行期间持有
  会话锁，用户输入与审批都要等它；`async_output` 的 `wait_ms` 上限即这条钉住时长的上限，
  不得为"少几轮往返"而放宽（`until_done` 同此理由被否）。

> **编号漂移修正（本轮读码发现）**：本节标题原写"编号接存储架构 §4"，但
> `docs/2026-09-08-session-storage-architecture/my_design.md` §4 的不变量用的是
> **`I1…I19`（无连字符）**，与本包的 `I-16…I-22` 不是同一条序号链（例如存储侧 I16 与
> 本侧 I-16 含义无关）。两套命名空间各管各的，引用时须带文档名，别当成同一编号系统。

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
      结果见 [`ab-2026-09-24.md`](ab-2026-09-24.md)：硬判据 1（墙钟合计 69.92s→49.13s，
      省 29.7%，其中 T3 23.13→3.95s）与 5（进程表 0 孤儿、整轮矩阵期间临时目录 59→59）
      **成立**，软判据 2（0.949→0.955）与 4（结果确实进入后续请求）**成立**，软判据 3
      （总 prompt）**不成立：T1 +127% / T2 +133% / T3 +6.7%**。第一轮因采集器"取回次数"
      口径写错（把每轮重放的历史也数进去）而作废，修好后 18 个回合整轮重跑，两轮判定一致。
- [x] 竞态闭合：`begin` 与 `snapshot` 同口径**按值返回副本**，派发侧不再锁外读表内可变
      字段。旧形状的反证留在 `async_exec_raceproof_test.go`（tag `raceproof`，opt-in 报红），
      回归钉在 `TestAsyncRegistryBeginHandsOutACopy`。
- [x] 目录泄漏闭合：`Router.CloseAsync` 登记进 `Runtime.Shutdown` 逆序链
      （`seelebridge/runtime_tools.go`）；close 当场试删，删不动（执行体还握着日志句柄）
      由最后一条 `finish` 补删，关停后 `begin` 直接报错不新建无人回收的目录。
      测试脚手架（`asyncTestRouter` / `newAsyncRegistryForTest`）同口径自收，派发型用例
      等执行体收尾才退出。修复前本机残留 44 个目录 + 每轮单测再漏 5 个；现在隔离复测一次
      B 臂回合 49 → 49、连续两轮单测 59 → 59 → 59。
- [x] 文档同步：`seelebridge/tools/README.md` 补「后台命令（轮询型）」小节与四条 Review
      边界；本文件 §0 补 P7 行与一条 A/B 冷启动口径。
- [x] 时间线记录：[`docs/devlog/2026-09-24-async-polling-ab-closeout.md`](../devlog/2026-09-24-async-polling-ab-closeout.md)
      ——竞态怎么从"潜在"升级成"实测存在"、目录泄漏的 44→49→回收过程、判据 3 为什么不放宽。

轮询型没有碰 I-17/I-18/I-19 的原因（值得先看再动 layout）：每个 `tool_call` 的
`tool_result` 就是受理回执，**配对在同单元内完成**，历史上不存在"未回答的 tool_use"，
因此四区判定与压缩选窗不需要加谓词。

未落地（本切片之外）：`tool_wait` 形态的同步等待（§P3.2）；同回合内工具并发；
判据 3 的成本压缩（取回等到终态 / `wait_ms` 贴近命令时长，实测 4 次取回可并成 1 次，
省 3 次信封重发）；
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
go test -tags asyncproof ./application/core/task_context/ -run TestAsyncProof -count=1 -v
                                                          # §10 S2 反证：**预期报红**（轮询被无进展预算掐死），
                                                          # 与 raceproof 同族：opt-in、永不进 CI、不许当坏测试修掉
```

## 10. 本轮台账（2026-09-25 · 常驻开 + 终止口 + 打点表跟随 worktable）

基线 tag：**`pre-async-poll-2` @ 38213d5**。先留一条事实：§8.4 承诺的 `pre-async-poll` tag
**实际不存在**（`git tag -l` 只有 v0.0.x 四个）——上一轮"动手前打 tag"没落地，本轮补上并以此为准。

### 10.0 所有者裁定（2026-09-25）

| # | 裁定 | 落点 |
|---|---|---|
| R1 | 能力**常驻开**，"这次要不要后台"由模型逐次传 `background` 决定 | S4（yaml 一行） |
| R2 | 后台行**复用 `kind=task` + `source_id=async:<handle>`** | S3（投影口径） |
| R3 | 终止口 = **`async_kill(handle)` + 会话销毁即杀**，不做 GUI 行内按钮 | S1 |
| R4 | **不做句柄持久化**（§P1 继续搁置），本轮先修打架 + 三件事 | 范围 |

**我对 R2 的修正（先认）**：给选项时我写的是"进 task 注册表则表格/打点块/事件零改动生效"。核实后
不成立——`record.Tasks` 随会话落盘（`application/core/session_history.go:399`），而句柄表纯内存，
重启后台账里会留下一条永远 `running` 的假行。因此**行身份口径照 R2 保留，但由 registry 只读投影
合成、不写进 task 注册表**（registry 是唯一事实源，行随状态出现、随终态/驱逐消失，天然不落盘）。
代价：`buildWorkTable` 与打点块各多一路输入 + 一个变更信号。同形状先例见仓库根 `MEMORY.md`
会话存储约定："subagent 只在工作表以 running/interrupted/done/failed 呈现"。

### 10.1 动手前就成立的两条缺陷

- **D1 轮询会被"无进展预算"掐死**。指纹 = `name + "\x00" + result`
  （`application/core/task_context/task_execution.go:151-168`），重复即**不推进** `ProgressEpoch`；
  而异步载荷刻意不含时间戳（`seelebridge/tools/async_exec.go:262-266`）。于是安静的长命令第 2 次起
  取回结果逐字节相同 → `noProgressRounds` 累加（`coordinator.go:552-563`）→ lite 第 6 / medium 第 10 次
  以 `no observable progress` 终止本轮，而命令其实还在跑。§8.3 的 18 回合没撞上，只因样本
  `sleep 20; echo SENT-*` 一直在吐增量。**状态：静态读码确定，复现见 S2。**
- **D2 子进程属性互相覆盖**。`scopedBash`（`router.go:496-498`）与 `startAsync`
  （`async_exec.go:452-455`）都先 `winhide.Apply`（`internal/winhide/winhide_windows.go:18` 设
  `HideWindow + CREATE_NO_WINDOW`）再 `security.ConfigureHiddenCommand`
  （`seelebridge/security/command_windows.go:13` **整体重写** `SysProcAttr{HideWindow:true}`）
  → `CREATE_NO_WINDOW` 被覆盖掉。不收口，`async_kill` 要加的进程组标志就成了第三个互相覆盖的写者。
  **状态：静态读码确定，未实地观察窗口闪烁；复现见 S0。**

### 10.2 判据（跑之前写死，跑之后不得改阈值）

| # | 命题 | 判定 | 口径 |
|---|---|---|---|
| J1 | 安静命令两次取回**逐字节相同** | 必须成立 | tools 用例断言两次 result 相等（D1 前提事实） |
| J2 | 同结果连续取回 6 次 + 有在途句柄 → 修复前本轮被终止 | 必须成立 | `asyncproof` 反证报红（opt-in，不进 CI） |
| J3 | 修复后同形状**不被终止**，封顶原因变成 tool-call 上限 | 必须成立 | 断言 reason 含 `tool-call limit` |
| J4 | `async_kill` 后子进程计数不增 | 必须成立 | 派 `sleep 30 & sleep 30`，杀后计数快照相等 |
| J5 | 硬超时同口径杀进程树（闭合 §8.6 末未证项） | 必须成立 | 同 J4，走上限而非手动杀 |
| J6 | 打点块含 `- async:<h> running`；终态后同轮重建即消失 | 必须成立 | work_table 用例断言块文 |
| J7 | 打点块**不含**命令原文、绝对路径、时间戳 | 必须成立 | 断言不含 home 片段与命令子串 |
| J8 | 驱逐已完成记录后工作表格对应行消失 | 必须成立 | registry 唯一事实源的直接后果 |
| J9 | 出厂配置 `async_exec.enabled=true`；结构零值仍 false | 必须成立 | 两条测试分开钉 |
| J10 | 常驻 schema 的固定代价 | 记录不判 | 每轮 **+213 token**（§4 逐轮表 r1 4476 vs 4263） |
| J11 | 打点块变化不得让前缀命中率回落 | 硬 | **成立**（§10.7 判据 6 实测：76 个可比请求 tail-growth 0 破坏；兑现率 A 0.981 / B 1.000）。块在请求尾部、不落历史（`context_runtime/coordinator.go:443-452`）+ 探针直接算相邻请求的 messages 区前缀 |

### 10.3 逐片勾选（2026-09-25 本轮实况）

**S0 子进程属性收口 — 完成**
- [x] S0.1 `security.ConfigureHiddenCommand` 改**合并**语义（`command_windows.go`）。
- [x] S0.2 进程树原语：`security/process_tree_windows.go`（Job Object）+
      `process_tree_other.go`（Setpgid / kill(-pgid)）。
- [x] S0.3 **先复现后修**：修前用例报红 `CREATE_NO_WINDOW lost: CreationFlags=0x0`
      （`_logs/s0_reproduce.log`），修后红→绿（`_logs/s0_green.log`）。
      顺带证伪了自己的一处投机抽象（曾用 `type cmdHandle = os.Process` 只为"能塞假句柄"，
      改成表里存可注入的 `processTree` 接口值）。

**S1 终止口 — 完成（含一处计划外修正）**
- [x] S1.1 `asyncRun.tree processTree`（只在 `g.mu` 内读写；终止动作在锁外做）。
- [x] S1.2 `state=killed` + `exit=137` + kill 注记进输出文件；`markKilled` 只在终止确认
      成功后落，杀不掉就报错不谎报（用例 `TestAsyncKillDoesNotLieWhenKillerFails`）。
- [x] S1.3 硬超时改走同一棵树（`cmd.Cancel = tree.Terminate`），**闭合了 §8.6 末那条
      "只杀 bash 不杀其子进程"的未证项**：`TestAsyncKillTerminatesProcessTree` 以
      "0.5 秒后才由后台子 shell 写的 GRANDCHILD 必须不出现"为行为判据。
- [x] S1.4 `async_kill(handle)` 注册 + 归 **GroupRW**（与 `bash` 同组，理由见 S1.4 原文）。
- [x] S1.5 会话销毁即杀：`Router.CloseSessionAsync` → `Runtime.ReleaseSessionAsync`
      → **显式**加进 `contract.RuntimePort`（不用可选断言；因此 adapters/e2e/gui/core 四个
      实现同步补齐，编译期就会拦漏实现）；调用点 `workspace_usecase.go`、`archive_session.go`。
- [x] S1.6 拆文件（命中上帝文件判据）：`async_exec.go`（表/状态）+ `async_run.go`（执行体）
      + `async_tools.go`（工具面）。
- ⚠ **计划外修正（推翻我方案里的写法）**：Windows 侧不能用 `taskkill /T`。实测
  `bash -c "(sleep 0.5; echo GRANDCHILD) & sleep 25"` 在 150ms 被 `/T` 杀掉后，GRANDCHILD
  仍然落进日志——MSYS2/Git Bash 的 fork 子 shell 不一定挂在直接父进程下，`/T` 靠父 PID
  枚举就漏。改成 **Job Object + KILL_ON_JOB_CLOSE**；建不出 Job 时退化为按 PID 杀并由
  `Degraded()` 说清（退化时不得主张"整棵进程树已终止"）。

**S2 修无进展打架 — 完成（判据本身已有红→绿对照，见 S2.5）**
- [x] S2.1 前提事实用例：`TestQuietPollResultsAreByteIdentical` 断言同一安静句柄两次取回
      **原始字节相等**（按结构体相等不算），并断言 `AsyncPendingFor` 只数本会话、只数在跑。
- [x] S2.2 `asyncRegistry.countRunningFor` → `Router.AsyncPendingFor` →
      `Runtime.AsyncPendingFor` → `contract.RuntimePort` → `task_context.Deps.AsyncPending`。
- [x] S2.3 `coordinator._AllowNextReActIteration`：epoch 未变但 `pending>0` ⇒ 不累加
      `noProgressRounds`；不碰载荷、不调阈值、不新增"轮询次数"阈值。
- [x] S2.4 封顶轴仍是 `MaxToolCalls/MaxToolRounds`（写进代码注释与本台账）。
- [x] S2.5 **红→绿对照补齐**（原 10.6 未验证链接 2）：
      `application/core/task_context/coordinator_async_budget_test.go` 把两臂放在同一条用例里
      ——同一 epoch 固定不动、只改 `AsyncPending` 的回答值：`pending=0` 必须被
      `no observable progress` 终止（= 修前形状），`pending>0` 十轮都不被终止；另一条钉住
      **未注入读面时判据原样退回**（不得把"读不到"当成"有在途"）。因判据可在包内直接驱动，
      原计划的 opt-in `asyncproof` 反证 tag **不再需要**，这组对照留在常规 CI 里。

**S3 打点表跟随 worktable + 实时探针 — 完成**
- [x] S3.1 新 DTO `application/contract/dto/async_run.go`（`AsyncRunRecord`）+
      `contract.RuntimePort` 两条口：`AsyncRunsSnapshot()` / `AsyncRunEvents()`（**显式加方法**，
      adapters/e2e/gui/core 四个实现同步补齐，漏实现编译期就拦）。
- [x] S3.2 变更信号：登记表 `changed chan struct{}`（容量 1、非阻塞、latest-wins），
      由 `begin`/`finish`/`noteOutput` 三处发。原方案里的 `onChange func()` 字段从未接上，
      本轮直接换成 channel（回调式接口的"不得回闯本表"约束要靠人记，channel 天然隔离）。
- [x] S3.3 只读投影行：`application/core/work_table_async.go`，行身份按裁定 2
      （`ID=SourceID=async:<handle>`、`kind=task`），**不进 task 注册表**。三处建表入口
      （实时重投影 / 会话快照 / 冷读面）统一走 `asyncRunsForTable()`，避免"切到哪个会话
      才看到哪些行"。
- [x] S3.4 尾部打点块并入：`workTableTraceBlockFor` 改为"任务行 + 在跑后台行"共用一份有界
      body（30 行上限不变），块内只有 句柄 / state / 已产出字节 / 描述或命令短截断。
      终态行不进块；只有后台命令在跑时块也会出现（附一行 `async:<句柄>` 的读法说明）。
- [x] S3.5 第 4 个生命周期消费者 `consumeAsyncRuns()` → 复用
      `refreshWorkTableFromSources()`（同一发布路径、同一 revision，表格与打点块不会分叉）。
- [x] S3.6 **owner 追加要求（2026-09-25）**：打点表要有 任务描述 / 运行的指令 / 后台情况实时
      查看，"这里需要做个探针"。落法：
      `bash background=true` 新增**必填** `description`（行标题，handler 拒绝缺它，
      不静默给占位符）；命令行原文进描述列；探针 `AsyncRuns()` 采样
      state/exit/已产出字节/末行/耗时/是否降级，`noteOutput` 按 1s 去抖驱动重投影。
      **前端零改动**：`work-table.js` 已有 描述列 / 附件列（日志路径）/ 可展开打点表
      （时间·操作·状态·证据·耗时），四个字段各自的归宿本来就存在。
- ⚠ **J7 判据被这条要求改写（记录差异，不改判据编号）**：原 J7 写"打点块不含命令原文、
      绝对路径、时间戳"。owner 要求"看得见在跑什么"，故命令原文**进 GUI 描述列**；
      打点块（进上下文）仍然不含绝对路径与时间戳，且用例
      `TestAsyncTraceLinesCarryNoPathsOrLogContent` 同时断言"含描述与字节数"和
      "不含 `log_path` / 不含末行原文"。分层结论：**探针到 GUI 可以带路径与时间，
      载荷与打点块不行**。
- [x] S3.7 后台行上限 `asyncWorkMaxRows=32`（在途优先、终态按最新补齐）：登记表总槽 256，
      全投影会把真实任务挤出 `WorkTableRows`。用例断言 52 条记录只出 32 行且在途行不被挤掉。
- 证据：`seelebridge/tools/async_probe_test.go`（7 条：读数/末行清洗限长/派发序稳定/降级如实报/
      去抖/描述与批次落表/关闭时为空）、`seelebridge/runtime_async_test.go`（逐列搬运 + nil 安全）、
      `application/core/work_table_async_test.go`（7 条：列位/终态映射/打点块口径/块出现与消失/
      驱逐即消失/信号驱动重投影/上限）、`_logs/async_s3_probe_test.log`、
      `_logs/async_s3_core_test.log`。
- 未做（诚实记账）：GUI 实机走查与真 API A/B 复跑（含"安静长命令"第 4 任务）仍未跑，见 10.6 末。

**S4 常驻开 — 完成**
- [x] S4.1 `config/seelex.yaml`：`enabled: false → true`（只动这段；同文件的 `context_*`
      注释块是另一会话在飞改动，未碰）。
- [x] S4.2 结构零值仍 false（`seelexctx/async_exec_config_test.go` 两条分开钉：
      `TestAsyncExecShippedConfigOn` / `TestAsyncExecZeroValueStaysOff`，即 J9）。
- [x] S4.3 既有 `TestLimitsAsyncExecDefaultsOff` 保留（它钉的正是"零值=关"，语义未变）。
- [x] S4.4 dist 配置副本链未受影响（本轮没有触碰 Makefile / 构建脚本）。

**S5 文档与不变量**
- [x] S5.1 `seelebridge/tools/README.md`：后台命令小节重写（三个工具面、文件三分、
      Job Object 与其退化口径、会话销毁即杀、`AsyncPendingFor` 与无进展判据的耦合），
      并新增「Review 要点」四条（锁内/锁外边界、killed 只在确认成功后落、载荷字节纪律、
      `attach` 未落上时自己 `tree.Close()`）。
- [x] S5.1b（本轮补）同一节再补第四份文件 `async_probe.go`、`background=true` 必填
      `description`、探针三类信号源与 1s 去抖，以及"字段分三层：表 / 探针(到 GUI) / 载荷(进上下文)"
      这条新 Review 判据；能力摘要行由"默认关"改口为"出厂 true、结构零值仍 false"。
- [x] S5.2 `application/core/README-work-table.md`：新增「后台命令行（第四路输入，只读投影）」
      一节（行身份、三处建表入口统一、上限、打点块字段收窄、第 4 个消费者、叶子锁纪律），
      并用 `python scripts/gen_core_readme_index.py` 刷新分卷索引。
      **注意**：脚本会重写全部五个分卷且以 CRLF 落盘——本轮只保留 async 相关的四个卷，
      `README-context.md` 的 +69 行是**另一会话在飞的 `context_compact_gate.go`**，已 checkout 还原；
      四个保留卷的行尾已改回 LF。索引刷新必须等那颗雷拆掉后在干净树上再做一次。
- [x] S5.3 新不变量 I-21 / I-22 已登记在本包 §1（并记下一处**编号漂移**：存储架构 §4 用的是
      `I1…I19` 无连字符，与本包 `I-16…I-22` 不是同一条序号链，本节标题"编号接存储架构 §4"
      原为误述，已就地说明）。
- [x] S5.4 `docs/devlog/2026-09-25-async-poll-permanent-on.md`。
- [x] S5.5 文档漂移：`AGENTS.md` 写 Seele v0.1.1 → 改为 `go.mod` 实解析的 **v0.3.0**。

### 10.7 第三轮真 API A/B：判据先写死（跑之前记，结果不回填阈值）

跑法（`manualsmoke` tag，opt-in，不进 CI）：

```text
$env:SEELEX_SMOKE_ACCOUNTS='<config/accounts.yaml>'
go test -tags manualsmoke . -run TestManualSmokeAsyncAB -count=1 -timeout=60m
# 任务 T1,T2,T3,T4 × 3 次重复 × 两臂（A=开关关 / B=开关开），唯一变量 limits.async_exec.enabled
```

**为什么每一步都写进提示词**：要测的是"轮询形状的成本/收益"，不是模型的手气。任务指令
把要调哪个工具、调几次、`wait_ms` 多少都写死（T4 明写"恰好 6 次"），两臂、每次重复走同
一条脚本；否则回合形状由模型随机决定，token 与缓存数字根本不可比。

**新增任务 T4（安静长命令）**：命令全程不输出一行，只在结束时 echo 标记；B 臂被要求轮询 6
次 ⇒ 6 次取回的载荷逐字节相同。这是 §10.1 D1 的形状，也是 S2 那条判据的真实 API 实测位。

**前缀不靠 provider 自证**：中间件代理留住每条请求的真实字节体，取 `messages` 数组的元素区
逐对相邻请求算最长公共字节前缀。`lcp == 上一条元素区长度` ⇒ 历史纯尾部生长（结构判据，与
provider 口径无关）；可命中上限 = 上一条的 prompt tokens，与它报的 `prompt_cache_hit_tokens`
并排成**兑现率**。字节复用率（Σlcp/Σbody）与兑现率同表列出——一个我们算、一个它报，
背离即问题。兑现率可能略 >1（DeepSeek 按 512-token 块对齐缓存块），所以只在两臂之间比，
不当绝对值读。

**判据（写死）**：

| 编号 | 命题 | 强度 |
| --- | --- | --- |
| 1 | 墙钟：效率任务（T1/T2/T3）逐任务中位 B ≤ A | 硬 |
| 2 | 缓存比率：Σcached/Σprompt 全体任务 B ≥ A−2pp | 软 |
| 3 | 总 prompt：效率任务逐任务中位 B ≤ A×1.10 | 软 |
| 4 | 结果真进上下文 / 确实取回过（T3、T4 按轮询次数判） | 软 |
| 5 | 进程表归零 + 输出目录有界 | 硬 |
| 6 | 结构：tail-growth 破坏次数 A=B=0；兑现率 B ≥ A−2pp；T4 无一次被"无进展预算"掐死 | 硬 |

判据 1/3 明确**不套 T4**：T4 的回合形状是为测 D1 而故意做重的（6 次串行轮询），拿它比墙钟
是把"为测 A 而设计的任务"塞进 B 的判据里；这一点在跑之前定，不是看到结果后再分。

**校准阶段抓到的两个采集器 bug（都不是链路缺陷，结论性数字全在此之前不可信）**：

1. **LCP 不能算在整条请求体上**：`tools` 在 JSON 里排在 `messages` 之后，整条字节的公共前缀
   会在"上一条的 messages 结尾 / tools 开头"处停下，实测 7 轮全部 `tail=0`、lcp 只有
   0.3k–4.2k（而请求体 18k–22k），而 provider 同时报 cached≈0.97——两者背离就是采集器错。
   改成只比 `messages` 元素区。
2. **外层方括号差一字节**：上一条的收尾 `]` 在下一条同一位置变成 `,`，LCP 恒比上一条长度少
   1（实测 501 vs 502）⇒ `tail` 永远不成立。取元素区（去掉外层 `[` `]`）后归零。
   诊断手段：`SEELEX_ASYNC_AB_DUMPDIR` 把每条请求体落盘，直接用字节比对看分叉点。

**实测结果（2026-09-26 01:30，deepseek-flash，命令 20s，T1-T4 × 3 次 × 两臂 = 24 回合；
原始数据 `_logs/async_ab_20260926.txt`，日志 `_logs/async_ab_full3.log`，用例退出 0）**

| 判据 | 结论 | 实测 |
| --- | --- | --- |
| 1 墙钟（只判 T1/T2/T3） | **成立[硬]** | 逐任务中位 A=[24343 24280 24350] B=[23696 24249 5916]；合计 A=72.97s B=53.86s（**−26.2%**） |
| 2 缓存比率 | 成立[软] | A=0.947（141056/148962） B=**0.956**（327424/342470）——B 反而更高 |
| 3 总 prompt ≤ A×1.10 | **不成立[软]** | 中位 A=[13204 13139 13079] B=[31090 30281 19888]；未命中边际 A=7906 → B=15046 |
| 4 结果真进上下文 | 成立[软] | 4 个任务全部有效样本，无一次不完整 |
| 5 进程/目录 | **成立[硬]** | 进程表增量 0；输出目录 +1/次、日志 ≤19B |
| 6 前缀结构与兑现率 | **成立[硬]** | tail-growth 破坏 **A=0 B=0**（76 个可比请求）；兑现率 A=0.981（93440/95234） B=**1.000**（275200/275300）；被"无进展预算"掐死的样本 = **0** |

逐任务聚合（3 次重复合计）：

| 任务 | A prompt / miss / 请求 / 墙钟 | B prompt / miss / 请求 / 墙钟 | Δprompt | Δmiss(未命中边际) |
| --- | --- | --- | --- | --- |
| T1 长命令并行读码 | 39747 / 2115 / 9 / 73.0s | 93300 / 4084 / 18 / 71.6s | +134.7% | **+93.1%** |
| T2 派发后写文件 | 44004 / 2148 / 10 / 73.3s | 93277 / 4061 / 18 / 71.8s | +112.0% | +89.1% |
| T3 派发后不取回收尾 | 39460 / 2212 / 9 / 73.3s | 54657 / 2433 / 11 / **18.1s** | +38.5% | +10.0% |
| T4 安静长命令轮询 6 次 | 25751 / 1431 / 6 / 69.0s | 101236 / 4468 / 19 / 62.0s | +293.1% | +212.2% |

三条读法（结论只到证据支持的程度）：

1. **轮询不破前缀，这次是算出来的不是推出来的**。结构判据（相邻请求的 `messages` 区
   必须严格尾部生长）在 76 个可比请求上 0 破坏；账面判据（provider 报的命中 ÷ 上一请求
   的 prompt tokens）两臂都 ≈1（A 0.981、B 1.000）。两句合起来才说明"该命中的都命中了，
   多出来的请求一条也没造成额外 miss"——只看命中率 0.956 是无法区分"没破"与"破了但历史
   本来就长"。
2. **成本判据仍不过，且要说清贵在哪**。名义 prompt B 是 A 的 1.1–3.9 倍，但**按全价计费的
   未命中边际**只从 7906 涨到 15046（+90%，绝对 +7.1k token）；差额几乎全是"每轮把历史
   重放一遍"的已缓存前缀。若命中部分按 1/C 折扣计价，两臂等效成本之比约为
   `(95k/C + 15.0k) / (82k/C + 7.9k)`：C=10 时约 +1.2 倍，C→∞ 时收敛到 +90%。
   本表不给具体钱数——账号折扣价不在本仓库可核事实之内。
3. **T4 是 S2 的真实 API 实测位**：命令 20s 全程零输出，B 臂脚本要求轮询 6 次
   （3 次重复 × 6 次 = 18 次载荷逐字节相同的取回）全部 200、无一被 `no observable progress`
   终止，回合内 `polls=6` 全部达成。修前这个形状会从第 6 次取回附近被判死。

**本轮不采信的一列**：「累计前缀账」里的字节复用率（A 0.039 / B 0.115）——分子取的是
`messages` 区、分母取的是整条请求体（含每请求恒定 ~16KB 的 tools），口径混算，绝对值无意义。
两臂同偏见所以先后排序还能看，但既然单位不一致就不该印出来。已在
`async_exec_ab_live_test.go` 改成两边都用 `messages` 区，**未复跑**（不影响上面任何一条判据：
判据 6 用的是结构布尔与 token 兑现率）。

### 10.4 不改（附理由）

- **载荷加时间戳/心跳**：能蒙过 D1，但拿缓存命中率换一个判据（P3 中段改写塌到 0.25 就是代价），不换。
- **`wait_ms` 上限 60s / `until_done`**：单次工具调用钉住会话更久会直接压住用户输入与会话锁，
  是另一条产品取舍；且与 I-22 冲突。
- **写 task 注册表**：见 10.0 修正——会引入跨重启假行。
- **§8.3 已定阈值**：本轮一条都不改，J1–J11 是新增命题。

### 10.5 未决 / 缺口（别当已证）

- **Responses API**：Seele 只有 `/chat/completions`（`agent/core/api/strategy_openai.go:94`）与
  `/v1/messages`（`strategy_anthropic.go:57`），**无 `/v1/responses` 策略层**。轮询对协议的唯一要求是
  "每个调用恰有一条紧邻回执"，`function_call`/`function_call_output` 成对 item 在形状层不会更差，
  但 P6 仍是**未验证**（不得当已排除）。它带服务端状态（`store` + `previous_response_id`）时会与
  "前缀由我们装配"的地基冲突——另议，不在本轮。
- **跨回合无人取回**：打点块缓解、不消除。模型答完就停，本轮之内仍无唤醒链路（推送型已废）。
- **压缩帧是否保留 async 锚点**：句柄只出现在打点块；一旦块的位置或口径变了，J11 结论作废须重测。
- **GUI 人工终止**（R3 砍掉）、**台账 P1 句柄落盘**（R4 砍掉，做它要先解决"未决 tool_use 的
  interrupted 回执合成"）。

### 10.6 门禁与链路验证状态（别把编译通过当成链路已证）

第二轮门禁（2026-09-25，Windows `CGO_ENABLED=1`，输出全部落 `_logs/` 后再 grep，不用 tail 截断）：

```text
go build ./...                                                        exit 0   （async_s3_build.log）
go vet ./...                                                          exit 0   （async_s3_vet.log）
gofmt -l seelebridge application internal e2e gui seelexctx config    空
go test ./seelebridge/... ./application/... ./seelexctx/... ./internal/... -race
                                                                      54 包 ok / 0 FAIL（async_s3_test.log）
go test -tags raceproof ./seelebridge/tools/ -run TestRaceProof -count=20 -race
                                                                      仍报红（2 处 DATA RACE，
                                                                      async_s3_raceproof20.log）
                                                                      ——旧指针形状的反证未被签名改动弄丢
```

原记两条"未验证链接"，**本轮均已闭合**：

1. ~~core 转发没有用例~~ → `TestDeleteSessionReleasesBackgroundRuns`
   （`application/core/session_release_async_test.go`）断言 `DeleteSession` 真的转了
   `Runtime.ReleaseSessionAsync`；`TestAsyncChangeSignalReprojectsWorkTable` 断言变化信号
   真的驱动重投影；`seelebridge/runtime_async_test.go` 逐列断言探针读数搬运不缺列。
2. ~~coordinator 新判据没有红→绿对照~~ → 见 S2.5（同一用例两臂：`pending=0` 被
   `no observable progress` 终止 / `pending>0` 十轮不被终止），`asyncproof` tag 不再需要。

**仍未验证（不得当已证）**：

- **GUI 实机走查**：派发 → 工作表格出现 `async:a3` 行（描述/指令/打点/附件四栏可见）→
  输出增长时行内字节与末行跟着变 → 终态后行落到 completed/failed 且从尾部块消失 →
  `async_kill` 后任务管理器无残留子进程。**未做**（需要 `make rebuild-gui`，其 dist 配置复制
  语义按仓库铁律要先经 owner 确认）。
- ~~真 API A/B 复跑~~ → **已跑（2026-09-26，T1-T4 × 3 × 两臂 = 24 回合）**：三条硬判据
  （墙钟、进程/目录、前缀结构与兑现率）全成立，成本判据仍不成立；数字与读法见 §10.7。
  唯一未复跑的是「字节复用率」那一列的口径修正（不影响任何判据）。
- **`ArchiveSession` 那条调用点**：与被测的 `DeleteSession` 共用同一个 helper
  `releaseSessionAsync`，调用点由读码确认（`archive_session.go`），但**没有**直接用例。
- **`ProcessTree.Degraded()` 的真实退化路径**：Job 建不出来时的 fallback 只在单测里用假树覆盖
  （`TestProbeReportsDegradedProcessTree`），本机从未真实触发（`TestNewProcessTreeNotDegraded`
  证明本机 Job 创建可用）。
- **P6 Responses API**：仍未验证（见 10.5）。
