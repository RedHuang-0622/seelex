# 2026-09-25 压缩：配置化、同会话串行、占用可见

一次性工作包台账（不是长期事实来源）。范围来自当轮对话的三条要求：
① 硬编码的压缩阈值改可配并写进文档；② 压缩改同会话串行阻塞到跑完；
③ 右栏 状态/概要 增加上下文占用条 + 占比曲线，采样点落盘。

## 0. 事实基线（动手前先核，全部为观测所得）

| # | 事实 | 证据 |
|---|---|---|
| F1 | 压缩有两个独立触发层：回合内框架控制器 + 回合边界装配 | `seelexctx/controller.go:237-253`（after_tool/after_assistant）、`application/core/chat.go:184,565`、`history_safety.go:113` |
| F2 | 回合内控制器只处理 `after_tool`/`after_assistant`，`before_model` 三事件都发但无 case | `vendor/.../session/loop.go:229,284,409` 对 `controller.go:238-252` |
| F3 | 显式压缩（`/compact`、`compact_context`）不设阈值前提、不受纪元节流 | `context_runtime/coordinator.go:152-165,517,525-526` |
| F4 | `compact_context` 工具已存在，与 `/compact` 同一落点 | `main.go:410-420`、`application/core/context_compact.go:262-283` |
| F5 | 框架层阈值原本硬编码 75/90/60 + window/8，且不消费 `max_tool_result_chars` | 改前 `controller.go:56-76,287-301`；`seelebridge/runtime_context.go:83,96,110,202`（未注入） |
| F6 | Seelex 前端**没有**上下文占用条：`token_audit`/`context_window`/`hard_threshold` 在 `gui/frontend` 零命中；`git log -S"上下文占用" --all` 空 | 本轮实测 |
| F7 | `TokenAudit` 只活在 `TaskContextProjection`，未进 `Snapshot`，因此前端拿不到 | `application/model/context.go:52-78` |
| F8 | 会话已有运行期输入队列（排队 +  durable 镜像 + 队列投影） | `application/core/service_input.go:132-150,199-221` |
| F9 | 压缩分界虚线与门禁进度条已存在（不是缺口） | `gui/frontend/dist/compaction-format.js:138-142`、`context-summary.js:6,82` |
| F10 | `frameworkSession.Session.ChatStream` 从进函数持 `Session.mu` 到出函数（整段 ReAct 循环、含工具派发都在锁内）；`History/AppendHistory/ClearHistory/ReplaceHistory` 用的是同一把非重入锁 | `vendor/.../Seele/session/chat.go:296-303,155-160,257-284`、`loop.go:358`（`rl.agent.Dispatch` 内联） |
| F11 | 回合内的历史写面**实测拿不到锁**：真实 `Session` + 工具处理函数里调 `sess.History()`，2s 内不返回，取消 ctx 后 `ChatStream` 也不退出 | `internal/adapters/engine_port_reentrance_test.go`（`_logs/s1_reentrance_probe.log`：PASS，4.02s） |

> F6 的结论：用户截图里 `27.4%/90% (26.4K/256K)` 那条不是 Seelex 渲染的（任何版本都没有），
> 归属为对照范本。Seelex 侧真正缺的是"占用可见"本身，按新增能力设计，且不复制它的口径问题。

## 1. 已完成（本轮，已验证）

