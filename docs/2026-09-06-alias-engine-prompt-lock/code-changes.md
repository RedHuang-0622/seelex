# 热挂载空闲会话被“活跃别名引擎”锁阻塞：复现与修复

日期：2026-09-06
性质：bugfix 工作记录（复现 RED → 修复 → 重跑 GREEN → 回归）
关联：当日「热会话切换 / 运行中会话切换」系列（
[hot-session-switch-intermittent](../2026-09-06-hot-session-switch-intermittent/code-changes.md)、
[session-switch-context-isolation](../2026-09-06-session-switch-context-isolation/code-changes.md)）。

## 1. 现象

两个会话 A、B 都在运行中，点击第三个会话 C 查看时，“切换动作往往要等被切走
的会话跑完才开始”：点击后界面迟迟不切，A（被切走的运行中会话）收尾后切换才
推进。与目标会话是热（驻留）还是冷（restoring 空壳）无关，主要取决于
**进程级“活跃别名引擎”当时指向哪个会话**。

## 2. 链路（锁与数据面）

```
core hotAttachSession(C)（C 空闲驻留）        application/core/session_lifecycle.go
  ├─ SetSystemPromptFor(C, prompt)          → 目标会话引擎，快速
  └─ 收尾 service.Deps.Engine.SetSystemPrompt(systemPrompt)
       → EnginePort.SetSystemPrompt（进程级活跃别名 port.engine）
         → framework session.Session.SetSystemPrompt 取 e.mu
           （Seele session/chat.go：ChatStream 全程持有 e.mu）
           → 若别名引擎 = 被切走的运行中会话 A
             → 阻塞到 A 的 ChatStream 返回（切换才开始）
```

## 3. 根因（已复现确认）

`EnginePort` 维护单一进程级“活跃别名”`port.engine/port.sessionID`。别名在
冷加载/物化/`ResumeSession` 时切换，热挂载只移视图指针、不动别名——因此别名
可以停留在某个**正在运行**的会话引擎上。core 热挂载空闲目标时，在已按目标
会话 `SetSystemPromptFor` 之后又无条件调一次全局 `SetSystemPrompt`：

- 路由引擎（`SessionChatEngine`）下该调用是冗余的（目标 prompt 已写入）；
- 但 `EnginePort.SetSystemPrompt` 走的是 `port.engine`（别名），若别名指向
  运行中会话 A，`framework Session.SetSystemPrompt` 会等在 `ChatStream` 全程
  持有的 `e.mu` 上 → 切换 RPC 阻塞到 A 收尾，即用户看到的“被切走的会话进行
  完才开始下一步切换动作”。

冷恢复路径（`resumeSessionCold` 收尾）也有同样的尾随全局写；它通常因
`ResumeRawSession` 已把别名切到目标而不阻塞，但存在与并发热切换相同的隐患。

## 4. 复现（RED → GREEN）

`repro_two_running_view_third_then_switched_finishes_test.go`
`TestSwitchToIdleThirdBlocksWhileAliasEngineSessionRuns`（生产全链路 E2E）：

1. 种子会话 A/B/C；卸载 A 再冷加载 → 活跃别名 = A 的引擎；
2. B 长任务在跑（request #4 阻塞）；A 长任务在跑（request #5 阻塞，
   别名引擎 A 被 ChatStream 持锁）；
3. 点击空闲热会话 C：ResumeSession(C) 必须 2s 内返回。

修复前：`ResumeSession(C)` 阻塞 >2s，放行 A 后才完成
（RED REPRO 输出）。修复后：立即返回并切到 C（GREEN）。

另含两组配套时序回归（hot/cold 两个会话在跑 + 被切走会话收尾后再切换），
以及 `-race` 高频竞争版本（A 收尾 ↔ C/B/A 反复切换）。

## 5. 修复

1. `internal/adapters/engine_port.go`：`SetSystemPromptFor` 在写目标会话引擎
   的同时同步进程级 prompt 缓存 `port.systemPrompt`（新建引擎仍自动继承）；
   绝不触碰 `port.engine`（活跃别名）。
2. `application/core/session_lifecycle.go`（热挂载收尾）与
   `application/core/session_history.go`（冷恢复收尾）：会话路由引擎下不再
   尾随调用全局 `SetSystemPrompt`；非路由单会话引擎（无跨会话别名面）保留
   原全局写。

不变量：**会话路由引擎下，切换/装载只按目标会话写 prompt；任何路径不得因
写 prompt 而阻塞在运行中会话的 framework `Session` 锁上。**

## 6. 验证

```text
go test . -run 'TestSwitchToIdleThirdBlocksWhileAliasEngineSessionRuns|TestTwoRunningViewThirdThenSwitchedFinishes'  ok
go test -race . -run 'TestSwitchToIdleThirdBlocksWhileAliasEngineSessionRuns|TestTwoRunningViewThirdThenSwitchedFinishes|TestTwoRunningViewThirdSwitchStress|TestThreeRunningSessionsSwitchDoesNotWaitAndKeepsContext'  ok
go test ./application/core ./seelebridge ./internal/adapters -count=1   ok
gofmt -l 改动文件 / go vet（core、adapters、根包）                        干净
```

## 7. 改动文件

- `internal/adapters/engine_port.go`（SetSystemPromptFor 同步缓存 + 不写别名）
- `application/core/session_lifecycle.go`（热挂载收尾跳过路由引擎全局写）
- `application/core/session_history.go`（冷恢复收尾跳过路由引擎全局写）
- `repro_two_running_view_third_then_switched_finishes_test.go`（新增 RED→GREEN 回归 + 配套时序/竞争用例）
- `application/core/README.md`（切换 prompt 写路径不变量 + Review 清单）
