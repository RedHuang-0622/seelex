# goal 回合三段式（ABBA 的另一半）+ goal 变更/取消的权限收口

- 日期：2026-09-29
- 范围：锁面审计 §4.1（"ABBA 的另一半"）+ 用户当场追加的两条语义（A / B）与一条权限诉求
- 不改：corr 信封与协议字段、`peer` 状态取值集合（前端仍认 `evaluating` / `advisory_pending`）、
  headless RPC 作为**人类/运维通道**的语义、B4 缺席矩阵本身

## 1. 改前：ABBA 的另一半，以及它顺带掩住的两件事

`Supervisor.RunEval` 用一把锁圈住**整轮** b 评审：

```go
s.mu.Lock()
defer s.mu.Unlock()
return s.runRoundLocked(ctx, trigger, ...)   // 准入 + evaluator.Evaluate + 提交 + 记录器
```

上一批给 `roleRound` 加回合闸门时把这条边记成了"设计级、未动"（见
`2026-09-29-lock-audit-fixes.md` / `store-lock-and-runtime-entry-batch.md` / `port-lock-host-handoff.md`
的"未决"节），本批处理。

后果有三条，前两条是死锁面，第三条是功能面：

1. **同 goroutine 自锁死**：`s.mu` 横跨 `evaluator.Evaluate`（模型调用 + b 的只读工具回合，
   秒到分钟级）。这一期间任何"回合内回头找 Supervisor"的回调——流式分片、迭代钩子、工具
   handler——只要走到 `s.mu` 就是**同一把非重入锁的重入**。这不是理论风险：上一批的
   `inFlightMu` 之所以要存在，正是因为"流式回调与持 `s.mu` 者同 goroutine"是一条**跨包契约**
   （`seelebridge` 把 `spec.OnDelta` 原样交给引擎，无从强制）。
2. **`s.mu ↔ roundGate` 成环**：`s.mu → roundGate`（回合内跑角色回合）是实测边；
   `roundGate → s.mu`（回合内的工具/钩子回头找 Supervisor）是纪律要防的形态。只要两者
   同时存在就是 ABBA。
3. **in-flight 近端形同虚设**：`Snapshot()` 用阻塞式 `s.mu.Lock`。`peer=evaluating` 期间前端
   每秒的轮询一直被挡到回合结束，而放行时 `clearInFlight()` 已经执行——用户"渲染不及时"的
   原始诉求（上一批做的进行中正文）在真实 GUI 里**根本没有窗口能看到**。

## 2. 改法：三段式（拆锁），不用 channel，不用 actor

选型理由（先排除另两条）：`gate`/预筛要**同步等结果**（EXEC 提收口必须拿到裁决），走 channel
就得再造一个 mailbox goroutine + 请求/应答 + 取消超时，等于手搓 actor 却仍要等结果；整体改
actor（单 goroutine 拥有 goal 状态）改动面覆盖 `goal` + `goal_coordinator` + 装配层 + headless +
前端投影，**收益与拆锁相同**（都只是让"执行期间不持锁"）。以后真需要时再做，不是本批的解。

结构（`techleader.go`）：

```text
RunEval / Notify / 终态 gate / 审批预筛
   └─ runRound
        ├─ beginRound      → beginRoundLocked（s.mu 内：判定 + 补帧 + 渲染 b 输入 + 占租约）
        ├─ evaluateRound   （**s.mu 外**：evaluator.Evaluate，模型与角色回合都在这）
        ├─ commitRound     → commitRoundLocked（s.mu 内：复核 goal + 校验裁决 + 落回合段 + 发信封 + 记账）
        └─ recordRound     （s.mu 外：记录器 = 宿主/落盘活）
```

**为什么环在结构上不成立**：`beginRoundLocked` 只做判定与账本追加（微秒级），`commitRoundLocked`
只改内存与发信封，二者都不调 `roundGate`。因此"持 `s.mu` 等 `roundGate`"的一方不存在，
`roundGate → s.mu` 这条反向边即使将来出现也不成环。同 goroutine 的重入问题同时消失。

**回合租约 `roundInFlight`（s.mu 下）**：旧实现靠"锁住整轮"顺带实现互斥，拆开后必须显式化。
它是**不可重入闸门且不排队**——排队等于把调用方挂在一个可能永远不结束的回合上（与上一批
`roleRound` 的 `ErrRoleRoundReentrant` 同一条纪律）。各调用点按自己的语义处理：

| 调用点 | 已有回合在飞时 | 理由 |
|---|---|---|
| `Notify`（a 事件登记） | 登记照旧、**本轮不评**（返回 nil） | 自助登记为主：事件落在 `execSeq` 账本上，下一次触发把它一起带进 b 的输入（用例钉住） |
| `ProposeFinish`（终态 gate） | `OutcomeEscalate`，goal 保持 active，**不 reap** | B4 铁律"a 永不等待 b"；刻意不 reap 是在飞那一轮还在用这个 peer |
| `PreScreenApproval` | `OutcomeEscalate`（转人工） | 审批侧默认拒绝兜底不变 |
| `AdvisorSeat.Act`（治理座位） | 良性跳过（零动作、不 break、不写 roundError） | 否则用户看到一条与实际相反的"治理回合失败" |
| `RunEval` / headless `goal_tl_eval` | `ErrRoundInFlight` | 外部边界显式失败，由调用方决定（headless 直接报错） |