- [x] **框架层阈值改读 limits**：`ContextWindowPolicy` 带 `SafetyReserveDivisor/SoftPercent/HardPercent/TargetPercent`；`NewContextWindowPolicy(window, output, Limits)` 经 `WithDefaults` 归一；`policy()` 只覆盖账号窗口/输出，不再重建策略丢比例。
- [x] **工具结果上限注入**：`seelexctx.NewToolResultProcessor(r.limits.MaxToolResultChars, …)`（主会话 + 节点）、`ControllerOptions.MaxToolResultChars`（主会话 + 节点）。
- [x] **互钉测试**：`seelexctx/controller_limits_test.go` 三条（比例改变数字 / Budget 覆盖保留比例 / 注入上限改变"算不算超大"判定）。
- [x] **文档**：`config/seelex.yaml` 每个旋钮写"干什么 + 往哪调达到什么效果 + 两层共同消费"；`seelexctx/README.md` 新增「阈值与上限的来源」；修两处注释漂移（工具结果默认 20000 → 实测 60000）；`CHANGELOG.md` Fixed 条目。
- [x] 验证：`go build ./...` exit 0；`go vet ./seelexctx/... ./seelebridge/...` 无输出；`go test ./seelexctx/... -count=1` 9 个包全 ok（日志 `_logs/limits-config-build.log`、`_logs/limits-config-test.log`）。默认值与旧硬编码逐位一致，未改配置=行为不变。

- [x] **S1 同会话串行阻塞（后端）**：显式压缩轮与同会话新回合不再并发读写引擎历史。
  - 状态面：`application/core/service_state.go` 新增 `compacting map[string]struct{}` +
    `compactSig chan struct{}`（`Core.ViewMu` 保护，形状照抄 `restoring`/`restoreSig`）。
  - 门：`application/core/context_compact_gate.go`（`acquireCompactionRound`/
    `releaseCompactionRound`/`awaitCompactionRound`/`deferSubmitUntilCompacted`）。
  - 领轮点：`Service.CompactContextNow`（`/compact` 与 `compact_context` 的同一落点）折叠前领轮、
    回执构造完成后收口；判据点：`submitConversation` 与 `submitConversationFor` 在 `Running`
    判据之后各加一条「该会话有压缩轮在跑」→ 挂到收口点，在同一目标会话上重放。
  - **与原计划的偏差（已核过再改）**：不再走"入队 + 收口时提升队首"。理由是要提升就得跨层回调
    （context_runtime → core）并放宽 `queuedInputUnitLocked` 的 `ErrQueueNotRunning` 口径，而队列
    的正常提升点本来在回合结束——显式压缩没有回合，两条路径叠加只是把简单事实复杂化。仓库里
    `restoring` 已经验证过"挂起-重放"这一形状（提交入口永不阻塞、消息一定发得出去），照抄它。
  - 测试：`TestSubmitParksWhileCompactionRoundOpen`（持轮时提交受理但不成回合，收口后自动补投）、
    `TestCompactionRoundIsAcquiredExclusively`（第二次压缩必须等收口；收口后不残留）、
    `TestCompactionGateIsScopedToItsSession`（按会话键控）。日志 `_logs/s1_gate_test.log`（3 PASS）。
  - 仓库级验证（等另一会话的 `seelebridge/tools|security` 编辑落地后跑）：`go build ./...` exit 0、
    `go vet ./application/core/... ./internal/adapters/...` exit 0（`_logs/s1_vet.log`）、
    `go test ./application/core/ -count=1` **ok 35.956s** exit 0（`_logs/s1_core_test.log`）、
    `gofmt -l` 只报 `seelebridge/tools/computer/screen_linux.go`（别人的在飞文件，`_logs/s1_fmt.log`）。
    `-race` 本轮未跑：本机 `CGO_ENABLED=0`，按仓库约定不得声称已执行。
  - 文档：`application/core/README-context.md` 新增「同会话压缩串行门」一节（触发/改法/加锁纪律/
    有牙证明/边界）；`CHANGELOG.md` Fixed 条目。
  - 未做（刻意）：回执文案不新增"排队中"措辞——提交入口的受理语义没变，被挂起的输入不产生回执；
    门禁进度条/占用条的可见性归 S2。

