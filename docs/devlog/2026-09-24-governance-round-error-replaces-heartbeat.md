# 治理面板不再猜墙钟：心跳下线、回合失败原因上桌面（2026-09-24）

> 触发（用户裁定）：`docs/devlog/2026-09-23-cold-start-draft-row-and-worktable-session-key.md`
> §5 把 `governance stalled` 定性为「前端过度断言」并留作决策项；本轮裁定为
> 「要么给出详细报错，要么不让它出现」，并追问 goal 的 A2A 为什么需要心跳。
>
> 结论：两个都做——墙钟判据连同它的数据源（心跳）一起删掉，被丢弃的治理回合错误
> 接回只读投影。**没有新增字段落点**：`HeartbeatAt` 的唯一读者是墙钟判据，
> `HeartbeatSeq` 的唯一读者是「心跳 #N」装饰文案。

## 1. 为什么原来会有心跳（调查结论，非推测）

心跳不是为了 A2A 协议，是为了绕开一个渲染时机问题：

- 治理回合是**同步**跑完的——`Service.goalAdvanceAfterChat`（`application/core/goal_service.go`）
  在 `ChatStream` 返回后的锁外安全点把整轮 Governor 跑掉，后端直到回合结束才推一次
  投影。回合进行中，面板上的 `Round`/`peer` 一动不动。
- 于是当时的设计（`docs/2026-09-08-govern-loop/design.md` §2「心跳」）给协调器加了
  `HeartbeatSeq/HeartbeatAt` 打点，让前端用「seq 还在涨 = 治理活着」当活度证据。
- 这个证据后来被更好的东西取代了：`peer_state`（b 生命周期 `evaluating`）与
  `in_flight`/`in_flight_chars`（进行中正文）。前端 `refreshGoalInFlight` 已经在用它们
  决定是否轮询快照。**心跳因此退化成同一事实的第二套口径**，且只被展示消费：
  全仓除视图组装外没有任何后端消费者（无 watchdog、无超时判定）。

## 2. 真正的缺口：错误根本没地方去

- `application/core/goal/techleader.go` 在评估失败时已经把原因分好类并抛出来：
  `ErrBadDirective` → 「b 回合已作答但裁决不可用」，其余 → 「b 回合失败(429/超时 → B4 缺席矩阵)」；
  同时 `peer.State = PeerAdvisoryPending`，goal 保持 `active`（安全默认：不拿不可用的裁决收口）。
- 但 `Service.goalAdvanceAfterChat` 用 `_ =` 把 `AdvanceAfterChat` 的返回丢弃，
  `chat.go` 也不看它的结果。结果是**后端知道、面板不知道**。
- 面板只剩墙钟可依据：`floor(now) > heartbeat_at + 10` 一律印 `governance stalled`。
  于是「目标空闲等你输入」（Round 0、无 ADVISOR 在飞）与「一次轮转被中止」共用同一句话。

## 3. 改法（走权威面，不加固补丁）

| 位置 | 改动 |
|---|---|
| `application/contract/dto/projection.go` | 删 `HeartbeatAt`/`HeartbeatSeq`，加 `RoundError string json:"round_error,omitempty"`（只读投影，进程内，不落盘） |
| `application/core/goal_coordinator.go` | 删心跳三件套（`heartbeatSeq`/`heartbeatAt`/`bumpHeartbeat`，连带失去唯一读者的 `now` 时间源与 `time` 依赖）；新增 `roundError map[string]string` 与 `noteRoundError`；`AdvanceAfterChat` 拆成「公开包装＝唯一登记点 + 私有 `advanceAfterChat`＝原逻辑」，`Next` 同步登记；`Begin` 换到新 goal 时清除 |
| `application/core/goal_service.go` | 保留 `_ =`（聊天回合本身成功，不能因治理失败报错给用户），注释写明失败原因的去处 |
| `gui/frontend/dist/app.js` | 删 `startGoalStallMonitor` 的墙钟判定与 `governance stalled`/`心跳 #N` 文案；定时器只保留「进行中快照轮询」职责，改名 `startGoalInFlightPoller`；新增 `round_error` 渲染行 |
| `gui/frontend/dist/styles.css` | `.goal-gov-stalled` → `.goal-gov-error` |
| `tui/goalteam.go` | 删 `心跳 #N`；新增 `✖ 本轮治理未完成: <原因>`，与 `✖ 治理已中断`（断环）分行 |

`ErrTLDisabled`（未装配评估器）按配置态处理，不记为失败。

## 4. 现在的三种可见状态（互不重叠）

- 评审在飞：`peer_state=evaluating` + `in_flight` 正文增量（原有能力，未动）。
- 本轮治理未完成：`round_error` 非空 → 面板/TUI 给出分类后的原始错误文本，goal 仍 `active`，
  下一轮继续尝试。
- 环已收束：`broken` + `break_reason`（原有断环横幅，未动）。
- 除此之外**不显示任何活度提示**——空闲不是异常，不需要提示。

## 5. 验证

- `gofmt -l application gui tui` 干净；`go build ./...` 通过；`go vet ./application/... ./tui/...` 通过。
- 测试口径同步：`goal_coordinator_test.go`（心跳单调测试 → `TestGoalCoordinatorRoundErrorVisible`
  钉住「失败可见 + 新 goal 清除」，成功回合补 `RoundError==""` 断言）、
  `tui/goalteam_test.go`（面板 fixture 带 `RoundError`，断言含「本轮治理未完成」）、
  `gui/headless_goal_test.go`（fake 视图改用心跳以外的字段）。
- 前端：无第三方依赖，`node --test gui/frontend/dist/*.test.mjs` 口径下无 stalled 相关断言。
- 未做：没有真机 GUI 复现一次「ADVISOR 回合失败」并截图（需要构造 429/坏裁决）；
  面板文案的实际观感未在浏览器里核对。

## 6. 遗留

1. `Service.goalCoordinatorFor` 失败时 `goalAdvanceAfterChat` 静默返回（装配期故障）——
   它不属于「治理回合失败」，现在也没有可见出口；若真要覆盖，应走同一条 `round_error` 面。
2. 治理回合失败**不自动重试**：要等下一次 chat 结束的 `turn_completed`。这是既有策略，本轮未动。