**记录器移出锁**（`recordRound`）：① 它是 tl role draft → sequencer → 主文档的落盘活，压在
`s.mu` 上就是让整轮治理等文件 I/O；② 它可能回头取宿主侧锁（`ViewMu` 等），在 `s.mu` 内调用
就是又造一条 `s.mu → 宿主锁` 的边。顺序不变（先发 corr 信封，再落原文），best-effort 不变。

## 3. A 语义（用户确认）：在飞不排队，按 B4 缺席默认

见上表：gate / 预筛遇到在飞立即升级人工，goal 保持 active。**不 reap** 是这次特意加的边界——
旧的"回合失败"分支会 `unbindIfTerminal("evicted_round_failure")`，如果照搬到"在飞"，就会把
正在跑的那一轮的 peer 上下文拆掉。

## 4. B 语义（用户确认）：取消 → 丢弃；变更 → 补 update

执行段挪出锁之后，一个旧实现**不可能出现**的状态成了可能：**回合执行期间顶栈 goal 被改或被收口**
（逃生 `AbortOnEscape`、人类/运维的 `goal_finish`/`goal_abort`、嵌套压栈）。提交段因此复核顶栈：

- **收口 / 取消**（栈空，或顶栈已换成别的 goal）→ 丢弃这一回合的结论：不发 corr 信封、不落 b
  回合段、不计 `evalCount`、不交给记录器，返回 `ErrRoundGoalGone`；gate 收到后按升级人工处理
  并 reap（旧 peer 绑的是已经不存在或已 paused 的目标）。**必须显式判**的后果是具体的：
  否则 gate 会拿一份属于旧 goal 的裁决去 `Finish` **新**的栈顶。
- **同一个 goal 被改**（更新）→ 结论照常落地，并补一条 `goal.update` 差异帧
  （`noteGoalChangedLocked`，`source=goal_updated_during_round`），补帧游标对齐当前 progress
  条数，下一次准入不再重复补同一段差异。

判据用 `goalStampOf` 指纹（ID / 状态 / 标题 / 正文 / 完成条件 / 进度条数与尾条），**不用
`UpdatedAt` 单判**：域时间是秒级（`time.Now().Unix()`），同一秒内的两次更新会得到同一个时间戳，
"改过但判不出"正是这里最不该有的漏。

## 5. 权限：goal 的修改与取消只经 TL（用户诉求）

三条通道现在口径明确：

| 动作 | agent 工具面（EXEC / 员工 / 子代理） | TL（ADVISOR）裁决侧 | 人类 / 运维面（headless RPC、显式会话 API） |
|---|---|---|---|
| 追加进度（`progress_*`） | 允许 | — | 允许 |
| 改定义（标题/正文/完成条件/范围） | **拒绝** | 允许 | 允许 |
| 取消（finish / abort） | 无此工具：只有 `goal_propose_finish`（提议）→ TL 裁决 → 才收口 | 允许（`verdict_done` / 逃生 `AbortOnEscape`） | 允许（显式人工操作） |

落地：

- `goal.UpdateRequest.ChangesDefinition()` 把"改定义"与"追加进度"分开（后者是执行侧汇报进展，
  不是重写目标）；`application/core` 的 `authorizeAgentGoalMutation` 是**唯一判定点**，
  `goalUpdateHandler` 是唯一调用点；
- `goal_update` 的 **schema 收窄**（去掉 `title`/`statement`）与描述改写，让模型不会去试；
- 为什么不让 agent 改定义：改定义 = 在被审查的目标上单方面换掉验收标准，ADVISOR 的评审依据
  当场失效（第 4 节的 B 语义正是这件事的兜底，这里是源头收口）；
- 为什么不拦人类/运维面：人不是 agent。拦掉它等于把"用户无法取消/修正自己的目标"当成安全，
  而且环逃生必须保留一条不过 gate 的收口口（`escape.go` 已论证：b 正是被判"不值得再问"的一侧）。
- **诚实边界**：未启用 ADVISOR 的会话里，`goal_propose_finish` 仍走 B4 直连收口
  （`OutcomeNoTL`）——那是"没有 b 可问"的安全默认，由 `TechLeaderConfig.Enabled` 决定，
  不由调用者决定，本批不以它为目标。

## 6. 验证

新增用例（`application/core/goal/round_lock_test.go`，每条都带显式超时：形状错了要失败，
不是把套件挂死）：