- [x] **S3 回合内显式压缩改为「当场折叠、当场生效」**（选型＝原方案 B 的方向，但落点既不是
  ContextController 交接、也不是 next-load 把手，而是引擎注入本轮 ctx 的**已持锁把手**）。
  - 判据（先写死再动手）：回合内的压缩**不得再取一次 `Session.mu`**，也**不得**把结果交给
    "下一次装载"——同回合的下一次模型请求必须已经带着折叠后的历史。
  - 引擎侧（Seele，本地 replace）：`session/inloop.go` 新增 `InLoop` 把手 + `InLoopFrom(ctx)`，
    由 `Chat`/`ChatStream` 取锁后注入 ctx，出锁前复位；世代守卫用「单调回合序号 + 进行中标志」
    两枚原子量（只靠序号并在出回合清零 → 下一回合从 1 重数，上一回合泄漏的把手会被误判有效）。
    三个动作（`History`/`ReplaceHistory`/`SetSystemPrompt`）不取锁，语义与锁外方法同源
    （`setSystemPromptLocked` 是同一段实现抽出来的）。`ReActLoop.ReplaceHistory` 的表达式与
    `handleContextEvent` 落地控制器决策时一致。下界：环内替换若会丢掉「assistant 已带
    tool_calls、其结果尚未 append」的在飞尾部 → `ErrInLoopInFlightDropped` 拒收且不动历史。
  - 端口侧：`contract.InLoopEngine`（可选端口，`(false, nil)`＝不在环内要回落、
    `(true, err)`＝在环内被拒**不得**回落）；`internal/adapters/engine_port_inloop.go` 实现——
    环内替换刻意**不 arm** `PrepareMainSessionHistory`（那是"交给下一次装载"），本轮收尾由
    循环自己 `saveToCache → DurableHistory.Save` 落盘。
  - 应用侧：`context_runtime/inloop_history.go` 的 `loopHistoryChannel`（按调用存在的值，不挂
    任何长期状态；`prepareOptions.inLoop` 与显式入参透传 ctx）。折叠路径上三处历史出入口改走
    通道：读 `coordinator.go:464/563` + `hasFoldableSessionContext`，写 `:637` +
    `rejectOversizedToolResults`，prompt `:468`；`PrepareProviderHistoryFor`/
    `PrepareNewHistoryContentFor` 同批改（后者由 `OnIterationComplete` 调用，不再依赖
    `SessionBacked` 那段提前 return 才不自锁）。`withInFlightTail` 只在环内把本轮在飞尾部接回
    折叠产物——不参与压缩判据、不改区间与阈值口径。
  - **策略零改动**：阈值/软硬线/窗口 N/帧构造/记录门槛/门禁顺序一行未动。对拍证据：
    `TestCompactContextInLoopAndOutOfLoopAgree` 用同一引擎类型、两种 ctx，断言判据量
    （compared/assembled/soft/hard）、被压区间（event/message 两端）、reason/origin 与写回的
    provider 历史逐字段相等，且通道计数一边非零、一边必须为零（证明"确实走了通道/确实回落"）。
  - 测试：`internal/adapters/engine_port_inloop_test.go` 三条（即时生效 + 在飞下界拒收 +
    环外不可用）与反向探针 `TestEngineHistoryFromToolHandlerSelfBlocks` 同时 PASS
    （`_logs/s3_inloop_test.log`）；核心层对拍（`_logs/s3_pair.log`）；
    Seele 侧 `session/inloop_test.go` 两条（`_logs/s3_seele_inloop.log`，全量套件
    `_logs/s3_sele_all.log`：仅 `TestRealChain*`/`TestTraceReal` 因本机 key 失效 401）。
  - 文档：`application/core/README-context.md` 新增环内通道一节；`CHANGELOG.md` Fixed；
    本台账把原方案 A/B 的写法作废。发布收尾（**用户执行**）：Seele 打 tag → 去掉
    `go.mod` 末尾的临时 replace → `GOWORK=off go mod vendor`。

