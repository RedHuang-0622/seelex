# 迭代边界注入撞会话锁：队列提升轮自锁死（2026-09-22 现场 / 2026-09-23 修复）

> 日期: 2026-09-23 | 范围: `application/core/{goal_service.go,chat.go}`、
> `application/contract/ports.go`
> 回归: `application/core/goal_directive_session_lock_test.go`（新，硬超时 + 全量 goroutine dump）
> 关联: [2026-09-16 advisor 裁决立刻可见](2026-09-16-advisor-verdict-visible-immediately.md)（注入时机口径）、
> [2026-09-22 Agent Team 发言环不含 user](2026-09-22-agentteam-ring-excludes-user.md)（"每轮 exec 由一条 user/队列输入驱动"）

## 1. 现场（用户口径）

dev GUI 用到一半整块冻住：某个会话永远停在「执行中」，运行期间排队的那条输入
一直挂在 `排队 01 · 等待`，**再也不发出去**；同一时刻另一个会话也是「运行中」；
取消/关任务都无效（任务残留关不掉）。进程侧取证：CPU 近乎 0、**没有任何 TCP
连接**（不是等 provider）、数据根 `lock.owner` 心跳照常（不是进程挂了）、磁盘从
最后一笔提交起再无写入 —— 一个卡在锁上的 Go 进程。

> 诊断期我为了取 goroutine 栈 attach 过 Delve（该 dev 产物带 `-s -w`，没有
> DWARF，`dlv` 只报 `could not find goroutine array`），attach 会让进程停住，
> 已用 `RPCServer.Detach{kill:false}` 脱离；该实例随后退出，**会话数据没有丢**
> （全部在 `dist/seelex-gui-dev/.seelex` 落盘）。事后没再复用过这条路径。

## 2. 根因（一条链，每一环都是生产代码）

```text
会话运行中收下一条输入 → 进本会话消息队列（"排队 01 · 等待"）
运行轮收尾：回合尾治理跑 ADVISOR 回合 → TL 指令进待注入队列（Peek 回放可见）
同一收尾把队列里的输入提升为下一轮（提升路径直接 go runChat，
        不经过 startChatFor）
提升出来的这一轮跑到工具迭代边界 → OnIterationComplete（**会话锁内**）
→ Service.GoalIterationCompleted 排空指令 → injectGoalDirectives
→ appendEngineMessage → EnginePort.AppendHistoryFor → Session.AppendHistory
→ 取「ChatStream 全程持有的那把锁」→ 同 goroutine 自锁死
```

关键是 Seele v0.3.0 `session/chat.go` 的锁纪律：

| 方法 | 锁 |
|---|---|
| `Session.ChatStream` | `e.mu.Lock()` 进函数 → `defer Unlock()` 出函数（整轮持有） |
| `Session.AppendHistory` / `History` / `ClearHistory` / `SetSystemPrompt` | 取**同一把** `e.mu`（不可重入） |
| `Session.HistoryIfAvailable` | `TryLock` + 已发布快照（观测面专用，永不阻塞） |

`application/core/tool_hooks.go` 的注释早就写着「回调不得重入 Session 的历史操作
（History/ReplaceHistory/AppendHistory），否则死锁」，但同一段回调里的
`svc.GoalIterationCompleted(ctx)` 仍然调了 `AppendHistory`；`GoalIterationCompleted`
的注释则写着「Session 锁内只允许 AppendHistory」——**两条口径相互矛盾，后者不成立**。
`contract.ChatEngine.AppendHistory` 的注释（"仅在 OnIterationComplete 中调用，
不加锁"）是这条误解的源头，本轮一并改掉。

## 3. 修法：注入点回到"下一次 ChatStream 前"（锁外），迭代边界只登记