| 用例 | 钉住的形状 |
|---|---|
| `TestRoundDoesNotHoldSupervisorLockWhileEvaluating` | 在飞时 `Snapshot()` 立刻返回、`peer=evaluating`、`InFlight` 读得到 |
| `TestRoundInFlightIsExplicitErrorNotQueue` | 第二个入口得 `ErrRoundInFlight`；回合结束后闸门重新可用 |
| `TestProposeFinishWhileRoundInFlightEscalates` | A 语义：gate 立刻升级、goal 保持 active |
| `TestPreScreenApprovalWhileRoundInFlightEscalates` | A 语义：预筛立刻转人工 |
| `TestInRoundCallbackDoesNotDeadlock` | 回合内回调 `Notify` 不死锁、不启动第二个回合、事件不丢（下一次回合的 `work.progress` 帧里能看到它） |
| `TestRoundDiscardedWhenGoalClosedDuringRound` | B 语义：丢弃（0 信封 / 0 回合段 / 0 计数）+ `ErrRoundGoalGone` |
| `TestRoundEmitsGoalUpdateFrameWhenGoalChangedDuringRound` | B 语义：结论照落 + 补 `goal.update` 帧（帧详情带新进度） |
| `TestAdvisorSeatSkipsWhenRoundInFlight` | 座位良性跳过（不 break、不 error、有说明） |

`application/core/goal_permission_test.go`：`TestAgentGoalUpdateCannotRewriteDefinition`——
agent 面四类定义字段逐个被拒（`errGoalDefinitionTLOnly`）且**不留痕**；追加进度允许；显式会话
API（人类面）仍能改定义；提议收口的结果是 TL 裁决或 B4 缺席回退。

**红演示（有牙，实测）**：把 `runRound` 临时还原成旧形状（整轮持 `s.mu`，直接调
`beginRoundLocked`/`commitRoundLocked`）后跑 `-run Round`，上述 8 条里 6 条转红，且症状与
预测逐条对应：

```text
--- FAIL: TestRoundDoesNotHoldSupervisorLockWhileEvaluating  Snapshot 被挡在 s.mu 外超时未返回
--- FAIL: TestRoundInFlightIsExplicitErrorNotQueue           第二个回合入口被排队挂住
--- FAIL: TestProposeFinishWhileRoundInFlightEscalates       终态 gate 被排队挂住
--- FAIL: TestPreScreenApprovalWhileRoundInFlightEscalates   审批预筛被排队挂住
--- FAIL: TestInRoundCallbackDoesNotDeadlock                 回合超时未结束（自锁死）
--- FAIL: TestAdvisorSeatSkipsWhenRoundInFlight              治理座位被排队挂住
```

（两条 B 语义用例在旧形状下同样通过——它们钉的是提交段语义，与锁面无关，这也是一致性检查：
说明红演示不是"整包炸"，而是**精确指向锁面**。）复原后全绿。

命令与结果：

```text
gofmt -l application/core/goal application/core/goal_service.go register_goal_tools.go   # 空
go build ./...                                                                          # ok
go vet ./application/core/... ./seelebridge/... .                                       # ok
go test ./application/core/goal/ -count=1                                               # ok
go test -race ./application/core/goal/ -count=1                                         # ok
go test ./application/core/ -count=1                                                    # ok
go test ./seelebridge/... -count=1                                                      # ok
```

## 7. 未决 / 风险（诚实标注）

1. **活体反向边未复现**：本批改前的只读勘察没有找到活的 `roundGate → s.mu` 边（b 的工具面是
   readonly，迭代钩子挂在主会话路径上）。因此"ABBA 成环"是**纪律要防的形态**，不是已观察到
   的现场事故；本批的价值在于把这条边对应的**前提**（持 `s.mu` 等 `roundGate`）从代码里去掉，
   而不是修一个复现过的环。同 goroutine 自锁死则是由形状直接推出的（用例 `TestInRoundCallbackDoesNotDeadlock`
   就是那条形状的活体判据）。
2. **`ViewMu → s.mu` 仍存在**：`application/core/runtime_projection.go` 在 `ViewMu.RLock` 内调
   `sup.Snapshot()`。它单独不成环（本批已把记录器移出 `s.mu`，`s.mu → 宿主锁` 的那一半被拿掉），
   但仍是"锁内调宿主"的同族形态，留给下一批。
3. **丢弃的回合不落文档**：被丢弃的回合不进 tl role draft（否则文档里会出现一条从未落地的裁决
   行）。代价是该回合的原文只在进程内消失，审计上看不到——逃生路径有归档（`ArchiveTLHistory`），
   普通收口期间的丢弃没有。若需要"留痕但不生效"，改 `ErrRoundGoalGone` 分支调记录器即可（一行）。
4. **TL 只"有资格"改定义，但还没有主动改定义的工具**：现在的通道是裁决（含 `verdict_done`
   收口）与纠偏指令；要让 TL 直接改写 acceptance/标题，需要给 `TLDirective` 加载荷字段（协议级
   扩展），留给下一批。
5. `application/core/README-work-table.md` 的生成器漂移（`refreshWorkTableLocked` 签名）属上一批，
   本批**未混入**，保持工作区干净。