- [x] **S3b 锁外折叠的三条 `port.mu → sess.mu` 引信（P1）**：判据「折叠路径上任何一次
  `port.mu` 持有都不得跨越一次可能阻塞的 `Session.mu` 等待」，逐条落地。
  - **对原台账的一处纠正**：写面不止非活跃分支。活跃分支（`engineCalls > 0` 时
    `replaceActiveHistoryLocked`）同样在 `port.mu.Lock()` 里调 `ClearHistory()`/
    `History()`/`AppendHistory()`——它只是"改成延迟安装"了一半（登记了 pending，却仍先
    就地改一次），所以也是引信。而且这次改**没有意义**：跑着的循环读的是
    `rl.history`，并在回合收尾时用整份 `rl.history` 覆盖会话视图，中途改它既被覆盖又
    可能堵死。现在 `engineCalls > 0` 一律只登记，不碰引擎。
  - 读面：`RawHistoryFor` 在 RLock 内只做查表，`engine.History()` 挪到锁外（权威语义
    不变，代价只是调用者自己等，进程不冻结）；`ClearHistory()`（活跃别名版）同改。
  - 登记面：`pendingHistory []types.Message + pendingSession string` → `pendingHistory
    map[string][]types.Message`。单槽装不下"任意会话都可能在飞"这件事，且 legacy
    `ChatStream` 出口会把别的会话的待安装装到自己头上（原代码就是这个形状）。
  - 顺带修掉的两处：① 非活跃分支原先 `ClearHistory()` 后把 `desired` 里的 system 行
    原样再 append 一遍——上游 `ClearHistory` 刻意保留 system，等于每次后台折叠复制一份
    prompt；现在与非活跃/活跃同用 `installHistoryInPlace`（只补缺）。②
    `installSessionEngineLocked` 原来无条件 `port.engine = fresh`，后台会话的折叠会把
    活跃会话切走；现在只有目标=活跃时才换别名。
  - 有牙证明（临时把三处改回修前形状，逐条跑）：`_logs/s3b_prefix_write.log` →
    「对正在跑回合的会话做锁外折叠 在 2s 内没返回」FAIL；`_logs/s3b_prefix_read.log` →
    「空闲会话的开回合被拖住」FAIL。装回修复后 `go test ./internal/adapters -count=1`
    **ok 5.397s**（`_logs/s3b_adapters_full.log`），新增 4 条用例在
    `engine_port_lockfuse_test.go`（非活跃写面 / 活跃写面 / 读面 / 登记按会话键控，
    每条都同时断言"折叠当场返回"与"别的会话照样开得动回合"）。
  - 回归网接住的自伤：`TestEnginePortLazyResumeCreatesOnlyRequestedSession` 报
    factory calls=2——我先 activate 再 install 导致工厂白造一台；改成先装后切别名，
    并把 `activateLocked` 收成"只查表不建引擎"。
  - 仓库级：`go build ./...`、`go vet ./internal/adapters/` exit 0；`go test ./... -count=1`
    **66 ok**，唯一 FAIL 仍是既有的 `e2e/TestEveryGoPackageDirectoryHasReadme`
    （255 处全在 gitignore 的 `vendor/` 里，与本轮无关）。`-race` 未跑（本机
    `CGO_ENABLED=0`，按仓库约定不得声称已执行）。
  - 文档：`internal/adapters/README.md` 新增「并发与锁纪律（EnginePort）」（两把锁的
    半径表 + 三条不变量 + Review 指南 + 测试清单），验证命令路径同步修正
    （`./application/adapters` → `./internal/adapters`）。