| # | 位置 | 改动 |
|---|---|---|
| 1 | `application/core/goal_service.go` | `GoalIterationCompleted` 只做 `Notify(turn_completed)`；**不再 Drain/注入**（指令留在待注入队列，下一次安全点交付） |
| 2 | `application/core/chat.go` | `runChat` 起手（`PrepareExecutionContextFor` 之前）补 `injectGoalDirectivesForStart` —— 队列提升出来的这一轮不经过 `startChatFor`，少了这一处裁决会晚一整轮 |
| 3 | `application/contract/ports.go` | `AppendHistory` 注释改成真规则：循环回调里**禁止**调用（ChatStream 同 goroutine 且持会话锁），应用侧注入一律放锁外安全点 |

语义边界（刻意保持）：

- **受信注入时机不变**：仍是"下一次 ChatStream 前"（`injectGoalDirectivesForStart`
  是唯一消费者），只是把队列提升轮也纳入这条口径；
- **可见回放不变**：裁决产出它的那一回合就可见（`publishPendingGoalDirectivesFor`
  用非消费的 `PeekDirectives`，见 2026-09-16 那篇）；
- **模型看到的裁决也不变**：`PrepareExecutionContextFor` 每轮按 transcript 重建
  provider wire，可见回放写的 `assistant + role_name=tl` 行本就在其中；
- 本轮**没有**把注入搬进 `ContextController`（那才是"锁内注入"的合法通道），
  属于独立决定，见 §5。

## 4. 验证：红 → 绿

新增 `application/core/goal_directive_session_lock_test.go`：夹具按 Seele v0.3.0
锁纪律写（`ChatStream` 全程持锁；`History/AppendHistory/ClearHistory/
ReplaceHistory/SetSystemPrompt` 取同一把锁），并驱动一轮真实工具迭代回调。

```text
# 修复前（RED，超时 + 全量 goroutine dump）：
$ go test ./application/core/ -run TestQueuedRoundMustNotReenterSessionLock -count=1 -v
    goal_directive_session_lock_test.go:212: RED: 队列提升的下一轮卡在会话锁上（会话永远「运行中」，排队消息发不出去，任务关不掉）
        goroutine 51 [sync.Mutex.Lock]:
        application/core.(*sessionLockEngine).AppendHistory(...)
          <- AppendHistoryFor <- goal_service.go:245 injectGoalDirectives
          <- goal_service.go:226 GoalIterationCompleted <- tool_hooks.go:358 OnIterationComplete
          <- sessionLockEngine.ChatStream（持锁） <- chatStream <- runChat
        created by ...runChat in goroutine 15      # 队列提升那一轮

# 修复后（GREEN）：
--- PASS: TestQueuedRoundMustNotReenterSessionLock (0.09s)
```

全量：

```text
$ go build ./...                                        → ok
$ go test ./application/... ./session/... ./internal/... ./tui/... -count=1
    → 23 包全 ok（application/core 19.2s）
$ go test . -count=1                                    → ok 19.6s（根包全链路复现用例）
$ gofmt -l application/core application/contract        → 空
```

## 5. 未做 / 风险

- **锁内注入的合法通道仍未接线**：真要"同一轮内把 TL 指令喂给模型"，应走
  `ContextController` 的 `ReplaceHistory` 决策（循环内同 goroutine、不需要会话锁），
  需要在 `seelexctx.ControllerOptions` 加一个"待注入消息"提供者，并由 seelebridge
  注入。本轮不做（改动面大于缺陷本身，且现有可见回放已把裁决送到模型上下文）。
- **`session_runtime/session_writer_test.go` 的构建失败已顺手修掉**（`writerTestSessions`
  缺 `SessionPort` 面）：它属于另一条未提交工作流的夹具，`go vet`/`go test` 在
  `./application/core/...` 上会因此整包红，无法验收本修复。夹具改成内嵌同包
  `granularPortTestSessions`，不再手写空实现。
- **实机未复核**：本轮的绿灯落在确定性用例与全量包测试上；dev GUI 现场（用户那次
  冻住）已随进程退出消失，重启后同一场景需按 §2 的时序复跑一次。