- [x] **S4 压缩可见性（前端三处，用户 2026-09-26 点名）**：概要有且仅有压缩栈表格、
  轴上折叠帧用虚线 + 深浅灰分层、对话区用明显分隔线标出最新帧且上方原文仍可读。
  - **概要 = 压缩栈表格**：`#project-overview`（那句与项目名/根路径/状态行重复的英文
    作用域说明）删除，`renderContextCompactions` 从卡片改 3 列表格（栈 / 折叠 / 帧），
    行按 `compactionStackOrder` 栈顶在前；`data-compact-open` 仍带**原数组下标**
    （视图侧 `compactions[index]` 记账，重排换下标就会点一行读另一行）。点开读帧正文，
    弹框入口同排。新增 `compaction-format.js:compactionStackOrder`（前沿判定仍只用
    `compactionFrontier`，不用时间/版本顶替）。
  - **轴多线谱**：`.axis-segment.is-compress` 从实心 3px 条改成**竖向虚线**
    （repeating-linear-gradient），栈顶帧 `is-frontier` 深灰、被更晚折叠取代的
    `is-stale` 浅灰；分界虚线与标签同用最新帧灰阶。色只两个 token
    （`--compaction-frame-latest/-stale` = `--text-strong`/`--faint` 的别名），
    **深色模式倒置由深浅基座自己完成**，不写第二套色值。
  - **对话区**：分界行改成"左右两条淡出虚线 + 一枚说明牌"，牌上明写
    「分界以上的原文仍在下方，可继续上翻阅读；只是不再随请求发给模型」——这句话原来
    只在悬停里，而"能不能读"正是他要的答案，不能靠悬停。
  - 浏览器实测（`_logs/s3c_visual.html`：真实渲染器 + 真实 styles.css，右栏限 300px，
    深浅各一份）：light `latest=#1f2328 / stale=#9096a0`，dark 反成
    `#e4e4e4 / #7e7e7e`；栈行版本色 frontier=rgb(31,35,40)、stale=rgb(144,150,160)；
    刻度 `background-image` 为 dashed 渐变且 frontier 取深灰；分界以上 8 条消息全部
    仍在 DOM 且可读。**第一版四列表格在 280px 里把中文按字符切碎**（截图里
    `message-1..message-663` 断成 `mes/sage-`），据此改成 3 列 + 两排 + 短按钮，
    实测正文列 168–178px、行高 60–64px、无裁切。
  - 测试：`node --test gui/frontend/dist/*.test.mjs` **469/469 pass**
    （`_logs/s3b_js4.log`）。新增/更新的钉：栈序与前沿标记、`is-frontier`/`is-stale`
    刻度、概要不复存在第二内容块、压缩块必须是表格；`status-panel.test.mjs` 的摆放
    口径改成两轮叠加后的现态。
  - 已知取舍（未处理，等他定）：深色模式下 `is-stale` 行文字用 `--faint`(#7e7e7e) 在
    #1f1f1f 上约 4:1，是"过时即弱化"的代价；若要可读性下限，把 `--compaction-frame-stale`
    抬到 `--muted` 即可（一处改，三处同步）。

## 2. 未做（下一切片，按依赖排序）

### S2 占用可见（快照面 + 落盘 + 前端）
- [ ] `Snapshot` 暴露占用面（复用 `model.TokenAudit`：budget/soft/hard/estimated/actual + 窗口），每次装配 bump。
- [ ] 采样点落盘：每回合一条 `{seq, at, compared, estimated, budget, soft, hard, compacted_version}`，走 §2.0 存储清单准入（新增模块需登记；`sessionstore/module_heads.go` 是注册点）。**未定**：与 `compact.jsonl` 同文件序列 vs 独立 `usage.jsonl`。
- [ ] 前端：一条占用条（含软/硬刻度）+ 迷你曲线（纵轴占预算 %、横轴回合、压缩点打标记）。
  **落点已变**：S4 之后「概要有且仅有压缩栈表格」，占用条不能再塞进概要——要么进 状态
  表（`#project-status` 行内或紧随其后），要么另开一个与 概要 平级的小节；动手前先定。
- [ ] 核对用户报的两条现象在 Seelex 侧是否成立：F9 的虚线与进度条已在，需实测"压缩后曲线/条是否真的下降"与"回合间显式压缩时进度条可见性"（此前实测过 2.5s 撤条导致的"看不到动画"）。

## 3. 明确不做

- 不追 Qoder 那条占用条的行为（不在本仓库，无事实可依据）。
- 不新增第二份进程级 limits 持有者：`seelexctx.Limits` 是唯一结构，`application/core/internal/limits` 已持有同一份，框架侧沿用调用方注入。
- 不把 `before_model` 加进控制器：压缩边界必须落在完整协议单元上（F2 是刻意行为，本轮已在 `seelexctx/README.md` 写明）。
